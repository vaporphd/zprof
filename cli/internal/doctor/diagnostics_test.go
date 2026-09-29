// cli/internal/doctor/diagnostics_test.go
package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	satisfyNewGuardChecks(t, proj)
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

// --- Diagnose(): telemetry-only mode without .zprof.yaml (issue #64) ------

// telemetryOnlyInfoMsg is the exact info-level text AC1 specifies for the
// no-manifest-but-telemetry-deployed branch.
const telemetryOnlyInfoMsg = "no .zprof.yaml — manifest checks skipped (telemetry-only project)"

// TestDiagnoseTelemetryOnlyMode covers the branching Diagnose must do on a
// manifest.LoadProject failure: physically absent (fs.ErrNotExist) with
// either collector script deployed falls back to the telemetry-only subset;
// physically absent with nothing deployed keeps the pre-existing single
// LevelError; and a present-but-broken manifest (not ErrNotExist) is
// unaffected by telemetry deployment either way.
func TestDiagnoseTelemetryOnlyMode(t *testing.T) {
	cases := []struct {
		name          string
		setup         func(t *testing.T, proj string)
		wantSingleErr bool
	}{
		{
			name: "collector deployed without manifest",
			setup: func(t *testing.T, proj string) {
				require.NoError(t, os.MkdirAll(filepath.Join(proj, ".claude"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(proj, ".claude", "zprof-collect.py"), []byte("#!/usr/bin/env python3\n"), 0o755))
			},
		},
		{
			name: "guard deployed without manifest, no collector",
			setup: func(t *testing.T, proj string) {
				require.NoError(t, os.MkdirAll(filepath.Join(proj, ".claude"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(proj, ".claude", "zprof-guard.py"), []byte("#!/usr/bin/env python3\n"), 0o755))
			},
		},
		{
			name:          "nothing at all",
			setup:         func(t *testing.T, proj string) {},
			wantSingleErr: true,
		},
		{
			name: "broken manifest with telemetry deployed stays an error, not telemetry-only",
			setup: func(t *testing.T, proj string) {
				require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"),
					[]byte("overlays: [this is not valid yaml: :\n"), 0o644))
				require.NoError(t, os.MkdirAll(filepath.Join(proj, ".claude"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(proj, ".claude", "zprof-collect.py"), []byte("#!/usr/bin/env python3\n"), 0o755))
			},
			wantSingleErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proj := t.TempDir()
			tc.setup(t, proj)
			t.Setenv("HOME", t.TempDir())
			repo := t.TempDir()

			issues, err := Diagnose(proj, repo)
			require.NoError(t, err)

			if tc.wantSingleErr {
				require.Len(t, issues, 1)
				require.Equal(t, LevelError, issues[0].Level)
				require.Contains(t, issues[0].Message, "failed to parse .zprof.yaml")
				return
			}

			require.False(t, hasLevel(issues, LevelError))
			require.True(t, findIssue(issues, LevelInfo, telemetryOnlyInfoMsg))
		})
	}
}

// TestDiagnoseTelemetryOnlySkipsManifestGatedChecks proves the telemetry-only
// branch both runs the guard-deployment checks — via the zero-value
// &manifest.ProjectManifest{} it feeds checkGuardDeployment, so guard
// defaults to enabled per ADR 0009 §8.3 — and skips every check gated on a
// real manifest, in particular checkTaskRunner: this fixture deliberately
// leaves .claude/agents/task-runner.md out, which checkTaskRunner would
// report as a LevelError had it run.
func TestDiagnoseTelemetryOnlySkipsManifestGatedChecks(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(proj, ".claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".claude", "zprof-collect.py"), []byte("#!/usr/bin/env python3\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".claude", "zprof-guard.py"), []byte("#!/usr/bin/env python3\n"), 0o755))
	// Present but empty: had checkTaskRunner run against it, it would
	// report task-runner.md as missing.
	require.NoError(t, os.MkdirAll(filepath.Join(proj, ".claude", "agents"), 0o755))

	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()

	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)

	require.False(t, hasLevel(issues, LevelError))
	require.False(t, findIssue(issues, LevelError, "task-runner"))
	require.True(t, findIssue(issues, LevelInfo, telemetryOnlyInfoMsg))
	// Guard checks are visible: both hook events are missing (no
	// settings.local.json deployed) and guard.json itself is missing.
	require.True(t, findIssue(issues, LevelWarn, "guard hooks missing for PreToolUse, SubagentStop"))
	require.True(t, findIssue(issues, LevelWarn, "guard.json is missing"))
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
		[]byte("---\nname: auditor\nmodel: opus\nreturn_format: |\n  completion: complete\n---\nBody.\n"), 0o644))
	// task-runner is a role, so it must also declare a return_format.
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".claude", "agents", "task-runner.md"),
		[]byte("---\nname: task-runner\nmodel: opus\nreturn_format: |\n  verdict: done\n---\nBody.\n"), 0o644))
	satisfyNewGuardChecks(t, proj)
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
	satisfyNewGuardChecks(t, proj)
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

// routeTaskRunnerFixture builds a minimal task-runner.md whose `## Роутинг`
// table references routeAgent (an agent not shipped as a file) and whose
// `### Условные агенты маршрутов` whitelist covers whitelisted.
func routeTaskRunnerFixture(routeAgent, whitelisted string) string {
	return "---\nname: task-runner\n---\n\n" +
		"## Роутинг\n\n" +
		"| Тип | Цепочка |\n" +
		"|---|---|\n" +
		"| Новая фича | `planner → " + routeAgent + " → tester` |\n\n" +
		"### Условные агенты маршрутов\n\n" +
		"Эти агенты существуют только при определённом overlay: `" + whitelisted + "`.\n\n" +
		"Имена агентов бери из таблицы `## Consilium`.\n\n" +
		"## Правила диспатча\n\n- Один агент за раз.\n"
}

func TestCheckRouteAgentsExistWarnsOnMissingUnwhitelisted(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "planner.md"), []byte("---\nname: planner\n---\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "tester.md"), []byte("---\nname: tester\n---\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "task-runner.md"),
		[]byte(routeTaskRunnerFixture("ghost-agent", "report-writer")), 0o644))

	issues := checkRouteAgentsExist(dir)
	require.Len(t, issues, 1)
	require.Equal(t, LevelWarn, issues[0].Level)
	require.Contains(t, issues[0].Message, `"ghost-agent"`)
}

func TestCheckRouteAgentsExistSilentWhenWhitelisted(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "planner.md"), []byte("---\nname: planner\n---\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "tester.md"), []byte("---\nname: tester\n---\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "task-runner.md"),
		[]byte(routeTaskRunnerFixture("report-writer", "report-writer")), 0o644))

	require.Empty(t, checkRouteAgentsExist(dir))
}

// A route agent that's physically present must never warn, regardless of
// the whitelist.
func TestCheckRouteAgentsExistSilentWhenAgentPresent(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	for _, name := range []string{"planner", "tester", "implementer"} {
		require.NoError(t, os.WriteFile(filepath.Join(agentsDir, name+".md"), []byte("---\nname: "+name+"\n---\n"), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "task-runner.md"),
		[]byte(routeTaskRunnerFixture("implementer", "report-writer")), 0o644))

	require.Empty(t, checkRouteAgentsExist(dir))
}

// A multi-overlay apply namespaces on-disk agent files (`implementer-ios.md`
// rather than `implementer.md`). The route table still names the bare role,
// and that must resolve via agents.RoleOf instead of a literal os.Stat, or
// every namespaced role false-positives as missing.
func TestCheckRouteAgentsExistSilentWhenOnlyNamespacedFileOnDisk(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "planner.md"), []byte("---\nname: planner\n---\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "tester.md"), []byte("---\nname: tester\n---\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "implementer-ios.md"), []byte("---\nname: implementer-ios\n---\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "task-runner.md"),
		[]byte(routeTaskRunnerFixture("implementer", "report-writer")), 0o644))

	require.Empty(t, checkRouteAgentsExist(dir))
}

// The same missing, unwhitelisted agent named in multiple chain cells must
// only warn once.
func TestCheckRouteAgentsExistDedupesRepeatedAgent(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "tester.md"), []byte("---\nname: tester\n---\n"), 0o644))
	trContent := "---\nname: task-runner\n---\n\n" +
		"## Роутинг\n\n" +
		"| Тип | Цепочка |\n" +
		"|---|---|\n" +
		"| Новая фича | `ghost-agent → tester` |\n" +
		"| Багфикс | `ghost-agent → tester` |\n\n" +
		"### Условные агенты маршрутов\n\n" +
		"Эти агенты существуют только при определённом overlay: `report-writer`.\n\n" +
		"## Правила диспатча\n\n- Один агент за раз.\n"
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "task-runner.md"), []byte(trContent), 0o644))

	issues := checkRouteAgentsExist(dir)
	require.Len(t, issues, 1)
	require.Contains(t, issues[0].Message, `"ghost-agent"`)
}

// No task-runner.md at all — checkTaskRunner already reports that; this
// check must stay silent rather than double-report.
func TestCheckRouteAgentsExistSilentWithoutTaskRunner(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude", "agents"), 0o755))

	require.Empty(t, checkRouteAgentsExist(dir))
}

// `## Роутинг` is the last section in the file — no `## ` heading follows it
// at all. sectionUntilNextH2's "no next heading found" fallback (return the
// rest of the file verbatim) must still surface a missing, unwhitelisted
// route agent instead of silently truncating to nothing.
func TestCheckRouteAgentsExistRoutingSectionIsLastInFile(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "tester.md"), []byte("---\nname: tester\n---\n"), 0o644))
	trContent := "---\nname: task-runner\n---\n\n" +
		"## Роутинг\n\n" +
		"| Тип | Цепочка |\n" +
		"|---|---|\n" +
		"| Новая фича | `ghost-agent → tester` |\n"
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "task-runner.md"), []byte(trContent), 0o644))

	issues := checkRouteAgentsExist(dir)
	require.Len(t, issues, 1)
	require.Contains(t, issues[0].Message, `"ghost-agent"`)
}

// The `### Условные агенты маршрутов` whitelist paragraph is the very last
// content in the file — no trailing blank line follows it. sectionParagraph's
// "no next blank line found" fallback (return the rest of the file verbatim)
// must still capture the whitelisted name instead of dropping it, which
// would otherwise turn a legitimate conditional agent into a false warning.
func TestCheckRouteAgentsExistWhitelistIsLastContentNoTrailingBlankLine(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "tester.md"), []byte("---\nname: tester\n---\n"), 0o644))
	trContent := "---\nname: task-runner\n---\n\n" +
		"## Роутинг\n\n" +
		"| Тип | Цепочка |\n" +
		"|---|---|\n" +
		"| Новая фича | `ghost-agent → tester` |\n\n" +
		"### Условные агенты маршрутов\n\n" +
		"Эти агенты существуют только при определённом overlay: `ghost-agent`."
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "task-runner.md"), []byte(trContent), 0o644))

	require.Empty(t, checkRouteAgentsExist(dir))
}

// `## Роутинг` exists but its body is prose with no markdown table at all
// (no line starts with `|`). There are no chain cells to parse, so the check
// must stay silent rather than mis-parsing prose as a route or crashing.
func TestCheckRouteAgentsExistSilentWhenRoutingSectionHasNoTable(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	trContent := "---\nname: task-runner\n---\n\n" +
		"## Роутинг\n\n" +
		"Маршрутизация описана в prose, таблицы нет — смотри AGENT_LOOP.md.\n\n" +
		"## Правила диспатча\n\n- Один агент за раз.\n"
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "task-runner.md"), []byte(trContent), 0o644))

	require.Empty(t, checkRouteAgentsExist(dir))
}

// doctorRepoRoot locates the zprof repository root from this test file's own
// path, mirroring internal/verdicts/repo_test.go's convention — cli/internal/
// doctor is three directories under the root, same depth as cli/internal/
// verdicts.
func doctorRepoRoot(t *testing.T) string {
	t.Helper()
	_, f, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root, err := filepath.Abs(filepath.Join(filepath.Dir(f), "..", "..", ".."))
	require.NoError(t, err)
	return root
}

// copyAgentFiles copies every top-level *.md file from srcDir into dstDir
// (creating dstDir if needed), overwriting on name collision. Used to
// assemble a realistic .claude/agents/ roster out of the actual profiles/base
// and profiles/overlays checkouts rather than a synthetic fixture.
func copyAgentFiles(t *testing.T, dstDir, srcDir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dstDir, 0o755))
	entries, err := os.ReadDir(srcDir)
	require.NoError(t, err)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dstDir, e.Name()), data, 0o644))
	}
}

// realTaskRunnerProjectFixture assembles .claude/agents/ out of the actual
// profiles/base/agents/ plus every overlay's agents/ EXCEPT re-macho, whose
// intake/unpacker/hypothesizer/verifier/report-writer are exactly the names
// the real `### Условные агенты маршрутов` whitelist covers. Excluding
// re-macho is deliberate: if its agents were on disk, checkRouteAgentsExist
// would find them via the plain os.Stat path and the whitelist parsing (five
// separate comma-joined backtick spans, plus non-agent backtick text like
// `.claude/agents/` and `re-macho` in the very same sentence) would never be
// exercised at all.
func realTaskRunnerProjectFixture(t *testing.T) string {
	t.Helper()
	root := doctorRepoRoot(t)
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	copyAgentFiles(t, agentsDir, filepath.Join(root, "profiles", "base", "agents"))

	overlaysDir := filepath.Join(root, "profiles", "overlays")
	entries, err := os.ReadDir(overlaysDir)
	require.NoError(t, err)
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "re-macho" {
			continue
		}
		src := filepath.Join(overlaysDir, e.Name(), "agents")
		if info, statErr := os.Stat(src); statErr == nil && info.IsDir() {
			copyAgentFiles(t, agentsDir, src)
		}
	}
	return proj
}

// TestCheckRouteAgentsExistAgainstRealTaskRunnerIsClean is the repo-level
// backstop for AC1/AC6: the actual `## Роутинг` table and `### Условные
// агенты маршрутов` whitelist in profiles/base/agents/task-runner.md — not a
// synthetic single-name fixture — must produce zero warnings once every
// generic-route role is deployed somewhere in the fleet, even though the
// real whitelist paragraph lists five agents as separate comma-joined
// backtick spans and also backtick-quotes non-agent text (`.claude/agents/`,
// `re-macho`) in that same sentence. A regression here means either the
// canonical route table names a role no overlay ships, or a routing-table
// edit broke the very whitelist parsing meant to keep RE-only agents from
// false-warning.
func TestCheckRouteAgentsExistAgainstRealTaskRunnerIsClean(t *testing.T) {
	proj := realTaskRunnerProjectFixture(t)
	require.Empty(t, checkRouteAgentsExist(proj))
}

// The mirror case against the real file: a genuinely missing, unwhitelisted
// agent named in two different routes (`implementer` appears in both the
// new-feature and the bugfix chain) must still warn exactly once, not
// per-occurrence — proving the dedupe behavior holds against the production
// table, not just the synthetic DedupesRepeatedAgent fixture.
func TestCheckRouteAgentsExistAgainstRealTaskRunnerWarnsOnGenuinelyMissingAgent(t *testing.T) {
	proj := realTaskRunnerProjectFixture(t)
	require.NoError(t, os.Remove(filepath.Join(proj, ".claude", "agents", "implementer.md")))

	issues := checkRouteAgentsExist(proj)
	require.Len(t, issues, 1)
	require.Equal(t, LevelWarn, issues[0].Level)
	require.Contains(t, issues[0].Message, `"implementer"`)
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

// --- git checkout hygiene (issue #62) -------------------------------------

// runGitCheckoutHygieneCmd runs a git subcommand against dir and fails the
// test immediately on error — a thin wrapper to keep the fixtures below
// readable; mirrors the exec.Command("git", "-C", dir, ...) calls used
// throughout this file for .agentlog/ fixtures.
func runGitCheckoutHygieneCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, out)
}

// initGitCheckoutHygieneRepo creates a git repo at dir on branch "main" with
// one commit, so a feature branch and worktrees can be created against a
// real ref. gitDefaultBranch falls back to "main" for a repo with no origin
// remote, which is exactly this fixture's shape.
func initGitCheckoutHygieneRepo(t *testing.T, dir string) {
	t.Helper()
	runGitCheckoutHygieneCmd(t, dir, "init", "-q", "-b", "main")
	runGitCheckoutHygieneCmd(t, dir, "-c", "user.email=t@t", "-c", "user.name=t",
		"commit", "-q", "-m", "init", "--allow-empty")
}

func TestCheckGitCheckoutHygieneCleanRepoOnDefaultBranchIsSilent(t *testing.T) {
	dir := t.TempDir()
	initGitCheckoutHygieneRepo(t, dir)

	require.Empty(t, checkGitCheckoutHygiene(dir))
}

func TestCheckGitCheckoutHygieneWarnsOnNonDetachedWorktreeOnDefaultBranch(t *testing.T) {
	dir := t.TempDir()
	initGitCheckoutHygieneRepo(t, dir)
	runGitCheckoutHygieneCmd(t, dir, "checkout", "-q", "-b", "feat")

	wt := filepath.Join(t.TempDir(), "wt")
	runGitCheckoutHygieneCmd(t, dir, "worktree", "add", wt, "main")

	issues := checkGitCheckoutHygiene(dir)
	require.True(t, findIssue(issues, LevelWarn, "non-detached"))
	require.True(t, findIssue(issues, LevelWarn, wt))
}

func TestCheckGitCheckoutHygieneSilentOnDetachedWorktreeOnDefaultBranch(t *testing.T) {
	dir := t.TempDir()
	initGitCheckoutHygieneRepo(t, dir)
	runGitCheckoutHygieneCmd(t, dir, "checkout", "-q", "-b", "feat")

	wt := filepath.Join(t.TempDir(), "wt")
	runGitCheckoutHygieneCmd(t, dir, "worktree", "add", "--detach", wt, "main")

	issues := checkGitCheckoutHygiene(dir)
	require.False(t, findIssue(issues, LevelWarn, "non-detached"))
}

func TestCheckGitCheckoutHygieneWarnsWhenMainNotOnDefaultBranchNoActiveRun(t *testing.T) {
	dir := t.TempDir()
	initGitCheckoutHygieneRepo(t, dir)
	runGitCheckoutHygieneCmd(t, dir, "checkout", "-q", "-b", "feat")

	issues := checkGitCheckoutHygiene(dir)
	require.True(t, findIssue(issues, LevelWarn, "main working tree is on"))
}

func TestCheckGitCheckoutHygieneSilentWhenMainNotOnDefaultBranchButRunActive(t *testing.T) {
	dir := t.TempDir()
	initGitCheckoutHygieneRepo(t, dir)
	runGitCheckoutHygieneCmd(t, dir, "checkout", "-q", "-b", "feat")

	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".zprof", "runs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".zprof", "runs", "2026-09-29-x.md"),
		[]byte("# task\nstarted: now\n\n| время | агент | verdict | artifact |\n"), 0o644))

	issues := checkGitCheckoutHygiene(dir)
	require.False(t, findIssue(issues, LevelWarn, "main working tree is on"))
}

// The main working tree itself can be in detached HEAD (e.g. a CI checkout,
// or a user who ran `git checkout --detach`), not just a linked worktree —
// main.branch is then empty, and checkGitCheckoutHygiene must render that as
// "detached HEAD" in the warning rather than an empty, confusing quoted
// string.
func TestCheckGitCheckoutHygieneWarnsWithDetachedHEADLabelWhenMainItselfIsDetached(t *testing.T) {
	dir := t.TempDir()
	initGitCheckoutHygieneRepo(t, dir)
	runGitCheckoutHygieneCmd(t, dir, "checkout", "-q", "--detach", "main")

	issues := checkGitCheckoutHygiene(dir)
	require.True(t, findIssue(issues, LevelWarn, `main working tree is on "detached HEAD"`))
}

// A run log exists but is already complete (task-runner wrote `## Итог`
// before returning its final schema) — hasActiveGitRun must tell this apart
// from a run genuinely in flight, so stale run history left over from a
// finished run must not silence the "main is on the wrong branch" warning.
func TestCheckGitCheckoutHygieneWarnsWhenMainNotOnDefaultBranchAndRunLogIsCompleted(t *testing.T) {
	dir := t.TempDir()
	initGitCheckoutHygieneRepo(t, dir)
	runGitCheckoutHygieneCmd(t, dir, "checkout", "-q", "-b", "feat")

	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".zprof", "runs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".zprof", "runs", "2026-09-29-x.md"),
		[]byte("# task\nstarted: now\n\n## Итог\nverdict: done\n"), 0o644))

	issues := checkGitCheckoutHygiene(dir)
	require.True(t, findIssue(issues, LevelWarn, "main working tree is on"))
}

// gitDefaultBranch's fallback-to-"main" path is exercised implicitly by
// every other fixture in this file (none of them configure an origin
// remote). This is the mirror case: a repo whose refs/remotes/origin/HEAD
// symref actually resolves — the way `git clone` sets it up — must report
// that real branch name, exercising the TrimSpace/TrimPrefix parsing of
// `git symbolic-ref`'s output rather than only its error path.
func TestGitDefaultBranchResolvesFromOriginHEADSymref(t *testing.T) {
	dir := t.TempDir()
	initGitCheckoutHygieneRepo(t, dir)
	runGitCheckoutHygieneCmd(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/develop")

	require.Equal(t, "develop", gitDefaultBranch(dir))
}

// Direct unit test of the fallback itself: no origin remote configured at
// all, so `git symbolic-ref` fails and gitDefaultBranch must guess "main"
// rather than propagate the error.
func TestGitDefaultBranchFallsBackToMainWithoutOriginRemote(t *testing.T) {
	dir := t.TempDir()
	initGitCheckoutHygieneRepo(t, dir)

	require.Equal(t, "main", gitDefaultBranch(dir))
}

func TestCheckGitCheckoutHygieneSilentWithoutGitRepo(t *testing.T) {
	dir := t.TempDir()
	require.Empty(t, checkGitCheckoutHygiene(dir))
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

// TestDiagnoseSurfacesRunnerBudgetWithoutAuditSection proves the wiring in
// Diagnose itself, not just checkRunnerBudget in isolation: a project with
// no `audit:` section at all (so AuditEnabled() is false) and a too-small
// `runner.max_dispatches` must still surface the budget warning through the
// public Diagnose entry point. A regression that re-gates the call behind
// proj.AuditEnabled() would pass every checkRunnerBudget-direct test above
// yet silently drop the issue here.
func TestDiagnoseSurfacesRunnerBudgetWithoutAuditSection(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"),
		[]byte("overlays: []\nrunner:\n  max_dispatches: 2\n"), 0o644))
	repo := t.TempDir()

	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)
	require.True(t, findIssue(issues, LevelWarn, "runner.max_dispatches"))
	require.False(t, findIssue(issues, LevelWarn, "deprecated"),
		"no audit.max_dispatches was set — the deprecation warning must not fire")
}

// --- global runner dispatch budget (issue #19) ---------------------------

// The counter is global, not audit-gated: a too-small budget must warn even
// with no audit: section at all.
func TestCheckRunnerBudgetWarnsBelowThreeWithoutAuditSection(t *testing.T) {
	proj := &manifest.ProjectManifest{Runner: &manifest.RunnerConfig{MaxDispatches: 2}}

	issues := checkRunnerBudget(proj)
	require.Len(t, issues, 1)
	require.Equal(t, LevelWarn, issues[0].Level)
	require.Contains(t, issues[0].Message, "runner.max_dispatches")
}

// A too-small budget set only via the deprecated audit.max_dispatches alias
// still falls through RunnerMaxDispatches() and must still warn — the
// budget check doesn't care which key set it. Setting the alias also trips
// the separate deprecation warning, so both fire together.
func TestCheckRunnerBudgetWarnsBelowThreeViaAuditAlias(t *testing.T) {
	proj := &manifest.ProjectManifest{Audit: &manifest.AuditConfig{Enabled: false, MaxDispatches: 2}}

	issues := checkRunnerBudget(proj)
	require.Len(t, issues, 2)
	require.True(t, findIssue(issues, LevelWarn, "runner.max_dispatches"))
	require.True(t, findIssue(issues, LevelWarn, "deprecated"))
}

// audit.max_dispatches being set at all is a deprecation warning,
// regardless of audit.enabled or whether the value is otherwise healthy.
func TestCheckRunnerBudgetWarnsOnDeprecatedAuditAlias(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		proj := &manifest.ProjectManifest{Audit: &manifest.AuditConfig{Enabled: enabled, MaxDispatches: 9}}

		issues := checkRunnerBudget(proj)
		require.Len(t, issues, 1)
		require.Equal(t, LevelWarn, issues[0].Level)
		require.Contains(t, issues[0].Message, "deprecated")
		require.Contains(t, issues[0].Message, "9")
	}
}

// Neither key set: default budget (14) is healthy and there's no alias to
// deprecate — silent.
func TestCheckRunnerBudgetSilentWhenNothingConfigured(t *testing.T) {
	require.Empty(t, checkRunnerBudget(&manifest.ProjectManifest{}))
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

// --- verdicts registry (ADR 0003 / issue #20) ----------------------------

// smallVerdictsRegistry is a minimal but internally consistent
// verdicts.yaml covering just the roles these tests exercise.
const smallVerdictsRegistry = `
version: 1
base_enum: [done, blocked, failed]
actions: [next, loop, insert, triage, escalate, abort]
universal:
  blocked: {base: blocked, action: triage}
quotes:
  auditor: [done]
roles:
  implementer:
    done:   {base: done,   action: next}
    failed: {base: failed, action: abort}
  reviewer:
    approve:            {base: done,   action: next}
    approve-with-fixes: {base: done,   action: "insert:implementer"}
    block:              {base: failed, action: "loop:implementer"}
  auditor:
    complete:   {base: done,    action: next}
    incomplete: {base: failed,  action: "loop:@audited"}
    blocked:    {base: blocked, action: escalate}
`

// writeVerdictsRepoFixture writes a repo checkout containing just
// base/verdicts.yaml at the path checkAgentVerdicts expects.
func writeVerdictsRepoFixture(t *testing.T, registry string) string {
	t.Helper()
	repo := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "base"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "base", "verdicts.yaml"), []byte(registry), 0o644))
	return repo
}

func TestCheckAgentVerdicts_CleanAgentIsSilent(t *testing.T) {
	repo := writeVerdictsRepoFixture(t, smallVerdictsRegistry)
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "implementer.md"),
		[]byte("---\nname: implementer\nreturn_format: |\n  verdict: done|blocked|failed\n---\nReturn `verdict: done` when finished.\n"), 0o644))

	require.Empty(t, checkAgentVerdicts(proj, repo))
}

func TestCheckAgentVerdicts_UnknownTokenInReturnFormatErrors(t *testing.T) {
	repo := writeVerdictsRepoFixture(t, smallVerdictsRegistry)
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "implementer.md"),
		[]byte("---\nname: implementer\nreturn_format: |\n  verdict: done|zzz\n---\nbody\n"), 0o644))

	issues := checkAgentVerdicts(proj, repo)
	require.True(t, findIssue(issues, LevelError, `token "zzz" of role "implementer" is not in verdicts.yaml`))
}

// TestCheckAgentVerdicts_BodyVerdictOutsideEnumErrors is the AC4 regression:
// a reviewer whose body returns `verdict: blocked` when `blocked` is not in
// its own return_format enum (systems-rust/reviewer.md's :10 vs :25 before
// the fix).
func TestCheckAgentVerdicts_BodyVerdictOutsideEnumErrors(t *testing.T) {
	repo := writeVerdictsRepoFixture(t, smallVerdictsRegistry)
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "reviewer.md"),
		[]byte("---\nname: reviewer\nreturn_format: |\n  verdict: approve|block\n---\n"+
			"Refuse self-review and return `verdict: blocked` with a reason.\n"), 0o644))

	issues := checkAgentVerdicts(proj, repo)
	require.True(t, findIssue(issues, LevelError, "is outside this agent's return_format enum"))
}

func TestCheckAgentVerdicts_LegacyCompletionKeyErrors(t *testing.T) {
	repo := writeVerdictsRepoFixture(t, smallVerdictsRegistry)
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "auditor.md"),
		[]byte("---\nname: auditor\nreturn_format: |\n  completion: complete|incomplete|blocked\n---\nbody\n"), 0o644))

	issues := checkAgentVerdicts(proj, repo)
	require.True(t, findIssue(issues, LevelError, `return_format must start with "verdict:", got "completion:"`))
}

// A namespaced role name (`reviewer-rs`, the way a multi-overlay apply
// writes it) must still resolve to `reviewer` in the registry.
func TestCheckAgentVerdicts_NamespacedRoleResolves(t *testing.T) {
	repo := writeVerdictsRepoFixture(t, smallVerdictsRegistry)
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "reviewer-rs.md"),
		[]byte("---\nname: reviewer-rs\nreturn_format: |\n  verdict: approve|zzz\n---\nbody\n"), 0o644))

	issues := checkAgentVerdicts(proj, repo)
	require.True(t, findIssue(issues, LevelError, `token "zzz" of role "reviewer" is not in verdicts.yaml`))
}

// A user's own agent (not a zprof role at all) outside the registry
// produces no issue — "none of doctor's business".
func TestCheckAgentVerdicts_UserAgentOutsideRegistryIsSilent(t *testing.T) {
	repo := writeVerdictsRepoFixture(t, smallVerdictsRegistry)
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "my-custom-agent.md"),
		[]byte("---\nname: my-custom-agent\nreturn_format: |\n  verdict: whatever\n---\nbody\n"), 0o644))

	require.Empty(t, checkAgentVerdicts(proj, repo))
}

// checkAgentVerdicts is gated on repoDir/base existing at all — a bare temp
// dir (what most doctor tests pass as repoDir) means nothing to validate
// against, not a project misconfiguration.
func TestCheckAgentVerdicts_SilentWhenRepoHasNoBaseDir(t *testing.T) {
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "implementer.md"),
		[]byte("---\nname: implementer\nreturn_format: |\n  verdict: done|blocked|failed\n---\nbody\n"), 0o644))

	require.Empty(t, checkAgentVerdicts(proj, t.TempDir()))
}

// Once repoDir/base is a real checkout but predates the registry file
// itself, doctor warns rather than silently skipping.
func TestCheckAgentVerdicts_WarnsWhenRegistryFileMissing(t *testing.T) {
	repo := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "base"), 0o755))
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "implementer.md"),
		[]byte("---\nname: implementer\nreturn_format: |\n  verdict: done|blocked|failed\n---\nbody\n"), 0o644))

	issues := checkAgentVerdicts(proj, repo)
	require.True(t, findIssue(issues, LevelWarn, "verdicts.yaml not found in repo checkout"))
}

// A verdicts.yaml that fails to parse at all (e.g. a typo'd top-level key
// tripping yaml.KnownFields) must surface as a doctor error, not a silent
// pass-through — internal/verdicts.Load's error wraps back through
// checkAgentVerdicts verbatim.
func TestCheckAgentVerdicts_RegistryLoadErrorReportsIssue(t *testing.T) {
	repo := writeVerdictsRepoFixture(t, "version: 1\nrole:\n  implementer: {}\n") // `role:` typo for `roles:`
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "implementer.md"),
		[]byte("---\nname: implementer\nreturn_format: |\n  verdict: done\n---\nbody\n"), 0o644))

	issues := checkAgentVerdicts(proj, repo)
	require.True(t, findIssue(issues, LevelError, "parse verdicts.yaml"))
}

// A verdicts.yaml that parses but is internally inconsistent (a spec's
// `base` isn't in `base_enum`) must fail Registry.Validate and surface as
// a doctor error distinct from a parse error.
func TestCheckAgentVerdicts_RegistryValidateErrorReportsIssue(t *testing.T) {
	inconsistent := `
version: 1
base_enum: [done, blocked, failed]
actions: [next]
roles:
  implementer:
    done: {base: bogus, action: next}
`
	repo := writeVerdictsRepoFixture(t, inconsistent)
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "implementer.md"),
		[]byte("---\nname: implementer\nreturn_format: |\n  verdict: done\n---\nbody\n"), 0o644))

	issues := checkAgentVerdicts(proj, repo)
	require.True(t, findIssue(issues, LevelError, "verdicts.yaml is inconsistent"))
}

// A role zprof's own roster knows (agents.RoleOf resolves it, e.g.
// "planner") but that the registry itself never defines is a genuine
// registry/roster drift — distinct from
// TestCheckAgentVerdicts_UserAgentOutsideRegistryIsSilent's "not a zprof
// role at all" case, which must stay silent.
func TestCheckAgentVerdicts_KnownRoleMissingFromRegistryErrors(t *testing.T) {
	repo := writeVerdictsRepoFixture(t, smallVerdictsRegistry) // defines no `planner` role
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "planner.md"),
		[]byte("---\nname: planner\nreturn_format: |\n  verdict: done\n---\nbody\n"), 0o644))

	issues := checkAgentVerdicts(proj, repo)
	require.True(t, findIssue(issues, LevelError, `role "planner" is missing from verdicts.yaml`))
}

// When .agentlog/schema.json exists but predates the verdicts registry
// (no `verdicts` key), doctor warns the project to redeploy rather than
// silently trusting a stale schema.
func TestCheckAgentVerdicts_SchemaJSONMissingVerdictsKeyWarns(t *testing.T) {
	repo := writeVerdictsRepoFixture(t, smallVerdictsRegistry)
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "implementer.md"),
		[]byte("---\nname: implementer\nreturn_format: |\n  verdict: done|blocked|failed\n---\nbody\n"), 0o644))
	agentlogDir := filepath.Join(proj, ".agentlog")
	require.NoError(t, os.MkdirAll(agentlogDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentlogDir, "schema.json"), []byte(`{"schema_version":1}`), 0o644))

	issues := checkAgentVerdicts(proj, repo)
	require.True(t, findIssue(issues, LevelWarn, "schema.json has no `verdicts` key"))
}

// The mirror of the above: once schema.json carries a `verdicts` key
// (redeployed via `zprof apply`), the warning must not fire.
func TestCheckAgentVerdicts_SchemaJSONWithVerdictsKeyIsSilent(t *testing.T) {
	repo := writeVerdictsRepoFixture(t, smallVerdictsRegistry)
	proj := t.TempDir()
	agentsDir := filepath.Join(proj, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "implementer.md"),
		[]byte("---\nname: implementer\nreturn_format: |\n  verdict: done|blocked|failed\n---\nbody\n"), 0o644))
	agentlogDir := filepath.Join(proj, ".agentlog")
	require.NoError(t, os.MkdirAll(agentlogDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentlogDir, "schema.json"), []byte(`{"schema_version":1,"verdicts":{"version":1}}`), 0o644))

	require.Empty(t, checkAgentVerdicts(proj, repo))
}

// --- guard hooks in settings.local.json (guard stage, ADR 0009) ----------

func guardHookJSON(events ...string) string {
	entries := make([]string, len(events))
	for i, e := range events {
		entries[i] = fmt.Sprintf(`"%s": [{"hooks": [{"type": "command", "command": "test -x zprof-guard.py && zprof-guard.py %s || true"}]}]`, e, e)
	}
	return "{\n  \"hooks\": {\n    " + strings.Join(entries, ",\n    ") + "\n  }\n}"
}

func TestCheckGuardHooks(t *testing.T) {
	cases := []struct {
		name            string
		setup           func(t *testing.T, dir string)
		wantEmpty       bool
		wantLevel       string
		wantContains    string
		wantNotContains string
	}{
		{
			// Gated on the guard script being deployed — a project that
			// never applied a guard-shipping base profile has nothing for
			// hooks to call.
			name:      "silent without guard script",
			wantEmpty: true,
		},
		{
			name: "warns when settings missing",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "zprof-guard.py"), []byte("#!/usr/bin/env python3\n"), 0o755))
			},
			wantLevel:    LevelWarn,
			wantContains: "guard hooks missing for PreToolUse, SubagentStop",
		},
		{
			name: "warns on partial install",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "zprof-guard.py"), []byte("#!/usr/bin/env python3\n"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "settings.local.json"),
					[]byte(guardHookJSON("PreToolUse")), 0o644))
			},
			wantLevel:       LevelWarn,
			wantContains:    "SubagentStop",
			wantNotContains: "PreToolUse,",
		},
		{
			name: "silent when fully installed",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "zprof-guard.py"), []byte("#!/usr/bin/env python3\n"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "settings.local.json"),
					[]byte(guardHookJSON("PreToolUse", "SubagentStop")), 0o644))
			},
			wantEmpty: true,
		},
		{
			name: "warns on malformed JSON",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "zprof-guard.py"), []byte("#!/usr/bin/env python3\n"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "settings.local.json"), []byte("{not valid json"), 0o644))
			},
			wantLevel:    LevelWarn,
			wantContains: "failed to parse",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.setup != nil {
				tc.setup(t, dir)
			}
			issues := checkGuardHooks(dir)
			if tc.wantEmpty {
				require.Empty(t, issues)
				return
			}
			require.Len(t, issues, 1)
			require.Equal(t, tc.wantLevel, issues[0].Level)
			require.Contains(t, issues[0].Message, tc.wantContains)
			if tc.wantNotContains != "" {
				require.NotContains(t, issues[0].Message, tc.wantNotContains)
			}
		})
	}
}

// --- guard.json (guard stage, ADR 0009) -----------------------------------

func TestCheckGuardConfig(t *testing.T) {
	cases := []struct {
		name         string
		setup        func(t *testing.T, dir string)
		wantEmpty    bool
		wantContains string
	}{
		{
			name:         "warns when missing",
			wantContains: "missing",
		},
		{
			name: "warns on malformed JSON",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "guard.json"), []byte("{not valid json"), 0o644))
			},
			wantContains: "failed to parse",
		},
		{
			name: "warns on empty rules",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "guard.json"), []byte(`{"rules": []}`), 0o644))
			},
			wantContains: "no rules",
		},
		{
			name: "silent when populated",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "guard.json"),
					[]byte(`{"rules": [{"id": "force_push", "tools": ["Bash"]}]}`), 0o644))
			},
			wantEmpty: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.setup != nil {
				tc.setup(t, dir)
			}
			issues := checkGuardConfig(dir)
			if tc.wantEmpty {
				require.Empty(t, issues)
				return
			}
			require.Len(t, issues, 1)
			require.Equal(t, LevelWarn, issues[0].Level)
			require.True(t, strings.HasPrefix(issues[0].Message, "guard.json"))
			require.Contains(t, issues[0].Message, tc.wantContains)
		})
	}
}

// --- permissions.deny vs guard.json's permissions_deny (guard stage) -----

func TestCheckPermissionsDeny(t *testing.T) {
	cases := []struct {
		name            string
		setup           func(t *testing.T, dir string)
		wantEmpty       bool
		wantContains    string
		wantNotContains string
	}{
		{
			name:      "silent when guard config missing",
			wantEmpty: true,
		},
		{
			name: "silent when permissions_deny empty",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "guard.json"), []byte(`{"permissions_deny": []}`), 0o644))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "settings.local.json"), []byte(`{"permissions": {"deny": []}}`), 0o644))
			},
			wantEmpty: true,
		},
		{
			name: "warns on missing entries",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "guard.json"),
					[]byte(`{"permissions_deny": ["deny-a", "deny-b"]}`), 0o644))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "settings.local.json"),
					[]byte(`{"permissions": {"deny": ["deny-a"]}}`), 0o644))
			},
			wantContains:    "deny-b",
			wantNotContains: "deny-a",
		},
		{
			name: "warns when settings missing",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "guard.json"),
					[]byte(`{"permissions_deny": ["deny-a"]}`), 0o644))
			},
			wantContains: "deny-a",
		},
		{
			name: "silent when superset",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "guard.json"),
					[]byte(`{"permissions_deny": ["deny-a"]}`), 0o644))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "settings.local.json"),
					[]byte(`{"permissions": {"deny": ["deny-a", "deny-b"]}}`), 0o644))
			},
			wantEmpty: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.setup != nil {
				tc.setup(t, dir)
			}
			issues := checkPermissionsDeny(dir)
			if tc.wantEmpty {
				require.Empty(t, issues)
				return
			}
			require.Len(t, issues, 1)
			require.Equal(t, LevelWarn, issues[0].Level)
			require.Contains(t, issues[0].Message, tc.wantContains)
			if tc.wantNotContains != "" {
				require.NotContains(t, issues[0].Message, tc.wantNotContains)
			}
		})
	}
}

// --- checkGuardDeployment: single gate on guard.enabled (guard stage) ----

func TestCheckGuardDeployment(t *testing.T) {
	enabled := true
	disabled := false
	cases := []struct {
		name        string
		setup       func(t *testing.T, dir string)
		proj        *manifest.ProjectManifest
		wantLen     int
		wantLevel   string
		wantMessage string
	}{
		{
			name: "aggregates when guard nil",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "zprof-guard.py"), []byte("#!/usr/bin/env python3\n"), 0o755))
			},
			proj: &manifest.ProjectManifest{},
		},
		{
			name: "aggregates when explicitly enabled",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "zprof-guard.py"), []byte("#!/usr/bin/env python3\n"), 0o755))
			},
			proj: &manifest.ProjectManifest{Guard: &manifest.GuardConfig{Enabled: &enabled}},
		},
		{
			// zprof-guard.py deliberately absent and guard.json deliberately
			// malformed — if the three underlying checks still ran, they'd
			// each produce a warning. The gate must suppress all three, not
			// just skip adding its own info on top.
			name: "silences underlying checks when disabled",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "guard.json"), []byte("{not valid json"), 0o644))
			},
			proj:        &manifest.ProjectManifest{Guard: &manifest.GuardConfig{Enabled: &disabled}},
			wantLen:     1,
			wantLevel:   LevelInfo,
			wantMessage: "guard disabled by project config",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)
			issues := checkGuardDeployment(dir, tc.proj)
			if tc.wantLen > 0 {
				require.Len(t, issues, tc.wantLen)
				require.Equal(t, tc.wantLevel, issues[0].Level)
				require.Equal(t, tc.wantMessage, issues[0].Message)
				return
			}
			require.NotEmpty(t, issues)
			require.False(t, findIssue(issues, LevelInfo, "guard disabled by project config"))
		})
	}
}

// --- checkRoleResolution: subagent meta.json lookup -----------------------

func slugFor(projectDir string) string {
	return "-" + strings.ReplaceAll(strings.TrimPrefix(projectDir, "/"), "/", "-")
}

// satisfyNewGuardChecks writes the minimal fixtures needed so
// checkGuardDeployment/checkRoleResolution stay silent: a guard.json with a
// non-empty rules list (checkGuardConfig has no zprof-guard.py gate — see
// plan-issue-29.md item 4 — so it fires on any project missing it), a HOME
// override, and a matching subagent meta.json with agentType set. Used by
// pre-existing "no issues" tests written before these two checks existed,
// so their fixtures never accounted for either.
func satisfyNewGuardChecks(t *testing.T, projectDir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, ".claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".claude", "guard.json"),
		[]byte(`{"rules": [{"id": "x", "tools": ["Bash"]}]}`), 0o644))

	home := t.TempDir()
	t.Setenv("HOME", home)
	metaDir := filepath.Join(home, ".claude", "projects", slugFor(projectDir), "session-1", "subagents")
	require.NoError(t, os.MkdirAll(metaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(metaDir, "agent-1.meta.json"),
		[]byte(`{"agentType": "implementer"}`), 0o644))
}

func TestCheckRoleResolution(t *testing.T) {
	cases := []struct {
		name string
		// setHome false simulates HOME unset (empty string), which must
		// short-circuit before any glob — there's nothing to resolve
		// against, and that must not be reported as a defect.
		setHome bool
		// setup receives the metaDir checkRoleResolution will glob and may
		// leave it never created (e.g. "no projects dir at all").
		setup       func(t *testing.T, metaDir string)
		wantEmpty   bool
		wantLevel   string
		wantMessage string // exact match; empty skips the exact check
	}{
		{
			name:        "info when no projects dir at all",
			setHome:     true,
			wantLevel:   LevelInfo,
			wantMessage: "role resolution unverified: no subagent meta found yet",
		},
		{
			name:    "silent when agentType found",
			setHome: true,
			setup: func(t *testing.T, metaDir string) {
				require.NoError(t, os.MkdirAll(metaDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(metaDir, "agent-1.meta.json"),
					[]byte(`{"agentType": "implementer"}`), 0o644))
			},
			wantEmpty: true,
		},
		{
			name:    "info when meta lacks agentType",
			setHome: true,
			setup: func(t *testing.T, metaDir string) {
				require.NoError(t, os.MkdirAll(metaDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(metaDir, "agent-1.meta.json"),
					[]byte(`{"model": "sonnet"}`), 0o644))
			},
			wantLevel: LevelInfo,
		},
		{
			name:    "silent when one of many meta has agentType",
			setHome: true,
			setup: func(t *testing.T, metaDir string) {
				require.NoError(t, os.MkdirAll(metaDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(metaDir, "agent-1.meta.json"),
					[]byte(`{"model": "sonnet"}`), 0o644))
				require.NoError(t, os.WriteFile(filepath.Join(metaDir, "agent-2.meta.json"),
					[]byte(`{"agentType": "tester"}`), 0o644))
			},
			wantEmpty: true,
		},
		{
			name:      "silent when HOME unset",
			setHome:   false,
			wantEmpty: true,
		},
		{
			// A directory named *.meta.json matches the glob but fails
			// os.ReadFile, exercising the read-error continue branch — the
			// function still falls through to the "unverified" info once
			// every match has failed to yield an agentType.
			name:    "skips unreadable meta file",
			setHome: true,
			setup: func(t *testing.T, metaDir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(metaDir, "agent-1.meta.json"), 0o755))
			},
			wantLevel:   LevelInfo,
			wantMessage: "role resolution unverified: no subagent meta found yet",
		},
		{
			// A *.meta.json file that isn't valid JSON is skipped via
			// continue rather than failing the whole check — exercising the
			// json.Unmarshal error branch — and a later, valid file with
			// agentType is still found.
			name:    "skips malformed meta JSON then finds later match",
			setHome: true,
			setup: func(t *testing.T, metaDir string) {
				require.NoError(t, os.MkdirAll(metaDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(metaDir, "agent-1.meta.json"), []byte("{not valid json"), 0o644))
				require.NoError(t, os.WriteFile(filepath.Join(metaDir, "agent-2.meta.json"),
					[]byte(`{"agentType": "implementer"}`), 0o644))
			},
			wantEmpty: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if !tc.setHome {
				t.Setenv("HOME", "")
			} else {
				home := t.TempDir()
				t.Setenv("HOME", home)
				if tc.setup != nil {
					tc.setup(t, filepath.Join(home, ".claude", "projects", slugFor(dir), "session-1", "subagents"))
				}
			}

			issues := checkRoleResolution(dir)
			if tc.wantEmpty {
				require.Empty(t, issues)
				return
			}
			require.Len(t, issues, 1)
			require.Equal(t, tc.wantLevel, issues[0].Level)
			if tc.wantMessage != "" {
				require.Equal(t, tc.wantMessage, issues[0].Message)
			}
		})
	}
}

// --- Diagnose(): guard checks are wired in ---------------------------------

// A project with no guard.yaml section and no zprof-guard.py/guard.json
// deployed still gets the guard.json-missing warning (checkGuardConfig has
// no deployment gate) and the role-resolution info — proof the two new
// lines in Diagnose() are actually reached, without regressing the count of
// issues the existing 19 checks already produce on this fixture.
func TestDiagnoseIncludesGuardAndRoleResolutionChecks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"), []byte("overlays: []\n"), 0o644))
	repo := t.TempDir()

	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)
	require.True(t, findIssue(issues, LevelWarn, "guard.json"))
	require.True(t, findIssue(issues, LevelInfo, "role resolution unverified"))
}

// --- checkNorthStarGate: gate has nothing to check without docs/NORTH_STAR.md (issue #60) ---

const northStarGateInfoMsg = "north-star gate has nothing to check; create docs/NORTH_STAR.md or disable the gate"

func writeNorthStarGateFile(t *testing.T, projectDir string) {
	t.Helper()
	gatesDir := filepath.Join(projectDir, ".claude", "agents", "gates")
	require.NoError(t, os.MkdirAll(gatesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gatesDir, "north-star-auditor.md"),
		[]byte("---\nname: north-star-auditor\n---\n"), 0o644))
}

func TestCheckNorthStarGate(t *testing.T) {
	cases := []struct {
		name      string
		setup     func(t *testing.T, projectDir string)
		proj      *manifest.ProjectManifest
		wantEmpty bool
	}{
		{
			name:      "gate absent, no manifest — no issue",
			setup:     func(t *testing.T, projectDir string) {},
			wantEmpty: true,
		},
		{
			name: "gate file present, docs/NORTH_STAR.md absent — info issue",
			setup: func(t *testing.T, projectDir string) {
				writeNorthStarGateFile(t, projectDir)
			},
			wantEmpty: false,
		},
		{
			name: "gate file present, docs/NORTH_STAR.md present — no issue",
			setup: func(t *testing.T, projectDir string) {
				writeNorthStarGateFile(t, projectDir)
				require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "docs"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(projectDir, "docs", "NORTH_STAR.md"), []byte("# North Star\n"), 0o644))
			},
			wantEmpty: true,
		},
		{
			name:      "no gate file, but manifest requests --with-gates and docs/NORTH_STAR.md absent — info issue",
			setup:     func(t *testing.T, projectDir string) {},
			proj:      &manifest.ProjectManifest{WithGates: true},
			wantEmpty: false,
		},
		{
			name:      "no gate file, manifest without --with-gates — no issue",
			setup:     func(t *testing.T, projectDir string) {},
			proj:      &manifest.ProjectManifest{},
			wantEmpty: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proj := t.TempDir()
			tc.setup(t, proj)
			issues := checkNorthStarGate(proj, tc.proj)
			if tc.wantEmpty {
				require.Empty(t, issues)
				return
			}
			require.Len(t, issues, 1)
			require.Equal(t, LevelInfo, issues[0].Level)
			require.Equal(t, northStarGateInfoMsg, issues[0].Message)
		})
	}
}

// TestDiagnoseNorthStarGateTelemetryOnly proves the check also fires through
// the telemetry-only path (diagnoseTelemetryOnly, issue #64) — zprof's own
// repo checkout runs through exactly this path, no .zprof.yaml, which is how
// issue #60 was discovered live in zprof's own dev loop.
func TestDiagnoseNorthStarGateTelemetryOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	proj := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(proj, ".claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".claude", "zprof-collect.py"), []byte("#!/usr/bin/env python3\n"), 0o755))
	writeNorthStarGateFile(t, proj)

	repo := t.TempDir()
	issues, err := Diagnose(proj, repo)
	require.NoError(t, err)
	require.True(t, findIssue(issues, LevelInfo, northStarGateInfoMsg))
}
