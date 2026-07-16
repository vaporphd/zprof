package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alcherk/zprof/internal/managed"
	"github.com/alcherk/zprof/internal/manifest"
	"github.com/alcherk/zprof/internal/overlay"
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
		".claude/agents/dev-orchestrator.md",
		".claude/agents/exploratory-orchestrator.md",
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

	// Assert .gitignore has thoughts/
	gi, _ := os.ReadFile(filepath.Join(proj, ".gitignore"))
	require.Contains(t, string(gi), "thoughts/")

	// Assert AGENT_LOOP.md has the base loop template composed in
	// (base/loop-templates/dev-pipeline.md), not just the overlay's own
	// loop block.
	loop, _ := os.ReadFile(filepath.Join(proj, "AGENT_LOOP.md"))
	require.Contains(t, string(loop), "следующая задача")
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
