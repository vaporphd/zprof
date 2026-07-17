package detect

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vaporphd/zprof/internal/manifest"
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

func TestScanSkipsHeavyDirs(t *testing.T) {
	proj := t.TempDir()
	// Top-level Xcode project: should be matched.
	require.NoError(t, os.MkdirAll(filepath.Join(proj, "App.xcodeproj"), 0o755))
	// A nested Pods/ dependency that itself vendors an .xcodeproj: the
	// scanner must skip descending into Pods/ entirely, so this one must
	// NOT show up in evidence (and, before the fix, a large real Pods/
	// tree would also thrash the disk with a walk per pattern).
	require.NoError(t, os.MkdirAll(filepath.Join(proj, "Pods", "Dependency.xcodeproj"), 0o755))

	rules, err := manifest.LoadDetect(filepath.Join("..", "..", "testdata", "overlays", "valid-detect.yaml"))
	require.NoError(t, err)

	matches := Scan(proj, []*manifest.DetectRules{rules})
	require.Len(t, matches, 1)
	require.Equal(t, []string{filepath.Join(proj, "App.xcodeproj")}, matches[0].Evidence)
}
