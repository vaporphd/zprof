# Per-task Scorecard (Phase 1) Implementation Plan — Part C — проводка: `zprof score`, Stop-хук, AGENT_LOOP, e2e

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After every task-runner run, `zprof score` prints a 0–100 card with per-role breakdown, appends a row to `.agentlog/scores.jsonl` and a `## Score` section to the run log — computed deterministically from `.agentlog/`.

**Architecture:** The Python collector (`profiles/base/zprof-collect.py`, stdlib only) extracts new facts into `.agentlog/`: full rows for nested dispatches (C1), the `verdict` value and `ext.{next,artifact,run_log}` (C2), an ordered `tool-events.jsonl` with `is_error` (C3), `ext.run_id` (C4). A new Go package `cli/internal/score` reads `.agentlog/` only, groups dispatches into runs, computes penalties P1–P7, renders the card and persists it. `zprof score` wires it; the Stop hook runs it after the collector; `AGENT_LOOP.md` tells main to paste the card into `followup.md`.

**Tech Stack:** Go 1.22 (cobra, testify/require, yaml.v3, stdlib `regexp`/`crypto/sha1`), Python 3.10+ stdlib only (pytest for tests).

**Spec:** `docs/superpowers/specs/2026-09-26-task-scorecard-design.md`
## Место в плане

**Зависит от:** Part A (коллектор для flush и e2e) и Parts B1–B3 (пакет `score`).
**Даёт дальше:** Команда `zprof score`, обновлённый Stop-хук в `apply/settings.go`, правило в `agent-loop-router.md`, сквозной тест `test_e2e_score.py`.
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

**Prompt layer**
- Modify `profiles/base/agent-loop-router.md` — rule: after task-runner returns, run `zprof score`, paste 3–4 card lines into `followup.md`.

---

### Task 9: `zprof score` command

**Files:**
- Create: `cli/internal/cmd/score.go`, `cli/internal/cmd/score_test.go`
- Modify: `cli/cmd/zprof/main.go` (register `cmd.NewScoreCmd(version)`)

**Interfaces:**
- Consumes: `score.LoadConfig`, `stats.ReadDispatches`, `score.ReadToolEvents`, `score.BuildRuns`, `score.LatestRun`, `score.FindRun`, `score.Compute`, `score.RenderCard`, `score.AppendScore`, `score.ReadScoredKeys`, `score.ScoreKey`, `score.WriteRunLogSection`, `eval.LocateSession`.
- Produces: `func NewScoreCmd(version string) *cobra.Command`. Flags: `--latest` (default true when no other selector), `--run <id|run_log|slug>`, `--all-missing`, `--json`, `--no-collect`, `--quiet`, `--project <dir>` (default cwd), `--agentlog <dir>` (default `<project>/.agentlog`). Exit code 0 on "no run to score" (prints `unscored: …`).

- [ ] **Step 1: Write the failing command test**

Create `cli/internal/cmd/score_test.go`:

```go
package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func scoreFixtureDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "score", "testdata", "run1")
}

func setupScoreProject(t *testing.T) (proj, agentlog string) {
	t.Helper()
	proj = t.TempDir()
	agentlog = filepath.Join(proj, ".agentlog")
	require.NoError(t, os.MkdirAll(agentlog, 0o755))
	for _, name := range []string{"dispatches.jsonl", "tool-events.jsonl", "schema.json"} {
		data, err := os.ReadFile(filepath.Join(scoreFixtureDir(), name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(agentlog, name), data, 0o644))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(proj, ".zprof", "runs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof", "runs", "2026-09-26-fixture.md"),
		[]byte("# fixture\n\n## Итог\nverdict: done\n"), 0o644))
	return proj, agentlog
}

func runScore(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := NewScoreCmd("test")
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(args)
	err := c.Execute()
	return out.String(), err
}

func TestScoreCmd_LatestPrintsCardAndPersists(t *testing.T) {
	proj, agentlog := setupScoreProject(t)
	out, err := runScore(t, "--project", proj, "--no-collect")
	require.NoError(t, err)
	require.Contains(t, out, "Score 45/100 · Lucky · done · 2026-09-26-fixture")

	scores, err := os.ReadFile(filepath.Join(agentlog, "scores.jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(scores), `"run_id":"claude-code:s1:t0"`)

	runLog, err := os.ReadFile(filepath.Join(proj, ".zprof", "runs", "2026-09-26-fixture.md"))
	require.NoError(t, err)
	require.Contains(t, string(runLog), "<!-- zprof:score:begin -->")
	require.Contains(t, string(runLog), "Score 45/100")
}

func TestScoreCmd_JSON(t *testing.T) {
	proj, _ := setupScoreProject(t)
	out, err := runScore(t, "--project", proj, "--no-collect", "--json")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(strings.TrimSpace(out), "{"), out)
	require.Contains(t, out, `"tier":"Lucky"`)
}

func TestScoreCmd_RunSelectorAndQuiet(t *testing.T) {
	proj, agentlog := setupScoreProject(t)
	out, err := runScore(t, "--project", proj, "--no-collect", "--quiet", "--run", "2026-09-26-fixture")
	require.NoError(t, err)
	require.Equal(t, "", out)
	_, err = os.Stat(filepath.Join(agentlog, "scores.jsonl"))
	require.NoError(t, err)
}

func TestScoreCmd_AllMissingIsIdempotent(t *testing.T) {
	proj, agentlog := setupScoreProject(t)
	_, err := runScore(t, "--project", proj, "--no-collect", "--all-missing", "--quiet")
	require.NoError(t, err)
	out, err := runScore(t, "--project", proj, "--no-collect", "--all-missing")
	require.NoError(t, err)
	require.Contains(t, out, "nothing to score")
	data, _ := os.ReadFile(filepath.Join(agentlog, "scores.jsonl"))
	require.Len(t, strings.Split(strings.TrimRight(string(data), "\n"), "\n"), 1)
}

func TestScoreCmd_NoRunIsNotAnError(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(proj, ".agentlog"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".agentlog", "dispatches.jsonl"), []byte(""), 0o644))
	out, err := runScore(t, "--project", proj, "--no-collect")
	require.NoError(t, err)
	require.Contains(t, out, "unscored: no task-runner dispatch")
}

func TestScoreCmd_DisabledInManifest(t *testing.T) {
	proj, _ := setupScoreProject(t)
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"), []byte("overlays: []\nscore:\n  enabled: false\n"), 0o644))
	out, err := runScore(t, "--project", proj, "--no-collect")
	require.NoError(t, err)
	require.Contains(t, out, "score disabled")
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd cli && go test ./internal/cmd/ -run ScoreCmd -count=1`
Expected: FAIL — `undefined: NewScoreCmd`.

- [ ] **Step 3: Implement `cmd/score.go`**

```go
// cli/internal/cmd/score.go
package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vaporphd/zprof/internal/eval"
	"github.com/vaporphd/zprof/internal/score"
	"github.com/vaporphd/zprof/internal/stats"
)

// NewScoreCmd returns `zprof score` — the per-task scorecard (spec
// docs/superpowers/specs/2026-09-26-task-scorecard-design.md §8).
func NewScoreCmd(version string) *cobra.Command {
	var (
		latest     bool
		runKey     string
		allMissing bool
		asJSON     bool
		noCollect  bool
		quiet      bool
		projectDir string
		agentlog   string
	)
	c := &cobra.Command{
		Use:   "score",
		Short: "Per-task scorecard (0–100) for the latest task-runner run",
		Long: `Reads .agentlog/dispatches.jsonl and tool-events.jsonl, groups dispatches
into task-runner runs, scores penalties P1–P7 and prints a card. Appends the
result to .agentlog/scores.jsonl and writes a ## Score section into the run
log. Deterministic — no LLM, zero tokens.

Unless --no-collect is given, the collector is flushed first so the current
session's finished dispatches are visible immediately.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			var err error
			if projectDir == "" {
				if projectDir, err = os.Getwd(); err != nil {
					return fmt.Errorf("getwd: %w", err)
				}
			}
			if agentlog == "" {
				agentlog = filepath.Join(projectDir, ".agentlog")
			}

			cfg, err := score.LoadConfig(projectDir, agentlog)
			if err != nil {
				return err
			}
			if !cfg.Enabled {
				if !quiet {
					fmt.Fprintln(out, "score disabled in .zprof.yaml (score.enabled: false)")
				}
				return nil
			}

			if !noCollect {
				if err := flushCollector(projectDir); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warn: collector flush skipped: %v\n", err)
				}
			}

			ds, _, err := stats.ReadDispatches(filepath.Join(agentlog, "dispatches.jsonl"))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("read dispatches: %w", err)
			}
			evs, err := score.ReadToolEvents(filepath.Join(agentlog, "tool-events.jsonl"))
			if err != nil {
				return err
			}
			runs := score.BuildRuns(ds, evs)

			var targets []score.Run
			switch {
			case allMissing:
				keys, err := score.ReadScoredKeys(filepath.Join(agentlog, "scores.jsonl"))
				if err != nil {
					return err
				}
				hash := cfg.WeightsHash()
				for _, r := range runs {
					if !keys[r.ID+"|"+hash] {
						targets = append(targets, r)
					}
				}
				if len(targets) == 0 {
					if !quiet {
						fmt.Fprintln(out, "nothing to score: every complete run already has a row for the current weights")
					}
					return nil
				}
			case runKey != "":
				r := score.FindRun(runs, runKey)
				if r == nil {
					return fmt.Errorf("no run matches %q (have %d runs)", runKey, len(runs))
				}
				targets = []score.Run{*r}
			default:
				_ = latest
				r := score.LatestRun(runs)
				if r == nil {
					if !quiet {
						fmt.Fprintln(out, "unscored: no task-runner dispatch in .agentlog (main dispatched directly, or run not finished)")
					}
					return nil
				}
				targets = []score.Run{*r}
			}

			for i, r := range targets {
				card := score.Compute(r, cfg, version)
				if err := score.AppendScore(filepath.Join(agentlog, "scores.jsonl"), card); err != nil {
					return err
				}
				rendered := score.RenderCard(card)
				if card.RunLog != "" {
					runLogPath := card.RunLog
					if !filepath.IsAbs(runLogPath) {
						runLogPath = filepath.Join(projectDir, runLogPath)
					}
					if err := score.WriteRunLogSection(runLogPath, rendered); err != nil && !errors.Is(err, os.ErrNotExist) {
						return err
					}
				}
				if quiet {
					continue
				}
				if asJSON {
					data, _ := json.Marshal(card)
					fmt.Fprintln(out, string(data))
				} else {
					if i > 0 {
						fmt.Fprintln(out, strings.Repeat("─", 60))
					}
					fmt.Fprint(out, rendered)
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&latest, "latest", true, "Score the most recent complete run (default)")
	c.Flags().StringVar(&runKey, "run", "", "Score a specific run: run_id, run log path, or slug suffix")
	c.Flags().BoolVar(&allMissing, "all-missing", false, "Score every complete run without a row for the current weights")
	c.Flags().BoolVar(&asJSON, "json", false, "Print the scores.jsonl row instead of the card")
	c.Flags().BoolVar(&noCollect, "no-collect", false, "Do not flush the collector first")
	c.Flags().BoolVar(&quiet, "quiet", false, "Persist only, print nothing (for hooks)")
	c.Flags().StringVar(&projectDir, "project", "", "Project directory (default: cwd)")
	c.Flags().StringVar(&agentlog, "agentlog", "", "Telemetry directory (default: <project>/.agentlog)")
	return c
}

// flushCollector runs the deployed collector in `stop` mode against the most
// recent session log for projectDir, feeding it the same JSON payload the
// Claude Code hook would. Missing collector or session is reported, not fatal.
func flushCollector(projectDir string) error {
	script := filepath.Join(projectDir, ".claude", "zprof-collect.py")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("no collector at %s", script)
	}
	transcript, err := eval.LocateSession("", projectDir)
	if err != nil {
		return err
	}
	sessionID := strings.TrimSuffix(filepath.Base(transcript), ".jsonl")
	payload, _ := json.Marshal(map[string]any{
		"session_id":      sessionID,
		"transcript_path": transcript,
		"cwd":             projectDir,
	})
	cmd := exec.Command("python3", script, "stop")
	cmd.Dir = projectDir
	cmd.Stdin = bytes.NewReader(payload)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("collector: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
```

- [ ] **Step 4: Register the command**

In `cli/cmd/zprof/main.go` add after `root.AddCommand(cmd.NewStatsCmd())`:

```go
	root.AddCommand(cmd.NewScoreCmd(version))
```

- [ ] **Step 5: Run tests and a real invocation**

Run: `cd cli && go test ./internal/cmd/ -run ScoreCmd -count=1 && go build ./... && go run ./cmd/zprof score --help | head -5`
Expected: tests PASS; help text prints.

- [ ] **Step 6: Commit**

```bash
git add cli/internal/cmd/score.go cli/internal/cmd/score_test.go cli/cmd/zprof/main.go
git commit -m "feat(cli): zprof score — per-task scorecard command"
```

---

### Task 10: Stop hook chains `zprof score` after the collector

**Files:**
- Modify: `cli/internal/apply/settings.go`
- Test: `cli/internal/apply/settings_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `.claude/settings.local.json` `Stop` hook command = collector guard command, then `; command -v zprof >/dev/null 2>&1 && cd "$CLAUDE_PROJECT_DIR" && zprof score --latest --quiet --no-collect || true`. Old installs (Stop command without `zprof score`) are upgraded in place on the next `zprof apply` / `zprof sync`.

Why one chained command instead of a second hook entry: Claude Code runs the hooks of one event in parallel; `zprof score --no-collect` must run *after* the collector has written `.agentlog/`.

- [ ] **Step 1: Write the failing tests**

Append to `cli/internal/apply/settings_test.go`:

```go
func stopCommands(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	require.NoError(t, err)
	var settings map[string]any
	require.NoError(t, json.Unmarshal(data, &settings))
	entries := settings["hooks"].(map[string]any)["Stop"].([]any)
	var cmds []string
	for _, e := range entries {
		for _, h := range e.(map[string]any)["hooks"].([]any) {
			cmds = append(cmds, h.(map[string]any)["command"].(string))
		}
	}
	return cmds
}

func TestEnsureHooksStopChainsScore(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, EnsureHooks(dir))
	cmds := stopCommands(t, dir)
	require.Len(t, cmds, 1, "one chained command, not two parallel hooks")
	c := cmds[0]
	require.Contains(t, c, `zprof-collect.py" stop`)
	require.Contains(t, c, "zprof score --latest --quiet --no-collect")
	require.Less(t, strings.Index(c, "zprof-collect.py"), strings.Index(c, "zprof score"), "collector runs first")
	require.Contains(t, c, `command -v zprof >/dev/null 2>&1 &&`, "score is skipped when zprof is not installed")
	require.True(t, strings.HasSuffix(c, "|| true"))

	// other events do not get the score chain
	data, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	require.Equal(t, 1, strings.Count(string(data), "zprof score"))
}

func TestEnsureHooksUpgradesOldStopCommand(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))
	old := map[string]any{"hooks": map[string]any{
		"Stop": []any{map[string]any{"hooks": []any{map[string]any{
			"type":    "command",
			"command": `test -x "$CLAUDE_PROJECT_DIR/.claude/zprof-collect.py" && "$CLAUDE_PROJECT_DIR/.claude/zprof-collect.py" stop || true`,
		}}}},
	}}
	data, _ := json.MarshalIndent(old, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), data, 0o644))

	require.NoError(t, EnsureHooks(dir))
	cmds := stopCommands(t, dir)
	require.Len(t, cmds, 1, "old entry replaced, not duplicated")
	require.Contains(t, cmds[0], "zprof score --latest --quiet --no-collect")

	require.NoError(t, EnsureHooks(dir)) // and it stays stable
	require.Len(t, stopCommands(t, dir), 1)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd cli && go test ./internal/apply/ -run 'EnsureHooks' -count=1`
Expected: FAIL — `TestEnsureHooksStopChainsScore`: command lacks "zprof score".

- [ ] **Step 3: Implement**

In `settings.go`, replace the `telemetryHooks` map and the loop body with an ordered spec list plus an upsert that replaces a stale zprof entry:

```go
// scoreHookCommand runs the per-task scorecard after the collector. It is
// chained into the same Stop command because Claude Code runs an event's
// hooks in parallel, and `zprof score --no-collect` must see the collector's
// output. `command -v zprof` keeps projects without the binary silent.
const scoreHookCommand = `command -v zprof >/dev/null 2>&1 && cd "$CLAUDE_PROJECT_DIR" && zprof score --latest --quiet --no-collect || true`

type hookSpec struct {
	event   string
	command string
}

// telemetryHooks lists, in install order, the command each hook event runs.
var telemetryHooks = []hookSpec{
	{"SubagentStop", fmt.Sprintf(hookGuardTemplate, "subagent-stop")},
	{"Stop", fmt.Sprintf(hookGuardTemplate, "stop") + "; " + scoreHookCommand},
	{"SessionStart", fmt.Sprintf(hookGuardTemplate, "session-start")},
}
```

Replace the `for event, mode := range telemetryHooks { … }` loop with:

```go
	for _, spec := range telemetryHooks {
		entry := map[string]any{
			"hooks": []any{
				map[string]any{"type": "command", "command": spec.command},
			},
		}
		existing, _ := hooks[spec.event].([]any)
		if idx := zprofHookIndex(existing); idx >= 0 {
			if hookCommand(existing[idx]) != spec.command {
				existing[idx] = entry // stale zprof hook (older command shape) → upgrade in place
				hooks[spec.event] = existing
			}
			continue
		}
		hooks[spec.event] = append(existing, entry)
	}
```

Replace `hasZprofHook` with:

```go
// zprofHookIndex returns the position of the entry that invokes
// zprof-collect.py, or -1. Used both to skip duplicates and to upgrade a
// stale command in place.
func zprofHookIndex(entries []any) int {
	for i, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "zprof-collect.py") {
			return i
		}
	}
	return -1
}

// hookCommand extracts the first command string of a hook entry ("" if malformed).
func hookCommand(entry any) string {
	m, _ := entry.(map[string]any)
	hs, _ := m["hooks"].([]any)
	if len(hs) == 0 {
		return ""
	}
	h, _ := hs[0].(map[string]any)
	s, _ := h["command"].(string)
	return s
}
```

Update the doc comment on `EnsureHooks` to mention the Stop chain and the in-place upgrade.

- [ ] **Step 4: Run the apply tests**

Run: `cd cli && go test ./internal/apply/ -count=1`
Expected: PASS. `TestEnsureHooksIdempotent` still expects 6 occurrences of `zprof-collect.py` — the Stop chain keeps exactly two.

- [ ] **Step 5: Verify on a real project (dry, no commit there)**

Run: `cd /Volumes/mydata/projects/apple-health-sync && zprof sync --help >/dev/null && go run /Volumes/mydata/projects/zprof/cli/cmd/zprof apply ios-swift --dry-run`
Expected: exit 0. (Actual hook rewrite happens on the next real `zprof apply`/`sync`, outside this plan.)

- [ ] **Step 6: Commit**

```bash
git add cli/internal/apply/settings.go cli/internal/apply/settings_test.go
git commit -m "feat(cli): Stop hook chains zprof score after the collector; upgrade stale hook in place"
```

---

### Task 11: AGENT_LOOP rule + cross-language smoke test

**Files:**
- Modify: `profiles/base/agent-loop-router.md` (result table row `done`, isolation rule 2)
- Test: `profiles/base/tests/test_e2e_score.py` (new; runs the collector on a nested fixture, then `go run ./cmd/zprof score`)

**Interfaces:**
- Consumes: everything above.
- Produces: the main-session rule that closes the loop (spec §7 «followup.md»).

- [ ] **Step 1: Edit the router**

In `profiles/base/agent-loop-router.md`, replace the `done` row of the «Что делать с результатом» table:

```markdown
| `done` | Сообщи пользователю `one_line` и `artifact`. Выполни `zprof score` и покажи карточку. Ничего не цитируй сверх этого. |
```

Replace isolation rule 2:

```markdown
2. После каждого dispatch раннера выполни `zprof score` (без аргументов) и
   впиши первые 4 строки карточки в `followup.md` вместо строк предыдущего
   run. Если `zprof` недоступен — ≤3 строки статуса, как раньше. Ответ
   раннера выброси из рабочей памяти. Балл — детерминированный, из
   `.agentlog/`; не пересчитывай его словами и не спорь с ним в followup.
```

- [ ] **Step 2: Write the smoke test**

Create `profiles/base/tests/test_e2e_score.py`:

```python
#!/usr/bin/env python3
"""Cross-language smoke: collector → .agentlog → `zprof score` card.

Skipped when `go` is not on PATH. Builds the nested-runner fixture from
test_nested_dispatches, runs the collector in `stop` mode via subprocess
(exactly like the hook), then runs the Go command against the result.
"""
import json, os, pathlib, shutil, subprocess, sys
import importlib
import pytest

HERE = pathlib.Path(__file__).parent
REPO = HERE.parent.parent.parent          # …/zprof
COLLECTOR = HERE.parent / "zprof-collect.py"

_spec = importlib.util.spec_from_file_location("test_nested_dispatches", HERE / "test_nested_dispatches.py")
nested = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(nested)


@pytest.mark.skipif(shutil.which("go") is None, reason="go toolchain not installed")
def test_collector_then_zprof_score(tmp_path):
    proj = tmp_path / "proj"
    proj.mkdir()
    logs = tmp_path / "logs"
    logs.mkdir()
    main, sub = nested._runner_tree(logs)
    # give the implementer child a failing test loop so the card has a finding
    child_transcript = sub / "agent-aaaa01.jsonl"
    lines = child_transcript.read_text().splitlines()
    lines.insert(1, json.dumps({"type": "assistant", "timestamp": "2026-09-26T10:03:00Z",
                                "message": {"role": "assistant", "model": "claude-sonnet-5",
                                            "content": [{"type": "tool_use", "id": "tuA", "name": "Bash",
                                                         "input": {"command": "swift test"}}],
                                            "usage": {"input_tokens": 1, "output_tokens": 1}}}))
    lines.insert(2, json.dumps({"type": "user", "timestamp": "2026-09-26T10:03:05Z",
                                "message": {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tuA",
                                                                          "content": "error", "is_error": True}]}}))
    lines.insert(3, json.dumps({"type": "assistant", "timestamp": "2026-09-26T10:03:10Z",
                                "message": {"role": "assistant", "model": "claude-sonnet-5",
                                            "content": [{"type": "tool_use", "id": "tuB", "name": "Bash",
                                                         "input": {"command": "swift test"}}],
                                            "usage": {"input_tokens": 1, "output_tokens": 1}}}))
    lines.insert(4, json.dumps({"type": "user", "timestamp": "2026-09-26T10:03:15Z",
                                "message": {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tuB",
                                                                          "content": "error", "is_error": True}]}}))
    child_transcript.write_text("\n".join(lines) + "\n")

    payload = json.dumps({"session_id": main.stem, "transcript_path": str(main), "cwd": str(proj)})
    r = subprocess.run([sys.executable, str(COLLECTOR), "stop"], input=payload, text=True,
                       capture_output=True, cwd=str(proj), timeout=60)
    assert r.returncode == 0, r.stderr
    agentlog = proj / ".agentlog"
    assert (agentlog / "dispatches.jsonl").exists()
    assert (agentlog / "tool-events.jsonl").exists(), (agentlog / "collect.log").read_text() if (agentlog / "collect.log").exists() else "no log"

    r = subprocess.run(["go", "run", "./cmd/zprof", "score", "--project", str(proj), "--no-collect"],
                       cwd=str(REPO / "cli"), capture_output=True, text=True, timeout=300)
    assert r.returncode == 0, r.stderr
    out = r.stdout
    assert out.startswith("Score "), out
    assert "/100 ·" in out and "· done ·" in out
    assert "P2 implementer: 1×" in out, out          # the blind retry we injected
    assert "implementer" in out and "task-runner" in out
    rows = [json.loads(l) for l in (agentlog / "scores.jsonl").read_text().splitlines()]
    assert rows[-1]["tier"] in ("Ideal", "Solid", "Lucky")
    assert rows[-1]["penalties"][1]["id"] == "P2" and rows[-1]["penalties"][1]["value"] == 1
```

- [ ] **Step 3: Run it**

Run: `python3 -m pytest profiles/base/tests/test_e2e_score.py -v`
Expected: PASS (first run compiles Go; allow a minute). If `tool-events.jsonl` is missing, read the printed `collect.log` — the C3 call site is inside the pass-2 loop and must run before `agents_done.add`.

- [ ] **Step 4: Run everything**

Run: `python3 -m pytest profiles/base/tests/ profiles/base/telemetry_test.py -q && cd cli && go test ./... -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add profiles/base/agent-loop-router.md profiles/base/tests/test_e2e_score.py
git commit -m "feat(base): AGENT_LOOP runs zprof score after each runner dispatch; e2e smoke"
```

---

## Out of scope for this plan (spec phases 2–3)

`zprof stats` «Runs» card, repeated-finding signal for `eval-telemetry`, `$` on the card (needs prices in the model registry), `is_error` allowlist, `cat`/`sed -n` in P3, and the pr-shepherd gate. Rolling the new hook out to existing projects is a normal `zprof sync` in each project (jarvis-in-hermes, apple-health-sync), done by Alex after the release. Existing `.agentlog/` data there was written by the old collector (no `tool-events.jsonl`, thin nested rows), so `zprof score --all-missing` on it yields `confidence: partial` cards at best; the first real cards come from runs made after the sync. The spec's «ручная проверка на живом проекте» therefore happens on the first post-sync task-runner run, not inside this plan.
