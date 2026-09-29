# Component Index

Generated: 2026-09-29 · Head: `3b7d10e0a6a8553c57746d0fb77a0073ff908208`

## Components

| Component | Path | Status | Depends | Doc |
|-----------|------|--------|---------|-----|
| collector | `profiles/base/zprof-collect.py` | implemented | profiles-base | [collector.md](collector.md) |
| guard | `profiles/base/zprof-guard.py`, `profiles/base/guard.yaml` | implemented | profiles-base | [guard.md](guard.md) |
| apply | `cli/internal/apply/`, `cli/internal/cmd/apply.go` | implemented | overlay, manifest, managed, models, agents, fsutil, verdicts | [apply.md](apply.md) |
| overlay | `cli/internal/overlay/` | implemented | manifest | — |
| manifest | `cli/internal/manifest/` | implemented | fsutil, models | — |
| managed | `cli/internal/managed/` | implemented | — | — |
| score | `cli/internal/score/` | implemented | fsutil, manifest, stats | [score.md](score.md) |
| stats | `cli/internal/stats/` | implemented | — | — |
| eval | `cli/internal/eval/` | implemented | — | — |
| agents | `cli/internal/agents/` | implemented | — | — |
| models | `cli/internal/models/` | implemented | — | — |
| detect | `cli/internal/detect/` | implemented | manifest | — |
| doctor | `cli/internal/doctor/` | implemented | agents, managed, manifest, models, overlay, verdicts | — |
| verdicts | `cli/internal/verdicts/` | implemented | — | — |
| wizard | `cli/internal/wizard/` | implemented | apply, detect, managed, manifest, overlay | — |
| sync | `cli/internal/sync/` | implemented | — | — |
| fsutil | `cli/internal/fsutil/` | implemented | — | — |
| cmd | `cli/internal/cmd/`, `cli/cmd/zprof/main.go` | implemented | apply, doctor, eval, managed, manifest, models, overlay, score, stats, sync, wizard | — |
| profiles-base | `profiles/base/agents/`, `profiles/base/workflows/`, `profiles/base/telemetry.yaml`, `profiles/base/manifest.yaml`, `profiles/base/verdicts.yaml` | implemented | — | — |
| overlays | `profiles/overlays/*` (backend-kotlin-jvm, backend-python, frontend-web, ios-swift, issue-loop-github-strict, kotlin-multiplatform, re-macho, systems-cpp, systems-rust, zcode-harness) | implemented | profiles-base | — |
| shakedown | `profiles/base/shakedown/` | implemented | — | — |

Depends columns for rows without a Doc link are read directly off Go imports
(`grep '"github.com/vaporphd/zprof/internal/' cli/internal/<pkg>/*.go`, excluding
`_test.go`), verified 2026-09-27, but have not had a per-component wiki file (AI Context)
written yet; treat them as provisional until `PLAN.md`'s P1/P2/P3 docs land.

## Dependency Graph

```
profiles-base
├── collector → [profiles-base]
├── guard → [profiles-base]   (apply dependant since #28 — deployGuard reads
│                              GuardScript/GuardSchema as artifacts, not a Go
│                              import, ADR-0009; score/stats read its
│                              .agentlog/guard-events.jsonl output as a data
│                              contract, not a Go import — #27, ADR-0008;
│                              doctor reads its deployed .claude/guard.json +
│                              settings.local.json hook entries as data, not a
│                              Go import — #29, design §10)
└── overlays → [profiles-base]

fsutil
├── manifest → [fsutil, models]
│   ├── overlay → [manifest]
│   ├── detect → [manifest]
│   └── doctor → [agents, managed, manifest, models, overlay, verdicts]
│       (+ reads guard's deployed .claude/guard.json/settings.local.json as
│        data, not a Go import — #29; falls back to a manifest-independent
│        check subset when .zprof.yaml is absent but telemetry/guard is
│        deployed on disk, instead of erroring — #64, ADR-0001; also warns
│        on git checkout/worktree hygiene via `git worktree list`
│        --porcelain, reading task-runner's `.zprof/runs/*.md` `## Итог`
│        marker as a heuristic data signal for "run still in flight", not a
│        Go import — #62, unrelated to guard, see guard.md's "Checkout
│        hygiene diagnostics"; also reads the north-star-auditor gate's
│        deployed presence + docs/NORTH_STAR.md's absence as a data signal,
│        not a Go import — #60, unrelated to guard.yaml/zprof-guard.py, see
│        guard.md's "North-star gate diagnostics")
├── score → [fsutil, manifest, stats]  (+ reads collector's dispatches.jsonl/
│                                        tool-events.jsonl and guard's
│                                        guard-events.jsonl as data, not Go imports;
│                                        + reads profiles-base's telemetry.yaml
│                                        (deployed as .agentlog/schema.json) for
│                                        mutating_bash_patterns/p2_exempt_patterns/
│                                        score_defaults, also data not a Go
│                                        import — #53, see score.md)
└── apply → [overlay, manifest, managed, models, agents, fsutil, verdicts]  (deploys collector + guard as artifacts, not Go imports — #28, ADR-0009)
    └── wizard → [apply, detect, managed, manifest, overlay]
        └── cmd → [apply, doctor, eval, managed, manifest, models, overlay, score, stats, sync, wizard]
            (cmd/stats.go also reads guard-events.jsonl directly via internal/score — #27)

verdicts  (leaf; loads/validates profiles/base/verdicts.yaml — ADR-0003)
├── doctor → [..., verdicts]   (checkAgentVerdicts diagnostic)
└── apply → [..., verdicts]    (renderSchema merges the registry into schema.json)
```

## Status Summary

- Implemented: 21 (as of `feat/guard-apply-deploy-28` / #28, ADR-0009: `guard`
  moved from in-progress to implemented — #23 landed the frame +
  stop-list/read-only rules; #24 the four §5.2/§5.3 context evaluators
  (`head_on_remote`, `linked_worktree`, `write_outside_repo`,
  `branch_pr_merged`) and the `context_error` journal event; #25 (ADR-0006)
  `merge_preflight`/`pr_create_gate` and the `allow_unverified` journal
  event — every `guard.yaml` rule active; #26 (ADR-0007) the `subagent-stop`
  mode — a `return_format` validator on `SubagentStop`, independent of
  `guard.yaml`/`CONTEXTS` — plus the `subagent-stop`/`format_unfixed` journal
  events; #27 (ADR-0008) the Go-side read of `guard-events.jsonl` in
  `zprof score` (P7 counts guard `deny`/`block` rows, card shows `(guard: N
  deny)`) and `zprof stats` (`guard: top rules` stderr line); #28 (ADR-0009)
  `zprof apply`/`zprof sync`/`--telemetry-only` deploying `.claude/zprof-guard.py`,
  rendering `.claude/guard.json` from the three-layer merge, and upserting the
  `PreToolUse`/`SubagentStop` hooks + `permissions.deny` — the six-issue
  milestone #23–#28 is now complete and a real project's `zprof apply` writes
  `.agentlog/guard-events.jsonl`, so #27's readers have something to read)
- In progress: 0
- Planned: 0
- Undocumented (has code, no wiki): 17 (overlay, manifest, managed, stats, eval,
  agents, models, detect, doctor, verdicts, wizard, sync, fsutil, cmd, profiles-base,
  overlays, shakedown — see `PLAN.md` for the P1/P2/P3 write order; `verdicts`
  (`cli/internal/verdicts/`) is new on `feat/verdicts-registry` / #20, ADR-0003;
  `score` moved out of this list on `fix/async-wait-p2-exempt-53` / #53 —
  `score.md` written, see `PLAN.md` § Notes)

## Known drift (flagged, not fixed)

`README.md`'s "v1 overlays" list does not match `profiles/overlays/` on disk. See
`PLAN.md` § Notes.
