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
