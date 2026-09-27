package score

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vaporphd/zprof/internal/stats"
)

func loadRun1(t *testing.T) []Run {
	t.Helper()
	ds, _, err := stats.ReadDispatches(filepath.Join(fixtureDir(), "dispatches.jsonl"))
	require.NoError(t, err)
	evs, err := ReadToolEvents(filepath.Join(fixtureDir(), "tool-events.jsonl"))
	require.NoError(t, err)
	return BuildRuns(ds, evs)
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
