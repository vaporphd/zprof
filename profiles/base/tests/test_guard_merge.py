"""Tests for `merge_preflight`/`pr_create_gate` (ADR-0006, #25).

ADR: docs/adr/0006-guard-merge-pr-gate.md (F1-F10)
Plan: tasks/plan-issue-25.md, step 6; issue #25 AC1-AC7

Reuses the module-loading pattern and helpers from `test_guard.py` (imported
by path, same as `test_guard_context.py`). All external `gh` calls go
through `monkeypatch.setattr(zprof_guard, "_run", fake)` (ADR-0006 F8,
mirrors the `_branch_pr_merged` tests in `test_guard_context.py`) — except
two end-to-end tests that stub a real `gh` binary on `PATH`, which exercise
`_run` itself (env, cwd, and the argv actually reaching the binary).
"""
import json
import os
import pathlib
import sys

import pytest

sys.path.insert(0, str(pathlib.Path(__file__).parent))
from test_guard import (
    _bash,
    _payload,
    _read_events,
    _run_guard,
    _write_config,
    build_guard_config,
    zprof_guard,
)


@pytest.fixture(autouse=True)
def _no_project_dir_env(monkeypatch):
    """Same precaution as test_guard.py/test_guard_context.py's fixture of the
    same name: not shared across test modules, must be redeclared here too.
    """
    monkeypatch.delenv("CLAUDE_PROJECT_DIR", raising=False)


# ---------------------------------------------------------------------------
# helpers
# ---------------------------------------------------------------------------

_MERGE_JSON_FIELDS = "number,body,closingIssuesReferences,state"


def _pr_json(number=7, body="", closing=None, state="OPEN"):
    return json.dumps({
        "number": number, "body": body,
        "closingIssuesReferences": closing if closing is not None else [],
        "state": state,
    })


def _run_recorder(response):
    """`fake` for `monkeypatch.setattr(zprof_guard, "_run", fake)`.

    `response` is a fixed `(rc, out)` tuple returned for every call. Every
    call is recorded on `fake.calls` as `(argv, cwd, timeout)` so a test can
    assert the exact argv / `wd` / `timeout` that reached `_run`.
    """
    calls = []

    def fake(argv, cwd, timeout):
        calls.append((list(argv), cwd, timeout))
        return response

    fake.calls = calls
    return fake


def _fail_if_called(argv, cwd, timeout):
    raise AssertionError(f"_run must not be called here, got {argv!r}")


def _expected_reason(rule_id: str, raw_reason: str) -> str:
    return zprof_guard.deny_output(rule_id, raw_reason)["hookSpecificOutput"]["permissionDecisionReason"]


# ---------------------------------------------------------------------------
# merge_preflight -- Closes#/Gate decision (AC3)
# ---------------------------------------------------------------------------

def test_merge_preflight_allow_valid_pr(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    fake = _run_recorder((0, _pr_json(number=7, body="Closes #12\n\n## Gate\nx")))
    monkeypatch.setattr(zprof_guard, "_run", fake)
    payload = _payload("Bash", _bash("gh pr merge 7"), role="pr-shepherd", cwd=tmp_path)
    assert zprof_guard.pre_tool(payload) is None
    assert fake.calls == [
        (["gh", "pr", "view", "7", "--json", _MERGE_JSON_FIELDS], str(tmp_path), zprof_guard._GH_TIMEOUT),
    ]
    assert _read_events(tmp_path) == []


def test_merge_preflight_deny_missing_closes(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    fake = _run_recorder((0, _pr_json(number=7, body="## Gate\nx")))
    monkeypatch.setattr(zprof_guard, "_run", fake)
    payload = _payload("Bash", _bash("gh pr merge 7"), role="pr-shepherd", cwd=tmp_path)
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert out["hookSpecificOutput"]["permissionDecisionReason"] == _expected_reason(
        "merge_preflight", "PR #7 без `Closes #`")


def test_merge_preflight_deny_missing_gate(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    fake = _run_recorder((0, _pr_json(number=7, body="Closes #12\nx")))
    monkeypatch.setattr(zprof_guard, "_run", fake)
    payload = _payload("Bash", _bash("gh pr merge 7"), role="pr-shepherd", cwd=tmp_path)
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert out["hookSpecificOutput"]["permissionDecisionReason"] == _expected_reason(
        "merge_preflight", "PR #7 без раздела `## Gate`")


def test_merge_preflight_deny_missing_both(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    fake = _run_recorder((0, _pr_json(number=7, body="nothing here")))
    monkeypatch.setattr(zprof_guard, "_run", fake)
    payload = _payload("Bash", _bash("gh pr merge 7"), role="pr-shepherd", cwd=tmp_path)
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert out["hookSpecificOutput"]["permissionDecisionReason"] == _expected_reason(
        "merge_preflight", "PR #7 без `Closes #` и без раздела `## Gate`")


def test_merge_preflight_allow_via_closing_issues_references(tmp_path, monkeypatch):
    """`closingIssuesReferences` non-empty satisfies the `Closes #` condition
    even without a textual match in `body` (AC3 disjunction)."""
    _write_config(tmp_path, build_guard_config())
    fake = _run_recorder((0, _pr_json(number=7, body="## Gate\nx", closing=[{"number": 12}])))
    monkeypatch.setattr(zprof_guard, "_run", fake)
    payload = _payload("Bash", _bash("gh pr merge 7"), role="pr-shepherd", cwd=tmp_path)
    assert zprof_guard.pre_tool(payload) is None


def test_merge_preflight_allow_when_state_not_open(tmp_path, monkeypatch):
    """A closed/merged PR is skipped without an event -- nothing left to protect."""
    _write_config(tmp_path, build_guard_config())
    fake = _run_recorder((0, _pr_json(number=7, body="", state="MERGED")))
    monkeypatch.setattr(zprof_guard, "_run", fake)
    payload = _payload("Bash", _bash("gh api repos/o/r/pulls/7/merge"), role="pr-shepherd", cwd=tmp_path)
    assert zprof_guard.pre_tool(payload) is None
    assert _read_events(tmp_path) == []


# ---------------------------------------------------------------------------
# merge_preflight -- PR number/selector extraction (AC2/AC6)
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("command,expected_argv_tail", [
    ("gh pr merge 7", ["7"]),
    ("gh pr merge https://github.com/o/r/pull/7", ["https://github.com/o/r/pull/7"]),
])
def test_merge_preflight_selector_forms(tmp_path, monkeypatch, command, expected_argv_tail):
    _write_config(tmp_path, build_guard_config())
    fake = _run_recorder((0, _pr_json(number=7, body="Closes #1\n\n## Gate\nx")))
    monkeypatch.setattr(zprof_guard, "_run", fake)
    payload = _payload("Bash", _bash(command), role="pr-shepherd", cwd=tmp_path,
                        session_id=f"sess-{command}")
    assert zprof_guard.pre_tool(payload) is None
    assert fake.calls[0][0] == ["gh", "pr", "view", *expected_argv_tail, "--json", _MERGE_JSON_FIELDS]


def test_merge_preflight_api_pulls_merge_with_repo(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    fake = _run_recorder((0, _pr_json(number=12, body="Closes #1\n\n## Gate\nx")))
    monkeypatch.setattr(zprof_guard, "_run", fake)
    payload = _payload("Bash", _bash("gh api -X PUT repos/o/r/pulls/12/merge"), role="pr-shepherd", cwd=tmp_path)
    assert zprof_guard.pre_tool(payload) is None
    assert fake.calls[0][0] == ["gh", "pr", "view", "12", "-R", "o/r", "--json", _MERGE_JSON_FIELDS]


def test_merge_preflight_api_pulls_merge_placeholder_repo(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    fake = _run_recorder((0, _pr_json(number=12, body="Closes #1\n\n## Gate\nx")))
    monkeypatch.setattr(zprof_guard, "_run", fake)
    payload = _payload("Bash", _bash("gh api repos/{owner}/{repo}/pulls/12/merge"), role="pr-shepherd", cwd=tmp_path)
    assert zprof_guard.pre_tool(payload) is None
    assert fake.calls[0][0] == ["gh", "pr", "view", "12", "--json", _MERGE_JSON_FIELDS]


def test_merge_preflight_repo_flag_forms(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    for command in ("gh pr merge 7 -R o/r", "gh pr merge 7 -Ro/r"):
        fake = _run_recorder((0, _pr_json(number=7, body="Closes #1\n\n## Gate\nx")))
        monkeypatch.setattr(zprof_guard, "_run", fake)
        payload = _payload("Bash", _bash(command), role="pr-shepherd", cwd=tmp_path,
                            session_id=f"sess-{command}")
        assert zprof_guard.pre_tool(payload) is None
        assert fake.calls[0][0] == ["gh", "pr", "view", "7", "-R", "o/r", "--json", _MERGE_JSON_FIELDS]


def test_merge_preflight_selector_ignores_flag_values(tmp_path, monkeypatch):
    """`--squash --subject "fix 3 bugs" 7` -- the selector is the first true
    positional, not a digit found inside a flag's value (Context §4)."""
    _write_config(tmp_path, build_guard_config())
    fake = _run_recorder((0, _pr_json(number=7, body="Closes #1\n\n## Gate\nx")))
    monkeypatch.setattr(zprof_guard, "_run", fake)
    payload = _payload("Bash", _bash('gh pr merge --squash --subject "fix 3 bugs" 7'),
                        role="pr-shepherd", cwd=tmp_path)
    assert zprof_guard.pre_tool(payload) is None
    assert fake.calls[0][0] == ["gh", "pr", "view", "7", "--json", _MERGE_JSON_FIELDS]


def test_merge_preflight_no_selector_when_only_fd_redirect(tmp_path, monkeypatch):
    """`gh pr merge 2>&1` -- the `2` is a redirect target, not a PR number (Context §7)."""
    _write_config(tmp_path, build_guard_config())
    fake = _run_recorder((0, _pr_json(number=9, body="Closes #1\n\n## Gate\nx")))
    monkeypatch.setattr(zprof_guard, "_run", fake)
    payload = _payload("Bash", _bash("gh pr merge 2>&1"), role="pr-shepherd", cwd=tmp_path)
    assert zprof_guard.pre_tool(payload) is None
    assert fake.calls[0][0] == ["gh", "pr", "view", "--json", _MERGE_JSON_FIELDS]


def test_merge_preflight_fallback_without_selector(tmp_path, monkeypatch):
    """`gh pr merge` with no selector at all -- a single unified `gh pr view`
    call (no selector) resolves the current branch's PR; the number in the
    deny reason comes from the JSON response, not the command (ADR-0006 F3)."""
    _write_config(tmp_path, build_guard_config())
    fake = _run_recorder((0, _pr_json(number=9, body="## Gate\nx")))
    monkeypatch.setattr(zprof_guard, "_run", fake)
    payload = _payload("Bash", _bash("gh pr merge"), role="pr-shepherd", cwd=tmp_path)
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert fake.calls[0][0] == ["gh", "pr", "view", "--json", _MERGE_JSON_FIELDS]
    assert out["hookSpecificOutput"]["permissionDecisionReason"] == _expected_reason(
        "merge_preflight", "PR #9 без `Closes #`")


# ---------------------------------------------------------------------------
# merge_preflight -- allow_unverified / parse_error (AC4)
# ---------------------------------------------------------------------------

_MERGE_FAILURE_RESPONSES = [
    (1, ""),
    (None, "TimeoutExpired"),
    (0, "not json"),
    (0, "[]"),
]

_CONTEXT_ERROR_KEYS = {"ts", "session_id", "event", "role", "dispatch_id", "tool",
                        "rule", "decision", "detail", "target", "input_hash", "run_id"}


@pytest.mark.parametrize("rc,out", _MERGE_FAILURE_RESPONSES)
def test_merge_preflight_allow_unverified_on_gh_failure(tmp_path, monkeypatch, rc, out):
    _write_config(tmp_path, build_guard_config())
    fake = _run_recorder((rc, out))
    monkeypatch.setattr(zprof_guard, "_run", fake)
    payload = _payload("Bash", _bash("gh pr merge 7"), role="pr-shepherd", cwd=tmp_path,
                        session_id=f"sess-fail-{rc}-{out}")
    assert zprof_guard.pre_tool(payload) is None

    events = _read_events(tmp_path)
    matching = [e for e in events if e["decision"] == "allow_unverified"]
    assert len(matching) == 1
    ev = matching[0]
    assert ev["event"] == "pre-tool"
    assert ev["rule"] == "merge_preflight"
    assert ev["detail"].startswith("preflight_unverified: ")
    assert set(ev.keys()) == _CONTEXT_ERROR_KEYS


def test_merge_preflight_parse_error_on_unclosed_quote(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    payload = _payload("Bash", _bash('gh pr merge "unclosed'), role="pr-shepherd", cwd=tmp_path,
                        session_id="sess-merge-parse-error")
    assert zprof_guard.pre_tool(payload) is None

    events = _read_events(tmp_path)
    matching = [e for e in events if e["decision"] == "allow_unverified"]
    assert len(matching) == 1
    assert matching[0]["rule"] == "merge_preflight"
    assert matching[0]["detail"].startswith("parse_error: ")


# ---------------------------------------------------------------------------
# merge_role: unaffected roles deny earlier, `_run` never called (AC1/AC7)
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("command", ["gh pr merge 7", "gh api repos/o/r/pulls/12/merge"])
@pytest.mark.parametrize("role", ["implementer", "main"])
def test_merge_role_denies_before_preflight_for_non_merge_roles(tmp_path, monkeypatch, role, command):
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    payload = _payload("Bash", _bash(command), role=role, cwd=tmp_path,
                        session_id=f"sess-{role}-{command}")
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    reason = out["hookSpecificOutput"]["permissionDecisionReason"]
    assert reason.startswith("zprof guard [merge_role]:")
    assert "zprof doctor" not in reason


@pytest.mark.parametrize("command", ["gh pr merge 7", "gh api repos/o/r/pulls/12/merge"])
def test_merge_role_denies_unknown_with_doctor_hint(tmp_path, monkeypatch, command):
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    payload = {"session_id": f"sess-unknown-{command}", "cwd": str(tmp_path), "tool_name": "Bash",
               "tool_input": _bash(command)}  # no agent_type, no transcript_path -> unknown
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    reason = out["hookSpecificOutput"]["permissionDecisionReason"]
    assert reason.startswith("zprof guard [merge_role]:")
    assert "zprof doctor" in reason


# ---------------------------------------------------------------------------
# pr_create_gate -- allow (AC5/AC6); `_run` must never be called (AC7)
# ---------------------------------------------------------------------------

CREATE_ROLES = ["implementer", "main", "pr-shepherd"]
_GOOD_BODY = "Closes #12\n\n## Gate\nsome content"
_BODY_NO_CLOSES = "## Gate\nx"
_BODY_NO_GATE = "Closes #1\nx"
_BODY_LITERAL_BACKSLASH_N = "Closes #1\\n## Gate\\nrest"  # literal "\n" -- not a real newline


@pytest.mark.parametrize("role", CREATE_ROLES)
def test_pr_create_gate_allow_inline_body_real_newlines(tmp_path, monkeypatch, role):
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    command = f'gh pr create -b "{_GOOD_BODY}"'
    payload = _payload("Bash", _bash(command), role=role, cwd=tmp_path, session_id=f"sess-{role}")
    assert zprof_guard.pre_tool(payload) is None


def test_pr_create_gate_allow_body_equals_form(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    command = f'gh pr create --body="{_GOOD_BODY}"'
    payload = _payload("Bash", _bash(command), role="implementer", cwd=tmp_path)
    assert zprof_guard.pre_tool(payload) is None


def test_pr_create_gate_allow_short_body_glued(tmp_path, monkeypatch):
    """`-bVALUE` (no space) -- slitno short form (AC5)."""
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    command = f'gh pr create -b"{_GOOD_BODY}"'
    payload = _payload("Bash", _bash(command), role="implementer", cwd=tmp_path)
    assert zprof_guard.pre_tool(payload) is None


def test_pr_create_gate_allow_body_file(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    (tmp_path / "body.md").write_text(_GOOD_BODY, encoding="utf-8")
    payload = _payload("Bash", _bash("gh pr create -F body.md"), role="implementer", cwd=tmp_path)
    assert zprof_guard.pre_tool(payload) is None


def test_pr_create_gate_allow_body_file_relative_to_cd(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    sub = tmp_path / "sub"
    sub.mkdir()
    (sub / "body.md").write_text(_GOOD_BODY, encoding="utf-8")
    payload = _payload("Bash", _bash("cd sub && gh pr create -F body.md"), role="implementer", cwd=tmp_path)
    assert zprof_guard.pre_tool(payload) is None


def test_pr_create_gate_allow_rtk_prefix(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    command = f'rtk gh pr create -b "{_GOOD_BODY}"'
    payload = _payload("Bash", _bash(command), role="implementer", cwd=tmp_path)
    assert zprof_guard.pre_tool(payload) is None


def test_pr_create_gate_allow_heredoc_body(tmp_path, monkeypatch):
    """Body passed as `"$(cat <<'EOF' ... EOF)"` -- one quoted token with real
    newlines; markers are checked against the embedded heredoc text (ADR-0006
    F4 accepted limitation, positive case)."""
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    command = 'gh pr create -b "$(cat <<\'EOF\'\n' + _GOOD_BODY + '\nEOF\n)"'
    payload = _payload("Bash", _bash(command), role="implementer", cwd=tmp_path)
    assert zprof_guard.pre_tool(payload) is None


# ---------------------------------------------------------------------------
# pr_create_gate -- deny
# ---------------------------------------------------------------------------

def test_pr_create_gate_deny_no_body(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    payload = _payload("Bash", _bash("gh pr create --title x"), role="implementer", cwd=tmp_path)
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert out["hookSpecificOutput"]["permissionDecisionReason"] == _expected_reason(
        "pr_create_gate",
        "нет тела PR: передай --body/-b или --body-file/-F с `Closes #N` и разделом `## Gate`")


def test_pr_create_gate_deny_title_consumes_dash_b_value(tmp_path, monkeypatch):
    """`-t "x -b y"` -- the whole quoted string is `-t`'s value; there is no
    body source at all, not a deny on the body's *content* (Context §... AC5)."""
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    payload = _payload("Bash", _bash('gh pr create -t "x -b y"'), role="implementer", cwd=tmp_path)
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert out["hookSpecificOutput"]["permissionDecisionReason"] == _expected_reason(
        "pr_create_gate",
        "нет тела PR: передай --body/-b или --body-file/-F с `Closes #N` и разделом `## Gate`")


@pytest.mark.parametrize("fill_flag", ["--fill", "--fill-first", "--fill-verbose", "-f", "-df"])
def test_pr_create_gate_deny_fill_flags_override_valid_body(tmp_path, monkeypatch, fill_flag):
    """`--fill*` denies even alongside a valid `-b` (AC5 -- priority, not conjunction)."""
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    command = f'gh pr create {fill_flag} -b "{_GOOD_BODY}"'
    payload = _payload("Bash", _bash(command), role="implementer", cwd=tmp_path,
                        session_id=f"sess-{fill_flag}")
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert out["hookSpecificOutput"]["permissionDecisionReason"] == _expected_reason(
        "pr_create_gate",
        "`--fill*` запрещён: тело PR должно содержать `Closes #N` и раздел `## Gate` — "
        "передай --body или --body-file")


def test_pr_create_gate_deny_missing_gate(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    command = f'gh pr create -b "{_BODY_NO_GATE}"'
    payload = _payload("Bash", _bash(command), role="implementer", cwd=tmp_path)
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert out["hookSpecificOutput"]["permissionDecisionReason"] == _expected_reason(
        "pr_create_gate", "тело PR без раздела `## Gate`")


def test_pr_create_gate_deny_missing_closes(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    command = f'gh pr create -b "{_BODY_NO_CLOSES}"'
    payload = _payload("Bash", _bash(command), role="implementer", cwd=tmp_path)
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert out["hookSpecificOutput"]["permissionDecisionReason"] == _expected_reason(
        "pr_create_gate", "тело PR без `Closes #`")


def test_pr_create_gate_deny_literal_backslash_n_before_gate(tmp_path, monkeypatch):
    """A literal `\\n` (two characters) inside a quoted `-b` value is not a
    real newline -- `## Gate` never starts a line -> deny (ADR-0006 F4
    accepted limitation)."""
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    command = f'gh pr create -b "{_BODY_LITERAL_BACKSLASH_N}"'
    payload = _payload("Bash", _bash(command), role="implementer", cwd=tmp_path)
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert out["hookSpecificOutput"]["permissionDecisionReason"] == _expected_reason(
        "pr_create_gate", "тело PR без раздела `## Gate`")


# ---------------------------------------------------------------------------
# pr_create_gate -- allow_unverified / parse_error (AC5)
# ---------------------------------------------------------------------------

def test_pr_create_gate_allow_unverified_stdin_body_file(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    payload = _payload("Bash", _bash("gh pr create -F -"), role="implementer", cwd=tmp_path,
                        session_id="sess-create-stdin")
    assert zprof_guard.pre_tool(payload) is None

    events = _read_events(tmp_path)
    matching = [e for e in events if e["decision"] == "allow_unverified"]
    assert len(matching) == 1
    assert matching[0]["rule"] == "pr_create_gate"
    assert matching[0]["detail"] == "preflight_unverified: body-file: stdin"


def test_pr_create_gate_allow_unverified_missing_body_file(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    payload = _payload("Bash", _bash("gh pr create --body-file missing.md"), role="implementer",
                        cwd=tmp_path, session_id="sess-create-missing-file")
    assert zprof_guard.pre_tool(payload) is None

    events = _read_events(tmp_path)
    matching = [e for e in events if e["decision"] == "allow_unverified"]
    assert len(matching) == 1
    assert matching[0]["detail"] == "preflight_unverified: body-file: not a regular file"


def test_pr_create_gate_parse_error_on_unclosed_quote(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    payload = _payload("Bash", _bash('gh pr create -b "unclosed'), role="implementer", cwd=tmp_path,
                        session_id="sess-create-parse-error")
    assert zprof_guard.pre_tool(payload) is None

    events = _read_events(tmp_path)
    matching = [e for e in events if e["decision"] == "allow_unverified"]
    assert len(matching) == 1
    assert matching[0]["rule"] == "pr_create_gate"
    assert matching[0]["detail"].startswith("parse_error: ")


def test_pr_create_gate_no_invocation_inside_quoted_text(tmp_path, monkeypatch):
    """`echo "gh pr create --fill"` -- the match regex fires on the literal
    text, but `_invocations` finds zero real invocations (it's one quoted
    token) -> allow, no event at all (ADR-0006 F2)."""
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    payload = _payload("Bash", _bash('echo "gh pr create --fill"'), role="implementer", cwd=tmp_path)
    assert zprof_guard.pre_tool(payload) is None
    assert _read_events(tmp_path) == []


# ---------------------------------------------------------------------------
# Regression: cases moved out of test_guard.py::UNKNOWN_CONTEXT_CASES (F7) --
# now real, deterministic outcomes instead of relying on the developer
# machine's git/gh state.
# ---------------------------------------------------------------------------

def test_regression_gh_pr_create_short_flags_denies_now_registered(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    payload = _payload("Bash", _bash("gh pr create -t x -b y"), role="implementer", cwd=tmp_path)
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert out["hookSpecificOutput"]["permissionDecisionReason"].startswith("zprof guard [pr_create_gate]:")


def test_regression_gh_pr_merge_without_number_is_now_registered(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    fake = _run_recorder((1, ""))
    monkeypatch.setattr(zprof_guard, "_run", fake)
    payload = _payload("Bash", _bash("gh pr merge 7"), role="pr-shepherd", cwd=tmp_path,
                        session_id="sess-regression-merge")
    assert zprof_guard.pre_tool(payload) is None
    events = _read_events(tmp_path)
    assert any(e["decision"] == "allow_unverified" and e["rule"] == "merge_preflight" for e in events)


# ---------------------------------------------------------------------------
# F8: two real end-to-end tests via a `gh` stub on PATH -- everything above
# uses a monkeypatched `_run` for speed and precise argv assertions; these
# two cover what monkeypatch can't: env, cwd and the argv actually reaching
# the binary.
# ---------------------------------------------------------------------------

def _write_gh_stub(bin_dir: pathlib.Path, argv_file: pathlib.Path, body: str) -> None:
    bin_dir.mkdir(parents=True, exist_ok=True)
    script = bin_dir / "gh"
    script.write_text(f'#!/bin/sh\nprintf \'%s\\n\' "$@" > "{argv_file}"\n{body}\n', encoding="utf-8")
    script.chmod(0o755)


def test_e2e_merge_preflight_allow_with_gh_on_path(tmp_path):
    root = tmp_path / "project"
    _write_config(root, build_guard_config())
    bin_dir = tmp_path / "bin"
    argv_file = tmp_path / "gh-argv.txt"
    pr_json = _pr_json(number=7, body="Closes #12\n\n## Gate\nx")
    _write_gh_stub(bin_dir, argv_file, f"cat <<'GHJSON'\n{pr_json}\nGHJSON")
    payload = _payload("Bash", _bash("gh pr merge 7"), role="pr-shepherd", cwd=root)
    result = _run_guard(root, json.dumps(payload),
                         env_extra={"PATH": f"{bin_dir}:{os.environ.get('PATH', '')}"})
    assert result.returncode == 0
    assert result.stdout == ""
    assert argv_file.read_text(encoding="utf-8").splitlines() == ["pr", "view", "7", "--json", _MERGE_JSON_FIELDS]


def test_e2e_merge_preflight_allow_unverified_when_gh_fails_on_path(tmp_path):
    root = tmp_path / "project"
    _write_config(root, build_guard_config())
    bin_dir = tmp_path / "bin"
    argv_file = tmp_path / "gh-argv.txt"
    _write_gh_stub(bin_dir, argv_file, "exit 1")
    payload = _payload("Bash", _bash("gh pr merge 7"), role="pr-shepherd", cwd=root,
                        session_id="sess-e2e-fail")
    result = _run_guard(root, json.dumps(payload),
                         env_extra={"PATH": f"{bin_dir}:{os.environ.get('PATH', '')}"})
    assert result.returncode == 0
    assert result.stdout == ""
    assert argv_file.read_text(encoding="utf-8").splitlines() == ["pr", "view", "7", "--json", _MERGE_JSON_FIELDS]

    events = _read_events(root)
    matching = [e for e in events if e["decision"] == "allow_unverified"]
    assert len(matching) == 1
    assert matching[0]["rule"] == "merge_preflight"
    assert matching[0]["detail"].startswith("preflight_unverified: ")
