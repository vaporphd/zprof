"""Repro for #34: async Agent dispatch + <task-notification> WITHOUT <tool-use-id>.

Current Claude Code emits some notifications (all task-runner ones in session
2026-09-27) with only <task-id>/<output-file>/<status>/<summary>/<note>/<result>.
`_parse_task_notification_xml` drops them (`return result if "tool_use_id" in
result else None`), so the dispatch stays `async_launched` forever and
`zprof score` sees no complete task-runner root.

Resolution sources (AC1): (a) subagents/agent-<task_id>.meta.json -> toolUseId,
(b) agentId -> tool_use_id from the async launch tool_result in the same transcript.
"""
import importlib
import json
import os
import pathlib
import subprocess
import sys

HERE = pathlib.Path(__file__).parent
COLLECTOR = HERE.parent / "zprof-collect.py"
_spec = importlib.util.spec_from_file_location("zprof_collect", COLLECTOR)
zprof_collect = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(zprof_collect)

SESSION = "sess-async-notool"
AID = "a0runner000000001"          # agentId == <task-id>
TUID = "toolu_01AsyncRunnerNoTuid"  # tool_use_id of the Agent call
NOTIF_TS = "2026-09-27T15:29:00.000Z"
RESULT = ("verdict: done\nartifact: PR #1\nrun_log: .zprof/runs/2026-09-27-x.md\n"
          "next: none\none_line: ok")


def _launch_lines(aid=AID, tuid=TUID):
    call = {"type": "assistant", "timestamp": "2026-09-27T14:57:12.000Z",
            "message": {"role": "assistant", "model": "claude-opus-5",
                        "content": [{"type": "tool_use", "id": tuid, "name": "Agent",
                                     "input": {"description": "run", "subagent_type": "task-runner",
                                               "run_in_background": True, "prompt": "p"}}]}}
    result = {"type": "user", "timestamp": "2026-09-27T14:57:12.716Z",
              "message": {"role": "user", "content": [{
                  "type": "tool_result", "tool_use_id": tuid,
                  "content": [{"type": "text",
                               "text": f"Async agent launched successfully.\nagentId: {aid} (internal ID)"}]}]},
              "toolUseResult": {"isAsync": True, "status": "async_launched", "agentId": aid,
                                "description": "run", "resolvedModel": "claude-sonnet-5",
                                "outputFile": f"/tmp/tasks/{aid}.output", "prompt": "p"}}
    return [json.dumps(call), json.dumps(result)]


def _notification_lines(task_id=AID):
    xml = (f"<task-notification>\n<task-id>{task_id}</task-id>\n"
           f"<output-file>/tmp/tasks/{task_id}.output</output-file>\n"
           "<status>completed</status>\n<summary>Agent \"run\" finished</summary>\n"
           "<note>A task-notification fires each time this agent stops.</note>\n"
           f"<result>{RESULT}</result>\n</task-notification>")
    q = {"type": "queue-operation", "operation": "enqueue", "timestamp": NOTIF_TS,
         "sessionId": SESSION, "content": xml}
    u = {"type": "user", "timestamp": "2026-09-27T15:29:00.015Z",
         "message": {"role": "user", "content": xml}}
    return [json.dumps(q), json.dumps(u)]


def _write_meta(root: pathlib.Path, aid=AID, tuid=TUID):
    sub = root / SESSION / "subagents"
    sub.mkdir(parents=True, exist_ok=True)
    (sub / f"agent-{aid}.meta.json").write_text(json.dumps(
        {"agentType": "task-runner", "toolUseId": tuid, "spawnDepth": 1, "description": "run"}))
    (sub / f"agent-{aid}.jsonl").write_text(json.dumps(
        {"type": "assistant", "timestamp": "2026-09-27T15:00:00Z",
         "message": {"role": "assistant", "model": "claude-sonnet-5",
                     "content": [{"type": "text", "text": RESULT}],
                     "usage": {"input_tokens": 10, "output_tokens": 5}}}) + "\n")


def _stop(main: pathlib.Path, agentlog: pathlib.Path, cwd: pathlib.Path):
    env = os.environ.copy()
    env["ZPROF_AGENTLOG"] = str(agentlog)
    payload = {"session_id": SESSION, "transcript_path": str(main), "cwd": str(cwd),
               "hook_event_name": "Stop", "stop_hook_active": False, "background_tasks": []}
    p = subprocess.run([sys.executable, str(COLLECTOR), "stop"], input=json.dumps(payload),
                       capture_output=True, text=True, env=env, timeout=30, check=False, cwd=str(cwd))
    assert p.returncode == 0, p.stderr


def _rows(agentlog):
    f = agentlog / "dispatches.jsonl"
    return [json.loads(l) for l in f.read_text().splitlines() if l.strip()] if f.exists() else []


def _raw(agentlog):
    f = agentlog / "raw" / f"{SESSION}.jsonl"
    return [json.loads(l) for l in f.read_text().splitlines() if l.strip()] if f.exists() else []


def _split_stops(tmp_path, with_meta: bool, notif_task_id=AID):
    """Real-world timing: Stop #1 sees only the launch, Stop #2 only the notification."""
    proj = tmp_path / "proj"
    proj.mkdir()
    agentlog = proj / ".agentlog"
    logs = tmp_path / "logs"
    logs.mkdir()
    main = logs / f"{SESSION}.jsonl"
    main.write_text("\n".join(_launch_lines()) + "\n")
    if with_meta:
        _write_meta(logs)
    _stop(main, agentlog, proj)
    with open(main, "a") as f:
        f.write("\n".join(_notification_lines(notif_task_id)) + "\n")
    _stop(main, agentlog, proj)
    return agentlog


# --- AC1: parser keeps notifications that only carry <task-id> -------------

def test_parse_keeps_notification_without_tool_use_id():
    notif = zprof_collect._parse_task_notification_xml(json.loads(_notification_lines()[0])["content"])
    assert notif is not None, "#34: notification without <tool-use-id> dropped"
    assert notif["task_id"] == AID
    assert notif["result"].startswith("verdict: done")


# --- AC1(a) + AC2 + AC3: meta.json resolution, in-process Stop path ---------

def test_meta_json_resolves_dispatch_in_process(tmp_path):
    main = tmp_path / f"{SESSION}.jsonl"
    main.write_text("\n".join(_launch_lines() + _notification_lines()) + "\n")
    _write_meta(tmp_path)
    sess = {"main_log_offset": 0, "main_log_size": 0, "main_log_head_sha": "", "agents_done": []}
    dispatches, _ = zprof_collect._extract_main_log(SESSION, main, sess)
    agentlog = tmp_path / ".agentlog"
    agentlog.mkdir()
    zprof_collect._collect_subagent_transcripts(agentlog, SESSION, str(main), set(), sess, dispatches,
                                                require_completion_evidence=True)
    done = [d for d in dispatches if d.get("dispatch_id") == TUID and d.get("dispatch_complete")]
    assert done, f"#34: no completed dispatch for {TUID}: {[(d.get('dispatch_id'), d.get('status')) for d in dispatches]}"
    d = done[-1]
    assert d["status"] == "completed"
    assert d["agent_id"] == AID
    assert d["ts_utc"] == NOTIF_TS
    assert d["returned"].startswith("verdict: done")
    assert AID in sess["agents_done"]            # AC3: counts as completion evidence


# --- AC1(a) + AC5 shape: split Stops, row must be a `zprof score` root -------

def test_meta_json_split_stops_yields_scoreable_root(tmp_path):
    agentlog = _split_stops(tmp_path, with_meta=True)
    rows = [r for r in _rows(agentlog) if r["dispatch_id"].endswith(":" + TUID)]
    assert rows, "launch row missing"
    last = rows[-1]  # cli/internal/score/reader.go BuildRuns: later rows win
    assert last.get("dispatch_complete") is True, f"#34: still {last.get('status')}"
    assert last.get("status") == "completed"
    assert last.get("verdict") == "done"
    assert last.get("role") == "task-runner"      # BuildRuns root predicate
    assert any(r.get("agent_id") == AID and r.get("dispatch_complete") for r in _raw(agentlog))


# --- AC1(b): no meta.json, fallback via launch tool_result agentId ---------

def test_fallback_launch_map_split_stops(tmp_path):
    agentlog = _split_stops(tmp_path, with_meta=False)
    rows = [r for r in _rows(agentlog) if r["dispatch_id"].endswith(":" + TUID)]
    done = [r for r in rows if r.get("dispatch_complete")]
    assert done, f"#34: fallback (b) did not resolve; statuses={[r.get('status') for r in rows]}"
    assert done[-1]["status"] == "completed" and done[-1].get("verdict") == "done"
    assert any(r.get("agent_id") == AID and r.get("dispatch_complete") for r in _raw(agentlog))


# --- AC1 negative: nothing resolves -> stays async_launched, loss recorded --

def test_unresolvable_notification_records_loss(tmp_path):
    agentlog = _split_stops(tmp_path, with_meta=False, notif_task_id="a0unknown00000000")
    rows = [r for r in _rows(agentlog) if r["dispatch_id"].endswith(":" + TUID)]
    assert [r["status"] for r in rows] == ["async_launched"]
    state = json.loads((agentlog / "state.json").read_text())
    assert state.get("losses", 0) >= 1, "#34 AC1: unresolved notification must be counted as loss"
