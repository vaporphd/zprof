// cli/internal/doctor/diagnostics_test.go
package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vaporphd/zprof/internal/manifest"
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
		ovDir := filepath.Join(repo, "overlays", n)
		require.NoError(t, os.MkdirAll(ovDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(ovDir, "manifest.yaml"),
			[]byte("name: "+n+"\nstop_list: [\"x\"]\n"), 0o644))
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
	ovDir := filepath.Join(repo, "overlays", "a")
	require.NoError(t, os.MkdirAll(ovDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ovDir, "manifest.yaml"),
		[]byte("name: a\nstop_list: [\"x\"]\n"), 0o644))
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
	// task-runner is a role, so it must also declare a return_format.
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".claude", "agents", "task-runner.md"),
		[]byte("---\nname: task-runner\nmodel: opus\nreturn_format: |\n  verdict: done\n---\nBody.\n"), 0o644))
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

// A missing overlay directory is checkOverlaysExist's turf — checkStopLists
// stays silent about it.
func TestCheckStopListsSilentWhenOverlayDirMissing(t *testing.T) {
	repo := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "overlays"), 0o755))

	require.Empty(t, checkStopLists([]string{"nonexistent"}, repo))
}

// The overlay's directory exists but manifest.yaml doesn't parse — nothing
// else in doctor catches this, and `apply`/`sync` fail on it outright, so
// checkStopLists must report it.
func TestCheckStopListsBrokenManifestErrors(t *testing.T) {
	repo := t.TempDir()
	ovDir := filepath.Join(repo, "overlays", "demo")
	require.NoError(t, os.MkdirAll(ovDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ovDir, "manifest.yaml"),
		[]byte("name: [this is not valid yaml\n"), 0o644))

	issues := checkStopLists([]string{"demo"}, repo)
	require.Len(t, issues, 1)
	require.Equal(t, LevelError, issues[0].Level)
	require.Contains(t, issues[0].Message, "manifest failed to load")
	require.Contains(t, issues[0].Path, "manifest.yaml")
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

// --- return_format contract (spec §10) ---------------------------------

// A role's caller parses its answer as a schema, so a role without
// return_format silently derails the loop.
func TestCheckAgentFrontmatterRoleMissingReturnFormat(t *testing.T) {
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "implementer.md"),
		[]byte("---\nname: implementer\nmodel: opus\n---\nbody\n"), 0o644))

	issues := checkAgentFrontmatter(proj)
	require.True(t, findIssue(issues, LevelError, "return_format"))
}

// Namespaced roles from a multi-overlay apply must be recognized as roles.
func TestCheckAgentFrontmatterNamespacedRoleMissingReturnFormat(t *testing.T) {
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "refactor-agent-ios.md"),
		[]byte("---\nname: refactor-agent-ios\nmodel: opus\n---\nbody\n"), 0o644))

	issues := checkAgentFrontmatter(proj)
	require.True(t, findIssue(issues, LevelError, `role "refactor-agent"`))
}

// Tool-agents are exempt — and so is a user's own agent sitting in
// .claude/agents/, which doctor has no business grading.
func TestCheckAgentFrontmatterToolAgentNeedsNoReturnFormat(t *testing.T) {
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "xcode-runner.md"),
		[]byte("---\nname: xcode-runner\nmodel: haiku\n---\nbody\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "my-own-helper.md"),
		[]byte("---\nname: my-own-helper\nmodel: haiku\n---\nbody\n"), 0o644))

	require.Empty(t, checkAgentFrontmatter(proj))
}

// A role that declares the field is clean.
func TestCheckAgentFrontmatterRoleWithReturnFormatIsClean(t *testing.T) {
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "reviewer.md"),
		[]byte("---\nname: reviewer\nmodel: opus\nreturn_format: |\n  verdict: approve\n---\nbody\n"), 0o644))

	require.Empty(t, checkAgentFrontmatter(proj))
}

// --- orphan agents (spec §10) ------------------------------------------

// writeRepoFixture builds a minimal but real zprof repo: base with one
// agent, plus the named overlays each with one agent.
func writeRepoFixture(t *testing.T, overlayAgents map[string][]string) string {
	t.Helper()
	repo := t.TempDir()
	baseDir := filepath.Join(repo, "base")
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "agents"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "manifest.yaml"),
		[]byte("name: base\nversion: 0.1.0\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "agents", "task-runner.md"),
		[]byte("---\nname: task-runner\n---\n"), 0o644))

	for name, list := range overlayAgents {
		dir := filepath.Join(repo, "overlays", name)
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.yaml"),
			[]byte("name: "+name+"\nloop_template: dev-pipeline\nstop_list: [\"x\"]\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "detect.yaml"),
			[]byte("name: "+name+"\ndetect:\n  any_file: [\"go.mod\"]\n  confidence: high\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "loop.md"), []byte("loop\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "claude-block.md"), []byte("block\n"), 0o644))
		for _, a := range list {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "agents", a+".md"),
				[]byte("---\nname: "+a+"\n---\n"), 0o644))
		}
	}
	return repo
}

func TestCheckOrphanAgentsWarnsOnUntrackedFile(t *testing.T) {
	repo := writeRepoFixture(t, map[string][]string{"demo": {"implementer"}})
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	for _, n := range []string{"task-runner", "implementer", "implementer-py"} {
		require.NoError(t, os.WriteFile(filepath.Join(agentsDir, n+".md"),
			[]byte("---\nname: "+n+"\n---\n"), 0o644))
	}

	// managed_agents empty — exactly the pre-migration project this check
	// exists for: prune can never fire, so the leftover must be shown.
	pm := &manifest.ProjectManifest{Overlays: []string{"demo"}}
	issues := checkOrphanAgents(proj, repo, pm)

	require.Len(t, issues, 1)
	require.Equal(t, LevelWarn, issues[0].Level)
	require.Contains(t, issues[0].Message, "implementer-py")
}

func TestCheckOrphanAgentsSilentOnDisownedFile(t *testing.T) {
	repo := writeRepoFixture(t, map[string][]string{"demo": {"implementer"}})
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "task-runner.md"),
		[]byte("---\nname: task-runner\n---\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "implementer.md"),
		[]byte("---\nname: implementer\n---\n"), 0o644))
	// The user's own agent, claimed via the frontmatter marker.
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "my-custom.md"),
		[]byte("---\nname: my-custom\nzprof_managed: false\n---\n"), 0o644))

	pm := &manifest.ProjectManifest{Overlays: []string{"demo"}}
	require.Empty(t, checkOrphanAgents(proj, repo, pm),
		"a file claimed with zprof_managed: false is not an orphan")
}

func TestCheckOrphanAgentsStillWarnsWhenMarkerUnparseable(t *testing.T) {
	repo := writeRepoFixture(t, map[string][]string{"demo": {"implementer"}})
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "task-runner.md"),
		[]byte("---\nname: task-runner\n---\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "implementer.md"),
		[]byte("---\nname: implementer\n---\n"), 0o644))
	// Broken YAML: the claim cannot be read, so it does not suppress.
	// Suppressing here would hide the orphan and the parse error together.
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "broken.md"),
		[]byte("---\nname: broken\nzprof_managed: [unclosed\n---\n"), 0o644))

	pm := &manifest.ProjectManifest{Overlays: []string{"demo"}}
	issues := checkOrphanAgents(proj, repo, pm)

	require.Len(t, issues, 1)
	require.Contains(t, issues[0].Message, "broken")
}

func TestCheckOrphanAgentsWarnsWhenDisowningAProvidedAgent(t *testing.T) {
	repo := writeRepoFixture(t, map[string][]string{"demo": {"implementer"}})
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "task-runner.md"),
		[]byte("---\nname: task-runner\n---\n"), 0o644))
	// `implementer` comes from the active overlay: the marker buys nothing,
	// and the user must be told rather than left feeling protected.
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "implementer.md"),
		[]byte("---\nname: implementer\nzprof_managed: false\n---\n"), 0o644))

	pm := &manifest.ProjectManifest{Overlays: []string{"demo"}}
	issues := checkOrphanAgents(proj, repo, pm)

	require.Len(t, issues, 1)
	require.Equal(t, LevelWarn, issues[0].Level)
	require.Contains(t, issues[0].Message, "overwrites")
}

func TestCheckOrphanAgentsSilentWhenTrackedOrInSources(t *testing.T) {
	repo := writeRepoFixture(t, map[string][]string{"demo": {"implementer"}})
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	for _, n := range []string{"task-runner", "implementer", "legacy-role"} {
		require.NoError(t, os.WriteFile(filepath.Join(agentsDir, n+".md"),
			[]byte("---\nname: "+n+"\n---\n"), 0o644))
	}

	// legacy-role is tracked, so prune owns it; the other two are in sources.
	pm := &manifest.ProjectManifest{Overlays: []string{"demo"}, ManagedAgents: []string{"legacy-role"}}
	require.Empty(t, checkOrphanAgents(proj, repo, pm))
}

// Gates live in a subdirectory and are only expected with --with-gates.
func TestCheckOrphanAgentsHandlesGateSubdirectory(t *testing.T) {
	repo := writeRepoFixture(t, map[string][]string{"demo": {"implementer"}})
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "base", "agents", "gates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "base", "agents", "gates", "plan-reviewer.md"),
		[]byte("---\nname: plan-reviewer\n---\n"), 0o644))

	proj := t.TempDir()
	gatesDir := filepath.Join(proj, ".claude", "agents", "gates")
	require.NoError(t, os.MkdirAll(gatesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gatesDir, "plan-reviewer.md"),
		[]byte("---\nname: plan-reviewer\n---\n"), 0o644))

	withGates := &manifest.ProjectManifest{Overlays: []string{"demo"}, WithGates: true}
	require.Empty(t, checkOrphanAgents(proj, repo, withGates),
		"a gate expected by --with-gates is not an orphan")

	withoutGates := &manifest.ProjectManifest{Overlays: []string{"demo"}}
	issues := checkOrphanAgents(proj, repo, withoutGates)
	require.Len(t, issues, 1)
	require.Contains(t, issues[0].Message, "gates/plan-reviewer")
}

// Retired names are reported by checkTaskRunner as errors; don't double-report.
func TestCheckOrphanAgentsSkipsRetiredNames(t *testing.T) {
	repo := writeRepoFixture(t, map[string][]string{"demo": {"implementer"}})
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "dev-orchestrator.md"),
		[]byte("---\nname: dev-orchestrator\n---\n"), 0o644))

	require.Empty(t, checkOrphanAgents(proj, repo, &manifest.ProjectManifest{Overlays: []string{"demo"}}))
}

// An unreadable repo means "cannot tell" — never turn that into a wall of
// false orphans. checkOverlaysExist reports the real problem.
func TestCheckOrphanAgentsSilentWithoutRepo(t *testing.T) {
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "whatever.md"),
		[]byte("---\nname: whatever\n---\n"), 0o644))

	require.Empty(t, checkOrphanAgents(proj, t.TempDir(), &manifest.ProjectManifest{}))
}

// --- .zprof/runs/ gitignored (spec §10) --------------------------------

func TestCheckRunsGitignoredWarnsWhenEntryMissing(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"),
		[]byte("thoughts/\n*.zprof.bak-*\n"), 0o644))

	issues := checkRunsGitignored(dir)
	require.Len(t, issues, 1)
	require.Equal(t, LevelWarn, issues[0].Level)
	require.Contains(t, issues[0].Message, ".zprof/runs/")
}

func TestCheckRunsGitignoredAcceptsEntryVariants(t *testing.T) {
	for _, entry := range []string{".zprof/runs/", ".zprof/runs", "/.zprof/runs/", ".zprof/", ".zprof"} {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"),
			[]byte("thoughts/\n"+entry+"\n"), 0o644))
		require.Empty(t, checkRunsGitignored(dir), "entry %q should count as coverage", entry)
	}
}

func TestCheckRunsGitignoredIgnoresCommentedEntry(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"),
		[]byte("# .zprof/runs/\nthoughts/\n"), 0o644))
	require.Len(t, checkRunsGitignored(dir), 1, "a commented-out entry ignores nothing")
}

// No .gitignore at all: silent until run logs actually exist — the project
// may not even be a git repo yet.
func TestCheckRunsGitignoredNoGitignore(t *testing.T) {
	dir := t.TempDir()
	require.Empty(t, checkRunsGitignored(dir))

	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".zprof", "runs"), 0o755))
	issues := checkRunsGitignored(dir)
	require.Len(t, issues, 1)
	require.Equal(t, LevelWarn, issues[0].Level)
}

// --- .agentlog/ gitignored (telemetry stage 1) --------------------------

func TestCheckAgentlogGitignoredWarnsWhenEntryMissing(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"),
		[]byte("thoughts/\n*.zprof.bak-*\n"), 0o644))

	issues := checkAgentlogGitignored(dir)
	require.Len(t, issues, 1)
	require.Equal(t, LevelWarn, issues[0].Level)
	require.Contains(t, issues[0].Message, ".agentlog/")
}

func TestCheckAgentlogGitignoredAcceptsEntryVariants(t *testing.T) {
	for _, entry := range []string{".agentlog/", ".agentlog", "/.agentlog/"} {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"),
			[]byte("thoughts/\n"+entry+"\n"), 0o644))
		require.Empty(t, checkAgentlogGitignored(dir), "entry %q should count as coverage", entry)
	}
}

func TestCheckAgentlogGitignoredIgnoresCommentedEntry(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"),
		[]byte("# .agentlog/\nthoughts/\n"), 0o644))
	require.Len(t, checkAgentlogGitignored(dir), 1, "a commented-out entry ignores nothing")
}

// No .gitignore at all: silent until .agentlog/ actually exists — the
// project may not even be a git repo yet.
func TestCheckAgentlogGitignoredNoGitignore(t *testing.T) {
	dir := t.TempDir()
	require.Empty(t, checkAgentlogGitignored(dir))

	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".agentlog"), 0o755))
	issues := checkAgentlogGitignored(dir)
	require.Len(t, issues, 1)
	require.Equal(t, LevelWarn, issues[0].Level)
}

// --- .agentlog/ not tracked by git (telemetry stage 1) -------------------

// A file `git add`ed before the .gitignore entry existed stays tracked
// forever — a different failure than checkAgentlogGitignored, which only
// looks at .gitignore content.
func TestCheckAgentlogNotTrackedErrorsOnTrackedFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, exec.Command("git", "-C", dir, "init", "-q", "-b", "main").Run())
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".agentlog"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".agentlog", "dispatches.jsonl"), []byte("{}\n"), 0o644))
	require.NoError(t, exec.Command("git", "-C", dir, "add", ".agentlog/dispatches.jsonl").Run())
	require.NoError(t, exec.Command("git", "-C", dir, "-c", "user.email=t@t", "-c", "user.name=t",
		"commit", "-q", "-m", "oops").Run())

	issues := checkAgentlogNotTracked(dir)
	require.Len(t, issues, 1)
	require.Equal(t, LevelError, issues[0].Level)
	require.Contains(t, issues[0].Message, "dispatches.jsonl")
}

func TestCheckAgentlogNotTrackedSilentWhenUntracked(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, exec.Command("git", "-C", dir, "init", "-q", "-b", "main").Run())
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".agentlog"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".agentlog", "dispatches.jsonl"), []byte("{}\n"), 0o644))

	require.Empty(t, checkAgentlogNotTracked(dir))
}

// Without a working git (no repo here) there's nothing to diagnose —
// silent rather than a wall of false positives.
func TestCheckAgentlogNotTrackedSilentWithoutGitRepo(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".agentlog"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".agentlog", "dispatches.jsonl"), []byte("{}\n"), 0o644))

	require.Empty(t, checkAgentlogNotTracked(dir))
}

// --- telemetry hooks in settings.local.json (telemetry stage 1) ----------

func telemetryHookJSON(events ...string) string {
	entries := make([]string, len(events))
	for i, e := range events {
		entries[i] = fmt.Sprintf(`"%s": [{"hooks": [{"type": "command", "command": "test -x zprof-collect.py && zprof-collect.py %s || true"}]}]`, e, e)
	}
	return "{\n  \"hooks\": {\n    " + strings.Join(entries, ",\n    ") + "\n  }\n}"
}

// Gated on the collector script being deployed — a project that never
// applied a telemetry-shipping base profile has nothing for hooks to call.
func TestCheckTelemetryHooksSilentWithoutCollector(t *testing.T) {
	require.Empty(t, checkTelemetryHooks(t.TempDir()))
}

func TestCheckTelemetryHooksWarnsWhenSettingsMissing(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "zprof-collect.py"), []byte("#!/usr/bin/env python3\n"), 0o755))

	issues := checkTelemetryHooks(dir)
	require.Len(t, issues, 1)
	require.Equal(t, LevelWarn, issues[0].Level)
	require.Contains(t, issues[0].Message, "settings.local.json")
}

func TestCheckTelemetryHooksWarnsOnPartialInstall(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "zprof-collect.py"), []byte("#!/usr/bin/env python3\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "settings.local.json"),
		[]byte(telemetryHookJSON("SubagentStop", "Stop")), 0o644))

	issues := checkTelemetryHooks(dir)
	require.Len(t, issues, 1)
	require.Equal(t, LevelWarn, issues[0].Level)
	require.Contains(t, issues[0].Message, "SessionStart")
}

func TestCheckTelemetryHooksSilentWhenFullyInstalled(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "zprof-collect.py"), []byte("#!/usr/bin/env python3\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "settings.local.json"),
		[]byte(telemetryHookJSON("SubagentStop", "Stop", "SessionStart")), 0o644))

	require.Empty(t, checkTelemetryHooks(dir))
}

func TestCheckTelemetryHooksWarnsOnMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "zprof-collect.py"), []byte("#!/usr/bin/env python3\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "settings.local.json"), []byte("{not valid json"), 0o644))

	issues := checkTelemetryHooks(dir)
	require.Len(t, issues, 1)
	require.Equal(t, LevelWarn, issues[0].Level)
	require.Contains(t, issues[0].Message, "failed to parse")
}

// --- python3 availability (telemetry stage 1) -----------------------------

// The dev/CI machine running this suite must have a working python3 — zprof
// itself depends on it (design §20), so this is a fair assumption to bake
// into the happy-path test rather than skip it.
func TestCheckPython3AvailableHappyPath(t *testing.T) {
	require.Empty(t, checkPython3Available())
}

func TestCheckPython3AvailableWarnsWhenMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	issues := checkPython3Available()
	require.Len(t, issues, 1)
	require.Equal(t, LevelWarn, issues[0].Level)
	require.Contains(t, issues[0].Message, "python3")
}

// --- git clean -xdf vulnerability reminder (telemetry stage 1) -----------

func TestCheckAgentlogCleanVulnerabilitySilentWhenAbsent(t *testing.T) {
	require.Empty(t, checkAgentlogCleanVulnerability(t.TempDir()))
}

func TestCheckAgentlogCleanVulnerabilitySilentWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".agentlog"), 0o755))
	require.Empty(t, checkAgentlogCleanVulnerability(dir))
}

func TestCheckAgentlogCleanVulnerabilityInfoWhenPopulated(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".agentlog"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".agentlog", "dispatches.jsonl"), []byte("{}\n"), 0o644))

	issues := checkAgentlogCleanVulnerability(dir)
	require.Len(t, issues, 1)
	require.Equal(t, LevelInfo, issues[0].Level)
	require.Contains(t, issues[0].Message, "git clean")
}

// --- wiring into Diagnose (telemetry stage 1) -----------------------------

func TestDiagnoseWiresInAgentlogChecks(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"), []byte("overlays: []\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(proj, ".agentlog"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".agentlog", "dispatches.jsonl"), []byte("{}\n"), 0o644))
	repo := t.TempDir()

	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)
	require.True(t, findIssue(issues, LevelWarn, ".agentlog/"))
	require.True(t, findIssue(issues, LevelInfo, "git clean"))
}

// --- doctor messages are English (project convention) -------------------

func TestDoctorMessagesAreEnglish(t *testing.T) {
	runner := checkTaskRunner(mustAgentsDirProject(t))
	require.NotEmpty(t, runner)

	repo := t.TempDir()
	ovDir := filepath.Join(repo, "overlays", "demo")
	require.NoError(t, os.MkdirAll(ovDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ovDir, "manifest.yaml"),
		[]byte("name: demo\nloop_template: dev-pipeline\n"), 0o644))

	runs := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(runs, ".zprof", "runs"), 0o755))
	for i := 0; i < 51; i++ {
		require.NoError(t, os.WriteFile(
			filepath.Join(runs, ".zprof", "runs", fmt.Sprintf("r%02d.md", i)), []byte("x"), 0o644))
	}

	// Telemetry fixture: a git repo with a tracked .agentlog/ file (no
	// .gitignore entry either), plus a deployed collector with no hooks
	// wired up — trips all four project-state telemetry checks at once.
	telemetry := t.TempDir()
	require.NoError(t, exec.Command("git", "-C", telemetry, "init", "-q", "-b", "main").Run())
	require.NoError(t, os.MkdirAll(filepath.Join(telemetry, ".agentlog"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(telemetry, ".agentlog", "dispatches.jsonl"), []byte("{}\n"), 0o644))
	require.NoError(t, exec.Command("git", "-C", telemetry, "add", ".agentlog/dispatches.jsonl").Run())
	require.NoError(t, exec.Command("git", "-C", telemetry, "-c", "user.email=t@t", "-c", "user.name=t",
		"commit", "-q", "-m", "oops").Run())
	require.NoError(t, os.MkdirAll(filepath.Join(telemetry, ".claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(telemetry, ".claude", "zprof-collect.py"), []byte("#!/usr/bin/env python3\n"), 0o755))

	var all []Issue
	all = append(all, runner...)
	all = append(all, checkStopLists([]string{"demo"}, repo)...)
	all = append(all, checkRunLogs(runs)...)
	all = append(all, checkRunsGitignored(runs)...)
	all = append(all, checkAgentlogGitignored(telemetry)...)
	all = append(all, checkAgentlogNotTracked(telemetry)...)
	all = append(all, checkTelemetryHooks(telemetry)...)
	all = append(all, checkPython3Available()...)
	all = append(all, checkAgentlogCleanVulnerability(telemetry)...)
	require.NotEmpty(t, all)

	for _, i := range all {
		for _, r := range i.Message {
			require.False(t, r >= 'а' && r <= 'я' || r >= 'А' && r <= 'Я' || r == 'ё' || r == 'Ё',
				"Issue.Message must be English, got Cyrillic in: %s", i.Message)
		}
	}
}

func mustAgentsDirProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "dev-orchestrator.md"),
		[]byte("---\nname: dev-orchestrator\n---\n"), 0o644))
	return dir
}
