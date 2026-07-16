// cli/internal/doctor/diagnostics_test.go
package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func hasLevel(issues []Issue, level string) bool {
	for _, i := range issues {
		if i.Level == level {
			return true
		}
	}
	return false
}

func findIssue(issues []Issue, level, substr string) bool {
	for _, i := range issues {
		if i.Level == level && strings.Contains(i.Message, substr) {
			return true
		}
	}
	return false
}

func TestDiagnoseTooManyOverlaysErrors(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"),
		[]byte("overlays: [a, b, c, d]\n"), 0o644))
	repo := t.TempDir()
	for _, n := range []string{"a", "b", "c", "d"} {
		require.NoError(t, os.MkdirAll(filepath.Join(repo, "overlays", n), 0o755))
	}
	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)
	require.True(t, hasLevel(issues, LevelError))
}

func TestDiagnoseWarnsOnMultipleOverlays(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"),
		[]byte("overlays: [a, b]\n"), 0o644))
	repo := t.TempDir()
	for _, n := range []string{"a", "b"} {
		require.NoError(t, os.MkdirAll(filepath.Join(repo, "overlays", n), 0o755))
	}
	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)
	require.True(t, hasLevel(issues, LevelWarn))
	require.False(t, hasLevel(issues, LevelError))
}

func TestDiagnoseSingleOverlayNoCountIssue(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"),
		[]byte("overlays: [a]\n"), 0o644))
	repo := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "overlays", "a"), 0o755))
	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)
	require.Empty(t, issues)
}

func TestDiagnoseUnknownOverlayErrors(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"),
		[]byte("overlays: [nonexistent]\n"), 0o644))
	repo := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "overlays"), 0o755))
	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)
	require.True(t, findIssue(issues, LevelError, "nonexistent"))
}

func TestDiagnoseInvalidManifestReportsIssueNotError(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"),
		[]byte("overlays: [this is not valid yaml: :\n"), 0o644))
	repo := t.TempDir()
	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)
	require.True(t, findIssue(issues, LevelError, "failed to parse .zprof.yaml"))
}

func TestDiagnoseAgentMissingModelField(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"), []byte("overlays: []\n"), 0o644))
	repo := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "planner.md"),
		[]byte("---\nname: planner\n---\nNo model here.\n"), 0o644))
	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)
	require.True(t, findIssue(issues, LevelError, "no model:"))
}

func TestDiagnoseAgentUnresolvableModel(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"), []byte("overlays: []\n"), 0o644))
	repo := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "planner.md"),
		[]byte("---\nname: planner\nmodel: gpt-5\n---\nBody.\n"), 0o644))
	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)
	require.True(t, findIssue(issues, LevelError, "gpt-5"))
}

func TestDiagnoseAgentResolvableModelIsClean(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"), []byte("overlays: []\n"), 0o644))
	repo := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents", "gates")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "auditor.md"),
		[]byte("---\nname: auditor\nmodel: opus\n---\nBody.\n"), 0o644))
	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)
	require.Empty(t, issues)
}

func TestDiagnoseUnclosedManagedBlockErrors(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"), []byte("overlays: []\n"), 0o644))
	repo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, "CLAUDE.md"),
		[]byte("intro\n<!-- zprof:begin overlay=base block=intro -->\nunclosed\n"), 0o644))
	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)
	require.True(t, findIssue(issues, LevelError, "managed marker error"))
}

func TestDiagnoseCleanProjectHasNoIssues(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"), []byte("overlays: []\n"), 0o644))
	repo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, "CLAUDE.md"),
		[]byte("intro\n<!-- zprof:begin overlay=base block=intro -->\nbody\n<!-- zprof:end -->\n"), 0o644))
	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)
	require.Empty(t, issues)
}
