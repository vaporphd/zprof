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

func TestCarryOverFrom_KeepsRunner(t *testing.T) {
	prev := &ProjectManifest{Runner: &RunnerConfig{MaxDispatches: 20}}
	m := &ProjectManifest{}
	m.CarryOverFrom(prev)
	require.Same(t, prev.Runner, m.Runner)
}

func TestRunnerMaxDispatches(t *testing.T) {
	tests := []struct {
		name   string
		runner *RunnerConfig
		audit  *AuditConfig
		want   int
	}{
		{
			name: "neither set falls back to default",
			want: defaultRunnerMaxDispatches,
		},
		{
			name:  "only audit.max_dispatches set",
			audit: &AuditConfig{MaxDispatches: 9},
			want:  9,
		},
		{
			name:   "only runner.max_dispatches set",
			runner: &RunnerConfig{MaxDispatches: 20},
			want:   20,
		},
		{
			name:   "both set, runner larger wins",
			runner: &RunnerConfig{MaxDispatches: 20},
			audit:  &AuditConfig{MaxDispatches: 9},
			want:   20,
		},
		{
			name:   "both set, audit larger wins",
			runner: &RunnerConfig{MaxDispatches: 5},
			audit:  &AuditConfig{MaxDispatches: 12},
			want:   12,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &ProjectManifest{Runner: tt.runner, Audit: tt.audit}
			require.Equal(t, tt.want, m.RunnerMaxDispatches())
		})
	}
}
