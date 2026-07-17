package managed

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"
)

// BackupBeforeWrite creates <path>.zprof.bak-<unixnano>[.N] if path exists,
// as a regular file created with O_CREATE|O_EXCL|O_WRONLY 0o600. O_EXCL
// prevents an attacker who can predict the timestamp from pre-creating the
// backup path as a symlink to an arbitrary target and having us overwrite
// it (TOCTOU). Nanosecond precision plus an incrementing suffix on collision
// removes the "two calls in the same second clobber each other" bug.
// Returns backup path (empty string if source didn't exist).
func BackupBeforeWrite(path string) (string, error) {
	src, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read source: %w", err)
	}
	stem := fmt.Sprintf("%s.zprof.bak-%d", path, time.Now().UnixNano())
	for i := 0; i < 32; i++ {
		bak := stem
		if i > 0 {
			bak = fmt.Sprintf("%s.%d", stem, i)
		}
		f, err := os.OpenFile(bak, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("open backup: %w", err)
		}
		if _, werr := f.Write(src); werr != nil {
			f.Close()
			os.Remove(bak)
			return "", fmt.Errorf("write backup: %w", werr)
		}
		if cerr := f.Close(); cerr != nil {
			return "", fmt.Errorf("close backup: %w", cerr)
		}
		return bak, nil
	}
	return "", fmt.Errorf("backup: exhausted collision suffixes for %s", stem)
}
