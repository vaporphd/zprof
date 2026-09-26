#!/usr/bin/env python3
"""Tests for nested dispatch extraction (spec §5 C1): children of task-runner get full rows."""
import json, pathlib, sys
import pytest

sys.path.insert(0, str(pathlib.Path(__file__).parent.parent))
import importlib
_mod_path = pathlib.Path(__file__).parent.parent / "zprof-collect.py"
_spec = importlib.util.spec_from_file_location("zprof_collect", _mod_path)
zprof_collect = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(zprof_collect)

_extract_dispatches_from_text = zprof_collect._extract_dispatches_from_text
_collect_subagent_transcripts = zprof_collect._collect_subagent_transcripts
_merge_dispatch = zprof_collect._merge_dispatch


def _usage_turn(ts, model="claude-sonnet-5", inp=1000, out=100):
    return json.dumps({"type": "assistant", "timestamp": ts,
                       "message": {"role": "assistant", "model": model,
                                   "content": [{"type": "text", "text": "working"}],
                                   "usage": {"input_tokens": inp, "output_tokens": out,
                                             "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0}}})


def _agent_call(ts, tool_use_id, subagent_type):
    return json.dumps({"type": "assistant", "timestamp": ts,
                       "message": {"role": "assistant", "model": "claude-sonnet-5",
                                   "content": [{"type": "tool_use", "id": tool_use_id, "name": "Agent",
                                                "input": {"description": "d", "subagent_type": subagent_type,
                                                          "run_in_background": False, "prompt": "p"}}]}})


def _agent_result(ts, tool_use_id, agent_type, agent_id, status="completed",
                  tool_uses=5, duration=1500, returned="verdict: done\nartifact: c1"):
    return json.dumps({"type": "user", "timestamp": ts,
                       "message": {"role": "user",
                                   "content": [{"type": "tool_result", "tool_use_id": tool_use_id,
                                                "content": [{"type": "text", "text": returned}]}]},
                       "toolUseResult": {"status": status, "agentId": agent_id, "agentType": agent_type,
                                         "resolvedModel": "claude-sonnet-5", "totalDurationMs": duration,
                                         "totalTokens": 3000, "totalToolUseCount": tool_uses,
                                         "usage": {"input_tokens": 2500, "output_tokens": 500,
                                                   "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0}}})


def _runner_tree(tmp_path, session_id="sess-nested", runner_id="zzzz01", child_id="aaaa01"):
    """Main log with one task-runner dispatch; runner transcript dispatches one implementer child."""
    main = tmp_path / f"{session_id}.jsonl"
    main.write_text("\n".join([
        _agent_call("2026-09-26T10:00:00Z", "toolu_R", "task-runner"),
        _agent_result("2026-09-26T10:30:00Z", "toolu_R", "task-runner", runner_id, tool_uses=3, duration=1800000,
                      returned="verdict: done\nartifact: PR #1\nrun_log: .zprof/runs/2026-09-26-x.md\none_line: ok"),
    ]) + "\n")
    sub = tmp_path / session_id / "subagents"
    sub.mkdir(parents=True)
    (sub / f"agent-{runner_id}.meta.json").write_text(json.dumps(
        {"agentType": "task-runner", "toolUseId": "toolu_R", "spawnDepth": 1}))
    (sub / f"agent-{runner_id}.jsonl").write_text("\n".join([
        _usage_turn("2026-09-26T10:00:01Z"),
        _agent_call("2026-09-26T10:01:00Z", "toolu_C", "implementer"),
        _agent_result("2026-09-26T10:05:00Z", "toolu_C", "implementer", child_id),
        _usage_turn("2026-09-26T10:06:00Z"),
    ]) + "\n")
    (sub / f"agent-{child_id}.meta.json").write_text(json.dumps(
        {"agentType": "implementer", "toolUseId": "toolu_C", "parentAgentId": runner_id, "spawnDepth": 2}))
    (sub / f"agent-{child_id}.jsonl").write_text("\n".join([
        _usage_turn("2026-09-26T10:02:00Z", inp=7000, out=700),
        _usage_turn("2026-09-26T10:04:00Z", inp=8000, out=800),
    ]) + "\n")
    return main, sub


def _sess():
    return {"main_log_offset": 0, "main_log_size": 0, "main_log_head_sha": "", "agents_done": []}


def _run(tmp_path, running=frozenset(), **kw):
    main, _ = _runner_tree(tmp_path, **kw)
    session_id = main.stem
    sess = _sess()
    dispatches, _meta = zprof_collect._extract_main_log(session_id, main, sess)
    agentlog = tmp_path / ".agentlog"
    agentlog.mkdir(exist_ok=True)
    _collect_subagent_transcripts(agentlog, session_id, str(main), set(running), sess, dispatches)
    return {d["dispatch_id"]: d for d in dispatches}, sess


def test_extract_dispatches_from_text_matches_main_log_shape():
    raw = "\n".join([_agent_call("t1", "toolu_A", "tester"),
                     _agent_result("t2", "toolu_A", "tester", "ag1", status="completed", tool_uses=2, duration=99)])
    out = _extract_dispatches_from_text("s", raw, {}, set())
    assert out["truncated"] is False and out["unparsed_lines"] == 0
    d = out["dispatches"][0]
    assert d["dispatch_id"] == "toolu_A" and d["role"] == "tester"
    assert d["status"] == "completed" and d["tool_uses"] == 2 and d["duration_ms"] == 99
    assert d["ts_utc"] == "t2" and d["returned"].startswith("verdict: done")


def test_nested_children_get_full_rows(tmp_path):
    rows, sess = _run(tmp_path)
    child = rows["toolu_C"]
    assert child["role"] == "implementer"
    assert child["status"] == "completed" and child["dispatch_complete"] is True
    assert child["tool_uses"] == 5 and child["duration_ms"] == 1500
    assert child["ts_utc"] == "2026-09-26T10:05:00Z"
    assert child["parent_dispatch_id"] == "toolu_R" and child["spawn_depth"] == 2
    assert child["transcript_captured"] is True
    # tokens come from the child's own transcript (7000+8000), not from toolUseResult (2500)
    assert child["tokens_input"] == 15000 and child["tokens_output"] == 1500
    assert "aaaa01" in sess["agents_done"] and "zzzz01" in sess["agents_done"]


def test_child_sorted_before_parent_still_gets_full_row(tmp_path):
    # agent ids chosen so the child's meta.json sorts BEFORE the runner's
    rows, _ = _run(tmp_path, runner_id="zzzz09", child_id="aaaa09")
    child = rows["toolu_C"]
    assert child["status"] == "completed" and child["tool_uses"] == 5 and child["ts_utc"] != ""


def test_child_of_running_parent_is_deferred(tmp_path):
    rows, sess = _run(tmp_path, running={"zzzz01"})
    assert "toolu_C" not in rows, "child must wait until its parent finishes"
    assert "aaaa01" not in sess["agents_done"]
    assert "zzzz01" not in sess["agents_done"]


def test_merge_dispatch_prefers_fresh_status_but_keeps_transcript_tokens():
    existing = {"dispatch_id": "x", "status": "unknown", "dispatch_complete": False, "ts_utc": "",
                "tokens_input": 15000, "tokens_output": 1500}
    fresh = {"dispatch_id": "x", "status": "completed", "dispatch_complete": True, "ts_utc": "t9",
             "tool_uses": 4, "duration_ms": 10, "tokens_input": 2500, "tokens_output": 500}
    _merge_dispatch(existing, fresh)
    assert existing["status"] == "completed" and existing["dispatch_complete"] is True
    assert existing["ts_utc"] == "t9" and existing["tool_uses"] == 4
    assert existing["tokens_input"] == 15000, "transcript tokens are more accurate than toolUseResult"
