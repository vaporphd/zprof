package managed

import (
	"fmt"
	"regexp"
	"strings"
)

// Block represents a single managed content block delimited by a
// zprof:begin/zprof:end marker pair.
type Block struct {
	Overlay   string
	Key       string
	Content   string
	StartLine int // 1-indexed line of the begin marker
	EndLine   int // 1-indexed line of the end marker
}

var (
	beginRe = regexp.MustCompile(`^\s*<!--\s*zprof:begin\s+overlay=([\w.-]+)\s+block=([\w.-]+)\s*-->\s*$`)
	endRe   = regexp.MustCompile(`^\s*<!--\s*zprof:end\s*-->\s*$`)
)

// ParseBlocks scans text for managed marker pairs and returns their contents.
// Mismatched or unclosed blocks return an error.
func ParseBlocks(text string) ([]Block, error) {
	lines := strings.Split(text, "\n")
	var out []Block
	var cur *Block
	var buf []string
	for i, line := range lines {
		lineNo := i + 1
		if m := beginRe.FindStringSubmatch(line); m != nil {
			if cur != nil {
				return nil, fmt.Errorf("nested begin at line %d (previous unclosed at %d)", lineNo, cur.StartLine)
			}
			cur = &Block{Overlay: m[1], Key: m[2], StartLine: lineNo}
			buf = buf[:0]
			continue
		}
		if endRe.MatchString(line) {
			if cur == nil {
				return nil, fmt.Errorf("unexpected end marker at line %d", lineNo)
			}
			cur.Content = strings.Join(buf, "\n")
			cur.EndLine = lineNo
			out = append(out, *cur)
			cur = nil
			continue
		}
		if cur != nil {
			buf = append(buf, line)
		}
	}
	if cur != nil {
		return nil, fmt.Errorf("unclosed block at line %d (overlay=%s block=%s)", cur.StartLine, cur.Overlay, cur.Key)
	}
	return out, nil
}
