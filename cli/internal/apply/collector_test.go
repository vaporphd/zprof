package apply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vaporphd/zprof/internal/overlay"
	"gopkg.in/yaml.v3"
)

func fullTelemetryBase() *overlay.Base {
	return &overlay.Base{
		CollectorScript: []byte("#!/usr/bin/env python3\nprint('collect')\n"),
		TelemetrySchema: []byte("redaction_patterns:\n  - \"sk-[a-zA-Z0-9]+\"\n"),
	}
}

// minimalVerdictsYAML is a tiny but internally consistent verdicts.yaml,
// enough to exercise renderSchema's merge without pulling in the full
// profiles/base/verdicts.yaml.
const minimalVerdictsYAML = `
version: 1
base_enum: [done, blocked, failed]
actions: [next, loop, insert, triage, escalate, abort]
universal:
  blocked: {base: blocked, action: triage}
roles:
  implementer:
    done: {base: done, action: next}
  reviewer:
    approve: {base: done,   action: next}
    block:   {base: failed, action: "loop:implementer"}
`

// --- renderSchema / verdicts merge (ADR 0003 §D4) -------------------------

func TestRenderSchema_MergesVerdictsUnderTopLevelKey(t *testing.T) {
	out, err := renderSchema([]byte("redaction_patterns: []\n"), []byte(minimalVerdictsYAML))
	require.NoError(t, err)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(out, &schema))
	verdictsField, ok := schema["verdicts"].(map[string]any)
	require.True(t, ok, "schema.json must have a `verdicts` object")
	require.Equal(t, float64(1), verdictsField["version"])
	roles, ok := verdictsField["roles"].(map[string]any)
	require.True(t, ok)
	reviewer, ok := roles["reviewer"].(map[string]any)
	require.True(t, ok)
	approve, ok := reviewer["approve"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "done", approve["base"])
	require.Equal(t, "next", approve["action"])
	// universal token merged into the role.
	blocked, ok := reviewer["blocked"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "blocked", blocked["base"])
}

func TestRenderSchema_WithoutVerdictsYAMLMatchesPriorOutputByteForByte(t *testing.T) {
	telemetry := []byte("redaction_patterns:\n  - \"sk-[a-zA-Z0-9]+\"\n")
	withoutVerdicts, err := renderSchema(telemetry, nil)
	require.NoError(t, err)

	var v interface{}
	require.NoError(t, yaml.Unmarshal(telemetry, &v))
	expected, err := json.MarshalIndent(v, "", "  ")
	require.NoError(t, err)
	expected = append(expected, '\n')

	require.Equal(t, expected, withoutVerdicts)
}

func TestRenderSchema_TelemetryDefiningVerdictsKeyIsAnError(t *testing.T) {
	_, err := renderSchema([]byte("verdicts: {}\n"), []byte(minimalVerdictsYAML))
	require.Error(t, err)
	require.Contains(t, err.Error(), `must not define top-level key "verdicts"`)
}

func TestRenderSchema_InvalidVerdictsYAMLIsAnError(t *testing.T) {
	_, err := renderSchema([]byte("redaction_patterns: []\n"), []byte("version: 1\nroles:\n  x:\n    done: {base: bogus-base, action: next}\n"))
	require.Error(t, err)
}

func TestDeployTelemetry_SchemaJSONHasVerdictsKeyWhenBaseProvidesRegistry(t *testing.T) {
	base := fullTelemetryBase()
	base.Verdicts = []byte(minimalVerdictsYAML)
	projectDir := t.TempDir()

	_, err := DeployTelemetry(projectDir, base)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(projectDir, ".agentlog", "schema.json"))
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(data, &schema))
	require.Contains(t, schema, "verdicts")
}

func TestDeployTelemetry_NoVerdictsYAMLMeansNoVerdictsKey(t *testing.T) {
	projectDir := t.TempDir()
	_, err := DeployTelemetry(projectDir, fullTelemetryBase())
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(projectDir, ".agentlog", "schema.json"))
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(data, &schema))
	require.NotContains(t, schema, "verdicts")
}

// TestDeployTelemetry is table-driven over the scenarios ADR 0001 (D5) calls
// out: writing exactly the telemetry files, tolerating a base with only
// partial telemetry content, and upgrading (not duplicating) a stale Stop hook.
func TestDeployTelemetry(t *testing.T) {
	cases := []struct {
		name    string
		base    *overlay.Base
		setup   func(t *testing.T, projectDir string)
		wantErr bool
		check   func(t *testing.T, projectDir string, written []string)
	}{
		{
			name: "writes exactly the three telemetry files",
			base: fullTelemetryBase(),
			check: func(t *testing.T, projectDir string, written []string) {
				require.ElementsMatch(t, []string{
					filepath.Join(projectDir, ".claude", "zprof-collect.py"),
					filepath.Join(projectDir, ".agentlog", "schema.json"),
					filepath.Join(projectDir, ".claude", "settings.local.json"),
				}, written)

				script, err := os.ReadFile(filepath.Join(projectDir, ".claude", "zprof-collect.py"))
				require.NoError(t, err)
				require.Contains(t, string(script), "print('collect')")

				_, err = os.Stat(filepath.Join(projectDir, ".agentlog", "schema.json"))
				require.NoError(t, err)
			},
		},
		{
			name: "does not create .zprof.yaml or agents",
			base: fullTelemetryBase(),
			check: func(t *testing.T, projectDir string, written []string) {
				_, err := os.Stat(filepath.Join(projectDir, ".zprof.yaml"))
				require.True(t, os.IsNotExist(err), ".zprof.yaml must not be written by --telemetry-only")
				_, err = os.Stat(filepath.Join(projectDir, ".claude", "agents"))
				require.True(t, os.IsNotExist(err), "agents dir must not be written by --telemetry-only")
				_, err = os.Stat(filepath.Join(projectDir, "CLAUDE.md"))
				require.True(t, os.IsNotExist(err), "CLAUDE.md must not be touched by --telemetry-only")
			},
		},
		{
			name: "base without telemetry content writes only the hook file",
			base: &overlay.Base{},
			check: func(t *testing.T, projectDir string, written []string) {
				require.ElementsMatch(t, []string{
					filepath.Join(projectDir, ".claude", "settings.local.json"),
				}, written)
				_, err := os.Stat(filepath.Join(projectDir, ".claude", "zprof-collect.py"))
				require.True(t, os.IsNotExist(err))
			},
		},
		{
			name:    "nil base is an error",
			base:    nil,
			wantErr: true,
		},
		{
			name: "upgrades a stale Stop hook in place, not a duplicate",
			base: fullTelemetryBase(),
			setup: func(t *testing.T, projectDir string) {
				claudeDir := filepath.Join(projectDir, ".claude")
				require.NoError(t, os.MkdirAll(claudeDir, 0o755))
				old := map[string]any{"hooks": map[string]any{
					"Stop": []any{map[string]any{"hooks": []any{map[string]any{
						"type":    "command",
						"command": `test -x "$CLAUDE_PROJECT_DIR/.claude/zprof-collect.py" && "$CLAUDE_PROJECT_DIR/.claude/zprof-collect.py" stop || true`,
					}}}},
				}}
				data, err := json.MarshalIndent(old, "", "  ")
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), data, 0o644))
			},
			check: func(t *testing.T, projectDir string, written []string) {
				cmds := stopCommands(t, projectDir)
				require.Len(t, cmds, 1, "stale entry replaced, not duplicated")
				require.Contains(t, cmds[0], "zprof score --latest --quiet --no-collect")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			if tc.setup != nil {
				tc.setup(t, projectDir)
			}
			written, err := DeployTelemetry(projectDir, tc.base)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.check != nil {
				tc.check(t, projectDir, written)
			}
		})
	}
}

// TestDeployTelemetryIdempotent mirrors TestEnsureHooksIdempotent: calling
// DeployTelemetry twice must not duplicate hook entries.
func TestDeployTelemetryIdempotent(t *testing.T) {
	projectDir := t.TempDir()
	base := fullTelemetryBase()

	_, err := DeployTelemetry(projectDir, base)
	require.NoError(t, err)
	_, err = DeployTelemetry(projectDir, base)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(projectDir, ".claude", "settings.local.json"))
	require.NoError(t, err)
	var settings map[string]any
	require.NoError(t, json.Unmarshal(data, &settings))
	hooks := settings["hooks"].(map[string]any)
	for _, event := range []string{"SubagentStop", "Stop", "SessionStart"} {
		entries, ok := hooks[event].([]any)
		require.True(t, ok, "%s missing", event)
		require.Len(t, entries, 1, "%s should have exactly one hook entry", event)
	}
}
