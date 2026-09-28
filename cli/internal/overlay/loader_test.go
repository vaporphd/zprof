package overlay

import (
	"os"
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
	require.NotEmpty(t, b.GuardScript, "GuardScript must be read when zprof-guard.py exists")
	require.Contains(t, string(b.GuardSchema), "version: 1")
}

func TestLoadBase_GuardFilesOptional(t *testing.T) {
	// A base profile predating guard support (no guard.yaml/zprof-guard.py)
	// must still load successfully, with both fields left empty.
	dir := t.TempDir()
	require.NoError(t, copyDir(filepath.Join("..", "..", "testdata", "repo", "base"), dir))
	require.NoError(t, os.Remove(filepath.Join(dir, "guard.yaml")))
	require.NoError(t, os.Remove(filepath.Join(dir, "zprof-guard.py")))

	b, err := LoadBase(dir)
	require.NoError(t, err)
	require.Empty(t, b.GuardScript)
	require.Empty(t, b.GuardSchema)
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := os.MkdirAll(d, 0o755); err != nil {
				return err
			}
			if err := copyDir(s, d); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(s)
		if err != nil {
			return err
		}
		if err := os.WriteFile(d, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func TestLoadOverlay(t *testing.T) {
	o, err := LoadOverlay(filepath.Join("..", "..", "testdata", "repo", "overlays", "fake-ios"))
	require.NoError(t, err)
	require.Equal(t, "fake-ios", o.Manifest.Name)
	require.NotNil(t, o.Detect)
	require.Contains(t, o.Agents, "architect")
	require.NotEmpty(t, o.LoopMD)
	require.NotEmpty(t, o.ClaudeBlock)
	require.Contains(t, string(o.GuardSchema), "overlay_only_rule")
}

func TestLoadOverlay_GuardSchemaOptional(t *testing.T) {
	// Most overlays ship no guard.yaml at all (#30 adds the first one) —
	// its absence must not be an error.
	src := filepath.Join("..", "..", "testdata", "repo", "overlays", "fake-ios")
	dir := filepath.Join(t.TempDir(), "fake-ios")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, copyDir(src, dir))
	require.NoError(t, os.Remove(filepath.Join(dir, "guard.yaml")))

	o, err := LoadOverlay(dir)
	require.NoError(t, err)
	require.Empty(t, o.GuardSchema)
}

func TestNamespaceAgent(t *testing.T) {
	require.Equal(t, "architect-ios", NamespaceAgent("architect", "ios-swift"))
	require.Equal(t, "architect-py", NamespaceAgent("architect", "backend-python"))
	require.Equal(t, "architect-web", NamespaceAgent("architect", "frontend-web"))
	require.Equal(t, "architect-macho", NamespaceAgent("architect", "re-macho"))
}
