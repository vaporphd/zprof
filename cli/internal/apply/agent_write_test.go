package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteAgentResolvesModelAlias(t *testing.T) {
	src := `---
name: planner
model: sonnet
tools: Read, Write
---
Планировщик.
`
	dir := t.TempDir()
	require.NoError(t, WriteAgent(dir, "planner", src, ""))
	data, _ := os.ReadFile(filepath.Join(dir, "planner.md"))
	require.Contains(t, string(data), "model: claude-sonnet-5")
	require.NotContains(t, string(data), "model: sonnet\n")
}

func TestWriteAgentAppliesOverride(t *testing.T) {
	src := `---
name: planner
model: sonnet
---
body
`
	dir := t.TempDir()
	require.NoError(t, WriteAgent(dir, "planner", src, "opus-1m"))
	data, _ := os.ReadFile(filepath.Join(dir, "planner.md"))
	require.Contains(t, string(data), "model: claude-opus-4-7[1m]")
}

func TestWriteAgentErrorsOnUnknownAlias(t *testing.T) {
	src := `---
name: x
model: gpt-5
---
`
	err := WriteAgent(t.TempDir(), "x", src, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown model alias")
}
