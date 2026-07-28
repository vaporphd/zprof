package apply

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/overlay"
)

func TestConsiliumListsTaskRunnerNotOrchestrators(t *testing.T) {
	opts := ApplyOpts{
		Base: &overlay.Base{
			Manifest: &manifest.OverlayManifest{Name: "base"},
			Agents: map[string]string{
				"task-runner": "---\nname: task-runner\nmodel: sonnet\n---\n",
				"planner":     "---\nname: planner\nmodel: sonnet\n---\n",
			},
		},
		Overlays: []*overlay.Overlay{
			{Manifest: &manifest.OverlayManifest{Name: "ios-swift"}, Agents: map[string]string{}},
		},
		Project: &manifest.ProjectManifest{},
	}

	got := buildConsiliumTable(opts)

	require.Contains(t, got, "| task-runner | task-runner | base |")
	require.NotContains(t, got, "dev-orchestrator")
	require.NotContains(t, got, "exploratory-orchestrator")
}

func TestIsKnownRoleAcceptsTaskRunner(t *testing.T) {
	require.True(t, IsKnownRole("task-runner"))
	require.False(t, IsKnownRole("dev-orchestrator"))
}
