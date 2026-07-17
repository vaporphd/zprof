package apply

import (
	"os"

	"github.com/vaporphd/zprof/internal/fsutil"
)

// writeFileAtomic thin wrapper around fsutil.WriteFileAtomic to keep
// existing call-sites unchanged.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	return fsutil.WriteFileAtomic(path, data, perm)
}
