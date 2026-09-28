## AI Context

Component: score
Path: `cli/internal/score/`
Status: implemented
Depends: [fsutil, manifest, stats]
Dependants: [cmd]
Exports: [Compute, Card, Signal, Penalty, Config, Defaults, LoadConfig, IsP2Exempt,
  IsMutatingBash, WeightsHash, RenderCard, BuildRuns, AttachGuardEvents, ReadToolEvents,
  ReadGuardEvents, LatestRun, FindRun, AppendScore, ReadScoredKeys, WriteRunLogSection,
  ScoreKey, TierFor, ShortModel]
Key invariants:
  - `Compute` scores exactly seven fixed penalties `P1..P7` (`PenaltyIDs`,
    `config.go:22`) in order, each `weight × min(1, value/saturation)`, points split
    across roles proportional to each role's numerator share
    (`penaltyFrom`, `metrics.go:34-57`); `Score = 100 − Σ points`, clamped `[0,100]`
    (`scoring.go:120-140`).
  - `Card.Signals` (issue #53, AC5) holds informational findings that are **not**
    penalties: no weight/saturation, never folded into `Score` or any role's
    `Penalty` column (`Signal`, `scoring.go:56-66`; `Compute` sets it independently
    of the P1-P7 loop, `scoring.go:155-157`). Currently the only signal is
    `busy-poll`.
  - P2 (blind retries) and the `busy-poll` signal both treat `sleep`/`wait` Bash
    commands as exempt via the same `Config.IsP2Exempt` predicate
    (`config.go:128-144`) — a `sleep 180` timing out on the Bash tool's own 120s
    default is async-child wait hygiene (task-runner.md "Ожидание async-ребёнка"),
    not a blind retry of a failed command and not itself part of a busy-poll streak.
    The target is matched **after** stripping one leading `rtk proxy `/`rtk `
    wrapper prefix (`stripRtkPrefix`, `config.go:71-80`), so `rtk proxy sleep 180`
    exempts identically to `sleep 180`.
  - `P2ExemptPatterns` is folded into `WeightsHash` (`config.go:247-257`) alongside
    weights/saturation/thresholds — a `scores.jsonl` row scored under a different
    exempt-pattern set is never treated as comparable to one scored under the
    current set (`ScoreKey` is `RunID + "|" + WeightsHash`, `persist.go:21`).
  - `LoadConfig` layers three sources in order — compiled Go defaults ← this
    project's `.agentlog/schema.json` (`telemetry.yaml` deployed as JSON) ←
    `.zprof.yaml`'s `score:` block — and a present-but-empty list in `schema.json`
    **replaces**, not extends, the default (`config.go:161-217`; verified by
    `TestLoadConfig_P2ExemptPatternsFromSchema`, `config_test.go`).
  - `computeBusyPoll` scores the 4th-and-later consecutive non-mutating `Bash` call
    in an unbroken streak (`busyPollThreshold = 3`, `metrics.go:305`) — a `sleep`/
    `wait` (`IsP2Exempt`) or any mutating event (`isMutating`) resets the streak to
    zero; a non-Bash, non-mutating call (`Read`, `Grep`, …) neither extends nor
    breaks it. `isMutating` is checked **before** the tool==`"Bash"` check so an
    `Edit`/`Write` between two Bash status checks always resets the streak even
    though it is not itself a Bash event (`metrics.go:328-337`, comment documents
    the bug this ordering fixes against `testdata/run1`).
  - P2's `detail` string counts occurrences of the specific top-repeated target,
    not the metric's run-wide total — with several distinct repeated targets the
    two diverge (`metrics.go:143-152`, fixed alongside the #53 exemption).
  - `Compute` never reads raw Claude Code session logs, only what the Python
    collector already normalized into `.agentlog/dispatches.jsonl` /
    `tool-events.jsonl` (package doc comment, `config.go:1-4`).
Spec refs: docs/superpowers/specs/2026-09-26-task-scorecard-design.md §6 (P1-P7
  model), §7 (card format), §8 (commands/config); docs/adr/0008-guard-events-score-integration.md
  (P7 guard-events.jsonl integration, documented in full in guard.md's
  "Score and stats integration")
Test coverage: `go test ./cli/internal/score/...` — 70 tests passed (verified
  2026-09-29, branch `fix/async-wait-p2-exempt-53`). Issue #53 additions:
  `config_test.go` (`TestDefaults_P2ExemptMatchesSleepAndWaitOnly`,
  `TestIsP2Exempt_RtkPrefixStripped`, `TestLoadConfig_P2ExemptPatternsFromSchema`,
  `TestWeightsHash_SensitiveToP2ExemptPatterns`), `metrics_test.go`
  (`TestBlindRetries_SleepExemptDoesNotCount`,
  `TestBlindRetries_NonExemptRepeatStillCountsAlongsideSleep`,
  `TestBlindRetries_RtkWrappedSleepIsExempt`,
  `TestBlindRetries_RtkWrappedNonExemptRepeatStillCounts`,
  `TestBusyPoll_TriggersOnConsecutiveNonMutatingBash`,
  `TestBusyPoll_AtOrBelowThresholdIsSilent`,
  `TestBusyPoll_SleepBetweenChecksResetsStreak`,
  `TestBusyPoll_EditBetweenChecksResetsStreak`), `render_test.go`
  (`TestRenderCard_BusyPollSignalLine`), `scoring_test.go`
  (`TestCompute_BusyPollSignalPopulatesCard`, `TestCompute_NoBusyPollLeavesSignalsEmpty`).
  `profiles/base/telemetry_test.py::test_schema` additionally asserts
  `p2_exempt_patterns` compiles and matches `sleep`/`wait` only (not
  `sleepy-time.sh`).

---

## Score — Per-Task Scorecard

`cli/internal/score` computes `zprof score`'s deterministic 0-100 scorecard for one
task-runner run, from `.agentlog/dispatches.jsonl`, `tool-events.jsonl`, and
`guard-events.jsonl` alone — no LLM, no re-reading of Claude Code session
transcripts (`cli/internal/cmd/score.go`, wired via `NewScoreCmd`). It is also read
by `zprof stats` for cross-run guard-rule aggregation (`cmd/stats.go:91,114`).

### The seven penalties

`Compute` (`scoring.go:120`) runs `computeP1..computeP7` (`metrics.go`) in fixed
order and turns each raw `metric` into a `Penalty` via `penaltyFrom`:
`points = weight × min(1, value/saturation)`, split across roles by each role's
share of the metric's numerator (`ByRole`). `Score = round(100 − Σ points)`,
clamped to `[0, 100]`.

| ID | What it measures | Default weight/saturation |
|----|-------------------|---------------------------|
| P1 | Tool error rate (`is_error=true` / all resolved events) | 20 / 0.20 |
| P2 | Blind retries: same `(tool, input_hash)` repeated after an error with no mutating event in between | 15 / 3 |
| P3 | Re-reads: `Read` of a path already read this dispatch, ≥4 total Reads (noise floor) | 10 / 0.5 |
| P4 | `tester: failed` immediately followed by an `implementer` step | 20 / 2 |
| P5 | Reviewer verdicts in `review_block_verdicts` (`block`, `changes-requested`, …) | 10 / 2 |
| P6 | Share of run tokens spent in dispatches with `status: failed/killed` or an unparsable return | 15 / 0.30 |
| P7 | Class-A contract violations (missing preamble/artifact/next-step/unparsed return) plus `guard-events.jsonl` `deny`/`block` rows (ADR-0008 — see [Guard](guard.md)'s "Score and stats integration") | 10 / 4 |

`Card.Penalties` always has all seven entries (zero-value ones included);
`RenderCard` (`render.go:20-42`) only prints the top three by points, plus any
penalty carrying `GuardDenies > 0` even if it didn't make the top three (ADR-0008
H5).

### P2 exemption and the busy-poll signal (issue #53)

Task-runner's contract for waiting on an async-dispatched child (`Agent` tool)
requires it to keep making tool calls in the same turn until the child returns —
never end the turn on bare prose — but to do so **without** busy-polling
(`profiles/base/agents/task-runner.md` §"Ожидание async-ребёнка",
`task-runner.md:178-201`, identical in `.claude/agents/task-runner.md`):

- A rare `sleep 120-180` between status checks (~one call per 2-3 minutes), never
  more often.
- An explicit Bash `timeout` parameter **larger** than the sleep duration — the
  Bash tool's own default timeout is 120000ms, so `sleep 180` without a `timeout:
  200000` (or similar) override fails on the tool's own clock, not on the child.
- One cheap fact per status check (e.g. `git log -1 --oneline && git status
  --porcelain` as a single Bash call), not a series of separate read-only
  commands.
- No busy-poll: frequent read-only checks (`date`, `git log`, `git diff`) used to
  "pass the time" without a `sleep` between them are forbidden.

`cli/internal/score` enforces the first half of this contract (don't penalize the
sanctioned wait) and detects violations of the second half (busy-poll), in two
mechanisms that share one predicate:

**`Config.IsP2Exempt(tool, target)`** (`config.go:128-144`) — true only for `Bash`
targets matching `P2Exempt` (default `^\s*(sleep|wait)\b`, `defaultP2Exempt`,
`config.go:63-65`; mirrored in `profiles/base/telemetry.yaml`'s
`p2_exempt_patterns` key, `telemetry.yaml:96-104`) after stripping one leading
`rtk proxy `/`rtk ` prefix (`rtkPrefixes`, `stripRtkPrefix`, `config.go:67-80` —
mirrors `zprof-guard.py`'s `_RTK_PREFIXES`, ADR D7 §4, so `rtk`-wrapped commands
match identically to their unwrapped form).

- `computeP2` (`metrics.go:121-154`) skips an exempt event entirely, in both
  directions: a timed-out `sleep 180` neither counts as a repeat of a prior
  `sleep 180` nor primes `lastErr` for a later, unrelated command sharing its
  `(tool, input_hash)` key. A real blind retry interleaved with sleeps (e.g.
  `swift test` failing, a `sleep`, then `swift test` again) still counts —
  skipping the sleep does not reset or otherwise disturb the unrelated command's
  own key state.
- `LoadConfig` (`config.go:161-217`) reads `p2_exempt_patterns` from this
  project's deployed `.agentlog/schema.json` if present and non-empty
  (**replacing**, not extending, the compiled default — same semantics as
  `mutating_bash_patterns`), then folds `P2ExemptPatterns` into `WeightsHash`
  (`config.go:247-257`): a change to the exempt set is a scoring-semantics
  change, not just a weight/saturation tweak, so historical `scores.jsonl` rows
  scored under a different exempt set get a disjoint `WeightsHash` and are never
  silently compared against rows scored under the new one.

**`computeBusyPoll`** (`metrics.go:320-362`) is a new *signal*, not an eighth
penalty — it has no weight or saturation and never touches `Score` (`Compute`
only appends it to `Card.Signals` when `value > 0`, `scoring.go:155-157`). It
walks each dispatch's tool events and counts a streak of consecutive
non-mutating `Bash` calls; the streak resets to zero on any mutating event
(`isMutating`, checked first so an `Edit`/`Write` between two Bash checks always
breaks the streak even though it isn't a Bash event itself) or on an
`IsP2Exempt` sleep/wait. The 4th call onward in an unbroken streak
(`busyPollThreshold = 3`) increments the signal — the task-runner contract
already tolerates a human occasionally splitting one combined status check into
2-3 quick Bash calls; only a longer unbroken run of them is a real busy-poll.
`RenderCard` prints signals after the scored findings, each on its own line
prefixed `· ` instead of `−N` (`render.go:44-48`):

```
· busy-poll implementer: 4 Bash подряд без sleep/правок между (посл. `git status --porcelain`)
```

This signal fired for real on the `pr-shepherd` role in run `#25`'s
`.agentlog` (25× `gh pr view` issued back-to-back with no `sleep`/mutation
between them) — the run this feature was built to catch went from score 47 to
62 and P2 15→0 once the sleep/wait exemption alone was applied, with the
busy-poll line then surfacing the unrelated pr-shepherd pattern the exemption
doesn't touch.

### Config loading and WeightsHash

`Defaults()` (`config.go:82-106`) compiles the Go-side defaults, which must be
kept in sync by hand with `profiles/base/telemetry.yaml`'s `score_defaults` /
`mutating_bash_patterns` / `p2_exempt_patterns` / `verdict_exempt_roles` /
`review_block_verdicts` keys (comments on each `var`/`func` say so explicitly —
there is no single source of truth shared between the Go binary and the deployed
project's schema at build time). `LoadConfig(projectDir, agentlogDir)`
(`config.go:161-217`) then layers, in order:

1. Compiled defaults.
2. `<agentlogDir>/schema.json` (this project's deployed `telemetry.yaml`, written
   by `apply.DeployTelemetry` — see [Apply](apply.md)) — a present, non-empty list
   field **replaces** the corresponding default list wholesale.
3. `<projectDir>/.zprof.yaml`'s `score:` block (`manifest.LoadProject`) — only
   known weight/saturation/threshold keys are merged (`mergeFloats`,
   `mergeThresholds`), and `score.enabled` can turn scoring off entirely.

`Config.WeightsHash()` (`config.go:247-257`) SHA1-hashes the canonical JSON
encoding of `{weights, saturation, thresholds, p2_exempt_patterns}` (first 12 hex
chars) and is stored on every `Card` (`scores.jsonl` row) plus folded into
`ScoreKey` (`persist.go:21`) — `zprof score`'s own re-run/append logic and
`zprof stats`'s cross-run aggregation both use this to avoid comparing scores
computed under incompatible rules.

### Runs, dispatches and persistence

`BuildRuns` (`reader.go:159`) groups the collector's flat `dispatches.jsonl` +
`tool-events.jsonl` into one `Run` per root task-runner dispatch (a run's `Steps`
give P4/P5 their role sequence, its `Events` map keyed by `DispatchID` feeds
P1-P3 and the busy-poll signal). `AttachGuardEvents` (`reader.go:247`) merges in
`guard-events.jsonl` rows for P7's guard-deny counting (ADR-0008). `AppendScore`
(`persist.go:25`) writes one JSON line to `.agentlog/scores.jsonl`, guarded by
`ReadScoredKeys`/`ScoreKey` against re-scoring the same `(RunID, WeightsHash)`
pair twice; `WriteRunLogSection` (`persist.go:75`) inserts the rendered card
under a `## Score` heading in the run's own `.zprof/runs/<id>.md` log.

### See also

- [Task-runner "Ожидание async-ребёнка" contract](../../profiles/base/agents/task-runner.md)
  — the source contract `IsP2Exempt`/`computeBusyPoll` enforce/detect; no
  dedicated wiki file yet (task-runner is one agent among many documented in
  bulk by `profiles-base.md`, P1 in `PLAN.md`, not yet written) — this file's
  "P2 exemption and the busy-poll signal" section above is its most complete
  written description to date.
- [Guard](guard.md) — "Score and stats integration" section: `guard-events.jsonl`
  format and P7's guard-deny counting (ADR-0008); [Apply](apply.md) —
  `DeployTelemetry` writes `.agentlog/schema.json` from `telemetry.yaml`, the
  source `LoadConfig` reads `p2_exempt_patterns`/`mutating_bash_patterns` etc.
  from.
- `stats` (`cli/internal/stats/`, doc not yet written — see `PLAN.md`) — `zprof
  stats` reads `score.GuardEvent`/`score.ReadGuardEvents` for its own `guard: top
  rules` aggregation (`cmd/stats.go:91,114`).
- [ADR-0008: guard-события в `zprof score` (P7) и `zprof stats`](../adr/0008-guard-events-score-integration.md)
- `docs/superpowers/specs/2026-09-26-task-scorecard-design.md` — original design
  (§6 penalty model, §7 card format, §8 commands/config); does not cover the
  `Signals`/busy-poll addition (#53), which post-dates it.
