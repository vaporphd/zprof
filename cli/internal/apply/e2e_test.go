package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vaporphd/zprof/internal/managed"
	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/overlay"
	"github.com/stretchr/testify/require"
)

func copyDir(t *testing.T, src, dst string) {
	require.NoError(t, filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	}))
}

func TestE2E_IOSApplyOnFixture(t *testing.T) {
	// Locate repo root (three dirs up from cli/internal/apply).
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)
	profilesDir := filepath.Join(root, "profiles")
	fixture := filepath.Join(root, "cli", "testdata", "projects", "smoke-ios")

	proj := t.TempDir()
	copyDir(t, fixture, proj)

	base, err := overlay.LoadBase(filepath.Join(profilesDir, "base"))
	require.NoError(t, err)
	ios, err := overlay.LoadOverlay(filepath.Join(profilesDir, "overlays", "ios-swift"))
	require.NoError(t, err)

	_, err = Apply(ApplyOpts{
		ProjectDir: proj, Base: base, Overlays: []*overlay.Overlay{ios},
		Project:   &manifest.ProjectManifest{Overlays: []string{"ios-swift"}, Language: "ru"},
		MergeMode: managed.ModeOverwrite,
	})
	require.NoError(t, err)

	// Assert expected files.
	//
	// NOTE: default Project.WithGates is false, so base/agents/gates/*.md
	// (north-star-auditor, evidence-auditor, plan-reviewer) must NOT be
	// written — see TestE2E_IOSApplyWithGates below for the WithGates:true
	// case where they ARE expected.
	for _, f := range []string{
		".claude/agents/planner.md",
		".claude/agents/docs-writer.md",
		".claude/agents/task-runner.md",
		".claude/agents/architect.md",
		".claude/agents/implementer.md",
		".claude/agents/tester.md",
		".claude/agents/bug-hunter.md",
		".claude/agents/reviewer.md",
		".claude/agents/refactor-agent.md",
		".claude/agents/explorer.md",
		".claude/agents/xcode-runner.md",
		".claude/agents/spm-manager.md",
		".claude/agents/simulator-driver.md",
		".claude/agents/testflight-shipper.md",
		".claude/agents/xcodegen-driver.md",
		".claude/agents/swiftlint-checker.md",
		"CLAUDE.md",
		"AGENT_LOOP.md",
		"todo.md",
		"lessons.md",
		"followup.md",
		"docs/PROJECT_SPEC.md",
		"docs/adr/0000-template.md",
		".zprof.yaml",
		".gitignore",
		"workflows/dev-pipeline.md",
	} {
		require.FileExists(t, filepath.Join(proj, f), "missing: %s", f)
	}

	for _, f := range []string{
		".claude/agents/gates/north-star-auditor.md",
		".claude/agents/gates/evidence-auditor.md",
		".claude/agents/gates/plan-reviewer.md",
	} {
		require.NoFileExists(t, filepath.Join(proj, f), "should be absent without --with-gates: %s", f)
	}

	// Assert agent model resolved (planner=sonnet → claude-sonnet-5)
	planner, _ := os.ReadFile(filepath.Join(proj, ".claude/agents/planner.md"))
	require.Contains(t, string(planner), "model: claude-sonnet-5")

	// Assert architect model resolved (opus → claude-opus-4-8)
	arch, _ := os.ReadFile(filepath.Join(proj, ".claude/agents/architect.md"))
	require.Contains(t, string(arch), "model: claude-opus-4-8")

	// Assert CLAUDE.md has ios-swift managed block
	claude, _ := os.ReadFile(filepath.Join(proj, "CLAUDE.md"))
	require.Contains(t, string(claude), "<!-- zprof:begin overlay=ios-swift block=stack-config -->")
	require.Contains(t, string(claude), "build_cmd:")

	// Assert CLAUDE.md has the auto-generated Consilium and Executing
	// tables, with at least one row for the ios-swift overlay (not
	// namespaced, since this is a single-overlay apply).
	require.Contains(t, string(claude), "## Consilium")
	require.Contains(t, string(claude), "## Executing")
	require.Contains(t, string(claude), "| implementer | implementer | ios-swift |")

	// Assert .gitignore has thoughts/
	gi, _ := os.ReadFile(filepath.Join(proj, ".gitignore"))
	require.Contains(t, string(gi), "thoughts/")

	// Assert AGENT_LOOP.md is now a thin router: only the base router
	// content, none of the workflow's own dispatch/trigger content that
	// used to be inlined via the old loop-template block.
	loop, _ := os.ReadFile(filepath.Join(proj, "AGENT_LOOP.md"))
	require.Contains(t, string(loop), "Контракт main-сессии")
	require.NotContains(t, string(loop), "Маршруты")
	require.NotContains(t, string(loop), "Trigger-фразы")

	// Assert workflows/dev-pipeline.md composes the base workflow content
	// (base/workflows/dev-pipeline.md) plus the ios-swift extension.
	wf, _ := os.ReadFile(filepath.Join(proj, "workflows", "dev-pipeline.md"))
	require.Contains(t, string(wf), "Маршруты")
	require.Contains(t, string(wf), "<!-- zprof:begin overlay=ios-swift block=workflow-extension -->")
}

// TestE2E_SecondApplyPrunesDroppedOverlayAgents is the regression for the
// dropped `managed_agents` carry-over: `zprof apply ios-swift
// backend-python` writes namespaced agents and records the roster, then
// `zprof apply ios-swift` must delete the backend-python half. Before the
// fix the second run built a fresh manifest that lost the roster, so prune
// saw previous=nil and every `*-py` agent stayed behind as a valid,
// dispatchable file that nothing would ever remove.
//
// The test walks the exact sequence cmd/apply.go performs — load the saved
// manifest, build a fresh one from the new flags, CarryOverFrom — so it
// fails if that call site regresses, not only if the engine does.
func TestE2E_SecondApplyPrunesDroppedOverlayAgents(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)
	profilesDir := filepath.Join(root, "profiles")

	proj := t.TempDir()
	copyDir(t, filepath.Join(root, "cli", "testdata", "projects", "smoke-ios"), proj)
	mfPath := filepath.Join(proj, ".zprof.yaml")

	base, err := overlay.LoadBase(filepath.Join(profilesDir, "base"))
	require.NoError(t, err)
	ios, err := overlay.LoadOverlay(filepath.Join(profilesDir, "overlays", "ios-swift"))
	require.NoError(t, err)
	py, err := overlay.LoadOverlay(filepath.Join(profilesDir, "overlays", "backend-python"))
	require.NoError(t, err)

	// First apply: two overlays, so every overlay agent is namespaced.
	first := &manifest.ProjectManifest{Overlays: []string{"ios-swift", "backend-python"}, Language: "ru"}
	_, err = Apply(ApplyOpts{
		ProjectDir: proj, Base: base, Overlays: []*overlay.Overlay{ios, py},
		Project: first, MergeMode: managed.ModeOverwrite,
	})
	require.NoError(t, err)

	pyAgents := []string{"implementer-py", "tester-py", "architect-py", "reviewer-py"}
	for _, n := range pyAgents {
		require.FileExists(t, filepath.Join(proj, ".claude", "agents", n+".md"))
	}

	saved, err := manifest.LoadProject(mfPath)
	require.NoError(t, err)
	require.Contains(t, saved.ManagedAgents, "implementer-py",
		"apply must persist the roster it wrote")

	// Second apply: ios-swift only, exactly as cmd/apply.go composes it.
	second := &manifest.ProjectManifest{Overlays: []string{"ios-swift"}, Language: "ru"}
	second.CarryOverFrom(saved)
	res, err := Apply(ApplyOpts{
		ProjectDir: proj, Base: base, Overlays: []*overlay.Overlay{ios},
		Project: second, MergeMode: managed.ModeOverwrite,
	})
	require.NoError(t, err)

	for _, n := range pyAgents {
		require.NoFileExists(t, filepath.Join(proj, ".claude", "agents", n+".md"),
			"orphan from the dropped overlay must be removed: %s", n)
		require.Contains(t, res.RemovedAgents, n)
		baks, globErr := filepath.Glob(filepath.Join(proj, ".claude", "agents", n+".md.zprof.bak-*"))
		require.NoError(t, globErr)
		require.Len(t, baks, 1, "every removal keeps a .bak: %s", n)
	}

	// The surviving overlay is now single, so its agents are un-namespaced;
	// the namespaced ios copies are orphans too and must be gone.
	require.FileExists(t, filepath.Join(proj, ".claude", "agents", "implementer.md"))
	require.NoFileExists(t, filepath.Join(proj, ".claude", "agents", "implementer-ios.md"))

	// And the roster on disk now describes only what the second apply wrote.
	after, err := manifest.LoadProject(mfPath)
	require.NoError(t, err)
	require.NotContains(t, after.ManagedAgents, "implementer-py")
	require.Contains(t, after.ManagedAgents, "implementer")
}

// User overrides must survive an apply the same way the roster does.
func TestCarryOverFromKeepsOverridesAndRoster(t *testing.T) {
	prev := &manifest.ProjectManifest{
		Overlays:       []string{"ios-swift", "backend-python"},
		ModelOverrides: map[string]string{"architect": "opus"},
		AgentOverrides: map[string]string{"tester": "tester-custom"},
		ManagedAgents:  []string{"implementer-py", "implementer-ios"},
	}
	next := &manifest.ProjectManifest{Overlays: []string{"ios-swift"}, Language: "ru"}
	next.CarryOverFrom(prev)

	require.Equal(t, prev.ModelOverrides, next.ModelOverrides)
	require.Equal(t, prev.AgentOverrides, next.AgentOverrides)
	require.Equal(t, prev.ManagedAgents, next.ManagedAgents)
	require.Equal(t, []string{"ios-swift"}, next.Overlays, "overlays come from the command line, not the old file")
}

func TestE2E_IOSApplyWithGates(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)
	profilesDir := filepath.Join(root, "profiles")
	fixture := filepath.Join(root, "cli", "testdata", "projects", "smoke-ios")

	proj := t.TempDir()
	copyDir(t, fixture, proj)

	base, err := overlay.LoadBase(filepath.Join(profilesDir, "base"))
	require.NoError(t, err)
	ios, err := overlay.LoadOverlay(filepath.Join(profilesDir, "overlays", "ios-swift"))
	require.NoError(t, err)

	_, err = Apply(ApplyOpts{
		ProjectDir: proj, Base: base, Overlays: []*overlay.Overlay{ios},
		Project:   &manifest.ProjectManifest{Overlays: []string{"ios-swift"}, Language: "ru", WithGates: true},
		MergeMode: managed.ModeOverwrite,
	})
	require.NoError(t, err)

	// With --with-gates, the gates/*.md agents ARE written.
	for _, f := range []string{
		".claude/agents/gates/north-star-auditor.md",
		".claude/agents/gates/evidence-auditor.md",
		".claude/agents/gates/plan-reviewer.md",
	} {
		require.FileExists(t, filepath.Join(proj, f), "missing with --with-gates: %s", f)
	}
}
