# Per-task Scorecard (Phase 1) Implementation Plan — Part B1 — Go `score`: config, manifest `score:`, reader, runs, fixture

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After every task-runner run, `zprof score` prints a 0–100 card with per-role breakdown, appends a row to `.agentlog/scores.jsonl` and a `## Score` section to the run log — computed deterministically from `.agentlog/`.

**Architecture:** The Python collector (`profiles/base/zprof-collect.py`, stdlib only) extracts new facts into `.agentlog/`: full rows for nested dispatches (C1), the `verdict` value and `ext.{next,artifact,run_log}` (C2), an ordered `tool-events.jsonl` with `is_error` (C3), `ext.run_id` (C4). A new Go package `cli/internal/score` reads `.agentlog/` only, groups dispatches into runs, computes penalties P1–P7, renders the card and persists it. `zprof score` wires it; the Stop hook runs it after the collector; `AGENT_LOOP.md` tells main to paste the card into `followup.md`.

**Tech Stack:** Go 1.22 (cobra, testify/require, yaml.v3, stdlib `regexp`/`crypto/sha1`), Python 3.10+ stdlib only (pytest for tests).

**Spec:** `docs/superpowers/specs/2026-09-26-task-scorecard-design.md`
## Место в плане

**Зависит от:** Форматы файлов из Part A (только описание схемы — фикстура `testdata/run1` пишется руками, поэтому B1 собирается и тестируется без Part A).
**Даёт дальше:** `score.Config`, `LoadConfig`, `WeightsHash`, `ToolEvent`, `ReadToolEvents`, `Run`, `BuildRuns`, `LatestRun`, `FindRun`, `manifest.ScoreConfig`, фикстура `testdata/run1`. Использует Part B2.
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

_Нет пунктов Review Focus, привязанных к задачам этой части._

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

### Task 6: Go `score` package — config, manifest `score:` section, reader, run grouping, fixture

**Files:**
- Create: `cli/internal/score/config.go`, `cli/internal/score/config_test.go`
- Create: `cli/internal/score/reader.go`, `cli/internal/score/reader_test.go`
- Create: `cli/internal/score/testdata/run1/dispatches.jsonl`, `cli/internal/score/testdata/run1/tool-events.jsonl`, `cli/internal/score/testdata/run1/schema.json`
- Modify: `cli/internal/manifest/project.go` (add `ScoreConfig`, `Score` field, `CarryOverFrom`), `cli/internal/manifest/project_test.go`

**Interfaces:**
- Consumes: `stats.Dispatch`, `stats.ReadDispatches(path)` from `cli/internal/stats`; `manifest.LoadProject(path)`.
- Produces (used by Tasks 7–9):
  ```go
  package score
  type Config struct {
      Enabled      bool
      Weights      map[string]float64 // P1..P7
      Saturation   map[string]float64 // P1..P7
      Thresholds   Thresholds
      MutatingBash []*regexp.Regexp
      MutatingTools map[string]bool   // Edit, Write, MultiEdit, NotebookEdit
      ExemptRoles  map[string]bool    // auditor, auditor-deep
  }
  type Thresholds struct{ Ideal, Solid int }
  func Defaults() Config
  func LoadConfig(projectDir, agentlogDir string) (Config, error) // defaults ← schema.json ← .zprof.yaml
  func (c Config) WeightsHash() string                             // sha1(canonical json)[:12]

  type ToolEvent struct {
      SchemaVersion int    `json:"schema_version"`
      DispatchID    string `json:"dispatch_id"`
      Seq           int    `json:"seq"`
      Ts            string `json:"ts"`
      Tool          string `json:"tool"`
      InputHash     string `json:"input_hash"`
      Target        string `json:"target"`
      IsError       *bool  `json:"is_error,omitempty"`
      ResultChars   int    `json:"result_chars"`
  }
  func ReadToolEvents(path string) ([]ToolEvent, error) // missing file → nil, nil; dedup by (dispatch_id, seq), later row wins

  type Run struct {
      ID         string
      Root       stats.Dispatch            // the task-runner dispatch
      Dispatches []stats.Dispatch          // root + all descendants
      Steps      []stats.Dispatch          // direct children of root, sorted by Timestamp then DispatchID
      Events     map[string][]ToolEvent    // dispatch_id → events sorted by Seq
      RunLog     string                    // Root.Ext["run_log"] or ""
  }
  func BuildRuns(ds []stats.Dispatch, evs []ToolEvent) []Run // one Run per complete task-runner dispatch, sorted by Root.Timestamp asc
  func LatestRun(runs []Run) *Run
  func FindRun(runs []Run, key string) *Run // exact ID, or RunLog equal / suffix, or ID suffix
  ```
- `manifest.ScoreConfig{Enabled *bool; Weights, Saturation map[string]float64; Thresholds *ScoreThresholds{Ideal, Solid int}}` on `ProjectManifest.Score`.

- [ ] **Step 1: Write the fixture — `testdata/run1/dispatches.jsonl`**

Eight rows, one run. Tokens sum to 400 000; `t7` is a failed implementer dispatch (wasted 40 000); `t2` has a preamble; `t7` lacks `ext.run_id` on purpose (exercises the parent walk).

```jsonl
{"schema_version":2,"harness":"claude-code","harness_version":"2.1.220","machine_id":"m","project_id":"p1","ts_utc":"2026-09-26T10:00:00Z","session_id":"s1","dispatch_id":"claude-code:s1:t0","seq":0,"spawn_depth":1,"role":"task-runner","model_resolved":"claude-sonnet-5","verdict":"done","status":"completed","dispatch_complete":true,"tokens_input":15000,"tokens_output":5000,"tokens_cache_read":0,"tokens_cache_creation":0,"tool_uses":7,"duration_ms":1800000,"has_preamble":false,"return_parsed":true,"transcript_captured":true,"ext":{"run_log":".zprof/runs/2026-09-26-fixture.md","run_id":"claude-code:s1:t0"}}
{"schema_version":2,"harness":"claude-code","harness_version":"2.1.220","machine_id":"m","project_id":"p1","ts_utc":"2026-09-26T10:01:00Z","session_id":"s1","dispatch_id":"claude-code:s1:t1","seq":0,"parent_dispatch_id":"claude-code:s1:t0","spawn_depth":2,"role":"planner","model_resolved":"claude-sonnet-5","verdict":"done","status":"completed","dispatch_complete":true,"tokens_input":15000,"tokens_output":5000,"tokens_cache_read":0,"tokens_cache_creation":0,"tool_uses":2,"duration_ms":60000,"has_preamble":false,"return_parsed":true,"artifact_exists":true,"transcript_captured":true,"ext":{"run_id":"claude-code:s1:t0"}}
{"schema_version":2,"harness":"claude-code","harness_version":"2.1.220","machine_id":"m","project_id":"p1","ts_utc":"2026-09-26T10:03:00Z","session_id":"s1","dispatch_id":"claude-code:s1:t7","seq":0,"parent_dispatch_id":"claude-code:s1:t0","spawn_depth":2,"role":"implementer","model_resolved":"claude-sonnet-5","status":"failed","dispatch_complete":true,"tokens_input":30000,"tokens_output":10000,"tokens_cache_read":0,"tokens_cache_creation":0,"tool_uses":0,"duration_ms":30000,"return_parsed":false,"transcript_captured":true}
{"schema_version":2,"harness":"claude-code","harness_version":"2.1.220","machine_id":"m","project_id":"p1","ts_utc":"2026-09-26T10:05:00Z","session_id":"s1","dispatch_id":"claude-code:s1:t2","seq":0,"parent_dispatch_id":"claude-code:s1:t0","spawn_depth":2,"role":"implementer","model_resolved":"claude-sonnet-5","verdict":"done","status":"completed","dispatch_complete":true,"tokens_input":100000,"tokens_output":10000,"tokens_cache_read":10000,"tokens_cache_creation":0,"tool_uses":10,"duration_ms":600000,"has_preamble":true,"return_parsed":true,"artifact_exists":true,"transcript_captured":true,"ext":{"run_id":"claude-code:s1:t0"}}
{"schema_version":2,"harness":"claude-code","harness_version":"2.1.220","machine_id":"m","project_id":"p1","ts_utc":"2026-09-26T10:20:00Z","session_id":"s1","dispatch_id":"claude-code:s1:t3","seq":0,"parent_dispatch_id":"claude-code:s1:t0","spawn_depth":2,"role":"tester","model_resolved":"claude-sonnet-5","verdict":"failed","status":"completed","dispatch_complete":true,"tokens_input":30000,"tokens_output":10000,"tokens_cache_read":0,"tokens_cache_creation":0,"tool_uses":2,"duration_ms":120000,"has_preamble":false,"return_parsed":true,"transcript_captured":true,"ext":{"run_id":"claude-code:s1:t0"}}
{"schema_version":2,"harness":"claude-code","harness_version":"2.1.220","machine_id":"m","project_id":"p1","ts_utc":"2026-09-26T10:25:00Z","session_id":"s1","dispatch_id":"claude-code:s1:t4","seq":0,"parent_dispatch_id":"claude-code:s1:t0","spawn_depth":2,"role":"implementer","model_resolved":"claude-sonnet-5","verdict":"done","status":"completed","dispatch_complete":true,"tokens_input":60000,"tokens_output":10000,"tokens_cache_read":10000,"tokens_cache_creation":0,"tool_uses":4,"duration_ms":300000,"has_preamble":false,"return_parsed":true,"artifact_exists":true,"transcript_captured":true,"ext":{"run_id":"claude-code:s1:t0"}}
{"schema_version":2,"harness":"claude-code","harness_version":"2.1.220","machine_id":"m","project_id":"p1","ts_utc":"2026-09-26T10:35:00Z","session_id":"s1","dispatch_id":"claude-code:s1:t5","seq":0,"parent_dispatch_id":"claude-code:s1:t0","spawn_depth":2,"role":"tester","model_resolved":"claude-sonnet-5","verdict":"done","status":"completed","dispatch_complete":true,"tokens_input":30000,"tokens_output":10000,"tokens_cache_read":0,"tokens_cache_creation":0,"tool_uses":1,"duration_ms":90000,"has_preamble":false,"return_parsed":true,"transcript_captured":true,"ext":{"run_id":"claude-code:s1:t0"}}
{"schema_version":2,"harness":"claude-code","harness_version":"2.1.220","machine_id":"m","project_id":"p1","ts_utc":"2026-09-26T10:40:00Z","session_id":"s1","dispatch_id":"claude-code:s1:t6","seq":0,"parent_dispatch_id":"claude-code:s1:t0","spawn_depth":2,"role":"reviewer","model_resolved":"claude-opus-5-5","verdict":"approve","status":"completed","dispatch_complete":true,"tokens_input":30000,"tokens_output":10000,"tokens_cache_read":0,"tokens_cache_creation":0,"tool_uses":1,"duration_ms":180000,"has_preamble":false,"return_parsed":true,"artifact_exists":true,"transcript_captured":true,"ext":{"run_id":"claude-code:s1:t0"}}
```

- [ ] **Step 2: Write the fixture — `testdata/run1/tool-events.jsonl`**

Twenty events, four errors (rate exactly 0.20). `t2`: one re-read (`a.swift` at seq 3 before any edit), then `swift test` fails three times in a row (two blind retries), an edit, then passes.

```jsonl
{"schema_version":1,"dispatch_id":"claude-code:s1:t1","seq":1,"ts":"2026-09-26T10:00:30Z","tool":"Read","input_hash":"h-spec","target":"/p/SPEC.md","is_error":false,"result_chars":900}
{"schema_version":1,"dispatch_id":"claude-code:s1:t1","seq":2,"ts":"2026-09-26T10:00:50Z","tool":"Write","input_hash":"h-plan","target":"/p/plan-1.md","is_error":false,"result_chars":10}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":1,"ts":"2026-09-26T10:03:10Z","tool":"Read","input_hash":"h-ra","target":"/p/a.swift","is_error":false,"result_chars":500}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":2,"ts":"2026-09-26T10:03:20Z","tool":"Read","input_hash":"h-rb","target":"/p/b.swift","is_error":false,"result_chars":500}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":3,"ts":"2026-09-26T10:03:30Z","tool":"Read","input_hash":"h-ra","target":"/p/a.swift","is_error":false,"result_chars":500}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":4,"ts":"2026-09-26T10:03:40Z","tool":"Read","input_hash":"h-rc","target":"/p/c.swift","is_error":false,"result_chars":500}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":5,"ts":"2026-09-26T10:04:00Z","tool":"Edit","input_hash":"h-ea1","target":"/p/a.swift","is_error":false,"result_chars":10}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":6,"ts":"2026-09-26T10:04:10Z","tool":"Bash","input_hash":"h-test","target":"swift test --package-path Packages/Core","is_error":true,"result_chars":4000}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":7,"ts":"2026-09-26T10:04:30Z","tool":"Bash","input_hash":"h-test","target":"swift test --package-path Packages/Core","is_error":true,"result_chars":4000}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":8,"ts":"2026-09-26T10:04:50Z","tool":"Bash","input_hash":"h-test","target":"swift test --package-path Packages/Core","is_error":true,"result_chars":4000}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":9,"ts":"2026-09-26T10:04:55Z","tool":"Edit","input_hash":"h-ea2","target":"/p/a.swift","is_error":false,"result_chars":10}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":10,"ts":"2026-09-26T10:04:59Z","tool":"Bash","input_hash":"h-test","target":"swift test --package-path Packages/Core","is_error":false,"result_chars":200}
{"schema_version":1,"dispatch_id":"claude-code:s1:t3","seq":1,"ts":"2026-09-26T10:19:00Z","tool":"Bash","input_hash":"h-test","target":"swift test --package-path Packages/Core","is_error":true,"result_chars":4000}
{"schema_version":1,"dispatch_id":"claude-code:s1:t3","seq":2,"ts":"2026-09-26T10:19:30Z","tool":"Read","input_hash":"h-rt","target":"/p/Tests/x.swift","is_error":false,"result_chars":300}
{"schema_version":1,"dispatch_id":"claude-code:s1:t4","seq":1,"ts":"2026-09-26T10:21:00Z","tool":"Read","input_hash":"h-ra","target":"/p/a.swift","is_error":false,"result_chars":500}
{"schema_version":1,"dispatch_id":"claude-code:s1:t4","seq":2,"ts":"2026-09-26T10:22:00Z","tool":"Edit","input_hash":"h-ea3","target":"/p/a.swift","is_error":false,"result_chars":10}
{"schema_version":1,"dispatch_id":"claude-code:s1:t4","seq":3,"ts":"2026-09-26T10:23:00Z","tool":"Bash","input_hash":"h-test","target":"swift test --package-path Packages/Core","is_error":false,"result_chars":200}
{"schema_version":1,"dispatch_id":"claude-code:s1:t4","seq":4,"ts":"2026-09-26T10:24:00Z","tool":"Bash","input_hash":"h-commit","target":"git commit -m fix","is_error":false,"result_chars":80}
{"schema_version":1,"dispatch_id":"claude-code:s1:t5","seq":1,"ts":"2026-09-26T10:34:00Z","tool":"Bash","input_hash":"h-test","target":"swift test --package-path Packages/Core","is_error":false,"result_chars":200}
{"schema_version":1,"dispatch_id":"claude-code:s1:t6","seq":1,"ts":"2026-09-26T10:39:00Z","tool":"Bash","input_hash":"h-diff","target":"git diff HEAD~1","is_error":false,"result_chars":3000}
```

- [ ] **Step 3: Write the fixture — `testdata/run1/schema.json`**

A trimmed deployed schema that overrides one saturation value, so the test proves schema.json is read:

```json
{
  "version": 2,
  "mutating_bash_patterns": ["\\s>>?\\s", "\\bsed\\s+-i\\b", "\\btee\\b", "\\b(mv|cp|rm|touch|mkdir)\\b", "\\bgit\\s+(commit|checkout|stash|reset|apply|cherry-pick|merge|rebase)\\b", "\\bxcodegen\\b"],
  "verdict_exempt_roles": ["auditor", "auditor-deep"],
  "score_defaults": {
    "weights": {"P1": 20, "P2": 15, "P3": 10, "P4": 20, "P5": 10, "P6": 15, "P7": 10},
    "saturation": {"P1": 0.20, "P2": 3, "P3": 0.5, "P4": 2, "P5": 2, "P6": 0.30, "P7": 4},
    "thresholds": {"ideal": 85, "solid": 60}
  }
}
```

- [ ] **Step 4: Write the failing config tests**

Create `cli/internal/score/config_test.go`:

```go
package score

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func fixtureDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "testdata", "run1")
}

func TestDefaults_WeightsSumTo100(t *testing.T) {
	c := Defaults()
	sum := 0.0
	for _, w := range c.Weights {
		sum += w
	}
	require.Equal(t, 100.0, sum)
	require.Len(t, c.Saturation, 7)
	require.Equal(t, Thresholds{Ideal: 85, Solid: 60}, c.Thresholds)
	require.True(t, c.Enabled)
	require.True(t, c.MutatingTools["Edit"])
	require.True(t, c.ExemptRoles["auditor-deep"])
}

func TestDefaults_MutatingBashExcludesBuildAndTest(t *testing.T) {
	c := Defaults()
	for _, cmd := range []string{"swift test --package-path Packages/Core", "cargo build --release", "go test ./...", "pytest -q", "make test", "git status", "cat foo.txt"} {
		require.False(t, c.IsMutatingBash(cmd), cmd)
	}
	for _, cmd := range []string{"cat > f.txt <<'EOF'", "sed -i 's/a/b/' f", "git commit -m x", "rm -rf build", "xcodegen generate", "echo hi | tee out.log"} {
		require.True(t, c.IsMutatingBash(cmd), cmd)
	}
}

func TestLoadConfig_SchemaJsonThenZprofYaml(t *testing.T) {
	proj := t.TempDir()
	agentlog := filepath.Join(proj, ".agentlog")
	require.NoError(t, os.MkdirAll(agentlog, 0o755))
	schema, err := os.ReadFile(filepath.Join(fixtureDir(), "schema.json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(agentlog, "schema.json"), schema, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"), []byte(
		"overlays: [ios-swift]\nscore:\n  weights: {P1: 30, P2: 5}\n  thresholds: {ideal: 90}\n"), 0o644))

	c, err := LoadConfig(proj, agentlog)
	require.NoError(t, err)
	require.Equal(t, 30.0, c.Weights["P1"], ".zprof.yaml overrides")
	require.Equal(t, 5.0, c.Weights["P2"])
	require.Equal(t, 10.0, c.Weights["P3"], "untouched keys keep defaults")
	require.Equal(t, 90, c.Thresholds.Ideal)
	require.Equal(t, 60, c.Thresholds.Solid, "partial thresholds override only what is set")
	require.True(t, c.Enabled)
}

func TestLoadConfig_NoFilesUsesDefaults(t *testing.T) {
	c, err := LoadConfig(t.TempDir(), filepath.Join(t.TempDir(), ".agentlog"))
	require.NoError(t, err)
	require.Equal(t, Defaults().Weights, c.Weights)
}

func TestLoadConfig_EnabledFalse(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"), []byte("overlays: []\nscore:\n  enabled: false\n"), 0o644))
	c, err := LoadConfig(proj, filepath.Join(proj, ".agentlog"))
	require.NoError(t, err)
	require.False(t, c.Enabled)
}

func TestWeightsHash_StableAndSensitive(t *testing.T) {
	a := Defaults()
	b := Defaults()
	require.Equal(t, a.WeightsHash(), b.WeightsHash())
	require.Len(t, a.WeightsHash(), 12)
	b.Weights["P1"] = 21
	require.NotEqual(t, a.WeightsHash(), b.WeightsHash())
	c := Defaults()
	c.Thresholds.Solid = 61
	require.NotEqual(t, a.WeightsHash(), c.WeightsHash())
}
```

- [ ] **Step 5: Run to verify failure**

Run: `cd cli && go test ./internal/score/ -count=1`
Expected: FAIL — `undefined: Defaults` (package does not compile yet).

- [ ] **Step 6: Add `ScoreConfig` to the manifest**

In `cli/internal/manifest/project.go`, add after the `Audit` field:

```go
	// Score configures `zprof score` (per-task scorecard). Nil = defaults
	// from telemetry.yaml / the compiled-in table; enabled unless
	// `enabled: false` is set explicitly.
	Score *ScoreConfig `yaml:"score,omitempty"`
```

and after `ABExperiment`:

```go
// ScoreConfig overrides scorecard weights, saturation points and tier thresholds.
// Only keys present override; the rest fall back to defaults.
type ScoreConfig struct {
	Enabled    *bool              `yaml:"enabled,omitempty"`
	Weights    map[string]float64 `yaml:"weights,omitempty"`
	Saturation map[string]float64 `yaml:"saturation,omitempty"`
	Thresholds *ScoreThresholds   `yaml:"thresholds,omitempty"`
}

// ScoreThresholds are tier cut-offs; zero means "not set".
type ScoreThresholds struct {
	Ideal int `yaml:"ideal,omitempty"`
	Solid int `yaml:"solid,omitempty"`
}
```

In `CarryOverFrom` add:

```go
	if m.Score == nil {
		m.Score = prev.Score
	}
```

Append to `cli/internal/manifest/project_test.go`:

```go
func TestLoadProjectManifest_ScoreSection(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".zprof.yaml")
	require.NoError(t, os.WriteFile(p, []byte("overlays: [x]\nscore:\n  enabled: false\n  weights: {P1: 30}\n  thresholds: {ideal: 90}\n"), 0o644))
	m, err := LoadProject(p)
	require.NoError(t, err)
	require.NotNil(t, m.Score)
	require.NotNil(t, m.Score.Enabled)
	require.False(t, *m.Score.Enabled)
	require.Equal(t, 30.0, m.Score.Weights["P1"])
	require.Equal(t, 90, m.Score.Thresholds.Ideal)
	require.Equal(t, 0, m.Score.Thresholds.Solid)
}

func TestCarryOverFrom_KeepsScore(t *testing.T) {
	enabled := false
	prev := &ProjectManifest{Score: &ScoreConfig{Enabled: &enabled}}
	m := &ProjectManifest{}
	m.CarryOverFrom(prev)
	require.Same(t, prev.Score, m.Score)
}
```

(Add `"os"` and `"path/filepath"` to that test file's imports if missing.)

Run: `cd cli && go test ./internal/manifest/ -count=1` → PASS.

- [ ] **Step 7: Implement `config.go`**

```go
// Package score computes the per-task scorecard from .agentlog/ data
// (spec: docs/superpowers/specs/2026-09-26-task-scorecard-design.md).
// It never reads raw Claude Code session logs — only what the Python
// collector already normalized.
package score

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/vaporphd/zprof/internal/manifest"
)

// PenaltyIDs is the fixed order of the seven penalties.
var PenaltyIDs = []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7"}

type Thresholds struct {
	Ideal int `json:"ideal"`
	Solid int `json:"solid"`
}

type Config struct {
	Enabled       bool
	Weights       map[string]float64
	Saturation    map[string]float64
	Thresholds    Thresholds
	MutatingBash  []*regexp.Regexp
	MutatingTools map[string]bool
	ExemptRoles   map[string]bool
}

var defaultMutatingBash = []string{
	`\s>>?\s`,
	`\bsed\s+-i\b`,
	`\btee\b`,
	`\b(mv|cp|rm|touch|mkdir)\b`,
	`\bgit\s+(commit|checkout|stash|reset|apply|cherry-pick|merge|rebase)\b`,
	`\bxcodegen\b`,
	`\b(cargo|go|swift)\s+fmt\b`,
	`\b(gofmt\s+-w|swiftformat|rustfmt)\b`,
}

// Defaults mirrors telemetry.yaml `score_defaults`; keep the two in sync.
func Defaults() Config {
	c := Config{
		Enabled:    true,
		Weights:    map[string]float64{"P1": 20, "P2": 15, "P3": 10, "P4": 20, "P5": 10, "P6": 15, "P7": 10},
		Saturation: map[string]float64{"P1": 0.20, "P2": 3, "P3": 0.5, "P4": 2, "P5": 2, "P6": 0.30, "P7": 4},
		Thresholds: Thresholds{Ideal: 85, Solid: 60},
		MutatingTools: map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true},
		ExemptRoles:   map[string]bool{"auditor": true, "auditor-deep": true},
	}
	c.MutatingBash = compilePatterns(defaultMutatingBash)
	return c
}

func compilePatterns(pats []string) []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, p := range pats {
		if re, err := regexp.Compile(p); err == nil {
			out = append(out, re)
		}
	}
	return out
}

// IsMutatingBash reports whether a Bash command changes files/state.
func (c Config) IsMutatingBash(command string) bool {
	for _, re := range c.MutatingBash {
		if re.MatchString(command) {
			return true
		}
	}
	return false
}

// schemaFile is the subset of .agentlog/schema.json (telemetry.yaml as JSON) we read.
type schemaFile struct {
	MutatingBashPatterns []string `json:"mutating_bash_patterns"`
	VerdictExemptRoles   []string `json:"verdict_exempt_roles"`
	ScoreDefaults        *struct {
		Weights    map[string]float64 `json:"weights"`
		Saturation map[string]float64 `json:"saturation"`
		Thresholds *Thresholds        `json:"thresholds"`
	} `json:"score_defaults"`
}

// LoadConfig layers: compiled defaults ← <agentlogDir>/schema.json ← <projectDir>/.zprof.yaml.
// Missing files are not errors; malformed ones are.
func LoadConfig(projectDir, agentlogDir string) (Config, error) {
	c := Defaults()

	if data, err := os.ReadFile(filepath.Join(agentlogDir, "schema.json")); err == nil {
		var s schemaFile
		if err := json.Unmarshal(data, &s); err != nil {
			return c, fmt.Errorf("parse schema.json: %w", err)
		}
		if len(s.MutatingBashPatterns) > 0 {
			c.MutatingBash = compilePatterns(s.MutatingBashPatterns)
		}
		if len(s.VerdictExemptRoles) > 0 {
			c.ExemptRoles = map[string]bool{}
			for _, r := range s.VerdictExemptRoles {
				c.ExemptRoles[r] = true
			}
		}
		if s.ScoreDefaults != nil {
			mergeFloats(c.Weights, s.ScoreDefaults.Weights)
			mergeFloats(c.Saturation, s.ScoreDefaults.Saturation)
			if s.ScoreDefaults.Thresholds != nil {
				mergeThresholds(&c.Thresholds, *s.ScoreDefaults.Thresholds)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return c, fmt.Errorf("read schema.json: %w", err)
	}

	manifestPath := filepath.Join(projectDir, ".zprof.yaml")
	if _, err := os.Stat(manifestPath); err == nil {
		m, err := manifest.LoadProject(manifestPath)
		if err != nil {
			return c, fmt.Errorf("load .zprof.yaml: %w", err)
		}
		if m.Score != nil {
			if m.Score.Enabled != nil {
				c.Enabled = *m.Score.Enabled
			}
			mergeFloats(c.Weights, m.Score.Weights)
			mergeFloats(c.Saturation, m.Score.Saturation)
			if m.Score.Thresholds != nil {
				mergeThresholds(&c.Thresholds, Thresholds{Ideal: m.Score.Thresholds.Ideal, Solid: m.Score.Thresholds.Solid})
			}
		}
	}
	return c, nil
}

func mergeFloats(dst, src map[string]float64) {
	for k, v := range src {
		if _, known := dst[k]; known {
			dst[k] = v
		}
	}
}

func mergeThresholds(dst *Thresholds, src Thresholds) {
	if src.Ideal > 0 {
		dst.Ideal = src.Ideal
	}
	if src.Solid > 0 {
		dst.Solid = src.Solid
	}
}

// WeightsHash identifies the scoring parameters so historical rows stay comparable.
// encoding/json sorts map keys, so the encoding is canonical.
func (c Config) WeightsHash() string {
	keys := make([]string, 0, len(c.Weights))
	for k := range c.Weights {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	payload := struct {
		Weights    map[string]float64 `json:"weights"`
		Saturation map[string]float64 `json:"saturation"`
		Thresholds Thresholds         `json:"thresholds"`
	}{c.Weights, c.Saturation, c.Thresholds}
	data, _ := json.Marshal(payload)
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])[:12]
}
```

- [ ] **Step 8: Run the config tests**

Run: `cd cli && go test ./internal/score/ -run 'Defaults|LoadConfig|WeightsHash' -count=1`
Expected: PASS.

- [ ] **Step 9: Write the failing reader tests**

Create `cli/internal/score/reader_test.go`:

```go
package score

import (
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
```

Add a tiny helper at the bottom of `reader_test.go`:

```go
func writeFile(path, content string) error { return os.WriteFile(path, []byte(content), 0o644) }
```

(and import `"os"`).

- [ ] **Step 10: Implement `reader.go`**

```go
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
	ResultChars   int    `json:"result_chars"`
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
```

- [ ] **Step 11: Run all package tests**

Run: `cd cli && go test ./internal/score/ ./internal/manifest/ -count=1`
Expected: PASS.

- [ ] **Step 12: Commit**

```bash
git add cli/internal/score cli/internal/manifest
git commit -m "feat(cli): score package — config layering, tool-events reader, run grouping"
```

---
