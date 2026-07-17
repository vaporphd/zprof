package apply

import (
	"os"
	"path/filepath"
	"regexp"

	"github.com/vaporphd/zprof/internal/models"
)

var modelLineRe = regexp.MustCompile(`(?m)^model:\s*(\S+)\s*$`)

// WriteAgent parses the frontmatter model field, resolves it via the model
// registry (or applies modelOverride if non-empty), and writes to
// destDir/agentName.md.
func WriteAgent(destDir, agentName, content, modelOverride string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	rewritten, err := resolveModelInAgent(content, modelOverride)
	if err != nil {
		return err
	}
	// Preserve subdirs (e.g. gates/foo).
	target := filepath.Join(destDir, agentName+".md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, []byte(rewritten), 0o644)
}

func resolveModelInAgent(content, override string) (string, error) {
	m := modelLineRe.FindStringSubmatch(content)
	if m == nil {
		return content, nil // no model field, leave alone
	}
	src := m[1]
	if override != "" {
		src = override
	}
	exact, err := models.Resolve(src)
	if err != nil {
		return "", err
	}
	return modelLineRe.ReplaceAllString(content, "model: "+exact), nil
}
