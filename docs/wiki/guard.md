## AI Context

Component: guard
Path: `profiles/base/zprof-guard.py` (rules: `profiles/base/guard.yaml`; tests:
  `profiles/base/tests/test_guard.py`)
Status: in-progress
Depends: [profiles-base]
Dependants: []
Exports: [pre_tool, evaluate_rules, resolve_role, dispatch_id, normalize_command,
  working_dir, project_root, load_config, deny_output, write_event, main,
  TOOLS_GUARDED, CONTEXTS]
Key invariants:
  - Fail-open always: `main()` wraps everything in `try/except`, logs a best-effort
    `decision:"error"` event on any exception, and always `sys.exit(0)` — a bug in the
    guard must never block a tool call (`profiles/base/zprof-guard.py:449-484`).
  - No `.claude/guard.json` → `load_config` returns `None` → silent allow, no event —
    guard-not-deployed is not an error (`zprof-guard.py:153-165`, `384-387`).
  - Only `Bash`, `Edit`, `Write`, `MultiEdit`, `NotebookEdit` pass through the guard;
    `Read`/`Grep`/`Glob`/`Agent` are not matched (latency) (`zprof-guard.py:24-26,
    380-382`).
  - Rule evaluation order is fixed and cheap-first: `tools` → `roles` → `not_roles` →
    `exempt_roles` → `match` → `context`; first firing rule in list order wins, no rule
    ever produces `allow` (`zprof-guard.py:204-262`).
  - A `context` name not registered in `CONTEXTS` makes the rule never fire (not deny,
    not skip-with-warning) — `CONTEXTS` is empty in #23, so all seven context-bearing
    rules (§5.2/§5.3/§5.5 `merge_preflight`/§5.6) are inert data until #24/#25 register
    evaluators (ADR-0004 D4, `zprof-guard.py:32, 237-247`).
  - `match`/`roles`/`not_roles` values starting with `$` (an unrendered `apply`
    reference) raise `ValueError` rather than being iterated as a regex/role list —
    prevents the reference string itself from silently matching everything
    (ADR-0004 D2, `zprof-guard.py:168-184`).
  - The script never reads `guard.yaml` — only `.claude/guard.json`; rendering
    `guard.yaml` → `guard.json` (resolving `$readonly_roles`, `$merge_roles`,
    `$mutating_bash_patterns`) is `zprof apply`'s job, not yet implemented (#28)
    (`zprof-guard.py:2-13, 153-165`; `guard.yaml:1-9`).
  - Deny events and the deny reason never contain the full command/content — `target`
    is the first two whitespace tokens of the normalized command or a file basename;
    `input_hash` is a 12-char sha1, mirroring (not importing) `zprof-collect.py`'s
    `_input_hash` byte-for-byte since the two scripts deploy separately
    (`zprof-guard.py:291-317`, ADR-0004 D9).
Spec refs: `docs/superpowers/specs/2026-09-27-guard-hooks-design.md` §4, §5, §7, §8.1
Test coverage: 105 unit + subprocess end-to-end tests in `test_guard.py`, incl.
  fail-open fault-injection (malformed `guard.json`, malformed stdin, bad regex,
  unrendered `$ref`, missing argv mode, missing subagent `meta.json`) and a drift check
  against `guard.yaml` (rule `id` set + `match` regex lists) — `python3 -m pytest
  profiles/base/tests/test_guard.py -q` → 105 passed (verified 2026-09-28,
  `feat/issue-23-zprof-guard-py`@`9ad6233`).

---

## Guard — Deterministic PreToolUse Enforcement

`zprof-guard.py` is a Claude Code `PreToolUse` hook: a stdlib-only Python script that
reads one tool-call payload from stdin, decides `deny` or silence (never `allow`,
never `ask`), and logs the decision. It replaces prompt-only policy ("don't force-push",
"only pr-shepherd merges") with a deterministic check that runs before the tool
executes. Issue #23 is the first of a six-issue milestone (#23–#28); this doc describes
what #23 actually ships, not the full design in the spec.

**Not yet deployed anywhere.** No project's `.claude/` directory runs this hook today —
that wiring (`zprof apply` writing `.claude/zprof-guard.py`, rendering
`guard.yaml` → `.claude/guard.json`, upserting the `PreToolUse` hook entry in
`settings.local.json`) is issue #28. Until then `guard.md` describes source-only
behavior, exercised by `test_guard.py` invoking the script directly.

### What's active in #23 vs. what's data-only

`guard.yaml` (`profiles/base/guard.yaml:1-138`) is the **full** catalog of rules
the eventual design calls for, but the rule engine only *acts* on rules whose
conditions it can evaluate today:

| Group (spec §) | Rules | Status in #23 |
|---|---|---|
| §5.1 stop-list, no context | `force_push`, `admin_merge`, `no_verify_commit`, `no_verify_other`, `branch_force_delete`, `remote_ref_delete`, `tag_delete`, `publish`, `curl_pipe_sh` | **active** — plain `tools` + `match` regex, no `context` |
| §5.4 read-only roles | `readonly_mutation` | **active** — `roles: $readonly_roles` + `match: $mutating_bash_patterns` |
| §5.5 merge gate | `merge_role` | **active** — `not_roles: $merge_roles`, no `context` needed (the rule engine is generic; see ADR-0004 D3 "conscious deviation") |
| §5.2 contextual stop-list | `rebase_published`, `amend_published`, `stash_in_worktree`, `remote_ref_delete_unmerged` | data only — `context` unregistered → never fires ([#24](../../plan-2.md)) |
| §5.3 write outside repo | `write_outside_repo` | data only — [#24](../../plan-2.md) |
| §5.5 merge preflight | `merge_preflight` | data only — [#25](../../plan-2.md) |
| §5.6 PR-create gate | `pr_create_gate` | data only — [#25](../../plan-2.md) |

`subagent-stop` mode (`zprof-guard.py:459-461`) is a recognized no-op placeholder for
the subagent-stop validator, [#26](../../plan-2.md). Integrating guard events into
`zprof score` is [#27](../../plan-2.md); actually deploying the hook into `.claude/`
is [#28](../../plan-2.md) — until #28 lands, everything in this file is inert in every
real project, including this one.

### Role resolution

`resolve_role(payload)` (`zprof-guard.py:63-87`) determines which zprof role made the
tool call, in order: (1) `payload["agent_type"]` if non-empty; (2) if
`transcript_path`'s parent directory is `subagents` and the filename matches
`agent-<id>.jsonl`, read `agentType` from the sibling `agent-<id>.meta.json` — a
missing/unparsable meta file resolves to `"unknown"` directly, it does not fall
through to step 3; (3) a `*.jsonl` transcript whose parent isn't `subagents` resolves
to `"main"`; (4) otherwise `"unknown"`. An unresolved role is logged once per
`session_id` (`event: "role_unresolved"`, deduped via `.agentlog/guard-state.json`,
last 200 sessions kept) rather than on every call (`zprof-guard.py:335-371, 398-411`).

`unknown` has no special-cased branch in the rule engine — it falls out of ordinary
set membership: `unknown` is not in `readonly_roles`, so `readonly_mutation` doesn't
apply to it, but it's also not in `merge_roles`, so `merge_role` does (ADR-0004 D2).

### Rule format (`guard.yaml` / `.claude/guard.json`)

Each rule: `id` (str, unique), `tools` (list, required), `match` (regex list, `re.search`
against a per-tool "subject" — normalized Bash command, or `file_path`/`notebook_path`
for Edit/Write/MultiEdit/NotebookEdit), `context` (name of a `CONTEXTS` evaluator),
`roles`/`not_roles` (role allow/deny lists), `reason` (Russian, no trailing period or
"don't work around" suffix — the script appends that). Top level: `version` (must be
`1`), `readonly_roles`, `merge_roles`, `allow_write_prefixes`, `permissions_deny`
(a `settings.local.json` belt-and-suspenders list, not read by this script),
`exempt_roles` (map `rule_id → [roles]`, empty in #23), `rules`.

`guard.yaml` is *implementer-authored source*, not what the script reads: `$readonly_roles`,
`$merge_roles`, `$mutating_bash_patterns` are bare scalars that `zprof apply` (#28) must
substitute when rendering `.claude/guard.json` — `mutating_bash_patterns` itself lives in
`profiles/base/telemetry.yaml:85-94` (shared with the existing P5/P6 telemetry scoring
logic, not duplicated here). `load_config` only ever parses JSON
(`zprof-guard.py:153-165`); an unrendered `$ref` reaching the script is a fail-open bug
condition, not a supported input (see invariant above).

`readonly_roles` (12, `guard.yaml:11-13`): `auditor`, `auditor-deep`, `explorer`,
`architect`, `reviewer`, `bug-hunter`, `expert-panel`, `evaluator`,
`evaluator-telemetry`, `evidence-auditor`, `north-star-auditor`, `plan-reviewer`.
`pr-shepherd` is deliberately excluded (ADR-0004 D6) — it stamps merge commits, so it
cannot be purely read-only; `merge_roles` is `[pr-shepherd]` alone.

### Deny output and telemetry

A firing rule produces the exact §5.7 payload via `deny_output`
(`zprof-guard.py:265-277`):

```json
{"hookSpecificOutput": {"hookEventName": "PreToolUse", "permissionDecision": "deny",
  "permissionDecisionReason": "zprof guard [<id>]: <reason>. Не обходи: верни `verdict: blocked`, reason: <id>."}}
```

`verdict: blocked` in the reason string is intentionally compatible with the
`base_enum` in `verdicts.yaml` ([ADR-0003](../adr/0003-verdicts-registry.md)), so a
denied agent's own return still validates against the schema. Every `deny`/`error`
event is appended as one JSON line to `.agentlog/guard-events.jsonl` under `flock` +
`fsync` (`write_event`, `zprof-guard.py:320-332`) — a separate file and a separate lock
(`.agentlog/.guard.lock`) from the telemetry collector's `.agentlog/.lock` and
`.agentlog/tool-events.jsonl`, because guard runs on every guarded tool call while the
collector holds its lock for whole-session operations (ADR-0004 D9).

### See also

- [Collector](collector.md) — sibling hook in `profiles/base/`; `guard`'s `_input_hash`
  mirrors (does not import) `collector`'s, since the two scripts deploy independently
- [Apply](apply.md) — does **not** deploy guard yet; deployment, `guard.yaml` →
  `guard.json` rendering, and the `PreToolUse` hook entry are #28
- [ADR-0004: `zprof-guard.py pre-tool` — frame, `guard.yaml`/`guard.json` format, stop-list §5.1, read-only roles](../adr/0004-zprof-guard-pre-tool-frame.md)
- `docs/superpowers/specs/2026-09-27-guard-hooks-design.md` — full guard design (§2
  decisions, §5 rule tables, §6 subagent-stop validator, §12 phase-2 deferred work)
