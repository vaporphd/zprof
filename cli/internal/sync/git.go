// Package sync provides git-based repository sync for zprof profile repos.
//
// Note: this package is named "sync", which collides with the stdlib "sync"
// package. Callers outside this package should import it with an alias, e.g.:
//
//	sy "github.com/vaporphd/zprof/internal/sync"
package sync

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	getter "github.com/hashicorp/go-getter"
)

// EnsureRepo is the legacy no-context entry point retained for callers that
// haven't wired context yet. New code should prefer EnsureRepoCtx so the
// user can cancel a stalled git-fetch via Ctrl-C instead of SIGKILL.
func EnsureRepo(remoteURL, localDir string) error {
	return EnsureRepoCtx(context.Background(), remoteURL, localDir)
}

// EnsureRepoCtx clones remoteURL into localDir on first run; on subsequent
// runs it re-fetches and syncs via go-getter's idempotent client. Passing
// a cancellable ctx lets the caller enforce a timeout and honor Ctrl-C.
func EnsureRepoCtx(ctx context.Context, remoteURL, localDir string) error {
	if err := os.MkdirAll(filepath.Dir(localDir), 0o755); err != nil {
		return err
	}
	client := &getter.Client{
		Ctx:  ctx,
		Src:  "git::" + withDefaultRef(remoteURL),
		Dst:  localDir,
		Mode: getter.ClientModeAny,
	}
	return client.Get()
}

// withDefaultRef ensures remoteURL carries an explicit ?ref= query parameter.
//
// go-getter's git getter only skips "git fetch origin -- <ref>" (an
// invalid, empty pathspec that git rejects with exit 128) when localDir
// does not yet exist and it takes the clone path. On every subsequent
// call - the normal "zprof sync" case, where localDir already holds a
// clone from a prior run - it takes the update path, which runs that
// fetch unconditionally. Without a ref, that command becomes
// `git fetch origin -- ""` and fails. Defaulting ref to "HEAD" (a ref
// git always resolves, pointing at the remote's default branch tip)
// keeps both the first-run clone and every later re-sync working.
func withDefaultRef(remoteURL string) string {
	if strings.Contains(remoteURL, "?ref=") || strings.Contains(remoteURL, "&ref=") {
		return remoteURL
	}
	u, err := url.Parse(remoteURL)
	if err != nil {
		return remoteURL
	}
	q := u.Query()
	if q.Get("ref") != "" {
		return remoteURL
	}
	q.Set("ref", "HEAD")
	u.RawQuery = q.Encode()
	return u.String()
}
