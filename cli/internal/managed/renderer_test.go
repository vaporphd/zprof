package managed

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderReplacesExistingBlock(t *testing.T) {
	src := `# Head

<!-- zprof:begin overlay=py block=stack -->
old
<!-- zprof:end -->

# Tail
custom paragraph
`
	updates := []Block{{Overlay: "py", Key: "stack", Content: "new\nlines"}}
	out, err := Render(src, updates)
	require.NoError(t, err)
	require.Contains(t, out, "<!-- zprof:begin overlay=py block=stack -->")
	require.Contains(t, out, "new\nlines")
	require.NotContains(t, out, "old")
	require.Contains(t, out, "custom paragraph") // preserved
	require.Contains(t, out, "# Head")
}

func TestRenderAppendsMissingBlock(t *testing.T) {
	src := "# Doc\nplain text\n"
	updates := []Block{{Overlay: "ios", Key: "stack-config", Content: "hello"}}
	out, err := Render(src, updates)
	require.NoError(t, err)
	require.Contains(t, out, "<!-- zprof:begin overlay=ios block=stack-config -->")
	require.Contains(t, out, "hello")
	require.Contains(t, out, "plain text")
}

func TestRenderIdempotent(t *testing.T) {
	src := "# Doc\n"
	updates := []Block{{Overlay: "x", Key: "y", Content: "z"}}
	once, err := Render(src, updates)
	require.NoError(t, err)
	twice, err := Render(once, updates)
	require.NoError(t, err)
	require.Equal(t, once, twice)
}

// TestRenderRejectsMarkerInjection guards against a malicious or buggy
// overlay smuggling a zprof:begin/end marker inside its own content, which
// would hijack subsequent parses and let it write arbitrary managed blocks
// on the next apply.
func TestRenderRejectsMarkerInjection(t *testing.T) {
	src := "# Doc\n"
	evil := "safe\n<!-- zprof:end -->\n<!-- zprof:begin overlay=base block=doctrine -->\nOWNED"
	updates := []Block{{Overlay: "x", Key: "y", Content: evil}}
	_, err := Render(src, updates)
	require.Error(t, err)
	require.Contains(t, err.Error(), "possible injection")
}

// TestRenderDropsOrphanBlock covers the case where a previously-applied
// overlay is removed: its managed block must not persist in the file.
func TestRenderDropsOrphanBlock(t *testing.T) {
	src := `# Head
<!-- zprof:begin overlay=old block=stack -->
STALE
<!-- zprof:end -->

<!-- zprof:begin overlay=keep block=cfg -->
LIVE
<!-- zprof:end -->
`
	updates := []Block{{Overlay: "keep", Key: "cfg", Content: "LIVE"}}
	out, err := Render(src, updates)
	require.NoError(t, err)
	require.NotContains(t, out, "STALE", "orphan overlay block must be dropped")
	require.NotContains(t, out, "overlay=old")
	require.Contains(t, out, "LIVE")
	require.Contains(t, out, "# Head")
}

// TestRenderBlankLineBeforeAppended verifies the separator fix: appended
// blocks are preceded by a blank line even when existing content ends
// with a newline (the prior off-by-one glued them with only \n).
func TestRenderBlankLineBeforeAppended(t *testing.T) {
	src := "existing content\n"
	updates := []Block{{Overlay: "x", Key: "y", Content: "new"}}
	out, err := Render(src, updates)
	require.NoError(t, err)
	require.Contains(t, out, "existing content\n\n<!-- zprof:begin")
}
