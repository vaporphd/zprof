package apply

import (
	"os"
	"path/filepath"

	"github.com/vaporphd/zprof/internal/overlay"
)

// EnsureStateFiles writes each state template to its canonical path in
// projectDir if not already present, leaving existing files untouched. In
// minimal mode, the docs/PROJECT_SPEC.md and docs/adr/0000-template.md
// templates are skipped. Returns the list of file paths that were created.
func EnsureStateFiles(projectDir string, base *overlay.Base, minimal bool) ([]string, error) {
	targets := map[string]string{
		"todo":     filepath.Join(projectDir, "todo.md"),
		"lessons":  filepath.Join(projectDir, "lessons.md"),
		"followup": filepath.Join(projectDir, "followup.md"),
	}
	if !minimal {
		targets["project-spec-skeleton"] = filepath.Join(projectDir, "docs", "PROJECT_SPEC.md")
		targets["adr-template"] = filepath.Join(projectDir, "docs", "adr", "0000-template.md")
	}

	var created []string
	for tmpl, dest := range targets {
		body, ok := base.StateTemplates[tmpl]
		if !ok {
			continue
		}
		if _, err := os.Stat(dest); err == nil {
			continue // already exists, don't touch
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, []byte(body), 0o644); err != nil {
			return nil, err
		}
		created = append(created, dest)
	}
	return created, nil
}
