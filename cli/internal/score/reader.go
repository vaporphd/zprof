package score

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

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

// Run is one task-runner dispatch with everything it spawned.
type Run struct {
	ID         string
	Root       stats.Dispatch
	Dispatches []stats.Dispatch
	Steps      []stats.Dispatch
	Events     map[string][]ToolEvent
	RunLog     string
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
	for _, d := range byID {
		rid := runIDFor(d, byID, roots)
		if rid == "" {
			continue
		}
		r := runs[rid]
		r.Dispatches = append(r.Dispatches, d)
		if d.ParentDispatchID == rid {
			r.Steps = append(r.Steps, d)
		}
	}
	for _, ev := range evs {
		for _, r := range runs {
			if _, in := indexOf(r.Dispatches, ev.DispatchID); in {
				r.Events[ev.DispatchID] = append(r.Events[ev.DispatchID], ev)
				break
			}
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

func indexOf(ds []stats.Dispatch, id string) (int, bool) {
	for i, d := range ds {
		if d.DispatchID == id {
			return i, true
		}
	}
	return -1, false
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
