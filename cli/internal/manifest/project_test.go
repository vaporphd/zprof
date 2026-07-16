package manifest

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadProjectManifest(t *testing.T) {
	m, err := LoadProject(filepath.Join("..", "..", "testdata", "projects", "multi-stack.zprof.yaml"))
	require.NoError(t, err)
	require.Equal(t, []string{"backend-python", "frontend-web"}, m.Overlays)
	require.Equal(t, "ru", m.Language)
	require.Equal(t, "opus-1m", m.ModelOverrides["architect-py"])
	require.Equal(t, "planner-strict", m.AgentOverrides["planner"])
}

func TestSaveAndReloadProjectManifest(t *testing.T) {
	m := &ProjectManifest{
		Overlays:  []string{"ios-swift"},
		Language:  "ru",
		WithGates: true,
	}
	p := filepath.Join(t.TempDir(), ".zprof.yaml")
	require.NoError(t, m.Save(p))

	m2, err := LoadProject(p)
	require.NoError(t, err)
	require.Equal(t, m.Overlays, m2.Overlays)
	require.True(t, m2.WithGates)
}

func TestResolvedModelUsesOverride(t *testing.T) {
	m := &ProjectManifest{ModelOverrides: map[string]string{"architect": "opus-1m"}}
	got, err := m.ResolvedModel("architect")
	require.NoError(t, err)
	require.Equal(t, "claude-opus-4-7[1m]", got)
}

func TestResolvedModelReturnsErrorWhenNoOverride(t *testing.T) {
	m := &ProjectManifest{ModelOverrides: map[string]string{}}
	_, err := m.ResolvedModel("architect")
	require.ErrorIs(t, err, ErrNoOverride)
}
