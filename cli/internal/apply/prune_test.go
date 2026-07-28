package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeAgentFile(t *testing.T, dir, name string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	p := filepath.Join(dir, name+".md")
	require.NoError(t, os.WriteFile(p, []byte("---\nname: "+name+"\nmodel: sonnet\n---\nbody\n"), 0o644))
	return p
}

// Агент, который zprof писал раньше и больше не поставляет, удаляется, а
// его содержимое сохраняется в .bak.
func TestPruneRemovesOrphanWrittenByZprof(t *testing.T) {
	dir := t.TempDir()
	p := writeAgentFile(t, dir, "legacy-agent")

	removed, err := PruneOrphanAgents(dir, []string{"legacy-agent", "planner"}, []string{"planner"})
	require.NoError(t, err)
	require.Equal(t, []string{"legacy-agent"}, removed)
	require.NoFileExists(t, p)

	baks, err := filepath.Glob(filepath.Join(dir, "legacy-agent.md.zprof.bak-*"))
	require.NoError(t, err)
	require.Len(t, baks, 1, "удаление должно оставить ровно один бэкап")
}

// Агент, которого zprof никогда не писал, не трогается — это файл пользователя.
func TestPruneKeepsUserAuthoredAgent(t *testing.T) {
	dir := t.TempDir()
	p := writeAgentFile(t, dir, "my-custom-agent")

	removed, err := PruneOrphanAgents(dir, []string{"planner"}, []string{"planner"})
	require.NoError(t, err)
	require.Empty(t, removed)
	require.FileExists(t, p)
}

// Упразднённые имена удаляются даже когда previous пуст — это проекты,
// применённые до появления managed_agents.
func TestPruneRemovesRetiredAgentWithoutTrackingList(t *testing.T) {
	dir := t.TempDir()
	orch := writeAgentFile(t, dir, "dev-orchestrator")
	expl := writeAgentFile(t, dir, "exploratory-orchestrator")

	removed, err := PruneOrphanAgents(dir, nil, []string{"planner", "task-runner"})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"dev-orchestrator", "exploratory-orchestrator"}, removed)
	require.NoFileExists(t, orch)
	require.NoFileExists(t, expl)
}

// Агент, присутствующий в текущем составе, не удаляется, даже если он же
// числится в previous.
func TestPruneKeepsStillLiveAgent(t *testing.T) {
	dir := t.TempDir()
	p := writeAgentFile(t, dir, "planner")

	removed, err := PruneOrphanAgents(dir, []string{"planner"}, []string{"planner"})
	require.NoError(t, err)
	require.Empty(t, removed)
	require.FileExists(t, p)
}

// Отсутствие файла на диске — не ошибка: запись могла быть удалена руками.
func TestPruneSkipsMissingFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(dir, 0o755))

	removed, err := PruneOrphanAgents(dir, []string{"ghost"}, nil)
	require.NoError(t, err)
	require.Empty(t, removed)
}
