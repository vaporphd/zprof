package score

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vaporphd/zprof/internal/stats"
)

func pointsByID(c Card) map[string]float64 {
	m := map[string]float64{}
	for _, p := range c.Penalties {
		m[p.ID] = p.Points
	}
	return m
}

func TestCompute_Run1Golden(t *testing.T) {
	runs := loadRun1(t)
	c := Compute(runs[0], Defaults(), "test")

	pts := pointsByID(c)
	require.InDelta(t, 20, pts["P1"], 0.01, "4/20 tool errors = 0.20 → saturated")
	require.InDelta(t, 10, pts["P2"], 0.01, "2 blind retries / 3")
	require.InDelta(t, 5, pts["P3"], 0.01, "1 reread / 4 reads = 0.25 → half of 10")
	require.InDelta(t, 10, pts["P4"], 0.01, "1 extra tester→implementer round / 2")
	require.InDelta(t, 0, pts["P5"], 0.01)
	require.InDelta(t, 5, pts["P6"], 0.01, "40k wasted / 400k = 0.10 → third of 15")
	require.InDelta(t, 5, pts["P7"], 0.01, "preamble + unparsed = 2 / 4")
	require.Equal(t, 45, c.Score)
	require.Equal(t, "Lucky", c.Tier)
	require.Equal(t, "done", c.Verdict)

	require.Equal(t, 310000, c.Facts.Tokens.Input)
	require.Equal(t, 70000, c.Facts.Tokens.Output)
	require.Equal(t, 20000, c.Facts.Tokens.CacheRead)
	require.Equal(t, 400000, c.Facts.Tokens.Total())
	require.Equal(t, 7, c.Facts.Dispatches, "children only")
	require.Equal(t, 20, c.Facts.ToolCalls)
	require.Equal(t, int64(1800000), c.Facts.DurationMs)
	require.Equal(t, []string{"planner", "implementer", "implementer", "tester", "implementer", "tester", "reviewer"}, c.Facts.Route)
	require.Equal(t, "claude-opus-5-5", c.Facts.Models["reviewer"])
	require.Equal(t, 7, c.Facts.ModelCounts["sonnet"])
	require.Equal(t, 1, c.Facts.ModelCounts["opus"])
	require.Equal(t, ToolCount{"Bash", 9}, c.Facts.ToolsTop[0])

	require.Equal(t, "full", c.Inputs.Confidence)
	require.Equal(t, 8, c.Inputs.Dispatches)
	require.Equal(t, 20, c.Inputs.ToolEvents)

	byRole := map[string]float64{}
	for _, r := range c.Roles {
		byRole[r.Role] = r.Penalty
	}
	require.InDelta(t, 50, byRole["implementer"], 0.01)
	require.InDelta(t, 5, byRole["tester"], 0.01)
	require.InDelta(t, 0, byRole["planner"], 0.01)
	require.Equal(t, "implementer", c.Roles[0].Role, "roles sorted by penalty desc")
	require.Equal(t, 1, c.ScoreSchema)
	require.Equal(t, Defaults().WeightsHash(), c.WeightsHash)
	require.Equal(t, ".zprof/runs/2026-09-26-fixture.md", c.RunLog)
}

func TestPenaltyAttributionSumsToPoints(t *testing.T) {
	runs := loadRun1(t)
	c := Compute(runs[0], Defaults(), "test")
	for _, p := range c.Penalties {
		sum := 0.0
		for _, v := range p.ByRole {
			sum += v
		}
		if p.Points > 0 {
			require.InDelta(t, p.Points, sum, 0.001, p.ID)
		}
	}
}

func TestTierFor(t *testing.T) {
	th := Thresholds{Ideal: 85, Solid: 60}
	cases := []struct {
		verdict string
		score   int
		want    string
	}{
		{"done", 85, "Ideal"}, {"done", 84, "Solid"}, {"done", 60, "Solid"}, {"done", 59, "Lucky"},
		{"approve", 90, "Ideal"}, {"approve-with-fixes", 70, "Solid"},
		{"blocked", 95, "Blocked"}, {"failed", 95, "Failed"}, {"", 95, "Unknown"}, {"weird", 10, "Unknown"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, TierFor(tc.verdict, tc.score, th), "%s/%d", tc.verdict, tc.score)
	}
}

func TestShortModel(t *testing.T) {
	require.Equal(t, "sonnet", ShortModel("claude-sonnet-5"))
	require.Equal(t, "opus", ShortModel("claude-opus-5-5"))
	require.Equal(t, "haiku", ShortModel("claude-haiku-4-5-20251001"))
	require.Equal(t, "gpt-5", ShortModel("gpt-5"))
	require.Equal(t, "?", ShortModel(""))
}

func TestCompute_ConfidencePartialListsRoles(t *testing.T) {
	root := mkDispatch("r", "task-runner", "", "done", "completed", "2026-09-26T10:00:00Z")
	tester := mkDispatch("t", "tester", "r", "done", "completed", "2026-09-26T10:05:00Z")
	tester.TranscriptCaptured = false
	run := BuildRuns([]stats.Dispatch{root, tester}, nil)[0]
	c := Compute(run, Defaults(), "test")
	require.Equal(t, "partial", c.Inputs.Confidence)
	require.Equal(t, []string{"tester"}, c.Inputs.TranscriptsMissing)
	require.Equal(t, 100, c.Score)
	require.Equal(t, "Ideal", c.Tier)
}

// mkDispatch builds a complete, captured dispatch. parent "" = no parent.
func mkDispatch(id, role, parent, verdict, status, ts string) stats.Dispatch {
	d := stats.Dispatch{
		DispatchID: "claude-code:s:" + id, Role: role, Verdict: verdict, Status: status,
		DispatchComplete: true, TsUTC: ts, TranscriptCaptured: true, ModelResolved: "claude-sonnet-5",
		TokensInput: 1000, TokensOutput: 100,
	}
	if parent != "" {
		d.ParentDispatchID = "claude-code:s:" + parent
	}
	d.Timestamp, _ = time.Parse(time.RFC3339, ts)
	return d
}

func bptr(b bool) *bool { return &b }

func ev(seq int, tool, hash, target string, isErr bool) ToolEvent {
	return ToolEvent{DispatchID: "claude-code:s:x", Seq: seq, Tool: tool, InputHash: hash, Target: target, IsError: bptr(isErr)}
}
