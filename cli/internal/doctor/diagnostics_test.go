// cli/internal/doctor/diagnostics_test.go
package doctor

import (
	"fmt"
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
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".claude", "agents", "task-runner.md"),
		[]byte("---\nname: task-runner\nmodel: opus\n---\nBody.\n"), 0o644))
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

// TestDiagnoseAgentBrokenYAMLFrontmatter guards the H0 regression: an
// agent whose description contains `: ` (colon+space) inside a plain
// scalar breaks YAML parsing. Claude Code silently drops the agent; doctor
// must catch it before ship.
func TestDiagnoseAgentBrokenYAMLFrontmatter(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"), []byte("overlays: []\n"), 0o644))
	repo := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	// Description contains `EN: "..."` — the exact H0 pattern.
	broken := "---\nname: implementer\n" +
		`description: Writes code. Triggers — EN: "implement", "add"; RU: "реализуй".` +
		"\nmodel: opus\n---\nbody\n"
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "implementer.md"), []byte(broken), 0o644))
	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)
	require.True(t, findIssue(issues, LevelError, "YAML frontmatter parse error"))
}

// TestDiagnoseAgentNoFrontmatterErrors — an agent .md missing the leading
// `---` fence isn't loadable by Claude Code either.
func TestDiagnoseAgentNoFrontmatterErrors(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"), []byte("overlays: []\n"), 0o644))
	repo := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "planner.md"),
		[]byte("# Planner\n\nNo YAML anywhere.\n"), 0o644))
	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)
	require.True(t, findIssue(issues, LevelError, "no YAML frontmatter"))
}

// TestDiagnoseAgentFrontmatterMissingName — `name` is the only frontmatter
// field the doctor treats as a hard contract because Claude Code keys the
// tool registry on it.
func TestDiagnoseAgentFrontmatterMissingName(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"), []byte("overlays: []\n"), 0o644))
	repo := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "x.md"),
		[]byte("---\nmodel: opus\ndescription: hi\n---\nbody\n"), 0o644))
	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)
	require.True(t, findIssue(issues, LevelError, "missing `name` field"))
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

func TestCheckTaskRunnerMissing(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude", "agents"), 0o755))

	issues := checkTaskRunner(dir)
	require.Len(t, issues, 1)
	require.Equal(t, LevelError, issues[0].Level)
	require.Contains(t, issues[0].Message, "task-runner")
}

func TestCheckTaskRunnerFlagsSurvivingOrchestrator(t *testing.T) {
	dir := t.TempDir()
	agents := filepath.Join(dir, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agents, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agents, "task-runner.md"), []byte("---\nname: task-runner\n---\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agents, "dev-orchestrator.md"), []byte("---\nname: dev-orchestrator\n---\n"), 0o644))

	issues := checkTaskRunner(dir)
	require.Len(t, issues, 1)
	require.Equal(t, LevelError, issues[0].Level)
	require.Contains(t, issues[0].Message, "dev-orchestrator")
}

func TestCheckStopListsEmptyOverlay(t *testing.T) {
	repo := t.TempDir()
	ovDir := filepath.Join(repo, "overlays", "demo")
	require.NoError(t, os.MkdirAll(ovDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ovDir, "manifest.yaml"),
		[]byte("name: demo\nloop_template: dev-pipeline\n"), 0o644))

	issues := checkStopLists([]string{"demo"}, repo)
	require.Len(t, issues, 1)
	require.Equal(t, LevelError, issues[0].Level)
	require.Contains(t, issues[0].Message, "stop_list")
}

func TestCheckRunLogsWarnsAboveFifty(t *testing.T) {
	dir := t.TempDir()
	runs := filepath.Join(dir, ".zprof", "runs")
	require.NoError(t, os.MkdirAll(runs, 0o755))
	for i := 0; i < 51; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(runs, fmt.Sprintf("r%02d.md", i)), []byte("x"), 0o644))
	}

	issues := checkRunLogs(dir)
	require.Len(t, issues, 1)
	require.Equal(t, LevelWarn, issues[0].Level)
}
