package managed

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackupCreatesBakCopy(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "CLAUDE.md")
	require.NoError(t, os.WriteFile(p, []byte("original"), 0o644))
	bak, err := BackupBeforeWrite(p)
	require.NoError(t, err)
	require.FileExists(t, bak)
	got, _ := os.ReadFile(bak)
	require.Equal(t, "original", string(got))
}

func TestBackupSkipsMissingFile(t *testing.T) {
	bak, err := BackupBeforeWrite(filepath.Join(t.TempDir(), "nope"))
	require.NoError(t, err)
	require.Empty(t, bak)
}
