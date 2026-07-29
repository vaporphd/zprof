package apply

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/vaporphd/zprof/internal/agents"
	"github.com/vaporphd/zprof/internal/managed"
)

// PruneOrphanAgents removes agent files zprof wrote on a previous apply
// that the current sources no longer provide. `previous` is the
// managed_agents list read from .zprof.yaml; `current` is what this apply
// just wrote. Names absent from `previous` are user-authored and are never
// touched. Every removal is backed up first. Returns the removed names.
func PruneOrphanAgents(agentDir string, previous, current []string) ([]string, error) {
	live := make(map[string]bool, len(current))
	for _, n := range current {
		live[n] = true
	}

	seen := map[string]bool{}
	var doomed []string
	for _, n := range append(append([]string(nil), previous...), agents.Retired...) {
		if n == "" || live[n] || seen[n] {
			continue
		}
		seen[n] = true
		doomed = append(doomed, n)
	}

	var removed []string
	for _, name := range doomed {
		path, ok := agentPathWithin(agentDir, name)
		if !ok {
			// The name came from a user-editable .zprof.yaml and escapes the
			// agents directory (`../../../foo`). Never resolve it — this is
			// the one code path in zprof that deletes files.
			continue
		}
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return removed, err
		}
		if _, err := managed.BackupBeforeWrite(path); err != nil {
			return removed, err
		}
		if err := os.Remove(path); err != nil {
			return removed, err
		}
		removed = append(removed, name)
	}
	return removed, nil
}

// agentPathWithin resolves agentDir/<name>.md and reports whether the result
// still lives under agentDir. Agent names are read from .zprof.yaml, which
// is a user-editable file, so a name like `../../../etc/x` would otherwise
// have PruneOrphanAgents delete outside the project.
func agentPathWithin(agentDir, name string) (string, bool) {
	root := filepath.Clean(agentDir)
	path := filepath.Clean(filepath.Join(root, name+".md"))
	if !strings.HasPrefix(path, root+string(filepath.Separator)) {
		return "", false
	}
	return path, true
}
