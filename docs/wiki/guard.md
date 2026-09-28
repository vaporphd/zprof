## AI Context

Component: guard
Path: `profiles/base/zprof-guard.py` (rules: `profiles/base/guard.yaml`; tests:
  `profiles/base/tests/test_guard*.py`)
Status: in-progress
Depends: [profiles-base]
Dependants: []
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
    `$mutating_bash_patterns`) is `zprof apply`'s job, not yet implemented (#28)
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
Spec refs: `docs/superpowers/specs/2026-09-27-guard-hooks-design.md` §4, §5, §6, §7,
  §8.1, §11, §13
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
  `feat/guard-subagent-stop-26`@`177539f`).

---

## Guard — Deterministic PreToolUse & SubagentStop Enforcement

`zprof-guard.py` is a Claude Code hook script with two modes, `pre-tool` and
`subagent-stop`. In `pre-tool` mode it reads one tool-call payload from stdin,
decides `deny` or silence (never `allow`, never `ask`), and logs the decision — it
replaces prompt-only policy ("don't force-push", "only pr-shepherd merges") with a
deterministic check that runs before the tool executes. In `subagent-stop` mode it
reads one `SubagentStop` payload and decides `block` or silence, checking that a
subagent's final reply opens with the `verdict:`/`completion:` line its own
`return_format` contract promises. Issues #23–#26 are the first four of a
six-issue milestone (#23–#28); this doc describes what's shipped so far — §5.1
stop-list, read-only roles, and the merge gate from #23; §5.2/§5.3 context rules
from #24; §5.5 merge preflight and §5.6 PR-create gate from #25; §6 the
subagent-stop `return_format` validator from #26 — not the full design in the spec.

**Not yet deployed anywhere.** No project's `.claude/` directory runs this hook today —
that wiring (`zprof apply` writing `.claude/zprof-guard.py`, rendering
`guard.yaml` → `.claude/guard.json`, upserting the `PreToolUse`/`SubagentStop` hook
entries in `settings.local.json`) is issue #28. This is true for **both** modes:
`subagent-stop` doesn't depend on `guard.json` the way `pre-tool` does (see below),
but nothing today invokes `zprof-guard.py subagent-stop` from a real `SubagentStop`
hook either. Until #28 lands, `guard.md` describes source-only behavior, exercised
by `test_guard.py`/`test_guard_subagent_stop.py` invoking the script directly.

### What's active in `guard.yaml` as of #23–#25

`guard.yaml` (`profiles/base/guard.yaml:1-139`) is the **full** catalog of rules
the eventual design calls for. As of #25 every rule in it is active — the last two
data-only placeholders (`merge_preflight`, `pr_create_gate`) got their evaluators
registered by ADR-0006. The subagent-stop `return_format` validator from #26 is
**not** a `guard.yaml` rule at all — it's a separate `main()` mode (see
"Subagent-stop validator" below) — so it never appears in this table. What
remains outside both is the *deployment* of the hook itself, #28:

| Group (spec §) | Rules | Status |
|---|---|---|
| §5.1 stop-list, no context | `force_push`, `admin_merge`, `no_verify_commit`, `no_verify_other`, `branch_force_delete`, `remote_ref_delete`, `tag_delete`, `publish`, `curl_pipe_sh` | **active** (#23) — plain `tools` + `match` regex, no `context`; `remote_ref_delete` additionally carries `not_roles: [pr-shepherd]` since #24 |
| §5.4 read-only roles | `readonly_mutation` | **active** (#23) — `roles: $readonly_roles` + `match: $mutating_bash_patterns` |
| §5.5 merge gate | `merge_role` | **active** (#23) — `not_roles: $merge_roles`, no `context` needed (the rule engine is generic; see ADR-0004 D3 "conscious deviation") |
| §5.2 contextual stop-list | `rebase_published`, `amend_published`, `stash_in_worktree`, `remote_ref_delete_unmerged` | **active** (#24) — `context: head_on_remote`/`linked_worktree`/`branch_pr_merged` now registered ([#24](../../plan-2.md), ADR-0005) |
| §5.3 write outside repo | `write_outside_repo` | **active** (#24) — `context: write_outside_repo` now registered ([#24](../../plan-2.md), ADR-0005) |
| §5.5 merge preflight | `merge_preflight` | **active** (#25) — `context: merge_preflight` now registered (ADR-0006) |
| §5.6 PR-create gate | `pr_create_gate` | **active** (#25) — `context: pr_create_gate` now registered, no `roles`/`not_roles` (applies to every role) (ADR-0006) |

Integrating guard events into `zprof score` is [#27](../../plan-2.md); actually
deploying the hook into `.claude/` is [#28](../../plan-2.md) — until #28 lands,
everything in this file is inert in every real project, including this one.

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

### See also

- [Collector](collector.md) — sibling hook in `profiles/base/`; `guard`'s `_input_hash`
  mirrors (does not import) `collector`'s, since the two scripts deploy independently
- [Apply](apply.md) — does **not** deploy guard yet; deployment, `guard.yaml` →
  `guard.json` rendering, and the `PreToolUse` hook entry are #28
- [ADR-0004: `zprof-guard.py pre-tool` — frame, `guard.yaml`/`guard.json` format, stop-list §5.1, read-only roles](../adr/0004-zprof-guard-pre-tool-frame.md)
- [ADR-0005: guard — context-evaluators `head_on_remote`, `linked_worktree`, `write_outside_repo`, `branch_pr_merged`](../adr/0005-guard-context-evaluators.md)
- [ADR-0006: guard — merge-гейт (`merge_preflight`) и PR-гейт (`pr_create_gate`), событие `allow_unverified`](../adr/0006-guard-merge-pr-gate.md)
- [ADR-0007: guard — валидатор `return_format` на `SubagentStop`](../adr/0007-guard-subagent-stop-validator.md)
- `docs/superpowers/specs/2026-09-27-guard-hooks-design.md` — full guard design (§2
  decisions, §5 rule tables, §6 subagent-stop validator, §12 phase-2 deferred work)
