package apply

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vaporphd/zprof/internal/managed"
	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/overlay"
)

// TestE2E_GuardDeploysAndEnforcesForcePush is AC11: a full `zprof apply`
// against the real base profile (profiles/base/guard.yaml,
// profiles/base/zprof-guard.py — not a Go-side fixture stub) must produce a
// settings.local.json with both guard hook entries and a populated
// permissions.deny, and the deployed .claude/zprof-guard.py must actually
// deny a force-push Bash call and allow an ordinary one when invoked exactly
// as Claude Code would invoke it: `<script> pre-tool` with the tool-call
// JSON on stdin.
//
// Skips (not fails) when python3 isn't on PATH — the same fail-open posture
// as the hook itself, applied to CI environments that don't ship Python.
func TestE2E_GuardDeploysAndEnforcesForcePush(t *testing.T) {
	python3, lookErr := exec.LookPath("python3")
	if lookErr != nil {
		t.Skip("python3 not found in PATH, skipping guard subprocess E2E")
	}

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)
	profilesDir := filepath.Join(root, "profiles")
	fixture := filepath.Join(root, "cli", "testdata", "projects", "smoke-ios")

	proj := t.TempDir()
	copyDir(t, fixture, proj)

	base, err := overlay.LoadBase(filepath.Join(profilesDir, "base"))
	require.NoError(t, err)
	require.NotEmpty(t, base.GuardScript, "profiles/base/zprof-guard.py must be loaded")
	require.NotEmpty(t, base.GuardSchema, "profiles/base/guard.yaml must be loaded")
	ios, err := overlay.LoadOverlay(filepath.Join(profilesDir, "overlays", "ios-swift"))
	require.NoError(t, err)

	_, err = Apply(ApplyOpts{
		ProjectDir: proj, Base: base, Overlays: []*overlay.Overlay{ios},
		Project:   &manifest.ProjectManifest{Overlays: []string{"ios-swift"}, Language: "ru"},
		MergeMode: managed.ModeOverwrite,
	})
	require.NoError(t, err)

	// --- 1. settings.local.json has both guard hook records + deny -------
	settingsData, err := readSettings(filepath.Join(proj, ".claude", "settings.local.json"))
	require.NoError(t, err)

	hooks, _ := settingsData["hooks"].(map[string]any)
	require.NotNil(t, hooks)

	preEntries, _ := hooks["PreToolUse"].([]any)
	require.Len(t, preEntries, 1)
	pre := preEntries[0].(map[string]any)
	require.Equal(t, "Bash|Edit|Write|MultiEdit|NotebookEdit", pre["matcher"])
	require.Contains(t, hookCommand(pre), "zprof-guard.py")
	require.Contains(t, hookCommand(pre), "pre-tool")

	subEntries, _ := hooks["SubagentStop"].([]any)
	var sawGuardSub, sawCollectorSub bool
	for _, e := range subEntries {
		m := e.(map[string]any)
		cmd := hookCommand(m)
		if strings.Contains(cmd, "zprof-guard.py") {
			sawGuardSub = true
			require.NotContains(t, m, "matcher", "guard SubagentStop hook has no matcher")
		}
		if strings.Contains(cmd, "zprof-collect.py") {
			sawCollectorSub = true
		}
	}
	require.True(t, sawGuardSub, "guard SubagentStop hook missing")
	require.True(t, sawCollectorSub, "collector SubagentStop hook missing")

	perms, _ := settingsData["permissions"].(map[string]any)
	require.NotNil(t, perms, "permissions.deny must have been populated by guard deploy")
	deny := toStringSlice(perms["deny"])
	require.NotEmpty(t, deny)
	require.Contains(t, deny, "Bash(git push --force*)")

	// --- 2. .claude/guard.json is well-formed, no leftover $refs ---------
	guardJSONPath := filepath.Join(proj, ".claude", "guard.json")
	guardJSON, err := os.ReadFile(guardJSONPath)
	require.NoError(t, err)
	require.NotContains(t, string(guardJSON), "$readonly_roles")
	require.NotContains(t, string(guardJSON), "$merge_roles")
	require.NotContains(t, string(guardJSON), "$mutating_bash_patterns")
	var guardDocJSON map[string]any
	require.NoError(t, json.Unmarshal(guardJSON, &guardDocJSON))
	require.Equal(t, float64(1), guardDocJSON["version"])

	scriptPath := filepath.Join(proj, ".claude", "zprof-guard.py")
	require.FileExists(t, scriptPath)

	// --- 3. subprocess: force-push is denied -------------------------------
	forcePushPayload := map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "git push --force origin main"},
		"cwd":        proj,
	}
	out := runGuardScript(t, python3, scriptPath, proj, forcePushPayload)
	require.NotEmpty(t, out, "force-push must produce a deny payload on stdout")

	var denyResp map[string]any
	require.NoError(t, json.Unmarshal(out, &denyResp), "stdout must be valid JSON: %s", out)
	hso, ok := denyResp["hookSpecificOutput"].(map[string]any)
	require.True(t, ok, "missing hookSpecificOutput: %s", out)
	require.Equal(t, "deny", hso["permissionDecision"])
	reason, _ := hso["permissionDecisionReason"].(string)
	require.Contains(t, reason, "force_push")
	require.Contains(t, reason, "Не обходи")

	// --- 4. subprocess: an ordinary command is allowed (silent) ----------
	statusPayload := map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "git status"},
		"cwd":        proj,
	}
	allowOut := runGuardScript(t, python3, scriptPath, proj, statusPayload)
	require.Empty(t, strings.TrimSpace(string(allowOut)), "an allowed call must produce no stdout")
}

// runGuardScript invokes zprof-guard.py exactly as the PreToolUse hook
// command does: `<script> pre-tool` with the call payload as JSON on stdin
// and CLAUDE_PROJECT_DIR set to root, so project_root() resolves to the
// deployed project rather than falling back to the test binary's cwd.
func runGuardScript(t *testing.T, python3, scriptPath, root string, payload map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	cmd := exec.Command(python3, scriptPath, "pre-tool")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CLAUDE_PROJECT_DIR="+root)
	cmd.Stdin = bytes.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	require.NoError(t, err, "zprof-guard.py always exits 0 (fail-open); stderr: %s", stderr.String())
	return stdout.Bytes()
}
