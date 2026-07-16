package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadDetectValid(t *testing.T) {
	d, err := LoadDetect(filepath.Join("..", "..", "testdata", "overlays", "valid-detect.yaml"))
	require.NoError(t, err)
	require.Equal(t, "ios-swift", d.Name)
	require.Contains(t, d.AnyFile, "*.xcodeproj")
	require.Len(t, d.AnyRegex, 1)
	require.Equal(t, "Package.swift", d.AnyRegex[0].Path)
	require.Equal(t, "high", d.Confidence)
}

func TestLoadDetectRejectsUnknownConfidence(t *testing.T) {
	// write ad-hoc invalid fixture
	tmp := t.TempDir()
	f := filepath.Join(tmp, "bad.yaml")
	err := os.WriteFile(f, []byte("name: x\ndetect:\n  confidence: extreme\n"), 0644)
	require.NoError(t, err)
	_, err = LoadDetect(f)
	require.Error(t, err)
	require.Contains(t, err.Error(), "confidence must be one of: high, medium, low")
}
