# Per-task Scorecard (Phase 1) Implementation Plan — Part B3 — Go `score`: карточка, `scores.jsonl`, секция в run log

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After every task-runner run, `zprof score` prints a 0–100 card with per-role breakdown, appends a row to `.agentlog/scores.jsonl` and a `## Score` section to the run log — computed deterministically from `.agentlog/`.

**Architecture:** The Python collector (`profiles/base/zprof-collect.py`, stdlib only) extracts new facts into `.agentlog/`: full rows for nested dispatches (C1), the `verdict` value and `ext.{next,artifact,run_log}` (C2), an ordered `tool-events.jsonl` with `is_error` (C3), `ext.run_id` (C4). A new Go package `cli/internal/score` reads `.agentlog/` only, groups dispatches into runs, computes penalties P1–P7, renders the card and persists it. `zprof score` wires it; the Stop hook runs it after the collector; `AGENT_LOOP.md` tells main to paste the card into `followup.md`.

**Tech Stack:** Go 1.22 (cobra, testify/require, yaml.v3, stdlib `regexp`/`crypto/sha1`), Python 3.10+ stdlib only (pytest for tests).

**Spec:** `docs/superpowers/specs/2026-09-26-task-scorecard-design.md`
## Место в плане

**Зависит от:** Part B2 (`Card`, `Compute`).
**Даёт дальше:** `RenderCard`, `AppendScore`, `ReadScoredKeys`, `ScoreKey`, `WriteRunLogSection`, маркеры. Использует Part C.
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

5. **Second `zprof score` on the same run with unchanged weights.** Expected: no duplicate row is *needed* by readers (readers take the latest per `(run_id, weights_hash)`), run-log section replaced in place, not appended twice. → Task 8 tests `TestWriteRunLogSection_Idempotent`, `TestReadScoredKeys`.

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

### Task 8: Card rendering and persistence (`scores.jsonl`, run-log section)

**Files:**
- Create: `cli/internal/score/render.go`, `cli/internal/score/persist.go`
- Test: `cli/internal/score/render_test.go`, `cli/internal/score/persist_test.go`

**Interfaces:**
- Consumes: `Card` (Task 7), `fsutil.WriteFileAtomic(path, data, perm)`.
- Produces:
  ```go
  func RenderCard(c Card) string                              // multi-line text, spec §7
  func AppendScore(path string, c Card) error                 // append one JSON line + fsync
  func ReadScoredKeys(path string) (map[string]bool, error)   // "<run_id>|<weights_hash>" → true; missing file → empty
  func ScoreKey(c Card) string                                // c.RunID + "|" + c.WeightsHash
  func WriteRunLogSection(runLogPath, card string) error      // replace/append between markers; os.ErrNotExist if no run log
  const MarkerBegin = "<!-- zprof:score:begin -->"
  const MarkerEnd   = "<!-- zprof:score:end -->"
  ```

- [ ] **Step 1: Write the failing render test**

Create `cli/internal/score/render_test.go`:

```go
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
```

- [ ] **Step 2: Implement `render.go`**

```go
package score

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// RenderCard produces the terminal card (spec §7). The first four lines are
// what main pastes into followup.md.
func RenderCard(c Card) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Score %d/100 · %s · %s · %s · confidence %s\n",
		c.Score, c.Tier, orDash(c.Verdict), slug(c), orDash(c.Inputs.Confidence))
	fmt.Fprintf(&b, "%s tok (in %s · out %s · cache %s) · %d dispatch · %d tool calls · %s · %s\n",
		humanTokens(c.Facts.Tokens.Total()), humanTokens(c.Facts.Tokens.Input), humanTokens(c.Facts.Tokens.Output),
		humanTokens(c.Facts.Tokens.CacheRead+c.Facts.Tokens.CacheCreation),
		c.Facts.Dispatches, c.Facts.ToolCalls, humanDuration(c.Facts.DurationMs), modelSummary(c.Facts.ModelCounts))

	findings := make([]Penalty, 0, len(c.Penalties))
	for _, p := range c.Penalties {
		if p.Points > 0 {
			findings = append(findings, p)
		}
	}
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Points > findings[j].Points })
	if len(findings) > 3 {
		findings = findings[:3]
	}
	if len(findings) == 0 {
		b.WriteString("no penalties\n")
	}
	for _, p := range findings {
		fmt.Fprintf(&b, "−%d %s %s\n", roundInt(p.Points), p.ID, p.Detail)
	}
	b.WriteString("\n")

	if len(c.Roles) > 0 {
		b.WriteString("role         tokens  calls  err  penalty  model\n")
		for _, r := range c.Roles {
			fmt.Fprintf(&b, "%-12s %6s %6d %4d  %7s  %s\n",
				r.Role, humanTokens(r.Tokens), r.Calls, r.Errors, penaltyCell(r.Penalty), orDash(r.Model))
		}
	}
	if len(c.Facts.ToolsTop) > 0 {
		parts := make([]string, 0, len(c.Facts.ToolsTop))
		for _, tc := range c.Facts.ToolsTop {
			parts = append(parts, fmt.Sprintf("%s %d", tc.Tool, tc.Count))
		}
		fmt.Fprintf(&b, "tools: %s\n", strings.Join(parts, " · "))
	}
	if len(c.Inputs.TranscriptsMissing) > 0 {
		fmt.Fprintf(&b, "missing transcripts: %s\n", strings.Join(c.Inputs.TranscriptsMissing, ", "))
	}
	if c.RunLog == "" {
		b.WriteString("run log: missing\n")
	}
	return b.String()
}

func slug(c Card) string {
	if c.RunLog != "" {
		return strings.TrimSuffix(filepath.Base(c.RunLog), ".md")
	}
	if n := len(c.RunID); n > 8 {
		return c.RunID[n-8:]
	}
	return orDash(c.RunID)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func roundInt(f float64) int { return int(f + 0.5) }

func penaltyCell(p float64) string {
	if p <= 0 {
		return "0"
	}
	return fmt.Sprintf("−%d", roundInt(p))
}

func humanTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e6), ".0") + "M"
	case n >= 1000:
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

func humanDuration(ms int64) string {
	if ms <= 0 {
		return "—"
	}
	min := ms / 60000
	if min < 1 {
		return fmt.Sprintf("%ds", ms/1000)
	}
	if min >= 120 {
		return fmt.Sprintf("%.1fh", float64(min)/60)
	}
	return fmt.Sprintf("%d min", min)
}

func modelSummary(counts map[string]int) string {
	if len(counts) == 0 {
		return "—"
	}
	type kv struct {
		k string
		v int
	}
	items := make([]kv, 0, len(counts))
	for k, v := range counts {
		items = append(items, kv{k, v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].v != items[j].v {
			return items[i].v > items[j].v
		}
		return items[i].k < items[j].k
	})
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%s×%d", it.k, it.v))
	}
	return strings.Join(parts, " ")
}
```

- [ ] **Step 3: Run the render tests**

Run: `cd cli && go test ./internal/score/ -run 'Render|HumanTokens' -count=1`
Expected: PASS. (Table rows are compared column-by-column via `strings.Fields`, so padding width is free to change; column order and values are not.)

- [ ] **Step 4: Write the failing persist tests**

Create `cli/internal/score/persist_test.go`:

```go
package score

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAppendScoreAndReadScoredKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nested", "scores.jsonl")
	keys, err := ReadScoredKeys(p)
	require.NoError(t, err)
	require.Empty(t, keys, "missing file is not an error")

	c := Compute(loadRun1(t)[0], Defaults(), "test")
	require.NoError(t, AppendScore(p, c))
	require.NoError(t, AppendScore(p, c))
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	require.Len(t, strings.Split(strings.TrimRight(string(data), "\n"), "\n"), 2)
	require.Contains(t, string(data), `"score":45`)

	keys, err = ReadScoredKeys(p)
	require.NoError(t, err)
	require.True(t, keys[ScoreKey(c)])
	require.Equal(t, c.RunID+"|"+c.WeightsHash, ScoreKey(c))
}

func TestWriteRunLogSection_Idempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "run.md")
	require.NoError(t, os.WriteFile(p, []byte("# task\n| t | a | v | x |\n\n## Итог\nverdict: done · artifact: PR #1\n"), 0o644))
	require.NoError(t, WriteRunLogSection(p, "Score 45/100 · Lucky\n"))
	require.NoError(t, WriteRunLogSection(p, "Score 72/100 · Solid\n"))
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	s := string(data)
	require.Equal(t, 1, strings.Count(s, MarkerBegin))
	require.Equal(t, 1, strings.Count(s, MarkerEnd))
	require.Contains(t, s, "Score 72/100")
	require.NotContains(t, s, "Score 45/100")
	require.True(t, strings.HasPrefix(s, "# task\n"), "original content preserved")
	require.Contains(t, s, "## Итог\nverdict: done")
	require.True(t, strings.Index(s, MarkerBegin) > strings.Index(s, "## Итог"), "section goes after ## Итог")
}

func TestWriteRunLogSection_MissingFile(t *testing.T) {
	err := WriteRunLogSection(filepath.Join(t.TempDir(), "nope.md"), "x")
	require.ErrorIs(t, err, os.ErrNotExist)
}
```

- [ ] **Step 5: Implement `persist.go`**

```go
package score

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vaporphd/zprof/internal/fsutil"
)

const (
	MarkerBegin = "<!-- zprof:score:begin -->"
	MarkerEnd   = "<!-- zprof:score:end -->"
)

// ScoreKey identifies a scored (run, parameters) pair.
func ScoreKey(c Card) string { return c.RunID + "|" + c.WeightsHash }

// AppendScore appends one JSON line to scores.jsonl (append-only; readers
// take the latest row per ScoreKey).
func AppendScore(path string, c Card) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal score: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return f.Sync()
}

// ReadScoredKeys returns the set of ScoreKey values already present.
func ReadScoredKeys(path string) (map[string]bool, error) {
	keys := map[string]bool{}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return keys, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)
	for sc.Scan() {
		var row struct {
			RunID       string `json:"run_id"`
			WeightsHash string `json:"weights_hash"`
		}
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil || row.RunID == "" {
			continue
		}
		keys[row.RunID+"|"+row.WeightsHash] = true
	}
	return keys, sc.Err()
}

// WriteRunLogSection puts the card between markers in the run log: replaces
// an existing section in place, otherwise appends one at the end.
func WriteRunLogSection(runLogPath, card string) error {
	data, err := os.ReadFile(runLogPath)
	if err != nil {
		return fmt.Errorf("read run log: %w", err)
	}
	s := string(data)
	section := MarkerBegin + "\n## Score\n```\n" + strings.TrimRight(card, "\n") + "\n```\n" + MarkerEnd
	if i := strings.Index(s, MarkerBegin); i >= 0 {
		if j := strings.Index(s[i:], MarkerEnd); j >= 0 {
			s = s[:i] + section + s[i+j+len(MarkerEnd):]
		} else {
			s = s[:i] + section + "\n"
		}
	} else {
		if !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		s += "\n" + section + "\n"
	}
	return fsutil.WriteFileAtomic(runLogPath, []byte(s), 0o644)
}
```

- [ ] **Step 6: Run the whole package**

Run: `cd cli && go test ./internal/score/ -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cli/internal/score
git commit -m "feat(cli): scorecard card rendering, scores.jsonl append, run-log section"
```

---
