#!/usr/bin/env python3
"""Tests for tool-events extraction (spec §5 C3)."""
import json, pathlib, sys
import pytest

sys.path.insert(0, str(pathlib.Path(__file__).parent.parent))
import importlib
_mod_path = pathlib.Path(__file__).parent.parent / "zprof-collect.py"
_spec = importlib.util.spec_from_file_location("zprof_collect", _mod_path)
zprof_collect = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(zprof_collect)

_extract_tool_events = zprof_collect._extract_tool_events
_write_tool_events = zprof_collect._write_tool_events
_input_hash = zprof_collect._input_hash
_collect_subagent_transcripts = zprof_collect._collect_subagent_transcripts


def _assistant(ts, *blocks):
    return json.dumps({"type": "assistant", "timestamp": ts,
                       "message": {"role": "assistant", "model": "claude-sonnet-5",
                                   "content": list(blocks),
                                   "usage": {"input_tokens": 10, "output_tokens": 5,
                                             "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0}}})


def _user_result(ts, tool_use_id, content, is_error=False):
    block = {"type": "tool_result", "tool_use_id": tool_use_id, "content": content}
    if is_error:
        block["is_error"] = True
    return json.dumps({"type": "user", "timestamp": ts,
                       "message": {"role": "user", "content": [block]}})


def _write_transcript(path, lines):
    path.write_text("\n".join(lines) + "\n")


def test_extract_tool_events_basic(tmp_path):
    t = tmp_path / "agent-abc.jsonl"
    _write_transcript(t, [
        _assistant("2026-09-26T10:00:01Z",
                   {"type": "tool_use", "id": "tu1", "name": "Read", "input": {"file_path": "/p/a.swift"}}),
        _user_result("2026-09-26T10:00:02Z", "tu1", "contents"),
        _assistant("2026-09-26T10:00:03Z",
                   {"type": "tool_use", "id": "tu2", "name": "Bash", "input": {"command": "swift test --package-path Packages/Core"}}),
        _user_result("2026-09-26T10:00:09Z", "tu2", "error: build failed", is_error=True),
        _assistant("2026-09-26T10:00:10Z",
                   {"type": "tool_use", "id": "tu3", "name": "Agent", "input": {"subagent_type": "tester", "prompt": "x"}}),
        _user_result("2026-09-26T10:00:20Z", "tu3", "verdict: done"),
        _assistant("2026-09-26T10:00:21Z",
                   {"type": "tool_use", "id": "tu4", "name": "Grep", "input": {"pattern": "TODO", "path": "/p"}}),
        # tu4 never gets a tool_result (truncated transcript)
    ])
    events = _extract_tool_events(t)
    assert [e["tool"] for e in events] == ["Read", "Bash", "Grep"], "Agent call must be excluded"
    assert [e["seq"] for e in events] == [1, 2, 3]
    assert events[0]["target"] == "/p/a.swift" and events[0]["is_error"] is False
    assert events[0]["result_chars"] == len("contents")
    assert events[1]["target"] == "swift test --package-path Packages/Core"
    assert events[1]["is_error"] is True
    assert events[2]["is_error"] is None and events[2]["result_chars"] is None
    assert events[2]["target"] == "/p", "path wins over pattern for target"
    assert events[0]["ts"] == "2026-09-26T10:00:01Z"


def test_input_hash_is_canonical_and_stable():
    a = _input_hash({"command": "swift test", "timeout": 5})
    b = _input_hash({"timeout": 5, "command": "swift test"})
    assert a == b and len(a) == 12
    assert _input_hash({"command": "swift build"}) != a


def test_target_truncates_long_commands_and_flattens_newlines():
    t = pathlib.Path(__file__).parent / "_tmp_cmd.jsonl"
    try:
        _write_transcript(t, [_assistant("ts", {"type": "tool_use", "id": "x", "name": "Bash",
                                               "input": {"command": "cat > f.txt <<'EOF'\n" + "y" * 100 + "\nEOF"}})])
        ev = _extract_tool_events(t)[0]
        assert "\n" not in ev["target"] and len(ev["target"]) == 60
    finally:
        t.unlink(missing_ok=True)


def test_write_tool_events_rows_and_redaction(tmp_path):
    agentlog = tmp_path / ".agentlog"
    agentlog.mkdir()
    import re
    pats = [("sk-", re.compile(r"sk-[a-zA-Z0-9]{20,}"))]
    events = [{"seq": 1, "ts": "t", "tool": "Bash", "input_hash": "abc", "target": "curl -H sk-AAAAAAAAAAAAAAAAAAAAAAAA", "is_error": False, "result_chars": 3},
              {"seq": 2, "ts": "t", "tool": "Read", "input_hash": "def", "target": "/p", "is_error": None, "result_chars": None}]
    n = _write_tool_events(agentlog, "claude-code:s1:tu9", events, pats)
    assert n == 2
    rows = [json.loads(l) for l in (agentlog / "tool-events.jsonl").read_text().splitlines()]
    assert rows[0]["dispatch_id"] == "claude-code:s1:tu9" and rows[0]["schema_version"] == 1
    assert "sk-AAAA" not in rows[0]["target"] and "redacted" in rows[0]["target"]
    assert "is_error" not in rows[1] and "result_chars" not in rows[1], "None values are dropped"


def test_collect_writes_tool_events_with_composite_id(tmp_path):
    session_id = "sess-te"
    main = tmp_path / f"{session_id}.jsonl"
    main.write_text("")
    sub = tmp_path / session_id / "subagents"
    sub.mkdir(parents=True)
    (sub / "agent-a1.meta.json").write_text(json.dumps({"agentType": "implementer", "toolUseId": "toolu_X", "spawnDepth": 1}))
    _write_transcript(sub / "agent-a1.jsonl", [
        _assistant("2026-09-26T10:00:01Z", {"type": "tool_use", "id": "tu1", "name": "Edit",
                                             "input": {"file_path": "/p/a.swift", "old_string": "a", "new_string": "b"}}),
        _user_result("2026-09-26T10:00:02Z", "tu1", "ok"),
        _assistant("2026-09-26T10:00:03Z", {"type": "text", "text": "verdict: done\nartifact: abc"}),
    ])
    agentlog = tmp_path / ".agentlog"
    agentlog.mkdir()
    sess = {"main_log_offset": 0, "main_log_size": 0, "main_log_head_sha": "", "agents_done": []}
    dispatches = [{"dispatch_id": "toolu_X", "session_id": session_id, "role": "implementer",
                   "status": "completed", "dispatch_complete": True, "seq": 0, "ts_utc": "t"}]
    _collect_subagent_transcripts(agentlog, session_id, str(main), set(), sess, dispatches)
    rows = [json.loads(l) for l in (agentlog / "tool-events.jsonl").read_text().splitlines()]
    assert len(rows) == 1
    assert rows[0]["dispatch_id"] == f"claude-code:{session_id}:toolu_X"
    assert rows[0]["tool"] == "Edit" and rows[0]["target"] == "/p/a.swift"
    # second pass: agent already done → no duplicate rows
    _collect_subagent_transcripts(agentlog, session_id, str(main), set(), sess, dispatches)
    assert len((agentlog / "tool-events.jsonl").read_text().splitlines()) == 1
