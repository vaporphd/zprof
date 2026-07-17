// Package fsutil provides safe filesystem primitives shared across zprof.
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to path via a temp file in the same directory
// followed by os.Rename. On POSIX filesystems Rename is atomic across a
// process crash, `no space left`, or SIGKILL: the target either still holds
// its previous contents or the fully-written new contents, never a truncated
// half-write. Callers that previously used os.WriteFile on managed files
// (CLAUDE.md, AGENT_LOOP.md, agents, state files, .zprof.yaml) must route
// through here.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	// The temp file MUST live in the same directory as the target so
	// Rename is a same-filesystem move (atomic). CreateTemp in os.TempDir()
	// would degrade to copy+delete across mount points.
	f, err := os.CreateTemp(dir, ".zprof.tmp-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmp := f.Name()
	success := false
	defer func() {
		if !success {
			os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}
	success = true
	return nil
}
