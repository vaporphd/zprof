# Telemetry Stage 2: `zprof stats` — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `zprof stats <path-to-agentlog>` reads `.agentlog/dispatches.jsonl` and produces a self-contained HTML dashboard with four reports: role health, economics, routes, and profile drift.

**Architecture:** New Go package `cli/internal/stats/` with a reader, aggregator, and HTML renderer. The command reads normalized JSONL rows (produced by Stage 1's collector), computes per-role and per-model aggregations, and renders a single HTML file. Pattern follows existing `cli/internal/eval/` — `strings.Builder` HTML, inline CSS, no JS dependencies, dark mode via `prefers-color-scheme`. First line of every report: loss counter.

**Tech Stack:** Go 1.22, cobra, testify/require, CSS-only charts

## Global Constraints

- Go 1.22 — `cli/go.mod`
- CI: `cd cli && go test -race -count=1 ./...` must pass
- HTML: self-contained, no external assets, no CDN, dark mode via CSS custom properties
- All doctor messages in English (`TestDoctorMessagesAreEnglish`)
- Import path: `github.com/vaporphd/zprof/internal/stats`
- Input: `.agentlog/dispatches.jsonl` — JSONL with fields from `profiles/base/telemetry.yaml`
- Only completed dispatches (`dispatch_complete: true`) count toward metrics; async launches (`seq: 0`, `status: async_launched`) are excluded from aggregation
- First line of every report section: loss counter (spec §6, С4)
- Existing 178 Go tests must continue passing

---

## Task 1: JSONL reader and dispatch types

**Files:**
- Create: `cli/internal/stats/types.go` — dispatch struct, report types
- Create: `cli/internal/stats/reader.go` — `ReadDispatches(path) ([]Dispatch, Losses, error)`
- Create: `cli/internal/stats/reader_test.go`
- Create: `cli/internal/stats/testdata/basic.jsonl` — fixture with 10-15 dispatches

**Interfaces:**
- Produces: `Dispatch` struct matching dispatches.jsonl fields, `ReadDispatches` function, `Losses` struct

The dispatch struct maps 1:1 to dispatches.jsonl fields. No transformation — that's the aggregator's job. The reader counts losses: unparsed lines, missing required fields, total lines read.

- [ ] **Step 1: Define types**

```go
// types.go
package stats

import "time"

type Dispatch struct {
    SchemaVersion    int    `json:"schema_version"`
    Harness          string `json:"harness"`
    HarnessVersion   string `json:"harness_version"`
    MachineID        string `json:"machine_id"`
    ProjectID        string `json:"project_id"`
    TsUTC            string `json:"ts_utc"`
    SessionID        string `json:"session_id"`
    DispatchID       string `json:"dispatch_id"`
    Seq              int    `json:"seq"`
    ParentDispatchID string `json:"parent_dispatch_id,omitempty"`
    SpawnDepth       int    `json:"spawn_depth"`
    Role             string `json:"role"`
    Overlay          string `json:"overlay,omitempty"`
    ConfigHash       string `json:"config_hash,omitempty"`
    ModelRequested   string `json:"model_requested,omitempty"`
    ModelResolved    string `json:"model_resolved,omitempty"`
    Verdict          string `json:"verdict,omitempty"`
    Status           string `json:"status"`
    DispatchComplete bool   `json:"dispatch_complete"`
    TokensInput      int    `json:"tokens_input"`
    TokensOutput     int    `json:"tokens_output"`
    TokensCacheRead  int    `json:"tokens_cache_read"`
    TokensCacheCreation int `json:"tokens_cache_creation"`
    ToolUses         int    `json:"tool_uses"`
    DurationMs       int64  `json:"duration_ms"`
    ArtifactExists   *bool  `json:"artifact_exists,omitempty"`
    HasPreamble      *bool  `json:"has_preamble,omitempty"`
    NextIsReachable  *bool  `json:"next_is_reachable,omitempty"`
    ReturnParsed     *bool  `json:"return_parsed,omitempty"`
    TranscriptRef    string `json:"transcript_ref,omitempty"`
    TranscriptCaptured bool `json:"transcript_captured"`
    TranscriptTruncated *bool `json:"transcript_truncated,omitempty"`
    Ext              map[string]any `json:"ext,omitempty"`

    // Computed, not from JSON
    Timestamp time.Time `json:"-"`
}

type Losses struct {
    TotalLines    int
    ParseErrors   int
    Incomplete    int // dispatch_complete == false
    MissingRole   int
    Unparsed      int // from collect.log if present
}
```

- [ ] **Step 2: Implement reader**

```go
// reader.go
package stats

import (
    "bufio"
    "encoding/json"
    "os"
    "time"
)

func ReadDispatches(path string) ([]Dispatch, Losses, error) {
    f, err := os.Open(path)
    if err != nil { return nil, Losses{}, err }
    defer f.Close()

    var dispatches []Dispatch
    var losses Losses
    sc := bufio.NewScanner(f)
    sc.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)
    for sc.Scan() {
        losses.TotalLines++
        var d Dispatch
        if err := json.Unmarshal(sc.Bytes(), &d); err != nil {
            losses.ParseErrors++
            continue
        }
        d.Timestamp, _ = time.Parse(time.RFC3339Nano, d.TsUTC)
        if d.Role == "" { losses.MissingRole++ }
        if !d.DispatchComplete { losses.Incomplete++ }
        dispatches = append(dispatches, d)
    }
    return dispatches, losses, sc.Err()
}
```

- [ ] **Step 3: Create fixture**

`cli/internal/stats/testdata/basic.jsonl` — 15 lines with a mix of roles, models, statuses. Include: 2 async_launched (seq=0), their completions (seq=1), 1 failed, 1 with has_preamble=true, 1 with empty role. Use real field names from hft_moex data.

- [ ] **Step 4: Write tests**

Test `ReadDispatches`: correct count, losses struct populated, timestamp parsed, handles malformed line gracefully.

- [ ] **Step 5: Run and commit**

```bash
cd cli && go test -race -count=1 ./internal/stats/...
git commit -m "feat(stats): JSONL reader and dispatch types"
```

---

## Task 2: Aggregator — compute all four reports

**Files:**
- Create: `cli/internal/stats/aggregate.go` — `Aggregate([]Dispatch, Losses) *Report`
- Create: `cli/internal/stats/aggregate_test.go`

**Interfaces:**
- Consumes: `[]Dispatch` and `Losses` from Task 1
- Produces: `Report` struct with `RoleHealth`, `Economics`, `Routes`, `Drift` sections

This is pure computation, no I/O. Takes dispatches and produces a struct ready for rendering. Only `dispatch_complete == true` dispatches count toward metrics. Async launches (`seq == 0` with `status == "async_launched"`) are excluded from aggregation.

- [ ] **Step 1: Define report types in types.go**

```go
type Report struct {
    ProjectID  string
    MachineID  string
    Harness    string
    TimeRange  [2]time.Time // first, last
    TotalDispatches int
    Sessions   int
    Losses     Losses
    Health     []RoleHealth
    Economics  EconomicsReport
    Routes     RoutesReport
    Drift      []DriftEntry
}

type RoleHealth struct {
    Role             string
    Dispatches       int
    Completed        int
    Failed           int
    PreambleCount    int  // has_preamble == true (bad)
    PreambleChecked  int  // has_preamble != nil
    ParsedCount      int  // return_parsed == true
    ParsedChecked    int
    ArtifactCount    int
    ArtifactChecked  int
    NextReachCount   int
    NextReachChecked int
    ComplianceRate   float64 // pct of checked that passed all
}

type EconomicsReport struct {
    TotalTokens      TokenBreakdown
    ByRole           []RoleEconomics
    ByModel          []ModelEconomics
}

type TokenBreakdown struct {
    Input, Output, CacheRead, CacheCreation int
}
func (t TokenBreakdown) Total() int { return t.Input + t.Output + t.CacheRead + t.CacheCreation }

type RoleEconomics struct {
    Role         string
    Dispatches   int
    Tokens       TokenBreakdown
    AvgPerDispatch int
    P50Duration  int64
    P95Duration  int64
}

type ModelEconomics struct {
    Model      string
    Dispatches int
    Tokens     TokenBreakdown
}

type RoutesReport struct {
    ByStatus       map[string]int  // completed, failed, killed, blocked
    TesterLoops    []TesterLoop    // sessions where tester ran 3+ times
    TopRoleChains  []RoleChain     // most common role sequences in sessions
}

type TesterLoop struct {
    SessionID string
    Rounds    int
}

type RoleChain struct {
    Chain string // "planner → architect → implementer → tester → reviewer"
    Count int
}

type DriftEntry struct {
    ConfigHash string
    Period     string // date range
    Dispatches int
    AvgTokens  int
    ComplianceRate float64
}
```

- [ ] **Step 2: Implement `Aggregate`**

Key aggregation logic:
- Filter: only `dispatch_complete == true` and not `status == "async_launched"`
- Group by role → `RoleHealth` + `RoleEconomics`
- Group by model → `ModelEconomics`
- Group by session → detect tester loops (tester role count ≥ 3 per session)
- Group by config_hash → `DriftEntry`
- Sort roles by total tokens descending
- Compute p50/p95 duration per role using sorted slice
- Count unique sessions

- [ ] **Step 3: Write tests**

Using the fixture from Task 1:
- Total dispatches excludes async_launched
- Role aggregation correct counts
- Token breakdown sums correctly
- p50/p95 computation on known values
- Empty input returns zero report, not error
- Losses propagated to report

- [ ] **Step 4: Run and commit**

```bash
cd cli && go test -race -count=1 ./internal/stats/...
git commit -m "feat(stats): aggregator — role health, economics, routes, drift"
```

---

## Task 3: HTML renderer

**Files:**
- Create: `cli/internal/stats/render.go` — `RenderHTML(Report) string`
- Create: `cli/internal/stats/render_test.go`

**Interfaces:**
- Consumes: `Report` from Task 2
- Produces: self-contained HTML string

Follows the **decision-oriented pattern** from `docs/hft-moex-telemetry-example-improved.html` — not a data dump, but a queue of actions sorted by risk. Five semantic color pairs (blue/amber/red/green/violet) for severity, not a categorical palette. `strings.Builder`, inline CSS with custom properties, dark mode via `prefers-color-scheme`, no JS. 

**Reference design file: `docs/hft-moex-telemetry-example-improved.html`** — the renderer must reproduce this structure, not the earlier `hft-moex-telemetry-example.html`. Read it to understand the CSS, layout, and component patterns.

- [ ] **Step 1: Implement HTML renderer**

Seven sections (matching the reference design):

1. **Header** — project name (large), eyebrow with "Agent telemetry · decision report", metadata grid (sessions, date range, project_id)
2. **Sticky nav** — anchor links to sections
3. **Trust block** — amber-bordered panel: overall trust verdict (Limited/Good/Full), three evidence stats (transcript %, unknown role %, unknown model %) with mini-bars
4. **Action queue** — P0/P1/P2 items sorted by risk. Each: severity badge, finding title, evidence paragraph, "Следующее действие" with concrete next step. Generated from aggregated data: Class A violations → P0, unknown gaps → P1, cost concentration → P1, duration tail → P2
5. **Volume strip** — 4-metric bar: dispatches, raw tokens, cache%, p50/p95
6. **Economics table** — role × dispatches × tokens × share (with mini bar chart) × avg × p50/p95 × signal text
7. **Contract (Class A)** — table + aside callout with "Парсер работает. Контракт — нет." pattern
8. **Report catalog** — 2-column grid of missing/available decision reports with status badges (нет outcome / можно строить / приоритет 0)

CSS: five semantic pairs (`--blue`/`--blue-soft`, `--amber`/`--amber-soft`, `--red`/`--red-soft`, `--green`/`--green-soft`, `--violet`/`--violet-soft`) with both light and dark mode values. Share bars use `<i style="width:N%">` inside track divs. No JS.

- [ ] **Step 2: Write tests**

Test structural anchors:
- Output contains `<!DOCTYPE html>`
- Contains project ID in header
- Contains losses section when losses > 0
- Contains one `<tr>` per role in health table
- Bar chart widths are percentage values between 0 and 100
- Dark mode CSS present
- No external URLs in output

- [ ] **Step 3: Run and commit**

```bash
cd cli && go test -race -count=1 ./internal/stats/...
git commit -m "feat(stats): HTML dashboard renderer"
```

---

## Task 4: CLI command and wiring

**Files:**
- Create: `cli/internal/cmd/stats.go` — `NewStatsCmd() *cobra.Command`
- Modify: `cli/cmd/zprof/main.go` — register `NewStatsCmd()`
- Create: `cli/internal/cmd/stats_test.go`

**Interfaces:**
- Consumes: `stats.ReadDispatches`, `stats.Aggregate`, `stats.RenderHTML` from Tasks 1-3
- Produces: `zprof stats <path> [--out file] [--format html|json]`

```
Usage:
  zprof stats <agentlog-dir> [flags]

Flags:
  --out string      Output path (default: stdout for json, <agentlog>/report.html for html)
  --format string   Output format: "html" or "json" (default: html)
```

- [ ] **Step 1: Implement command**

```go
func NewStatsCmd() *cobra.Command {
    var (
        outPath string
        format  string
    )
    c := &cobra.Command{
        Use:   "stats <agentlog-dir> [<agentlog-dir>...]",
        Short: "Generate telemetry dashboard from .agentlog/ data",
        Args:  cobra.MinimumNArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            // For each path:
            //   1. ReadDispatches(filepath.Join(path, "dispatches.jsonl"))
            //   2. Aggregate(dispatches, losses)
            //   3. RenderHTML(report) or json.MarshalIndent(report)
            //   4. Write to outPath or default
        },
    }
    c.Flags().StringVar(&outPath, "out", "", "...")
    c.Flags().StringVar(&format, "format", "html", "...")
    return c
}
```

- [ ] **Step 2: Register in main.go**

Add `root.AddCommand(cmd.NewStatsCmd())` in `main.go`.

- [ ] **Step 3: Write command test**

Test that running `stats` on the testdata fixture produces HTML containing expected anchors.

- [ ] **Step 4: Integration test with real data**

Run `zprof stats /Volumes/mydata/projects/hft_moex/.agentlog/` and verify output is valid HTML.

- [ ] **Step 5: Run full suite and commit**

```bash
cd cli && go test -race -count=1 ./...
git commit -m "feat(stats): zprof stats command — CLI wiring"
```

---

## Task 5: Visual verification and polish

**Files:**
- Modify: `cli/internal/stats/render.go` — polish based on rendering
- No new tests (visual verification)

**Interfaces:**
- Consumes: working `zprof stats` from Task 4

- [ ] **Step 1: Generate report from real data**

```bash
cd cli && go build -o bin/zprof ./cmd/zprof/
./bin/zprof stats /Volumes/mydata/projects/hft_moex/.agentlog/ --out /tmp/hft-report.html
```

- [ ] **Step 2: Open and verify in browser**

Check: layout, spacing, dark mode, bar chart proportions, losses banner, all four sections present and populated.

- [ ] **Step 3: Fix any visual issues**

Adjust CSS, spacing, sort orders based on what looks wrong.

- [ ] **Step 4: Run tests and commit**

```bash
cd cli && go test -race -count=1 ./...
git commit -m "fix(stats): visual polish after browser verification"
```

---

## Task dependency graph

```
Task 1 (reader + types) → Task 2 (aggregator) → Task 3 (renderer) → Task 4 (CLI) → Task 5 (polish)
```

Strictly sequential — each task builds on the previous.

---

## What this plan does NOT cover

- Rebuilding `zprof eval` over `.agentlog/` (separate plan)
- Deleting `parser.go` (after eval is rebuilt)
- Level 2 evaluator dispatch on signal (Stage 3)
- `--serve` flag for live dashboard (future)
