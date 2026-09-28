#!/usr/bin/env python3
"""zprof guard — deterministic PreToolUse enforcement, runs as a Claude Code hook.

Usage: zprof-guard.py <mode>
Modes: pre-tool | subagent-stop (subagent-stop is a no-op in this issue, see #26)

Reads a JSON payload from stdin, writes at most one JSON line to stdout
(a deny decision) and appends an event to `.agentlog/guard-events.jsonl`.
Always exits 0 — a bug here must never block a tool call (fail-open).

ADR: docs/adr/0004-zprof-guard-pre-tool-frame.md
Spec: docs/superpowers/specs/2026-09-27-guard-hooks-design.md §4, §5, §7, §8.1
"""
import fcntl
import hashlib
import json
import os
import re
import sys
from collections.abc import Callable
from datetime import datetime, timezone
from pathlib import Path

# Tools whose calls pass through the guard at all. Read/Grep/Glob/Agent are
# not matched — see spec §13 (latency).
TOOLS_GUARDED = frozenset({"Bash", "Edit", "Write", "MultiEdit", "NotebookEdit"})

# Registry of built-in `context` evaluators, keyed by name. Empty in #23 —
# rules referencing an unregistered context simply never fire (ADR D4).
# Signature: (call, rule, config) -> bool | str. False/None = does not fire;
# True = fires with rule["reason"]; str = fires with that reason instead.
CONTEXTS: dict[str, Callable[[dict, dict, dict], "bool | str | None"]] = {}


# ---------------------------------------------------------------------------
# Role & dispatch resolution (ADR D7, spec §4)
# ---------------------------------------------------------------------------

_AGENT_TRANSCRIPT_RE = re.compile(r"^agent-(.+)\.jsonl$")


def _read_meta(transcript_path) -> dict | None:
    """Read `agent-<id>.meta.json` next to a subagent transcript.

    Returns None when `transcript_path` is not a subagent transcript path
    (parent dir isn't `subagents`, name doesn't match `agent-<id>.jsonl`),
    the meta.json file is missing, or it fails to parse. Never raises.
    """
    if not isinstance(transcript_path, str) or not transcript_path:
        return None
    p = Path(transcript_path)
    m = _AGENT_TRANSCRIPT_RE.match(p.name)
    if not m or p.parent.name != "subagents":
        return None
    meta_path = p.parent / f"agent-{m.group(1)}.meta.json"
    try:
        data = json.loads(meta_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None
    return data if isinstance(data, dict) else None


def resolve_role(payload: dict) -> str:
    """Resolve the calling role, per spec §4 / ADR D7. Never raises.

    (1) `payload["agent_type"]`, if non-empty. (2) `transcript_path` whose
    parent dir is `subagents` and name matches `agent-<id>.jsonl` — read
    `agentType` from the sibling meta.json; missing file/key/bad JSON here
    is terminal → "unknown" (does not fall through to step 3). (3) name
    ends in `.jsonl` and parent isn't `subagents` → "main" (existence of
    the file itself is not checked). (4) otherwise "unknown".
    """
    agent_type = payload.get("agent_type")
    if isinstance(agent_type, str) and agent_type:
        return agent_type

    transcript_path = payload.get("transcript_path")
    if isinstance(transcript_path, str) and transcript_path:
        p = Path(transcript_path)
        if _AGENT_TRANSCRIPT_RE.match(p.name) and p.parent.name == "subagents":
            meta = _read_meta(transcript_path)
            role = meta.get("agentType") if meta else None
            return role if isinstance(role, str) and role else "unknown"
        if p.name.endswith(".jsonl") and p.parent.name != "subagents":
            return "main"

    return "unknown"


def dispatch_id(payload: dict) -> str | None:
    """`toolUseId` from the same meta.json used by `resolve_role`; None for main/unknown."""
    meta = _read_meta(payload.get("transcript_path"))
    if not meta:
        return None
    tid = meta.get("toolUseId")
    return tid if isinstance(tid, str) and tid else None


# ---------------------------------------------------------------------------
# Command normalization (ADR D7, spec §4)
# ---------------------------------------------------------------------------

_RTK_PREFIXES = ("rtk proxy ", "rtk ")
_CD_RE = re.compile(r'^cd\s+("([^"]*)"|\'([^\']*)\'|(\S+))\s*(&&|;)')


def normalize_command(cmd: str) -> str:
    """Strip one leading `rtk proxy `/`rtk ` prefix and collapse whitespace.

    Quotes are not parsed further (spec §13: `git commit -m "drop -n flag"`
    deny is an accepted false positive).
    """
    cmd = cmd.lstrip()
    for prefix in _RTK_PREFIXES:
        if cmd.startswith(prefix):
            cmd = cmd[len(prefix):]
            break
    return re.sub(r"\s+", " ", cmd).strip()


def working_dir(cmd: str, cwd: str) -> str:
    """Working directory for a (normalized) command: `cd <path> &&`/`;` or `cwd`.

    Not used by the rule engine in #23 — API surface for the #24/#25
    context evaluators.
    """
    m = _CD_RE.match(cmd)
    if not m:
        return cwd
    path = m.group(2) if m.group(2) is not None else m.group(3) if m.group(3) is not None else m.group(4)
    path = os.path.expanduser(path)
    if not os.path.isabs(path):
        path = os.path.join(cwd, path)
    return os.path.normpath(path)


# ---------------------------------------------------------------------------
# Config & rule engine (ADR D2, D7)
# ---------------------------------------------------------------------------


def project_root(payload: dict) -> str:
    """`$CLAUDE_PROJECT_DIR` if set and a directory; else `payload["cwd"]`; else cwd."""
    env_root = os.environ.get("CLAUDE_PROJECT_DIR")
    if env_root and os.path.isdir(env_root):
        return env_root
    cwd = payload.get("cwd")
    if isinstance(cwd, str) and cwd:
        return cwd
    return os.getcwd()


def load_config(root: str) -> dict | None:
    """Read `<root>/.claude/guard.json`. Missing file → None (guard not deployed).

    Unparsable / not a dict / `version` != 1 → raises (caller's try/except
    turns this into a fail-open `error` event). Only JSON is ever read.
    """
    path = Path(root) / ".claude" / "guard.json"
    if not path.exists():
        return None
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict) or data.get("version") != 1:
        raise ValueError("guard.json: expected a JSON object with version 1")
    return data


def _validated_str_list(value, field: str) -> list[str]:
    """Normalize `match`/`roles`/`not_roles` to list[str].

    Raises ValueError on an unrendered `$ref` (apply didn't substitute it —
    iterating that string as a regex/role list would be a silent security
    hole) or any type that isn't str/list[str]. None/missing → [].
    """
    if value is None:
        return []
    if isinstance(value, str):
        value = [value]
    if not isinstance(value, list) or not all(isinstance(v, str) for v in value):
        raise ValueError(f"{field}: expected str or list[str]")
    for v in value:
        if v.startswith("$"):
            raise ValueError(f"{field}: unrendered reference {v!r}")
    return value


def _subject_for(tool_name: str, tool_input: dict) -> tuple[str | None, str | None]:
    """Match subject per ADR D2, and the normalized Bash command (or None)."""
    if tool_name == "Bash":
        cmd = tool_input.get("command")
        if isinstance(cmd, str):
            norm = normalize_command(cmd)
            return norm, norm
        return None, None
    if tool_name in ("Edit", "Write", "MultiEdit"):
        fp = tool_input.get("file_path")
        return (fp if isinstance(fp, str) else None), None
    if tool_name == "NotebookEdit":
        np = tool_input.get("notebook_path")
        return (np if isinstance(np, str) else None), None
    return None, None


def _check_rule(rule: dict, call: dict, config: dict) -> str | None:
    """Return the (raw, un-suffixed) deny reason if `rule` fires for `call`, else None.

    Order (cheap before expensive, ADR D2): tools → roles → not_roles →
    exempt_roles → match → context. May raise ValueError (bad $ref/type)
    or re.error (bad regex) — left to the caller's try/except (fail-open).
    """
    tools = rule.get("tools")
    if not isinstance(tools, list) or call["tool_name"] not in tools:
        return None

    roles = _validated_str_list(rule.get("roles"), "roles")
    if roles and call["role"] not in roles:
        return None

    not_roles = _validated_str_list(rule.get("not_roles"), "not_roles")
    if not_roles and call["role"] in not_roles:
        return None

    exempt_roles = config.get("exempt_roles")
    if isinstance(exempt_roles, dict):
        exempt_for_rule = exempt_roles.get(rule.get("id"))
        if isinstance(exempt_for_rule, list) and call["role"] in exempt_for_rule:
            return None

    match = _validated_str_list(rule.get("match"), "match")
    if match:
        subject = call.get("subject")
        if not isinstance(subject, str) or not subject:
            return None
        if not any(re.search(pattern, subject) for pattern in match):
            return None

    context = rule.get("context")
    if context:
        evaluator = CONTEXTS.get(context)
        if evaluator is None:
            return None  # unregistered context: rule never fires (ADR D4)
        result = evaluator(call, rule, config)
        if not result:
            return None
        if isinstance(result, str):
            return result

    return str(rule.get("reason", ""))


def evaluate_rules(call: dict, config: dict) -> dict | None:
    """Walk `config["rules"]` in order; first firing rule wins (ADR D2)."""
    rules = config.get("rules")
    if not isinstance(rules, list):
        return None
    for rule in rules:
        if not isinstance(rule, dict):
            continue
        reason = _check_rule(rule, call, config)
        if reason is not None:
            return {"id": rule.get("id"), "reason": reason}
    return None


def deny_output(rule_id: str, reason: str) -> dict:
    """Exact §5.7 deny payload (ADR D8)."""
    clean = reason.rstrip().rstrip(".")
    return {
        "hookSpecificOutput": {
            "hookEventName": "PreToolUse",
            "permissionDecision": "deny",
            "permissionDecisionReason": (
                f"zprof guard [{rule_id}]: {clean}. "
                f"Не обходи: верни `verdict: blocked`, reason: {rule_id}."
            ),
        }
    }


# ---------------------------------------------------------------------------
# Journal: .agentlog/guard-events.jsonl + guard-state.json (ADR D9)
# ---------------------------------------------------------------------------

_MAX_ROLE_UNRESOLVED_SESSIONS = 200


def _now_ts() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def _input_hash(tool_input) -> str:
    """sha1 of the canonical JSON of a tool input, first 12 hex chars.

    Mirrors `zprof-collect.py:_input_hash` byte for byte (can't import it —
    the two scripts are deployed separately).
    """
    try:
        canon = json.dumps(tool_input, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
    except (TypeError, ValueError):
        canon = repr(tool_input)
    return hashlib.sha1(canon.encode("utf-8")).hexdigest()[:12]


_ENV_ASSIGNMENT_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*=\S*$")


def _target(tool_name: str, tool_input: dict, command: str | None) -> str | None:
    """First two whitespace tokens of the command, or basename of file/notebook path.

    Leading `NAME=value` shell env-var assignments (e.g. `GH_TOKEN=ghp_xxx gh
    release create`) are skipped before picking the two tokens — the journal
    must never record a secret verbatim (review P1-1).
    """
    if tool_name == "Bash":
        if not command:
            return None
        tokens = command.split()
        while tokens and _ENV_ASSIGNMENT_RE.match(tokens[0]):
            tokens = tokens[1:]
        return " ".join(tokens[:2]) if tokens else None
    if tool_name in ("Edit", "Write", "MultiEdit"):
        fp = tool_input.get("file_path")
        return os.path.basename(fp) if isinstance(fp, str) and fp else None
    if tool_name == "NotebookEdit":
        np = tool_input.get("notebook_path")
        return os.path.basename(np) if isinstance(np, str) and np else None
    return None


def write_event(event: dict, root: str) -> None:
    """Append one JSON line to `.agentlog/guard-events.jsonl`, `flock` + `fsync`."""
    agentlog = Path(root) / ".agentlog"
    agentlog.mkdir(parents=True, exist_ok=True)
    path = agentlog / "guard-events.jsonl"
    with open(path, "a") as f:
        fcntl.flock(f.fileno(), fcntl.LOCK_EX)
        try:
            f.write(json.dumps(event, ensure_ascii=False) + "\n")
            f.flush()
            os.fsync(f.fileno())
        finally:
            fcntl.flock(f.fileno(), fcntl.LOCK_UN)


def _safe_write_event(event: dict, root: str) -> None:
    """`write_event`, with any failure swallowed (review P1-2).

    `.agentlog/` being unwritable (missing perms, a plain file where a dir
    is expected, a full disk...) must never turn an already-decided deny
    into an allow by raising out of `pre_tool()` into `main()`'s outer
    fail-open `except` — the journal is best-effort, the decision is not.
    """
    try:
        write_event(event, root)
    except Exception:
        pass


def _note_role_unresolved(session_id, root: str) -> bool:
    """True the first time `session_id` is seen with an unresolved role.

    Dedup state lives in `.agentlog/guard-state.json`, read-modify-write
    under `.agentlog/.guard.lock` (not the collector's `.agentlog/.lock` —
    guard runs on every Bash/Edit, the collector holds its lock for seconds).

    Any failure touching `.agentlog/` (review P1-2: e.g. it exists as a
    plain file, or the filesystem rejects the write) is swallowed and
    treated as "not seen before" — this is a best-effort dedup for a purely
    informational event and must never break the surrounding rule check.
    """
    if not session_id:
        return True
    try:
        agentlog = Path(root) / ".agentlog"
        agentlog.mkdir(parents=True, exist_ok=True)
        lock_path = agentlog / ".guard.lock"
        state_path = agentlog / "guard-state.json"
        lock_path.touch(exist_ok=True)
        with open(lock_path, "r+") as lf:
            fcntl.flock(lf.fileno(), fcntl.LOCK_EX)
            try:
                try:
                    state = json.loads(state_path.read_text(encoding="utf-8"))
                except (OSError, json.JSONDecodeError):
                    state = {}
                if not isinstance(state, dict):
                    state = {}
                sessions = state.get("role_unresolved_sessions")
                if not isinstance(sessions, list):
                    sessions = []
                if session_id in sessions:
                    return False
                sessions = (sessions + [session_id])[-_MAX_ROLE_UNRESOLVED_SESSIONS:]
                new_state = {"version": 1, "role_unresolved_sessions": sessions}
                tmp = state_path.with_suffix(".json.tmp")
                tmp.write_text(json.dumps(new_state, ensure_ascii=False))
                os.replace(tmp, state_path)
                return True
            finally:
                fcntl.flock(lf.fileno(), fcntl.LOCK_UN)
    except Exception:
        return True


# ---------------------------------------------------------------------------
# pre-tool entrypoint (ADR D7)
# ---------------------------------------------------------------------------


def pre_tool(payload: dict) -> dict | None:
    """Evaluate one `PreToolUse` call. Returns the deny payload, or None (allow/silent)."""
    tool_name = payload.get("tool_name")
    if tool_name not in TOOLS_GUARDED:
        return None

    root = project_root(payload)
    config = load_config(root)
    if config is None:
        return None

    role = resolve_role(payload)
    did = dispatch_id(payload)
    tool_input = payload.get("tool_input")
    if not isinstance(tool_input, dict):
        tool_input = {}
    subject, command = _subject_for(tool_name, tool_input)
    session_id = payload.get("session_id")
    input_hash = _input_hash(tool_input)

    if role == "unknown" and _note_role_unresolved(session_id, root):
        _safe_write_event({
            "ts": _now_ts(),
            "session_id": session_id,
            "event": "role_unresolved",
            "role": "unknown",
            "dispatch_id": did,
            "tool": tool_name,
            "rule": None,
            "decision": None,
            "target": _target(tool_name, tool_input, command),
            "input_hash": input_hash,
            "run_id": None,
        }, root)

    call = {
        "tool_name": tool_name,
        "role": role,
        "dispatch_id": did,
        "subject": subject,
        "command": command,
        "root": root,
        "cwd": payload.get("cwd"),
        "tool_input": tool_input,
    }
    hit = evaluate_rules(call, config)
    if hit is None:
        return None

    clean_reason = str(hit["reason"]).rstrip().rstrip(".")
    _safe_write_event({
        "ts": _now_ts(),
        "session_id": session_id,
        "event": "pre-tool",
        "role": role,
        "dispatch_id": did,
        "tool": tool_name,
        "rule": hit["id"],
        "decision": "deny",
        "target": _target(tool_name, tool_input, command),
        "input_hash": input_hash,
        "run_id": None,
    }, root)
    return deny_output(hit["id"], clean_reason)


# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------


def main() -> None:
    mode = None
    payload = None
    try:
        mode = sys.argv[1]
        payload = json.load(sys.stdin)
        if not isinstance(payload, dict):
            raise TypeError("payload is not a JSON object")

        out = None
        if mode == "pre-tool":
            out = pre_tool(payload)
        # Any other mode (incl. "subagent-stop", implemented in #26) is a no-op.

        if out is not None:
            sys.stdout.write(json.dumps(out, ensure_ascii=False))
    except Exception as e:
        try:
            root = project_root(payload) if isinstance(payload, dict) else os.getcwd()
            write_event({
                "ts": _now_ts(),
                "session_id": payload.get("session_id") if isinstance(payload, dict) else None,
                "event": mode,
                "role": None,
                "dispatch_id": None,
                "tool": payload.get("tool_name") if isinstance(payload, dict) else None,
                "rule": None,
                "decision": "error",
                "detail": type(e).__name__,
                "target": None,
                "input_hash": None,
                "run_id": None,
            }, root)
        except Exception:
            pass
    sys.exit(0)


if __name__ == "__main__":
    main()
