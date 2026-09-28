## AI Context

Component: guard
Path: `profiles/base/zprof-guard.py` (rules: `profiles/base/guard.yaml`; tests:
  `profiles/base/tests/test_guard*.py`; deploy: `cli/internal/apply/guard.go`)
Status: implemented
Depends: [profiles-base]
Dependants: [apply, score, stats, doctor]
Exports: [pre_tool, subagent_stop, evaluate_rules, resolve_role, dispatch_id,
  normalize_command, working_dir, project_root, load_config, deny_output,
  write_event, main, TOOLS_GUARDED, CONTEXTS]
Key invariants:
  - Fail-open always: `main()` wraps everything in `try/except`, logs a best-effort
    `decision:"error"` event on any exception, and always `sys.exit(0)` — a bug in the
    guard must never block a tool call (`profiles/base/zprof-guard.py:813-848`).
  - No `.claude/guard.json` → `load_config` returns `None` → silent allow, no event —
    guard-not-deployed is not an error (`zprof-guard.py:156-168`, `729-730`).
  - Only `Bash`, `Edit`, `Write`, `MultiEdit`, `NotebookEdit` pass through the guard;
    `Read`/`Grep`/`Glob`/`Agent` are not matched (latency) (`zprof-guard.py:26-28,
    723-725`).
  - Rule evaluation order is fixed and cheap-first: `tools` → `roles` → `not_roles` →
    `exempt_roles` → `match` → `context`; first firing rule in list order wins, no rule
    ever produces `allow` (`zprof-guard.py:207-265`).
  - `CONTEXTS` is populated with six evaluators as of #25 — `head_on_remote`,
    `linked_worktree`, `write_outside_repo`, `branch_pr_merged` (#24) plus
    `merge_preflight`, `pr_create_gate` (#25) — covering §5.2/§5.3/§5.5/§5.6. A
    `context` name still not registered makes the rule never fire (not deny, not
    skip-with-warning) (ADR-0004 D4, ADR-0005; `zprof-guard.py:29-35, 1012-1019`).
  - Three of the four #24 evaluators (`head_on_remote`, `linked_worktree`,
    `write_outside_repo`) are fail-open: a `git` call that errors or times out is
    recorded as a `context_error` journal event (`decision: null`) and the evaluator
    returns `False` (does not fire). `branch_pr_merged` is the one fail-closed
    evaluator — its whole body runs inside `try/except Exception: return True` and it
    never emits `context_error`, because an uncaught exception here would otherwise
    escape into `main()`'s outer fail-open handler and *allow* an irreversible remote
    branch deletion (ADR-0005 E2/E6; `zprof-guard.py:330-338, 515, 579-581`).
  - `merge_preflight` (#25) is also fail-open, but through a third mechanism,
    distinct from both #24 patterns: any `gh` failure (non-zero exit, timeout,
    bad JSON, unexpected shape) calls `_note_unverified`, not
    `_note_context_error` — it logs `event: "pre-tool", decision:
    "allow_unverified"` (a conscious gate decision), not `decision: null` (a
    silent diagnostic). There is no outer `try/except Exception` around
    `_merge_preflight`'s body (unlike `branch_pr_merged`); an unexpected bug
    reaches `main()`'s fail-open handler and logs `decision: "error"` instead
    (ADR-0006 F1, Consequences; `zprof-guard.py:347-360, 787-855`).
  - `pr_create_gate` (#25) has no `roles`/`not_roles` — it applies to every role
    including `main`, `unknown`, `pr-shepherd`, unlike every other rule in
    `guard.yaml`; it never calls `_run` (no network), so it cannot produce
    `allow_unverified` for network reasons — only for an unreadable
    `--body-file` (`guard.yaml:135-139`; `zprof-guard.py:947-1009`).
  - Role `unknown` on a rule whose deny depends on role (`roles`/`not_roles`
    non-empty — currently `merge_role`, `remote_ref_delete`) gets an appended
    hint in the deny reason: `"; роль не разрешена (роль вызывающего не
    определена — unknown), проверь `zprof doctor`"`. `_is_role_gated` decides
    generically off the rule's `roles`/`not_roles`, not a hardcoded rule id —
    role-independent rules like `force_push` never get the hint (ADR-0006 F5;
    `zprof-guard.py:1152-1167, 1261-1263`).
  - `remote_ref_delete` carries `not_roles: [pr-shepherd]`; for pr-shepherd the
    decision moves to `remote_ref_delete_unmerged` (`roles: [pr-shepherd]`, `context:
    branch_pr_merged`) instead — a drift test asserts the two role lists stay
    set-equal (ADR-0004 D5, ADR-0005; `guard.yaml:52-57, 99-105`;
    `test_guard_context.py:393-395`).
  - `match`/`roles`/`not_roles` values starting with `$` (an unrendered `apply`
    reference) raise `ValueError` rather than being iterated as a regex/role list —
    prevents the reference string itself from silently matching everything
    (ADR-0004 D2, `zprof-guard.py:171-187`).
  - The script never reads `guard.yaml` — only `.claude/guard.json`; rendering
    `guard.yaml` → `guard.json` (resolving `$readonly_roles`, `$merge_roles`,
    `$mutating_bash_patterns`) is `zprof apply`'s job (`deployGuard`,
    `cli/internal/apply/guard.go`, #28, ADR-0009) — see "Deployment" below
    (`zprof-guard.py:2-13, 156-168`; `guard.yaml:1-9`).
  - Deny events and the deny reason never contain the full command/content — `target`
    is the first two whitespace tokens of the normalized command or a file basename;
    `input_hash` is a 12-char sha1, mirroring (not importing) `zprof-collect.py`'s
    `_input_hash` byte-for-byte since the two scripts deploy separately
    (`zprof-guard.py:602-635`, ADR-0004 D9).
  - `subagent-stop` mode (ADR-0007, #26) is a second, independent `main()` branch —
    it never calls `load_config()`, never touches `CONTEXTS`, and does not require
    `.claude/guard.json` to exist at all, unlike every rule described above
    (`zprof-guard.py:1462-1479, 1562-1563`).
  - `subagent_stop`'s final-reply text source order is `payload["last_assistant_message"]`
    → `payload["agent_transcript_path"]` → `payload["transcript_path"]` **only if** that
    path is itself a subagent transcript (`agent-<id>.jsonl` under `subagents/`) — the
    main session's own `transcript_path` is never read as if it were the subagent's last
    turn (ADR-0007 G5; `zprof-guard.py:1398-1423`).
  - Two different "file missing" outcomes: an unreadable/missing **contract** file
    (`.claude/agents/<role>.md`, then `.claude/agents/gates/<role>.md`) is caught by a
    local `except OSError` — silent pass, no journal event; an unreadable/corrupt
    **transcript** has no local `try/except` and propagates to `main()`'s single outer
    `except Exception`, which logs `decision: "error"` and still exits 0 (ADR-0007 G5;
    `zprof-guard.py:1375-1395, 1426-1459, 1567-1585`).
  - A first-time `return_format` violation blocks once (`event: "subagent-stop",
    decision: "block"`); the same violation on retry (`payload["stop_hook_active"] is
    True`) logs `event: "format_unfixed", decision: null` instead and returns `None` —
    never blocks a second time, bounding the cost to one extra subagent turn
    (ADR-0007 G1/G8; `zprof-guard.py:1504-1519`).
  - `dispatch_id()` writes the **raw** `toolUseId`, never the collector's composite
    `claude-code:<session_id>:<toolUseId>` — `zprof score`'s `AttachGuardEvents`
    matches guard events to runs on the last `:`-separated segment specifically
    because of this asymmetry (#27, ADR-0008 H2.1; `zprof-guard.py:96-102`).
  - `zprof score`'s P7 counts a guard `deny`/`block` row as a contract violation
    **regardless of `verdict_exempt_roles`** — the guard-events loop in `computeP7`
    is separate from the contract-fields loop above it and never checks
    `cfg.ExemptRoles` (#27, ADR-0008 H3; `cli/internal/score/metrics.go:261-268`).
  - `guard.enabled: false` (`.zprof.yaml`) gates only *deployment* — the
    base→overlay→project `guard.yaml` merge and `$ref` resolution always run,
    so a broken overlay `guard.yaml` fails `zprof apply` even for a project
    that has guard disabled; `.claude/zprof-guard.py`/`guard.json` from a prior
    enabled apply are left on disk untouched, not deleted (#28, ADR-0009 I4/I6;
    `cli/internal/apply/guard.go:483-544`).
  - `permissions.deny` upsert/subtraction is computed from the **current**
    merge result, not from `.claude/guard.json` on disk — disabling guard
    subtracts today's `permissions_deny`, not whatever was deployed
    historically; foreign `deny` entries and the rest of `permissions.*` are
    never touched (#28, ADR-0009 I4; `cli/internal/apply/settings.go:257-320`).
  - `zprof doctor` (#29, design §10) diagnoses guard's deployed state entirely
    read-only, off the same artifacts `deployGuard` writes — it never imports
    `zprof-guard.py` or `guard.yaml`. `checkGuardDeployment` is the single
    gate on `.zprof.yaml`'s `guard.enabled: false`: when set, it returns one
    `info` Issue and skips `checkGuardHooks`/`checkGuardConfig`/
    `checkPermissionsDeny` entirely, so a project that opted out isn't nagged
    about a deployment it declined (`cli/internal/doctor/diagnostics.go:1131-1146`).
    `checkRoleResolution` is the one guard-adjacent check that ignores that
    gate — it inspects `~/.claude/projects/<slug>/*/subagents/*.meta.json` for
    an `agentType` key regardless of `guard.enabled`, since role resolution
    also feeds `resolve_role`'s non-guard callers
    (`cli/internal/doctor/diagnostics.go:1247-1273`).
  - The guard *doctrine* (the one-line policy an agent reads, distinct from the
    `zprof-guard.py`/`guard.yaml` mechanism above) lives entirely in the
    prompt layer, not in the script: `profiles/base/manifest.yaml`'s
    `guard_doctrine` key and `profiles/base/claude-block-base.md`'s
    `### Guard` subsection carry the same sentence, rendered into a project's
    `CLAUDE.md` under the `<!-- zprof:begin doctrine -->` managed block
    (`## Doctrine` → `### Guard`); `guard_doctrine` is an unknown top-level
    key to `manifest.OverlayManifest`'s loose YAML unmarshal, so unlike
    `stop_list` it is never rendered into `## Stop list` and `checkStopLists`
    never sees it (#30; see "Doctrine and prompt contracts" below).
  - Two overlay `guard.yaml` files exercise the base→overlay merge
    (`mergeRules`, "Deployment" above) with real, non-synthetic content:
    `profiles/overlays/ios-swift/guard.yaml`'s `exempt_roles: {publish:
    [testflight-shipper]}` and `profiles/overlays/backend-python/guard.yaml`'s
    `pip_install` rule (`tools: [Bash]`, denies `pip install`/`pip3
    install`/`poetry add`) — both #30, both prompt/config-only, no change to
    `zprof-guard.py` or the Go merge/render logic itself (see "Doctrine and
    prompt contracts" below).
  - `zprof doctor` on a project with **no `.zprof.yaml` at all** (`fs.ErrNotExist`,
    not a parse error) but a deployed `.claude/zprof-collect.py` or
    `.claude/zprof-guard.py` — zprof's own repo checkout, `--telemetry-only`,
    ADR-0001/#22 — runs `checkGuardDeployment` against a zero-value
    `&manifest.ProjectManifest{}` instead of skipping it: its `proj.Guard !=
    nil && !proj.Guard.IsEnabled()` gate (`diagnostics.go:1187`) short-circuits
    on `proj.Guard == nil` without ever calling `IsEnabled()`, so guard is
    diagnosed as enabled — the same outcome `GuardConfig.IsEnabled()`'s own
    nil-receiver default gives (ADR-0009 §8.3, `manifest/project.go:126-128`),
    just reached without calling it (#64; `diagnostics.go:159-174`; see
    "Telemetry-only diagnostics" below).
Spec refs: `docs/superpowers/specs/2026-09-27-guard-hooks-design.md` §4, §5, §6, §7,
  §8.1–§8.4, §9, §10, §11, §13
Test coverage: 112 unit + subprocess end-to-end tests in `test_guard.py` (stop-list,
  read-only roles, merge gate, D5 `remote_ref_delete`/`remote_ref_delete_unmerged`
  role split) plus 30 tests in `test_guard_context.py` (the four #24 context
  evaluators, the `context_error` journal event, the D5 drift invariant) plus 56
  tests in `test_guard_merge.py` (#25 — `merge_preflight`/`pr_create_gate` table
  cases via `monkeypatch(zprof_guard, "_run", ...)`, two real-subprocess PATH-stub
  e2e cases for AC6, the `unknown`-role `zprof doctor` hint, the two regressed
  `UNKNOWN_CONTEXT_CASES` entries) plus 64 tests in `test_guard_subagent_stop.py`
  (#26 — frontmatter/`return_format:` parsing incl. `|-`/`>`/inline-value
  non-matches, the `blocked-<reason>` prefix on the live `pr-shepherd.md` contract,
  `last_assistant_message`-over-transcript priority, main-transcript-never-read,
  `agents/<role>.md`-without-`return_format`-doesn't-fall-through-to-`gates/`,
  `stop_hook_active` → `format_unfixed`, AC7 corrupt-transcript → `error`), incl.
  fail-open fault-injection (malformed `guard.json`, malformed stdin, bad regex,
  unrendered `$ref`, missing argv mode, missing subagent `meta.json`, `_run`
  timeout/`OSError` via `monkeypatch`) and a drift check against `guard.yaml` (rule
  `id` set + `match` regex lists) — `python3 -m pytest profiles/base/tests/test_guard.py
  profiles/base/tests/test_guard_context.py profiles/base/tests/test_guard_merge.py
  profiles/base/tests/test_guard_subagent_stop.py -q` → 272 passed (501 passed for
  `profiles/base/tests/` as a whole; verified 2026-09-28,
  `feat/guard-subagent-stop-26`@`177539f`). #28's Go-side deploy is unrelated code
  (`zprof-guard.py`/`guard.yaml` themselves are unchanged by #28) and covered
  separately in `cli/internal/apply/guard_test.go` (parse/merge/`$ref`-resolve/
  render, 30+ cases) and `guard_e2e_test.go` (a real `zprof-guard.py` subprocess
  denying `git push --force` after `deployGuard` writes it) — see `apply.md`'s
  Test coverage entry for the full count. #29's doctor checks are covered by
  24 unit tests in `cli/internal/doctor/diagnostics_test.go` (5 for
  `checkGuardHooks` — missing/partial/malformed-JSON, 4 for `checkGuardConfig`
  — missing/malformed/empty-`rules`/populated, 5 for `checkPermissionsDeny`
  — diffing incl. superset, 3 for `checkGuardDeployment` — the
  `guard.enabled: false` gate, 7 for `checkRoleResolution` — incl.
  unreadable/malformed `meta.json`) plus
  `TestDiagnoseIncludesGuardAndRoleResolutionChecks` — `go test
  ./internal/doctor/...` → 108 passed, 95.5% coverage (verified 2026-09-28,
  `feat/doctor-guard-checks-29`@`d60e4a3`; `go test ./...` → 458 passed for the
  Go module as a whole). #30 (doctrine string + contract lines + two overlay
  `guard.yaml` files, prompt/config-only — no `zprof-guard.py`/merge-logic
  change) adds one pytest case,
  `test_backend_python_pip_install_rule_denies_pip_and_poetry`
  (`profiles/base/tests/test_guard.py:423-441`, three deny + one allow
  assertion against the engine directly with the overlay's exact rule body)
  and two Go E2E additions in `cli/internal/apply/guard_e2e_test.go`: an
  `exempt_roles.publish` assertion appended to the existing
  `TestE2E_GuardDeploysAndEnforcesForcePush` (:104-108) and a new
  `TestE2E_GuardDeploysBackendPythonPipInstallRule` (:147-224) that runs a
  real `Apply()` with the `backend-python` overlay and a real
  `zprof-guard.py` subprocess denying `pip install`/`poetry add` while
  allowing `uv add`. #64's telemetry-only `Diagnose()` branch is covered by
  `TestDiagnoseTelemetryOnlyMode` (table cases: collector-only, guard-only,
  neither deployed, and a broken-but-present manifest, which must stay the
  old single-`LevelError` behavior even with telemetry deployed) and
  `TestDiagnoseTelemetryOnlySkipsManifestGatedChecks` (asserts
  `checkTaskRunner` does *not* run against a fixture that would otherwise
  fail it, and that guard's checks still fire off the zero-value manifest)
  in `cli/internal/doctor/diagnostics_test.go:113-186`.

---

## Guard — Deterministic PreToolUse & SubagentStop Enforcement

`zprof-guard.py` is a Claude Code hook script with two modes, `pre-tool` and
`subagent-stop`. In `pre-tool` mode it reads one tool-call payload from stdin,
decides `deny` or silence (never `allow`, never `ask`), and logs the decision — it
replaces prompt-only policy ("don't force-push", "only pr-shepherd merges") with a
deterministic check that runs before the tool executes. In `subagent-stop` mode it
reads one `SubagentStop` payload and decides `block` or silence, checking that a
subagent's final reply opens with the `verdict:`/`completion:` line its own
`return_format` contract promises. Issues #23–#28 are the six-issue milestone this
doc describes — §5.1 stop-list, read-only roles, and the merge gate from #23;
§5.2/§5.3 context rules from #24; §5.5 merge preflight and §5.6 PR-create gate
from #25; §6 the subagent-stop `return_format` validator from #26; §7 the
`zprof score`/`zprof stats` guard-events integration from #27 (Go-side only —
the guard script itself is unchanged); §8.2–§8.4/§9 the `zprof apply` deployment
from #28 (also Go-side only, see "Deployment" below) — not the full design in
the spec (§12's phase-2 items are still deferred). #30 adds prompt-layer-only
artifacts on top of #23–#29 — a doctrine string, two agent-contract lines, and
two overlay `guard.yaml` files exercising the already-implemented merge — see
"Doctrine and prompt contracts" below.

**Deployed by `zprof apply` since #28.** `zprof apply <overlay>...`, `zprof sync`,
and `zprof apply --telemetry-only` all write `.claude/zprof-guard.py` and render
`.claude/guard.json`, and upsert the `PreToolUse`/`SubagentStop` guard hooks into
`.claude/settings.local.json` — see "Deployment" below for how. This is true for
**both** modes: `subagent-stop` doesn't depend on `guard.json` the way `pre-tool`
does (see below), but its hook entry is deployed identically to `pre-tool`'s.

### What's active in `guard.yaml` as of #23–#25

`guard.yaml` (`profiles/base/guard.yaml:1-139`) is the **full** catalog of rules
the eventual design calls for. As of #25 every rule in it is active — the last two
data-only placeholders (`merge_preflight`, `pr_create_gate`) got their evaluators
registered by ADR-0006. The subagent-stop `return_format` validator from #26 is
**not** a `guard.yaml` rule at all — it's a separate `main()` mode (see
"Subagent-stop validator" below) — so it never appears in this table.

| Group (spec §) | Rules | Status |
|---|---|---|
| §5.1 stop-list, no context | `force_push`, `admin_merge`, `no_verify_commit`, `no_verify_other`, `branch_force_delete`, `remote_ref_delete`, `tag_delete`, `publish`, `curl_pipe_sh` | **active** (#23) — plain `tools` + `match` regex, no `context`; `remote_ref_delete` additionally carries `not_roles: [pr-shepherd]` since #24 |
| §5.4 read-only roles | `readonly_mutation` | **active** (#23) — `roles: $readonly_roles` + `match: $mutating_bash_patterns` |
| §5.5 merge gate | `merge_role` | **active** (#23) — `not_roles: $merge_roles`, no `context` needed (the rule engine is generic; see ADR-0004 D3 "conscious deviation") |
| §5.2 contextual stop-list | `rebase_published`, `amend_published`, `stash_in_worktree`, `remote_ref_delete_unmerged` | **active** (#24) — `context: head_on_remote`/`linked_worktree`/`branch_pr_merged` now registered ([#24](../../plan-2.md), ADR-0005) |
| §5.3 write outside repo | `write_outside_repo` | **active** (#24) — `context: write_outside_repo` now registered ([#24](../../plan-2.md), ADR-0005) |
| §5.5 merge preflight | `merge_preflight` | **active** (#25) — `context: merge_preflight` now registered (ADR-0006) |
| §5.6 PR-create gate | `pr_create_gate` | **active** (#25) — `context: pr_create_gate` now registered, no `roles`/`not_roles` (applies to every role) (ADR-0006) |

Reading guard events into `zprof score`/`zprof stats` shipped in
[#27](../../plan-2.md) (ADR-0008; see "Score and stats integration" below);
deploying the hook into `.claude/` shipped in [#28](../../plan-2.md) (ADR-0009;
see "Deployment" below) — a project that runs `zprof apply`/`zprof sync` (or
`zprof apply --telemetry-only`) now actually writes
`.agentlog/guard-events.jsonl`, so #27's reading code has something to read.

### Role resolution

`resolve_role(payload)` (`zprof-guard.py:66-91`) determines which zprof role made the
tool call, in order: (1) `payload["agent_type"]` if non-empty; (2) if
`transcript_path`'s parent directory is `subagents` and the filename matches
`agent-<id>.jsonl`, read `agentType` from the sibling `agent-<id>.meta.json` — a
missing/unparsable meta file resolves to `"unknown"` directly, it does not fall
through to step 3; (3) a `*.jsonl` transcript whose parent isn't `subagents` resolves
to `"main"`; (4) otherwise `"unknown"`. An unresolved role is logged once per
`session_id` (`event: "role_unresolved"`, deduped via `.agentlog/guard-state.json`,
last 200 sessions kept) rather than on every call (`zprof-guard.py:670-712, 740-753`).

`unknown` has no special-cased branch in the rule engine — it falls out of ordinary
set membership: `unknown` is not in `readonly_roles`, so `readonly_mutation` doesn't
apply to it, but it's also not in `merge_roles`, so `merge_role` does (ADR-0004 D2).

Since #25, when a role-gated rule (`roles`/`not_roles` non-empty) denies an `unknown`
call, `pre_tool()` appends a hint to the deny reason — `"; роль не разрешена (роль
вызывающего не определена — unknown), проверь `zprof doctor`"` — so an agent whose
role failed to resolve gets pointed at the diagnosis command instead of a bare
"merge выполняет только pr-shepherd". `_is_role_gated(config, rule_id)` looks up the
rule by id in `config["rules"]` and checks `roles`/`not_roles` generically — no rule
id is hardcoded, so the hint also fires for `remote_ref_delete`
(`not_roles: [pr-shepherd]`) and stays silent for role-independent rules like
`force_push`, where it would be misleading (ADR-0006 F5; `zprof-guard.py:1152-1167,
1261-1263`).

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
(`zprof-guard.py:156-168`); an unrendered `$ref` reaching the script is a fail-open bug
condition, not a supported input (see invariant above).

`readonly_roles` (12, `guard.yaml:11-13`): `auditor`, `auditor-deep`, `explorer`,
`architect`, `reviewer`, `bug-hunter`, `expert-panel`, `evaluator`,
`evaluator-telemetry`, `evidence-auditor`, `north-star-auditor`, `plan-reviewer`.
`pr-shepherd` is deliberately excluded (ADR-0004 D6) — it stamps merge commits, so it
cannot be purely read-only; `merge_roles` is `[pr-shepherd]` alone.

### Context evaluators (`CONTEXTS`, #24/#25)

`CONTEXTS` (`zprof-guard.py:29-35`) is populated near the bottom of the module
(`zprof-guard.py:1012-1019`) with six evaluators, each backing one or more
`guard.yaml` rules (ADR-0005, ADR-0006):

| Evaluator | Rule(s) | Fires when |
|---|---|---|
| `head_on_remote` | `rebase_published`, `amend_published` | HEAD is reachable from a remote-tracking branch (`git branch -r --contains HEAD`) **and** has an upstream (`git rev-parse --abbrev-ref @{u}`) — "no upstream" alone is a normal "no" (detached HEAD / unpublished branch), not an error, so the upstream check only runs once publication is already confirmed (`zprof-guard.py:341-368`) |
| `linked_worktree` | `stash_in_worktree` | `git rev-parse --git-dir --git-common-dir`, both resolved relative to the command's working dir (not the guard process's cwd) via `os.path.realpath`, differ (`zprof-guard.py:370-393`) |
| `write_outside_repo` | `write_outside_repo` | the realpath'd `file_path`/`notebook_path` target matches none of `allow_write_prefixes` (glob-aware, `$VAR`/`~` expanded) and isn't inside a linked worktree of this repo (`git rev-parse --git-common-dir`, run from the nearest existing ancestor dir, resolves to `$CLAUDE_PROJECT_DIR/.git`) (`zprof-guard.py:463-508`) |
| `branch_pr_merged` | `remote_ref_delete_unmerged` (`roles: [pr-shepherd]`) | a parsed `git push --delete`/`:<ref>` names exactly one branch, and `gh pr list --head <name> --state merged --json number` returns no merged PR (`zprof-guard.py:515-581`) |

All external calls go through `_run` (`zprof-guard.py:291-317`) — a `subprocess.run`
wrapper with a timeout (`_GIT_TIMEOUT` 3s, `_GH_TIMEOUT` 10s) and
`GIT_TERMINAL_PROMPT=0`/`GH_PROMPT_DISABLED=1`/`GIT_OPTIONAL_LOCKS=0` to prevent
interactive hangs, that never raises: `(0, stdout)` on success, `(returncode, "")` on
a non-zero exit, `(None, ExceptionClassName)` on timeout/`OSError`.

**Fail-open vs. fail-closed.** `head_on_remote`, `linked_worktree` and
`write_outside_repo` are fail-open: a `_run` error calls `_note_context_error`
(`zprof-guard.py:330-338`), which accumulates a diagnostic on `call["context_errors"]`,
and the evaluator returns `False` (does not fire — a broken evaluator must not block
an unrelated tool call). `branch_pr_merged` is the one fail-closed evaluator
(ADR-0005 E6): its entire body runs inside `try/except Exception: return True`, and it
never calls `_note_context_error` — any failure (unparseable command, non-zero/timeout
`gh`, malformed JSON, ambiguous/zero delete targets) denies rather than silently
allowing an irreversible remote branch deletion (`zprof-guard.py:515, 579-581`).

**`context_error` journal event.** `pre_tool()` flushes `call["context_errors"]` to
`.agentlog/guard-events.jsonl` in a `finally` block after `evaluate_rules` (so a later
rule raising — bad regex/`$ref`, fail-open — doesn't swallow earlier context errors),
one line per failure: `event: "context_error"`, `decision: null`, `rule` (the failing
rule's id), `detail` (`"<argv[0]> <argv[1]>: exit <rc>"` or `"...: <ExceptionName>"` —
never paths, stdout or stderr), all other keys as in the D9 deny event
(`zprof-guard.py:767-786`).

### Merge gate (`merge_preflight`) and PR-create gate (`pr_create_gate`), #25

ADR-0006 registers the two evaluators that §5.5/§5.6 rules had been pointing at
since #23/#24 without effect (data-only, D4). Both parse the **raw**
`call["tool_input"]["command"]`, not the whitespace-normalized `call["command"]`
that `normalize_command` produces for `match` regexes — normalization collapses
`\s+` to a single space, including newlines *inside quotes*, which would destroy
a PR body's `## Gate` line before it could ever be found (ADR-0006 Context §3).
Both share two helpers (`zprof-guard.py:613-678`):

- `_shell_tokens(raw)` — `shlex.shlex(..., posix=True, punctuation_chars=True)`
  with `commenters = ""` (so `#` in `-t "a#b"` isn't treated as a comment,
  ADR-0006 Context §7); raises `ValueError` on an unbalanced quote/trailing
  backslash, which both evaluators turn into `_note_unverified(..., _PARSE_ERROR,
  "shlex")` + `return False` (allow, not deny — F3/F4).
- `_invocations(tokens, words)` — finds every occurrence of an argv prefix like
  `("gh", "pr", "merge")` by `os.path.basename` (so `rtk gh pr create`,
  `/opt/homebrew/bin/gh pr create` match without special-casing), returning the
  argument list up to the next shell-operator token; a bare occurrence only
  inside a quoted string (`grep -r "gh pr create" docs/`) yields zero
  invocations, silently — no event, by design (ADR-0006 F2).

**`merge_preflight`** (rule `merge_preflight`, `roles: $merge_roles`;
`zprof-guard.py:787-855`) fires on `gh pr merge ...` and `gh api .../pulls/N/merge`
invocations by the merge role(s). For each parsed target `(selector, repo)` it
runs one `gh pr view [selector] [-R repo] --json number,body,closingIssuesReferences,state`
(`_GH_TIMEOUT` 10s) and derives the deny/allow from the JSON: `state != "OPEN"`
skips the target silently (nothing left to protect); otherwise the PR must have
either a `Closes #N` in the body or a non-empty `closingIssuesReferences`, **and**
a `## Gate` section (`_CLOSES_RE`/`_GATE_RE`, `zprof-guard.py:668-669`) — missing
either (or both) returns a PR-specific deny reason, e.g. `` PR #7 без раздела
`## Gate` ``. Any `gh` failure for a target (non-zero exit, timeout, invalid JSON,
unexpected shape) is fail-open: `_note_unverified(call, rule, _PREFLIGHT_UNVERIFIED,
...)`, that target is skipped, not denied (§5.5 decision 1 — a network/`gh` hiccup
must not block a routine, revertible merge). Unlike `branch_pr_merged`, there is no
outer `try/except Exception` — an unexpected bug is not caught here and falls
through to `main()`'s fail-open handler, logging `decision: "error"` instead of
`allow_unverified` (ADR-0006 F3, Consequences).

**`pr_create_gate`** (rule `pr_create_gate`, no `roles`/`not_roles` —
`zprof-guard.py:947-1009`) fires on every `gh pr create ...` invocation by
**any** role (§5.6: "anyone creating a PR in a zprof project gives `Closes #N`
and `## Gate`"); the only opt-out is `exempt_roles.pr_create_gate` in
`.zprof.yaml`. It never calls `_run` — no network, only local `shlex` and (for
`--body-file`) a local file read. Per invocation: any `--fill`/`--fill-first`/
`--fill-verbose`/`-f` flag denies outright (fill bypasses the body check, so it's
checked before body sources, even if `-b` is also present); no `-b`/`--body`/
`-F`/`--body-file` source denies "нет тела PR"; otherwise each body source is
checked for both markers via `_missing_markers`. A `--body-file` pointing at
stdin (`-`, `/dev/stdin`, `/dev/fd/0`) or an unreadable/missing file is
**fail-open**, not deny — `_note_unverified(..., _PREFLIGHT_UNVERIFIED, ...)` and
the invocation is skipped: `working_dir()` is a heuristic that can't see `cd`/
`pushd`/`git -C` mid-chain, so a false mismatch would wrongly deny a valid PR,
while a genuinely missing file makes `gh` itself fail with no PR created — allow
costs nothing there (ADR-0006 F4).

**`allow_unverified` journal event.** Both evaluators share `_note_unverified`
(`zprof-guard.py:347-360`), a `_note_context_error`-shaped accumulator on
`call["unverified"]` but a distinct *decision*, not a diagnostic: `context_error`
means "the evaluator couldn't answer, the rule stays silent" (`decision: null`);
`allow_unverified` means "a gate rule made a conscious fail-open call"
(`decision: "allow_unverified"`). `pre_tool()`'s `finally` block flushes it right
after the `context_errors` loop (`zprof-guard.py:1170-1285`), one line per
skipped target/source: `event: "pre-tool"`, `decision: "allow_unverified"`, `rule`
(the id of `merge_preflight`/`pr_create_gate` — never the reason code, so
`zprof stats` "top rules" still attributes correctly), `detail` =
`"<code>: <specifics>"` where `<code>` is `preflight_unverified` or `parse_error`
(a closed set — `_PREFLIGHT_UNVERIFIED`/`_PARSE_ERROR`,
`zprof-guard.py:342-344`) and `<specifics>` is a fixed string or exception class
name only (never a path, command text, stdout, or stderr). It's written even when
a later rule in the same call ultimately denies — it records that *this* rule
passed the call through unverified, not the call's final outcome (ADR-0006 F1).

### Subagent-stop validator (`return_format`), ADR-0007 / #26

`subagent_stop(payload)` (`zprof-guard.py:1462-1542`) is `main()`'s second mode,
wired in as one `elif mode == "subagent-stop"` inside the same top-level `try` as
`pre-tool` (`zprof-guard.py:1562-1563`). It reuses `resolve_role`/`dispatch_id`/
`project_root`/`_now_ts`/`write_event`/`_safe_write_event` unchanged, but **does
not** call `load_config()`, does not touch `CONTEXTS`, and does not require
`.claude/guard.json` to exist — it is a standalone `main()` branch, not a
`guard.yaml` rule (ADR-0007 Decision). It checks that a subagent's final reply
starts with the `verdict:`/`completion:` line its own `return_format: |` contract
promises, with an allow-listed value.

**Contract lookup (`_load_role_contract`, `zprof-guard.py:1375-1395`).**
`<root>/.claude/agents/<role>.md`, then `<root>/.claude/agents/gates/<role>.md` —
`root` is `project_root(payload)`, the same helper `pre_tool()` uses. The first
file that can be *read* wins outright, even when its parse result is `None` (no
`return_format: |` block) — a role's `agents/<role>.md` without one never falls
through to `gates/<role>.md`. A missing/unreadable file (`OSError`) is the
expected "no contract here" outcome, caught locally, not a fail-open condition.
`role in ("main", "unknown")`, or a role containing `/`/`\` or starting with `.`,
short-circuits to `None` without touching disk.

**Frontmatter parsing (`_return_format_contract`, `zprof-guard.py:1293-1337`) is a
hand-rolled line scanner, not a YAML parser** (ADR-0007 G3): an opening `---`
line, a `return_format: |` line (only the bare `|` indicator — `|-`, `>`, or an
inline value are treated as "block absent", since no file in the repo uses them
today), then the first non-blank/non-`#` line inside the block must match
`^\s*(verdict|completion):\s*(.+?)\s*$` or the whole contract parses to `None`
(pass, not an error). The raw value is `|`-split into a lowercased allow-list; a
whole-value or whole-element `<...>` sentinel means "any value" (`values =
None`); an element containing `<` (e.g. `blocked-<reason>`, the live
`pr-shepherd.md:10` contract) matches as a literal prefix via `_value_allowed`
(`zprof-guard.py:1356-1372`) — `blocked-worktree-locked` passes, `blocked-` alone
doesn't.

**Final text — the important deviation from a literal reading of the spec**
(ADR-0007 G5). `_final_text` (`zprof-guard.py:1398-1423`) resolves the reply text
in this order:

1. `payload["last_assistant_message"]`, if a string — no file I/O; the real
   production path.
2. Else `payload["agent_transcript_path"]`, if non-empty.
3. Else `payload["transcript_path"]`, but **only** when that path is itself a
   subagent transcript (`agent-<id>.jsonl` under a `subagents/` directory — the
   same test `resolve_role()` uses). On `SubagentStop`, `transcript_path` is
   normally the **main session's** transcript, not the subagent's; it is never
   read as if it were the subagent's own last turn.
4. Neither source usable → `None` → pass, no journal entry.

The last `type == "assistant"` record's text is extracted by
`_last_assistant_text` (`zprof-guard.py:1426-1459`): `content` as a plain string,
or joined `text` blocks from a list (`thinking`/`tool_use` blocks ignored). No
`assistant` record at all → `None` → pass, not an error.

**Fail-open asymmetry — two different "missing file" outcomes.** A
missing/unreadable **contract** file is caught by a local `except OSError` inside
`_load_role_contract` — silent pass, no journal event. A missing/unreadable or
corrupt **transcript** (`OSError`, `json.JSONDecodeError`, `UnicodeDecodeError`)
has **no** local `try/except` in `_last_assistant_text` — it propagates out of
`subagent_stop()` to `main()`'s single outer `except Exception`
(`zprof-guard.py:1567-1585`), which logs `decision: "error"`, `event:
"subagent-stop"`, and still exits 0. The contract is loaded *before* the final
text is resolved specifically so a role with no contract never opens a
transcript at all.

**Looping guard (`stop_hook_active`).** A first-time violation returns
`{"decision": "block", "reason": ...}` and logs `event: "subagent-stop", decision:
"block", rule: "return_format", detail: "key"|"value"` — which of the two checks
failed (`"key"` = wrong/missing/preamble'd opening key, `"value"` = value not in
the allow-list), never the response's first line itself (it may contain secrets;
that line only appears in the `reason` string sent back to the subagent). If the
*same* call still fails on the resulting retry (`payload["stop_hook_active"] is
True`), the mode does **not** block a second time — it logs `event:
"format_unfixed", decision: null` instead (not counted by P7's deny/block tally,
§7) and returns `None`, bounding the cost of a violation to one extra subagent
turn.

**Output shape differs from `pre-tool`'s `deny_output`.** There is no
`hookSpecificOutput`/`permissionDecision` wrapper — `subagent_stop` returns the
bare `{"decision": "block", "reason": "..."}` shape a `SubagentStop` hook expects;
`main()` writes it to stdout as-is, same as `pre-tool`'s output.

See [ADR-0007: guard — валидатор `return_format` на `SubagentStop`](../adr/0007-guard-subagent-stop-validator.md)
for the full G1–G10 decision record (exact journal-event field mapping, frontmatter
edge cases, the `blocked-<reason>` prefix rationale, and deviations from the
original plan/AC text) — not duplicated here.

### Deny output and telemetry

A firing rule produces the exact §5.7 payload via `deny_output`
(`zprof-guard.py:268-280`):

```json
{"hookSpecificOutput": {"hookEventName": "PreToolUse", "permissionDecision": "deny",
  "permissionDecisionReason": "zprof guard [<id>]: <reason>. Не обходи: верни `verdict: blocked`, reason: <id>."}}
```

`verdict: blocked` in the reason string is intentionally compatible with the
`base_enum` in `verdicts.yaml` ([ADR-0003](../adr/0003-verdicts-registry.md)), so a
denied agent's own return still validates against the schema. Every `deny`/`error`
event is appended as one JSON line to `.agentlog/guard-events.jsonl` under `flock` +
`fsync` (`write_event`, `zprof-guard.py:641-652`) — a separate file and a separate lock
(`.agentlog/.guard.lock`) from the telemetry collector's `.agentlog/.lock` and
`.agentlog/tool-events.jsonl`, because guard runs on every guarded tool call while the
collector holds its lock for whole-session operations (ADR-0004 D9). `context_error`
(#24) reuses the same file/lock/`_safe_write_event` path, with `decision: null`
instead of `"deny"`/`"error"`; `allow_unverified` (#25) reuses it too, with
`decision: "allow_unverified"` and `event: "pre-tool"` (it's a gate *decision*,
not a diagnostic — see previous section). `subagent-stop`/`format_unfixed` (#26)
reuse the same file/lock/`_safe_write_event` path too, but are written by
`subagent_stop()` rather than `pre_tool()` and carry `tool: null`, `target: null`,
`input_hash: null` — there is no tool call to describe (see previous section,
ADR-0007 G1).

### Score and stats integration (#27, ADR-0008)

`.agentlog/guard-events.jsonl` (this file, written by `write_event`) is a data
contract, not a Go import: the readers live in `cli/internal/score/`, `guard`
itself never calls into Go. `score.GuardEvent`
(`cli/internal/score/reader.go:75-86`) mirrors the ten §7 keys as plain
strings; `score.ReadGuardEvents` (`reader.go:96-120`) parses the file —
missing file → `(nil, nil)`, a malformed line is skipped, **no** dedup (rows
carry no `seq` and the guard never rewrites the file, unlike the collector's
`tool-events.jsonl`). Unlike `ReadToolEvents`, a row with `dispatch_id: null`
(JSON `null` → Go `""`) is kept, not skipped — it's the legitimate shape for
`role: "main"`/`"unknown"` events and the only entry point into the temporal
fallback below. `run_id` (always `null`) and `detail` are read from the JSON
but dropped — neither P7 nor `zprof stats` uses them.

**Matching events to runs — `score.AttachGuardEvents`**
(`reader.go:247-282`), called from `cmd/score.go:82-87` right after
`score.BuildRuns`, without changing `BuildRuns`'s signature:

- `dispatch_id != ""` — matched by `rawID()` (`reader.go:221-226`, the
  substring after the last `:`) against every dispatch in every run. Guard
  writes the **raw** `toolUseId` (see invariant above); `dispatches.jsonl`/
  `tool-events.jsonl` carry the collector's composite
  `claude-code:<session_id>:<toolUseId>` — `rawID` strips both down to the
  same key. No match (dispatch outside any complete run) drops the event,
  same as `ToolEvent` handling in `BuildRuns`; there is no temporal fallback
  for these rows.
- `dispatch_id == ""` (`main`/`unknown`) — the only temporal fallback: `ts` is
  parsed as RFC3339Nano (unparsable → dropped), then matched against the
  first run (runs are already time-sorted) whose window
  `[Root.Timestamp − DurationMs, Root.Timestamp]` contains it (inclusive) and
  whose `session_id` agrees when both the event and the run's root have one
  set. A run with a zero `Root.Timestamp` or `DurationMs <= 0` is never a
  candidate.

**P7 — `computeP7`** (`cli/internal/score/metrics.go:261-268`) adds a
**second, independent loop** over `run.GuardEvents` after the existing
contract-fields loop: each row with `Decision` `"deny"` or `"block"` adds one
violation (`allow_unverified`/`error`/`""` — i.e. `format_unfixed`/
`role_unresolved`/`context_error` — do not count), attributed to the event's
own `Role`. As the invariant above states, `cfg.ExemptRoles` does not reach
this loop. When `m.guard > 0`, `computeP7`'s `Detail` string gets a
`" (guard: %d deny)"` suffix (label is literally `deny` even for `block`
rows, per spec §7) and the count is copied into a new field,
`Penalty.GuardDenies` (`metrics.go:28-31`, `json:"guard_denies,omitempty"`) —
zero on every other penalty, omitted from `scores.jsonl` when zero, so old
rows and runs with no guard deny are byte-for-byte unchanged.
`Config.WeightsHash()` doesn't change (P7's weight/saturation are untouched),
so `--all-missing` does **not** retroactively re-score already-scored runs
just because guard events later appear for them.

**Card rendering — `RenderCard`** (`cli/internal/score/render.go:31-38`): a
penalty with `GuardDenies > 0` is always appended to the printed findings even
when it didn't make the top-3-by-points cut — otherwise the `(guard: N deny)`
suffix could be computed but never shown on a card. `GuardDenies == 0` leaves
the card identical to pre-#27 output.

**`zprof stats`** (`cli/internal/cmd/stats.go:91-97, 114-153`) reads the same
`guard-events.jsonl` per `<agentlog-dir>` argument after writing
`report.html`/`report.json` (neither of which changes — this is stderr-only),
applies the same `--session`/`--role` filters already applied to dispatches,
counts `deny`/`block` rows by `Rule` (same filter as P7), and — if any rows
matched — prints one line to stderr: `guard: top rules: <rule>×<n> ...` (top
5, count desc then rule name asc). Zero matches (including a missing file)
prints nothing.

Both integrations are exercised by
`cli/internal/score/{reader,metrics,render}_test.go` and
`cli/internal/cmd/{score,stats}_test.go` — table-driven cases for the raw/
composite `dispatch_id` match, the temporal-fallback window edges,
`ExemptRoles` non-exemption, and the stats filter/format — plus the
`--session`/`--role` combinations; `go test ./...` → 373 passed (verified
2026-09-28, `feat/guard-events-score-27`).

See [ADR-0008: guard-события в `zprof score` (P7) и `zprof stats`](../adr/0008-guard-events-score-integration.md)
for the full H1–H8 decision record (why the match key is the raw `toolUseId`
and not a `runOf`-style index, why the temporal window is
`[end − duration, end]` rather than "largest timestamp ≤ ts", and the exact
golden-fixture numbers) — not duplicated here.

### Deployment (`zprof apply`, #28, ADR-0009)

All of the Go-side deploy logic lives in `cli/internal/apply/guard.go`
(`deployGuard` and its helpers), with three small, narrowly-scoped changes
elsewhere: `cli/internal/apply/settings.go` (the two guard hook specs and
`permissions.deny` upsert/subtract), `cli/internal/manifest/project.go`
(`GuardConfig`, the project layer), and `cli/internal/overlay/loader.go`
(`Base.GuardScript`/`GuardSchema`, `Overlay.GuardSchema`). This is the code
that turns `guard.yaml` (implementer-authored source, above) into
`.claude/guard.json` (what `zprof-guard.py` actually reads) — see
[Apply](apply.md)'s "DeployTelemetry and `--telemetry-only`" section for how
it's wired into `zprof apply`/`zprof sync`/`--telemetry-only`; this section
covers what the merge and render actually do.

**Three-layer merge, always computed.** `deployGuard` parses `Base.GuardSchema`
(required `version: 1`), then each applied overlay's `guard.yaml` if it ships
one (`version` optional), then folds in the project's `.zprof.yaml` `guard:`
section (`manifest.GuardConfig`) — in that order, base → overlays → project
(`guard.go:206-251, 498-521`). The four known string lists
(`readonly_roles`, `merge_roles`, `allow_write_prefixes`, `permissions_deny`)
concatenate across base/overlay layers with first-occurrence-order dedup;
`exempt_roles` unions per rule id; `rules` merge by `id` — a later layer's
rule with an existing id replaces it **in place**, a new id is appended
(`mergeRules`, `guard.go:288-311`). Any top-level key this Go binary doesn't
know about round-trips verbatim through `Extra` (later layer wins) — a newer
profile's new `guard.yaml` key doesn't break an older `zprof` binary
(`guard.go:39-53, 313-326`). The project layer is different from an overlay:
`readonly_roles`/`allow_write_outside` only *append*, `exempt_roles` only
*unions*, but `merge_roles` — if the project sets a non-empty list —
**replaces** the merged result wholesale rather than extending it (an empty
project list must never be read as "nobody merges"); a non-empty
`extra_deny_bash` produces one synthetic `id: "extra_deny"` rule
(`tools: [Bash]`, `match` = the deduped list, a fixed `reason` string with no
"Не обходи…" tail — the script appends that tail exactly once) merged in by
the same by-id rule (`guard.go:224-251`, `extraDenyReason` constant).
**The merge runs unconditionally, even when the project's guard is
disabled** — a broken overlay `guard.yaml` fails `zprof apply` regardless of
`guard.enabled`.

**`$ref` resolution, one pass, after merge.** `resolveGuardRefs` walks every
rule's `match`/`roles`/`not_roles` once, over the already-merged doc, and
substitutes a bare `"$readonly_roles"`/`"$merge_roles"`/
`"$mutating_bash_patterns"` value with a copy of the resolved list
(`guard.go:377-434`). `$mutating_bash_patterns` is read lazily from
`Base.TelemetrySchema` (`profiles/base/telemetry.yaml`'s
`mutating_bash_patterns` key) — parsed only if some rule actually references
it, so an older base without that key doesn't break a `guard.yaml` that never
uses the reference (`guard.go:336-375`). Three things are apply-time errors
(fail-closed, unlike the script's own runtime fail-open): an unknown `$name`;
a `$name` inside a list (a splice the format doesn't support); and a
`roles`/`not_roles` reference that resolves to an empty list (an empty
`$merge_roles` must never silently mean "merge is unrestricted for everyone"
— that's exactly the silent-disable the script's own `ValueError` on a raw
`$ref` guards against at runtime, closed here at apply time instead).

**Render.** `guardDoc.render()` produces `guard.json`'s exact shape —
`version: 1` plus the known keys, `rules`, and `Extra` — with nil
slices/maps normalized to `[]`/`{}` (never JSON `null`, which
`load_config()` would reject) and `encoding/json`'s sorted map keys giving a
byte-stable result across repeated applies with unchanged input
(`guard.go:436-481`).

**`enabled` gates deployment, not the merge.** `guard.enabled: false` in
`.zprof.yaml` still runs the full merge/resolve above (so profile bugs are
still caught), but skips writing `.claude/zprof-guard.py`/`guard.json` and
instead *removes* both guard hooks and subtracts the current
`permissions_deny` list from `settings.local.json` — computed from **this**
merge, not from whatever `guard.json` is sitting on disk, since the file is
an output artifact, not an ownership registry (`ensureGuardSettings`,
`settings.go:127-167`). Files from a previous enabled apply are left as-is,
neither rewritten nor deleted.

**Hooks.** Two entries, defined in `settings.go`'s `guardHooks`: `PreToolUse`
carries `matcher: "Bash|Edit|Write|MultiEdit|NotebookEdit"` (`TOOLS_GUARDED`)
and runs `zprof-guard.py pre-tool`; `SubagentStop` has no matcher and runs
`zprof-guard.py subagent-stop` — both wrapped in the same
`test -x ... && ... || true` fail-open shell guard the collector's hooks use
(`settings.go:23-54`). `zprofHookIndex` matches an existing entry by
substring of the script name, so the guard's `SubagentStop` entry and the
collector's pre-existing `SubagentStop` entry (`zprof-collect.py`) coexist on
the same event and upgrade independently when either script's command or
matcher changes (`settings.go:216-234`).

**Wiring.** `deployGuard(projectDir, base, GuardLayers{Overlays, Project})` is
called from inside `DeployTelemetry` (`collector.go`), never duplicated —
`zprof apply`'s `engine.go` and `zprof apply --telemetry-only`
(`cmd/apply.go`) both go through the one function, so the two paths cannot
deploy guard differently. `--telemetry-only` builds `GuardLayers` from an
existing `.zprof.yaml` if present (loading each of its overlays' `guard.yaml`
too), or falls back to base-only defaults (guard enabled, no overlay/project
layer) when there's no project manifest yet. `ProjectManifest.CarryOverFrom`
copies a previously saved `Guard` forward when the fresh manifest being built
doesn't set one explicitly — without this, every `zprof apply <overlay>`
would silently drop the project's `guard:` section and redeploy guard at its
default (enabled) (`manifest/project.go:168-193`). `zprof sync` needs no
special case: it loads the full saved manifest, `Guard` included.

Test coverage: `cli/internal/apply/guard_test.go` (parse/merge/`$ref`-resolve/
render unit tests, AC1–AC9) and `guard_e2e_test.go` (real `zprof-guard.py`
subprocess denying `git push --force` after `deployGuard`, AC10/AC11) —
`go test ./...` → 429 passed (verified 2026-09-28,
`feat/guard-apply-deploy-28`). See
[ADR-0009: `zprof apply` деплоит guard](../adr/0009-guard-apply-deploy.md)
for the full decision record (why rules stay raw maps, why `$ref` resolution
is a single post-merge pass, the exact `extra_deny` reason text, and why
`enabled: false` leaves prior files on disk) — not duplicated here.

### Doctor checks (`zprof doctor`, #29)

`zprof doctor` (`cli/internal/doctor/diagnostics.go`) diagnoses guard's
*deployed* state — the same artifacts `deployGuard` writes above — entirely
read-only, five checks past design §10, appended at the end of `Diagnose()`'s
existing 18-check list (`diagnostics.go:122-123`):

| Check | Gate | Level | Reports |
|---|---|---|---|
| `checkGuardHooks` (`:985-1021`) | `.claude/zprof-guard.py` exists | warn | `settings.local.json` is missing either the `PreToolUse` or `SubagentStop` hook entry running `zprof-guard.py` (one unified message for "file missing" and "hooks incomplete" — unlike `checkTelemetryHooks`'s two separate messages, per AC1) |
| `checkGuardConfig` (`:1036-1062`) | none — always runs | warn | `.claude/guard.json` missing, unparseable, or its `rules` list is empty |
| `checkPermissionsDeny` (`:1083-1129`) | self-gates on a readable, non-empty `guard.json.permissions_deny` | warn | `settings.local.json`'s `permissions.deny` is missing one or more of those values, lists them by name |
| `checkGuardDeployment` (`:1137-1146`) | `proj.Guard != nil && !proj.Guard.IsEnabled()` | info (gate) / aggregates the three above | when guard is disabled: `"guard disabled by project config"`, and the three checks above never run at all — a project that opted out isn't nagged about a deployment it declined |
| `checkRoleResolution` (`:1247-1273`) | none — ignores `guard.enabled` | info | no `*/subagents/*.meta.json` with an `agentType` key found yet under `~/.claude/projects/<slug>/`, `slug` = project path with `/`→`-` |

**`checkGuardConfig` deliberately has no `.claude/zprof-guard.py` gate**,
unlike `checkGuardHooks` — an asymmetry from design §10's table, not an
oversight: `deployGuard` only ever writes the script and `guard.json`
together (`apply/guard.go:498-501`), so "script absent, `guard.json` present"
doesn't happen via a normal `apply`. **`checkPermissionsDeny` self-gates
instead of taking a gate parameter** — an unreadable/malformed `guard.json`
is already reported by `checkGuardConfig`, so it stays silent (`nil`) rather
than duplicate that warning; an empty `permissions_deny` list also yields
`nil` (nothing to compare against). **`checkRoleResolution`'s `HOME` lookup
uses `os.Getenv("HOME")`, not `os.UserHomeDir()`** — the deliberate deviation
from `internal/eval.LocateSession`'s otherwise-identical slug algorithm, so
tests can override it with `t.Setenv` (design §10, `plan-issue-29.md` item 8).

`checkGuardHooks` reuses `hookArrayHasScript` (renamed from
`hookArrayHasCollector` for this issue, `diagnostics.go:954-969`) — the same
helper `checkTelemetryHooks` uses for `zprof-collect.py` — parameterized on
the script substring instead of hardcoding it, a pure signature refactor that
doesn't change `checkTelemetryHooks`'s own behavior.

### Telemetry-only diagnostics (no `.zprof.yaml`, #64)

Before #64, `Diagnose()` (`cli/internal/doctor/diagnostics.go:98-135`) had one
failure mode for any `manifest.LoadProject` error, physically-missing file or
malformed YAML alike: a single `LevelError` Issue and nothing else. That broke
`zprof doctor` on zprof's own repo checkout — telemetry/guard are deployed
here via `zprof apply --telemetry-only` (ADR-0001, #22) but no `.zprof.yaml`
is ever written by a `--telemetry-only` deployment (unless one already
existed from a prior full `apply`), so every run reported a false top-level
error instead of the report below.

`Diagnose` now branches on the load error (`diagnostics.go:98-110`):

- **File physically absent** (`errors.Is(err, fs.ErrNotExist)`) **and**
  `telemetryDeployed(projectDir)` finds `.claude/zprof-collect.py` or
  `.claude/zprof-guard.py` on disk (`diagnostics.go:141-148`, a plain
  `os.Stat`, checked either-or) → `diagnoseTelemetryOnly` runs instead of the
  error path (`diagnostics.go:159-174`).
- **File physically absent, nothing deployed** → unchanged: the single
  `LevelError` "failed to parse .zprof.yaml" Issue.
- **File present but broken** (a YAML parse error — not `fs.ErrNotExist`) →
  unchanged regardless of whether telemetry is deployed: still the single
  `LevelError`. A malformed-but-present manifest is never silently treated as
  "no manifest" — see `TestDiagnoseTelemetryOnlyMode`'s "broken manifest with
  telemetry deployed stays an error" case above.

`diagnoseTelemetryOnly` (`diagnostics.go:159-174`) opens with one `LevelInfo`
Issue — `"no .zprof.yaml — manifest checks skipped (telemetry-only
project)"` — then runs exactly the 9 of `Diagnose`'s ~21 checks that need no
manifest at all: `checkRunsGitignored`, `checkRunLogs`,
`checkAgentlogGitignored`, `checkAgentlogNotTracked`, `checkTelemetryHooks`,
`checkPython3Available`, `checkAgentlogCleanVulnerability`,
`checkGuardDeployment`, `checkRoleResolution` — the same nine, in the same
order, as their position in the full list (`diagnostics.go:113-133`). Every
check gated on `proj` itself — `checkOverlayCount`, `checkOverlaysExist`,
`checkAgentFrontmatter`, `checkAgentVerdicts`, `checkAgentModels`,
`checkManagedMarkers`, `checkTaskRunner`, `checkRouteAgentsExist`,
`checkStopLists`, `checkOrphanAgents`, `checkAuditConfig`,
`checkRunnerBudget` — is skipped outright, not fed a synthetic manifest:
there is no overlay selection or agent roster to validate without one.
`checkGuardDeployment` is the deliberate exception, not a tenth skip — see
the matching Key invariant above for why it takes a zero-value
`&manifest.ProjectManifest{}` instead.

This is doctor's own control-flow change, not a guard-specific one — it
governs every manifest-dependent check in `Diagnose()`, not only the guard
checks documented above — but it's recorded here rather than in a
freestanding `doctor.md` (still P2, not yet written, per `PLAN.md`) because
`checkGuardDeployment`'s zero-value-manifest handling is the one piece of
`diagnoseTelemetryOnly` that's genuinely guard-specific, and this file is
already `zprof doctor`'s documented home (see "Doctor checks" above and
[Apply](apply.md)'s "DeployTelemetry and `--telemetry-only`" section, which
this feature diagnoses the deployed shape of).

### Doctrine and prompt contracts (#30)

Issue #30 is prompt/config-only: it does not touch `zprof-guard.py`,
`guard.yaml`, or the Go merge/render logic in `cli/internal/apply/guard.go` —
it adds the human/agent-facing text that tells an agent what the hook already
enforces, plus two overlay `guard.yaml` files that exercise the base→overlay
merge (`mergeRules`, "Deployment" above) with real content instead of only
the force-push fixture `TestE2E_GuardDeploysAndEnforcesForcePush` used before.

**Doctrine string.** The same one-line sentence — `` "Guard: стоп-лист, merge
и формат ответа проверяет хук; на deny не ищи обход — верни `verdict:
blocked` с reason" `` — is duplicated in two prompt-layer sources:
`profiles/base/manifest.yaml`'s `guard_doctrine` key (`manifest.yaml:11`) and
`profiles/base/claude-block-base.md`'s `### Guard` subsection
(`claude-block-base.md:24-26`), placed between `### Изоляция` and `### Свои
правила`. `guard_doctrine` is not `stop_list` — it is a new top-level key
that `manifest.OverlayManifest`'s loose `yaml.Unmarshal` ignores safely (no Go
change needed), and unlike `stop_list` it is never rendered into `##
Stop list` by `buildStopListBlock` (`cli/internal/apply/tables.go:166`), so
`checkStopLists` (`zprof doctor`) never inspects it. This repo's own
`CLAUDE.md` was regenerated from `claude-block-base.md` (no `.zprof.yaml`
here, so `zprof apply` was run against a scratch project and the resulting
`<!-- zprof:begin doctrine --> … <!-- zprof:end doctrine -->` block was
copied in by hand, same precedent as #15) — the `### Guard` subsection now
sits at `CLAUDE.md:25-27`, inside `## Doctrine`, leaving `## Consilium`/`##
Executing`/`## Stop list` untouched.

**Agent contracts.** Two existing prompt sections got one new line each,
telling an agent that a guard `deny` is the same category of event as the
stop-list — don't rephrase the command to route around it, report it instead:

- `profiles/base/agents/pr-shepherd.md`, new rule `0.8` in `# 0. HARD RULES`
  (`pr-shepherd.md:40`): *"Guard `deny` = стоп-лист. Если команда этого
  invocation получает `deny` от zprof guard-хука, не ищи обход и не
  перефразируй команду, чтобы обойти правило — верни `verdict:
  blocked-guard` с `question`, описывающим что заблокировано и почему."*
- `profiles/base/agents/task-runner.md`, appended to the existing `##
  Стоп-лист` section (`task-runner.md:399-400`): *"`deny` от zprof
  guard-хука на любой команде субагента — тот же случай, что и стоп-лист: не
  ищи обход, верни `verdict: blocked` с reason."*

Both deploy copies (`.claude/agents/pr-shepherd.md`,
`.claude/agents/task-runner.md`) carry the identical diff and stay
byte-for-byte in sync with their `profiles/base/agents/` source, per this
repo's own convention.

**Overlay `guard.yaml` files.** Two new files, both merged in on top of
`profiles/base/guard.yaml` by the #28/ADR-0009 three-layer merge (no `version`
key — required only for the base layer):

- `profiles/overlays/ios-swift/guard.yaml` — `exempt_roles: {publish:
  [testflight-shipper]}`, exempting the overlay's TestFlight/App-Store
  publishing role from the base `publish` stop-list rule (`xcrun altool` /
  `fastlane pilot|deliver` would otherwise be denied for every role).
- `profiles/overlays/backend-python/guard.yaml` — a new rule `pip_install`
  (`tools: [Bash]`, `match: ['\b(pip|pip3)\s+install\b', '\bpoetry\s+add\b']`)
  denying `pip install`/`pip3 install`/`poetry add` with a reason pointing at
  `uv add <pkg>` (this overlay's lockfile is `uv.lock`, not
  `requirements.txt`/`poetry.lock`).

Both are exercised as regression fixtures for the already-implemented merge,
not as new merge behavior — see Test coverage above for the specific pytest
and Go E2E cases (`TestE2E_GuardDeploysAndEnforcesForcePush`'s new
`exempt_roles.publish` assertion, and the new
`TestE2E_GuardDeploysBackendPythonPipInstallRule`).

### See also

- [Collector](collector.md) — sibling hook in `profiles/base/`; `guard`'s `_input_hash`
  mirrors (does not import) `collector`'s, since the two scripts deploy independently
- [Apply](apply.md) — deploys guard since #28: `guard.yaml` → `guard.json`
  rendering and the `PreToolUse`/`SubagentStop` hook entries, see
  "Deployment" above
- `cli/internal/score/` — reads `guard-events.jsonl` (§7 format), no wiki file
  yet (P1 in `PLAN.md`); see "Score and stats integration" above
- `cli/internal/doctor/` — diagnoses guard's deployed state read-only (§10),
  no wiki file yet (P2 in `PLAN.md`); see "Doctor checks" above, and
  "Telemetry-only diagnostics" above for the `.zprof.yaml`-absent branch
  (#64) that reaches `checkGuardDeployment` with a zero-value manifest
- [ADR-0001: config_hash resolution and telemetry-only redeploy](../adr/0001-collector-config-hash-and-telemetry-redeploy.md)
- [ADR-0004: `zprof-guard.py pre-tool` — frame, `guard.yaml`/`guard.json` format, stop-list §5.1, read-only roles](../adr/0004-zprof-guard-pre-tool-frame.md)
- [ADR-0005: guard — context-evaluators `head_on_remote`, `linked_worktree`, `write_outside_repo`, `branch_pr_merged`](../adr/0005-guard-context-evaluators.md)
- [ADR-0006: guard — merge-гейт (`merge_preflight`) и PR-гейт (`pr_create_gate`), событие `allow_unverified`](../adr/0006-guard-merge-pr-gate.md)
- [ADR-0007: guard — валидатор `return_format` на `SubagentStop`](../adr/0007-guard-subagent-stop-validator.md)
- [ADR-0008: guard-события в `zprof score` (P7) и `zprof stats`](../adr/0008-guard-events-score-integration.md)
- [ADR-0009: `zprof apply` деплоит guard — `guard.json`, хуки с `matcher`, `permissions.deny`](../adr/0009-guard-apply-deploy.md)
- `docs/superpowers/specs/2026-09-27-guard-hooks-design.md` — full guard design (§2
  decisions, §5 rule tables, §6 subagent-stop validator, §7 telemetry/score
  integration, §8 apply deployment, §12 phase-2 deferred work)
