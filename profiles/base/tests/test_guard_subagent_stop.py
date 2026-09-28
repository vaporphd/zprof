"""Tests for zprof-guard.py `subagent-stop` return_format validator (ADR-0007, #26).

ADR: docs/adr/0007-guard-subagent-stop-validator.md (G1-G9)
Parent ADR: docs/adr/0004-zprof-guard-pre-tool-frame.md (D2 fail-open, D7 роль/диспатч, D9 журнал)
Plan: tasks/plan-issue-26.md, step 4; issue #26 AC1-AC8

Reuses the module-loading pattern, `_read_events` and the subprocess runner
`_run_guard` from `test_guard.py` (imported by path -- no `__init__.py` in
this directory -- instead of duplicating them). The `subagent-stop` payload
shape differs enough from `pre-tool`'s (`last_assistant_message` /
`agent_transcript_path` / `stop_hook_active` instead of `tool_name` /
`tool_input`) that its own small payload builder lives here rather than
reusing `test_guard._payload`.
"""
import json
import pathlib
import sys

import pytest

sys.path.insert(0, str(pathlib.Path(__file__).parent))
from test_guard import (
    _read_events,
    _run_guard,
    zprof_guard,
)

BASE_DIR = pathlib.Path(__file__).parent.parent
AGENTS_DIR = BASE_DIR / "agents"
GATES_DIR = AGENTS_DIR / "gates"


@pytest.fixture(autouse=True)
def _no_project_dir_env(monkeypatch):
    """Same precaution as test_guard.py: autouse fixtures don't apply across
    test files, so this must be redeclared here too -- direct function-call
    tests must not pick up a real CLAUDE_PROJECT_DIR from the environment."""
    monkeypatch.delenv("CLAUDE_PROJECT_DIR", raising=False)


# ---------------------------------------------------------------------------
# helpers
# ---------------------------------------------------------------------------

def _write_contract(root: pathlib.Path, role: str, text: str, gate: bool = False) -> pathlib.Path:
    rel = f".claude/agents/gates/{role}.md" if gate else f".claude/agents/{role}.md"
    path = root / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")
    return path


def _copy_real_contract(root: pathlib.Path, role: str, gate: bool = False) -> str:
    """Copy a real `profiles/base/agents/[gates/]<role>.md` into `<root>/.claude/agents/...`.

    Returns the file text, so callers can derive the exact (key, raw, values)
    via `zprof_guard._return_format_contract` instead of hardcoding it --
    real contracts drift, the parser is the source of truth.
    """
    src = (GATES_DIR if gate else AGENTS_DIR) / f"{role}.md"
    text = src.read_text(encoding="utf-8")
    _write_contract(root, role, text, gate=gate)
    return text


def _sa_payload(role=None, cwd=None, session_id="sess-1", last_assistant_message=None,
                 transcript_path=None, agent_transcript_path=None, stop_hook_active=None):
    payload = {"session_id": session_id, "cwd": str(cwd)}
    if role is not None:
        payload["agent_type"] = role
    if last_assistant_message is not None:
        payload["last_assistant_message"] = last_assistant_message
    if transcript_path is not None:
        payload["transcript_path"] = str(transcript_path)
    if agent_transcript_path is not None:
        payload["agent_transcript_path"] = str(agent_transcript_path)
    if stop_hook_active is not None:
        payload["stop_hook_active"] = stop_hook_active
    return payload


def _expected_reason(role: str, key: str, raw: str, first: str) -> str:
    """Mirrors `subagent_stop()`'s reason template exactly (ADR-0007 G1)."""
    shown = first if len(first) <= 120 else first[:120] + "…"
    return (
        f"zprof guard [return_format]: ответ {role} должен начинаться строкой "
        f"`{key}: {raw}`. Сейчас первая строка: «{shown}». "
        f"Перепиши ответ по return_format без преамбулы."
    )


def _write_transcript(path: pathlib.Path, records: list) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        "\n".join(json.dumps(r, ensure_ascii=False) for r in records) + "\n", encoding="utf-8"
    )


def _assistant_record(content) -> dict:
    return {"type": "assistant", "message": {"content": content}}


# ---------------------------------------------------------------------------
# synthetic contract fixtures (module-level text; no live .md needs them)
# ---------------------------------------------------------------------------

COMPLETION_CONTRACT = """---
name: legacy-role
description: synthetic `completion:` contract for AC2/AC4 (no live .md uses this key post-#20 migration)
tools: Read
model: sonnet
return_format: |
  completion: complete|incomplete|blocked
  notes: <free text>
---

# Legacy Role (synthetic fixture)
"""

FREEFORM_CONTRACT = """---
name: freeform-role
description: synthetic return_format whose value is a bare <...> sentinel (check 2 skipped)
tools: Read
model: sonnet
return_format: |
  verdict: <one short paragraph, no fixed vocabulary>
  one_line: <≤120 chars>
---

# Freeform Role (synthetic fixture)
"""

NO_RETURN_FORMAT_CONTRACT = """---
name: no-format-role
description: has frontmatter, no return_format block at all
tools: Read
---

# No Format Role (synthetic fixture)
"""

RETURN_FORMAT_NO_VERDICT_CONTRACT = """---
name: no-verdict-role
description: has a return_format block, but its first significant line isn't verdict/completion
tools: Read
return_format: |
  artifact: <path>
  one_line: <≤120 chars>
---

# No Verdict Role (synthetic fixture)
"""

VALID_VERDICT_CONTRACT = """---
name: shadowed-role
description: synthetic contract with a normal verdict list, used as the gates/ decoy in G6 p.4
tools: Read
return_format: |
  verdict: approved|denied
  one_line: <≤120 chars>
---

# Shadowed Role (synthetic fixture)
"""


# ---------------------------------------------------------------------------
# _return_format_contract: frontmatter / block parsing (ADR G3)
# ---------------------------------------------------------------------------

def test_return_format_contract_no_frontmatter():
    assert zprof_guard._return_format_contract("just a markdown file\nno frontmatter\n") is None


def test_return_format_contract_unclosed_frontmatter():
    text = "---\nname: x\nreturn_format: |\n  verdict: a|b\n"  # no closing ---
    assert zprof_guard._return_format_contract(text) is None


def test_return_format_contract_absent_block_entirely():
    text = "---\nname: x\ndescription: y\n---\nbody\n"
    assert zprof_guard._return_format_contract(text) is None


def test_return_format_contract_block_present_but_first_line_not_verdict():
    text = "---\nname: x\nreturn_format: |\n  artifact: <path>\n  one_line: <text>\n---\nbody\n"
    assert zprof_guard._return_format_contract(text) is None


def test_return_format_contract_non_pipe_indicator_treated_as_absent():
    """Only exactly `return_format: |` is recognized -- `|-`/`>`/inline are "no block" (G3 p.3)."""
    text = "---\nname: x\nreturn_format: |-\n  verdict: a|b\n---\nbody\n"
    assert zprof_guard._return_format_contract(text) is None


def test_return_format_contract_stops_at_next_top_level_key_before_any_content():
    text = "---\nname: x\nreturn_format: |\ntools: Read\n---\nbody\n"
    assert zprof_guard._return_format_contract(text) is None


def test_return_format_contract_skips_blank_and_comment_lines():
    text = (
        "---\nname: x\nreturn_format: |\n"
        "  # CRITICAL: no preamble\n"
        "\n"
        "  verdict: done|blocked\n"
        "---\nbody\n"
    )
    assert zprof_guard._return_format_contract(text) == ("verdict", "done|blocked", ["done", "blocked"])


def test_return_format_contract_recognizes_completion_key():
    text = "---\nname: x\nreturn_format: |\n  completion: complete|incomplete\n---\nbody\n"
    assert zprof_guard._return_format_contract(text) == (
        "completion", "complete|incomplete", ["complete", "incomplete"]
    )


def test_return_format_contract_raw_is_unnormalized():
    """`raw` (group 2) is kept exactly as written, spaces and all -- not the parsed `values`."""
    text = "---\nname: x\nreturn_format: |\n  verdict: approved | changes-required\n---\nbody\n"
    _key, raw, values = zprof_guard._return_format_contract(text)
    assert raw == "approved | changes-required"
    assert values == ["approved", "changes-required"]


# ---------------------------------------------------------------------------
# _parse_return_format_values (ADR G4)
# ---------------------------------------------------------------------------

def test_parse_return_format_values_wildcard_whole_value():
    assert zprof_guard._parse_return_format_values("<free text>") is None
    assert zprof_guard._parse_return_format_values("  <free text>  ") is None


def test_parse_return_format_values_wildcard_not_split_by_inner_pipe():
    """`<a | b>` is a single sentinel, not two elements split on `|` (G4 p.1)."""
    assert zprof_guard._parse_return_format_values("<a | b>") is None


def test_parse_return_format_values_wildcard_element_in_list():
    assert zprof_guard._parse_return_format_values("done|<anything>|blocked") is None


def test_parse_return_format_values_strips_whitespace_around_pipe():
    assert zprof_guard._parse_return_format_values("approved | changes-required") == [
        "approved", "changes-required"
    ]


def test_parse_return_format_values_drops_empty_elements():
    assert zprof_guard._parse_return_format_values("a|b|") == ["a", "b"]


def test_parse_return_format_values_lowercases():
    assert zprof_guard._parse_return_format_values("Done|BLOCKED") == ["done", "blocked"]


def test_parse_return_format_values_keeps_bracketed_prefix_element():
    assert zprof_guard._parse_return_format_values("merged|blocked-<reason>") == [
        "merged", "blocked-<reason>"
    ]


# ---------------------------------------------------------------------------
# _value_allowed: exact match + `blocked-<reason>` prefix match (ADR G4)
# ---------------------------------------------------------------------------

def test_value_allowed_exact_match():
    assert zprof_guard._value_allowed("done", ["done", "blocked", "failed"]) is True
    assert zprof_guard._value_allowed("unknown", ["done", "blocked", "failed"]) is False


def test_value_allowed_prefix_for_bracketed_element():
    values = ["merged-stamped", "blocked-<reason>"]
    assert zprof_guard._value_allowed("blocked-worktree-locked", values) is True
    assert zprof_guard._value_allowed("merged-stamped", values) is True
    assert zprof_guard._value_allowed("blocked-", values) is False  # not strictly longer than prefix
    assert zprof_guard._value_allowed("blockedfoo", values) is False  # no '-' separator


# ---------------------------------------------------------------------------
# _load_role_contract: role-name guard clause + candidate order (ADR G6)
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("role", ["main", "unknown", "", "a/b", "a\\b", "../etc", ".hidden"])
def test_load_role_contract_rejects_unsafe_or_reserved_roles(tmp_path, role):
    assert zprof_guard._load_role_contract(str(tmp_path), role) is None


def test_load_role_contract_gates_fallback_when_agents_file_missing(tmp_path):
    _write_contract(tmp_path, "gate-only-role", VALID_VERDICT_CONTRACT, gate=True)
    assert zprof_guard._load_role_contract(str(tmp_path), "gate-only-role") == (
        "verdict", "approved|denied", ["approved", "denied"]
    )


def test_load_role_contract_none_when_neither_file_exists(tmp_path):
    assert zprof_guard._load_role_contract(str(tmp_path), "ghost-role") is None


def test_load_role_contract_first_file_wins_even_without_return_format(tmp_path):
    """G6 p.4: `agents/<role>.md` without a `return_format` block never falls
    through to `gates/<role>.md`, even though the latter has a valid one."""
    _write_contract(tmp_path, "shadowed-role", NO_RETURN_FORMAT_CONTRACT)
    _write_contract(tmp_path, "shadowed-role", VALID_VERDICT_CONTRACT, gate=True)
    assert zprof_guard._load_role_contract(str(tmp_path), "shadowed-role") is None


# ---------------------------------------------------------------------------
# _final_text: source resolution + fail-open boundary (ADR G5)
# ---------------------------------------------------------------------------

def test_final_text_last_assistant_message_wins_without_touching_disk(tmp_path):
    payload = {
        "last_assistant_message": "verdict: done",
        "agent_transcript_path": str(tmp_path / "does-not-exist.jsonl"),
    }
    assert zprof_guard._final_text(payload) == "verdict: done"


def test_final_text_agent_transcript_path_preferred_over_transcript_path(tmp_path):
    good = tmp_path / "sess" / "subagents" / "agent-good01.jsonl"
    _write_transcript(good, [_assistant_record("verdict: from-agent-path")])
    bad = tmp_path / "sess.jsonl"  # would be a main-session transcript if it were used
    bad.write_text("not valid json\n", encoding="utf-8")
    payload = {"agent_transcript_path": str(good), "transcript_path": str(bad)}
    assert zprof_guard._final_text(payload) == "verdict: from-agent-path"


def test_final_text_ignores_main_level_transcript_path(tmp_path):
    """Key ADR-0007 finding (G5, Context п.5): `transcript_path` on `SubagentStop`
    is the *main* session's transcript, not the subagent's -- it must never be
    read as a fallback, even when it's the only field present."""
    main_level = tmp_path / "sess-main.jsonl"  # parent dir isn't "subagents"
    main_level.write_text("garbage, must never be opened\n", encoding="utf-8")
    payload = {"transcript_path": str(main_level)}
    assert zprof_guard._final_text(payload) is None


def test_final_text_accepts_subagent_shaped_transcript_path(tmp_path):
    sub = tmp_path / "sess" / "subagents" / "agent-abc01.jsonl"
    _write_transcript(sub, [_assistant_record("verdict: done")])
    payload = {"transcript_path": str(sub)}
    assert zprof_guard._final_text(payload) == "verdict: done"


def test_final_text_none_without_any_source(tmp_path):
    assert zprof_guard._final_text({}) is None
    assert zprof_guard._final_text({"transcript_path": ""}) is None


# ---------------------------------------------------------------------------
# _last_assistant_text: transcript record parsing
# ---------------------------------------------------------------------------

def test_last_assistant_text_concatenates_text_blocks_and_skips_others(tmp_path):
    jsonl = tmp_path / "agent-x.jsonl"
    _write_transcript(jsonl, [
        {"type": "user", "message": {"content": "ignored"}},
        _assistant_record([
            {"type": "text", "text": "verdict: "},
            {"type": "tool_use", "name": "Bash", "input": {}},
            {"type": "text", "text": "done"},
        ]),
    ])
    assert zprof_guard._last_assistant_text(str(jsonl)) == "verdict: done"


def test_last_assistant_text_string_content(tmp_path):
    jsonl = tmp_path / "agent-y.jsonl"
    _write_transcript(jsonl, [_assistant_record("verdict: done")])
    assert zprof_guard._last_assistant_text(str(jsonl)) == "verdict: done"


def test_last_assistant_text_last_of_multiple_assistant_records_wins(tmp_path):
    jsonl = tmp_path / "agent-z.jsonl"
    _write_transcript(jsonl, [
        _assistant_record("first, intermediate turn"),
        {"type": "user", "message": {"content": "continue"}},
        _assistant_record("verdict: done"),
    ])
    assert zprof_guard._last_assistant_text(str(jsonl)) == "verdict: done"


def test_last_assistant_text_none_without_any_assistant_record(tmp_path):
    jsonl = tmp_path / "agent-w.jsonl"
    _write_transcript(jsonl, [{"type": "user", "message": {"content": "hi"}}])
    assert zprof_guard._last_assistant_text(str(jsonl)) is None


def test_last_assistant_text_blank_lines_skipped(tmp_path):
    jsonl = tmp_path / "agent-v.jsonl"
    jsonl.write_text(
        "\n" + json.dumps(_assistant_record("verdict: done")) + "\n\n", encoding="utf-8"
    )
    assert zprof_guard._last_assistant_text(str(jsonl)) == "verdict: done"


# ---------------------------------------------------------------------------
# subagent_stop(): silent / pass cases (AC1, AC2, AC3, AC4, AC6 "success")
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("value", ["done", "blocked", "failed"])
def test_subagent_stop_silent_on_valid_verdict_task_runner(tmp_path, value):
    _copy_real_contract(tmp_path, "task-runner")
    payload = _sa_payload(role="task-runner", cwd=tmp_path,
                           last_assistant_message=f"verdict: {value}\nartifact: PR#1\none_line: ok")
    assert zprof_guard.subagent_stop(payload) is None
    assert not (tmp_path / ".agentlog").exists()


def test_subagent_stop_silent_on_completion_key(tmp_path):
    _write_contract(tmp_path, "legacy-role", COMPLETION_CONTRACT)
    payload = _sa_payload(role="legacy-role", cwd=tmp_path,
                           last_assistant_message="completion: complete\nnotes: all good")
    assert zprof_guard.subagent_stop(payload) is None


def test_subagent_stop_blocks_on_invalid_completion_value(tmp_path):
    _write_contract(tmp_path, "legacy-role", COMPLETION_CONTRACT)
    payload = _sa_payload(role="legacy-role", cwd=tmp_path,
                           last_assistant_message="completion: partial\nnotes: x")
    out = zprof_guard.subagent_stop(payload)
    assert out is not None
    assert out["decision"] == "block"


def test_subagent_stop_silent_on_freeform_wildcard_value(tmp_path):
    _write_contract(tmp_path, "freeform-role", FREEFORM_CONTRACT)
    payload = _sa_payload(role="freeform-role", cwd=tmp_path,
                           last_assistant_message="verdict: this can be literally anything at all\none_line: ok")
    assert zprof_guard.subagent_stop(payload) is None


def test_subagent_stop_freeform_still_requires_key_prefix(tmp_path):
    """Check 1 (`<key>:` prefix) is mandatory even when check 2 is skipped (wildcard value)."""
    _write_contract(tmp_path, "freeform-role", FREEFORM_CONTRACT)
    payload = _sa_payload(role="freeform-role", cwd=tmp_path,
                           last_assistant_message="Здесь без ключа.\nverdict: whatever")
    out = zprof_guard.subagent_stop(payload)
    assert out is not None
    assert out["decision"] == "block"


@pytest.mark.parametrize("value", ["approved", "changes-required"])
def test_subagent_stop_silent_gates_fallback_plan_reviewer_spaces_around_pipe(tmp_path, value):
    """AC1 gates/ fallback + regression: `plan-reviewer.md` has spaces around `|`."""
    _copy_real_contract(tmp_path, "plan-reviewer", gate=True)
    payload = _sa_payload(role="plan-reviewer", cwd=tmp_path,
                           last_assistant_message=f"verdict: {value}\nartifact: docs/review.md")
    assert zprof_guard.subagent_stop(payload) is None
    assert not (tmp_path / ".agentlog" / "guard-events.jsonl").exists()


def test_subagent_stop_silent_critical_comment_pr_shepherd(tmp_path):
    """`# CRITICAL: ...` comment before the first significant line doesn't break parsing."""
    _copy_real_contract(tmp_path, "pr-shepherd")
    payload = _sa_payload(role="pr-shepherd", cwd=tmp_path,
                           last_assistant_message="verdict: merged-stamped\npr: #26\nstamp_sha: abc123")
    assert zprof_guard.subagent_stop(payload) is None


def test_subagent_stop_silent_critical_comment_auditor(tmp_path):
    _copy_real_contract(tmp_path, "auditor")
    payload = _sa_payload(role="auditor", cwd=tmp_path,
                           last_assistant_message="verdict: complete\nintegrity: clean\nevidence: x")
    assert zprof_guard.subagent_stop(payload) is None


def test_subagent_stop_silent_role_without_contract_file(tmp_path):
    payload = _sa_payload(role="ghost-role", cwd=tmp_path, last_assistant_message="verdict: done")
    assert zprof_guard.subagent_stop(payload) is None
    assert not (tmp_path / ".agentlog" / "guard-events.jsonl").exists()


def test_subagent_stop_silent_contract_without_return_format_block(tmp_path):
    _write_contract(tmp_path, "no-format-role", NO_RETURN_FORMAT_CONTRACT)
    payload = _sa_payload(role="no-format-role", cwd=tmp_path, last_assistant_message="anything at all")
    assert zprof_guard.subagent_stop(payload) is None


def test_subagent_stop_silent_return_format_without_verdict_first_line(tmp_path):
    _write_contract(tmp_path, "no-verdict-role", RETURN_FORMAT_NO_VERDICT_CONTRACT)
    payload = _sa_payload(role="no-verdict-role", cwd=tmp_path, last_assistant_message="anything at all")
    assert zprof_guard.subagent_stop(payload) is None


def test_subagent_stop_first_contract_file_wins_ignores_gates_content(tmp_path):
    """Functional variant of G6 p.4: an otherwise-blockable response stays
    silent because `agents/<role>.md` (no return_format) wins outright and
    `gates/<role>.md` (which *would* block this response) is never consulted."""
    _write_contract(tmp_path, "shadowed-role", NO_RETURN_FORMAT_CONTRACT)
    _write_contract(tmp_path, "shadowed-role", VALID_VERDICT_CONTRACT, gate=True)
    payload = _sa_payload(role="shadowed-role", cwd=tmp_path,
                           last_assistant_message="totally not verdict shaped")
    assert zprof_guard.subagent_stop(payload) is None
    assert not (tmp_path / ".agentlog" / "guard-events.jsonl").exists()


def test_subagent_stop_silent_transcript_without_assistant_record(tmp_path):
    _copy_real_contract(tmp_path, "task-runner")
    jsonl = tmp_path / "sess" / "subagents" / "agent-noassist01.jsonl"
    _write_transcript(jsonl, [
        {"type": "user", "message": {"content": "go"}},
        {"type": "system", "message": {"content": "setup"}},
    ])
    payload = _sa_payload(role="task-runner", cwd=tmp_path, agent_transcript_path=jsonl)
    assert zprof_guard.subagent_stop(payload) is None
    assert not (tmp_path / ".agentlog" / "guard-events.jsonl").exists()


def test_subagent_stop_silent_empty_final_text(tmp_path):
    """ADR G7: whitespace-only final text is a silent pass, not a block."""
    _copy_real_contract(tmp_path, "task-runner")
    payload = _sa_payload(role="task-runner", cwd=tmp_path, last_assistant_message="   \n  \n")
    assert zprof_guard.subagent_stop(payload) is None


def test_subagent_stop_silent_when_assistant_content_has_no_text_blocks(tmp_path):
    """G7 via the transcript path: an assistant record exists, but its
    content is only non-text blocks (tool_use) -> concatenation is ''."""
    _copy_real_contract(tmp_path, "task-runner")
    jsonl = tmp_path / "sess" / "subagents" / "agent-notext01.jsonl"
    _write_transcript(jsonl, [
        {"type": "assistant", "message": {"content": [{"type": "tool_use", "name": "Bash"}]}},
    ])
    payload = _sa_payload(role="task-runner", cwd=tmp_path, agent_transcript_path=jsonl)
    assert zprof_guard.subagent_stop(payload) is None


def test_subagent_stop_silent_main_level_transcript_path_not_used_even_when_corrupt(tmp_path):
    """Key ADR-0007 finding (G5): with `agent_type` set explicitly (so role
    resolution doesn't need `transcript_path`), a `transcript_path` pointing
    at a main-session-shaped file is never opened as a fallback -- proven by
    stuffing it with invalid JSON that would raise if it were ever read."""
    _copy_real_contract(tmp_path, "task-runner")
    main_transcript = tmp_path / "sess-main.jsonl"
    main_transcript.write_text("not-json-at-all, must never be parsed\n", encoding="utf-8")
    payload = {"agent_type": "task-runner", "cwd": str(tmp_path), "session_id": "sess-main",
               "transcript_path": str(main_transcript)}
    assert zprof_guard.subagent_stop(payload) is None
    assert not (tmp_path / ".agentlog" / "guard-events.jsonl").exists()


# ---------------------------------------------------------------------------
# subagent_stop(): block cases (AC4, AC5)
# ---------------------------------------------------------------------------

def test_subagent_stop_blocks_on_preamble_before_verdict(tmp_path):
    text = _copy_real_contract(tmp_path, "task-runner")
    key, raw, _values = zprof_guard._return_format_contract(text)
    payload = _sa_payload(role="task-runner", cwd=tmp_path,
                           last_assistant_message="Я всё сделал и запушил PR.\nverdict: done\nartifact: x")
    out = zprof_guard.subagent_stop(payload)
    assert out == {
        "decision": "block",
        "reason": _expected_reason("task-runner", key, raw, "Я всё сделал и запушил PR."),
    }
    events = _read_events(tmp_path)
    assert len(events) == 1
    ev = events[0]
    for field in ("ts", "session_id", "event", "role", "dispatch_id", "tool", "rule",
                  "decision", "detail", "target", "input_hash", "run_id"):
        assert field in ev, field
    assert ev["event"] == "subagent-stop"
    assert ev["decision"] == "block"
    assert ev["detail"] == "key"
    assert ev["role"] == "task-runner"
    assert ev["rule"] == "return_format"
    assert ev["tool"] is None
    assert ev["target"] is None
    assert ev["input_hash"] is None
    assert ev["run_id"] is None


def test_subagent_stop_blocks_on_invalid_value(tmp_path):
    text = _copy_real_contract(tmp_path, "task-runner")
    key, raw, _values = zprof_guard._return_format_contract(text)
    payload = _sa_payload(role="task-runner", cwd=tmp_path,
                           last_assistant_message="verdict: unknown-status\nartifact: x")
    out = zprof_guard.subagent_stop(payload)
    assert out == {
        "decision": "block",
        "reason": _expected_reason("task-runner", key, raw, "verdict: unknown-status"),
    }
    events = _read_events(tmp_path)
    assert len(events) == 1
    assert events[0]["detail"] == "value"
    assert events[0]["decision"] == "block"


def test_subagent_stop_block_reason_truncates_long_first_line(tmp_path):
    _copy_real_contract(tmp_path, "task-runner")
    long_line = "x" * 150
    payload = _sa_payload(role="task-runner", cwd=tmp_path,
                           last_assistant_message=f"{long_line}\nverdict: done")
    out = zprof_guard.subagent_stop(payload)
    assert out is not None
    expected_shown = long_line[:120] + "…"
    assert f"«{expected_shown}»" in out["reason"]
    assert long_line not in out["reason"]


# ---------------------------------------------------------------------------
# subagent_stop(): stop_hook_active (AC6)
# ---------------------------------------------------------------------------

def test_subagent_stop_stop_hook_active_true_passes_with_format_unfixed_event(tmp_path):
    _copy_real_contract(tmp_path, "task-runner")
    payload = _sa_payload(role="task-runner", cwd=tmp_path,
                           last_assistant_message="preamble\nverdict: done", stop_hook_active=True)
    out = zprof_guard.subagent_stop(payload)
    assert out is None
    events = _read_events(tmp_path)
    assert len(events) == 1
    ev = events[0]
    assert ev["event"] == "format_unfixed"
    assert ev["decision"] is None
    assert ev["detail"] == "key"
    assert ev["role"] == "task-runner"
    assert ev["rule"] == "return_format"
    assert not (tmp_path / ".agentlog" / "guard-state.json").exists()


def test_subagent_stop_stop_hook_active_false_still_blocks(tmp_path):
    _copy_real_contract(tmp_path, "task-runner")
    payload = _sa_payload(role="task-runner", cwd=tmp_path,
                           last_assistant_message="preamble\nverdict: done", stop_hook_active=False)
    out = zprof_guard.subagent_stop(payload)
    assert out is not None
    assert out["decision"] == "block"
    events = _read_events(tmp_path)
    assert len(events) == 1
    assert events[0]["event"] == "subagent-stop"


def test_subagent_stop_block_event_dispatch_id_from_sibling_meta_json(tmp_path):
    _copy_real_contract(tmp_path, "task-runner")
    sub = tmp_path / "home" / "proj" / "sess-x" / "subagents"
    jsonl = sub / "agent-abc123.jsonl"
    _write_transcript(jsonl, [_assistant_record("preamble\nverdict: done")])
    (sub / "agent-abc123.meta.json").write_text(
        json.dumps({"agentType": "task-runner", "toolUseId": "toolu_XYZ"}), encoding="utf-8"
    )
    payload = {"agent_type": "task-runner", "cwd": str(tmp_path), "session_id": "sess-disp",
               "agent_transcript_path": str(jsonl)}
    out = zprof_guard.subagent_stop(payload)
    assert out is not None
    events = _read_events(tmp_path)
    assert len(events) == 1
    assert events[0]["dispatch_id"] == "toolu_XYZ"


# ---------------------------------------------------------------------------
# subagent_stop(): `blocked-<reason>` regression on the real pr-shepherd.md
# contract (ADR G4, the case that motivated prefix matching)
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("word,expect_block", [
    ("blocked-worktree-locked", False),  # prefix match on `blocked-<reason>`
    ("blocked-external", False),  # literal element, exact match
    ("blocked-", True),  # not strictly longer than the prefix
    ("blockedfoo", True),  # no '-' separator, doesn't match the prefix
])
def test_subagent_stop_blocked_reason_prefix_regression_pr_shepherd(tmp_path, word, expect_block):
    _copy_real_contract(tmp_path, "pr-shepherd")
    payload = _sa_payload(role="pr-shepherd", cwd=tmp_path,
                           last_assistant_message=f"verdict: {word}\npr: #26")
    out = zprof_guard.subagent_stop(payload)
    if expect_block:
        assert out is not None
        assert out["decision"] == "block"
    else:
        assert out is None


# ---------------------------------------------------------------------------
# journal regression: success never touches .agentlog/ at all (AC6)
# ---------------------------------------------------------------------------

def test_subagent_stop_success_never_touches_agentlog(tmp_path):
    _copy_real_contract(tmp_path, "task-runner")
    payload = _sa_payload(role="task-runner", cwd=tmp_path,
                           last_assistant_message="verdict: done\nartifact: x")
    assert zprof_guard.subagent_stop(payload) is None
    assert not (tmp_path / ".agentlog").exists()


# ---------------------------------------------------------------------------
# fail-open boundary: corrupt transcript raises out of subagent_stop() (ADR
# G5) -- direct calls prove the raise, subprocess-level E2E proves exit 0 +
# `error` event (AC7)
# ---------------------------------------------------------------------------

def test_last_assistant_text_raises_on_invalid_json_line(tmp_path):
    jsonl = tmp_path / "broken.jsonl"
    jsonl.write_text('{"type": "assistant", "message": {"content": "ok"}}\nnot json\n', encoding="utf-8")
    with pytest.raises(json.JSONDecodeError):
        zprof_guard._last_assistant_text(str(jsonl))


def test_last_assistant_text_raises_on_missing_file(tmp_path):
    with pytest.raises(OSError):
        zprof_guard._last_assistant_text(str(tmp_path / "does-not-exist.jsonl"))


def test_subagent_stop_raises_on_corrupt_transcript(tmp_path):
    _copy_real_contract(tmp_path, "task-runner")
    jsonl = tmp_path / "sess" / "subagents" / "agent-corrupt01.jsonl"
    jsonl.parent.mkdir(parents=True)
    jsonl.write_text("not json at all\n", encoding="utf-8")
    payload = _sa_payload(role="task-runner", cwd=tmp_path, agent_transcript_path=jsonl)
    with pytest.raises(json.JSONDecodeError):
        zprof_guard.subagent_stop(payload)


def test_subagent_stop_raises_on_missing_transcript_file(tmp_path):
    _copy_real_contract(tmp_path, "task-runner")
    missing = tmp_path / "sess" / "subagents" / "agent-missing01.jsonl"
    payload = _sa_payload(role="task-runner", cwd=tmp_path, agent_transcript_path=missing)
    with pytest.raises(OSError):
        zprof_guard.subagent_stop(payload)


def test_e2e_subagent_stop_fail_open_on_corrupt_transcript(tmp_path):
    _copy_real_contract(tmp_path, "task-runner")
    jsonl = tmp_path / "sess" / "subagents" / "agent-corrupt02.jsonl"
    jsonl.parent.mkdir(parents=True)
    jsonl.write_text("not json at all\n", encoding="utf-8")
    payload = {"agent_type": "task-runner", "cwd": str(tmp_path), "session_id": "sess-corrupt",
               "agent_transcript_path": str(jsonl)}
    result = _run_guard(tmp_path, json.dumps(payload), mode="subagent-stop")
    assert result.returncode == 0
    assert result.stdout == ""
    events = _read_events(tmp_path)
    assert len(events) == 1
    assert events[0]["decision"] == "error"
    assert events[0]["event"] == "subagent-stop"
    assert events[0]["detail"] == "JSONDecodeError"


def test_e2e_subagent_stop_fail_open_on_missing_transcript_file(tmp_path):
    _copy_real_contract(tmp_path, "task-runner")
    missing = tmp_path / "sess" / "subagents" / "agent-missing02.jsonl"
    payload = {"agent_type": "task-runner", "cwd": str(tmp_path), "session_id": "sess-missing",
               "agent_transcript_path": str(missing)}
    result = _run_guard(tmp_path, json.dumps(payload), mode="subagent-stop")
    assert result.returncode == 0
    assert result.stdout == ""
    events = _read_events(tmp_path)
    assert len(events) == 1
    assert events[0]["decision"] == "error"
    assert events[0]["event"] == "subagent-stop"
    assert events[0]["detail"] == "FileNotFoundError"


# ---------------------------------------------------------------------------
# True end-to-end via subprocess: exact stdout + journal for the block and
# silent-success paths (mirrors test_guard.py's `_run_guard` usage)
# ---------------------------------------------------------------------------

def test_e2e_subagent_stop_block_stdout_and_event(tmp_path):
    text = _copy_real_contract(tmp_path, "task-runner")
    key, raw, _values = zprof_guard._return_format_contract(text)
    payload = {"agent_type": "task-runner", "cwd": str(tmp_path), "session_id": "sess-e2e",
               "last_assistant_message": "Сделано.\nverdict: done"}
    result = _run_guard(tmp_path, json.dumps(payload), mode="subagent-stop")
    assert result.returncode == 0
    assert result.stderr == ""
    out = json.loads(result.stdout)
    assert out["decision"] == "block"
    assert out["reason"] == _expected_reason("task-runner", key, raw, "Сделано.")

    events = _read_events(tmp_path)
    assert len(events) == 1
    ev = events[0]
    assert ev["event"] == "subagent-stop"
    assert ev["decision"] == "block"
    assert ev["role"] == "task-runner"
    assert ev["rule"] == "return_format"
    assert ev["detail"] == "key"
    assert ev["tool"] is None
    assert ev["target"] is None
    assert ev["input_hash"] is None
    assert ev["run_id"] is None


def test_e2e_subagent_stop_silent_success_empty_stdout_no_journal(tmp_path):
    _copy_real_contract(tmp_path, "task-runner")
    payload = {"agent_type": "task-runner", "cwd": str(tmp_path), "session_id": "sess-ok",
               "last_assistant_message": "verdict: done\nartifact: x"}
    result = _run_guard(tmp_path, json.dumps(payload), mode="subagent-stop")
    assert result.returncode == 0
    assert result.stdout == ""
    assert result.stderr == ""
    assert not (tmp_path / ".agentlog").exists()
