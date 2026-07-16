package detect

import (
	"path/filepath"
	"testing"

	"github.com/alcherk/zprof/internal/manifest"
	"github.com/stretchr/testify/require"
)

func TestScanFindsIOS(t *testing.T) {
	rules, err := manifest.LoadDetect(filepath.Join("..", "..", "testdata", "overlays", "valid-detect.yaml"))
	require.NoError(t, err)
	matches := Scan(filepath.Join("..", "..", "testdata", "projects", "fake-ios"), []*manifest.DetectRules{rules})
	require.Len(t, matches, 1)
	require.Equal(t, "ios-swift", matches[0].OverlayName)
	require.Equal(t, "high", matches[0].Confidence)
	require.NotEmpty(t, matches[0].Evidence)
}

func TestScanEmptyProjectYieldsNoMatches(t *testing.T) {
	rules, err := manifest.LoadDetect(filepath.Join("..", "..", "testdata", "overlays", "valid-detect.yaml"))
	require.NoError(t, err)
	matches := Scan(filepath.Join("..", "..", "testdata", "projects", "fake-empty"), []*manifest.DetectRules{rules})
	require.Empty(t, matches)
}
