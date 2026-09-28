# Component Index

Generated: 2026-09-28 · Head: `b3b00abd8891a504af8afdc6966f62f0b4b43bb4`

## Components

| Component | Path | Status | Depends | Doc |
|-----------|------|--------|---------|-----|
| collector | `profiles/base/zprof-collect.py` | implemented | profiles-base | [collector.md](collector.md) |
| guard | `profiles/base/zprof-guard.py`, `profiles/base/guard.yaml` | in-progress | profiles-base | [guard.md](guard.md) |
| apply | `cli/internal/apply/`, `cli/internal/cmd/apply.go` | implemented | overlay, manifest, managed, models, agents, fsutil, verdicts | [apply.md](apply.md) |
| overlay | `cli/internal/overlay/` | implemented | manifest | — |
| manifest | `cli/internal/manifest/` | implemented | fsutil, models | — |
| managed | `cli/internal/managed/` | implemented | — | — |
| score | `cli/internal/score/` | implemented | fsutil, manifest, stats | — |
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
├── guard → [profiles-base]   (not yet an apply dependant — deploy is #28;
│                              score/stats read its .agentlog/guard-events.jsonl
│                              output as a data contract, not a Go import — #27, ADR-0008)
└── overlays → [profiles-base]

fsutil
├── manifest → [fsutil, models]
│   ├── overlay → [manifest]
│   ├── detect → [manifest]
│   └── doctor → [agents, managed, manifest, models, overlay, verdicts]
├── score → [fsutil, manifest, stats]  (+ reads collector's dispatches.jsonl/
│                                        tool-events.jsonl and guard's
│                                        guard-events.jsonl as data, not Go imports)
└── apply → [overlay, manifest, managed, models, agents, fsutil, verdicts]  (deploys collector as an artifact, not a Go import)
    └── wizard → [apply, detect, managed, manifest, overlay]
        └── cmd → [apply, doctor, eval, managed, manifest, models, overlay, score, stats, sync, wizard]
            (cmd/stats.go also reads guard-events.jsonl directly via internal/score — #27)

verdicts  (leaf; loads/validates profiles/base/verdicts.yaml — ADR-0003)
├── doctor → [..., verdicts]   (checkAgentVerdicts diagnostic)
└── apply → [..., verdicts]    (renderSchema merges the registry into schema.json)
```

## Status Summary

- Implemented: 20
- In progress: 1 (guard — #23 lands the frame + stop-list/read-only rules; #24
  lands the four §5.2/§5.3 context evaluators (`head_on_remote`,
  `linked_worktree`, `write_outside_repo`, `branch_pr_merged`) and the
  `context_error` journal event; #25 (ADR-0006) lands `merge_preflight`/
  `pr_create_gate` and the `allow_unverified` journal event — every
  `guard.yaml` rule is now active; #26 (ADR-0007) lands the `subagent-stop`
  mode — a `return_format` validator on `SubagentStop`, independent of
  `guard.yaml`/`CONTEXTS` — plus the `subagent-stop`/`format_unfixed` journal
  events; #27 (ADR-0008) lands the Go-side read of `guard-events.jsonl` in
  `zprof score` (P7 counts guard `deny`/`block` rows, card shows `(guard: N
  deny)`) and `zprof stats` (`guard: top rules` stderr line) — the guard
  script itself is unchanged; #28 remains: `zprof apply` deployment of the
  hook into `.claude/`, still blocking both guard modes — and therefore #27's
  readers — from seeing any data in a real project)
- Planned: 0
- Undocumented (has code, no wiki): 18 (overlay, manifest, managed, score, stats, eval,
  agents, models, detect, doctor, verdicts, wizard, sync, fsutil, cmd, profiles-base,
  overlays, shakedown — see `PLAN.md` for the P1/P2/P3 write order; `verdicts`
  (`cli/internal/verdicts/`) is new on `feat/verdicts-registry` / #20, ADR-0003)

## Known drift (flagged, not fixed)

`README.md`'s "v1 overlays" list does not match `profiles/overlays/` on disk. See
`PLAN.md` § Notes.
