package score

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/vaporphd/zprof/internal/stats"
)

// ToolEvent is one row of .agentlog/tool-events.jsonl (telemetry.yaml `tool_events`).
type ToolEvent struct {
	SchemaVersion int    `json:"schema_version"`
	DispatchID    string `json:"dispatch_id"`
	Seq           int    `json:"seq"`
	Ts            string `json:"ts"`
	Tool          string `json:"tool"`
	InputHash     string `json:"input_hash"`
	Target        string `json:"target"`
	IsError       *bool  `json:"is_error,omitempty"`
	// Mutating is set by the collector for Bash events, matched against the
	// full command (Target is cut to 60 chars). nil on rows from older
	// collectors — isMutating then falls back to matching Target.
	Mutating    *bool `json:"mutating,omitempty"`
	ResultChars int   `json:"result_chars"`
}

// ReadToolEvents parses tool-events.jsonl. A missing file yields (nil, nil).
// Malformed lines are skipped. Duplicate (dispatch_id, seq) keys keep the
// later row — the collector may re-append after a crash between write and
// state.json save.
func ReadToolEvents(path string) ([]ToolEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	type key struct {
		id  string
		seq int
	}
	index := map[key]int{}
	var out []ToolEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)
	for sc.Scan() {
		var ev ToolEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil || ev.DispatchID == "" {
			continue
		}
		k := key{ev.DispatchID, ev.Seq}
		if i, dup := index[k]; dup {
			out[i] = ev
			continue
		}
		index[k] = len(out)
		out = append(out, ev)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return out, nil
}

// GuardEvent is one row of .agentlog/guard-events.jsonl (guard spec §7).
type GuardEvent struct {
	Ts         string `json:"ts"`
	SessionID  string `json:"session_id"`
	Event      string `json:"event"`
	Role       string `json:"role"`
	DispatchID string `json:"dispatch_id"` // raw toolUseId; "" for main/unknown (JSON null)
	Tool       string `json:"tool"`
	Rule       string `json:"rule"`
	Decision   string `json:"decision"`
	Target     string `json:"target"`
	InputHash  string `json:"input_hash"`
}

// ReadGuardEvents parses guard-events.jsonl (guard spec §7, ADR-0008 H1). A
// missing file yields (nil, nil). Malformed lines are skipped. Unlike
// ReadToolEvents, an empty dispatch_id is NOT a reason to skip a row — it is
// the legitimate value for role main/unknown and the only entry point into
// the temporal fallback in AttachGuardEvents. No dedup: rows carry no seq
// and the collector never rewrites the file — events are returned in file
// order. Filtering by decision/event is left to the caller (P7, `zprof
// stats`).
func ReadGuardEvents(path string) ([]GuardEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var out []GuardEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)
	for sc.Scan() {
		var ev GuardEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue
		}
		out = append(out, ev)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return out, nil
}

// Run is one task-runner dispatch with everything it spawned.
type Run struct {
	ID          string
	Root        stats.Dispatch
	Dispatches  []stats.Dispatch
	Steps       []stats.Dispatch
	Events      map[string][]ToolEvent
	GuardEvents []GuardEvent // in file order; flat — GuardEvent carries its own Role
	RunLog      string
}

const maxChainHops = 16

// runIDFor resolves the run a dispatch belongs to: ext.run_id when present,
// otherwise the nearest complete task-runner ancestor via parent_dispatch_id.
func runIDFor(d stats.Dispatch, byID map[string]stats.Dispatch, roots map[string]bool) string {
	if v, ok := d.Ext["run_id"].(string); ok && roots[v] {
		return v
	}
	cur, hops := d, 0
	for hops < maxChainHops {
		if roots[cur.DispatchID] {
			return cur.DispatchID
		}
		parent, ok := byID[cur.ParentDispatchID]
		if !ok {
			return ""
		}
		cur = parent
		hops++
	}
	return ""
}

// BuildRuns groups dispatches into runs. Only complete task-runner dispatches
// are roots; dispatches with no runner ancestor are dropped. Runs are sorted
// by root timestamp ascending; Steps by timestamp then id; Events by Seq.
func BuildRuns(ds []stats.Dispatch, evs []ToolEvent) []Run {
	byID := make(map[string]stats.Dispatch, len(ds))
	roots := map[string]bool{}
	for _, d := range ds {
		// later rows (higher seq, or re-collection) win
		byID[d.DispatchID] = d
		if d.Role == "task-runner" && d.DispatchComplete {
			roots[d.DispatchID] = true
		}
	}
	runs := map[string]*Run{}
	for id := range roots {
		root := byID[id]
		r := &Run{ID: id, Root: root, Events: map[string][]ToolEvent{}}
		if rl, ok := root.Ext["run_log"].(string); ok {
			r.RunLog = rl
		}
		runs[id] = r
	}
	runOf := make(map[string]string, len(byID)) // dispatchID → runID
	for _, d := range byID {
		rid := runIDFor(d, byID, roots)
		if rid == "" {
			continue
		}
		runOf[d.DispatchID] = rid
		r := runs[rid]
		r.Dispatches = append(r.Dispatches, d)
		if d.ParentDispatchID == rid {
			r.Steps = append(r.Steps, d)
		}
	}
	for _, ev := range evs {
		if rid, ok := runOf[ev.DispatchID]; ok {
			runs[rid].Events[ev.DispatchID] = append(runs[rid].Events[ev.DispatchID], ev)
		}
	}
	out := make([]Run, 0, len(runs))
	for _, r := range runs {
		sort.Slice(r.Dispatches, func(i, j int) bool { return lessDispatch(r.Dispatches[i], r.Dispatches[j]) })
		sort.Slice(r.Steps, func(i, j int) bool { return lessDispatch(r.Steps[i], r.Steps[j]) })
		for id := range r.Events {
			evs := r.Events[id]
			sort.Slice(evs, func(i, j int) bool { return evs[i].Seq < evs[j].Seq })
		}
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return lessDispatch(out[i].Root, out[j].Root) })
	return out
}

func lessDispatch(a, b stats.Dispatch) bool {
	if !a.Timestamp.Equal(b.Timestamp) {
		return a.Timestamp.Before(b.Timestamp)
	}
	return a.DispatchID < b.DispatchID
}

// rawID strips a dispatch_id down to its last ":"-separated segment: guard
// events carry the raw toolUseId, dispatches.jsonl/tool-events.jsonl carry
// the composite claude-code:<session_id>:<toolUseId> (ADR-0008 H2.1). A
// string without ":" passes through unchanged.
func rawID(s string) string {
	if i := strings.LastIndex(s, ":"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// AttachGuardEvents distributes guard events over runs already built by
// BuildRuns (ADR-0008 H2). BuildRuns itself is not called and its signature
// does not change.
//
//   - DispatchID != "": matched by rawID against every run.Dispatches entry
//     (including the root). No match — the dispatch is outside any complete
//     run — drops the event, symmetric to ToolEvent handling in BuildRuns.
//     No temporal fallback for these rows.
//   - DispatchID == "" (main/unknown): the only temporal fallback. Ts is
//     parsed as RFC3339Nano; unparsable — dropped. The event attaches to the
//     first run (runs is already sorted ascending by root timestamp) whose
//     window [Root.Timestamp−DurationMs, Root.Timestamp] contains ts
//     (inclusive both ends) and whose SessionID matches when both the event
//     and the root have one set. A run is skipped as a candidate when its
//     Root.Timestamp is zero or DurationMs <= 0 — "active" is undefined for
//     it. No window matches — dropped.
//
// Pure: no stderr, no errors; runs is returned in the same order, mutated in
// place — a run that received no events keeps GuardEvents == nil.
func AttachGuardEvents(runs []Run, events []GuardEvent) []Run {
	byRaw := map[string]int{}
	for i := range runs {
		for _, d := range runs[i].Dispatches {
			byRaw[rawID(d.DispatchID)] = i
		}
	}
	for _, e := range events {
		if e.DispatchID != "" {
			if i, ok := byRaw[rawID(e.DispatchID)]; ok {
				runs[i].GuardEvents = append(runs[i].GuardEvents, e)
			}
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, e.Ts)
		if err != nil {
			continue
		}
		for i := range runs {
			root := runs[i].Root
			if root.Timestamp.IsZero() || root.DurationMs <= 0 {
				continue
			}
			start := root.Timestamp.Add(-time.Duration(root.DurationMs) * time.Millisecond)
			if ts.Before(start) || ts.After(root.Timestamp) {
				continue
			}
			if e.SessionID != "" && root.SessionID != "" && e.SessionID != root.SessionID {
				continue
			}
			runs[i].GuardEvents = append(runs[i].GuardEvents, e)
			break
		}
	}
	return runs
}

// LatestRun returns the run with the newest root timestamp, or nil.
func LatestRun(runs []Run) *Run {
	if len(runs) == 0 {
		return nil
	}
	r := runs[len(runs)-1]
	return &r
}

// FindRun matches key against run ID (exact or suffix) or run_log (exact or suffix).
func FindRun(runs []Run, key string) *Run {
	if key == "" {
		return nil
	}
	for i := range runs {
		if runs[i].ID == key {
			return &runs[i]
		}
	}
	for i := range runs {
		r := runs[i]
		if r.RunLog != "" && (r.RunLog == key || strings.HasSuffix(strings.TrimSuffix(r.RunLog, ".md"), key)) {
			return &r
		}
		if strings.HasSuffix(r.ID, key) {
			return &r
		}
	}
	return nil
}
