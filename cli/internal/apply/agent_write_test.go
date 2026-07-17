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

// TestResolveModelInAgentLeavesBodyAlone guards H4: the rewriter must
// only touch the YAML frontmatter, not example `model:` lines in the
// prompt body (e.g. inside a code fence).
func TestResolveModelInAgentLeavesBodyAlone(t *testing.T) {
	src := "---\nname: planner\nmodel: sonnet\n---\nExample YAML in prompt body:\n\n```yaml\nmodel: opus\n```\n"
	out, err := resolveModelInAgent(src, "")
	require.NoError(t, err)
	// Frontmatter got resolved:
	require.Contains(t, out, "---\nname: planner\nmodel: claude-sonnet-5\n---")
	// Body example unchanged:
	require.Contains(t, out, "model: opus")
}

// TestResolveModelInAgentNoFrontmatter — a file without --- must be
// returned verbatim even if it mentions `model:` in prose.
func TestResolveModelInAgentNoFrontmatter(t *testing.T) {
	src := "No frontmatter here.\nmodel: sonnet is just prose.\n"
	out, err := resolveModelInAgent(src, "")
	require.NoError(t, err)
	require.Equal(t, src, out)
}
