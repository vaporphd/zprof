# Per-task Scorecard (Phase 1) Implementation Plan — Part B2 — Go `score`: метрики P1–P7, scoring, ярусы, атрибуция

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After every task-runner run, `zprof score` prints a 0–100 card with per-role breakdown, appends a row to `.agentlog/scores.jsonl` and a `## Score` section to the run log — computed deterministically from `.agentlog/`.

**Architecture:** The Python collector (`profiles/base/zprof-collect.py`, stdlib only) extracts new facts into `.agentlog/`: full rows for nested dispatches (C1), the `verdict` value and `ext.{next,artifact,run_log}` (C2), an ordered `tool-events.jsonl` with `is_error` (C3), `ext.run_id` (C4). A new Go package `cli/internal/score` reads `.agentlog/` only, groups dispatches into runs, computes penalties P1–P7, renders the card and persists it. `zprof score` wires it; the Stop hook runs it after the collector; `AGENT_LOOP.md` tells main to paste the card into `followup.md`.

**Tech Stack:** Go 1.22 (cobra, testify/require, yaml.v3, stdlib `regexp`/`crypto/sha1`), Python 3.10+ stdlib only (pytest for tests).

**Spec:** `docs/superpowers/specs/2026-09-26-task-scorecard-design.md`
## Место в плане

**Зависит от:** Part B1 целиком (типы, фикстура, `loadRun1`).
**Даёт дальше:** `Penalty`, `Card`, `Facts`, `Inputs`, `RoleRow`, `Compute`, `TierFor`, `ShortModel`, `computeP1…P7`. Использует Part B3.
**Индекс всех частей:** `2026-09-26-task-scorecard-phase1.md`

## Global Constraints

- Python collector: **stdlib only**, `exit(0)` always, every new branch inside existing try/except → `collect.log`; `fcntl.flock` + `os.fsync()` on writes (spec §5, project CLAUDE.md).
- Go: `internal/` packages, table-driven tests with `testify/require`, error wrapping `fmt.Errorf("...: %w", err)`.
- Go `score` package reads **only** `.agentlog/` (`dispatches.jsonl`, `tool-events.jsonl`, `schema.json`) plus `.zprof.yaml`. No raw session JSONL parsing in Go (spec §4, §8).
- Boundary: **Python extracts facts, Go interprets.** No metric computed in Python (spec §4).
- Weights sum to 100: P1 20, P2 15, P3 10, P4 20, P5 10, P6 15, P7 10. Saturation: P1 0.20, P2 3, P3 0.5, P4 2, P5 2, P6 0.30, P7 4. Thresholds: Ideal ≥ 85, Solid 60–84, Lucky < 60 (spec §6).
- Tiers: verdict `done`/`approve*` → Ideal/Solid/Lucky by score; `blocked` → `Blocked`; `failed` → `Failed` (spec §6).
- `verdict_exempt_roles: [auditor, auditor-deep]` are excluded from P6-by-`return_parsed` and from P7 (spec §6, §9).
- `tool-events.jsonl` excludes `Agent`/`Task` tool calls (spec §5 C3). `input_hash` = sha1 of `json.dumps(input, sort_keys=True, separators=(",",":"), ensure_ascii=False)`, first 12 hex.
- `dispatch_id` in `tool-events.jsonl` uses the same composite form as `dispatches.jsonl`: `claude-code:<session>:<toolu_id>`.
- Commits: `feat(base):` for collector, `feat(cli):` for Go, `test(...)`, `docs:` (project CLAUDE.md).
- Prompt files (`agent-loop-router.md`): Russian text, English keys.

## Review Focus (этой части)

1. **Blind retry where the retried command is itself a "mutating" Bash pattern** (e.g. `sed -i` fails twice). Expected: counted as a blind retry, then state resets. → Task 7 test `TestBlindRetries_MutatingCommandStillCounts`.
2. **`swift test` / `cargo test` repeated after an error with only a build in between.** Expected: counted (build/test commands are **not** mutating). → Task 1 amends the spec; Task 7 test `TestBlindRetries_BuildBetweenDoesNotReset`.
4. **A run whose root task-runner returned `blocked` with an `artifact:` that does not exist.** Expected: tier `Blocked`, P7 does not count `artifact_exists=false` for blocked. → Task 7 test `TestP7_BlockedDoesNotCountMissingArtifact`.

---

## File Structure (этой части)

**Go — `cli/internal/`**
- Create `score/config.go` — `Config`, `Defaults()`, `LoadConfig(projectDir, agentlogDir)`, `WeightsHash()`.
- Create `score/reader.go` — `ToolEvent`, `ReadToolEvents(path)`, `Run`, `BuildRuns(dispatches, events)`, `LatestRun`, `FindRun`.
- Create `score/metrics.go` — `Penalty`, `computeP1…P7`.
- Create `score/scoring.go` — `Card`, `Facts`, `Inputs`, `Compute(run, cfg) Card`, `tierFor`.
- Create `score/render.go` — `RenderCard(Card) string`, `findingText`.
- Create `score/persist.go` — `AppendScore`, `ReadScoredKeys`, `WriteRunLogSection`.
- Create `score/testdata/run1/{dispatches.jsonl,tool-events.jsonl}` + `score/*_test.go`.
- Modify `manifest/project.go` — `ScoreConfig`, `CarryOverFrom`.
- Create `cmd/score.go` — `NewScoreCmd()`; modify `cli/cmd/zprof/main.go` to register.
- Modify `apply/settings.go` — Stop hook chains `zprof score`; upgrade path for old Stop command; `apply/settings_test.go`.

---

### Task 7: Go metrics P1–P7, scoring, tiers, attribution

**Files:**
- Create: `cli/internal/score/metrics.go`, `cli/internal/score/scoring.go`
- Test: `cli/internal/score/metrics_test.go`, `cli/internal/score/scoring_test.go`

**Interfaces:**
- Consumes: `Run`, `Config`, `ToolEvent` (Task 6); `stats.Dispatch`.
- Produces:
  ```go
  type Penalty struct {
      ID     string             `json:"id"`
      Value  float64            `json:"value"`   // raw metric: rate, count or share
      Points float64            `json:"points"`  // weight × min(1, value/saturation)
      ByRole map[string]float64 `json:"by_role"` // points attributed per role, sums to Points
      Detail string             `json:"detail,omitempty"`
  }
  type Tokens struct { Input, Output, CacheRead, CacheCreation int }  // json: input, output, cache_read, cache_creation
  func (t Tokens) Total() int
  type ToolCount struct { Tool string `json:"tool"`; Count int `json:"count"` }
  type RoleRow struct { Role string; Tokens int; Calls int; Errors int; Penalty float64; Model string } // json snake_case
  type Facts struct { Tokens Tokens; Dispatches int; ToolCalls int; ToolsTop []ToolCount; DurationMs int64; Route []string; Models map[string]string; ModelCounts map[string]int }
  type Inputs struct { Dispatches int; ToolEvents int; TranscriptsMissing []string; Confidence string }
  type Card struct { ScoreSchema int; ZprofVersion string; RunID, RunLog, SessionID, ProjectID, TsUTC, Verdict, Tier string; Score int; Penalties []Penalty; Roles []RoleRow; Facts Facts; Inputs Inputs; WeightsHash string }
  func Compute(run Run, cfg Config, zprofVersion string) Card
  func TierFor(verdict string, score int, th Thresholds) string // Ideal|Solid|Lucky|Blocked|Failed|Unknown
  func ShortModel(model string) string // "claude-sonnet-5" → "sonnet"
  ```
  JSON tags are snake_case versions of the field names (`score_schema`, `zprof_version`, `run_id`, `run_log`, `session_id`, `project_id`, `ts_utc`, `weights_hash`, `tool_calls`, `tools_top`, `duration_ms`, `model_counts`, `tool_events`, `transcripts_missing`).

- [ ] **Step 1: Write the failing golden test on the fixture**

Create `cli/internal/score/scoring_test.go`:

```go
package score

import (
	"testing"

	"github.com/stretchr/testify/require"
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
```

Add `"github.com/vaporphd/zprof/internal/stats"` to the imports and this helper (shared with `metrics_test.go`) at the bottom of `scoring_test.go`:

```go
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
```

(import `"time"` too.)

- [ ] **Step 2: Write the failing metric edge-case tests**

Create `cli/internal/score/metrics_test.go`:

```go
package score

import (
	"testing"

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

func TestWastedTokens_ExemptRolesAndKilled(t *testing.T) {
	root := mkDispatch("r", "task-runner", "", "done", "completed", "2026-09-26T10:00:00Z")
	root.TokensInput, root.TokensOutput = 0, 0
	impl := mkDispatch("i", "implementer", "r", "", "killed", "2026-09-26T10:01:00Z")
	impl.TokensInput, impl.TokensOutput = 3000, 0
	aud := mkDispatch("a", "auditor", "r", "", "completed", "2026-09-26T10:02:00Z")
	aud.ReturnParsed = bptr(false) // auditor contract starts with completion:, not verdict:
	aud.TokensInput, aud.TokensOutput = 5000, 0
	ok := mkDispatch("o", "tester", "r", "done", "completed", "2026-09-26T10:03:00Z")
	ok.TokensInput, ok.TokensOutput = 2000, 0
	run := BuildRuns([]stats.Dispatch{root, impl, aud, ok}, nil)[0]
	p := computeP6(run, Defaults())
	require.InDelta(t, 0.30, p.value, 0.001, "3000 of 10000")
	require.Equal(t, 3000.0, p.byRole["implementer"])
	_, hasAud := p.byRole["auditor"]
	require.False(t, hasAud)
}

func TestP7_BlockedDoesNotCountMissingArtifact(t *testing.T) {
	root := mkDispatch("r", "task-runner", "", "blocked", "completed", "2026-09-26T10:00:00Z")
	root.ArtifactExists = bptr(false)
	impl := mkDispatch("i", "implementer", "r", "blocked", "completed", "2026-09-26T10:01:00Z")
	impl.ArtifactExists = bptr(false)
	aud := mkDispatch("a", "auditor", "r", "", "completed", "2026-09-26T10:02:00Z")
	aud.HasPreamble = bptr(true) // exempt role → ignored
	run := BuildRuns([]stats.Dispatch{root, impl, aud}, nil)[0]
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
```

- [ ] **Step 3: Run to verify failure**

Run: `cd cli && go test ./internal/score/ -count=1`
Expected: FAIL — `undefined: computeP2`, `undefined: Compute`, …

- [ ] **Step 4: Implement `metrics.go`**

```go
package score

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vaporphd/zprof/internal/stats"
)

// metric is the raw outcome of one P-check before weights are applied.
type metric struct {
	value  float64            // compared against Saturation[id]
	byRole map[string]float64 // numerator share per role (errors, retries, tokens, …)
	detail string             // human text for the card (RU)
}

func newMetric() metric { return metric{byRole: map[string]float64{}} }

// Penalty is one scored P-check.
type Penalty struct {
	ID     string             `json:"id"`
	Value  float64            `json:"value"`
	Points float64            `json:"points"`
	ByRole map[string]float64 `json:"by_role"`
	Detail string             `json:"detail,omitempty"`
}

// penaltyFrom applies weight × min(1, value/saturation) and splits the points
// across roles proportionally to their numerator share.
func penaltyFrom(id string, m metric, cfg Config) Penalty {
	p := Penalty{ID: id, Value: m.value, ByRole: map[string]float64{}, Detail: m.detail}
	sat := cfg.Saturation[id]
	if sat <= 0 || m.value <= 0 {
		return p
	}
	frac := m.value / sat
	if frac > 1 {
		frac = 1
	}
	p.Points = cfg.Weights[id] * frac
	total := 0.0
	for _, v := range m.byRole {
		total += v
	}
	if total > 0 {
		for r, v := range m.byRole {
			p.ByRole[r] = p.Points * v / total
		}
	}
	return p
}

func roleIndex(run Run) map[string]string {
	m := make(map[string]string, len(run.Dispatches))
	for _, d := range run.Dispatches {
		m[d.DispatchID] = d.Role
	}
	return m
}

func isMutating(e ToolEvent, cfg Config) bool {
	if cfg.MutatingTools[e.Tool] {
		return true
	}
	return e.Tool == "Bash" && cfg.IsMutatingBash(e.Target)
}

func isErr(e ToolEvent) bool { return e.IsError != nil && *e.IsError }

func tokens(d stats.Dispatch) int {
	return d.TokensInput + d.TokensOutput + d.TokensCacheRead + d.TokensCacheCreation
}

// P1 — tool error rate: errored / events that got a result.
func computeP1(run Run, cfg Config) metric {
	m := newMetric()
	roles := roleIndex(run)
	total, errs := 0, 0
	for id, evs := range run.Events {
		for _, e := range evs {
			if e.IsError == nil {
				continue
			}
			total++
			if *e.IsError {
				errs++
				m.byRole[roles[id]]++
			}
		}
	}
	if total > 0 {
		m.value = float64(errs) / float64(total)
	}
	m.detail = fmt.Sprintf("%d/%d tool errors (%.0f%%)", errs, total, m.value*100)
	return m
}

// P2 — blind retries: same (tool, input_hash) repeated after an error with no
// mutating event in between. Order per event: check, then reset-if-mutating, then record.
func computeP2(run Run, cfg Config) metric {
	m := newMetric()
	roles := roleIndex(run)
	targets := map[string]int{}
	for id, evs := range run.Events {
		lastErr := map[string]bool{}
		for _, e := range evs {
			key := e.Tool + "|" + e.InputHash
			if lastErr[key] {
				m.value++
				m.byRole[roles[id]]++
				targets[e.Target]++
			}
			if isMutating(e, cfg) {
				lastErr = map[string]bool{}
			}
			lastErr[key] = isErr(e)
		}
	}
	if m.value > 0 {
		m.detail = fmt.Sprintf("%s: %d× `%s` без правок между", topRole(m.byRole), int(m.value), truncate(topKey(targets), 40))
	}
	return m
}

// P3 — re-reads: Read of a path already read in this dispatch with no mutating
// event since. Only dispatches with ≥ 4 Reads count (noise floor).
func computeP3(run Run, cfg Config) metric {
	m := newMetric()
	roles := roleIndex(run)
	sumReads, sumRereads := 0, 0
	for id, evs := range run.Events {
		reads, rereads := 0, 0
		seen := map[string]bool{}
		for _, e := range evs {
			if isMutating(e, cfg) {
				seen = map[string]bool{}
				continue
			}
			if e.Tool != "Read" {
				continue
			}
			reads++
			if seen[e.Target] {
				rereads++
			}
			seen[e.Target] = true
		}
		if reads >= 4 {
			sumReads += reads
			sumRereads += rereads
			if rereads > 0 {
				m.byRole[roles[id]] += float64(rereads)
			}
		}
	}
	if sumReads > 0 {
		m.value = float64(sumRereads) / float64(sumReads)
	}
	if sumRereads > 0 {
		m.detail = fmt.Sprintf("%s: %d перечитываний из %d Read", topRole(m.byRole), sumRereads, sumReads)
	}
	return m
}

// P4 — tester `failed` immediately followed by an implementer step.
func computeP4(run Run, cfg Config) metric {
	m := newMetric()
	for i := 0; i+1 < len(run.Steps); i++ {
		s := run.Steps[i]
		if s.Role == "tester" && s.Verdict == "failed" && run.Steps[i+1].Role == "implementer" {
			m.value++
		}
	}
	if m.value > 0 {
		m.byRole["implementer"] = m.value
		m.detail = fmt.Sprintf("tester→implementer: %d лишн. раунд(ов)", int(m.value))
	}
	return m
}

// P5 — reviewer `block` verdicts.
func computeP5(run Run, cfg Config) metric {
	m := newMetric()
	for _, s := range run.Steps {
		if s.Role == "reviewer" && s.Verdict == "block" {
			m.value++
		}
	}
	if m.value > 0 {
		m.byRole["implementer"] = m.value
		m.detail = fmt.Sprintf("reviewer block ×%d", int(m.value))
	}
	return m
}

// P6 — share of run tokens spent in dispatches that produced nothing:
// status failed/killed, or an unparsable return (exempt roles excluded).
func computeP6(run Run, cfg Config) metric {
	m := newMetric()
	total, wasted := 0, 0
	for _, d := range run.Dispatches {
		tk := tokens(d)
		total += tk
		failed := d.Status == "failed" || d.Status == "killed"
		unparsed := d.ReturnParsed != nil && !*d.ReturnParsed && !cfg.ExemptRoles[d.Role]
		if failed || unparsed {
			wasted += tk
			m.byRole[d.Role] += float64(tk)
		}
	}
	if total > 0 {
		m.value = float64(wasted) / float64(total)
	}
	if wasted > 0 {
		m.detail = fmt.Sprintf("%.0f%% токенов в dispatch'ах без результата (%s)", m.value*100, joinRoles(m.byRole))
	}
	return m
}

// P7 — Class-A contract violations, exempt roles excluded; a missing artifact
// on a `blocked` verdict is not a violation.
func computeP7(run Run, cfg Config) metric {
	m := newMetric()
	for _, d := range run.Dispatches {
		if cfg.ExemptRoles[d.Role] {
			continue
		}
		n := 0
		if d.HasPreamble != nil && *d.HasPreamble {
			n++
		}
		if d.ArtifactExists != nil && !*d.ArtifactExists && d.Verdict != "blocked" {
			n++
		}
		if d.NextIsReachable != nil && !*d.NextIsReachable {
			n++
		}
		if d.ReturnParsed != nil && !*d.ReturnParsed {
			n++
		}
		if n > 0 {
			m.value += float64(n)
			m.byRole[d.Role] += float64(n)
		}
	}
	if m.value > 0 {
		m.detail = fmt.Sprintf("%d нарушений контракта (%s)", int(m.value), joinRoles(m.byRole))
	}
	return m
}

func topRole(byRole map[string]float64) string { return topKey(toIntMap(byRole)) }

func toIntMap(m map[string]float64) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = int(v)
	}
	return out
}

func topKey(counts map[string]int) string {
	best, bestN := "", -1
	for k, n := range counts {
		if n > bestN || (n == bestN && k < best) {
			best, bestN = k, n
		}
	}
	return best
}

func joinRoles(byRole map[string]float64) string {
	keys := make([]string, 0, len(byRole))
	for k := range byRole {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
```

- [ ] **Step 5: Implement `scoring.go`**

```go
package score

import (
	"math"
	"sort"
	"strings"

	"github.com/vaporphd/zprof/internal/stats"
)

const ScoreSchema = 1

type Tokens struct {
	Input         int `json:"input"`
	Output        int `json:"output"`
	CacheRead     int `json:"cache_read"`
	CacheCreation int `json:"cache_creation"`
}

func (t Tokens) Total() int { return t.Input + t.Output + t.CacheRead + t.CacheCreation }

type ToolCount struct {
	Tool  string `json:"tool"`
	Count int    `json:"count"`
}

type RoleRow struct {
	Role    string  `json:"role"`
	Tokens  int     `json:"tokens"`
	Calls   int     `json:"calls"`
	Errors  int     `json:"errors"`
	Penalty float64 `json:"penalty"`
	Model   string  `json:"model"`
}

type Facts struct {
	Tokens      Tokens            `json:"tokens"`
	Dispatches  int               `json:"dispatches"`
	ToolCalls   int               `json:"tool_calls"`
	ToolsTop    []ToolCount       `json:"tools_top"`
	DurationMs  int64             `json:"duration_ms"`
	Route       []string          `json:"route"`
	Models      map[string]string `json:"models"`
	ModelCounts map[string]int    `json:"model_counts"`
}

type Inputs struct {
	Dispatches         int      `json:"dispatches"`
	ToolEvents         int      `json:"tool_events"`
	TranscriptsMissing []string `json:"transcripts_missing"`
	Confidence         string   `json:"confidence"`
}

// Card is one row of .agentlog/scores.jsonl.
type Card struct {
	ScoreSchema  int       `json:"score_schema"`
	ZprofVersion string    `json:"zprof_version"`
	RunID        string    `json:"run_id"`
	RunLog       string    `json:"run_log,omitempty"`
	SessionID    string    `json:"session_id"`
	ProjectID    string    `json:"project_id"`
	TsUTC        string    `json:"ts_utc"`
	Verdict      string    `json:"verdict"`
	Tier         string    `json:"tier"`
	Score        int       `json:"score"`
	Penalties    []Penalty `json:"penalties"`
	Roles        []RoleRow `json:"roles"`
	Facts        Facts     `json:"facts"`
	Inputs       Inputs    `json:"inputs"`
	WeightsHash  string    `json:"weights_hash"`
}

// TierFor maps verdict + score onto the spec §6 tiers.
func TierFor(verdict string, score int, th Thresholds) string {
	switch {
	case verdict == "blocked":
		return "Blocked"
	case verdict == "failed":
		return "Failed"
	case verdict == "done" || verdict == "ok" || strings.HasPrefix(verdict, "approve"):
		if score >= th.Ideal {
			return "Ideal"
		}
		if score >= th.Solid {
			return "Solid"
		}
		return "Lucky"
	}
	return "Unknown"
}

// ShortModel: "claude-sonnet-5" → "sonnet", "claude-opus-5-5" → "opus"; unknown vendors pass through.
func ShortModel(model string) string {
	if model == "" {
		return "?"
	}
	parts := strings.Split(model, "-")
	if parts[0] == "claude" && len(parts) > 1 {
		return parts[1]
	}
	return model
}

// Compute scores one run under cfg.
func Compute(run Run, cfg Config, zprofVersion string) Card {
	checks := []func(Run, Config) metric{computeP1, computeP2, computeP3, computeP4, computeP5, computeP6, computeP7}
	penalties := make([]Penalty, 0, len(checks))
	sum := 0.0
	rolePenalty := map[string]float64{}
	for i, fn := range checks {
		p := penaltyFrom(PenaltyIDs[i], fn(run, cfg), cfg)
		penalties = append(penalties, p)
		sum += p.Points
		for r, v := range p.ByRole {
			rolePenalty[r] += v
		}
	}
	score := int(math.Round(100 - sum))
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	card := Card{
		ScoreSchema:  ScoreSchema,
		ZprofVersion: zprofVersion,
		RunID:        run.ID,
		RunLog:       run.RunLog,
		SessionID:    run.Root.SessionID,
		ProjectID:    run.Root.ProjectID,
		TsUTC:        run.Root.TsUTC,
		Verdict:      run.Root.Verdict,
		Score:        score,
		Tier:         TierFor(run.Root.Verdict, score, cfg.Thresholds),
		Penalties:    penalties,
		WeightsHash:  cfg.WeightsHash(),
	}
	card.Facts, card.Roles = facts(run, rolePenalty)
	card.Inputs = inputs(run)
	return card
}

func facts(run Run, rolePenalty map[string]float64) (Facts, []RoleRow) {
	f := Facts{Models: map[string]string{}, ModelCounts: map[string]int{}}
	rows := map[string]*RoleRow{}
	toolCounts := map[string]int{}
	var childDuration int64

	for _, d := range run.Dispatches {
		f.Tokens.Input += d.TokensInput
		f.Tokens.Output += d.TokensOutput
		f.Tokens.CacheRead += d.TokensCacheRead
		f.Tokens.CacheCreation += d.TokensCacheCreation
		if d.ModelResolved != "" {
			f.ModelCounts[ShortModel(d.ModelResolved)]++
		}
		if d.DispatchID != run.ID {
			f.Dispatches++
			childDuration += d.DurationMs
			if d.ModelResolved != "" {
				f.Models[d.Role] = d.ModelResolved
			}
		}
		row := rows[d.Role]
		if row == nil {
			row = &RoleRow{Role: d.Role}
			rows[d.Role] = row
		}
		row.Tokens += tokens(d)
		if d.ModelResolved != "" {
			row.Model = ShortModel(d.ModelResolved)
		}
		for _, e := range run.Events[d.DispatchID] {
			f.ToolCalls++
			toolCounts[e.Tool]++
			row.Calls++
			if isErr(e) {
				row.Errors++
			}
		}
	}
	for _, s := range run.Steps {
		f.Route = append(f.Route, s.Role)
	}
	f.DurationMs = run.Root.DurationMs
	if f.DurationMs == 0 {
		f.DurationMs = childDuration
	}
	for tool, n := range toolCounts {
		f.ToolsTop = append(f.ToolsTop, ToolCount{tool, n})
	}
	sort.Slice(f.ToolsTop, func(i, j int) bool {
		if f.ToolsTop[i].Count != f.ToolsTop[j].Count {
			return f.ToolsTop[i].Count > f.ToolsTop[j].Count
		}
		return f.ToolsTop[i].Tool < f.ToolsTop[j].Tool
	})
	if len(f.ToolsTop) > 5 {
		f.ToolsTop = f.ToolsTop[:5]
	}

	out := make([]RoleRow, 0, len(rows))
	for role, row := range rows {
		row.Penalty = rolePenalty[role]
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Penalty != out[j].Penalty {
			return out[i].Penalty > out[j].Penalty
		}
		if out[i].Tokens != out[j].Tokens {
			return out[i].Tokens > out[j].Tokens
		}
		return out[i].Role < out[j].Role
	})
	return f, out
}

func inputs(run Run) Inputs {
	in := Inputs{Dispatches: len(run.Dispatches), Confidence: "full", TranscriptsMissing: []string{}}
	for _, evs := range run.Events {
		in.ToolEvents += len(evs)
	}
	seen := map[string]bool{}
	for _, d := range run.Dispatches {
		truncated := d.TranscriptTruncated != nil && *d.TranscriptTruncated
		if (!d.TranscriptCaptured || truncated) && !seen[d.Role] {
			seen[d.Role] = true
			in.TranscriptsMissing = append(in.TranscriptsMissing, d.Role)
		}
	}
	sort.Strings(in.TranscriptsMissing)
	if len(in.TranscriptsMissing) > 0 {
		in.Confidence = "partial"
	}
	return in
}

// unused-import guard for stats in this file
var _ stats.Dispatch
```

- [ ] **Step 6: Run the package tests**

Run: `cd cli && go test ./internal/score/ -count=1 -v -run 'Compute|Blind|Rereads|ToolErrors|LoopRounds|Wasted|P7|Tier|ShortModel|Attribution'`
Expected: PASS. If `TestCompute_Run1Golden` fails on `ToolsTop[0]`, count Bash events in the fixture (9) — the assertion is correct; the sort must be count-desc then name-asc.

- [ ] **Step 7: Commit**

```bash
git add cli/internal/score
git commit -m "feat(cli): scorecard metrics P1-P7, tiers, per-role attribution"
```

---
