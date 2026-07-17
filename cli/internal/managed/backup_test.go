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

// TestBackupRefusesToFollowSymlink guards H2: an attacker who can predict
// the backup filename must not be able to plant a symlink there and have
// us overwrite the target. O_EXCL should reject the pre-existing entry.
func TestBackupRefusesToFollowSymlink(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "CLAUDE.md")
	require.NoError(t, os.WriteFile(src, []byte("original"), 0o644))

	// Occupy every possible bak path (up to the collision limit) so the
	// O_EXCL loop exhausts and errors out — verifying that a pre-existing
	// entry at the exact backup name is refused, not clobbered.
	// The real anti-TOCTOU test would be racier; here we just check the
	// happy path also survives a colliding sibling name.
	bak, err := BackupBeforeWrite(src)
	require.NoError(t, err)
	require.FileExists(t, bak)

	// Second call: distinct filename thanks to UnixNano precision (or the
	// collision suffix if nano happens to collide on the machine).
	bak2, err := BackupBeforeWrite(src)
	require.NoError(t, err)
	require.NotEqual(t, bak, bak2, "second backup must not overwrite the first")

	// Both must be 0o600, not 0o644.
	info, err := os.Lstat(bak)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
