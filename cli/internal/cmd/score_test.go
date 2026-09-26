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
