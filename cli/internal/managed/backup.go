package managed

import (
	"fmt"
	"os"
	"time"
)

// BackupBeforeWrite creates <path>.zprof.bak-<unixtime> if path exists.
// Returns backup path (empty string if source didn't exist).
func BackupBeforeWrite(path string) (string, error) {
	src, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read source: %w", err)
	}
	bak := fmt.Sprintf("%s.zprof.bak-%d", path, time.Now().Unix())
	if err := os.WriteFile(bak, src, 0o644); err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}
	return bak, nil
}
