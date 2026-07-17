package eval

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocateSessionResolvesAbsolutePath(t *testing.T) {
	tmp := t.TempDir()
	log := filepath.Join(tmp, "abcd.jsonl")
	require.NoError(t, os.WriteFile(log, []byte("{}\n"), 0o644))
	got, err := LocateSession(log, tmp)
	require.NoError(t, err)
	require.Equal(t, log, got)
}

func TestLocateSessionErrorsWhenAbsentPath(t *testing.T) {
	_, err := LocateSession("/nonexistent/does-not-exist.jsonl", "/tmp")
	require.Error(t, err)
}
