# Component Index

Generated: 2026-09-27 · Head: `c10a5f0b34c9ab1f167e3b537164ae5efab8f903`

## Components

| Component | Path | Status | Depends | Doc |
|-----------|------|--------|---------|-----|
| collector | `profiles/base/zprof-collect.py` | implemented | profiles-base | [collector.md](collector.md) |
| apply | `cli/internal/apply/`, `cli/internal/cmd/apply.go` | implemented | overlay, manifest, managed, models, agents, fsutil | [apply.md](apply.md) |
| overlay | `cli/internal/overlay/` | implemented | manifest | — |
| manifest | `cli/internal/manifest/` | implemented | fsutil, models | — |
| managed | `cli/internal/managed/` | implemented | — | — |
| score | `cli/internal/score/` | implemented | fsutil, manifest, stats | — |
| stats | `cli/internal/stats/` | implemented | — | — |
| eval | `cli/internal/eval/` | implemented | — | — |
| agents | `cli/internal/agents/` | implemented | — | — |
| models | `cli/internal/models/` | implemented | — | — |
| detect | `cli/internal/detect/` | implemented | manifest | — |
| doctor | `cli/internal/doctor/` | implemented | agents, managed, manifest, models, overlay | — |
| wizard | `cli/internal/wizard/` | implemented | apply, detect, managed, manifest, overlay | — |
| sync | `cli/internal/sync/` | implemented | — | — |
| fsutil | `cli/internal/fsutil/` | implemented | — | — |
| cmd | `cli/internal/cmd/`, `cli/cmd/zprof/main.go` | implemented | apply, doctor, eval, managed, manifest, models, overlay, score, stats, sync, wizard | — |
| profiles-base | `profiles/base/agents/`, `profiles/base/workflows/`, `profiles/base/telemetry.yaml`, `profiles/base/manifest.yaml` | implemented | — | — |
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
└── overlays → [profiles-base]

fsutil
├── manifest → [fsutil, models]
│   ├── overlay → [manifest]
│   ├── detect → [manifest]
│   └── doctor → [agents, managed, manifest, models, overlay]
├── score → [fsutil, manifest, stats]
└── apply → [overlay, manifest, managed, models, agents, fsutil]  (deploys collector as an artifact, not a Go import)
    └── wizard → [apply, detect, managed, manifest, overlay]
        └── cmd → [apply, doctor, eval, managed, manifest, models, overlay, score, stats, sync, wizard]
```

## Status Summary

- Implemented: 19
- In progress: 0
- Planned: 0
- Undocumented (has code, no wiki): 17 (overlay, manifest, managed, score, stats, eval,
  agents, models, detect, doctor, wizard, sync, fsutil, cmd, profiles-base, overlays,
  shakedown — see `PLAN.md` for the P1/P2/P3 write order)

## Known drift (flagged, not fixed)

`README.md`'s "v1 overlays" list does not match `profiles/overlays/` on disk. See
`PLAN.md` § Notes.
