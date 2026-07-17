package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LocateSession finds the JSONL to evaluate. Resolution order:
//
//  1. If `arg` looks like a path (contains "/" or ends ".jsonl"), use it.
//  2. If `arg` is a bare session ID (e.g. "445b990e-…"), find it under
//     ~/.claude/projects/<slug>/<arg>.jsonl for any slug.
//  3. If `arg` is empty, look under ~/.claude/projects/<slug matching cwd>/
//     and pick the most-recently-modified .jsonl.
//
// Returns the resolved path or an error explaining what wasn't found.
func LocateSession(arg, cwd string) (string, error) {
	if arg != "" && (strings.ContainsRune(arg, '/') || strings.HasSuffix(arg, ".jsonl")) {
		if _, err := os.Stat(arg); err != nil {
			return "", fmt.Errorf("session log not found: %s", arg)
		}
		return arg, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	projectsRoot := filepath.Join(home, ".claude", "projects")

	if arg != "" {
		matches, _ := filepath.Glob(filepath.Join(projectsRoot, "*", arg+".jsonl"))
		if len(matches) == 0 {
			return "", fmt.Errorf("no session log matches ID %q under %s", arg, projectsRoot)
		}
		return matches[0], nil
	}

	// No arg — auto-detect from cwd. Claude Code's slug rule is: absolute
	// path with '/' replaced by '-'. So /Volumes/mydata/foo becomes
	// -Volumes-mydata-foo.
	slug := "-" + strings.ReplaceAll(strings.TrimPrefix(cwd, "/"), "/", "-")
	dir := filepath.Join(projectsRoot, slug)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("no logs found for cwd %s (looked under %s): %w", cwd, dir, err)
	}
	type item struct {
		path string
		mod  int64
	}
	var candidates []item
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, item{
			path: filepath.Join(dir, e.Name()),
			mod:  info.ModTime().UnixNano(),
		})
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no .jsonl session logs in %s", dir)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].mod > candidates[j].mod })
	return candidates[0].path, nil
}
