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
import fnmatch
import hashlib
import json
import os
import re
import subprocess
import sys
from collections.abc import Callable
from datetime import datetime, timezone
from pathlib import Path

# Tools whose calls pass through the guard at all. Read/Grep/Glob/Agent are
# not matched — see spec §13 (latency).
TOOLS_GUARDED = frozenset({"Bash", "Edit", "Write", "MultiEdit", "NotebookEdit"})

# Registry of built-in `context` evaluators, keyed by name. Populated at the
# bottom of this module (ADR-0005, #24) — rules referencing a still
# unregistered context (§5.5/§5.6, #25) simply never fire (ADR D4).
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
# Context evaluators (ADR-0005, #24)
# ---------------------------------------------------------------------------

_GIT_TIMEOUT = 3
_GH_TIMEOUT = 10


def _run(argv: list[str], cwd: str, timeout: float) -> tuple[int | None, str]:
    """Run one external command for a context evaluator. Never raises.

    Returns `(0, stdout)` on success; `(returncode, "")` on a non-zero exit —
    stdout is discarded and stderr is never inspected (locale-dependent);
    `(None, type(exc).__name__)` on `TimeoutExpired`/`OSError` (missing
    binary, `cwd` not a directory, ...).

    Deciding whether a given return means "error" or a normal "no" is left
    to the caller — the same non-zero exit means different things for
    different questions (ADR-0005 Context §2). `_run` itself writes nothing
    to the journal. Evaluators call this via the module-level name (not a
    bound default argument) so tests can `monkeypatch.setattr(module, "_run", fake)`
    to exercise `branch_pr_merged` without a network/`gh`.
    """
    env = dict(os.environ)
    env["GIT_TERMINAL_PROMPT"] = "0"
    env["GH_PROMPT_DISABLED"] = "1"
    env["GIT_OPTIONAL_LOCKS"] = "0"
    try:
        proc = subprocess.run(
            argv, cwd=cwd, timeout=timeout, capture_output=True, text=True,
            stdin=subprocess.DEVNULL, check=False, env=env,
        )
    except (subprocess.TimeoutExpired, OSError) as e:
        return None, type(e).__name__
    if proc.returncode == 0:
        return 0, proc.stdout
    return proc.returncode, ""


def _context_detail(argv: list[str], rc: int | None, out: str) -> str:
    """`context_error` `detail`: `"<argv[0]> <argv[1]>: exit <rc>"` or
    `"...: <ExceptionName>"` (`out` holds the exception class name when
    `rc is None`). Never includes paths, stdout or stderr (spec §7)."""
    head = " ".join(argv[:2])
    return f"{head}: {out}" if rc is None else f"{head}: exit {rc}"


def _note_context_error(call: dict, rule: dict, detail: str) -> None:
    """Accumulate a diagnostic context-evaluator failure on `call`. No I/O.

    `pre_tool()` flushes `call["context_errors"]` to the journal after rule
    evaluation (ADR-0005 E2) — evaluators have no `session_id`/`input_hash`/
    `target` to build a full event themselves, and `setdefault` lets them be
    called in unit tests against a bare `call` dict.
    """
    call.setdefault("context_errors", []).append({"rule": rule.get("id"), "detail": detail})


def _head_on_remote(call: dict, rule: dict, config: dict) -> bool:
    """`head_on_remote` (rules `rebase_published`/`amend_published`, ADR-0005 E3).

    True when HEAD is reachable from some remote-tracking branch (published)
    *and* has an upstream. The cheap, unambiguous check runs first: if HEAD
    isn't on any remote branch, the upstream check is skipped entirely — "no
    upstream" is a normal "no" (detached HEAD, unpublished branch), not an
    error, so it must not be the check that decides whether we're in a repo
    at all (a bare `git rev-parse @{u}` gives the same exit 128 for both).
    """
    wd = working_dir(call.get("command") or "", call.get("cwd") or call["root"])
    argv = ["git", "branch", "-r", "--contains", "HEAD"]
    rc, out = _run(argv, wd, _GIT_TIMEOUT)
    if rc is None or rc != 0:
        _note_context_error(call, rule, _context_detail(argv, rc, out))
        return False
    if out.strip() == "":
        return False  # HEAD not published yet -- allow, no second call needed

    argv2 = ["git", "rev-parse", "--abbrev-ref", "@{u}"]
    rc2, out2 = _run(argv2, wd, _GIT_TIMEOUT)
    if rc2 == 0:
        return True
    if rc2 is None:
        _note_context_error(call, rule, _context_detail(argv2, rc2, out2))
        return False
    return False  # no upstream / detached HEAD -- a repo was already proven above, not an error


def _linked_worktree(call: dict, rule: dict, config: dict) -> bool:
    """`linked_worktree` (rule `stash_in_worktree`, ADR-0005 E4).

    True when the current worktree is a linked worktree: `git-dir` and
    `git-common-dir` differ once both are resolved relative to `wd` (git
    prints them relative to its own cwd, not the guard process's cwd — a
    bare `os.path.realpath` would resolve from the wrong directory).
    """
    wd = working_dir(call.get("command") or "", call.get("cwd") or call["root"])
    argv = ["git", "rev-parse", "--git-dir", "--git-common-dir"]
    rc, out = _run(argv, wd, _GIT_TIMEOUT)
    if rc is None or rc != 0:
        _note_context_error(call, rule, _context_detail(argv, rc, out))
        return False
    lines = [line for line in out.splitlines() if line.strip()]
    if len(lines) < 2:
        _note_context_error(call, rule, "git rev-parse: unexpected output")
        return False
    git_dir = os.path.realpath(os.path.join(wd, lines[0]))
    common_dir = os.path.realpath(os.path.join(wd, lines[1]))
    return git_dir != common_dir


_ENV_HEAD_RE = re.compile(r"^\$([A-Za-z_][A-Za-z0-9_]*)")
_GLOB_CHARS = frozenset("*?[")


def _expand_prefix_head(prefix: str, root: str) -> str | None:
    """Expand the `$VAR`/`~` head of one `allow_write_prefixes` entry.

    Returns None when the whole prefix must be skipped: an unset/empty env
    var, or `~` with no HOME to expand against (ADR-0005 E5 step 1). Only
    the head is substituted — a glob tail like `*/memory/` is left as-is.
    `$VAR` is only recognized at the very start of the prefix.
    """
    if prefix == "$CLAUDE_PROJECT_DIR" or prefix.startswith("$CLAUDE_PROJECT_DIR/"):
        return root + prefix[len("$CLAUDE_PROJECT_DIR"):]
    m = _ENV_HEAD_RE.match(prefix)
    if m:
        value = os.environ.get(m.group(1))
        if not value:
            return None
        return value + prefix[m.end():]
    if prefix.startswith("~"):
        head, _, rest = prefix.partition("/")
        expanded = os.path.expanduser(head)
        if expanded == head:
            return None  # no HOME to expand against
        return expanded + ("/" + rest if rest else "")
    return prefix


def _prefix_segments(prefix: str, root: str) -> "tuple[list[str], list[str]] | None":
    """Literal head (realpath'd) + untouched glob tail segments for one prefix.

    None means "this prefix never matches anything" (unresolvable $VAR/~, or
    not absolute after expansion). ADR-0005 E5 steps 1.2-1.3: only the
    literal head (up to the first segment containing `*?[`) is realpath'd —
    glob segments are left untouched so `fnmatch` still sees them.
    """
    expanded = _expand_prefix_head(prefix, root)
    if not expanded or not os.path.isabs(expanded):
        return None
    if expanded != "/":
        expanded = expanded.rstrip("/")
    segs = expanded.split("/")
    k = len(segs)
    for i, seg in enumerate(segs):
        if any(c in seg for c in _GLOB_CHARS):
            k = i
            break
    literal = "/".join(segs[:k]) or "/"
    head_segs = os.path.realpath(literal).split("/")
    return head_segs, segs[k:]


def _prefix_matches(head_segs: list[str], tail: list[str], rsegs: list[str]) -> bool:
    """Positional segment match: literal head by equality, glob tail by `fnmatchcase`.

    Prefix semantics only — path segments beyond `len(head_segs) + len(tail)`
    are not inspected (ADR-0005 E5 step 2). A tail glob segment (`*`) matches
    exactly one path segment; it can never absorb a `/`.
    """
    if len(head_segs) + len(tail) > len(rsegs):
        return False
    if rsegs[:len(head_segs)] != head_segs:
        return False
    return all(
        fnmatch.fnmatchcase(rsegs[len(head_segs) + j], pat)
        for j, pat in enumerate(tail)
    )


def _write_outside_repo(call: dict, rule: dict, config: dict) -> bool:
    """`write_outside_repo` (Edit/Write/MultiEdit/NotebookEdit, ADR-0005 E5).

    Subject is `call["subject"]` (already `file_path`/`notebook_path`, D2);
    there's no shell command here, so `working_dir()` is not used. Deny
    unless the realpath'd target sits under one of the (expanded, glob-aware)
    `allow_write_prefixes`, or under a linked worktree of this project
    (`git rev-parse --git-common-dir`, run from the nearest existing
    ancestor directory, resolves to `$CLAUDE_PROJECT_DIR/.git`).
    """
    subject = call.get("subject")
    if not isinstance(subject, str) or not subject:
        return False
    path = subject if os.path.isabs(subject) else os.path.join(call.get("cwd") or call["root"], subject)
    real = os.path.realpath(path)
    rsegs = real.split("/")

    prefixes = config.get("allow_write_prefixes")
    if not isinstance(prefixes, list) or not all(isinstance(p, str) for p in prefixes):
        raise ValueError("allow_write_prefixes: expected list[str]")
    for prefix in prefixes:
        expanded = _prefix_segments(prefix, call["root"])
        if expanded is None:
            continue
        head_segs, tail = expanded
        if _prefix_matches(head_segs, tail, rsegs):
            return False

    d = os.path.dirname(real)
    while d != "/" and not os.path.isdir(d):
        d = os.path.dirname(d)
    argv = ["git", "rev-parse", "--git-common-dir"]
    rc, out = _run(argv, d, _GIT_TIMEOUT)
    if rc == 0:
        common = os.path.realpath(os.path.join(d, out.strip()))
        expected = os.path.join(os.path.realpath(call["root"]), ".git")
        if common == expected:
            return False
    elif rc is None:
        _note_context_error(call, rule, _context_detail(argv, rc, out))
    return True  # no prefix matched, and allowance did not confirm a linked worktree of this repo


_QUOTE_RE = re.compile(r'^(["\'])(.*)\1$')
_REF_NAME_RE = re.compile(r'^[A-Za-z0-9._][A-Za-z0-9._/-]*$')


def _strip_one_quote_pair(token: str) -> str:
    m = _QUOTE_RE.match(token)
    return m.group(2) if m else token


def _branch_pr_merged(call: dict, rule: dict, config: dict) -> bool:
    """`branch_pr_merged` (rule `remote_ref_delete_unmerged`, roles: [pr-shepherd]).

    ADR-0005 E6 — the one fail-closed evaluator. Any failure (unparseable
    command, non-zero/timeout/missing `gh`, malformed JSON, empty result) is
    a *decision* (deny), not silence, so unlike the other three evaluators
    this never calls `_note_context_error`. The whole body runs inside
    `try/except Exception: return True` — an uncaught exception here would
    otherwise escape into `main()`'s outer fail-open `except` and produce an
    allow, exactly backwards for an irreversible remote branch deletion.
    """
    try:
        command = call.get("command") or ""
        targets: set[str] = set()
        for segment in re.split(r"&&|\|\||;|\|", command):
            tokens = [_strip_one_quote_pair(t) for t in segment.split()]
            push_at = None
            for i in range(len(tokens) - 1):
                if tokens[i] == "git" and tokens[i + 1] == "push":
                    push_at = i
                    break
            if push_at is None:
                continue

            delete_mode = False
            positionals: list[str] = []
            for tok in tokens[push_at + 2:]:
                if tok.startswith("-"):
                    if tok in ("--delete", "-d"):
                        delete_mode = True
                    continue  # other flags (and their values) fall through below
                positionals.append(tok)
            if not positionals:
                continue

            for refspec in positionals[1:]:  # positionals[0] is the remote
                if delete_mode:
                    if ":" in refspec:
                        return True  # unparseable
                    name = refspec
                elif refspec.startswith(":"):
                    name = refspec[1:]
                else:
                    continue  # ordinary push, not a deletion target

                if name.startswith("refs/heads/"):
                    name = name[len("refs/heads/"):]
                elif name.startswith("refs/"):
                    return True  # refs/tags/..., refs/pull/... -- unparseable
                if ".." in name or not _REF_NAME_RE.match(name):
                    return True
                targets.add(name)

        if len(targets) != 1:
            return True  # zero or ambiguous (multiple) targets -- deny without calling gh
        name = next(iter(targets))

        wd = working_dir(command, call.get("cwd") or call["root"])
        argv = ["gh", "pr", "list", "--head", name, "--state", "merged", "--json", "number"]
        rc, out = _run(argv, wd, _GH_TIMEOUT)
        if rc != 0:
            return True
        data = json.loads(out)
        return not (isinstance(data, list) and len(data) > 0)
    except Exception:
        return True


CONTEXTS.update({
    "head_on_remote": _head_on_remote,
    "linked_worktree": _linked_worktree,
    "write_outside_repo": _write_outside_repo,
    "branch_pr_merged": _branch_pr_merged,
})


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
        "context_errors": [],
    }
    try:
        hit = evaluate_rules(call, config)
    finally:
        # ADR-0005 E2: flushed in `finally` so a later rule raising (bad
        # regex/$ref -> fail-open) doesn't swallow earlier context_errors.
        for err in call.get("context_errors") or []:
            _safe_write_event({
                "ts": _now_ts(),
                "session_id": session_id,
                "event": "context_error",
                "role": role,
                "dispatch_id": did,
                "tool": tool_name,
                "rule": err.get("rule"),
                "decision": None,
                "detail": err.get("detail"),
                "target": _target(tool_name, tool_input, command),
                "input_hash": input_hash,
                "run_id": None,
            }, root)

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
