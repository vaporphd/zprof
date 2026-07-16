package managed

import (
	"fmt"
	"strings"
)

// Render returns text with each Block in updates written. Blocks matching
// (Overlay, Key) in existing text are replaced in place; blocks not present
// are appended at the end (separated by a blank line).
func Render(existing string, updates []Block) (string, error) {
	if _, err := ParseBlocks(existing); err != nil {
		return "", err
	}

	key := func(overlay, blockKey string) string { return overlay + "\x00" + blockKey }
	updateByKey := map[string]Block{}
	for _, b := range updates {
		updateByKey[key(b.Overlay, b.Key)] = b
	}

	// Replace in place: walk lines, when a begin marker matches an update,
	// swap the block content; else copy.
	lines := strings.Split(existing, "\n")
	var out []string
	handled := map[string]bool{}
	for i := 0; i < len(lines); i++ {
		if m := beginRe.FindStringSubmatch(lines[i]); m != nil {
			k := key(m[1], m[2])
			if upd, ok := updateByKey[k]; ok {
				out = append(out, fmt.Sprintf("<!-- zprof:begin overlay=%s block=%s -->", upd.Overlay, upd.Key))
				out = append(out, upd.Content)
				out = append(out, "<!-- zprof:end -->")
				handled[k] = true
				// skip until end marker
				for i++; i < len(lines) && !endRe.MatchString(lines[i]); i++ {
				}
				continue
			}
		}
		out = append(out, lines[i])
	}

	// Append updates that weren't in the existing text.
	appended := false
	for _, b := range updates {
		if handled[key(b.Overlay, b.Key)] {
			continue
		}
		if !appended {
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			appended = true
		}
		out = append(out, fmt.Sprintf("<!-- zprof:begin overlay=%s block=%s -->", b.Overlay, b.Key))
		out = append(out, b.Content)
		out = append(out, "<!-- zprof:end -->")
	}

	// Ensure trailing newline
	rendered := strings.Join(out, "\n")
	if !strings.HasSuffix(rendered, "\n") {
		rendered += "\n"
	}
	return rendered, nil
}
