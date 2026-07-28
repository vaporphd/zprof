package apply

import (
	"os"
	"path/filepath"

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
		path := filepath.Join(agentDir, name+".md")
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
