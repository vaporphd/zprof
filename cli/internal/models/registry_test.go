package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolve(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"opus", "claude-opus-4-8"},
		{"opus-1m", "claude-opus-4-7[1m]"},
		{"opus-4-6", "claude-opus-4-6[1m]"},
		{"sonnet", "claude-sonnet-5"},
		{"haiku", "claude-haiku-4-5-20251001"},
		{"fable", "claude-fable-5"},
		{"claude-opus-4-8", "claude-opus-4-8"}, // pass-through exact ID
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := Resolve(tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestResolveUnknownAlias(t *testing.T) {
	_, err := Resolve("gpt-5")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown model alias")
	require.Contains(t, err.Error(), "opus, opus-1m, opus-4-6, sonnet, haiku, fable")
}
