package sync

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureRepoClonesFromLocalGit(t *testing.T) {
	tmpRemote := t.TempDir()
	require.NoError(t, exec.Command("git", "-C", tmpRemote, "init", "-q", "-b", "main").Run())
	require.NoError(t, os.WriteFile(filepath.Join(tmpRemote, "README.md"), []byte("test"), 0o644))
	require.NoError(t, exec.Command("git", "-C", tmpRemote, "add", "README.md").Run())
	require.NoError(t, exec.Command("git", "-C", tmpRemote, "-c", "user.email=t@t", "-c", "user.name=t",
		"commit", "-m", "init").Run())

	target := filepath.Join(t.TempDir(), "clone")
	require.NoError(t, EnsureRepo("file://"+tmpRemote, target))
	require.FileExists(t, filepath.Join(target, "README.md"))
}
