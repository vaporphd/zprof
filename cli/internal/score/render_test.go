package score

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderCard_Run1(t *testing.T) {
	c := Compute(loadRun1(t)[0], Defaults(), "test")
	out := RenderCard(c)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	require.Equal(t, "Score 45/100 · Lucky · done · 2026-09-26-fixture · confidence full", lines[0])
	require.Equal(t, "400k tok (in 310k · out 70k · cache 20k) · 7 dispatch · 20 tool calls · 30 min · sonnet×7 opus×1", lines[1])
	// top-3 findings by points, desc
	require.True(t, strings.HasPrefix(lines[2], "−20 P1 "), lines[2])
	require.Contains(t, lines[2], "4/20 tool errors (20%)")
	require.True(t, strings.HasPrefix(lines[3], "−10 P"), lines[3]) // P2 or P4 (both 10) — order by id
	require.Contains(t, lines[3], "P2 implementer: 2× `swift test --package-path Packages/Core` без правок между")
	require.True(t, strings.HasPrefix(lines[4], "−10 P4 "), lines[4])
	require.Equal(t, "", lines[5])
	require.Equal(t, []string{"role", "tokens", "calls", "err", "penalty", "model"}, strings.Fields(lines[6]))
	require.Equal(t, []string{"implementer", "240k", "14", "3", "−50", "sonnet"}, strings.Fields(lines[7]))
	require.Equal(t, []string{"tester", "80k", "3", "1", "−5", "sonnet"}, strings.Fields(lines[8]))
	require.Contains(t, out, "tools: Bash 9 · Read 7 · Edit 3 · Write 1")
	require.NotContains(t, out, "missing transcripts")
	require.NotContains(t, out, "run log: missing")
}

func TestRenderCard_PartialAndNoRunLog(t *testing.T) {
	c := Card{Score: 100, Tier: "Ideal", Verdict: "done", RunID: "claude-code:s:abcdefgh",
		Inputs: Inputs{Confidence: "partial", TranscriptsMissing: []string{"tester"}},
		Facts:  Facts{ModelCounts: map[string]int{}}}
	out := RenderCard(c)
	require.Contains(t, out, "Score 100/100 · Ideal · done · abcdefgh · confidence partial")
	require.Contains(t, out, "missing transcripts: tester")
	require.Contains(t, out, "run log: missing")
	require.Contains(t, out, "no penalties")
}

func TestHumanTokens(t *testing.T) {
	require.Equal(t, "0", humanTokens(0))
	require.Equal(t, "999", humanTokens(999))
	require.Equal(t, "1k", humanTokens(1000))
	require.Equal(t, "20k", humanTokens(20000))
	require.Equal(t, "1.2M", humanTokens(1_234_000))
}
