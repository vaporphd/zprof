package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vaporphd/zprof/internal/managed"
	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/overlay"
	"github.com/stretchr/testify/require"
)

func loadTestRepo(t *testing.T) (*overlay.Base, *overlay.Overlay) {
	repo := filepath.Join("..", "..", "testdata", "repo")
	b, err := overlay.LoadBase(filepath.Join(repo, "base"))
	require.NoError(t, err)
	o, err := overlay.LoadOverlay(filepath.Join(repo, "overlays", "fake-ios"))
	require.NoError(t, err)
	return b, o
}

func TestApplySingleOverlay(t *testing.T) {
	proj := t.TempDir()
	b, o := loadTestRepo(t)
	res, err := Apply(ApplyOpts{
		ProjectDir: proj,
		Base:       b,
		Overlays:   []*overlay.Overlay{o},
		Project:    &manifest.ProjectManifest{Overlays: []string{"fake-ios"}, Language: "ru"},
		MergeMode:  managed.ModeOverwrite,
	})
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(proj, ".claude", "agents", "planner.md"))
	require.FileExists(t, filepath.Join(proj, ".claude", "agents", "architect.md"))
	require.FileExists(t, filepath.Join(proj, "AGENT_LOOP.md"))
	require.FileExists(t, filepath.Join(proj, "CLAUDE.md"))
	require.FileExists(t, filepath.Join(proj, "todo.md"))
	require.FileExists(t, filepath.Join(proj, ".zprof.yaml"))

	claude, _ := os.ReadFile(filepath.Join(proj, "CLAUDE.md"))
	require.Contains(t, string(claude), "<!-- zprof:begin overlay=fake-ios block=stack-config -->")

	require.NotEmpty(t, res.CreatedAgents)
	require.NotEmpty(t, res.UpdatedFiles)
}

func TestApplyIdempotent(t *testing.T) {
	proj := t.TempDir()
	b, o := loadTestRepo(t)
	opts := ApplyOpts{ProjectDir: proj, Base: b, Overlays: []*overlay.Overlay{o},
		Project: &manifest.ProjectManifest{Overlays: []string{"fake-ios"}}, MergeMode: managed.ModeOverwrite}
	_, err := Apply(opts)
	require.NoError(t, err)
	claude1, _ := os.ReadFile(filepath.Join(proj, "CLAUDE.md"))
	_, err = Apply(opts)
	require.NoError(t, err)
	claude2, _ := os.ReadFile(filepath.Join(proj, "CLAUDE.md"))
	require.Equal(t, string(claude1), string(claude2))
}

func TestWorkflowFileComposesBaseAndOverlay(t *testing.T) {
	proj := t.TempDir()
	b, o := loadTestRepo(t)
	_, err := Apply(ApplyOpts{
		ProjectDir: proj,
		Base:       b,
		Overlays:   []*overlay.Overlay{o},
		Project:    &manifest.ProjectManifest{Overlays: []string{"fake-ios"}, Language: "ru"},
		MergeMode:  managed.ModeOverwrite,
	})
	require.NoError(t, err)

	wfPath := filepath.Join(proj, "workflows", "dev-pipeline.md")
	require.FileExists(t, wfPath)
	data, err := os.ReadFile(wfPath)
	require.NoError(t, err)
	content := string(data)

	// Base workflow block present with its own marker + content.
	require.Contains(t, content, "<!-- zprof:begin overlay=base block=workflow-dev-pipeline -->")
	require.Contains(t, content, "dev-pipeline template")

	// Overlay's workflow-extension block present with its own marker + content.
	require.Contains(t, content, "<!-- zprof:begin overlay=fake-ios block=workflow-extension -->")
	require.Contains(t, content, "loop.md")
}

func TestApplyMultiOverlayNamespacesAgents(t *testing.T) {
	// use fake-ios twice under two names to prove namespacing wiring
	proj := t.TempDir()
	b, o := loadTestRepo(t)
	// clone overlay with renamed manifest.Name
	o2 := *o
	m := *o.Manifest
	m.Name = "fake-py"
	o2.Manifest = &m
	_, err := Apply(ApplyOpts{
		ProjectDir: proj,
		Base:       b,
		Overlays:   []*overlay.Overlay{o, &o2},
		Project:    &manifest.ProjectManifest{Overlays: []string{"fake-ios", "fake-py"}},
		MergeMode:  managed.ModeOverwrite,
	})
	require.NoError(t, err)
	// architect should be namespaced when >1 overlay
	require.FileExists(t, filepath.Join(proj, ".claude", "agents", "architect-fake-ios.md"))
	require.FileExists(t, filepath.Join(proj, ".claude", "agents", "architect-fake-py.md"))
}

func TestBuildStopListBlockMergesBaseAndOverlays(t *testing.T) {
	opts := ApplyOpts{
		Base: &overlay.Base{
			Manifest: &manifest.OverlayManifest{
				Name:     "base",
				StopList: []string{"force-push в опубликованную ветку", "релиз, деплой, публикация пакета"},
			},
		},
		Overlays: []*overlay.Overlay{
			{Manifest: &manifest.OverlayManifest{
				Name:     "ios-swift",
				StopList: []string{"загрузка билда в TestFlight", "релиз, деплой, публикация пакета"},
			}},
		},
	}

	got := buildStopListBlock(opts)

	require.Contains(t, got, "## Stop list")
	require.Contains(t, got, "| force-push в опубликованную ветку | base |")
	require.Contains(t, got, "| загрузка билда в TestFlight | ios-swift |")
	// Дубль, объявленный и в base, и в overlay, рендерится один раз — с
	// источником base, потому что base идёт первым. Проверяем не просто
	// счётчик строки (он остался бы равен 1 и при развороте правила
	// «первый выигрывает» на «последний»), а именно то, какой источник
	// выжил: ряд с source=base должен присутствовать, ряд с
	// source=ios-swift для той же строки — отсутствовать.
	require.Equal(t, 1, strings.Count(got, "релиз, деплой, публикация пакета"))
	require.Contains(t, got, "| релиз, деплой, публикация пакета | base |")
	require.NotContains(t, got, "| релиз, деплой, публикация пакета | ios-swift |")
}

// Удаление файлов агентов — единственная деструктивная операция apply,
// поэтому она отчитывается именами, а не счётчиком.
func TestFormatRemovedAgentsNamesAndBackupHint(t *testing.T) {
	require.Empty(t, FormatRemovedAgents(nil))

	out := FormatRemovedAgents([]string{"tester-py", "implementer-py"})
	require.Contains(t, out, "Удалено агентов: 2")
	require.Contains(t, out, ".bak")
	// Имена отсортированы, чтобы вывод не прыгал между запусками.
	require.Contains(t, out, "implementer-py, tester-py")
}
