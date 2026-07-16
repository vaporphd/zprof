package managed

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseBlocksSingle(t *testing.T) {
	src := `# Doc

<!-- zprof:begin overlay=ios-swift block=stack-config -->
stack:
  ios:
    build: xcodebuild
<!-- zprof:end -->

## Custom stuff
custom content here
`
	blocks, err := ParseBlocks(src)
	require.NoError(t, err)
	require.Len(t, blocks, 1)
	require.Equal(t, "ios-swift", blocks[0].Overlay)
	require.Equal(t, "stack-config", blocks[0].Key)
	require.Contains(t, blocks[0].Content, "stack:")
	require.Contains(t, blocks[0].Content, "build: xcodebuild")
}

func TestParseBlocksMultiple(t *testing.T) {
	src := `<!-- zprof:begin overlay=py block=a -->
A
<!-- zprof:end -->
between
<!-- zprof:begin overlay=web block=b -->
B
<!-- zprof:end -->
`
	blocks, err := ParseBlocks(src)
	require.NoError(t, err)
	require.Len(t, blocks, 2)
	require.Equal(t, "py", blocks[0].Overlay)
	require.Equal(t, "web", blocks[1].Overlay)
}

func TestParseBlocksMismatchedMarkers(t *testing.T) {
	src := `<!-- zprof:begin overlay=x block=y -->
oops no end
`
	_, err := ParseBlocks(src)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unclosed block")
}

func TestParseBlocksStrayEnd(t *testing.T) {
	src := `<!-- zprof:end -->
`
	_, err := ParseBlocks(src)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected end marker")
}
