#!/usr/bin/env python3
"""Tests for subagent transcript capture (Task 4).

Covers: transcript extraction, gzip copy, dispatch enrichment,
tool-results copy, running/done agent skipping, meta.json correlation.
"""
import gzip, json, os, pathlib, shutil, sys
import pytest

sys.path.insert(0, str(pathlib.Path(__file__).parent.parent))

import importlib
_mod_path = pathlib.Path(__file__).parent.parent / "zprof-collect.py"
_spec = importlib.util.spec_from_file_location("zprof_collect", _mod_path)
zprof_collect = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(zprof_collect)

_extract_subagent_transcript = zprof_collect._extract_subagent_transcript
_gzip_copy = zprof_collect._gzip_copy
_collect_subagent_transcripts = zprof_collect._collect_subagent_transcripts
_extract_main_log = zprof_collect._extract_main_log

FIXTURES = pathlib.Path(__file__).parent / "fixtures"


def _empty_sess():
    return {
        "main_log_offset": 0,
        "main_log_size": 0,
        "main_log_head_sha": "",
        "agents_done": [],
    }


def _make_meta_json(agent_type="implementer", tool_use_id="toolu_01ABC",
                    parent_agent_id="", spawn_depth=1):
    """Return a meta.json dict matching real Claude Code format."""
    meta = {
        "agentType": agent_type,
        "toolUseId": tool_use_id,
        "spawnDepth": spawn_depth,
    }
    if parent_agent_id:
        meta["parentAgentId"] = parent_agent_id
    return meta


def _make_transcript_lines(model="claude-opus-5",
                           input_tokens=10000, output_tokens=2000,
                           cache_read=80000, cache_creation=5000,
                           turns=2):
    """Generate minimal subagent transcript JSONL lines."""
    lines = [
        json.dumps({"type": "summary", "sessionId": "agent-test",
                    "timestamp": "2026-08-01T11:00:00Z", "version": "2.1.220"}),
    ]
    for i in range(turns):
        lines.append(json.dumps({
            "type": "assistant",
            "message": {
                "role": "assistant",
                "model": model,
                "content": [{"type": "text", "text": f"Turn {i+1} response."}],
                "usage": {
                    "input_tokens": input_tokens,
                    "cache_creation_input_tokens": cache_creation,
                    "cache_read_input_tokens": cache_read,
                    "output_tokens": output_tokens,
                    "server_tool_use": {"web_search_requests": 0},
                },
            },
            "uuid": f"u{i}",
            "timestamp": f"2026-08-01T11:00:0{i+1}.000Z",
            "sessionId": "agent-test",
            "version": "2.1.220",
        }))
        # Add a user turn between assistant turns (except after last)
        if i < turns - 1:
            lines.append(json.dumps({
                "type": "user",
                "message": {"role": "user", "content": "Continue."},
                "timestamp": f"2026-08-01T11:00:0{i+1}.500Z",
            }))
    return "\n".join(lines) + "\n"


def _setup_subagents_dir(tmp_path, agent_id="deadbeef01234567",
                         meta=None, transcript_content=None):
    """Create a realistic session directory with subagents/ structure.

    Returns (transcript_path, agentlog_dir).
    """
    # Simulate: ~/.claude/projects/slug/session-id.jsonl
    # So session dir = ~/.claude/projects/slug/session-id/
    session_dir = tmp_path / "session-id"
    transcript_path = tmp_path / "session-id.jsonl"

    # Create main transcript (minimal)
    transcript_path.write_text(json.dumps({
        "type": "summary", "sessionId": "session-id",
        "timestamp": "2026-08-01T10:00:00Z", "version": "2.1.220"
    }) + "\n")

    # Create subagents dir
    subagents_dir = session_dir / "subagents"
    subagents_dir.mkdir(parents=True, exist_ok=True)

    if meta is None:
        meta = _make_meta_json()

    # Write meta.json
    meta_path = subagents_dir / f"agent-{agent_id}.meta.json"
    meta_path.write_text(json.dumps(meta))

    # Write transcript
    if transcript_content is None:
        transcript_content = _make_transcript_lines()
    transcript_file = subagents_dir / f"agent-{agent_id}.jsonl"
    transcript_file.write_text(transcript_content)

    # agentlog dir
    agentlog = tmp_path / ".agentlog"
    agentlog.mkdir(parents=True, exist_ok=True)

    return str(transcript_path), agentlog


# -----------------------------------------------------------------------
# _extract_subagent_transcript unit tests
# -----------------------------------------------------------------------

class TestExtractSubagentTranscript:
    """Unit tests for _extract_subagent_transcript."""

    def test_extracts_from_fixture(self):
        result = _extract_subagent_transcript(FIXTURES / "subagent_transcript.jsonl")
        assert result["model"] == "claude-opus-5"
        # 2 assistant turns: 10000+12000 = 22000
        assert result["tokens_input"] == 22000
        # 2000+3000 = 5000
        assert result["tokens_output"] == 5000
        # 80000+90000 = 170000
        assert result["tokens_cache_read"] == 170000
        # 5000+0 = 5000
        assert result["tokens_cache_creation"] == 5000

    def test_missing_file_returns_zeros(self):
        result = _extract_subagent_transcript(pathlib.Path("/nonexistent/file.jsonl"))
        assert result["tokens_input"] == 0
        assert result["tokens_output"] == 0
        assert result["model"] == ""

    def test_empty_file(self, tmp_path):
        f = tmp_path / "empty.jsonl"
        f.write_text("")
        result = _extract_subagent_transcript(f)
        assert result["tokens_input"] == 0
        assert result["model"] == ""

    def test_no_assistant_turns(self, tmp_path):
        f = tmp_path / "no_assistant.jsonl"
        f.write_text(json.dumps({
            "type": "user", "message": {"role": "user", "content": "Hello"}
        }) + "\n")
        result = _extract_subagent_transcript(f)
        assert result["tokens_input"] == 0
        assert result["tokens_output"] == 0

    def test_model_from_last_turn(self, tmp_path):
        """If model changes between turns, keep the last one."""
        f = tmp_path / "multi_model.jsonl"
        lines = []
        for model in ["claude-sonnet-5", "claude-opus-5"]:
            lines.append(json.dumps({
                "type": "assistant",
                "message": {
                    "role": "assistant", "model": model,
                    "content": [{"type": "text", "text": "x"}],
                    "usage": {"input_tokens": 100, "output_tokens": 50,
                              "cache_read_input_tokens": 0,
                              "cache_creation_input_tokens": 0},
                },
            }))
        f.write_text("\n".join(lines) + "\n")
        result = _extract_subagent_transcript(f)
        assert result["model"] == "claude-opus-5"
        assert result["tokens_input"] == 200

    def test_malformed_lines_skipped(self, tmp_path):
        f = tmp_path / "malformed.jsonl"
        content = (
            '{"type":"assistant","message":{"role":"assistant","model":"claude-opus-5",'
            '"content":[],"usage":{"input_tokens":100,"output_tokens":50,'
            '"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}\n'
            'this is not json\n'
            '{"type":"assistant","message":{"role":"assistant","model":"claude-opus-5",'
            '"content":[],"usage":{"input_tokens":200,"output_tokens":75,'
            '"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}\n'
        )
        f.write_text(content)
        result = _extract_subagent_transcript(f)
        assert result["tokens_input"] == 300
        assert result["tokens_output"] == 125


# -----------------------------------------------------------------------
# _gzip_copy unit tests
# -----------------------------------------------------------------------

class TestGzipCopy:
    def test_creates_gzip_file(self, tmp_path):
        src = tmp_path / "source.txt"
        src.write_text("Hello world\n" * 100)
        dst = tmp_path / "out" / "source.txt.gz"
        _gzip_copy(src, dst)
        assert dst.exists()
        # Decompress and verify content
        with gzip.open(dst, "rb") as f:
            assert f.read() == src.read_bytes()

    def test_gzip_compresses(self, tmp_path):
        src = tmp_path / "big.txt"
        src.write_text("repeated content\n" * 1000)
        dst = tmp_path / "big.txt.gz"
        _gzip_copy(src, dst)
        assert dst.stat().st_size < src.stat().st_size

    def test_creates_parent_dirs(self, tmp_path):
        src = tmp_path / "source.txt"
        src.write_text("data")
        dst = tmp_path / "a" / "b" / "c" / "out.gz"
        _gzip_copy(src, dst)
        assert dst.exists()


# -----------------------------------------------------------------------
# _collect_subagent_transcripts integration tests
# -----------------------------------------------------------------------

class TestCollectSubagentTranscripts:
    """Integration tests for _collect_subagent_transcripts."""

    def test_enriches_matching_dispatch(self, tmp_path):
        """When main log dispatch matches meta.json toolUseId, enrich it."""
        agent_id = "deadbeef01234567"
        tool_use_id = "toolu_01AsyncDispatchAAAAAAAAA"
        meta = _make_meta_json(
            agent_type="implementer",
            tool_use_id=tool_use_id,
            parent_agent_id="a54a62add7dc3a394",
            spawn_depth=2,
        )
        tp, agentlog = _setup_subagents_dir(
            tmp_path, agent_id=agent_id, meta=meta,
            transcript_content=_make_transcript_lines(
                model="claude-opus-5",
                input_tokens=5000, output_tokens=1000,
                cache_read=40000, cache_creation=2000, turns=3,
            ),
        )
        sess = _empty_sess()
        # Simulate a dispatch from main log extraction
        dispatches = [{
            "dispatch_id": tool_use_id,
            "session_id": "session-id",
            "role": "implementer",
            "status": "completed",
            "dispatch_complete": True,
            "seq": 1,
            "ts_utc": "2026-08-01T11:05:00Z",
        }]

        _collect_subagent_transcripts(
            agentlog, "session-id", tp, set(), sess, dispatches)

        d = dispatches[0]
        assert d["role"] == "implementer"
        assert d["spawn_depth"] == 2
        assert d["parent_dispatch_id"] == "harness:session:a54a62add7dc3a394"
        assert d["transcript_captured"] is True
        assert d["transcript_ref"].startswith("transcripts/")
        assert d["transcript_ref"].endswith(".jsonl.gz")
        # 3 turns * 5000 = 15000
        assert d["tokens_input"] == 15000
        # 3 turns * 1000 = 3000
        assert d["tokens_output"] == 3000
        assert d["tokens_cache_read"] == 120000  # 3 * 40000
        assert d["tokens_cache_creation"] == 6000  # 3 * 2000
        assert d["model_resolved"] == "claude-opus-5"

    def test_creates_gzipped_transcript(self, tmp_path):
        """Transcript file is gzip-copied to .agentlog/transcripts/."""
        agent_id = "deadbeef01234567"
        tp, agentlog = _setup_subagents_dir(tmp_path, agent_id=agent_id)
        sess = _empty_sess()
        dispatches = []

        _collect_subagent_transcripts(
            agentlog, "session-id", tp, set(), sess, dispatches)

        gz_path = agentlog / "transcripts" / f"{agent_id}.jsonl.gz"
        assert gz_path.exists()
        # Verify it decompresses to valid content
        with gzip.open(gz_path, "rb") as f:
            content = f.read().decode("utf-8")
        assert "assistant" in content

    def test_skips_running_agents(self, tmp_path):
        """Agents in running_agents set are skipped."""
        agent_id = "deadbeef01234567"
        tp, agentlog = _setup_subagents_dir(tmp_path, agent_id=agent_id)
        sess = _empty_sess()
        dispatches = []

        _collect_subagent_transcripts(
            agentlog, "session-id", tp, {agent_id}, sess, dispatches)

        # Should not have created transcript file
        gz_path = agentlog / "transcripts" / f"{agent_id}.jsonl.gz"
        assert not gz_path.exists()
        # Agent not added to agents_done
        assert agent_id not in sess.get("agents_done", [])

    def test_skips_done_agents(self, tmp_path):
        """Agents already in agents_done are skipped."""
        agent_id = "deadbeef01234567"
        tp, agentlog = _setup_subagents_dir(tmp_path, agent_id=agent_id)
        sess = _empty_sess()
        sess["agents_done"] = [agent_id]
        dispatches = []

        _collect_subagent_transcripts(
            agentlog, "session-id", tp, set(), sess, dispatches)

        # Should not have created transcript file
        gz_path = agentlog / "transcripts" / f"{agent_id}.jsonl.gz"
        assert not gz_path.exists()
        # No new dispatches created
        assert len(dispatches) == 0

    def test_marks_agents_done(self, tmp_path):
        """Processed agents are added to agents_done in state."""
        agent_id = "deadbeef01234567"
        tp, agentlog = _setup_subagents_dir(tmp_path, agent_id=agent_id)
        sess = _empty_sess()
        dispatches = []

        _collect_subagent_transcripts(
            agentlog, "session-id", tp, set(), sess, dispatches)

        assert agent_id in sess["agents_done"]

    def test_creates_dispatch_when_no_match(self, tmp_path):
        """When meta.json toolUseId has no matching dispatch, create one."""
        agent_id = "deadbeef01234567"
        meta = _make_meta_json(
            agent_type="code-reviewer",
            tool_use_id="toolu_01NoMatch",
            spawn_depth=1,
        )
        tp, agentlog = _setup_subagents_dir(
            tmp_path, agent_id=agent_id, meta=meta)
        sess = _empty_sess()
        dispatches = []  # no dispatches from main log

        _collect_subagent_transcripts(
            agentlog, "session-id", tp, set(), sess, dispatches)

        assert len(dispatches) == 1
        d = dispatches[0]
        assert d["dispatch_id"] == "toolu_01NoMatch"
        assert d["session_id"] == "session-id"
        assert d["agent_id"] == agent_id
        assert d["role"] == "code-reviewer"
        assert d["transcript_captured"] is True

    def test_no_subagents_dir(self, tmp_path):
        """When there's no subagents/ directory, nothing crashes."""
        transcript_path = tmp_path / "session-id.jsonl"
        transcript_path.write_text('{"type":"summary"}\n')
        agentlog = tmp_path / ".agentlog"
        agentlog.mkdir()
        sess = _empty_sess()
        dispatches = [{"dispatch_id": "toolu_01X", "session_id": "s1"}]

        _collect_subagent_transcripts(
            agentlog, "s1", str(transcript_path), set(), sess, dispatches)

        # Dispatch should get transcript_captured=false
        assert dispatches[0]["transcript_captured"] is False

    def test_transcript_captured_false_for_unmatched(self, tmp_path):
        """Dispatches without matching transcript get transcript_captured=false."""
        transcript_path = tmp_path / "session-id.jsonl"
        transcript_path.write_text('{"type":"summary"}\n')
        agentlog = tmp_path / ".agentlog"
        agentlog.mkdir()
        sess = _empty_sess()
        dispatches = [
            {"dispatch_id": "toolu_01A", "session_id": "s1"},
            {"dispatch_id": "toolu_01B", "session_id": "s1"},
        ]

        _collect_subagent_transcripts(
            agentlog, "s1", str(transcript_path), set(), sess, dispatches)

        for d in dispatches:
            assert d["transcript_captured"] is False

    def test_multiple_agents(self, tmp_path):
        """Multiple subagents in one session are all processed."""
        session_dir = tmp_path / "session-id"
        transcript_path = tmp_path / "session-id.jsonl"
        transcript_path.write_text('{"type":"summary"}\n')
        subagents_dir = session_dir / "subagents"
        subagents_dir.mkdir(parents=True)

        agent_ids = ["agent111111111111", "agent222222222222"]
        tool_ids = ["toolu_01First", "toolu_01Second"]

        for aid, tid in zip(agent_ids, tool_ids):
            meta = _make_meta_json(agent_type="impl", tool_use_id=tid)
            (subagents_dir / f"agent-{aid}.meta.json").write_text(json.dumps(meta))
            (subagents_dir / f"agent-{aid}.jsonl").write_text(
                _make_transcript_lines(turns=1, input_tokens=500, output_tokens=100))

        agentlog = tmp_path / ".agentlog"
        agentlog.mkdir()
        sess = _empty_sess()
        dispatches = []

        _collect_subagent_transcripts(
            agentlog, "session-id", str(transcript_path), set(), sess, dispatches)

        assert len(dispatches) == 2
        assert set(d["dispatch_id"] for d in dispatches) == set(tool_ids)
        assert set(sess["agents_done"]) == set(agent_ids)
        # Both transcripts gzipped
        for aid in agent_ids:
            assert (agentlog / "transcripts" / f"{aid}.jsonl.gz").exists()

    def test_no_parent_agent_id(self, tmp_path):
        """When meta.json has no parentAgentId, parent_dispatch_id is not set."""
        agent_id = "deadbeef01234567"
        meta = _make_meta_json(
            agent_type="Explore",
            tool_use_id="toolu_01NoParent",
            spawn_depth=1,
        )
        # No parentAgentId in meta
        tp, agentlog = _setup_subagents_dir(
            tmp_path, agent_id=agent_id, meta=meta)
        sess = _empty_sess()
        dispatches = []

        _collect_subagent_transcripts(
            agentlog, "session-id", tp, set(), sess, dispatches)

        d = dispatches[0]
        assert "parent_dispatch_id" not in d
        assert d["spawn_depth"] == 1


class TestCollectSubagentToolResults:
    """Tool-results directory copy."""

    def test_copies_tool_results(self, tmp_path):
        """Files from tool-results/ are copied to .agentlog/tool-results/."""
        session_dir = tmp_path / "session-id"
        transcript_path = tmp_path / "session-id.jsonl"
        transcript_path.write_text('{"type":"summary"}\n')

        tr_dir = session_dir / "tool-results"
        tr_dir.mkdir(parents=True)
        (tr_dir / "result-001.txt").write_text("tool output data")
        (tr_dir / "result-002.txt").write_text("more output")

        agentlog = tmp_path / ".agentlog"
        agentlog.mkdir()
        sess = _empty_sess()
        dispatches = []

        _collect_subagent_transcripts(
            agentlog, "session-id", str(transcript_path), set(), sess, dispatches)

        assert (agentlog / "tool-results" / "result-001.txt").exists()
        assert (agentlog / "tool-results" / "result-002.txt").exists()
        assert (agentlog / "tool-results" / "result-001.txt").read_text() == "tool output data"

    def test_does_not_overwrite_existing_tool_results(self, tmp_path):
        """Existing tool-results files are not overwritten."""
        session_dir = tmp_path / "session-id"
        transcript_path = tmp_path / "session-id.jsonl"
        transcript_path.write_text('{"type":"summary"}\n')

        tr_dir = session_dir / "tool-results"
        tr_dir.mkdir(parents=True)
        (tr_dir / "result-001.txt").write_text("new version")

        agentlog = tmp_path / ".agentlog"
        dest_tr = agentlog / "tool-results"
        dest_tr.mkdir(parents=True)
        (dest_tr / "result-001.txt").write_text("old version")

        sess = _empty_sess()
        dispatches = []

        _collect_subagent_transcripts(
            agentlog, "session-id", str(transcript_path), set(), sess, dispatches)

        # Should keep old version
        assert (dest_tr / "result-001.txt").read_text() == "old version"

    def test_no_tool_results_dir(self, tmp_path):
        """When there's no tool-results/ directory, nothing crashes."""
        transcript_path = tmp_path / "session-id.jsonl"
        transcript_path.write_text('{"type":"summary"}\n')
        agentlog = tmp_path / ".agentlog"
        agentlog.mkdir()
        sess = _empty_sess()
        dispatches = []

        # Should not raise
        _collect_subagent_transcripts(
            agentlog, "session-id", str(transcript_path), set(), sess, dispatches)


class TestEndToEndWithMainLog:
    """End-to-end: main log extraction + subagent transcript enrichment."""

    def test_async_dispatch_enriched_by_transcript(self, tmp_path):
        """An async dispatch from main log is enriched with transcript data."""
        # Set up main transcript (async dispatch fixture)
        session_dir = tmp_path / "sess-async-001"
        main_log = tmp_path / "sess-async-001.jsonl"
        shutil.copy(FIXTURES / "async_dispatch.jsonl", main_log)

        # Set up subagent transcript for the async agent
        agent_id = "abcdef1234567890a"
        subagents_dir = session_dir / "subagents"
        subagents_dir.mkdir(parents=True)

        meta = {
            "agentType": "implementer",
            "description": "Task 1: implement feature",
            "toolUseId": "toolu_01AsyncDispatchAAAAAAAAA",
            "parentAgentId": "root_session_id",
            "spawnDepth": 1,
        }
        (subagents_dir / f"agent-{agent_id}.meta.json").write_text(json.dumps(meta))
        (subagents_dir / f"agent-{agent_id}.jsonl").write_text(
            _make_transcript_lines(
                model="claude-opus-5",
                input_tokens=25000, output_tokens=8000,
                cache_read=100000, cache_creation=3000, turns=2,
            ))

        agentlog = tmp_path / ".agentlog"
        agentlog.mkdir()
        sess = _empty_sess()

        # Step 1: extract main log
        dispatches, meta_out = _extract_main_log(
            "sess-async-001", main_log, sess)

        # Step 2: enrich with transcripts
        _collect_subagent_transcripts(
            agentlog, "sess-async-001", str(main_log), set(), sess, dispatches)

        # Find the completion dispatch (seq=1, status=completed)
        completions = [d for d in dispatches if d.get("status") == "completed"]
        assert len(completions) >= 1
        comp = completions[0]

        # Should be enriched
        assert comp["transcript_captured"] is True
        assert comp["role"] == "implementer"
        assert comp["spawn_depth"] == 1
        assert comp["parent_dispatch_id"] == "harness:session:root_session_id"
        assert comp["model_resolved"] == "claude-opus-5"
        # 2 turns: 25000*2 = 50000
        assert comp["tokens_input"] == 50000
        assert comp["tokens_output"] == 16000

        # Launch dispatch should also be enriched (same dispatch_id)
        launches = [d for d in dispatches if d.get("status") == "async_launched"]
        assert len(launches) >= 1
        launch = launches[0]
        assert launch["transcript_captured"] is True

        # Transcript file should exist
        gz = agentlog / "transcripts" / f"{agent_id}.jsonl.gz"
        assert gz.exists()

        # Agent marked as done
        assert agent_id in sess["agents_done"]

    def test_sync_dispatch_not_enriched_without_transcript(self, tmp_path):
        """Sync dispatches get transcript_captured=false when no transcript exists."""
        main_log = tmp_path / "sess-sync-001.jsonl"
        shutil.copy(FIXTURES / "sync_dispatch.jsonl", main_log)

        agentlog = tmp_path / ".agentlog"
        agentlog.mkdir()
        sess = _empty_sess()

        dispatches, _ = _extract_main_log("sess-sync-001", main_log, sess)
        _collect_subagent_transcripts(
            agentlog, "sess-sync-001", str(main_log), set(), sess, dispatches)

        assert len(dispatches) == 1
        assert dispatches[0]["transcript_captured"] is False


class TestMetaJsonEdgeCases:
    """Edge cases in meta.json parsing."""

    def test_missing_transcript_file(self, tmp_path):
        """meta.json exists but transcript .jsonl is missing."""
        session_dir = tmp_path / "session-id"
        transcript_path = tmp_path / "session-id.jsonl"
        transcript_path.write_text('{"type":"summary"}\n')
        subagents_dir = session_dir / "subagents"
        subagents_dir.mkdir(parents=True)

        agent_id = "deadbeef01234567"
        meta = _make_meta_json(tool_use_id="toolu_01NoTranscript")
        (subagents_dir / f"agent-{agent_id}.meta.json").write_text(json.dumps(meta))
        # No .jsonl file created

        agentlog = tmp_path / ".agentlog"
        agentlog.mkdir()
        sess = _empty_sess()
        dispatches = []

        _collect_subagent_transcripts(
            agentlog, "session-id", str(transcript_path), set(), sess, dispatches)

        # Should still create dispatch from meta
        assert len(dispatches) == 1
        d = dispatches[0]
        assert d["transcript_captured"] is False  # no transcript to copy
        assert "tokens_input" not in d  # no usage data when transcript missing
        assert d["role"] == "implementer"
        # Agent still marked as done
        assert agent_id in sess["agents_done"]

    def test_malformed_meta_json(self, tmp_path):
        """Malformed meta.json is skipped with error log."""
        session_dir = tmp_path / "session-id"
        transcript_path = tmp_path / "session-id.jsonl"
        transcript_path.write_text('{"type":"summary"}\n')
        subagents_dir = session_dir / "subagents"
        subagents_dir.mkdir(parents=True)

        agent_id = "deadbeef01234567"
        (subagents_dir / f"agent-{agent_id}.meta.json").write_text("not valid json{")
        (subagents_dir / f"agent-{agent_id}.jsonl").write_text(
            _make_transcript_lines(turns=1))

        agentlog = tmp_path / ".agentlog"
        agentlog.mkdir()
        sess = _empty_sess()
        dispatches = []

        _collect_subagent_transcripts(
            agentlog, "session-id", str(transcript_path), set(), sess, dispatches)

        # No dispatch created (meta unreadable)
        assert len(dispatches) == 0
        # Error should be logged
        log = (agentlog / "collect.log").read_text()
        assert "failed to read meta.json" in log

    def test_empty_tool_use_id(self, tmp_path):
        """meta.json with empty toolUseId creates dispatch with meta: prefix."""
        agent_id = "deadbeef01234567"
        meta = _make_meta_json(tool_use_id="")
        tp, agentlog = _setup_subagents_dir(
            tmp_path, agent_id=agent_id, meta=meta)
        sess = _empty_sess()
        dispatches = []

        _collect_subagent_transcripts(
            agentlog, "session-id", tp, set(), sess, dispatches)

        assert len(dispatches) == 1
        assert dispatches[0]["dispatch_id"] == f"meta:{agent_id}"
