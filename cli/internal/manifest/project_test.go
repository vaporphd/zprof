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

// TestLoadProjectManifest_GuardSection is table-driven over the shapes
// GuardConfig must parse — mirrors TestLoadProjectManifest_ScoreSection.
func TestLoadProjectManifest_GuardSection(t *testing.T) {
	cases := []struct {
		name  string
		yaml  string
		check func(t *testing.T, g *GuardConfig)
	}{
		{
			name: "no guard section at all",
			yaml: "overlays: [x]\n",
			check: func(t *testing.T, g *GuardConfig) {
				require.Nil(t, g)
			},
		},
		{
			name: "enabled: false",
			yaml: "overlays: [x]\nguard:\n  enabled: false\n",
			check: func(t *testing.T, g *GuardConfig) {
				require.NotNil(t, g)
				require.NotNil(t, g.Enabled)
				require.False(t, *g.Enabled)
				require.False(t, g.IsEnabled())
			},
		},
		{
			name: "full section",
			yaml: "overlays: [x]\nguard:\n" +
				"  extra_deny_bash: [\"rm -rf /\"]\n" +
				"  merge_roles: [pr-shepherd, architect]\n" +
				"  readonly_roles: [explorer]\n" +
				"  allow_write_outside: [\"/tmp/scratch\"]\n" +
				"  exempt_roles:\n    force_push: [architect]\n",
			check: func(t *testing.T, g *GuardConfig) {
				require.NotNil(t, g)
				require.True(t, g.IsEnabled(), "nil Enabled means on")
				require.Equal(t, []string{"rm -rf /"}, g.ExtraDenyBash)
				require.Equal(t, []string{"pr-shepherd", "architect"}, g.MergeRoles)
				require.Equal(t, []string{"explorer"}, g.ReadonlyRoles)
				require.Equal(t, []string{"/tmp/scratch"}, g.AllowWriteOutside)
				require.Equal(t, []string{"architect"}, g.ExemptRoles["force_push"])
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, ".zprof.yaml")
			require.NoError(t, os.WriteFile(p, []byte(tc.yaml), 0o644))
			m, err := LoadProject(p)
			require.NoError(t, err)
			tc.check(t, m.Guard)
		})
	}
}

func TestGuardConfig_IsEnabled_NilIsOn(t *testing.T) {
	var g *GuardConfig
	require.True(t, g.IsEnabled())
}

func TestCarryOverFrom_KeepsGuard(t *testing.T) {
	prev := &ProjectManifest{Guard: &GuardConfig{ReadonlyRoles: []string{"explorer"}}}
	m := &ProjectManifest{}
	m.CarryOverFrom(prev)
	require.Same(t, prev.Guard, m.Guard)
}

func TestCarryOverFrom_DoesNotOverwriteExplicitGuard(t *testing.T) {
	prev := &ProjectManifest{Guard: &GuardConfig{ReadonlyRoles: []string{"explorer"}}}
	fresh := &GuardConfig{ReadonlyRoles: []string{"architect"}}
	m := &ProjectManifest{Guard: fresh}
	m.CarryOverFrom(prev)
	require.Same(t, fresh, m.Guard)
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
