#!/usr/bin/env python3
"""zprof guard — deterministic PreToolUse/SubagentStop enforcement, runs as a Claude Code hook.

Usage: zprof-guard.py <mode>
Modes: pre-tool | subagent-stop

Reads a JSON payload from stdin, writes at most one JSON line to stdout —
deny (pre-tool) or block (subagent-stop) — and appends an event to
`.agentlog/guard-events.jsonl`. Always exits 0 — a bug here must never
block a tool call or a subagent turn (fail-open).

ADR: docs/adr/0004-zprof-guard-pre-tool-frame.md,
docs/adr/0007-guard-subagent-stop-validator.md
Spec: docs/superpowers/specs/2026-09-27-guard-hooks-design.md §4, §5, §6, §7, §8.1
"""
import fcntl
import fnmatch
import hashlib
import json
import os
import re
import shlex
import subprocess
import sys
from collections.abc import Callable
from datetime import datetime, timezone
from pathlib import Path

# Tools whose calls pass through the guard at all. Read/Grep/Glob/Agent are
# not matched — see spec §13 (latency).
TOOLS_GUARDED = frozenset({"Bash", "Edit", "Write", "MultiEdit", "NotebookEdit"})

# Registry of built-in `context` evaluators, keyed by name. Populated at the
# bottom of this module (ADR-0005 #24, ADR-0006 #25). A rule referencing an
# unregistered context simply never fires (ADR D4).
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


# `detail` codes for `_note_unverified` — a closed set (ADR-0006 F1).
_PREFLIGHT_UNVERIFIED = "preflight_unverified"
_PARSE_ERROR = "parse_error"


def _note_unverified(call: dict, rule: dict, code: str, detail: str) -> None:
    """Accumulate a fail-open `allow_unverified` decision on `call`. No I/O.

    Mirrors `_note_context_error` in shape (accumulate on `call`, flush in
    `pre_tool()`'s `finally`) but is a distinct *decision*, not a diagnostic:
    `context_error` means "the context evaluator couldn't answer, the rule
    stays silent" (`decision: null`); `allow_unverified` means "a gate rule
    made a conscious fail-open call" (`decision: "allow_unverified"`).
    Readers/doctor (#28/#29) must be able to tell them apart by `event`/
    `decision` alone, without parsing `detail` (ADR-0006 F1). `detail` is
    always `"<code>: <specifics>"`; `code` is one of `_PREFLIGHT_UNVERIFIED`/
    `_PARSE_ERROR` — `<specifics>` is a fixed string or exception class name
    only, never a path/stdout/stderr (spec §7).
    """
    call.setdefault("unverified", []).append({"rule": rule.get("id"), "detail": f"{code}: {detail}"})


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


# ---------------------------------------------------------------------------
# Merge/PR gates (ADR-0006, #25)
# ---------------------------------------------------------------------------

_OPERATOR_CHARS = frozenset("();<>|&")


def _shell_tokens(raw: str) -> list[str]:
    """Tokenize a raw shell command line, preserving quoted newlines.

    `lex.commenters = ""` — `#` is an ordinary character, not a comment
    marker (Context §7: `-t a#b -b x` must not lose `-b x`). Operators
    (`&&`, `||`, `;`, `|`, `>&`, `<<`, `(`, `)`) come out as their own
    tokens; quoted content (including embedded newlines) stays one token —
    this is why evaluators parse `tool_input["command"]` and not the
    whitespace-normalized `call["command"]` (Context §3). Raises `ValueError`
    on an unbalanced quote or trailing backslash — the caller decides what
    that means (`parse_error`, F3/F4).
    """
    lex = shlex.shlex(raw, posix=True, punctuation_chars=True)
    lex.whitespace_split = True
    lex.commenters = ""
    return list(lex)


def _is_operator_token(tok: str) -> bool:
    return bool(tok) and all(c in _OPERATOR_CHARS for c in tok)


def _invocations(tokens: list[str], words: tuple[str, ...]) -> list[list[str]]:
    """All `args` following each occurrence of `words` (e.g. `("gh", "pr", "create")`).

    An occurrence is `os.path.basename(tokens[i]) == words[0]` with the
    following tokens matching `words[1:]` exactly — finds `gh`, `rtk gh`,
    `/opt/homebrew/bin/gh` without special-casing wrappers (ADR-0006 F2).
    `args` runs from there to the next operator token or the end of
    `tokens`. A trailing fd-redirect target (`2>&1` → lone digit right
    before a `<`/`>` operator) is dropped from `args` (Context §7). Zero
    occurrences (the words matched only inside a quoted token, e.g. `grep -r
    "gh pr create" docs/`) → `[]`, deliberately not distinguished from "no
    invocation at all" by the caller.
    """
    results: list[list[str]] = []
    n = len(tokens)
    w = len(words)
    i = 0
    while i < n:
        if os.path.basename(tokens[i]) == words[0] and tokens[i + 1:i + w] == list(words[1:]):
            j = i + w
            args: list[str] = []
            while j < n and not _is_operator_token(tokens[j]):
                args.append(tokens[j])
                j += 1
            if j < n and tokens[j][:1] in ("<", ">") and args and re.match(r"^\d+$", args[-1]):
                args.pop()
            results.append(args)
            i = j
        else:
            i += 1
    return results


_CLOSES_RE = re.compile(r"(?i)\bcloses\s+#\d+")
_GATE_RE = re.compile(r"(?m)^##\s+Gate\b")


def _missing_markers(body: str) -> list[str]:
    """Subset of `["`Closes #`", "раздела `## Gate`"]` missing from `body`, in this order."""
    missing = []
    if not _CLOSES_RE.search(body):
        missing.append("`Closes #`")
    if not _GATE_RE.search(body):
        missing.append("раздела `## Gate`")
    return missing


_MERGE_JSON_FIELDS = "number,body,closingIssuesReferences,state"

_MERGE_VALUE_LONG = frozenset({
    "subject", "body", "body-file", "author-email", "match-head-commit", "repo",
})
_MERGE_VALUE_SHORT = frozenset({"t", "b", "F", "A", "R"})


def _parse_merge_args(args: list[str]) -> tuple[str | None, str | None, bool]:
    """One `gh pr merge` invocation's `args` -> `(selector, repo, ok)`.

    pflag semantics: `--name=value` slitno; `--name` from the value set
    consumes the next token; a short cluster (`-xyz`) reads left to right,
    the first char in the value set absorbs the rest of the token or the
    next token, other chars are boolean; `--` ends flag parsing. `-R/--repo`
    is remembered (last wins); the first positional is `selector`, later
    positionals are ignored. `ok=False` means `selector` or `repo` starts
    with `-` (only reachable after `--`, or as a flag's own value) — an
    injection guard, caller notes `parse_error` and skips this target
    (ADR-0006 F3).
    """
    repo: str | None = None
    selector: str | None = None
    end_of_flags = False
    i = 0
    n = len(args)
    while i < n:
        tok = args[i]
        if end_of_flags:
            if selector is None:
                selector = tok
            i += 1
            continue
        if tok == "--":
            end_of_flags = True
            i += 1
            continue
        if tok.startswith("--") and len(tok) > 2:
            name, eq, val = tok[2:].partition("=")
            consumed = 1
            if not eq:
                val = None
                if name in _MERGE_VALUE_LONG:
                    if i + 1 < n:
                        val = args[i + 1]
                        consumed = 2
                    else:
                        val = ""
            if name == "repo" and val is not None:
                repo = val
            i += consumed
            continue
        if tok.startswith("-") and len(tok) > 1:
            rest = tok[1:]
            j = 0
            consumed_next = False
            while j < len(rest):
                ch = rest[j]
                if ch in _MERGE_VALUE_SHORT:
                    remainder = rest[j + 1:]
                    if remainder:
                        val = remainder
                    elif i + 1 < n:
                        val = args[i + 1]
                        consumed_next = True
                    else:
                        val = ""
                    if ch == "R":
                        repo = val
                    break
                j += 1
            i += 2 if consumed_next else 1
            continue
        if selector is None:
            selector = tok
        i += 1
    ok = not ((selector is not None and selector.startswith("-"))
              or (repo is not None and repo.startswith("-")))
    return selector, repo, ok


_API_MERGE_SEARCH_RE = re.compile(r"/?pulls/(\d+)/merge\b")
_API_MERGE_FULL_RE = re.compile(r"^/?repos/([^/]+)/([^/]+)/pulls/(\d+)/merge/?$")
_SAFE_NAME_RE = re.compile(r"^[A-Za-z0-9_.-]+$")
_API_REPO_PLACEHOLDERS = frozenset({"{owner}", "{repo}", ":owner", ":repo"})


def _merge_target_from_api_args(args: list[str]) -> "tuple[str, str | None] | None":
    """One `gh api` invocation's `args` -> `(number, repo | None)`, or None
    when no token matches `/pulls/<N>/merge` at all (ADR-0006 F3)."""
    for tok in args:
        m_search = _API_MERGE_SEARCH_RE.search(tok)
        if not m_search:
            continue
        m_full = _API_MERGE_FULL_RE.match(tok)
        if m_full:
            owner, repo_name, number = m_full.group(1), m_full.group(2), m_full.group(3)
            if _SAFE_NAME_RE.match(owner) and _SAFE_NAME_RE.match(repo_name):
                return number, f"{owner}/{repo_name}"
            if owner in _API_REPO_PLACEHOLDERS and repo_name in _API_REPO_PLACEHOLDERS:
                return number, None
        return m_search.group(1), None
    return None


def _merge_preflight(call: dict, rule: dict, config: dict) -> "bool | str":
    """`merge_preflight` (rule `merge_preflight`, roles: `$merge_roles`, ADR-0006 F3).

    Fail-open on any `gh` failure — `_note_unverified` + this target is
    skipped, never a deny (§5.5 decision 1: a sync failure must not block a
    routine, revertible merge). Unlike `_branch_pr_merged` there is no
    top-level `try/except Exception`: expected failures are caught
    explicitly below; an unexpected exception is a bug and reaches `main()`'s
    outer fail-open `except` -> allow + `error` (ADR-0004 D2).
    """
    raw = call.get("tool_input", {}).get("command")
    if not isinstance(raw, str):
        return False
    try:
        tokens = _shell_tokens(raw)
    except ValueError:
        _note_unverified(call, rule, _PARSE_ERROR, "shlex")
        return False

    wd = working_dir(call.get("command") or "", call.get("cwd") or call["root"])

    targets: list[tuple[str | None, str | None]] = []
    for args in _invocations(tokens, ("gh", "pr", "merge")):
        selector, repo, ok = _parse_merge_args(args)
        if not ok:
            _note_unverified(call, rule, _PARSE_ERROR, "selector")
            continue
        targets.append((selector, repo))
    for args in _invocations(tokens, ("gh", "api")):
        target = _merge_target_from_api_args(args)
        if target is not None:
            targets.append(target)

    if not targets:
        return False

    for selector, repo in targets:
        argv = ["gh", "pr", "view", *([selector] if selector else []),
                *(["-R", repo] if repo else []), "--json", _MERGE_JSON_FIELDS]
        rc, out = _run(argv, wd, _GH_TIMEOUT)
        if rc is None or rc != 0:
            _note_unverified(call, rule, _PREFLIGHT_UNVERIFIED, _context_detail(argv, rc, out))
            continue
        try:
            data = json.loads(out)
        except ValueError:
            _note_unverified(call, rule, _PREFLIGHT_UNVERIFIED, "gh pr view: invalid json")
            continue
        number = data.get("number") if isinstance(data, dict) else None
        if (not isinstance(data, dict)
                or not isinstance(number, int) or isinstance(number, bool)
                or not isinstance(data.get("body"), str)
                or not isinstance(data.get("closingIssuesReferences"), list)
                or not isinstance(data.get("state"), str)):
            _note_unverified(call, rule, _PREFLIGHT_UNVERIFIED, "gh pr view: unexpected shape")
            continue

        if data["state"] != "OPEN":
            continue  # closed/merged already -- nothing to protect, not an event

        body = data["body"]
        closes_ok = bool(_CLOSES_RE.search(body)) or len(data["closingIssuesReferences"]) > 0
        gate_ok = bool(_GATE_RE.search(body))
        if closes_ok and gate_ok:
            continue
        if not closes_ok and not gate_ok:
            return f"PR #{number} без `Closes #` и без раздела `## Gate`"
        if not closes_ok:
            return f"PR #{number} без `Closes #`"
        return f"PR #{number} без раздела `## Gate`"

    return False


_CREATE_VALUE_LONG = frozenset({
    "body", "body-file", "title", "base", "head", "assignee", "label",
    "milestone", "project", "reviewer", "repo", "template", "recover",
})
_CREATE_VALUE_SHORT = frozenset({"b", "F", "t", "B", "H", "a", "l", "m", "p", "r", "R", "T"})
_CREATE_FILL_LONG = frozenset({"fill", "fill-first", "fill-verbose"})
_STDIN_MARKERS = frozenset({"-", "/dev/stdin", "/dev/fd/0"})
_BODY_FILE_MAX = 1_048_576


def _parse_create_args(args: list[str]) -> "tuple[bool, list[tuple[str, str]]]":
    """One `gh pr create` invocation's `args` -> `(fill, sources)`.

    `sources` is `[("inline", value)]`/`[("file", value)]` (at most one of
    each) in the order each source type first appears; a repeated `-b`/`-F`
    overwrites the value in place (pflag last-value-wins). `-f`/`--fill*`
    (including `--fill=…`, exact name match, and `-f` anywhere in a short
    cluster before a value-taking char, e.g. `-df`) sets `fill` — checked
    with priority over any body source by the caller (ADR-0006 F4).
    """
    fill = False
    source_values: dict[str, str] = {}
    order: list[str] = []
    end_of_flags = False
    i = 0
    n = len(args)
    while i < n:
        tok = args[i]
        if end_of_flags or tok == "-" or not tok.startswith("-"):
            i += 1
            continue
        if tok == "--":
            end_of_flags = True
            i += 1
            continue
        if tok.startswith("--") and len(tok) > 2:
            name, eq, val = tok[2:].partition("=")
            if name in _CREATE_FILL_LONG:
                fill = True
                i += 1
                continue
            consumed = 1
            if not eq:
                val = None
                if name in _CREATE_VALUE_LONG:
                    if i + 1 < n:
                        val = args[i + 1]
                        consumed = 2
                    else:
                        val = ""
            if name in ("body", "body-file") and val is not None:
                key = "inline" if name == "body" else "file"
                if key not in source_values:
                    order.append(key)
                source_values[key] = val
            i += consumed
            continue
        rest = tok[1:]
        j = 0
        consumed_next = False
        while j < len(rest):
            ch = rest[j]
            if ch == "f":
                fill = True
                j += 1
                continue
            if ch in _CREATE_VALUE_SHORT:
                remainder = rest[j + 1:]
                if remainder:
                    val = remainder
                elif i + 1 < n:
                    val = args[i + 1]
                    consumed_next = True
                else:
                    val = ""
                if ch in ("b", "F"):
                    key = "inline" if ch == "b" else "file"
                    if key not in source_values:
                        order.append(key)
                    source_values[key] = val
                break
            j += 1
        i += 2 if consumed_next else 1
    return fill, [(key, source_values[key]) for key in order]


def _pr_create_gate(call: dict, rule: dict, config: dict) -> "bool | str":
    """`pr_create_gate` (rule `pr_create_gate`, no `roles`/`not_roles`, ADR-0006 F4).

    Applies to every role — §5.6: "anyone creating a PR in a zprof project
    gives `Closes #N` and `## Gate`"; targeted exemption is
    `exempt_roles.pr_create_gate` in `.zprof.yaml`. Never calls `_run` — no
    network involved, only local `shlex` and a local file read.
    """
    raw = call.get("tool_input", {}).get("command")
    if not isinstance(raw, str):
        return False
    try:
        tokens = _shell_tokens(raw)
    except ValueError:
        _note_unverified(call, rule, _PARSE_ERROR, "shlex")
        return False

    wd = working_dir(call.get("command") or "", call.get("cwd") or call["root"])
    invocations = _invocations(tokens, ("gh", "pr", "create"))
    if not invocations:
        return False

    for args in invocations:
        fill, sources = _parse_create_args(args)
        if fill:
            return ("`--fill*` запрещён: тело PR должно содержать `Closes #N` и раздел "
                    "`## Gate` — передай --body или --body-file")
        if not sources:
            return ("нет тела PR: передай --body/-b или --body-file/-F с `Closes #N` "
                    "и разделом `## Gate`")

        unverified_this_call = False
        for kind, value in sources:
            if kind == "inline":
                body = value
            else:
                if value in _STDIN_MARKERS:
                    _note_unverified(call, rule, _PREFLIGHT_UNVERIFIED, "body-file: stdin")
                    unverified_this_call = True
                    break
                path = os.path.expanduser(value)
                if not os.path.isabs(path):
                    path = os.path.join(wd, path)
                if not os.path.isfile(path):
                    _note_unverified(call, rule, _PREFLIGHT_UNVERIFIED, "body-file: not a regular file")
                    unverified_this_call = True
                    break
                try:
                    with open(path, encoding="utf-8", errors="replace") as f:
                        body = f.read(_BODY_FILE_MAX)
                except OSError as e:
                    _note_unverified(call, rule, _PREFLIGHT_UNVERIFIED, f"body-file: {type(e).__name__}")
                    unverified_this_call = True
                    break

            missing = _missing_markers(body)
            if missing:
                return "тело PR без " + " и без ".join(missing)

        if unverified_this_call:
            continue  # this invocation is unverified, not denied -- check the next one

    return False


CONTEXTS.update({
    "head_on_remote": _head_on_remote,
    "linked_worktree": _linked_worktree,
    "write_outside_repo": _write_outside_repo,
    "branch_pr_merged": _branch_pr_merged,
    "merge_preflight": _merge_preflight,
    "pr_create_gate": _pr_create_gate,
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


def _is_role_gated(config: dict, rule_id) -> bool:
    """True when the rule with this `id` restricts by non-empty `roles`/`not_roles`.

    Used only for AC1's `unknown`-role hint (ADR-0006 F5): the criterion is
    "this rule's deny depends on the caller's role", not a hardcoded rule id
    — it applies equally to `merge_role` and `remote_ref_delete`, and gives
    no hint for role-independent rules like `force_push` (would be
    misleading there).
    """
    rules = config.get("rules")
    if not isinstance(rules, list):
        return False
    for rule in rules:
        if isinstance(rule, dict) and rule.get("id") == rule_id:
            return bool(rule.get("roles")) or bool(rule.get("not_roles"))
    return False


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
        "unverified": [],
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
        # ADR-0006 F1: same shape, distinct decision -- a gate rule (merge_preflight/
        # pr_create_gate) chose to allow without being able to verify the PR body.
        # Written even when a later rule ultimately denies the same call (this rule
        # still passed the call through unverified; the `deny` row records the outcome).
        for u in call.get("unverified") or []:
            _safe_write_event({
                "ts": _now_ts(),
                "session_id": session_id,
                "event": "pre-tool",
                "role": role,
                "dispatch_id": did,
                "tool": tool_name,
                "rule": u.get("rule"),
                "decision": "allow_unverified",
                "detail": u.get("detail"),
                "target": _target(tool_name, tool_input, command),
                "input_hash": input_hash,
                "run_id": None,
            }, root)

    if hit is None:
        return None

    reason = hit["reason"]
    if role == "unknown" and _is_role_gated(config, hit["id"]):
        reason = (f"{str(reason).rstrip().rstrip('.')}; роль не разрешена "
                  f"(роль вызывающего не определена — unknown), проверь `zprof doctor`")
    clean_reason = str(reason).rstrip().rstrip(".")
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
# subagent-stop entrypoint (ADR-0007, #26)
# ---------------------------------------------------------------------------

_RETURN_FORMAT_HEADER = "return_format: |"
_TOP_LEVEL_KEY_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_-]*:")
_VERDICT_LINE_RE = re.compile(r"^\s*(verdict|completion):\s*(.+?)\s*$")
_WILDCARD_VALUE_RE = re.compile(r"^<.*>$")


def _return_format_contract(text: str) -> "tuple[str, str, list[str] | None] | None":
    """Parse the `return_format: |` block out of an agent contract's frontmatter.

    Pure function, no I/O (ADR-0007 G3/G4). Returns `(key, raw, values)`:
    `key` is `verdict`/`completion`, `raw` is the value exactly as written
    (unnormalized), `values` is the `|`-split allow-list, or `None` when
    the whole value is a bare `<...>` sentinel ("any value" — check 2 is
    skipped). Returns `None` (not an error, just "pass") when there's no
    frontmatter, no `return_format: |` block, or its first significant
    line isn't `verdict:`/`completion:`.
    """
    lines = text.splitlines()
    if not lines or lines[0].rstrip() != "---":
        return None

    close_idx = None
    for i in range(1, len(lines)):
        if lines[i].rstrip() == "---":
            close_idx = i
            break
    if close_idx is None:
        return None

    block_start = None
    for i in range(1, close_idx):
        if lines[i].rstrip() == _RETURN_FORMAT_HEADER:
            block_start = i + 1
            break
    if block_start is None:
        return None

    for i in range(block_start, close_idx):
        line = lines[i]
        if _TOP_LEVEL_KEY_RE.match(line):
            break
        s = line.strip()
        if s == "" or s.startswith("#"):
            continue
        m = _VERDICT_LINE_RE.match(line)
        if not m:
            return None
        key, raw = m.group(1), m.group(2)
        return key, raw, _parse_return_format_values(raw)

    return None


def _parse_return_format_values(raw: str) -> "list[str] | None":
    """`|`-split allow-list for a `return_format` value (ADR-0007 G4).

    A whole-value `<...>` sentinel (before splitting — `<a | b>` must not
    be split into two elements), or a list where any single element is a
    whole-value `<...>` sentinel, both mean "any value" — `None`.
    """
    if _WILDCARD_VALUE_RE.match(raw.strip()):
        return None
    items = [s.strip().lower() for s in raw.split("|")]
    items = [s for s in items if s]
    if any(_WILDCARD_VALUE_RE.match(item) for item in items):
        return None
    return items


def _value_allowed(word: str, values: list[str]) -> bool:
    """True when `word` matches an allow-list element (ADR-0007 G4).

    An element containing `<` (e.g. `blocked-<reason>`) matches as a
    literal prefix: `word.startswith(prefix)` where `prefix` is the part
    before the first `<`, and `word` must be strictly longer than
    `prefix` (`blocked-` alone doesn't count). Every other element must
    match `word` exactly.
    """
    for item in values:
        if "<" in item:
            prefix = item.split("<", 1)[0]
            if word.startswith(prefix) and len(word) > len(prefix):
                return True
        elif word == item:
            return True
    return False


def _load_role_contract(root: str, role: str) -> "tuple[str, str, list[str] | None] | None":
    """Find and parse `role`'s `return_format` contract (ADR-0007 G6).

    Tries `<root>/.claude/agents/<role>.md`, then
    `<root>/.claude/agents/gates/<role>.md`. A file that can't be read
    (`OSError`, incl. `FileNotFoundError`) is the expected "role has no
    contract here" path — caught locally, next candidate tried — not a
    corruption to fail-open on (contrast `_last_assistant_text` below).
    The first file that *is* read wins outright, even if its parse result
    is `None` (no `return_format` block): a role's `agents/<role>.md`
    lacking `return_format` never falls through to `gates/<role>.md`.
    """
    if not role or role in ("main", "unknown") or "/" in role or "\\" in role or role.startswith("."):
        return None
    for rel in (f".claude/agents/{role}.md", f".claude/agents/gates/{role}.md"):
        try:
            text = (Path(root) / rel).read_text(encoding="utf-8")
        except OSError:
            continue
        return _return_format_contract(text)
    return None


def _final_text(payload: dict) -> str | None:
    """Resolve the subagent's final reply text (ADR-0007 G5).

    `payload["last_assistant_message"]`, if a string, wins outright — no
    file I/O, the real-prod path. Otherwise falls back to a transcript:
    `agent_transcript_path` if non-empty, else `transcript_path` but only
    when that path is itself a subagent transcript (`agent-<id>.jsonl`
    under a `subagents/` dir — same test as `resolve_role`); the main
    session's own transcript is never read. `None` when neither source is
    usable — pass, not an error.
    """
    msg = payload.get("last_assistant_message")
    if isinstance(msg, str):
        return msg

    path = payload.get("agent_transcript_path")
    if not (isinstance(path, str) and path):
        tp = payload.get("transcript_path")
        if isinstance(tp, str) and tp:
            p = Path(tp)
            if _AGENT_TRANSCRIPT_RE.match(p.name) and p.parent.name == "subagents":
                path = tp

    if not (isinstance(path, str) and path):
        return None
    return _last_assistant_text(path)


def _last_assistant_text(path: str) -> str | None:
    """Last `type == "assistant"` record's text from a subagent transcript.

    Deliberately has **no** local `try/except` (ADR-0007 G5): a missing
    file, unreadable permissions, invalid JSON on a line, or invalid
    UTF-8 must propagate out of `subagent_stop()` to `main()`'s outer
    `except Exception` (AC7) — a corrupt/unreadable transcript is
    unexpected, unlike a missing contract file (`_load_role_contract`).
    Returns `None` (a clean pass, not an error) when the file parses fine
    but contains no `assistant` record at all.
    """
    last = None
    with open(path, encoding="utf-8") as f:
        for line in f:
            if line.strip() == "":
                continue
            rec = json.loads(line)
            if isinstance(rec, dict) and rec.get("type") == "assistant":
                last = rec
    if last is None:
        return None

    message = last.get("message")
    if not isinstance(message, dict):
        return ""
    content = message.get("content")
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return "".join(
            b["text"] for b in content
            if isinstance(b, dict) and b.get("type") == "text" and isinstance(b.get("text"), str)
        )
    return ""


def subagent_stop(payload: dict) -> dict | None:
    """Evaluate one `SubagentStop` call against its role's `return_format` (ADR-0007 G8).

    Returns `{"decision": "block", "reason": ...}` on a first-time format
    violation (also logs `event: "subagent-stop"`), or `None` on: no
    contract for this role, no usable final text, a passing check
    (silent — nothing written to the journal), or a repeat violation with
    `stop_hook_active is True` (logs `event: "format_unfixed"` instead,
    to avoid blocking forever). The contract is loaded *before* the final
    text so a role without one never opens a transcript at all; reading a
    corrupt transcript raises out of this function on purpose (G5).
    """
    role = resolve_role(payload)
    root = project_root(payload)

    contract = _load_role_contract(root, role)
    if contract is None:
        return None
    key, raw, values = contract

    text = _final_text(payload)
    if text is None or not text.strip():
        return None
    first = next(line.strip() for line in text.splitlines() if line.strip())

    detail = None
    if not first.lower().startswith(f"{key}:"):
        detail = "key"
    elif values is not None:
        rest = first.split(":", 1)[1].strip()
        word = rest.split()[0].lower() if rest else ""
        if not word or not _value_allowed(word, values):
            detail = "value"

    if detail is None:
        return None

    session_id = payload.get("session_id")
    did = dispatch_id({
        "transcript_path": payload.get("agent_transcript_path") or payload.get("transcript_path"),
    })

    if payload.get("stop_hook_active") is True:
        _safe_write_event({
            "ts": _now_ts(),
            "session_id": session_id,
            "event": "format_unfixed",
            "role": role,
            "dispatch_id": did,
            "tool": None,
            "rule": "return_format",
            "decision": None,
            "detail": detail,
            "target": None,
            "input_hash": None,
            "run_id": None,
        }, root)
        return None

    _safe_write_event({
        "ts": _now_ts(),
        "session_id": session_id,
        "event": "subagent-stop",
        "role": role,
        "dispatch_id": did,
        "tool": None,
        "rule": "return_format",
        "decision": "block",
        "detail": detail,
        "target": None,
        "input_hash": None,
        "run_id": None,
    }, root)

    shown = first if len(first) <= 120 else first[:120] + "…"
    reason = (
        f"zprof guard [return_format]: ответ {role} должен начинаться строкой "
        f"`{key}: {raw}`. Сейчас первая строка: «{shown}». "
        f"Перепиши ответ по return_format без преамбулы."
    )
    return {"decision": "block", "reason": reason}


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
        elif mode == "subagent-stop":
            out = subagent_stop(payload)

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
