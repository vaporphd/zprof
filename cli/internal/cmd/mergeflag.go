// Package cmd shared helpers for the --merge flag and its interactive
// resolver. Extracted so `apply`, `sync`, and `init` all wire the same
// three managed-block merge modes: overwrite / preserve / interactive.
package cmd

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/vaporphd/zprof/internal/managed"
)

// parseMergeFlag turns the --merge string into a MergeMode and a resolver
// (nil for non-interactive modes). Unknown values return an error so a
// typo doesn't silently degrade to overwrite-and-clobber.
func parseMergeFlag(v string) (managed.MergeMode, managed.ConflictResolver, error) {
	switch v {
	case "", "overwrite":
		return managed.ModeOverwrite, nil, nil
	case "preserve":
		return managed.ModePreserve, nil, nil
	case "interactive":
		return managed.ModeInteractive, interactiveResolver, nil
	default:
		return 0, nil, fmt.Errorf("--merge: unknown mode %q (want: overwrite|preserve|interactive)", v)
	}
}

// interactiveResolver prompts the user for each conflicting managed block
// via huh's Select widget. Choices: keep existing (hand-edited), take
// incoming (regenerated), or paste a manual merge. Returns the chosen
// content. Errors from huh (e.g. the terminal isn't a TTY) surface up so
// the caller can bail rather than silently clobbering the user's edit.
func interactiveResolver(b managed.Block, existing, incoming string) (string, error) {
	const (
		keepExisting = "existing"
		takeIncoming = "incoming"
	)
	choice := takeIncoming
	title := fmt.Sprintf("Conflict in overlay=%s block=%s — resolve?", b.Overlay, b.Key)
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(title).
			Description(fmt.Sprintf("--- existing ---\n%s\n\n--- incoming ---\n%s", existing, incoming)).
			Options(
				huh.NewOption("Keep existing (hand-edited)", keepExisting),
				huh.NewOption("Take incoming (regenerated)", takeIncoming),
			).
			Value(&choice),
	))
	if err := form.Run(); err != nil {
		return "", fmt.Errorf("interactive merge prompt: %w", err)
	}
	if choice == keepExisting {
		return existing, nil
	}
	return incoming, nil
}
