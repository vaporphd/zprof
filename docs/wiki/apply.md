## AI Context

Component: apply
Path: `cli/internal/apply/` (CLI wiring: `cli/internal/cmd/apply.go`)
Status: implemented
Depends: [overlay, manifest, managed, models, agents, fsutil, verdicts]
Dependants: [cmd]
Exports: [Apply, ApplyOpts, ApplyResult, DeployTelemetry, EnsureHooks, WriteAgent, FormatRemovedAgents]
Key invariants:
  - `Apply` requires `Base != nil` and at least one overlay
    (`cli/internal/apply/engine.go:58-63`) — a base-only apply is intentionally not
    supported (would rewrite the `consilium`/`executing` blocks in `CLAUDE.md` and drop
    zprof's own manually-written executor agents; see ADR-0001 "Alternatives considered").
  - `DeployTelemetry` writes only `.claude/zprof-collect.py`, `.agentlog/schema.json`, and
    the telemetry hook entries in `.claude/settings.local.json` — no agents, no
    `.zprof.yaml`, no `CLAUDE.md`/`AGENT_LOOP.md` (`cli/internal/apply/collector.go:59-79`).
  - `deployCollector` unconditionally overwrites the collector script and schema on every
    call — they are generated artifacts owned by the base profile, not user-editable state
    files (`collector.go:14-19`).
  - `renderSchema` merges `Base.Verdicts` (`verdicts.yaml`, parsed and validated via
    `internal/verdicts`) into `.agentlog/schema.json` under a top-level `verdicts` key,
    normalized (universal tokens folded into each role) so a schema.json reader never
    needs the registry's anchors/`templates`/`quotes`. `telemetry.yaml` defining its own
    `verdicts` key, or a registry that fails `Validate()`, fails the apply outright
    (fail-closed) rather than deploying a stale or ambiguous contract
    (`collector.go:82-134`; ADR-0003 §D4).
  - `zprof apply --telemetry-only` rejects overlay arguments (mutually exclusive with the
    flag) and shares the same `DeployTelemetry` call as the full `Apply` path
    (`cli/internal/cmd/apply.go:27-35, 43-64`).
  - `EnsureHooks` upserts the Stop/SubagentStop/SessionStart hook entries idempotently by
    locating the existing `zprof-collect.py` invocation and replacing it in place,
    preserving unrelated `settings.local.json` keys such as `permissions`
    (`cli/internal/apply/settings.go:31-49, 79-105`).
  - Deleting an agent file is the only destructive step `Apply` performs on its own; it is
    always reported by name with a `.bak` left alongside
    (`PruneOrphanAgents` / `FormatRemovedAgents`, `engine.go:39-52, 104-108`).
Spec refs: docs/adr/0001-collector-config-hash-and-telemetry-redeploy.md,
  docs/adr/0003-verdicts-registry.md
Test coverage: `go test ./cli/internal/apply/... ./cli/internal/cmd/...` — 69 tests passed
  (verified 2026-09-27, branch `feat/verdicts-registry`), including `collector_test.go`'s
  `TestDeployTelemetry`, `TestDeployTelemetryIdempotent`, and the six added for the
  `verdicts.yaml` merge (`TestRenderSchema_MergesVerdictsUnderTopLevelKey`,
  `TestRenderSchema_WithoutVerdictsYAMLMatchesPriorOutputByteForByte`,
  `TestRenderSchema_TelemetryDefiningVerdictsKeyIsAnError`,
  `TestRenderSchema_InvalidVerdictsYAMLIsAnError`,
  `TestDeployTelemetry_SchemaJSONHasVerdictsKeyWhenBaseProvidesRegistry`,
  `TestDeployTelemetry_NoVerdictsYAMLMeansNoVerdictsKey`), plus `cmd/apply_test.go`
  (`TestApplyTelemetryOnlyArgs`, `TestApplyTelemetryOnlyDeploysWithoutOverlay`,
  `TestApplyTelemetryOnlyDryRunWritesNothing`) and the pre-existing `engine_test.go`,
  `e2e_test.go`, `settings_test.go`, `prune_test.go`, `agent_write_test.go`,
  `tables_test.go`, `state_files_test.go` for the full `Apply` path.

---

## Apply — Profile Application Engine

`cli/internal/apply` is what `zprof apply <overlay>...` and `zprof init` call to actually
write a project's `.claude/agents/`, managed `CLAUDE.md`/`AGENT_LOOP.md`/`workflows/*.md`,
state files, telemetry, `.gitignore`, and `.zprof.yaml`. `Apply(ApplyOpts) (*ApplyResult,
error)` (`engine.go:57`) orchestrates seven steps in order (`engine.go:57-169`):

1. Write base agents (skip `gates/*` unless `WithGates`), resolving per-agent model
   overrides.
2. Write overlay agents, namespaced (`overlay-agentname.md`) when more than one overlay is
   applied.
3. Prune agents a previous apply wrote that are no longer in this roster
   (`PruneOrphanAgents`), backing each one up.
4. Render `AGENT_LOOP.md` (thin router block).
5. Render `workflows/<name>.md` (base workflow + overlay extension, composed per
   `LoopTemplate`).
6. Render `CLAUDE.md` (doctrine + per-overlay stack config + consilium/executing/stop-list
   tables) via the managed-block merge in `internal/managed` (see `managed.md`, planned).
7. Ensure state files (`followup.md`, `lessons.md`, `todo.md`, …), deploy telemetry
   (below), append `.gitignore` entries, and persist `.zprof.yaml`.

### DeployTelemetry and `--telemetry-only`

Steps 5.5/5.6 of `Apply` — writing the collector script + schema and upserting hooks — are
factored into a standalone function:

```go
func DeployTelemetry(projectDir string, base *overlay.Base) ([]string, error)
```

(`collector.go:59-79`). It calls `deployCollector` (writes
`.claude/zprof-collect.py` from `base.CollectorScript` and `.agentlog/schema.json` from
`base.TelemetrySchema`, converted YAML→JSON) and then `EnsureHooks` (upserts the telemetry
hook entries). `Apply` itself now calls `DeployTelemetry` rather than duplicating the two
steps (`engine.go:147-156`).

Since ADR-0003 (#20), `deployCollector`'s schema step also folds `base.Verdicts`
(`profiles/base/verdicts.yaml`) into the rendered `schema.json` under a top-level
`verdicts` key (`renderSchema` / `mergeVerdicts`, `collector.go:82-134`). The registry is
parsed and `Validate()`-checked with the same `internal/verdicts` package `doctor` uses,
so a project's `zprof apply` and CI's repo-level consistency check can never disagree on
what a valid registry looks like. Deploying it as a merged key rather than a separate file
means the registry rides along for any consumer that already reads `schema.json` (the
Python collector, `zprof score`, and a planned guard renderer) without a new file to wire
up, and `--telemetry-only` redeploys it without any signature change.

This exists because a full `Apply` requires at least one overlay and, applied in zprof's
own repo, would overwrite zprof's own hand-written executor agents and rewrite managed
`CLAUDE.md` blocks — destructive for a project that only needs its telemetry collector
refreshed after a `profiles/base/zprof-collect.py` bug fix (see ADR-0001, decision D3).
`zprof apply --telemetry-only` (`cli/internal/cmd/apply.go:16-64, 138-139`) exposes this
narrow path: it accepts zero overlay arguments (rejecting any if given), skips loading or
carrying over `.zprof.yaml`, and calls `apply.DeployTelemetry(pwd, base)` directly.
`--minimal`, `--with-gates`, `--merge` have no effect in this mode. `--dry-run` prints the
three target paths without writing.

### Hooks

`EnsureHooks` (`settings.go:49`) reads/creates `.claude/settings.local.json`, and for each
of `SubagentStop` / `Stop` / `SessionStart` finds any existing entry that already invokes
`zprof-collect.py` (`zprofHookIndex`, `settings.go:105`) and replaces it in place if the
command differs, or appends a new entry otherwise. The `Stop` hook command is the collector
guard chained with `scoreHookCommand` (`settings.go:20-24, 34`), so `zprof score
--latest --quiet --no-collect` runs automatically after collection — no code change was
needed for this to "just work" once the hook entry is refreshed by a redeploy.

### See also

- `collector` (`profiles/base/zprof-collect.py`) — [collector.md](collector.md), the script
  this package deploys and keeps up to date
- `overlay` (`cli/internal/overlay/`, doc not yet written — see `PLAN.md`) — loads
  `Base`/`Overlay` from `profiles/`, source of `CollectorScript`/`TelemetrySchema`/`Verdicts`
  (`overlay/loader.go:48-51, 205-218`, reading `profiles/base/verdicts.yaml`)
- `verdicts` (`cli/internal/verdicts/`, doc not yet written — see `INDEX.md`) — parses and
  validates `verdicts.yaml`; `apply.renderSchema` calls `verdicts.Parse`/`Registry.Validate`/
  `Registry.Normalized` to produce the merged `schema.json` key; `doctor`'s
  `checkAgentVerdicts` uses the same package against every applied agent's contract
- [ADR-0001: config_hash resolution and telemetry-only redeploy](../adr/0001-collector-config-hash-and-telemetry-redeploy.md)
- [ADR-0003: verdicts registry — task-runner mapping, doctor check, schema.json deploy](../adr/0003-verdicts-registry.md)
