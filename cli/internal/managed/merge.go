package managed

import (
	"fmt"
	"strings"
)

// MergeMode selects how Merge resolves a block whose existing content
// differs from the incoming generated content.
type MergeMode int

const (
	// ModeOverwrite always replaces existing content with the incoming block.
	ModeOverwrite MergeMode = iota
	// ModePreserve always keeps the existing content, discarding the incoming block.
	ModePreserve
	// ModeInteractive delegates the decision to a ConflictResolver.
	ModeInteractive
)

// Conflict describes a managed block whose existing content differed from
// the incoming generated content.
type Conflict struct {
	Overlay, Key, Existing, Incoming string
}

// ConflictResolver decides the final content for a conflicting block. It
// receives the incoming Block along with the existing and incoming content
// and returns the content to use.
type ConflictResolver func(b Block, existing, incoming string) (string, error)

// Merge applies updates to existing text according to mode. Blocks whose
// existing content matches the incoming content pass through unchanged.
// Blocks that differ are resolved per mode and reported as Conflicts.
func Merge(existing string, updates []Block, mode MergeMode, resolve ConflictResolver) (string, []Conflict, error) {
	blocks, err := ParseBlocks(existing)
	if err != nil {
		return "", nil, err
	}
	byKey := map[string]Block{}
	for _, b := range blocks {
		byKey[b.Overlay+"\x00"+b.Key] = b
	}
	var conflicts []Conflict
	final := make([]Block, 0, len(updates))
	for _, u := range updates {
		k := u.Overlay + "\x00" + u.Key
		cur, exists := byKey[k]
		if !exists || strings.TrimSpace(cur.Content) == strings.TrimSpace(u.Content) {
			final = append(final, u)
			continue
		}
		switch mode {
		case ModeOverwrite:
			// Overwrite always wins; nothing for the caller to reconcile,
			// so no Conflict is reported.
			final = append(final, u)
		case ModePreserve:
			conflicts = append(conflicts, Conflict{Overlay: u.Overlay, Key: u.Key, Existing: cur.Content, Incoming: u.Content})
			final = append(final, cur)
		case ModeInteractive:
			if resolve == nil {
				return "", nil, fmt.Errorf("interactive mode requires resolver")
			}
			conflicts = append(conflicts, Conflict{Overlay: u.Overlay, Key: u.Key, Existing: cur.Content, Incoming: u.Content})
			chosen, err := resolve(u, cur.Content, u.Content)
			if err != nil {
				return "", nil, err
			}
			final = append(final, Block{Overlay: u.Overlay, Key: u.Key, Content: chosen})
		default:
			return "", nil, fmt.Errorf("unknown merge mode %d", mode)
		}
	}
	rendered, err := Render(existing, final)
	if err != nil {
		return "", nil, err
	}
	return rendered, conflicts, nil
}
