package score

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/vaporphd/zprof/internal/stats"
)

// oneDispatchRun: root + one implementer "x" carrying the given events.
func oneDispatchRun(events ...ToolEvent) Run {
	root := mkDispatch("r", "task-runner", "", "done", "completed", "2026-09-26T10:00:00Z")
	x := mkDispatch("x", "implementer", "r", "done", "completed", "2026-09-26T10:05:00Z")
	return BuildRuns([]stats.Dispatch{root, x}, events)[0]
}

func TestBlindRetries_CountsRepeatsAfterErrorWithoutMutation(t *testing.T) {
	run := oneDispatchRun(
		ev(1, "Bash", "h-test", "swift test", true),
		ev(2, "Bash", "h-test", "swift test", true),
		ev(3, "Bash", "h-test", "swift test", false),
	)
	p := computeP2(run, Defaults())
	require.Equal(t, 2.0, p.value)
	require.Equal(t, 2.0, p.byRole["implementer"])
}

func TestBlindRetries_BuildBetweenDoesNotReset(t *testing.T) {
	run := oneDispatchRun(
		ev(1, "Bash", "h-test", "swift test", true),
		ev(2, "Bash", "h-build", "swift build", false),
		ev(3, "Bash", "h-test", "swift test", true),
	)
	require.Equal(t, 1.0, computeP2(run, Defaults()).value)
}

func TestBlindRetries_EditBetweenResets(t *testing.T) {
	run := oneDispatchRun(
		ev(1, "Bash", "h-test", "swift test", true),
		ev(2, "Edit", "h-e", "/p/a.swift", false),
		ev(3, "Bash", "h-test", "swift test", true),
	)
	require.Equal(t, 0.0, computeP2(run, Defaults()).value)
}

func TestBlindRetries_MutatingCommandStillCounts(t *testing.T) {
	run := oneDispatchRun(
		ev(1, "Bash", "h-sed", "sed -i 's/a/b/' f", true),
		ev(2, "Bash", "h-sed", "sed -i 's/a/b/' f", true),
	)
	require.Equal(t, 1.0, computeP2(run, Defaults()).value, "check happens before the mutating reset")
}

func TestBlindRetries_MutatingBashResetsOthers(t *testing.T) {
	run := oneDispatchRun(
		ev(1, "Bash", "h-test", "swift test", true),
		ev(2, "Bash", "h-w", "cat > f.swift <<'EOF'", false),
		ev(3, "Bash", "h-test", "swift test", true),
	)
	require.Equal(t, 0.0, computeP2(run, Defaults()).value)
}

func TestRereads_SkipsDispatchesWithFewerThanFourReads(t *testing.T) {
	run := oneDispatchRun(
		ev(1, "Read", "a", "/a", false), ev(2, "Read", "a", "/a", false), ev(3, "Read", "b", "/b", false),
	)
	require.Equal(t, 0.0, computeP3(run, Defaults()).value)
}

func TestRereads_MutationResetsSeenPaths(t *testing.T) {
	run := oneDispatchRun(
		ev(1, "Read", "a", "/a", false), ev(2, "Read", "b", "/b", false),
		ev(3, "Edit", "e", "/a", false),
		ev(4, "Read", "a", "/a", false), ev(5, "Read", "c", "/c", false),
	)
	require.Equal(t, 0.0, computeP3(run, Defaults()).value, "4 reads, no reread after the edit")
}

func TestRereads_CountsRepeatWithoutMutation(t *testing.T) {
	run := oneDispatchRun(
		ev(1, "Read", "a", "/a", false), ev(2, "Read", "b", "/b", false),
		ev(3, "Read", "a", "/a", false), ev(4, "Read", "c", "/c", false),
	)
	p := computeP3(run, Defaults())
	require.InDelta(t, 0.25, p.value, 0.001)
	require.Equal(t, 1.0, p.byRole["implementer"])
}

func TestToolErrors_RateAndAttribution(t *testing.T) {
	root := mkDispatch("r", "task-runner", "", "done", "completed", "2026-09-26T10:00:00Z")
	a := mkDispatch("a", "implementer", "r", "done", "completed", "2026-09-26T10:01:00Z")
	b := mkDispatch("b", "tester", "r", "done", "completed", "2026-09-26T10:02:00Z")
	evs := []ToolEvent{
		{DispatchID: a.DispatchID, Seq: 1, Tool: "Bash", InputHash: "1", IsError: bptr(true)},
		{DispatchID: a.DispatchID, Seq: 2, Tool: "Bash", InputHash: "2", IsError: bptr(false)},
		{DispatchID: b.DispatchID, Seq: 1, Tool: "Bash", InputHash: "3", IsError: bptr(true)},
		{DispatchID: b.DispatchID, Seq: 2, Tool: "Grep", InputHash: "4"}, // no result → excluded from denominator
	}
	run := BuildRuns([]stats.Dispatch{root, a, b}, evs)[0]
	p := computeP1(run, Defaults())
	require.InDelta(t, 2.0/3.0, p.value, 0.001)
	require.Equal(t, 1.0, p.byRole["implementer"])
	require.Equal(t, 1.0, p.byRole["tester"])
}

func TestLoopRounds_OnlyFailedTesterFollowedByImplementer(t *testing.T) {
	root := mkDispatch("r", "task-runner", "", "done", "completed", "2026-09-26T10:00:00Z")
	steps := []stats.Dispatch{
		mkDispatch("1", "implementer", "r", "done", "completed", "2026-09-26T10:01:00Z"),
		mkDispatch("2", "tester", "r", "failed", "completed", "2026-09-26T10:02:00Z"),
		mkDispatch("3", "implementer", "r", "done", "completed", "2026-09-26T10:03:00Z"),
		mkDispatch("4", "tester", "r", "failed", "completed", "2026-09-26T10:04:00Z"),
		mkDispatch("5", "implementer", "r", "done", "completed", "2026-09-26T10:05:00Z"),
		mkDispatch("6", "tester", "r", "failed", "completed", "2026-09-26T10:06:00Z"),
		mkDispatch("7", "reviewer", "r", "block", "completed", "2026-09-26T10:07:00Z"), // failed tester → reviewer: not a round
	}
	run := BuildRuns(append([]stats.Dispatch{root}, steps...), nil)[0]
	p4 := computeP4(run, Defaults())
	require.Equal(t, 2.0, p4.value)
	require.Equal(t, 2.0, p4.byRole["implementer"])
	p5 := computeP5(run, Defaults())
	require.Equal(t, 1.0, p5.value)
	require.Equal(t, 1.0, p5.byRole["implementer"])
	c := Compute(run, Defaults(), "t")
	pts := pointsByID(c)
	require.InDelta(t, 20, pts["P4"], 0.01, "2 extra rounds saturate")
	require.InDelta(t, 5, pts["P5"], 0.01)
}

// TestWastedTokens_KilledAndUnparsedAuditor: since #20 (ADR 0003) auditors
// are no longer in ExemptRoles — their contract now starts with `verdict:`
// like everyone else's, so an unparsed auditor response is a real P6 hit,
// not a role to look away from.
func TestWastedTokens_KilledAndUnparsedAuditor(t *testing.T) {
	root := mkDispatch("r", "task-runner", "", "done", "completed", "2026-09-26T10:00:00Z")
	root.TokensInput, root.TokensOutput = 0, 0
	impl := mkDispatch("i", "implementer", "r", "", "killed", "2026-09-26T10:01:00Z")
	impl.TokensInput, impl.TokensOutput = 3000, 0
	aud := mkDispatch("a", "auditor", "r", "", "completed", "2026-09-26T10:02:00Z")
	aud.ReturnParsed = bptr(false) // e.g. legacy completion: response
	aud.TokensInput, aud.TokensOutput = 5000, 0
	ok := mkDispatch("o", "tester", "r", "done", "completed", "2026-09-26T10:03:00Z")
	ok.TokensInput, ok.TokensOutput = 2000, 0
	run := BuildRuns([]stats.Dispatch{root, impl, aud, ok}, nil)[0]
	p := computeP6(run, Defaults())
	require.InDelta(t, 0.80, p.value, 0.001, "3000+5000 of 10000")
	require.Equal(t, 3000.0, p.byRole["implementer"])
	require.Equal(t, 5000.0, p.byRole["auditor"])
}

func TestP7_BlockedDoesNotCountMissingArtifact(t *testing.T) {
	root := mkDispatch("r", "task-runner", "", "blocked", "completed", "2026-09-26T10:00:00Z")
	root.ArtifactExists = bptr(false)
	impl := mkDispatch("i", "implementer", "r", "blocked", "completed", "2026-09-26T10:01:00Z")
	impl.ArtifactExists = bptr(false)
	run := BuildRuns([]stats.Dispatch{root, impl}, nil)[0]
	require.Equal(t, 0.0, computeP7(run, Defaults()).value)
	c := Compute(run, Defaults(), "t")
	require.Equal(t, "Blocked", c.Tier)
	require.Equal(t, 100, c.Score)
}

func TestP7_CountsFourKinds(t *testing.T) {
	root := mkDispatch("r", "task-runner", "", "done", "completed", "2026-09-26T10:00:00Z")
	d := mkDispatch("d", "implementer", "r", "done", "completed", "2026-09-26T10:01:00Z")
	d.HasPreamble = bptr(true)
	d.ArtifactExists = bptr(false)
	d.NextIsReachable = bptr(false)
	d.ReturnParsed = bptr(false)
	run := BuildRuns([]stats.Dispatch{root, d}, nil)[0]
	p := computeP7(run, Defaults())
	require.Equal(t, 4.0, p.value)
	require.Equal(t, 4.0, p.byRole["implementer"])
	require.InDelta(t, 10, pointsByID(Compute(run, Defaults(), "t"))["P7"], 0.01)
}

func TestIsMutating_PrefersFlag(t *testing.T) {
	cfg := Defaults()
	yes, no := true, false
	long := ToolEvent{Tool: "Bash", Target: "cd /x/y/z && echo prefix-only-head", Mutating: &yes}
	require.True(t, isMutating(long, cfg), "flag from the full command wins over a non-matching head")
	sed := ToolEvent{Tool: "Bash", Target: "sed -i s/a/b/ f.go", Mutating: &no}
	require.False(t, isMutating(sed, cfg), "explicit false wins over a matching target")
	legacy := ToolEvent{Tool: "Bash", Target: "sed -i s/a/b/ f.go"}
	require.True(t, isMutating(legacy, cfg), "nil flag falls back to target")
	require.False(t, isMutating(ToolEvent{Tool: "Bash", Target: "swift test"}, cfg))
	require.True(t, isMutating(ToolEvent{Tool: "Edit", Mutating: &no}, cfg), "mutating tools stay mutating")
}

func TestP5_ConfigurableVerdicts(t *testing.T) {
	root := mkDispatch("r", "task-runner", "", "done", "completed", "2026-09-26T10:00:00Z")
	rv1 := mkDispatch("v1", "reviewer", "r", "changes-requested", "completed", "2026-09-26T10:01:00Z")
	rv2 := mkDispatch("v2", "reviewer", "r", "approve-with-fixes", "completed", "2026-09-26T10:02:00Z")
	rv3 := mkDispatch("v3", "reviewer", "r", "block", "completed", "2026-09-26T10:03:00Z")
	run := BuildRuns([]stats.Dispatch{root, rv1, rv2, rv3}, nil)[0]
	require.Equal(t, 2.0, computeP5(run, Defaults()).value, "changes-requested + block; approve-with-fixes is not a block")

	cfg := Defaults()
	cfg.ReviewBlockVerdicts = map[string]bool{"approve-with-fixes": true}
	require.Equal(t, 1.0, computeP5(run, cfg).value)
}

func TestTruncate_RunesNotBytes(t *testing.T) {
	s := "проверить длинную команду"
	got := truncate(s, 10)
	require.True(t, utf8.ValidString(got), "must not cut a multi-byte rune: %q", got)
	require.Equal(t, "проверить…", got)
	require.Equal(t, 10, utf8.RuneCountInString(got))
	require.Equal(t, "короткая", truncate("короткая", 10), "short strings pass through")
}
