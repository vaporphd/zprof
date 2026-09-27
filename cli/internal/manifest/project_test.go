package manifest

import (
	"os"
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

func TestLoadProjectManifest_ScoreSection(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".zprof.yaml")
	require.NoError(t, os.WriteFile(p, []byte("overlays: [x]\nscore:\n  enabled: false\n  weights: {P1: 30}\n  thresholds: {ideal: 90}\n"), 0o644))
	m, err := LoadProject(p)
	require.NoError(t, err)
	require.NotNil(t, m.Score)
	require.NotNil(t, m.Score.Enabled)
	require.False(t, *m.Score.Enabled)
	require.Equal(t, 30.0, m.Score.Weights["P1"])
	require.Equal(t, 90, m.Score.Thresholds.Ideal)
	require.Equal(t, 0, m.Score.Thresholds.Solid)
}

func TestCarryOverFrom_KeepsScore(t *testing.T) {
	enabled := false
	prev := &ProjectManifest{Score: &ScoreConfig{Enabled: &enabled}}
	m := &ProjectManifest{}
	m.CarryOverFrom(prev)
	require.Same(t, prev.Score, m.Score)
}
