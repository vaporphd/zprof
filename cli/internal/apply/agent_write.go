package apply

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/vaporphd/zprof/internal/models"
)

// modelLineRe matches a `model: <value>` line at line-start. Applied ONLY
// inside YAML frontmatter (between the leading and next `---` delimiters),
// never against the body — a `model:` line inside a fenced code block or
// example must be left alone.
//
// Uses [^\S\n]* for horizontal whitespace instead of \s* so the trailing
// newline is preserved: \s eats \n, and in multiline mode the `$` anchor
// would then let the match consume the line separator, so ReplaceAllString
// would glue the following line to `model: <value>`.
var modelLineRe = regexp.MustCompile(`(?m)^model:[^\S\n]*(\S+)[^\S\n]*$`)

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
	return writeFileAtomic(target, []byte(rewritten), 0o644)
}

// frontmatterBounds returns [start,end) byte offsets of the frontmatter body
// (excluding the enclosing `---` delimiters). ok=false when no frontmatter.
// Accepts leading BOM/whitespace-free content: frontmatter must start on line 1.
func frontmatterBounds(content string) (start, end int, ok bool) {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return 0, 0, false
	}
	// Skip past the opening delimiter line.
	nl := strings.IndexByte(content, '\n')
	if nl < 0 {
		return 0, 0, false
	}
	bodyStart := nl + 1
	// Find the next line that is exactly "---".
	rest := content[bodyStart:]
	for offset := 0; offset < len(rest); {
		line := rest[offset:]
		if idx := strings.IndexByte(line, '\n'); idx >= 0 {
			candidate := strings.TrimRight(line[:idx], "\r")
			if candidate == "---" {
				return bodyStart, bodyStart + offset, true
			}
			offset += idx + 1
			continue
		}
		if strings.TrimRight(line, "\r") == "---" {
			return bodyStart, bodyStart + len(rest), true
		}
		break
	}
	return 0, 0, false
}

func resolveModelInAgent(content, override string) (string, error) {
	start, end, ok := frontmatterBounds(content)
	if !ok {
		// No frontmatter: nothing to rewrite even if the body mentions
		// `model:` in examples.
		return content, nil
	}
	fm := content[start:end]
	m := modelLineRe.FindStringSubmatch(fm)
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
	// Replace only the first occurrence inside the frontmatter, then splice
	// back — leaves the body untouched.
	rewrittenFM := modelLineRe.ReplaceAllString(fm, "model: "+exact)
	return content[:start] + rewrittenFM + content[end:], nil
}
