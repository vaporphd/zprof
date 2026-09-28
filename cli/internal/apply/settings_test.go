package apply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureHooksCreatesFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, EnsureHooks(dir))

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	require.NoError(t, err)

	var settings map[string]any
	require.NoError(t, json.Unmarshal(data, &settings))

	hooks, ok := settings["hooks"].(map[string]any)
	require.True(t, ok, "hooks key missing")
	require.Contains(t, hooks, "SubagentStop")
	require.Contains(t, hooks, "Stop")
	require.Contains(t, hooks, "SessionStart")
}

func TestEnsureHooksPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))

	existing := map[string]any{
		"hooks": map[string]any{
			"UserPromptSubmit": []any{map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": "echo hi"}},
			}},
		},
		"permissions": map[string]any{"allow": []string{"Read"}},
	}
	data, err := json.MarshalIndent(existing, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), data, 0o644))

	require.NoError(t, EnsureHooks(dir))

	data, err = os.ReadFile(filepath.Join(claudeDir, "settings.local.json"))
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))

	hooks := result["hooks"].(map[string]any)
	require.Contains(t, hooks, "UserPromptSubmit", "existing hook clobbered")
	require.Contains(t, hooks, "SubagentStop", "new hook not added")
	require.Contains(t, result, "permissions", "non-hook key clobbered")

	// The pre-existing UserPromptSubmit hook body must survive untouched.
	upsEntries := hooks["UserPromptSubmit"].([]any)
	require.Len(t, upsEntries, 1)
	upsBody, err := json.Marshal(upsEntries[0])
	require.NoError(t, err)
	require.Contains(t, string(upsBody), "echo hi")
}

func TestEnsureHooksIdempotent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, EnsureHooks(dir))
	require.NoError(t, EnsureHooks(dir)) // second call must not duplicate

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	require.NoError(t, err)

	var settings map[string]any
	require.NoError(t, json.Unmarshal(data, &settings))
	hooks := settings["hooks"].(map[string]any)

	// Exactly one hook entry per event, not two after the repeat call.
	for _, event := range []string{"SubagentStop", "Stop", "SessionStart"} {
		entries, ok := hooks[event].([]any)
		require.True(t, ok, "%s missing", event)
		require.Len(t, entries, 1, "%s should have exactly one hook entry, not a duplicate", event)
	}

	// The guard command references zprof-collect.py twice by design (the
	// `test -x` existence check, then the actual invocation), so 3 hooks
	// give 6 substring occurrences — not 3. What idempotency guarantees is
	// that this count does not grow on a repeat call (it would be 12 if
	// EnsureHooks duplicated entries).
	count := strings.Count(string(data), "zprof-collect.py")
	require.Equal(t, 6, count, "should have exactly 6 references (2 per hook x 3 hooks), not duplicated by the repeat call")
}

func TestEnsureHooksGuardCommand(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, EnsureHooks(dir))

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	require.NoError(t, err)

	require.Contains(t, string(data), `test -x`, "guard missing")
	require.Contains(t, string(data), `|| true`, "fallback missing")
	require.Contains(t, string(data), "subagent-stop")
	require.Contains(t, string(data), " stop ")
	require.Contains(t, string(data), "session-start")
}

func stopCommands(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	require.NoError(t, err)
	var settings map[string]any
	require.NoError(t, json.Unmarshal(data, &settings))
	entries := settings["hooks"].(map[string]any)["Stop"].([]any)
	var cmds []string
	for _, e := range entries {
		for _, h := range e.(map[string]any)["hooks"].([]any) {
			cmds = append(cmds, h.(map[string]any)["command"].(string))
		}
	}
	return cmds
}

func TestEnsureHooksStopChainsScore(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, EnsureHooks(dir))
	cmds := stopCommands(t, dir)
	require.Len(t, cmds, 1, "one chained command, not two parallel hooks")
	c := cmds[0]
	require.Contains(t, c, `zprof-collect.py" stop`)
	require.Contains(t, c, "zprof score --latest --quiet --no-collect")
	require.Less(t, strings.Index(c, "zprof-collect.py"), strings.Index(c, "zprof score"), "collector runs first")
	require.Contains(t, c, `command -v zprof >/dev/null 2>&1 &&`, "score is skipped when zprof is not installed")
	require.True(t, strings.HasSuffix(c, "|| true"))

	// other events do not get the score chain
	data, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	require.Equal(t, 1, strings.Count(string(data), "zprof score"))
}

func TestEnsureHooksUpgradesOldStopCommand(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))
	old := map[string]any{"hooks": map[string]any{
		"Stop": []any{map[string]any{"hooks": []any{map[string]any{
			"type":    "command",
			"command": `test -x "$CLAUDE_PROJECT_DIR/.claude/zprof-collect.py" && "$CLAUDE_PROJECT_DIR/.claude/zprof-collect.py" stop || true`,
		}}}},
	}}
	data, _ := json.MarshalIndent(old, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), data, 0o644))

	require.NoError(t, EnsureHooks(dir))
	cmds := stopCommands(t, dir)
	require.Len(t, cmds, 1, "old entry replaced, not duplicated")
	require.Contains(t, cmds[0], "zprof score --latest --quiet --no-collect")

	require.NoError(t, EnsureHooks(dir)) // and it stays stable
	require.Len(t, stopCommands(t, dir), 1)
}

// --- guard hooks (ADR 0009 I8) --------------------------------------------

func readHooks(t *testing.T, dir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	require.NoError(t, err)
	var settings map[string]any
	require.NoError(t, json.Unmarshal(data, &settings))
	hooks, _ := settings["hooks"].(map[string]any)
	require.NotNil(t, hooks)
	return hooks
}

func TestEnsureGuardSettings_UpsertsBothHooksWithMatcher(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, ensureGuardSettings(dir, true, nil))

	hooks := readHooks(t, dir)
	require.Contains(t, hooks, "PreToolUse")
	require.Contains(t, hooks, "SubagentStop")

	preEntries := hooks["PreToolUse"].([]any)
	require.Len(t, preEntries, 1)
	pre := preEntries[0].(map[string]any)
	require.Equal(t, "Bash|Edit|Write|MultiEdit|NotebookEdit", pre["matcher"])
	require.Contains(t, hookCommand(pre), "zprof-guard.py")
	require.Contains(t, hookCommand(pre), "pre-tool")

	subEntries := hooks["SubagentStop"].([]any)
	require.Len(t, subEntries, 1)
	sub := subEntries[0].(map[string]any)
	require.NotContains(t, sub, "matcher", "SubagentStop guard hook has no matcher")
	require.Contains(t, hookCommand(sub), "subagent-stop")
}

func TestEnsureGuardSettings_SubagentStopCoexistsWithCollector(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, EnsureHooks(dir))                    // writes the collector SubagentStop entry
	require.NoError(t, ensureGuardSettings(dir, true, nil)) // adds the guard one alongside it

	hooks := readHooks(t, dir)
	entries := hooks["SubagentStop"].([]any)
	require.Len(t, entries, 2, "collector and guard SubagentStop entries coexist, not merged")

	var sawCollector, sawGuard bool
	for _, e := range entries {
		cmd := hookCommand(e)
		if strings.Contains(cmd, "zprof-collect.py") {
			sawCollector = true
		}
		if strings.Contains(cmd, "zprof-guard.py") {
			sawGuard = true
		}
	}
	require.True(t, sawCollector)
	require.True(t, sawGuard)

	// Re-running both is idempotent: still exactly 2 entries, not 4.
	require.NoError(t, EnsureHooks(dir))
	require.NoError(t, ensureGuardSettings(dir, true, nil))
	hooks = readHooks(t, dir)
	require.Len(t, hooks["SubagentStop"].([]any), 2)
}

func TestEnsureGuardSettings_UpgradesStaleMatcherInPlace(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, ensureGuardSettings(dir, true, nil))

	// Simulate an older matcher shape on disk.
	settingsFile := filepath.Join(dir, ".claude", "settings.local.json")
	data, err := os.ReadFile(settingsFile)
	require.NoError(t, err)
	var settings map[string]any
	require.NoError(t, json.Unmarshal(data, &settings))
	hooks := settings["hooks"].(map[string]any)
	pre := hooks["PreToolUse"].([]any)
	pre[0].(map[string]any)["matcher"] = "Bash"
	out, err := json.MarshalIndent(settings, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(settingsFile, out, 0o644))

	require.NoError(t, ensureGuardSettings(dir, true, nil))
	hooks = readHooks(t, dir)
	entries := hooks["PreToolUse"].([]any)
	require.Len(t, entries, 1, "upgraded in place, not duplicated")
	require.Equal(t, "Bash|Edit|Write|MultiEdit|NotebookEdit", entries[0].(map[string]any)["matcher"])
}

func TestEnsureGuardSettings_DisabledRemovesGuardHooksOnly(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, EnsureHooks(dir))
	require.NoError(t, ensureGuardSettings(dir, true, nil))

	require.NoError(t, ensureGuardSettings(dir, false, nil))
	hooks := readHooks(t, dir)
	require.NotContains(t, hooks, "PreToolUse", "empty event removed entirely")
	subEntries := hooks["SubagentStop"].([]any)
	require.Len(t, subEntries, 1, "only the guard entry removed, collector's stays")
	require.Contains(t, hookCommand(subEntries[0]), "zprof-collect.py")
}

func TestEnsureGuardSettings_PermissionsDenyUpsertNoDupsForeignPreserved(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))
	existing := map[string]any{
		"permissions": map[string]any{
			"allow": []any{"Read"},
			"deny":  []any{"Bash(rm -rf /)"},
		},
	}
	data, err := json.MarshalIndent(existing, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), data, 0o644))

	deny := []string{"Bash(git push --force*)", "Bash(git push --force*)"} // dup on purpose
	require.NoError(t, ensureGuardSettings(dir, true, deny))
	require.NoError(t, ensureGuardSettings(dir, true, deny)) // idempotent

	out, err := os.ReadFile(filepath.Join(claudeDir, "settings.local.json"))
	require.NoError(t, err)
	var settings map[string]any
	require.NoError(t, json.Unmarshal(out, &settings))
	perms := settings["permissions"].(map[string]any)
	require.ElementsMatch(t, []string{"Read"}, toStringSlice(perms["allow"]))
	require.ElementsMatch(t, []string{"Bash(rm -rf /)", "Bash(git push --force*)"}, toStringSlice(perms["deny"]))
}

func TestEnsureGuardSettings_PermissionsDenyRemovalKeepsForeign(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, ensureGuardSettings(dir, true, []string{"Bash(git push --force*)", "Bash(gh pr merge --admin*)"}))

	// A user (or another tool) adds a foreign deny entry by hand.
	settingsFile := filepath.Join(dir, ".claude", "settings.local.json")
	data, err := os.ReadFile(settingsFile)
	require.NoError(t, err)
	var settings map[string]any
	require.NoError(t, json.Unmarshal(data, &settings))
	perms := settings["permissions"].(map[string]any)
	perms["deny"] = append(perms["deny"].([]any), "Bash(curl evil.sh)")
	out, err := json.MarshalIndent(settings, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(settingsFile, out, 0o644))

	require.NoError(t, ensureGuardSettings(dir, false, []string{"Bash(git push --force*)", "Bash(gh pr merge --admin*)"}))

	data, err = os.ReadFile(settingsFile)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &settings))
	permsRaw, ok := settings["permissions"]
	require.True(t, ok, "permissions key itself is never deleted")
	perms = permsRaw.(map[string]any)
	require.Equal(t, []string{"Bash(curl evil.sh)"}, toStringSlice(perms["deny"]))
}

func TestEnsureGuardSettings_EnabledEmptyDenyCreatesNoKeys(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, ensureGuardSettings(dir, true, nil))
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	require.NoError(t, err)
	var settings map[string]any
	require.NoError(t, json.Unmarshal(data, &settings))
	require.NotContains(t, settings, "permissions")
}

func toStringSlice(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
