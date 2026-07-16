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
