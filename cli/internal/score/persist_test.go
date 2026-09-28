package score

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAppendScoreAndReadScoredKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nested", "scores.jsonl")
	keys, err := ReadScoredKeys(p)
	require.NoError(t, err)
	require.Empty(t, keys, "missing file is not an error")

	c := Compute(loadRun1(t)[0], Defaults(), "test")
	require.NoError(t, AppendScore(p, c))
	require.NoError(t, AppendScore(p, c))
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	require.Len(t, strings.Split(strings.TrimRight(string(data), "\n"), "\n"), 2)
	require.Contains(t, string(data), `"score":40`)

	keys, err = ReadScoredKeys(p)
	require.NoError(t, err)
	require.True(t, keys[ScoreKey(c)])
	require.Equal(t, c.RunID+"|"+c.WeightsHash, ScoreKey(c))
}

func TestWriteRunLogSection_Idempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "run.md")
	require.NoError(t, os.WriteFile(p, []byte("# task\n| t | a | v | x |\n\n## Итог\nverdict: done · artifact: PR #1\n"), 0o644))
	require.NoError(t, WriteRunLogSection(p, "Score 45/100 · Lucky\n"))
	require.NoError(t, WriteRunLogSection(p, "Score 72/100 · Solid\n"))
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	s := string(data)
	require.Equal(t, 1, strings.Count(s, MarkerBegin))
	require.Equal(t, 1, strings.Count(s, MarkerEnd))
	require.Contains(t, s, "Score 72/100")
	require.NotContains(t, s, "Score 45/100")
	require.True(t, strings.HasPrefix(s, "# task\n"), "original content preserved")
	require.Contains(t, s, "## Итог\nverdict: done")
	require.True(t, strings.Index(s, MarkerBegin) > strings.Index(s, "## Итог"), "section goes after ## Итог")
}

func TestWriteRunLogSection_MissingFile(t *testing.T) {
	err := WriteRunLogSection(filepath.Join(t.TempDir(), "nope.md"), "x")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestWriteRunLogSection_DanglingBeginMarkerKeepsContent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "run.md")
	original := "# task\n" + MarkerBegin + "\nold partial\n" + "## Итог\nverdict: done\n"
	require.NoError(t, os.WriteFile(p, []byte(original), 0o644))
	require.NoError(t, WriteRunLogSection(p, "Score 45/100 · Lucky\n"))
	require.NoError(t, WriteRunLogSection(p, "Score 72/100 · Solid\n"))
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	s := string(data)
	require.Contains(t, s, "## Итог\nverdict: done")
	require.Contains(t, s, "old partial")
	require.Equal(t, 1, strings.Count(s, MarkerEnd))
	require.Equal(t, 2, strings.Count(s, MarkerBegin))
	require.Contains(t, s, "Score 72/100")
	require.NotContains(t, s, "Score 45/100")
}
