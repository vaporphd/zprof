# docs/wiki/ — Documentation Plan

## Anchor

- Head SHA: `c10a5f0b34c9ab1f167e3b537164ae5efab8f903`
- Branch: `feat/issue-22-collector-config-hash`
- Triggering event: bootstrap (`docs/wiki/` did not exist)

## Structure

| Priority | Doc | Sources in project | Reader question |
|----------|-----|--------------------|------------------|
| P0 | `collector.md` | `profiles/base/zprof-collect.py` | "how does the telemetry collector work, and how is `config_hash`/`verdict` computed?" |
| P0 | `apply.md` | `cli/internal/apply/`, `cli/internal/cmd/apply.go` | "what does `zprof apply` write, and what does `--telemetry-only` do?" |
| P0 | `guard.md` | `profiles/base/zprof-guard.py`, `profiles/base/guard.yaml` | "how does the `PreToolUse` guard hook decide deny vs. silence, and which rules are actually live vs. data-only?" |
| P1 | `overlay.md` | `cli/internal/overlay/` | "how are the base profile and overlays loaded from disk?" |
| P1 | `manifest.md` | `cli/internal/manifest/` | "what's persisted in `.zprof.yaml`, and how do model/agent overrides carry over?" |
| P1 | `managed.md` | `cli/internal/managed/` | "how are managed blocks in `CLAUDE.md`/`AGENT_LOOP.md`/`workflows/*.md` merged without clobbering user edits?" |
| P1 | `score.md` | `cli/internal/score/` | "how is the per-task scorecard (`zprof score`) computed from `.agentlog/`?" |
| P1 | `profiles-base.md` | `profiles/base/agents/`, `profiles/base/workflows/`, `profiles/base/telemetry.yaml`, `profiles/base/manifest.yaml` | "what agents/workflows/schema does the base profile ship, independent of any overlay?" |
| P2 | `cmd.md` | `cli/internal/cmd/`, `cli/cmd/zprof/main.go` | "what subcommands does the `zprof` CLI expose?" |
| P2 | `stats.md` | `cli/internal/stats/` | "how does `zprof stats` aggregate telemetry across sessions (incl. `config_hash` drift grouping)?" |
| P2 | `eval.md` | `cli/internal/eval/` | "how does zprof parse Claude Code session transcripts into per-dispatch records?" |
| P2 | `agents.md` | `cli/internal/agents/` | "where is the canonical zprof agent roster defined?" |
| P2 | `models.md` | `cli/internal/models/` | "how are per-agent model overrides resolved?" |
| P2 | `detect.md` | `cli/internal/detect/` | "how does zprof detect a project's stack for `zprof init`?" |
| P2 | `doctor.md` | `cli/internal/doctor/` | "what does `zprof doctor` check?" |
| P2 | `wizard.md` | `cli/internal/wizard/` | "what does the `zprof init` wizard do?" |
| P2 | `sync.md` | `cli/internal/sync/` | "how does `zprof sync` update a git-hosted profile repo?" |
| P2 | `fsutil.md` | `cli/internal/fsutil/` | "what filesystem primitives (atomic write, backup) are shared across packages?" |
| P2 | `overlays.md` | `profiles/overlays/*` | "what stack-specific overlays exist and what does each contribute (agents, workflow extension, `.gitignore`)?" |
| P3 | `shakedown.md` | `profiles/base/shakedown/` | "what is the shakedown eval harness and how does `zprof shakedown` use it?" |

## Notes

- No `docs/PROJECT_SPEC.md` exists in this repo (checked: `test -f docs/PROJECT_SPEC.md` →
  missing) and there is no R-number/N-number spec convention in use — `docs/specs/` holds
  dated proposal docs instead. Wiki files should cross-reference `docs/adr/*` and
  `docs/specs/*` by filename, not by fabricated spec IDs.
- **Known drift (flagged, not fixed):** `README.md`'s "v1 overlays" list (`ios-swift,
  android-kotlin, backend-python, frontend-web / re-macho, systems-cpp, systems-rust`) does
  not match `profiles/overlays/` on disk (`backend-kotlin-jvm`, `backend-python`,
  `frontend-web`, `ios-swift`, `issue-loop-github-strict`, `kotlin-multiplatform`,
  `re-macho`, `systems-cpp`, `systems-rust`, `zcode-harness`). No `android-kotlin` directory
  exists; `issue-loop-github-strict`, `kotlin-multiplatform`, `zcode-harness`,
  `backend-kotlin-jvm` are undocumented in README. Per wiki-keeper scope this is not
  rewritten here — `overlays.md` (P2, not yet written) should describe the actual overlay
  set and note the README gap.
- This bootstrap run additionally wrote `collector.md` and `apply.md` (the two P0 docs
  whose sources changed on `feat/issue-22-collector-config-hash`) and `INDEX.md`, ahead of
  the P1/P2/P3 docs above, per wiki-keeper's own judgment call for a first run that lands
  alongside a feature branch touching those components.
- `guard.md` (P0, added retroactively to this table) was written on
  `feat/issue-23-zprof-guard-py` / #23 — same judgment call as above: `zprof-guard.py`
  and `guard.yaml` are new top-level sources in `profiles/base/`, so they get their own
  doc rather than waiting for the generic `profiles-base.md` (P1, not yet written).
  #23 is the first of a six-issue milestone (#23–#28); `guard.md` is `Status: in-progress`
  until #28 (`zprof apply` deployment) lands — see `guard.md` and `plan-2.md` for the
  issue breakdown.
- MAINTAIN run on `feat/guard-events-score-27` / #27 (ADR-0008) updated `guard.md`
  in place (new "Score and stats integration" section, `Dependants: [score, stats]`,
  two invariants) rather than writing `score.md`/`stats.md` — those stay P1/P2, not
  yet written. #27's source changes are entirely in `cli/internal/score/` and
  `cli/internal/cmd/{score,stats}.go`, consuming a file format
  (`.agentlog/guard-events.jsonl`) that `guard.md` already owns, not a new
  top-level component. `INDEX.md`'s dependency graph got the matching edge (data
  contract, not a Go import — same pattern already used for collector → apply).
- MAINTAIN run on `feat/guard-apply-deploy-28` / #28 (ADR-0009) closed the
  six-issue milestone: `guard.md` moved `Status: in-progress` → `implemented`,
  `Dependants` gained `apply`, and got a new "Deployment" section (the
  three-layer merge, `$ref` resolution, render, `enabled` gating, hooks,
  wiring — mirroring the pattern used for #27's "Score and stats integration").
  `apply.md` was updated in place (not a new doc) — `guard.go` lives inside the
  existing `cli/internal/apply` package, not a new top-level component: its
  `DeployTelemetry` invariant, "DeployTelemetry and `--telemetry-only`" section,
  and "Hooks" section all got surgical updates for the new `GuardLayers`
  parameter and `ensureGuardSettings`, plus a corrected claim (the guard
  renderer does *not* consume the merged `schema.json` the way `zprof score`
  does — it reads `Base.TelemetrySchema` directly). `INDEX.md`: `guard` row to
  `implemented`, the dependency graph's `guard`/`apply` nodes got the new
  artifact-deploy edge (same pattern as collector → apply), Status Summary
  `Implemented: 21` / `In progress: 0`.
- MAINTAIN run on `feat/doctor-guard-checks-29` / #29 updated `guard.md` in
  place (new "Doctor checks" section, `Dependants` gained `doctor`, one new
  invariant, Spec refs gained §10, Test coverage entry gained the 24-test
  doctor breakdown) rather than writing `doctor.md` — `doctor.md` stays P2,
  not yet written; #29's five checks are read-only diagnostics over guard's
  already-documented deployed artifacts (`.claude/guard.json`,
  `settings.local.json` hook entries, `guard.enabled`), not a new top-level
  component, same reasoning as #27/#28 above. `INDEX.md` got the matching
  data-contract edges (`guard`'s dependency-graph note gained the `doctor`
  clause; `doctor`'s own graph line gained a `(+ reads guard's ... — #29)`
  annotation) and a refreshed Head SHA — no Status Summary change since
  neither component's `Status`/count moved.
- MAINTAIN run on `fix/issue-64-doctor-telemetry-only` / #64 updated
  `guard.md` in place again (new "Telemetry-only diagnostics" section, one
  new Key invariant on `checkGuardDeployment`'s zero-value-manifest handling,
  Test coverage entry gained the two new `Diagnose()` telemetry-only tests)
  rather than writing `doctor.md` — same P2/"not yet written" status as #29's
  entry above. Unlike #29, this change is doctor's own top-level
  `Diagnose()` control flow (which checks run at all when `.zprof.yaml` is
  absent), not a guard-specific diagnostic — flagged explicitly in the new
  section rather than silently filed as if it were guard-only. It landed in
  `guard.md` anyway because `checkGuardDeployment`/`checkRoleResolution` are
  the two guard-related checks among the nine the telemetry-only branch
  runs (both already documented in this file's "Doctor checks" table per
  #29) and this file is already `zprof doctor`'s documented home per that
  precedent; the other seven checks it runs are collector/telemetry-general
  and were already running unchanged before #64 — no new claim needed in
  `collector.md`. `apply.md`'s "DeployTelemetry and
  `--telemetry-only`" section got a one-paragraph cross-reference (that
  section is what a reader following `--telemetry-only` finds first).
  `INDEX.md`: `doctor`'s own graph line gained a `(...; falls back to a
  manifest-independent check subset ... — #64, ADR-0001)` clause, refreshed
  Head SHA — no Status Summary change.
- MAINTAIN run on `fix/async-wait-p2-exempt-53` / #53 wrote `score.md` (P1 in
  the table above) for the first time — the component itself is not new
  (`cli/internal/score/` predates this issue), but #53 is its first
  guard-unrelated substantial change since bootstrap (every prior change to
  this package was folded into `guard.md`'s "Score and stats integration"
  per #27/ADR-0008, since it was only ever a `guard-events.jsonl` reader
  before now), and the new P2-exemption/`busy-poll`-signal behavior is
  exactly the kind of "how does scoring work and why doesn't it penalize
  this" question this wiki exists to answer without sending a reader to the
  Go source — same judgment call as the collector.md/apply.md bootstrap
  writes and guard.md's retroactive add for #23. `score.md` documents the
  full P1-P7 model (not just the #53 delta) since no prior version existed
  to update surgically. It does **not** document `task-runner.md`'s agent
  contract as its own wiki file — task-runner is one agent among many
  covered in bulk by `profiles-base.md` (still P1, not yet written); #53's
  task-runner.md change (the "Ожидание async-ребёнка" section) is quoted
  and cross-referenced from `score.md`'s new "P2 exemption and the
  busy-poll signal" section instead, since that section is what actually
  enforces/detects the contract and is where a reader asking "why doesn't
  P2 penalize `sleep`?" lands first — same precedent as guard.md folding in
  `task-runner.md`/`pr-shepherd.md` prompt-diff descriptions for #30 rather
  than waiting for `profiles-base.md`. No `apply.md` change: `p2_exempt_patterns`
  rides through the existing `renderSchema`/`DeployTelemetry` path unchanged
  (no new render code), the same way `mutating_bash_patterns` and
  `verdict_exempt_roles` already do without a mention in `apply.md`; it is
  documented in `score.md` instead, which is the actual consumer of the key.
  `INDEX.md`: `score` row got its `score.md` Doc link, dependency-graph
  annotation gained the `p2_exempt_patterns`/schema.json clause, moved out
  of "Undocumented", refreshed Head SHA — no Status Summary count change
  otherwise (score was already `implemented`).
- MAINTAIN run on `fix/worktree-checkout-hygiene-62` / #62 added a new
  "Checkout hygiene diagnostics" section to `guard.md` for
  `checkGitCheckoutHygiene` (the new `zprof doctor` check that warns on the
  worktree-leak pattern #62 fixed) rather than writing `doctor.md` — same
  "not yet written, still P2" call as #29/#64's doctor entries, but for a
  different reason than either: #64's telemetry-only branching at least had
  one genuinely guard-specific piece (`checkGuardDeployment`'s zero-value
  manifest) to hang the placement on; `checkGitCheckoutHygiene` has **none**
  — it never touches `guard.json`/hooks/deployment, it's pure git
  worktree/branch state tied to the pr-shepherd/task-runner contract change
  in the same issue. It landed in `guard.md` anyway purely on the
  established "this file is already `zprof doctor`'s documented home"
  precedent, stated explicitly in the new section rather than silently
  implied. Flagging for whoever eventually promotes `doctor.md` out of P2:
  at that point `guard.md`'s "Doctor checks" / "Telemetry-only diagnostics"
  / "Checkout hygiene diagnostics" sections should probably migrate there
  wholesale (guard.md is 900+ lines and covers a shrinking share of what
  `zprof doctor` actually checks — 3 of doctor's ~22 checks are
  guard-related, the rest, including #62's, aren't) — not attempted in this
  run, out of scope for a single-issue sync.
  This run also discovered and fixed **stale line-number citations**
  unrelated to #62's actual behavior change: `db7e720` inserted 158 lines
  into `diagnostics.go` ahead of `checkGuardHooks`/`checkGuardConfig`/
  `checkPermissionsDeny`/`checkGuardDeployment`/`checkRoleResolution`,
  silently invalidating every `diagnostics.go:<line>` citation in `guard.md`
  pointing at or after that insertion point (AI Context invariants, the
  "Doctor checks" table, "Telemetry-only diagnostics"). All were
  recomputed against the current file and corrected in place; the
  "Telemetry-only diagnostics" section's check count also got a genuine
  content fix (9 → 10: `checkGitCheckoutHygiene` runs in
  `diagnoseTelemetryOnly` too, per `db7e720`'s diff, and wasn't previously
  mentioned there since it didn't exist yet). Also fixed in passing: the
  "See also" section's `score` bullet still said "no wiki file yet"
  although `score.md` was written for #53 — unrelated to #62 but adjacent
  and trivially verifiable (`ls docs/wiki/score.md`).
  `INDEX.md`: `doctor`'s dependency-graph annotation gained a `#62` clause
  (git worktree/checkout hygiene, `.zprof/runs/*.md` `## Итог` as a data
  signal, not a Go import), refreshed Head SHA — no Status Summary change
  (`doctor` was already counted under "Undocumented", stays there; `guard`
  was already `implemented`, stays there).
- MAINTAIN run on `fix-60-north-star-gate-skip` / #60 added a new
  "North-star gate diagnostics" section to `guard.md` for
  `checkNorthStarGate` (the new `zprof doctor` check that flags a
  deployed/requested `north-star-auditor` gate with no `docs/NORTH_STAR.md`
  to check against) — same "not yet written, still P2" call as #29/#64/#62's
  doctor entries, for the same reason as #62's: `checkNorthStarGate` never
  touches `guard.json`/hooks/deployment, it's a completely separate
  mechanism (the `--with-gates` gate pipeline dispatched by
  `task-runner.md`, not the `PreToolUse`/`SubagentStop` `guard.yaml` rules
  engine). It landed in `guard.md` anyway purely on the established "this
  file is already `zprof doctor`'s documented home" precedent, stated
  explicitly in the new section (same as #62's). The section also covers
  the two prompt-layer fixes that shipped in the same commit — the gate
  contract's new rule 0 (`skip` verdict when the file is absent) and
  task-runner's pre-dispatch existence check — since the Go-side diagnostic
  only exists to catch the gap those two fixes intentionally leave silent
  by default, and `verdicts.yaml`'s new `skip` token. No `apply.md` change:
  nothing about `--with-gates` deployment or rendering changed, only the
  gate's own prompt contract and a doctor check. `INDEX.md`: `doctor`'s
  dependency-graph annotation gained a `#60` clause (north-star gate
  presence + `docs/NORTH_STAR.md` absence as a data signal, not a Go
  import), refreshed Head SHA — no Status Summary change (`doctor` stays
  under "Undocumented"; `guard` stays `implemented`). Gate verified green
  before this run: `go test ./internal/doctor/...` → 136 passed, 95.5%
  coverage.
