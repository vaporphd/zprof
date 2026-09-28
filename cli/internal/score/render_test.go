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

	require.Equal(t, "Score 40/100 · Lucky · done · 2026-09-26-fixture · confidence full", lines[0])
	require.Equal(t, "400k tok (in 310k · out 70k · cache 20k) · 7 dispatch · 20 tool calls · 30 min · sonnet×7 opus×1", lines[1])
	// top-3 findings by points, desc
	require.True(t, strings.HasPrefix(lines[2], "−20 P1 "), lines[2])
	require.Contains(t, lines[2], "4/20 tool errors (20%)")
	require.True(t, strings.HasPrefix(lines[3], "−10 P"), lines[3]) // P2 or P4 (both 10) — order by id
	require.Contains(t, lines[3], "P2 implementer: 2× `swift test --package-path Packages/Core` без правок между")
	require.True(t, strings.HasPrefix(lines[4], "−10 P4 "), lines[4])
	// P7 (guard: 2 deny) is outside top-3 by points but pinned after it — ADR-0008 H5.
	require.Equal(t, "−10 P7 4 нарушений контракта (implementer, main) (guard: 2 deny)", lines[5])
	require.Equal(t, "", lines[6])
	require.Equal(t, []string{"role", "tokens", "calls", "err", "penalty", "model"}, strings.Fields(lines[7]))
	require.Equal(t, []string{"implementer", "240k", "14", "3", "−53", "sonnet"}, strings.Fields(lines[8]))
	require.Equal(t, []string{"tester", "80k", "3", "1", "−5", "sonnet"}, strings.Fields(lines[9]))
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

// TestRenderCard_GuardDeniesPinsPenaltyOutsideTop3 is a synthetic, minimal
// reproduction of the golden fixture's P7 pinning behavior (ADR-0008 H5),
// isolated from the rest of TestRenderCard_Run1's assertions.
func TestRenderCard_GuardDeniesPinsPenaltyOutsideTop3(t *testing.T) {
	c := Card{Score: 55, Tier: "Lucky", Verdict: "done", RunID: "claude-code:s:abcdefgh",
		Facts: Facts{ModelCounts: map[string]int{}},
		Penalties: []Penalty{
			{ID: "P1", Points: 30, Detail: "p1 detail"},
			{ID: "P2", Points: 20, Detail: "p2 detail"},
			{ID: "P3", Points: 15, Detail: "p3 detail"},
			{ID: "P7", Points: 3, Detail: "p7 detail (guard: 1 deny)", GuardDenies: 1},
		}}
	out := RenderCard(c)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Equal(t, "−30 P1 p1 detail", lines[2])
	require.Equal(t, "−20 P2 p2 detail", lines[3])
	require.Equal(t, "−15 P3 p3 detail", lines[4])
	require.Equal(t, "−3 P7 p7 detail (guard: 1 deny)", lines[5], "P7 is pinned after top-3 because GuardDenies > 0")
}

// TestRenderCard_ZeroGuardDeniesUnchanged pins the AC4 invariant: a card
// where no penalty has GuardDenies renders byte-for-byte as before this
// feature — no pinned line, no empty extra slot.
func TestRenderCard_ZeroGuardDeniesUnchanged(t *testing.T) {
	c := Card{Score: 70, Tier: "Solid", Verdict: "done", RunID: "claude-code:s:abcdefgh",
		Facts: Facts{ModelCounts: map[string]int{}},
		Penalties: []Penalty{
			{ID: "P1", Points: 30, Detail: "p1 detail"},
			{ID: "P7", Points: 0, Detail: "", GuardDenies: 0},
		}}
	out := RenderCard(c)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Equal(t, "−30 P1 p1 detail", lines[2])
	require.Equal(t, "", lines[3], "no pinned P7 line — GuardDenies is 0")
}

func TestHumanTokens(t *testing.T) {
	require.Equal(t, "0", humanTokens(0))
	require.Equal(t, "999", humanTokens(999))
	require.Equal(t, "1k", humanTokens(1000))
	require.Equal(t, "20k", humanTokens(20000))
	require.Equal(t, "1.2M", humanTokens(1_234_000))
}
