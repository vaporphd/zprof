"""Tests for the four context evaluators registered by zprof-guard.py in #24.

ADR: docs/adr/0005-guard-context-evaluators.md (E1-E9)
Parent ADR: docs/adr/0004-zprof-guard-pre-tool-frame.md (D2, D4, D5, D9)
Plan: tasks/plan-1.md, step 6; issue #24 AC1-AC7

Reuses the module-loading pattern, `guard.json` fixture and small helpers
from `test_guard.py` (imported by path, not by package, since there's no
`__init__.py` in this directory) instead of duplicating them.
"""
import json
import pathlib
import shutil
import subprocess
import sys
import tempfile

import pytest

sys.path.insert(0, str(pathlib.Path(__file__).parent))
from test_guard import (
    _bash,
    _payload,
    _read_events,
    _write_config,
    build_guard_config,
    zprof_guard,
)


@pytest.fixture(autouse=True)
def _no_project_dir_env(monkeypatch):
    """Same precaution as test_guard.py's fixture of the same name: this
    module's own autouse fixtures don't apply across test files, so it must
    be redeclared here too (direct calls must not pick up a real
    CLAUDE_PROJECT_DIR from the environment this test suite happens to run in).
    """
    monkeypatch.delenv("CLAUDE_PROJECT_DIR", raising=False)


# ---------------------------------------------------------------------------
# git helpers (mirrors the `git init` pattern in test_normalization.py)
# ---------------------------------------------------------------------------

def _git(args, cwd) -> subprocess.CompletedProcess:
    return subprocess.run(["git", *args], cwd=str(cwd), capture_output=True, text=True, check=True)


def _init_repo(repo: pathlib.Path) -> None:
    repo.mkdir(parents=True, exist_ok=True)
    _git(["init"], repo)
    _git(["config", "user.email", "t@example.com"], repo)
    _git(["config", "user.name", "t"], repo)
    (repo / "f.txt").write_text("1", encoding="utf-8")
    _git(["add", "."], repo)
    _git(["commit", "-m", "init", "--no-gpg-sign"], repo)
    _git(["branch", "-M", "main"], repo)


def _init_pushed_repo(base: pathlib.Path) -> tuple[pathlib.Path, pathlib.Path]:
    """A `repo/` with one commit, pushed to a bare `remote.git` as `origin/main`."""
    remote = base / "remote.git"
    _git(["init", "--bare", str(remote)], base)
    repo = base / "repo"
    _init_repo(repo)
    _git(["remote", "add", "origin", str(remote)], repo)
    _git(["push", "-u", "origin", "main"], repo)
    return repo, remote


def _project(tmp_path: pathlib.Path) -> pathlib.Path:
    root = tmp_path / "project"
    root.mkdir()
    _write_config(root, build_guard_config())
    return root


# ---------------------------------------------------------------------------
# AC1: head_on_remote (rebase_published, amend_published) -- 1 deny + 2 allow each
# ---------------------------------------------------------------------------

HEAD_ON_REMOTE_CASES = [
    ("rebase_published", "git rebase main"),
    ("amend_published", 'git commit --amend -m x'),
]


@pytest.mark.parametrize("rule_id,command", HEAD_ON_REMOTE_CASES)
def test_head_on_remote_deny_when_head_published(tmp_path, rule_id, command):
    """HEAD has an upstream and is reachable from a remote branch -> deny."""
    repo, _ = _init_pushed_repo(tmp_path)
    _write_config(repo, build_guard_config())
    payload = _payload("Bash", _bash(command), role="implementer", cwd=repo)
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert out["hookSpecificOutput"]["permissionDecisionReason"].startswith(f"zprof guard [{rule_id}]:")


@pytest.mark.parametrize("rule_id,command", HEAD_ON_REMOTE_CASES)
def test_head_on_remote_allow_when_no_remote_at_all(tmp_path, rule_id, command):
    """No remote configured -> `git branch -r --contains HEAD` is empty -> allow, no 2nd call."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    _write_config(repo, build_guard_config())
    payload = _payload("Bash", _bash(command), role="implementer", cwd=repo)
    assert zprof_guard.pre_tool(payload) is None


@pytest.mark.parametrize("rule_id,command", HEAD_ON_REMOTE_CASES)
def test_head_on_remote_allow_when_head_not_yet_pushed(tmp_path, rule_id, command):
    """Upstream exists, but HEAD is a local commit ahead of it -> allow (AC1)."""
    repo, _ = _init_pushed_repo(tmp_path)
    (repo / "f.txt").write_text("2", encoding="utf-8")
    _git(["commit", "-am", "local only", "--no-gpg-sign"], repo)
    _write_config(repo, build_guard_config())
    payload = _payload("Bash", _bash(command), role="implementer", cwd=repo)
    assert zprof_guard.pre_tool(payload) is None


# ---------------------------------------------------------------------------
# AC2: linked_worktree (stash_in_worktree) -- 1 deny + 2 allow
# ---------------------------------------------------------------------------

def test_linked_worktree_deny_in_linked_worktree(tmp_path, monkeypatch):
    repo, _ = _init_pushed_repo(tmp_path)
    _write_config(repo, build_guard_config())
    wt = tmp_path / "wt"
    _git(["worktree", "add", str(wt), "-b", "feature"], repo)
    # A linked worktree's own `cwd` has no `.claude/`; $CLAUDE_PROJECT_DIR
    # (set by the real Claude Code session) is what locates guard.json /
    # .agentlog for the main project (ADR-0004 D7).
    monkeypatch.setenv("CLAUDE_PROJECT_DIR", str(repo))
    payload = _payload("Bash", _bash("git stash"), role="implementer", cwd=wt)
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert out["hookSpecificOutput"]["permissionDecisionReason"].startswith("zprof guard [stash_in_worktree]:")


def test_linked_worktree_allow_in_main_repo(tmp_path):
    repo, _ = _init_pushed_repo(tmp_path)
    _write_config(repo, build_guard_config())
    payload = _payload("Bash", _bash("git stash"), role="implementer", cwd=repo)
    assert zprof_guard.pre_tool(payload) is None


def test_linked_worktree_allow_in_main_repo_subdirectory(tmp_path):
    repo, _ = _init_pushed_repo(tmp_path)
    _write_config(repo, build_guard_config())
    sub = repo / "sub"
    sub.mkdir()
    payload = _payload("Bash", _bash("git stash"), role="implementer", cwd=sub)
    assert zprof_guard.pre_tool(payload) is None


# ---------------------------------------------------------------------------
# AC3: context_error outside any git repo, for all three §5.2 rules
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("rule_id,command", [
    ("rebase_published", "git rebase main"),
    ("amend_published", "git commit --amend -m x"),
    ("stash_in_worktree", "git stash"),
])
def test_context_error_written_outside_git_repo(tmp_path, rule_id, command):
    non_repo = tmp_path / "not-a-repo"
    non_repo.mkdir()
    _write_config(non_repo, build_guard_config())
    payload = _payload("Bash", _bash(command), role="implementer", cwd=non_repo,
                        session_id=f"sess-{rule_id}")
    assert zprof_guard.pre_tool(payload) is None  # no repo -> rule doesn't fire, not a deny

    events = _read_events(non_repo)
    errors = [e for e in events if e["event"] == "context_error" and e["rule"] == rule_id]
    assert len(errors) == 1
    assert errors[0]["decision"] is None
    assert errors[0]["detail"]
    assert errors[0]["role"] == "implementer"


# ---------------------------------------------------------------------------
# AC4/AC5: write_outside_repo
# ---------------------------------------------------------------------------

def test_write_outside_repo_deny_etc(tmp_path):
    root = _project(tmp_path)
    payload = _payload("Write", {"file_path": "/etc/x", "content": "x"}, role="implementer", cwd=root)
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert out["hookSpecificOutput"]["permissionDecisionReason"].startswith("zprof guard [write_outside_repo]:")
    # ADR-0005 E5 step 3.4 (deviation from plan step 5/6): "not a git repo" is
    # the expected, normal answer for the linked-worktree allowance check here
    # -- it must NOT also produce a context_error next to the deny.
    events = _read_events(root)
    assert not [e for e in events if e["event"] == "context_error"]


def test_write_outside_repo_allow_inside_project(tmp_path):
    root = _project(tmp_path)
    target = root / "src" / "file.py"
    payload = _payload("Write", {"file_path": str(target), "content": "x"}, role="implementer", cwd=root)
    assert zprof_guard.pre_tool(payload) is None


def test_write_outside_repo_allow_claude_plans(tmp_path, monkeypatch):
    root = _project(tmp_path)
    home = tmp_path / "home"
    home.mkdir()
    monkeypatch.setenv("HOME", str(home))
    target = home / ".claude" / "plans" / "todo.md"
    payload = _payload("Write", {"file_path": str(target), "content": "x"}, role="implementer", cwd=root)
    assert zprof_guard.pre_tool(payload) is None


def test_write_outside_repo_allow_tmpdir_claude(tmp_path, monkeypatch):
    """`$TMPDIR/claude-*` prefix: literal head realpath'd, glob tail untouched (ADR Context §4)."""
    root = _project(tmp_path)
    fake_tmp = tmp_path / "vartmp"
    fake_tmp.mkdir()
    monkeypatch.setenv("TMPDIR", str(fake_tmp))
    target = fake_tmp / "claude-501" / "scratchpad" / "x.md"
    target.parent.mkdir(parents=True)
    payload = _payload("Write", {"file_path": str(target), "content": "x"}, role="implementer", cwd=root)
    assert zprof_guard.pre_tool(payload) is None


def test_write_outside_repo_allow_tmp_claude_literal_prefix(tmp_path):
    """The literal `/tmp/claude-*` entry (not `$TMPDIR`-derived)."""
    root = _project(tmp_path)
    scratch_dir = pathlib.Path(tempfile.mkdtemp(prefix="claude-", dir="/tmp"))
    try:
        target = scratch_dir / "x.md"
        payload = _payload("Write", {"file_path": str(target), "content": "x"}, role="implementer", cwd=root)
        assert zprof_guard.pre_tool(payload) is None
    finally:
        shutil.rmtree(scratch_dir, ignore_errors=True)


def test_write_outside_repo_allow_linked_worktree_including_missing_ancestor(tmp_path):
    """Linked worktree outside the project dir; target's parent dir doesn't
    exist yet -> climb to the nearest existing ancestor (AC5)."""
    root = tmp_path / "project"
    _init_repo(root)
    _write_config(root, build_guard_config())

    wt = tmp_path / "wt"  # sibling of `root`, outside $CLAUDE_PROJECT_DIR
    _git(["worktree", "add", str(wt), "-b", "feature"], root)

    target = wt / "newsub" / "file.md"  # `newsub` does not exist yet
    payload = _payload("Write", {"file_path": str(target), "content": "x"}, role="implementer", cwd=root)
    assert zprof_guard.pre_tool(payload) is None


def test_write_outside_repo_context_error_on_allowance_git_timeout(tmp_path, monkeypatch):
    """ADR-0005 E5 step 3.4: `rc is None` (timeout/missing git) during the
    linked-worktree allowance check -- unlike a plain "not a repo" answer --
    is a real context_error, even though the final decision stays `deny`.
    """
    root = _project(tmp_path)
    monkeypatch.setattr(zprof_guard, "_run", lambda argv, cwd, timeout: (None, "TimeoutExpired"))
    target = tmp_path / "outside" / "file.md"
    target.parent.mkdir(parents=True)
    payload = _payload("Write", {"file_path": str(target), "content": "x"}, role="implementer", cwd=root)
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert out["hookSpecificOutput"]["permissionDecisionReason"].startswith("zprof guard [write_outside_repo]:")

    events = _read_events(root)
    errors = [e for e in events if e["event"] == "context_error" and e["rule"] == "write_outside_repo"]
    assert len(errors) == 1


# ---------------------------------------------------------------------------
# AC4 regression: `~/.claude/projects/*/memory/` must match posegment-wise,
# not by slicing the prefix at its first `*` and comparing as a string
# prefix (plan step 5) -- that would allow writes anywhere under
# `~/.claude/projects/<slug>/**`, not just `.../<slug>/memory/**`.
# ---------------------------------------------------------------------------

def test_write_outside_repo_memory_prefix_allow(tmp_path, monkeypatch):
    root = _project(tmp_path)
    home = tmp_path / "home"
    home.mkdir()
    monkeypatch.setenv("HOME", str(home))
    target = home / ".claude" / "projects" / "my-slug" / "memory" / "file.md"
    payload = _payload("Write", {"file_path": str(target), "content": "x"}, role="implementer", cwd=root)
    assert zprof_guard.pre_tool(payload) is None


@pytest.mark.parametrize("suffix", ["session.jsonl", "subagents/agent-x.meta.json"])
def test_write_outside_repo_memory_prefix_deny_sibling_paths(tmp_path, monkeypatch, suffix):
    root = _project(tmp_path)
    home = tmp_path / "home"
    home.mkdir()
    monkeypatch.setenv("HOME", str(home))
    target = home / ".claude" / "projects" / "my-slug" / suffix
    target.parent.mkdir(parents=True, exist_ok=True)
    payload = _payload("Write", {"file_path": str(target), "content": "x"}, role="implementer", cwd=root)
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert out["hookSpecificOutput"]["permissionDecisionReason"].startswith("zprof guard [write_outside_repo]:")


# ---------------------------------------------------------------------------
# D5/branch_pr_merged (remote_ref_delete_unmerged, roles: [pr-shepherd]):
# fail-closed evaluator, exercised via monkeypatched `_run` (no network/gh).
# ---------------------------------------------------------------------------

def test_branch_pr_merged_allow_when_gh_reports_merged(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run",
                         lambda argv, cwd, timeout: (0, json.dumps([{"number": 1}])))
    payload = _payload("Bash", _bash("git push origin --delete feat/x"), role="pr-shepherd", cwd=tmp_path)
    assert zprof_guard.pre_tool(payload) is None


def test_branch_pr_merged_deny_when_gh_reports_empty(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", lambda argv, cwd, timeout: (0, json.dumps([])))
    payload = _payload("Bash", _bash("git push origin --delete feat/x"), role="pr-shepherd", cwd=tmp_path)
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert out["hookSpecificOutput"]["permissionDecisionReason"].startswith(
        "zprof guard [remote_ref_delete_unmerged]:")


def test_branch_pr_merged_deny_when_gh_errors(tmp_path, monkeypatch):
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", lambda argv, cwd, timeout: (1, ""))
    payload = _payload("Bash", _bash("git push origin --delete feat/x"), role="pr-shepherd", cwd=tmp_path)
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert out["hookSpecificOutput"]["permissionDecisionReason"].startswith(
        "zprof guard [remote_ref_delete_unmerged]:")


def test_branch_pr_merged_deny_on_timeout_without_context_error(tmp_path, monkeypatch):
    """Fail-closed, not fail-silent: unlike the other three evaluators, a
    timeout here must NOT produce a `context_error` -- the deny itself is
    the recorded decision (ADR-0005 E6)."""
    _write_config(tmp_path, build_guard_config())
    monkeypatch.setattr(zprof_guard, "_run", lambda argv, cwd, timeout: (None, "TimeoutExpired"))
    payload = _payload("Bash", _bash("git push origin --delete feat/x"), role="pr-shepherd",
                        cwd=tmp_path, session_id="sess-branch-timeout")
    out = zprof_guard.pre_tool(payload)
    assert out is not None

    events = _read_events(tmp_path)
    assert not [e for e in events if e["event"] == "context_error"]
    assert any(e["event"] == "pre-tool" and e["decision"] == "deny" for e in events)


def test_branch_pr_merged_deny_on_unparseable_branch_name_without_calling_gh(tmp_path, monkeypatch):
    """`git push origin --delete a b` deletes two refs -- ambiguous -> deny
    without ever calling `gh` (spec §5.2 footnote / ADR-0005 E6)."""
    _write_config(tmp_path, build_guard_config())

    def _fail_if_called(argv, cwd, timeout):
        raise AssertionError(f"gh must not be called for an unparseable command, got {argv!r}")

    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    payload = _payload("Bash", _bash("git push origin --delete a b"), role="pr-shepherd", cwd=tmp_path)
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert out["hookSpecificOutput"]["permissionDecisionReason"].startswith(
        "zprof guard [remote_ref_delete_unmerged]:")


@pytest.mark.parametrize("command", [
    "git push origin :refs/tags/v1",         # refs/tags/... is unparseable, not refs/heads/
    "git push origin --delete",              # no refspec at all
])
def test_branch_pr_merged_deny_on_other_unparseable_forms_without_calling_gh(tmp_path, monkeypatch, command):
    _write_config(tmp_path, build_guard_config())

    def _fail_if_called(argv, cwd, timeout):
        raise AssertionError(f"gh must not be called for {command!r}, got {argv!r}")

    monkeypatch.setattr(zprof_guard, "_run", _fail_if_called)
    payload = _payload("Bash", _bash(command), role="pr-shepherd", cwd=tmp_path)
    out = zprof_guard.pre_tool(payload)
    assert out is not None
    assert out["hookSpecificOutput"]["permissionDecisionReason"].startswith(
        "zprof guard [remote_ref_delete_unmerged]:")
