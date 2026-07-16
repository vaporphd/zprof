package overlay

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadBase(t *testing.T) {
	b, err := LoadBase(filepath.Join("..", "..", "testdata", "repo", "base"))
	require.NoError(t, err)
	require.Equal(t, "base", b.Manifest.Name)
	require.Contains(t, b.Agents, "planner")
	require.Contains(t, b.Agents["planner"], "Планировщик")
	require.Contains(t, b.Workflows, "dev-pipeline")
	require.Contains(t, b.StateTemplates, "todo")
	require.Contains(t, b.Router, "Agent loop router")
}

func TestLoadOverlay(t *testing.T) {
	o, err := LoadOverlay(filepath.Join("..", "..", "testdata", "repo", "overlays", "fake-ios"))
	require.NoError(t, err)
	require.Equal(t, "fake-ios", o.Manifest.Name)
	require.NotNil(t, o.Detect)
	require.Contains(t, o.Agents, "architect")
	require.NotEmpty(t, o.LoopMD)
	require.NotEmpty(t, o.ClaudeBlock)
}

func TestNamespaceAgent(t *testing.T) {
	require.Equal(t, "architect-ios", NamespaceAgent("architect", "ios-swift"))
	require.Equal(t, "architect-py", NamespaceAgent("architect", "backend-python"))
	require.Equal(t, "architect-web", NamespaceAgent("architect", "frontend-web"))
	require.Equal(t, "architect-macho", NamespaceAgent("architect", "re-macho"))
}
