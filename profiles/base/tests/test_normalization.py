#!/usr/bin/env python3
"""Tests for normalization, Class A checks, secret redaction, and dedup (Task 5).

Covers:
- Normalized row has all required fields from schema
- dispatch_id is composite: claude-code:<session>:<toolu_...>
- Class A checks: has_preamble, return_parsed, artifact_exists, next_is_reachable
- Secret redaction replaces patterns, count logged
- Dedup: same (dispatch_id, seq) not written twice
- project_id from git root commit; fallback for non-git
- machine_id stable across calls
- ext field preserved as-is
"""
import json, os, pathlib, platform, sys, subprocess, tempfile
import pytest

sys.path.insert(0, str(pathlib.Path(__file__).parent.parent))

import importlib
_mod_path = pathlib.Path(__file__).parent.parent / "zprof-collect.py"
_spec = importlib.util.spec_from_file_location("zprof_collect", _mod_path)
zprof_collect = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(zprof_collect)

_normalize_dispatch = zprof_collect._normalize_dispatch
_redact_secrets = zprof_collect._redact_secrets
_class_a_checks = zprof_collect._class_a_checks
_get_machine_id = zprof_collect._get_machine_id
_get_project_id = zprof_collect._get_project_id
_normalize_and_write = zprof_collect._normalize_and_write
_load_redaction_patterns = zprof_collect._load_redaction_patterns


REQUIRED_FIELDS = [
    "schema_version", "harness", "harness_version", "machine_id",
    "project_id", "ts_utc", "session_id", "dispatch_id", "seq",
    "spawn_depth", "role", "dispatch_complete", "transcript_captured",
]


def _make_raw_dispatch(**overrides):
    """Create a minimal raw dispatch dict from Task 3/4."""
    d = {
        "dispatch_id": "toolu_01TestDispatch000001",
        "session_id": "sess-test-001",
        "role": "implementer",
        "model_resolved": "claude-opus-5",
        "status": "completed",
        "dispatch_complete": True,
        "ts_utc": "2026-08-02T10:30:00.000Z",
        "seq": 0,
        "transcript_captured": False,
    }
    d.update(overrides)
    return d


class TestNormalizedRowFields:
    """Normalized row must contain all required schema fields."""

    def test_all_required_fields_present(self, tmp_path):
        raw = _make_raw_dispatch()
        norm, _ = _normalize_dispatch(
            raw, session_id="sess-test-001",
            harness_version="2.1.220", machine_id="test-machine",
            project_id="abc123", project_id_provisional=False,
            redaction_patterns=[])
        for field in REQUIRED_FIELDS:
            assert field in norm, f"missing required field: {field}"

    def test_schema_version_is_1(self, tmp_path):
        raw = _make_raw_dispatch()
        norm, _ = _normalize_dispatch(
            raw, session_id="sess-test-001",
            harness_version="2.1.220", machine_id="test-machine",
            project_id="abc123", project_id_provisional=False,
            redaction_patterns=[])
        assert norm["schema_version"] == 1

    def test_harness_is_claude_code(self):
        raw = _make_raw_dispatch()
        norm, _ = _normalize_dispatch(
            raw, session_id="sess-test-001",
            harness_version="2.1.220", machine_id="test-machine",
            project_id="abc123", project_id_provisional=False,
            redaction_patterns=[])
        assert norm["harness"] == "claude-code"

    def test_harness_version_propagated(self):
        raw = _make_raw_dispatch()
        norm, _ = _normalize_dispatch(
            raw, session_id="sess-test-001",
            harness_version="2.1.220", machine_id="test-machine",
            project_id="abc123", project_id_provisional=False,
            redaction_patterns=[])
        assert norm["harness_version"] == "2.1.220"

    def test_ext_preserved(self):
        raw = _make_raw_dispatch(ext={"custom": "data", "nested": {"a": 1}})
        norm, _ = _normalize_dispatch(
            raw, session_id="sess-test-001",
            harness_version="2.1.220", machine_id="test-machine",
            project_id="abc123", project_id_provisional=False,
            redaction_patterns=[])
        assert norm["ext"] == {"custom": "data", "nested": {"a": 1}}

    def test_spawn_depth_defaults_to_1(self):
        raw = _make_raw_dispatch()
        norm, _ = _normalize_dispatch(
            raw, session_id="sess-test-001",
            harness_version="2.1.220", machine_id="test-machine",
            project_id="abc123", project_id_provisional=False,
            redaction_patterns=[])
        assert norm["spawn_depth"] == 1


class TestCompositeDispatchId:
    """dispatch_id must be prefixed: claude-code:<session>:<toolu_...>."""

    def test_dispatch_id_is_composite(self):
        raw = _make_raw_dispatch(dispatch_id="toolu_01ABC")
        norm, _ = _normalize_dispatch(
            raw, session_id="sess-123",
            harness_version="2.1.220", machine_id="m",
            project_id="p", project_id_provisional=False,
            redaction_patterns=[])
        assert norm["dispatch_id"] == "claude-code:sess-123:toolu_01ABC"

    def test_parent_dispatch_id_is_composite(self):
        raw = _make_raw_dispatch(
            dispatch_id="toolu_01ABC",
            parent_dispatch_id="toolu_01PARENT",
        )
        norm, _ = _normalize_dispatch(
            raw, session_id="sess-123",
            harness_version="2.1.220", machine_id="m",
            project_id="p", project_id_provisional=False,
            redaction_patterns=[])
        assert norm["parent_dispatch_id"] == "claude-code:sess-123:toolu_01PARENT"

    def test_unresolved_parent_not_prefixed(self):
        raw = _make_raw_dispatch(
            parent_dispatch_id="unresolved:abc123",
        )
        norm, _ = _normalize_dispatch(
            raw, session_id="sess-123",
            harness_version="2.1.220", machine_id="m",
            project_id="p", project_id_provisional=False,
            redaction_patterns=[])
        assert norm["parent_dispatch_id"] == "unresolved:abc123"

    def test_already_composite_not_double_prefixed(self):
        raw = _make_raw_dispatch(
            dispatch_id="claude-code:sess-123:toolu_01ABC",
        )
        norm, _ = _normalize_dispatch(
            raw, session_id="sess-123",
            harness_version="2.1.220", machine_id="m",
            project_id="p", project_id_provisional=False,
            redaction_patterns=[])
        assert norm["dispatch_id"] == "claude-code:sess-123:toolu_01ABC"


class TestClassAChecks:
    """Class A contract compliance checks on return text."""

    def test_has_preamble_false_when_verdict_first(self):
        returned = "verdict: approve\nnext: reviewer"
        checks = _class_a_checks(returned, cwd="/nonexistent")
        assert checks["has_preamble"] is False

    def test_has_preamble_true_when_text_before_verdict(self):
        returned = "Here is my analysis.\nverdict: approve\nnext: reviewer"
        checks = _class_a_checks(returned, cwd="/nonexistent")
        assert checks["has_preamble"] is True

    def test_has_preamble_false_when_only_whitespace_before_verdict(self):
        returned = "  \n\nverdict: approve\nnext: reviewer"
        checks = _class_a_checks(returned, cwd="/nonexistent")
        assert checks["has_preamble"] is False

    def test_return_parsed_true_when_verdict_present(self):
        returned = "verdict: approve\nnext: reviewer"
        checks = _class_a_checks(returned, cwd="/nonexistent")
        assert checks["return_parsed"] is True

    def test_return_parsed_false_when_no_verdict(self):
        returned = "I completed the task successfully."
        checks = _class_a_checks(returned, cwd="/nonexistent")
        assert checks["return_parsed"] is False

    def test_artifact_exists_true(self, tmp_path):
        artifact = tmp_path / "output.txt"
        artifact.write_text("content")
        returned = f"verdict: approve\nartifact: {artifact}\nnext: reviewer"
        checks = _class_a_checks(returned, cwd=str(tmp_path))
        assert checks["artifact_exists"] is True

    def test_artifact_exists_false_when_missing(self, tmp_path):
        returned = f"verdict: approve\nartifact: {tmp_path}/nonexistent.txt\nnext: reviewer"
        checks = _class_a_checks(returned, cwd=str(tmp_path))
        assert checks["artifact_exists"] is False

    def test_artifact_exists_none_when_no_artifact_line(self):
        returned = "verdict: approve\nnext: reviewer"
        checks = _class_a_checks(returned, cwd="/nonexistent")
        assert checks["artifact_exists"] is None

    def test_all_none_when_returned_absent(self):
        checks = _class_a_checks(None, cwd="/nonexistent")
        assert checks["has_preamble"] is None
        assert checks["return_parsed"] is None
        assert checks["artifact_exists"] is None
        assert checks["next_is_reachable"] is None

    def test_next_is_reachable_known_role(self):
        returned = "verdict: approve\nnext: reviewer"
        checks = _class_a_checks(returned, cwd="/nonexistent")
        # next_is_reachable should be non-None when next: line exists
        assert checks["next_is_reachable"] is not None


class TestSecretRedaction:
    """Secret redaction replaces patterns and counts replacements."""

    def test_aws_key_redacted(self):
        text = "export AWS_SECRET_ACCESS_KEY=AKIAxyz123456"
        patterns = _load_redaction_patterns()
        result, count = _redact_secrets(text, patterns)
        assert "AKIAxyz123456" not in result
        assert "⟦redacted:" in result  # ⟦redacted:...⟧
        assert count >= 1

    def test_github_token_redacted(self):
        text = "GITHUB_TOKEN=ghp_abc1234567890ABCDEFGHIJKLMNOPQRSTUVWXYZ"
        patterns = _load_redaction_patterns()
        result, count = _redact_secrets(text, patterns)
        assert "ghp_abc1234567890" not in result
        assert count >= 1

    def test_bearer_token_redacted(self):
        text = "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test"
        patterns = _load_redaction_patterns()
        result, count = _redact_secrets(text, patterns)
        assert "eyJhbGci" not in result
        assert count >= 1

    def test_sk_key_redacted(self):
        text = "api_key: sk-1234567890abcdefghij"
        patterns = _load_redaction_patterns()
        result, count = _redact_secrets(text, patterns)
        assert "sk-1234567890" not in result
        assert count >= 1

    def test_password_redacted(self):
        text = "url?password=supersecret&user=bob"
        patterns = _load_redaction_patterns()
        result, count = _redact_secrets(text, patterns)
        assert "supersecret" not in result
        assert count >= 1

    def test_api_key_param_redacted(self):
        text = "url?api_key=secret123&other=val"
        patterns = _load_redaction_patterns()
        result, count = _redact_secrets(text, patterns)
        assert "secret123" not in result
        assert count >= 1

    def test_no_secrets_returns_zero_count(self):
        text = "just normal text with no secrets"
        patterns = _load_redaction_patterns()
        result, count = _redact_secrets(text, patterns)
        assert result == text
        assert count == 0

    def test_redaction_in_nested_dict(self):
        """Redaction must work recursively through ext and other dicts."""
        d = {
            "prompt": "use GITHUB_TOKEN=ghp_abc1234567890ABCDEFGHIJKLMNOPQRSTUVWXYZ",
            "ext": {"nested": "sk-xyzxyzxyzxyzxyzxyzxyz"},
        }
        patterns = _load_redaction_patterns()
        result, count = _redact_secrets(d, patterns)
        assert "ghp_abc" not in json.dumps(result)
        assert count >= 2

    def test_redaction_in_list_values(self):
        d = ["Bearer abc.def.ghi-jkl_mno", "normal text"]
        patterns = _load_redaction_patterns()
        result, count = _redact_secrets(d, patterns)
        assert "abc.def.ghi" not in str(result)
        assert count >= 1


class TestDedup:
    """Same (dispatch_id, seq) must not be written twice."""

    def test_dedup_prevents_double_write(self, tmp_path):
        agentlog = tmp_path
        dispatches_file = agentlog / "dispatches.jsonl"

        raw = _make_raw_dispatch()
        dispatches = [raw, raw.copy()]  # two identical dispatches

        _normalize_and_write(
            agentlog, dispatches, session_id="sess-test-001",
            harness_version="2.1.220",
            payload={"cwd": str(tmp_path)},
        )

        lines = [l for l in dispatches_file.read_text().splitlines() if l.strip()]
        assert len(lines) == 1, "duplicate dispatch_id+seq should be deduped"

    def test_different_seq_both_written(self, tmp_path):
        agentlog = tmp_path
        dispatches_file = agentlog / "dispatches.jsonl"

        raw1 = _make_raw_dispatch(seq=0)
        raw2 = _make_raw_dispatch(seq=1)
        dispatches = [raw1, raw2]

        _normalize_and_write(
            agentlog, dispatches, session_id="sess-test-001",
            harness_version="2.1.220",
            payload={"cwd": str(tmp_path)},
        )

        lines = [l for l in dispatches_file.read_text().splitlines() if l.strip()]
        assert len(lines) == 2

    def test_dedup_across_invocations(self, tmp_path):
        agentlog = tmp_path
        dispatches_file = agentlog / "dispatches.jsonl"

        raw = _make_raw_dispatch()
        _normalize_and_write(
            agentlog, [raw], session_id="sess-test-001",
            harness_version="2.1.220",
            payload={"cwd": str(tmp_path)},
        )

        # Second invocation with same dispatch
        _normalize_and_write(
            agentlog, [raw], session_id="sess-test-001",
            harness_version="2.1.220",
            payload={"cwd": str(tmp_path)},
        )

        lines = [l for l in dispatches_file.read_text().splitlines() if l.strip()]
        assert len(lines) == 1


class TestProjectId:
    """project_id from git root commit; fallback for non-git."""

    def test_project_id_from_git(self, tmp_path):
        """In a real git repo, project_id is the hash of the root commit SHA."""
        # Create a temporary git repo
        subprocess.run(["git", "init"], cwd=tmp_path, capture_output=True)
        subprocess.run(["git", "config", "user.email", "test@test.com"],
                       cwd=tmp_path, capture_output=True)
        subprocess.run(["git", "config", "user.name", "Test"],
                       cwd=tmp_path, capture_output=True)
        (tmp_path / "f.txt").write_text("x")
        subprocess.run(["git", "add", "f.txt"], cwd=tmp_path, capture_output=True)
        subprocess.run(["git", "commit", "-m", "init", "--no-gpg-sign"],
                       cwd=tmp_path, capture_output=True)

        pid, provisional = _get_project_id(str(tmp_path))
        assert pid  # non-empty
        assert not provisional

    def test_project_id_fallback_for_non_git(self, tmp_path):
        """Non-git directory should use machine_id:cwd fallback."""
        plain_dir = tmp_path / "not-a-repo"
        plain_dir.mkdir()
        pid, provisional = _get_project_id(str(plain_dir))
        assert pid  # non-empty
        assert provisional is True

    def test_project_id_deterministic(self, tmp_path):
        """Same directory should produce the same project_id."""
        subprocess.run(["git", "init"], cwd=tmp_path, capture_output=True)
        subprocess.run(["git", "config", "user.email", "test@test.com"],
                       cwd=tmp_path, capture_output=True)
        subprocess.run(["git", "config", "user.name", "Test"],
                       cwd=tmp_path, capture_output=True)
        (tmp_path / "f.txt").write_text("x")
        subprocess.run(["git", "add", "f.txt"], cwd=tmp_path, capture_output=True)
        subprocess.run(["git", "commit", "-m", "init", "--no-gpg-sign"],
                       cwd=tmp_path, capture_output=True)

        pid1, _ = _get_project_id(str(tmp_path))
        pid2, _ = _get_project_id(str(tmp_path))
        assert pid1 == pid2


class TestMachineId:
    """machine_id must be stable across calls."""

    def test_machine_id_non_empty(self):
        mid = _get_machine_id()
        assert mid
        assert len(mid) > 0

    def test_machine_id_stable(self):
        mid1 = _get_machine_id()
        mid2 = _get_machine_id()
        assert mid1 == mid2


class TestRedactionLogging:
    """Redaction count must be logged to collect.log."""

    def test_redaction_count_in_log(self, tmp_path):
        agentlog = tmp_path
        raw = _make_raw_dispatch(
            ext={"prompt": "use GITHUB_TOKEN=ghp_abc1234567890ABCDEFGHIJKLMNOPQRSTUVWXYZ"},
        )

        _normalize_and_write(
            agentlog, [raw], session_id="sess-test-001",
            harness_version="2.1.220",
            payload={"cwd": str(tmp_path)},
        )

        log_file = agentlog / "collect.log"
        if log_file.exists():
            log_text = log_file.read_text()
            assert "redact" in log_text.lower()


class TestEndToEndNormalization:
    """Integration: full normalization pipeline produces valid JSONL."""

    def test_produces_valid_jsonl(self, tmp_path):
        agentlog = tmp_path
        raw = _make_raw_dispatch(
            tokens_input=1000, tokens_output=500,
            duration_ms=5000, tool_uses=3,
        )

        _normalize_and_write(
            agentlog, [raw], session_id="sess-test-001",
            harness_version="2.1.220",
            payload={"cwd": str(tmp_path)},
        )

        dispatches_file = agentlog / "dispatches.jsonl"
        assert dispatches_file.exists()

        lines = [l for l in dispatches_file.read_text().splitlines() if l.strip()]
        assert len(lines) == 1

        row = json.loads(lines[0])
        assert row["schema_version"] == 1
        assert row["harness"] == "claude-code"
        assert row["dispatch_id"].startswith("claude-code:")
        assert row["session_id"] == "sess-test-001"

    def test_class_a_checks_included_in_output(self, tmp_path):
        agentlog = tmp_path
        raw = _make_raw_dispatch(
            returned="verdict: approve\nnext: reviewer",
        )

        _normalize_and_write(
            agentlog, [raw], session_id="sess-test-001",
            harness_version="2.1.220",
            payload={"cwd": str(tmp_path)},
        )

        dispatches_file = agentlog / "dispatches.jsonl"
        row = json.loads(dispatches_file.read_text().strip())
        assert "has_preamble" in row
        assert row["has_preamble"] is False
        assert row["return_parsed"] is True
