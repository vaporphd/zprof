package manifest

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadOverlayValid(t *testing.T) {
	m, err := LoadOverlay(filepath.Join("..", "..", "testdata", "overlays", "valid-manifest.yaml"))
	require.NoError(t, err)
	require.Equal(t, "ios-swift", m.Name)
	require.Equal(t, "0.1.0", m.Version)
	require.Equal(t, "dev-pipeline", m.LoopTemplate)
	require.Contains(t, m.Roles, "architect")
	require.Contains(t, m.ToolAgents, "xcode-runner")
}

func TestLoadOverlayInvalidEmptyName(t *testing.T) {
	_, err := LoadOverlay(filepath.Join("..", "..", "testdata", "overlays", "invalid-empty-name.yaml"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "name is required")
}

func TestLoadOverlayInvalidLoopTemplate(t *testing.T) {
	_, err := LoadOverlay(filepath.Join("..", "..", "testdata", "overlays", "invalid-loop.yaml"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "loop_template must be one of: dev-pipeline, exploratory")
}

// TestLoadOverlayEmptyLoopTemplateAllowed guards the base manifest case: base
// doesn't consume loop_template (apply engine only reads it for real overlays),
// so an empty value must be accepted rather than fail validation.
func TestLoadOverlayEmptyLoopTemplateAllowed(t *testing.T) {
	m, err := LoadOverlay(filepath.Join("..", "..", "testdata", "overlays", "no-loop-template.yaml"))
	require.NoError(t, err)
	require.Equal(t, "base", m.Name)
	require.Equal(t, "", m.LoopTemplate)
}
