package score

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vaporphd/zprof/internal/stats"
)

func loadRun1(t *testing.T) []Run {
	t.Helper()
	ds, _, err := stats.ReadDispatches(filepath.Join(fixtureDir(), "dispatches.jsonl"))
	require.NoError(t, err)
	evs, err := ReadToolEvents(filepath.Join(fixtureDir(), "tool-events.jsonl"))
	require.NoError(t, err)
	guardEvs, err := ReadGuardEvents(filepath.Join(fixtureDir(), "guard-events.jsonl"))
	require.NoError(t, err)
	return AttachGuardEvents(BuildRuns(ds, evs), guardEvs)
}

func TestReadToolEvents_MissingFileIsEmpty(t *testing.T) {
	evs, err := ReadToolEvents(filepath.Join(t.TempDir(), "nope.jsonl"))
	require.NoError(t, err)
	require.Empty(t, evs)
}

func TestReadToolEvents_DedupBySeqLaterWins(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tool-events.jsonl")
	require.NoError(t, writeFile(p, `{"schema_version":1,"dispatch_id":"d","seq":1,"tool":"Read","input_hash":"a","is_error":false}
{"schema_version":1,"dispatch_id":"d","seq":1,"tool":"Read","input_hash":"a","is_error":true}
not json
{"schema_version":1,"dispatch_id":"d","seq":2,"tool":"Bash","input_hash":"b"}
`))
	evs, err := ReadToolEvents(p)
	require.NoError(t, err)
	require.Len(t, evs, 2)
	require.True(t, *evs[0].IsError)
	require.Nil(t, evs[1].IsError, "missing is_error stays nil")
}

func TestBuildRuns_GroupsRootAndDescendants(t *testing.T) {
	runs := loadRun1(t)
	require.Len(t, runs, 1)
	r := runs[0]
	require.Equal(t, "claude-code:s1:t0", r.ID)
	require.Equal(t, "task-runner", r.Root.Role)
	require.Len(t, r.Dispatches, 8, "root + 7 children (t7 has no ext.run_id and is found via parent walk)")
	require.Len(t, r.Steps, 7)
	roles := make([]string, 0, len(r.Steps))
	for _, s := range r.Steps {
		roles = append(roles, s.Role)
	}
	require.Equal(t, []string{"planner", "implementer", "implementer", "tester", "implementer", "tester", "reviewer"}, roles)
	require.Equal(t, ".zprof/runs/2026-09-26-fixture.md", r.RunLog)
	require.Len(t, r.Events["claude-code:s1:t2"], 10)
	require.Equal(t, 1, r.Events["claude-code:s1:t2"][0].Seq)
}

func TestBuildRuns_IgnoresIncompleteRunnerAndOrphans(t *testing.T) {
	ds := []stats.Dispatch{
		{DispatchID: "claude-code:s:r1", Role: "task-runner", DispatchComplete: false, TsUTC: "2026-09-26T10:00:00Z"},
		{DispatchID: "claude-code:s:x", Role: "explorer", DispatchComplete: true, TsUTC: "2026-09-26T10:01:00Z"},
		{DispatchID: "claude-code:s:r2", Role: "task-runner", DispatchComplete: true, TsUTC: "2026-09-26T11:00:00Z"},
		{DispatchID: "claude-code:s:c", Role: "tester", ParentDispatchID: "claude-code:s:r2", DispatchComplete: true, TsUTC: "2026-09-26T11:05:00Z"},
	}
	runs := BuildRuns(ds, nil)
	require.Len(t, runs, 1)
	require.Equal(t, "claude-code:s:r2", runs[0].ID)
	require.Len(t, runs[0].Dispatches, 2)
}

func TestLatestAndFindRun(t *testing.T) {
	ds := []stats.Dispatch{
		{DispatchID: "claude-code:s:r1", Role: "task-runner", DispatchComplete: true, TsUTC: "2026-09-26T10:00:00Z", Ext: map[string]any{"run_log": ".zprof/runs/2026-09-26-a.md"}},
		{DispatchID: "claude-code:s:r2", Role: "task-runner", DispatchComplete: true, TsUTC: "2026-09-26T11:00:00Z", Ext: map[string]any{"run_log": ".zprof/runs/2026-09-26-b.md"}},
	}
	runs := BuildRuns(ds, nil)
	require.Equal(t, "claude-code:s:r2", LatestRun(runs).ID)
	require.Equal(t, "claude-code:s:r1", FindRun(runs, "claude-code:s:r1").ID, "exact id")
	require.Equal(t, "claude-code:s:r1", FindRun(runs, "2026-09-26-a").ID, "run_log suffix")
	require.Equal(t, "claude-code:s:r2", FindRun(runs, ":r2").ID, "id suffix")
	require.Nil(t, FindRun(runs, "zzz"))
	require.Nil(t, LatestRun(nil))
}

func writeFile(path, content string) error { return os.WriteFile(path, []byte(content), 0o644) }

func TestBuildRuns_EventsForUnknownDispatchAreDropped(t *testing.T) {
	root := mkDispatch("r", "task-runner", "", "done", "completed", "2026-09-26T10:00:00Z")
	x := mkDispatch("x", "implementer", "r", "done", "completed", "2026-09-26T10:05:00Z")
	orphan := mkDispatch("o", "explorer", "", "done", "completed", "2026-09-26T10:06:00Z")
	evs := []ToolEvent{
		ev(2, "Read", "h2", "/a", false),
		ev(1, "Read", "h1", "/b", false),
		{DispatchID: "claude-code:s:o", Seq: 1, Tool: "Read"},       // dispatch outside any run
		{DispatchID: "claude-code:s:missing", Seq: 1, Tool: "Read"}, // no such dispatch
	}
	runs := BuildRuns([]stats.Dispatch{root, x, orphan}, evs)
	require.Len(t, runs, 1)
	require.Len(t, runs[0].Events, 1)
	got := runs[0].Events["claude-code:s:x"]
	require.Len(t, got, 2)
	require.Equal(t, 1, got[0].Seq, "events sorted by seq")
}

func TestReadGuardEvents_MissingFileIsEmpty(t *testing.T) {
	evs, err := ReadGuardEvents(filepath.Join(t.TempDir(), "nope.jsonl"))
	require.NoError(t, err)
	require.Empty(t, evs)
}

// TestReadGuardEvents_MalformedLineSkippedNullDispatchIDKept regression-tests
// the most dangerous copy-paste mistake from ReadToolEvents (ADR-0008 H1):
// dispatch_id: null is the legitimate main/unknown case and must survive.
func TestReadGuardEvents_MalformedLineSkippedNullDispatchIDKept(t *testing.T) {
	p := filepath.Join(t.TempDir(), "guard-events.jsonl")
	require.NoError(t, writeFile(p, `{"ts":"2026-09-26T09:00:00Z","session_id":"s1","event":"pre-tool","role":"implementer","dispatch_id":"t1","tool":"Bash","rule":"force_push","decision":"deny","target":"git push","input_hash":"h1"}
not json
{"ts":"2026-09-26T09:05:00Z","session_id":"s1","event":"pre-tool","role":"main","dispatch_id":null,"tool":"Bash","rule":"force_push","decision":"deny","target":"git push","input_hash":"h2"}
`))
	evs, err := ReadGuardEvents(p)
	require.NoError(t, err)
	require.Len(t, evs, 2, "malformed line dropped, the two valid rows around it kept")
	require.Equal(t, "t1", evs[0].DispatchID)
	require.Equal(t, "", evs[1].DispatchID, "null dispatch_id must NOT be dropped — it is the main/unknown fallback entry point")
	require.Equal(t, "main", evs[1].Role)
}

// guardDispatch builds a stats.Dispatch with explicit SessionID/DurationMs
// control, for AttachGuardEvents tests that mkDispatch's shortcuts don't cover.
func guardDispatch(id, role, parent, session, ts string, durationMs int64) stats.Dispatch {
	d := stats.Dispatch{
		DispatchID: "claude-code:" + session + ":" + id, Role: role, Status: "completed",
		SessionID: session, DispatchComplete: true, TsUTC: ts, DurationMs: durationMs,
	}
	if role == "task-runner" {
		d.Verdict = "done"
	}
	if parent != "" {
		d.ParentDispatchID = "claude-code:" + session + ":" + parent
	}
	d.Timestamp, _ = time.Parse(time.RFC3339, ts)
	return d
}

func TestAttachGuardEvents_DispatchIDMatchesRawAndComposite(t *testing.T) {
	root := guardDispatch("r", "task-runner", "", "s1", "2026-09-26T10:00:00Z", 1800000)
	x := guardDispatch("x", "implementer", "r", "s1", "2026-09-26T10:05:00Z", 60000)
	runs := BuildRuns([]stats.Dispatch{root, x}, nil)

	raw := GuardEvent{Ts: "2026-09-26T10:05:30Z", DispatchID: "x", Decision: "deny", Rule: "force_push"}
	composite := GuardEvent{Ts: "2026-09-26T10:05:40Z", DispatchID: "claude-code:s1:x", Decision: "block", Rule: "return_format"}
	runs = AttachGuardEvents(runs, []GuardEvent{raw, composite})

	require.Len(t, runs, 1)
	require.Len(t, runs[0].GuardEvents, 2, "both the raw toolUseId and the composite id resolve to the same run")
}

func TestAttachGuardEvents_DispatchIDOutsideAnyRunDropped(t *testing.T) {
	root := guardDispatch("r", "task-runner", "", "s1", "2026-09-26T10:00:00Z", 1800000)
	runs := BuildRuns([]stats.Dispatch{root}, nil)
	runs = AttachGuardEvents(runs, []GuardEvent{
		{Ts: "2026-09-26T10:05:00Z", DispatchID: "no-such-dispatch", Decision: "deny"},
	})
	require.Empty(t, runs[0].GuardEvents, "no temporal fallback for events that carry a dispatch_id")
}

func TestAttachGuardEvents_TemporalFallbackPicksEnclosingRunAmongTwo(t *testing.T) {
	root1 := guardDispatch("r1", "task-runner", "", "s1", "2026-09-26T10:00:00Z", 1800000) // window [09:30, 10:00]
	root2 := guardDispatch("r2", "task-runner", "", "s1", "2026-09-26T11:00:00Z", 1800000) // window [10:30, 11:00]
	runs := BuildRuns([]stats.Dispatch{root1, root2}, nil)
	require.Len(t, runs, 2)

	mainEvent := GuardEvent{Ts: "2026-09-26T10:45:00Z", SessionID: "s1", Role: "main", Decision: "deny", Rule: "force_push"}
	runs = AttachGuardEvents(runs, []GuardEvent{mainEvent})
	require.Empty(t, runs[0].GuardEvents, "10:45 is outside run1's [09:30,10:00] window")
	require.Len(t, runs[1].GuardEvents, 1, "10:45 falls inside run2's [10:30,11:00] window")

	// Regression on "≤ largest preceding root" from the plan's rejected
	// fallback: ts between end1 and start2 belongs to neither window.
	runs2 := BuildRuns([]stats.Dispatch{root1, root2}, nil)
	between := GuardEvent{Ts: "2026-09-26T10:15:00Z", SessionID: "s1", Decision: "deny"}
	runs2 = AttachGuardEvents(runs2, []GuardEvent{between})
	require.Empty(t, runs2[0].GuardEvents)
	require.Empty(t, runs2[1].GuardEvents, "gap between the two runs' windows is not covered by either")
}

func TestAttachGuardEvents_TemporalFallbackDropsUnparsableOrOutOfRangeTs(t *testing.T) {
	root := guardDispatch("r", "task-runner", "", "s1", "2026-09-26T10:00:00Z", 1800000) // window [09:30, 10:00]
	runs := BuildRuns([]stats.Dispatch{root}, nil)
	events := []GuardEvent{
		{Ts: "not-a-timestamp", Decision: "deny"},
		{Ts: "", Decision: "deny"},
		{Ts: "2026-09-26T09:00:00Z", Decision: "deny"}, // before the window
	}
	runs = AttachGuardEvents(runs, events)
	require.Empty(t, runs[0].GuardEvents)
}

func TestAttachGuardEvents_TemporalFallbackSessionMismatchDropped(t *testing.T) {
	root := guardDispatch("r", "task-runner", "", "s1", "2026-09-26T10:00:00Z", 1800000) // window [09:30, 10:00]
	runs := BuildRuns([]stats.Dispatch{root}, nil)
	runs = AttachGuardEvents(runs, []GuardEvent{
		{Ts: "2026-09-26T09:45:00Z", SessionID: "s2", Decision: "deny"},
	})
	require.Empty(t, runs[0].GuardEvents, "event's session_id differs from the root's — different session, not the same runner")
}

func TestAttachGuardEvents_ZeroDurationRootNotACandidate(t *testing.T) {
	root := guardDispatch("r", "task-runner", "", "s1", "2026-09-26T10:00:00Z", 0)
	runs := BuildRuns([]stats.Dispatch{root}, nil)
	runs = AttachGuardEvents(runs, []GuardEvent{
		{Ts: "2026-09-26T10:00:00Z", SessionID: "s1", Decision: "deny"},
	})
	require.Empty(t, runs[0].GuardEvents, "DurationMs <= 0 means \"active\" is undefined — the run is not a candidate")
}
