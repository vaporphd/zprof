// cli/internal/cmd/agents_test.go
package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alcherk/zprof/internal/manifest"
	"github.com/stretchr/testify/require"
)

func TestSetModelOverride(t *testing.T) {
	dir := t.TempDir()
	base := &manifest.ProjectManifest{Overlays: []string{"ios-swift"}}
	require.NoError(t, base.Save(filepath.Join(dir, ".zprof.yaml")))

	origCwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { os.Chdir(origCwd) })

	c := NewAgentsCmd()
	c.SetArgs([]string{"set", "architect", "--model", "opus-1m"})
	require.NoError(t, c.Execute())

	m, err := manifest.LoadProject(filepath.Join(dir, ".zprof.yaml"))
	require.NoError(t, err)
	require.Equal(t, "opus-1m", m.ModelOverrides["architect"])
}

func TestSetAgentOverride(t *testing.T) {
	dir := t.TempDir()
	base := &manifest.ProjectManifest{Overlays: []string{"ios-swift"}}
	require.NoError(t, base.Save(filepath.Join(dir, ".zprof.yaml")))

	origCwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { os.Chdir(origCwd) })

	c := NewAgentsCmd()
	c.SetArgs([]string{"set", "planner", "planner-strict"})
	require.NoError(t, c.Execute())

	m, err := manifest.LoadProject(filepath.Join(dir, ".zprof.yaml"))
	require.NoError(t, err)
	require.Equal(t, "planner-strict", m.AgentOverrides["planner"])
}

func TestSetRejectsUnknownModelAlias(t *testing.T) {
	dir := t.TempDir()
	base := &manifest.ProjectManifest{Overlays: []string{"ios-swift"}}
	require.NoError(t, base.Save(filepath.Join(dir, ".zprof.yaml")))

	origCwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { os.Chdir(origCwd) })

	c := NewAgentsCmd()
	c.SetArgs([]string{"set", "architect", "--model", "not-a-real-model"})
	require.Error(t, c.Execute())

	m, err := manifest.LoadProject(filepath.Join(dir, ".zprof.yaml"))
	require.NoError(t, err)
	require.Empty(t, m.ModelOverrides)
}
