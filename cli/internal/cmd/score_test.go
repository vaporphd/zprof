package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func scoreFixtureDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "score", "testdata", "run1")
}

func setupScoreProject(t *testing.T) (proj, agentlog string) {
	t.Helper()
	proj = t.TempDir()
	agentlog = filepath.Join(proj, ".agentlog")
	require.NoError(t, os.MkdirAll(agentlog, 0o755))
	for _, name := range []string{"dispatches.jsonl", "tool-events.jsonl", "schema.json"} {
		data, err := os.ReadFile(filepath.Join(scoreFixtureDir(), name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(agentlog, name), data, 0o644))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(proj, ".zprof", "runs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof", "runs", "2026-09-26-fixture.md"),
		[]byte("# fixture\n\n## Итог\nverdict: done\n"), 0o644))
	return proj, agentlog
}

func runScore(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := NewScoreCmd("test")
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(args)
	err := c.Execute()
	return out.String(), err
}

func TestScoreCmd_LatestPrintsCardAndPersists(t *testing.T) {
	proj, agentlog := setupScoreProject(t)
	out, err := runScore(t, "--project", proj, "--no-collect")
	require.NoError(t, err)
	require.Contains(t, out, "Score 45/100 · Lucky · done · 2026-09-26-fixture")

	scores, err := os.ReadFile(filepath.Join(agentlog, "scores.jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(scores), `"run_id":"claude-code:s1:t0"`)

	runLog, err := os.ReadFile(filepath.Join(proj, ".zprof", "runs", "2026-09-26-fixture.md"))
	require.NoError(t, err)
	require.Contains(t, string(runLog), "<!-- zprof:score:begin -->")
	require.Contains(t, string(runLog), "Score 45/100")
}

func TestScoreCmd_JSON(t *testing.T) {
	proj, _ := setupScoreProject(t)
	out, err := runScore(t, "--project", proj, "--no-collect", "--json")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(strings.TrimSpace(out), "{"), out)
	require.Contains(t, out, `"tier":"Lucky"`)
}

func TestScoreCmd_RunSelectorAndQuiet(t *testing.T) {
	proj, agentlog := setupScoreProject(t)
	out, err := runScore(t, "--project", proj, "--no-collect", "--quiet", "--run", "2026-09-26-fixture")
	require.NoError(t, err)
	require.Equal(t, "", out)
	_, err = os.Stat(filepath.Join(agentlog, "scores.jsonl"))
	require.NoError(t, err)
}

func TestScoreCmd_AllMissingIsIdempotent(t *testing.T) {
	proj, agentlog := setupScoreProject(t)
	_, err := runScore(t, "--project", proj, "--no-collect", "--all-missing", "--quiet")
	require.NoError(t, err)
	out, err := runScore(t, "--project", proj, "--no-collect", "--all-missing")
	require.NoError(t, err)
	require.Contains(t, out, "nothing to score")
	data, _ := os.ReadFile(filepath.Join(agentlog, "scores.jsonl"))
	require.Len(t, strings.Split(strings.TrimRight(string(data), "\n"), "\n"), 1)
}

func TestScoreCmd_NoRunIsNotAnError(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(proj, ".agentlog"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".agentlog", "dispatches.jsonl"), []byte(""), 0o644))
	out, err := runScore(t, "--project", proj, "--no-collect")
	require.NoError(t, err)
	require.Contains(t, out, "unscored: no task-runner dispatch")
}

func TestScoreCmd_DisabledInManifest(t *testing.T) {
	proj, _ := setupScoreProject(t)
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"), []byte("overlays: []\nscore:\n  enabled: false\n"), 0o644))
	out, err := runScore(t, "--project", proj, "--no-collect")
	require.NoError(t, err)
	require.Contains(t, out, "score disabled")
}

func TestScoreCmd_RunAndAllMissingConflict(t *testing.T) {
	proj, _ := setupScoreProject(t)
	_, err := runScore(t, "--project", proj, "--no-collect", "--run", "2026-09-26-fixture", "--all-missing")
	require.Error(t, err)
	require.Contains(t, err.Error(), "mutually exclusive")
}

func TestScoreCmd_LatestFalseNeedsSelector(t *testing.T) {
	proj, _ := setupScoreProject(t)
	_, err := runScore(t, "--project", proj, "--no-collect", "--latest=false")
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires --run or --all-missing")
}

func TestScoreCmd_AllMissingSkipsLegacy(t *testing.T) {
	proj, agentlog := setupScoreProject(t)
	legacy := `{"schema_version":1,"harness":"claude-code","ts_utc":"2026-09-25T09:00:00Z","session_id":"s0","dispatch_id":"claude-code:s0:L0","seq":0,"role":"task-runner","status":"completed","dispatch_complete":true,"transcript_captured":true}
{"schema_version":1,"harness":"claude-code","ts_utc":"","session_id":"s0","dispatch_id":"claude-code:s0:L1","seq":0,"parent_dispatch_id":"claude-code:s0:L0","role":"implementer","status":"completed","dispatch_complete":true,"transcript_captured":true}
`
	f, err := os.OpenFile(filepath.Join(agentlog, "dispatches.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(legacy)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	out, err := runScore(t, "--project", proj, "--no-collect", "--all-missing")
	require.NoError(t, err)
	require.Contains(t, out, "skipped 1 legacy run(s) (collected before schema v2)")
	data, _ := os.ReadFile(filepath.Join(agentlog, "scores.jsonl"))
	require.Len(t, strings.Split(strings.TrimRight(string(data), "\n"), "\n"), 1)
	require.NotContains(t, string(data), "claude-code:s0:L0")

	// --run still scores it, with the legacy line visible.
	out, err = runScore(t, "--project", proj, "--no-collect", "--run", "claude-code:s0:L0")
	require.NoError(t, err)
	require.Contains(t, out, "legacy data:")
}

func TestScoreCmd_RunLogOutsideRunsDirIsIgnored(t *testing.T) {
	for _, runLog := range []string{"notes/x.md", ".zprof/runs/../../notes/x.md"} {
		proj, agentlog := setupScoreProject(t)
		dpath := filepath.Join(agentlog, "dispatches.jsonl")
		data, err := os.ReadFile(dpath)
		require.NoError(t, err)
		data = []byte(strings.ReplaceAll(string(data), `".zprof/runs/2026-09-26-fixture.md"`, `"`+runLog+`"`))
		require.NoError(t, os.WriteFile(dpath, data, 0o644))
		target := filepath.Join(proj, "notes", "x.md")
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
		require.NoError(t, os.WriteFile(target, []byte("# notes\n"), 0o644))

		out, err := runScore(t, "--project", proj, "--no-collect")
		require.NoError(t, err, runLog)
		require.Contains(t, out, "Score 45/100")
		got, err := os.ReadFile(target)
		require.NoError(t, err)
		require.Equal(t, "# notes\n", string(got), "run_log %q outside .zprof/runs must not be written", runLog)
		_, err = os.Stat(filepath.Join(agentlog, "scores.jsonl"))
		require.NoError(t, err, "the score row is still persisted")
	}
}
