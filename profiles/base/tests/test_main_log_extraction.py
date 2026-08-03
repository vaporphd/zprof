#!/usr/bin/env python3
"""Tests for main log extraction — sync, async, legacy paths."""
import hashlib, json, os, pathlib, shutil, sys
import pytest

# Add parent to path so we can import the module directly
sys.path.insert(0, str(pathlib.Path(__file__).parent.parent))

# Import extraction internals
# We import from zprof-collect.py which has a hyphen, so use importlib
import importlib
_mod_path = pathlib.Path(__file__).parent.parent / "zprof-collect.py"
_spec = importlib.util.spec_from_file_location("zprof_collect", _mod_path)
zprof_collect = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(zprof_collect)

_extract_main_log = zprof_collect._extract_main_log
_sha256_head = zprof_collect._sha256_head
_parse_task_notification_xml = zprof_collect._parse_task_notification_xml

FIXTURES = pathlib.Path(__file__).parent / "fixtures"


def _empty_sess():
    """Return a fresh session state dict (no prior offset)."""
    return {
        "main_log_offset": 0,
        "main_log_size": 0,
        "main_log_head_sha": "",
        "agents_done": [],
    }


class TestSyncDispatch:
    """Path 1: assistant tool_use + user toolUseResult (completed)."""

    def test_extracts_dispatch_id(self):
        dispatches, meta = _extract_main_log(
            "sess-sync-001", FIXTURES / "sync_dispatch.jsonl", _empty_sess())
        assert len(dispatches) == 1
        assert dispatches[0]["dispatch_id"] == "toolu_01SyncDispatchAAAAAAAAAA"

    def test_extracts_role(self):
        dispatches, _ = _extract_main_log(
            "sess-sync-001", FIXTURES / "sync_dispatch.jsonl", _empty_sess())
        assert dispatches[0]["role"] == "Explore"

    def test_extracts_model_resolved(self):
        dispatches, _ = _extract_main_log(
            "sess-sync-001", FIXTURES / "sync_dispatch.jsonl", _empty_sess())
        assert dispatches[0]["model_resolved"] == "claude-sonnet-5"

    def test_status_completed(self):
        dispatches, _ = _extract_main_log(
            "sess-sync-001", FIXTURES / "sync_dispatch.jsonl", _empty_sess())
        assert dispatches[0]["status"] == "completed"
        assert dispatches[0]["dispatch_complete"] is True

    def test_extracts_duration_and_tool_uses(self):
        dispatches, _ = _extract_main_log(
            "sess-sync-001", FIXTURES / "sync_dispatch.jsonl", _empty_sess())
        d = dispatches[0]
        assert d["duration_ms"] == 28000
        assert d["tool_uses"] == 12
        assert d["total_tokens"] == 45000

    def test_extracts_token_breakdown(self):
        dispatches, _ = _extract_main_log(
            "sess-sync-001", FIXTURES / "sync_dispatch.jsonl", _empty_sess())
        d = dispatches[0]
        assert d["tokens_input"] == 30000
        assert d["tokens_output"] == 8000
        assert d["tokens_cache_read"] == 5000
        assert d["tokens_cache_creation"] == 2000

    def test_harness_version_extracted(self):
        _, meta = _extract_main_log(
            "sess-sync-001", FIXTURES / "sync_dispatch.jsonl", _empty_sess())
        assert meta["harness_version"] == "2.1.220"

    def test_session_id_set(self):
        dispatches, _ = _extract_main_log(
            "sess-sync-001", FIXTURES / "sync_dispatch.jsonl", _empty_sess())
        assert dispatches[0]["session_id"] == "sess-sync-001"

    def test_seq_is_zero_for_sync(self):
        dispatches, _ = _extract_main_log(
            "sess-sync-001", FIXTURES / "sync_dispatch.jsonl", _empty_sess())
        assert dispatches[0]["seq"] == 0

    def test_no_unparsed_lines(self):
        _, meta = _extract_main_log(
            "sess-sync-001", FIXTURES / "sync_dispatch.jsonl", _empty_sess())
        assert meta["unparsed_lines"] == 0


class TestAsyncDispatch:
    """Path 2: async launch + task-notification completion."""

    def test_async_launch_and_completion(self):
        dispatches, _ = _extract_main_log(
            "sess-async-001", FIXTURES / "async_dispatch.jsonl", _empty_sess())
        # 1 async_launched + 1 completed (queue-op and user deduplicated)
        launches = [d for d in dispatches if d.get("status") == "async_launched"]
        completions = [d for d in dispatches if d.get("status") == "completed"]
        assert len(launches) == 1
        assert len(completions) == 1

    def test_async_launch_not_complete(self):
        dispatches, _ = _extract_main_log(
            "sess-async-001", FIXTURES / "async_dispatch.jsonl", _empty_sess())
        launch = [d for d in dispatches if d["status"] == "async_launched"][0]
        assert launch["dispatch_complete"] is False
        assert launch["agent_id"] == "abcdef1234567890a"

    def test_async_completion_is_complete(self):
        dispatches, _ = _extract_main_log(
            "sess-async-001", FIXTURES / "async_dispatch.jsonl", _empty_sess())
        completions = [d for d in dispatches if d["status"] == "completed"]
        assert len(completions) == 1
        assert completions[0]["dispatch_complete"] is True

    def test_async_dispatch_id_matches(self):
        dispatches, _ = _extract_main_log(
            "sess-async-001", FIXTURES / "async_dispatch.jsonl", _empty_sess())
        ids = {d["dispatch_id"] for d in dispatches}
        assert "toolu_01AsyncDispatchAAAAAAAAA" in ids

    def test_model_on_async_launch(self):
        dispatches, _ = _extract_main_log(
            "sess-async-001", FIXTURES / "async_dispatch.jsonl", _empty_sess())
        launch = [d for d in dispatches if d["status"] == "async_launched"][0]
        assert launch["model_resolved"] == "claude-sonnet-5"


class TestLegacyTaskNotification:
    """Path 3: queue-operation with legacy task-notification XML."""

    def test_legacy_dispatch_extracted(self):
        dispatches, _ = _extract_main_log(
            "sess-legacy-001", FIXTURES / "task_notification_legacy.jsonl", _empty_sess())
        # queue-operation + user message deduplicated to 1 notification
        assert len(dispatches) == 1

    def test_legacy_dispatch_id(self):
        dispatches, _ = _extract_main_log(
            "sess-legacy-001", FIXTURES / "task_notification_legacy.jsonl", _empty_sess())
        assert dispatches[0]["dispatch_id"] == "toolu_01LegacyDispatchAAAAAAAA"

    def test_legacy_status_completed(self):
        dispatches, _ = _extract_main_log(
            "sess-legacy-001", FIXTURES / "task_notification_legacy.jsonl", _empty_sess())
        assert dispatches[0]["dispatch_complete"] is True

    def test_legacy_subagent_tokens(self):
        dispatches, _ = _extract_main_log(
            "sess-legacy-001", FIXTURES / "task_notification_legacy.jsonl", _empty_sess())
        assert dispatches[0]["total_tokens"] == 55000

    def test_legacy_tool_uses(self):
        dispatches, _ = _extract_main_log(
            "sess-legacy-001", FIXTURES / "task_notification_legacy.jsonl", _empty_sess())
        assert dispatches[0]["tool_uses"] == 18

    def test_legacy_duration(self):
        dispatches, _ = _extract_main_log(
            "sess-legacy-001", FIXTURES / "task_notification_legacy.jsonl", _empty_sess())
        assert dispatches[0]["duration_ms"] == 42000

    def test_legacy_agent_id(self):
        dispatches, _ = _extract_main_log(
            "sess-legacy-001", FIXTURES / "task_notification_legacy.jsonl", _empty_sess())
        assert dispatches[0]["agent_id"] == "legacy123456abcde"


class TestTruncatedSession:
    """Truncated file: parse up to last complete line."""

    def test_truncated_extracts_complete_lines(self):
        dispatches, meta = _extract_main_log(
            "sess-trunc-001", FIXTURES / "truncated_session.jsonl", _empty_sess())
        # The fixture has 3 valid lines + 1 truncated — should parse 1 dispatch
        assert len(dispatches) == 1
        assert dispatches[0]["dispatch_id"] == "toolu_01TruncDispatchAAAAAAAA"

    def test_truncated_flag_set(self):
        _, meta = _extract_main_log(
            "sess-trunc-001", FIXTURES / "truncated_session.jsonl", _empty_sess())
        assert meta["truncated"] is True


class TestCompactBoundary:
    """File with compact_boundary marker — dispatches before and after."""

    def test_both_dispatches_extracted(self):
        dispatches, _ = _extract_main_log(
            "sess-compact-001", FIXTURES / "with_compact_boundary.jsonl", _empty_sess())
        ids = [d["dispatch_id"] for d in dispatches]
        assert "toolu_01CompactBeforeAAAAAAAA" in ids
        assert "toolu_01CompactAfterAAAAAAAAAA" in ids

    def test_compact_boundary_does_not_disrupt(self):
        dispatches, meta = _extract_main_log(
            "sess-compact-001", FIXTURES / "with_compact_boundary.jsonl", _empty_sess())
        assert len(dispatches) == 2
        assert meta["unparsed_lines"] == 0


class TestNotificationDedup:
    """Claude Code writes every notification twice — queue-op + user.
    The second must be silently dropped, not counted as seq=2."""

    def test_duplicate_notification_deduped(self):
        dispatches, _ = _extract_main_log(
            "sess-async-001", FIXTURES / "async_dispatch.jsonl", _empty_sess())
        completions = [d for d in dispatches if d["status"] == "completed"]
        # The fixture has queue-op AND user with same dispatch_id+status
        assert len(completions) == 1
        assert completions[0]["seq"] == 1  # first (and only) notification

    def test_legacy_duplicate_deduped(self):
        dispatches, _ = _extract_main_log(
            "sess-legacy-001", FIXTURES / "task_notification_legacy.jsonl", _empty_sess())
        assert len(dispatches) == 1
        assert dispatches[0]["seq"] == 1


class TestMultiNotify:
    """Same task-id notified twice via SendMessage resume.

    This requires two extraction passes (different invocations) because
    within one pass, same (dispatch_id, status) is deduped.
    """

    def test_cross_invocation_seq_persisted(self, tmp_path):
        """Invocation 1 gets seq=1; invocation 2 (resume) gets seq=2."""
        log = tmp_path / "session.jsonl"
        # Write first notification batch
        lines_1 = [
            json.dumps({"type": "summary", "sessionId": "s1",
                        "timestamp": "2026-08-01T14:00:00Z", "version": "2.1.220"}),
            json.dumps({"type": "user", "message": {"role": "user",
                        "content": "<task-notification>\n<task-id>agent1</task-id>\n"
                                   "<tool-use-id>toolu_01Multi</tool-use-id>\n"
                                   "<status>completed</status>\n"
                                   "<summary>First pass</summary>\n</task-notification>"},
                        "timestamp": "2026-08-01T14:05:00Z",
                        "sessionId": "s1", "version": "2.1.220"}),
        ]
        log.write_text("\n".join(lines_1) + "\n")

        sess = _empty_sess()
        d1, m1 = _extract_main_log("s1", log, sess)
        assert len(d1) == 1
        assert d1[0]["seq"] == 1

        # Persist state (simulates what _collect_session does)
        sess["main_log_offset"] = m1["offset"]
        sess["main_log_size"] = m1["size"]
        sess["main_log_head_sha"] = m1["head_sha"]
        sess["notify_seq"] = m1["notify_seq"]

        # Append second notification (after SendMessage resume)
        with open(log, "a") as f:
            f.write(json.dumps({"type": "user", "message": {"role": "user",
                    "content": "<task-notification>\n<task-id>agent1</task-id>\n"
                               "<tool-use-id>toolu_01Multi</tool-use-id>\n"
                               "<status>completed</status>\n"
                               "<summary>Second pass</summary>\n</task-notification>"},
                    "timestamp": "2026-08-01T14:15:00Z",
                    "sessionId": "s1", "version": "2.1.220"}) + "\n")

        d2, m2 = _extract_main_log("s1", log, sess)
        assert len(d2) == 1
        assert d2[0]["seq"] == 2  # persisted notify_seq ensures seq=2

    def test_launch_has_seq_zero(self):
        dispatches, _ = _extract_main_log(
            "sess-multi-001", FIXTURES / "multi_notify.jsonl", _empty_sess())
        launch = [d for d in dispatches if d["status"] == "async_launched"][0]
        assert launch["seq"] == 0


class TestOffsetVerification:
    """Offset + hash verification for incremental reads."""

    def test_second_read_returns_only_new(self, tmp_path):
        """After first read, appending lines => only new dispatches."""
        # Write initial content
        log = tmp_path / "test.jsonl"
        with open(FIXTURES / "sync_dispatch.jsonl") as f:
            initial = f.read()
        log.write_text(initial)

        # First read
        sess = _empty_sess()
        dispatches1, meta1 = _extract_main_log("test", log, sess)
        assert len(dispatches1) == 1

        # Update session state with meta from first read
        sess["main_log_offset"] = meta1["offset"]
        sess["main_log_size"] = meta1["size"]
        sess["main_log_head_sha"] = meta1["head_sha"]

        # Append a new dispatch line
        with open(log, "a") as f:
            f.write(json.dumps({
                "type": "assistant",
                "message": {"role": "assistant", "model": "claude-sonnet-5",
                            "content": [{"type": "tool_use",
                                         "id": "toolu_01SecondDispatchAAAAAAAA",
                                         "name": "Agent",
                                         "input": {"subagent_type": "Plan",
                                                   "prompt": "Plan."}}]},
                "timestamp": "2026-08-01T10:10:00.000Z",
                "sessionId": "test", "version": "2.1.220"
            }) + "\n")
            f.write(json.dumps({
                "type": "user",
                "message": {"role": "user",
                            "content": [{"type": "tool_result",
                                         "tool_use_id": "toolu_01SecondDispatchAAAAAAAA",
                                         "content": "Plan complete."}]},
                "toolUseResult": {"status": "completed",
                                  "agentType": "Plan",
                                  "resolvedModel": "claude-sonnet-5",
                                  "totalDurationMs": 5000,
                                  "totalTokens": 10000,
                                  "totalToolUseCount": 3},
                "timestamp": "2026-08-01T10:10:30.000Z",
                "sessionId": "test", "version": "2.1.220"
            }) + "\n")

        # Second read with updated session state
        dispatches2, meta2 = _extract_main_log("test", log, sess)
        assert len(dispatches2) == 1
        assert dispatches2[0]["dispatch_id"] == "toolu_01SecondDispatchAAAAAAAA"

    def test_tampered_file_re_reads_from_zero(self, tmp_path):
        """If the file head changes, re-read from zero."""
        log = tmp_path / "test.jsonl"
        with open(FIXTURES / "sync_dispatch.jsonl") as f:
            original = f.read()
        log.write_text(original)

        # First read
        sess = _empty_sess()
        dispatches1, meta1 = _extract_main_log("test", log, sess)
        assert len(dispatches1) == 1

        sess["main_log_offset"] = meta1["offset"]
        sess["main_log_size"] = meta1["size"]
        sess["main_log_head_sha"] = meta1["head_sha"]

        # Tamper: rewrite with different content
        log.write_text(original.replace("sess-sync-001", "sess-tampered"))

        # Second read — should re-read from zero
        dispatches2, _ = _extract_main_log("test", log, sess)
        assert len(dispatches2) == 1  # re-read gets the dispatch again
        assert dispatches2[0]["session_id"] == "test"  # session_id comes from param, not file

    def test_shrunken_file_re_reads_from_zero(self, tmp_path):
        """If the file got smaller, re-read from zero."""
        log = tmp_path / "test.jsonl"
        with open(FIXTURES / "sync_dispatch.jsonl") as f:
            original = f.read()
        log.write_text(original)

        sess = _empty_sess()
        _, meta1 = _extract_main_log("test", log, sess)
        sess["main_log_offset"] = meta1["offset"]
        sess["main_log_size"] = meta1["size"]
        sess["main_log_head_sha"] = meta1["head_sha"]

        # Shrink the file
        log.write_text(original[:50] + "\n")

        dispatches, _ = _extract_main_log("test", log, sess)
        # Should re-read from zero (file shrank)
        assert isinstance(dispatches, list)

    def test_unchanged_file_returns_empty(self, tmp_path):
        """If nothing was appended, return no new dispatches."""
        log = tmp_path / "test.jsonl"
        with open(FIXTURES / "sync_dispatch.jsonl") as f:
            original = f.read()
        log.write_text(original)

        sess = _empty_sess()
        _, meta1 = _extract_main_log("test", log, sess)
        sess["main_log_offset"] = meta1["offset"]
        sess["main_log_size"] = meta1["size"]
        sess["main_log_head_sha"] = meta1["head_sha"]

        # Read again without changes
        dispatches, _ = _extract_main_log("test", log, sess)
        assert dispatches == []


class TestMissingFile:
    """Non-existent file path."""

    def test_missing_file_returns_empty(self):
        dispatches, meta = _extract_main_log(
            "none", pathlib.Path("/nonexistent/path.jsonl"), _empty_sess())
        assert dispatches == []
        assert meta["offset"] == 0


class TestParseTaskNotificationXml:
    """Unit tests for _parse_task_notification_xml."""

    def test_standard_notification(self):
        xml = """<task-notification>
<task-id>abc123</task-id>
<tool-use-id>toolu_01ABC</tool-use-id>
<status>completed</status>
<summary>Agent finished</summary>
</task-notification>"""
        result = _parse_task_notification_xml(xml)
        assert result["task_id"] == "abc123"
        assert result["tool_use_id"] == "toolu_01ABC"
        assert result["status"] == "completed"

    def test_legacy_with_tokens(self):
        xml = """<task-notification>
<task-id>xyz789</task-id>
<tool-use-id>toolu_01XYZ</tool-use-id>
<status>completed</status>
<subagent_tokens>50000</subagent_tokens>
<tool_uses>15</tool_uses>
<duration_ms>30000</duration_ms>
</task-notification>"""
        result = _parse_task_notification_xml(xml)
        assert result["subagent_tokens"] == 50000
        assert result["tool_uses"] == 15
        assert result["duration_ms"] == 30000

    def test_no_notification_returns_none(self):
        assert _parse_task_notification_xml("just some text") is None

    def test_missing_tool_use_id_returns_none(self):
        xml = "<task-notification><task-id>abc</task-id></task-notification>"
        assert _parse_task_notification_xml(xml) is None


class TestTerminalStatuses:
    """dispatch_complete must be True for all terminal statuses,
    not just 'completed'. Real data has 'failed', 'killed', 'stopped'."""

    def test_failed_is_terminal(self, tmp_path):
        log = tmp_path / "failed.jsonl"
        lines = [
            json.dumps({"type": "summary", "sessionId": "s1",
                        "timestamp": "2026-08-01T10:00:00Z", "version": "2.1.220"}),
            json.dumps({"type": "user", "message": {"role": "user",
                        "content": "<task-notification>\n<task-id>a1</task-id>\n"
                                   "<tool-use-id>toolu_01Failed</tool-use-id>\n"
                                   "<status>failed</status>\n"
                                   "<summary>Agent failed</summary>\n</task-notification>"},
                        "timestamp": "2026-08-01T10:05:00Z",
                        "sessionId": "s1", "version": "2.1.220"}),
        ]
        log.write_text("\n".join(lines) + "\n")
        dispatches, _ = _extract_main_log("s1", log, _empty_sess())
        assert len(dispatches) == 1
        assert dispatches[0]["status"] == "failed"
        assert dispatches[0]["dispatch_complete"] is True

    def test_killed_is_terminal(self, tmp_path):
        log = tmp_path / "killed.jsonl"
        lines = [
            json.dumps({"type": "summary", "sessionId": "s1",
                        "timestamp": "2026-08-01T10:00:00Z", "version": "2.1.220"}),
            json.dumps({"type": "user", "message": {"role": "user",
                        "content": "<task-notification>\n<task-id>a2</task-id>\n"
                                   "<tool-use-id>toolu_01Killed</tool-use-id>\n"
                                   "<status>killed</status>\n"
                                   "<summary>Agent killed</summary>\n</task-notification>"},
                        "timestamp": "2026-08-01T10:05:00Z",
                        "sessionId": "s1", "version": "2.1.220"}),
        ]
        log.write_text("\n".join(lines) + "\n")
        dispatches, _ = _extract_main_log("s1", log, _empty_sess())
        assert dispatches[0]["dispatch_complete"] is True

    def test_stopped_is_terminal(self, tmp_path):
        log = tmp_path / "stopped.jsonl"
        lines = [
            json.dumps({"type": "summary", "sessionId": "s1",
                        "timestamp": "2026-08-01T10:00:00Z", "version": "2.1.220"}),
            json.dumps({"type": "user", "message": {"role": "user",
                        "content": "<task-notification>\n<task-id>a3</task-id>\n"
                                   "<tool-use-id>toolu_01Stopped</tool-use-id>\n"
                                   "<status>stopped</status>\n"
                                   "<summary>Agent stopped</summary>\n</task-notification>"},
                        "timestamp": "2026-08-01T10:05:00Z",
                        "sessionId": "s1", "version": "2.1.220"}),
        ]
        log.write_text("\n".join(lines) + "\n")
        dispatches, _ = _extract_main_log("s1", log, _empty_sess())
        assert dispatches[0]["dispatch_complete"] is True

    def test_async_launched_is_not_terminal(self):
        dispatches, _ = _extract_main_log(
            "sess-async-001", FIXTURES / "async_dispatch.jsonl", _empty_sess())
        launch = [d for d in dispatches if d["status"] == "async_launched"][0]
        assert launch["dispatch_complete"] is False

    def test_sync_completed_via_toolUseResult(self, tmp_path):
        """toolUseResult with status='failed' should be dispatch_complete=True."""
        log = tmp_path / "sync_fail.jsonl"
        lines = [
            json.dumps({"type": "summary", "sessionId": "s1",
                        "timestamp": "2026-08-01T10:00:00Z", "version": "2.1.220"}),
            json.dumps({"type": "assistant", "message": {"role": "assistant",
                        "content": [{"type": "tool_use", "id": "toolu_01SyncFail",
                                     "name": "Agent",
                                     "input": {"subagent_type": "impl", "prompt": "x"}}]},
                        "timestamp": "2026-08-01T10:00:01Z", "sessionId": "s1",
                        "version": "2.1.220"}),
            json.dumps({"type": "user", "message": {"role": "user",
                        "content": [{"type": "tool_result",
                                     "tool_use_id": "toolu_01SyncFail",
                                     "content": "Error."}]},
                        "toolUseResult": {"status": "failed",
                                          "agentType": "impl",
                                          "resolvedModel": "claude-sonnet-5"},
                        "timestamp": "2026-08-01T10:00:30Z", "sessionId": "s1",
                        "version": "2.1.220"}),
        ]
        log.write_text("\n".join(lines) + "\n")
        dispatches, _ = _extract_main_log("s1", log, _empty_sess())
        assert len(dispatches) == 1
        assert dispatches[0]["status"] == "failed"
        assert dispatches[0]["dispatch_complete"] is True


class TestSkillToolUseResult:
    """Skill invocations have toolUseResult but are NOT Agent/Task dispatches.

    They should not produce dispatch records (no matching pending tool_use).
    """

    def test_skill_not_extracted_as_dispatch(self, tmp_path):
        log = tmp_path / "skill.jsonl"
        lines = [
            json.dumps({"type": "summary", "sessionId": "s1",
                        "timestamp": "2026-08-01T10:00:00Z", "version": "2.1.220"}),
            json.dumps({"type": "assistant", "message": {"role": "assistant",
                        "content": [{"type": "tool_use", "id": "toolu_01Skill",
                                     "name": "Skill",
                                     "input": {"skill": "grilling"}}]},
                        "timestamp": "2026-08-01T10:00:01Z", "sessionId": "s1",
                        "version": "2.1.220"}),
            json.dumps({"type": "user", "message": {"role": "user",
                        "content": [{"type": "tool_result",
                                     "tool_use_id": "toolu_01Skill",
                                     "content": "Skill loaded."}]},
                        "toolUseResult": {"success": True, "commandName": "grilling"},
                        "timestamp": "2026-08-01T10:00:02Z", "sessionId": "s1",
                        "version": "2.1.220"}),
        ]
        log.write_text("\n".join(lines) + "\n")
        dispatches, _ = _extract_main_log("s1", log, _empty_sess())
        # Skill is not Agent/Task, so no dispatch should be extracted
        assert len(dispatches) == 0
