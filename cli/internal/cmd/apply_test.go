// cli/internal/cmd/apply_test.go
package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestApplyTelemetryOnlyArgs is table-driven over the --telemetry-only /
// overlay-argument interactions from ADR 0001: the flag makes overlay
// arguments an error (mutual exclusion) and, without the flag, at least one
// overlay is still required (behavior unchanged).
func TestApplyTelemetryOnlyArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr bool
		errSub  string
	}{
		{
			name:    "telemetry-only with overlay args is rejected",
			args:    []string{"--telemetry-only", "ios-swift"},
			wantErr: true,
			errSub:  "--telemetry-only",
		},
		{
			name:    "no args and no flag still requires an overlay",
			args:    []string{},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewApplyCmd()
			c.SilenceUsage = true
			c.SilenceErrors = true
			c.SetArgs(tc.args)
			err := c.Execute()
			if tc.wantErr {
				require.Error(t, err)
				if tc.errSub != "" {
					require.Contains(t, err.Error(), tc.errSub)
				}
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestApplyTelemetryOnlyDeploysWithoutOverlay(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)
	profilesDir := filepath.Join(root, "profiles")

	proj := t.TempDir()
	origCwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(proj))
	t.Cleanup(func() { require.NoError(t, os.Chdir(origCwd)) })

	t.Setenv("ZPROF_REPO", profilesDir)

	c := NewApplyCmd()
	c.SetArgs([]string{"--telemetry-only"})
	require.NoError(t, c.Execute())

	deployed, err := os.ReadFile(filepath.Join(proj, ".claude", "zprof-collect.py"))
	require.NoError(t, err)
	source, err := os.ReadFile(filepath.Join(profilesDir, "base", "zprof-collect.py"))
	require.NoError(t, err)
	require.Equal(t, string(source), string(deployed), "deployed collector must match profiles/base source")

	_, err = os.Stat(filepath.Join(proj, ".claude", "settings.local.json"))
	require.NoError(t, err, "telemetry hooks must be written")

	_, err = os.Stat(filepath.Join(proj, ".zprof.yaml"))
	require.True(t, os.IsNotExist(err), ".zprof.yaml must not be created by --telemetry-only")
	_, err = os.Stat(filepath.Join(proj, ".claude", "agents"))
	require.True(t, os.IsNotExist(err), "agents must not be written by --telemetry-only")
	_, err = os.Stat(filepath.Join(proj, "CLAUDE.md"))
	require.True(t, os.IsNotExist(err), "CLAUDE.md must not be touched by --telemetry-only")
}

func TestApplyTelemetryOnlyDryRunWritesNothing(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)
	profilesDir := filepath.Join(root, "profiles")

	proj := t.TempDir()
	origCwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(proj))
	t.Cleanup(func() { require.NoError(t, os.Chdir(origCwd)) })

	t.Setenv("ZPROF_REPO", profilesDir)

	c := NewApplyCmd()
	c.SetArgs([]string{"--telemetry-only", "--dry-run"})
	require.NoError(t, c.Execute())

	_, err = os.Stat(filepath.Join(proj, ".claude"))
	require.True(t, os.IsNotExist(err), "--dry-run must not write any files")
}
