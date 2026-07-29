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

// Оверлей без `executing:` и без implementer'а (процессные оверлеи вроде
// issue-loop-github-strict, read-only вроде re-macho) владельца файлов не
// имеет. Строку не выводим вовсе: подставить туда `task-runner` значит
// объявить владельцем файлов агента, чей промпт запрещает ему писать код,
// — а он эту таблицу читает.
func TestExecutingTableOmitsOverlayWithoutFileOwner(t *testing.T) {
	opts := ApplyOpts{
		Base:    &overlay.Base{Manifest: &manifest.OverlayManifest{Name: "base"}},
		Project: &manifest.ProjectManifest{},
		Overlays: []*overlay.Overlay{
			{Manifest: &manifest.OverlayManifest{Name: "issue-loop-github-strict"}, Agents: map[string]string{
				"pr-shepherd": "---\nname: pr-shepherd\n---\n",
			}},
		},
	}

	got := buildExecutingTable(opts)

	require.NotContains(t, got, "task-runner")
	require.NotContains(t, got, "issue-loop-github-strict")
	// Заголовок остаётся — секция управляемая, её отсутствие сломало бы merge.
	require.Contains(t, got, "## Executing")
}

// А оверлей с implementer'ом строку по-прежнему получает.
func TestExecutingTableKeepsImplementerRow(t *testing.T) {
	opts := ApplyOpts{
		Base:    &overlay.Base{Manifest: &manifest.OverlayManifest{Name: "base"}},
		Project: &manifest.ProjectManifest{},
		Overlays: []*overlay.Overlay{
			{Manifest: &manifest.OverlayManifest{Name: "systems-rust"}, Agents: map[string]string{
				"implementer": "---\nname: implementer\n---\n",
			}},
		},
	}

	require.Contains(t, buildExecutingTable(opts), "| implementer |")
}

// Преамбула стоп-листа обязана связывать обоих акторов: в списке есть
// force-push и удаление веток — операции main-сессии, а не раннера.
func TestStopListPreambleBindsBothActors(t *testing.T) {
	opts := ApplyOpts{
		Base: &overlay.Base{Manifest: &manifest.OverlayManifest{
			Name:     "base",
			StopList: []string{"force-push, rebase или amend опубликованной ветки"},
		}},
		Project:  &manifest.ProjectManifest{},
		Overlays: []*overlay.Overlay{{Manifest: &manifest.OverlayManifest{Name: "ios-swift"}}},
	}

	got := buildStopListBlock(opts)

	require.Contains(t, got, "task-runner")
	require.Contains(t, got, "Main-сессия")
	require.Contains(t, got, "force-push")
}
