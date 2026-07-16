// Package sync provides git-based repository sync for zprof profile repos.
//
// Note: this package is named "sync", which collides with the stdlib "sync"
// package. Callers outside this package should import it with an alias, e.g.:
//
//	sy "github.com/alcherk/zprof/internal/sync"
package sync

import (
	"context"
	"os"
	"path/filepath"

	getter "github.com/hashicorp/go-getter"
)

// EnsureRepo clones remoteURL into localDir on first run; on subsequent runs
// it re-fetches and syncs via go-getter's idempotent client, equivalent to a
// fetch+reset --hard to the remote's default branch.
func EnsureRepo(remoteURL, localDir string) error {
	if err := os.MkdirAll(filepath.Dir(localDir), 0o755); err != nil {
		return err
	}
	client := &getter.Client{
		Ctx:  context.Background(),
		Src:  "git::" + remoteURL,
		Dst:  localDir,
		Mode: getter.ClientModeAny,
	}
	return client.Get()
}
