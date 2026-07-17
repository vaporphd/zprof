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
	// Reject marker injection: overlay-supplied content must never contain
	// a zprof:begin or zprof:end marker. Otherwise a crafted overlay could
	// terminate its own block and open a new one, hijacking other managed
	// blocks on the next apply. Escape the marker on write instead of
	// rejecting silently, but flag it as an error the caller can surface.
	for _, b := range updates {
		if ContainsMarker(b.Content) {
			return "", fmt.Errorf("overlay=%s block=%s: content contains zprof marker (possible injection)", b.Overlay, b.Key)
		}
	}

	key := func(overlay, blockKey string) string { return overlay + "\x00" + blockKey }
	updateByKey := map[string]Block{}
	for _, b := range updates {
		updateByKey[key(b.Overlay, b.Key)] = b
	}

	// Replace-in-place / drop-orphan: walk lines. On a begin marker whose
	// (overlay,key) is in updates, swap content. On any other zprof block
	// (marker present but no matching update), drop it — that block belongs
	// to a removed overlay or a retired key and must not persist. Non-marker
	// lines are copied verbatim.
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
			}
			// Skip everything up to and including the end marker (both for
			// handled updates and for orphan drops).
			for i++; i < len(lines); i++ {
				if endRe.MatchString(lines[i]) {
					break
				}
			}
			continue
		}
		out = append(out, lines[i])
	}
	// Trim trailing empty lines produced by an orphan drop at the end of
	// the file so we don't accumulate blank lines on every apply.
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}

	// Append updates that weren't in the existing text. If there's any
	// existing content, separate it from new blocks with a blank line.
	appended := false
	for _, b := range updates {
		if handled[key(b.Overlay, b.Key)] {
			continue
		}
		if !appended {
			if len(out) > 0 {
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
