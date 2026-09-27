# Per-task Scorecard (Phase 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After every task-runner run, `zprof score` prints a 0–100 card with per-role breakdown, appends a row to `.agentlog/scores.jsonl` and a `## Score` section to the run log — computed deterministically from `.agentlog/`.

**Architecture:** The Python collector (`profiles/base/zprof-collect.py`, stdlib only) extracts new facts into `.agentlog/`: full rows for nested dispatches (C1), the `verdict` value and `ext.{next,artifact,run_log}` (C2), an ordered `tool-events.jsonl` with `is_error` (C3), `ext.run_id` (C4). A new Go package `cli/internal/score` reads `.agentlog/` only, groups dispatches into runs, computes penalties P1–P7, renders the card and persists it. `zprof score` wires it; the Stop hook runs it after the collector; `AGENT_LOOP.md` tells main to paste the card into `followup.md`.

**Tech Stack:** Go 1.22 (cobra, testify/require, yaml.v3, stdlib `regexp`/`crypto/sha1`), Python 3.10+ stdlib only (pytest for tests).

**Spec:** `docs/superpowers/specs/2026-09-26-task-scorecard-design.md`

## Части плана

Фаза 1 разрезана на пять файлов по границам подсистем; номера задач сквозные (1–11), интерфейсы между частями — в блоке **Interfaces** каждой задачи.

| Часть | Файл | Задачи |
|---|---|---|
| A | `2026-09-26-task-scorecard-phase1-a-collector.md` | 1–5 |
| B1 | `2026-09-26-task-scorecard-phase1-b1-score-config-reader.md` | 6 |
| B2 | `2026-09-26-task-scorecard-phase1-b2-score-metrics.md` | 7 |
| B3 | `2026-09-26-task-scorecard-phase1-b3-score-render-persist.md` | 8 |
| C | `2026-09-26-task-scorecard-phase1-c-wiring.md` | 9–11 |

Порядок: A и B1 независимы (B1 работает на рукописной фикстуре), дальше B2 → B3 → C. Каждая часть заканчивается зелёными тестами и своими коммитами.

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

## Review Focus

1. **Blind retry where the retried command is itself a "mutating" Bash pattern** (e.g. `sed -i` fails twice). Expected: counted as a blind retry, then state resets. → Task 7 test `TestBlindRetries_MutatingCommandStillCounts`.
2. **`swift test` / `cargo test` repeated after an error with only a build in between.** Expected: counted (build/test commands are **not** mutating). → Task 1 amends the spec; Task 7 test `TestBlindRetries_BuildBetweenDoesNotReset`.
3. **Child agent finished while its parent task-runner is still running (async runner).** Expected: child is deferred, not written as a thin meta-only row that later blocks the full row via dedup. → Task 4 test `test_child_of_running_parent_is_deferred`.
4. **A run whose root task-runner returned `blocked` with an `artifact:` that does not exist.** Expected: tier `Blocked`, P7 does not count `artifact_exists=false` for blocked. → Task 7 test `TestP7_BlockedDoesNotCountMissingArtifact`.
5. **Second `zprof score` on the same run with unchanged weights.** Expected: no duplicate row is *needed* by readers (readers take the latest per `(run_id, weights_hash)`), run-log section replaced in place, not appended twice. → Task 8 tests `TestWriteRunLogSection_Idempotent`, `TestReadScoredKeys`.

---
