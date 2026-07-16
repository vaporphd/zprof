package managed

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeOverwriteAlwaysWins(t *testing.T) {
	src := "<!-- zprof:begin overlay=x block=y -->\nHAND-EDITED\n<!-- zprof:end -->\n"
	updates := []Block{{Overlay: "x", Key: "y", Content: "GENERATED"}}
	out, conflicts, err := Merge(src, updates, ModeOverwrite, nil)
	require.NoError(t, err)
	require.Empty(t, conflicts)
	require.Contains(t, out, "GENERATED")
	require.NotContains(t, out, "HAND-EDITED")
}

func TestMergePreserveKeepsExistingIfDiffers(t *testing.T) {
	src := "<!-- zprof:begin overlay=x block=y -->\nHAND-EDITED\n<!-- zprof:end -->\n"
	updates := []Block{{Overlay: "x", Key: "y", Content: "GENERATED"}}
	out, conflicts, err := Merge(src, updates, ModePreserve, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	require.Contains(t, out, "HAND-EDITED")
	require.NotContains(t, out, "GENERATED")
}

func TestMergeInteractiveCallsResolver(t *testing.T) {
	src := "<!-- zprof:begin overlay=x block=y -->\nHAND\n<!-- zprof:end -->\n"
	updates := []Block{{Overlay: "x", Key: "y", Content: "NEW"}}
	called := false
	resolver := func(b Block, existing, incoming string) (string, error) {
		called = true
		require.Equal(t, "HAND", strings.TrimSpace(existing))
		require.Equal(t, "NEW", incoming)
		return "MERGED", nil
	}
	out, conflicts, err := Merge(src, updates, ModeInteractive, resolver)
	require.NoError(t, err)
	require.True(t, called)
	require.Len(t, conflicts, 1)
	require.Contains(t, out, "MERGED")
}
