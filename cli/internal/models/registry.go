package models

import (
	"fmt"
	"strings"
)

// Aliases maps tier alias → exact Claude model ID as of 2026-07-16.
var Aliases = map[string]string{
	"opus":    "claude-opus-4-8",
	"opus-1m": "claude-opus-4-7[1m]",
	"opus-4-6": "claude-opus-4-6[1m]",
	"sonnet":  "claude-sonnet-5",
	"haiku":   "claude-haiku-4-5-20251001",
	"fable":   "claude-fable-5",
}

// aliasOrder lists valid aliases in a stable order for error messages,
// since Go map iteration order is randomized.
var aliasOrder = []string{"opus", "opus-1m", "opus-4-6", "sonnet", "haiku", "fable"}

// Resolve returns the exact model ID for either a tier alias or an
// already-exact model ID. Exact IDs pass through if they start with "claude-".
func Resolve(name string) (string, error) {
	if id, ok := Aliases[name]; ok {
		return id, nil
	}
	if strings.HasPrefix(name, "claude-") {
		return name, nil
	}
	return "", fmt.Errorf("unknown model alias %q — valid aliases: %s (or use exact claude-* ID)",
		name, strings.Join(aliasOrder, ", "))
}
