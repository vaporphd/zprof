"""Repro + regression tests for #52: nested async dispatches (children of a
task-runner) get stitched to their <task-notification> the same way main-log
dispatches do.

Root cause (bug-hunter, confirmed): a task-runner's own transcript carries
`<task-notification>` as a `type=="attachment"` record with
`attachment.commandMode=="task-notification"` and the XML in
`attachment.prompt` — a form `_extract_dispatches_from_text` didn't parse at
all (it only looked at `queue-operation` and `user` records). Secondary: the
nested Pass-1 call in `_collect_subagent_transcripts` didn't pass
`agent_index`, so notifications without `<tool-use-id>` couldn't resolve via
meta.json for nested children the way main-log notifications do.
"""
import importlib
import json
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).parent.parent))

_mod_path = pathlib.Path(__file__).parent.parent / "zprof-collect.py"
_spec = importlib.util.spec_from_file_location("zprof_collect", _mod_path)
zprof_collect = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(zprof_collect)

_extract_dispatches_from_text = zprof_collect._extract_dispatches_from_text
_extract_main_log = zprof_collect._extract_main_log
_collect_subagent_transcripts = zprof_collect._collect_subagent_transcripts
_normalize_and_write = zprof_collect._normalize_and_write

SESSION = "sess-nested-attachment"
RUNNER_ID = "rrrrrrrrrrrrrrrrr"
TUID_R = "toolu_Runner"
CHILD_A_ID = "aaaaaaaaaaaaaaaaa"   # notification carries <tool-use-id>
TUID_A = "toolu_ChildA"
CHILD_B_ID = "bbbbbbbbbbbbbbbbb"   # notification carries only <task-id>, resolved via meta.json
TUID_B = "toolu_ChildB"
CHILD_C_ID = "ccccccccccccccccc"   # never notifies — stays async_launched
TUID_C = "toolu_ChildC"

RESULT_A = "verdict: done\nartifact: a.txt\nnext: none\none_line: ok a"
RESULT_B = "verdict: done\nartifact: b.txt\nnext: none\none_line: ok b"


def _usage_turn(ts, inp=1000, out=100):
    return json.dumps({"type": "assistant", "timestamp": ts,
                       "message": {"role": "assistant", "model": "claude-sonnet-5",
                                   "content": [{"type": "text", "text": "working"}],
                                   "usage": {"input_tokens": inp, "output_tokens": out,
                                             "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0}}})


def _agent_call(ts, tool_use_id, subagent_type, run_in_background=False):
    return json.dumps({"type": "assistant", "timestamp": ts,
                       "message": {"role": "assistant", "model": "claude-sonnet-5",
                                   "content": [{"type": "tool_use", "id": tool_use_id, "name": "Agent",
                                                "input": {"description": "d", "subagent_type": subagent_type,
                                                          "run_in_background": run_in_background, "prompt": "p"}}]}})


def _agent_sync_result(ts, tool_use_id, agent_type, agent_id, returned="verdict: done\nartifact: c1"):
    return json.dumps({"type": "user", "timestamp": ts,
                       "message": {"role": "user",
                                   "content": [{"type": "tool_result", "tool_use_id": tool_use_id,
                                                "content": [{"type": "text", "text": returned}]}]},
                       "toolUseResult": {"status": "completed", "agentId": agent_id, "agentType": agent_type,
                                         "resolvedModel": "claude-sonnet-5", "totalDurationMs": 1800000,
                                         "totalTokens": 3000, "totalToolUseCount": 3,
                                         "usage": {"input_tokens": 2500, "output_tokens": 500,
                                                   "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0}}})


def _agent_async_launch(ts, tool_use_id, agent_type, agent_id):
    """Async launch: Agent tool_use + isAsync toolUseResult (status=async_launched)."""
    call = _agent_call(ts, tool_use_id, agent_type, run_in_background=True)
    result = json.dumps({"type": "user", "timestamp": ts,
                         "message": {"role": "user", "content": [{
                             "type": "tool_result", "tool_use_id": tool_use_id,
                             "content": [{"type": "text",
                                          "text": f"Async agent launched successfully.\nagentId: {agent_id}"}]}]},
                         "toolUseResult": {"isAsync": True, "status": "async_launched", "agentId": agent_id,
                                           "description": "d", "resolvedModel": "claude-sonnet-5",
                                           "outputFile": f"/tmp/tasks/{agent_id}.output", "prompt": "p"}})
    return call + "\n" + result


def _notif_xml(task_id, tool_use_id, status, result):
    tags = [f"<task-id>{task_id}</task-id>"]
    if tool_use_id:
        tags.append(f"<tool-use-id>{tool_use_id}</tool-use-id>")
    tags.append(f"<status>{status}</status>")
    tags.append('<summary>Agent finished</summary>')
    tags.append(f"<result>{result}</result>")
    tags.append("<usage><subagent_tokens>500</subagent_tokens><tool_uses>2</tool_uses>"
                "<duration_ms>1200</duration_ms></usage>")
    return "<task-notification>\n" + "\n".join(tags) + "\n</task-notification>"


def _attachment_notification(ts, task_id, tool_use_id, status="completed",
                             result=RESULT_A, source_uuid="src-1"):
    """A task-runner-transcript notification: type=="attachment",
    attachment.commandMode=="task-notification", XML in attachment.prompt."""
    xml = _notif_xml(task_id, tool_use_id, status, result)
    att = {"type": "queued_command", "prompt": xml, "source_uuid": source_uuid,
          "commandMode": "task-notification", "timestamp": ts,
          "usage": {"totalTokens": 500, "toolUses": 2, "durationMs": 1200}}
    return json.dumps({"type": "attachment", "agentId": RUNNER_ID, "attachment": att,
                       "timestamp": ts, "sessionId": SESSION, "uuid": "u-" + source_uuid,
                       "isSidechain": True})


def _write_meta(sub, agent_id, tuid, agent_type, parent_agent_id="", spawn_depth=1):
    sub.mkdir(parents=True, exist_ok=True)
    meta = {"agentType": agent_type, "toolUseId": tuid, "spawnDepth": spawn_depth}
    if parent_agent_id:
        meta["parentAgentId"] = parent_agent_id
    (sub / f"agent-{agent_id}.meta.json").write_text(json.dumps(meta))


def _runner_tree(tmp_path):
    """main log: one synchronous task-runner dispatch.
    Runner transcript: async-launches 3 children; 2 of them notify via
    `attachment`, one never notifies."""
    main = tmp_path / f"{SESSION}.jsonl"
    main.write_text("\n".join([
        _agent_call("2026-09-28T10:00:00Z", TUID_R, "task-runner"),
        _agent_sync_result("2026-09-28T10:30:00Z", TUID_R, "task-runner", RUNNER_ID,
                           returned="verdict: done\nartifact: PR #1\none_line: ok"),
    ]) + "\n")

    sub = tmp_path / SESSION / "subagents"
    _write_meta(sub, RUNNER_ID, TUID_R, "task-runner")
    _write_meta(sub, CHILD_A_ID, TUID_A, "implementer", parent_agent_id=RUNNER_ID, spawn_depth=2)
    _write_meta(sub, CHILD_B_ID, TUID_B, "tester", parent_agent_id=RUNNER_ID, spawn_depth=2)
    _write_meta(sub, CHILD_C_ID, TUID_C, "reviewer", parent_agent_id=RUNNER_ID, spawn_depth=2)

    runner_lines = [
        _usage_turn("2026-09-28T10:00:05Z"),
        _agent_async_launch("2026-09-28T10:01:00Z", TUID_A, "implementer", CHILD_A_ID),
        # Child A: notification WITH <tool-use-id> -- exercises the attachment
        # parsing path directly, no agent_index resolution needed.
        _attachment_notification("2026-09-28T10:05:00Z", CHILD_A_ID, TUID_A,
                                 result=RESULT_A, source_uuid="notif-a"),
        _agent_async_launch("2026-09-28T10:06:00Z", TUID_B, "tester", CHILD_B_ID),
        # Child B: notification WITHOUT <tool-use-id> -- must resolve via the
        # agent_index built from meta.json (Pass 1 wiring fix).
        _attachment_notification("2026-09-28T10:10:00Z", CHILD_B_ID, "",
                                 result=RESULT_B, source_uuid="notif-b"),
        _agent_async_launch("2026-09-28T10:11:00Z", TUID_C, "reviewer", CHILD_C_ID),
        # Child C: no notification at all -- stays async_launched.
    ]
    (sub / f"agent-{RUNNER_ID}.jsonl").write_text("\n".join(runner_lines) + "\n")

    for agent_id in (CHILD_A_ID, CHILD_B_ID, CHILD_C_ID):
        (sub / f"agent-{agent_id}.jsonl").write_text(_usage_turn("2026-09-28T10:02:00Z", inp=2000, out=200) + "\n")

    return main, sub


def _sess():
    return {"main_log_offset": 0, "main_log_size": 0, "main_log_head_sha": "", "agents_done": []}


def _run(tmp_path):
    main, _ = _runner_tree(tmp_path)
    sess = _sess()
    dispatches, _meta = _extract_main_log(SESSION, main, sess)
    agentlog = tmp_path / ".agentlog"
    agentlog.mkdir(exist_ok=True)
    _collect_subagent_transcripts(agentlog, SESSION, str(main), set(), sess, dispatches,
                                  require_completion_evidence=True)
    return dispatches, sess, agentlog


def _by_id(dispatches):
    out = {}
    for d in dispatches:
        out.setdefault(d["dispatch_id"], []).append(d)
    return out


# --- AC3: child with <tool-use-id> resolves via the attachment path,
# child without one resolves via agent_index (meta.json), and a child that
# never notifies stays async_launched -----------------------------------

def test_nested_children_via_attachment_notification(tmp_path):
    dispatches, sess, _agentlog = _run(tmp_path)
    by_id = _by_id(dispatches)

    # Child A: notification carried <tool-use-id> -- must be completed.
    a_rows = by_id[TUID_A]
    assert any(r["status"] == "completed" and r["dispatch_complete"] for r in a_rows), a_rows
    a = [r for r in a_rows if r["status"] == "completed"][-1]
    assert a["parent_dispatch_id"] == TUID_R
    assert a["returned"].startswith("verdict: done")

    # Child B: notification had no <tool-use-id> -- must resolve via
    # agent_index (built from meta.json in Pass 1) and complete too.
    b_rows = by_id[TUID_B]
    assert any(r["status"] == "completed" and r["dispatch_complete"] for r in b_rows), b_rows
    b = [r for r in b_rows if r["status"] == "completed"][-1]
    assert b["parent_dispatch_id"] == TUID_R
    assert b["returned"].startswith("verdict: done")

    # Child C: never notified -- remains async_launched, not agents_done.
    c_rows = by_id[TUID_C]
    assert all(r["status"] == "async_launched" for r in c_rows), c_rows
    assert not any(r.get("dispatch_complete") for r in c_rows)
    assert CHILD_C_ID not in sess["agents_done"]

    assert CHILD_A_ID in sess["agents_done"]
    assert CHILD_B_ID in sess["agents_done"]


def test_nested_children_get_verdict_after_normalize(tmp_path):
    dispatches, _sess, agentlog = _run(tmp_path)
    payload = {"cwd": str(tmp_path)}
    _normalize_and_write(agentlog, dispatches, session_id=SESSION, harness_version="", payload=payload)
    rows = [json.loads(l) for l in (agentlog / "dispatches.jsonl").read_text().splitlines() if l.strip()]

    a_rows = [r for r in rows if r["dispatch_id"] == f"claude-code:{SESSION}:{TUID_A}"]
    assert a_rows, [r["dispatch_id"] for r in rows]
    a = a_rows[-1]
    assert a["verdict"] == "done"
    assert a["dispatch_complete"] is True
    assert a["parent_dispatch_id"] == f"claude-code:{SESSION}:{TUID_R}" or a["parent_dispatch_id"] == TUID_R

    b_rows = [r for r in rows if r["dispatch_id"] == f"claude-code:{SESSION}:{TUID_B}"]
    assert b_rows
    b = b_rows[-1]
    assert b["verdict"] == "done"
    assert b["dispatch_complete"] is True

    c_rows = [r for r in rows if r["dispatch_id"] == f"claude-code:{SESSION}:{TUID_C}"]
    assert c_rows
    assert c_rows[-1]["status"] == "async_launched"
    assert c_rows[-1]["dispatch_complete"] is False


# --- AC3 dedup: attachment notification alongside queue-operation/user
# duplicates in the MAIN log must not double-count / bump seq -------------

def test_attachment_dedups_against_queue_operation_and_user_duplicates():
    """A logical notification can arrive three ways for the same tool_use_id
    + status (attachment, queue-operation, user) -- Claude Code's own
    duplication plus the new attachment form this fix adds support for.
    They must collapse into exactly one dispatch, seq must not grow past 1.
    """
    xml = _notif_xml(CHILD_A_ID, TUID_A, "completed", RESULT_A)
    att_line = _attachment_notification("t3", CHILD_A_ID, TUID_A, result=RESULT_A, source_uuid="dup")
    qop_line = json.dumps({"type": "queue-operation", "operation": "enqueue", "timestamp": "t4",
                           "sessionId": SESSION, "content": xml})
    user_line = json.dumps({"type": "user", "timestamp": "t5", "message": {"role": "user", "content": xml}})

    raw = "\n".join([
        _agent_async_launch("t1", TUID_A, "implementer", CHILD_A_ID),
        att_line, qop_line, user_line,
    ])
    notify_seq: dict = {}
    seen: set = set()
    out = _extract_dispatches_from_text(SESSION, raw, notify_seq, seen, agent_index={})

    completed = [d for d in out["dispatches"] if d["status"] == "completed"]
    assert len(completed) == 1, completed
    assert completed[0]["dispatch_id"] == TUID_A
    assert completed[0]["seq"] == 1
    assert notify_seq[TUID_A] == 1


# --- AC1: attachment.usage backfills tokens/duration only when the XML
# itself has no <usage> block ------------------------------------------------

def test_attachment_usage_backfills_when_xml_lacks_usage_block():
    xml = ("<task-notification>\n<task-id>zz1</task-id>\n"
           "<tool-use-id>toolu_NoUsageInXml</tool-use-id>\n<status>completed</status>\n"
           "<summary>Background command finished</summary>\n</task-notification>")
    att = {"type": "queued_command", "prompt": xml, "commandMode": "task-notification",
          "usage": {"totalTokens": 777, "toolUses": 3, "durationMs": 4242}}
    raw = json.dumps({"type": "attachment", "attachment": att, "timestamp": "t1"})

    out = _extract_dispatches_from_text(SESSION, raw, {}, set(), agent_index={})
    d = out["dispatches"][0]
    assert d["total_tokens"] == 777
    assert d["tool_uses"] == 3
    assert d["duration_ms"] == 4242


def test_attachment_usage_does_not_override_xml_usage():
    xml = ("<task-notification>\n<task-id>zz2</task-id>\n"
           "<tool-use-id>toolu_XmlUsageWins</tool-use-id>\n<status>completed</status>\n"
           "<usage><subagent_tokens>111</subagent_tokens><tool_uses>1</tool_uses>"
           "<duration_ms>22</duration_ms></usage>\n</task-notification>")
    att = {"type": "queued_command", "prompt": xml, "commandMode": "task-notification",
          "usage": {"totalTokens": 999, "toolUses": 9, "durationMs": 9999}}
    raw = json.dumps({"type": "attachment", "attachment": att, "timestamp": "t1"})

    out = _extract_dispatches_from_text(SESSION, raw, {}, set(), agent_index={})
    d = out["dispatches"][0]
    assert d["total_tokens"] == 111
    assert d["tool_uses"] == 1
    assert d["duration_ms"] == 22
