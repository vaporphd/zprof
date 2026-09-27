## AI Context

Component: collector
Path: `profiles/base/zprof-collect.py`
Status: implemented
Depends: [profiles-base]
Dependants: [apply, score, stats]
Exports: [Collector, State, main, _agent_file_index, _resolve_agent_file, _agent_config_hash]
Key invariants:
  - Runs as a Claude Code hook (SubagentStop / Stop / SessionStart), always `exit(0)`;
    errors are logged, never raised out of `main()` (`profiles/base/zprof-collect.py:28`,
    `220-229`).
  - `config_hash` is snapshotted at SubagentStop time and stored on the pointer, because
    the agent file can be rewritten (e.g. by a later `zprof apply`) before the session's
    Stop — using the live file at Stop would misattribute the contract version
    (`zprof-collect.py:236-250`).
  - `agent_type` → agent file resolution is by frontmatter `name:` (falling back to file
    stem), never by concatenating `agent_type` into a path — no path traversal, and
    namespaced/gate files resolve correctly (`zprof-collect.py:970-1023`).
  - Ambiguous resolution (>1 file sharing a `name:`) yields `config_hash = None`, not a
    guess — a wrong hash is worse than a missing one (`zprof-collect.py:1006-1023`).
  - `state.json` is written atomically (temp file + `fsync` + `rename`) and access to
    `.agentlog/` is serialized with an `flock`-based lock (`AgentlogLock`,
    `zprof-collect.py:136-170`, `202-209`).
Spec refs: docs/adr/0001-collector-config-hash-and-telemetry-redeploy.md
Test coverage: pytest, `profiles/base/tests/` — `test_config_hash.py` (35 tests: frontmatter
  parsing, name/stem indexing, resolution incl. ambiguity/symlink-escape/backup-file
  skip, hash stability/invalidation on body or `model:` edits, SubagentStop pointer
  snapshot + consumption, fallback source labeling), plus `test_e2e.py`,
  `test_normalization.py`, `test_subagent_transcripts.py`, `test_nested_dispatches.py`
  for the surrounding dispatch pipeline. Verified green: `python3 -m pytest
  profiles/base/tests/test_config_hash.py -q` → 35 passed (run 2026-09-27).

---

## Collector — Telemetry Hook Script

`zprof-collect.py` is a single stdlib-only Python script deployed by `zprof apply` (see
[apply.md](apply.md)) to `.claude/zprof-collect.py` in a project. Claude Code invokes it as
a hook — `SubagentStop`, `Stop`, `SessionStart` — and it turns raw session/subagent
transcripts into the structured telemetry that `.agentlog/` holds (`dispatches.jsonl`,
`tool-events.jsonl`, `state.json`, gzip'd transcript copies).

`main()` (`zprof-collect.py:28`) dispatches on the hook mode to `Collector.run()`
(`:212, 220-229`), which calls one of `_handle_subagent_stop`, `_handle_stop`,
`_handle_session_start`. Every path is designed to never raise: on any failure it logs to
`.agentlog/` and still `exit(0)`s, so a collector bug can never block or fail an agent's
turn.

### config_hash

`config_hash` answers "which exact version of the agent's `.md` contract produced this
dispatch". It matters because `zprof apply` can rewrite an agent file at any time (a new
overlay version, a `model:` override), and telemetry needs to know which contract a given
dispatch actually ran under, not just which one is on disk *now*.

Resolution has three pieces:

1. **`_agent_file_index(cwd)`** (`:970-1003`) — walks `.claude/agents/**/*.md`,
   building two dicts: by frontmatter `name:` and by file stem, skipping symlinks that
   escape the root and zprof's own `*.bak-*` files.
2. **`_resolve_agent_file(cwd, agent_type)`** (`:1006-1023`) — looks up `agent_type` (the
   string Claude Code reports for a subagent) in the name index, falling back to the stem
   index. Exactly one match → that file. Zero matches → `(None, 0)` (builtin agents like
   `Explore`, plugin agents, user-level `~/.claude/agents`). More than one match →
   `(None, N)`, and the caller records `N` as `config_hash_candidates` rather than
   guessing.
3. **`_agent_config_hash(cwd, agent_type)`** (`:1026-1042`) — hashes the resolved file's
   raw bytes with `sha256(...).hexdigest()[:12]`. No normalization, so a `model:` override
   (part of the agent's effective contract) also changes the hash.

**When it's computed.** `_handle_subagent_stop` (`:231-250`) computes the hash immediately
and stores it on the pointer for that `agent_id`, alongside `config_hash_candidates` when
ambiguous. Later, in the transcript-collection pass (`_collect_subagent_transcripts`,
`:1173-1188`), the pointer is popped (consumed) and its `config_hash` used, with
`ext.config_hash_source = "subagent-stop"`. If no pointer exists — e.g. `SessionStart`
recovering a dead session — the hash is resolved fresh from the file currently on disk,
with `ext.config_hash_source = "collect"`. Both `config_hash` and `verdict` are top-level
fields in `profiles/base/telemetry.yaml` (`:26`, `:31`); `config_hash_source` and
`config_hash_ambiguous` live under the schema's open `ext` map.

### verdict

The first line starting with `verdict:` in an agent's return text is parsed as the
role-level judgment (`zprof-collect.py:1562-1572`, `_class_a_checks`) and, if the raw
dispatch didn't already carry a `verdict`, filled in via `verdict_value`
(`:1707-1708`). This logic was already correct before this branch; what changed was fixing
a *stale deployed copy* of the script in zprof's own `.claude/` that predated it — see
[apply.md](apply.md) and the ADR for the redeploy story.

### State and recovery

`State` (`:174-209`) tracks per-session watermarks (`main_log_offset`,
`agents_done`, …) and the SubagentStop-to-Stop pointer map, persisted to
`.agentlog/state.json` via atomic temp-file-then-`rename`, with `os.fsync` before the
rename. `AgentlogLock` (`:136-170`) wraps all `.agentlog/` access in a non-blocking
`flock`, so concurrent hook invocations don't race on `state.json` or the JSONL logs.
Pointers are consumed (popped) once used by transcript collection, so they don't
accumulate unboundedly across sessions.

### See also

- [Apply](apply.md) — deploys this script into a project and upserts the hooks that invoke it
- [ADR-0001: config_hash resolution and telemetry-only redeploy](../adr/0001-collector-config-hash-and-telemetry-redeploy.md)
- `score` (`cli/internal/score/`, doc not yet written — see `PLAN.md`) — consumes
  `dispatches.jsonl` produced here
