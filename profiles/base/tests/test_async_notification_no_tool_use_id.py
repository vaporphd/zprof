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


def _notification_lines(task_id=AID, status="completed"):
    xml = (f"<task-notification>\n<task-id>{task_id}</task-id>\n"
           f"<output-file>/tmp/tasks/{task_id}.output</output-file>\n"
           f"<status>{status}</status>\n<summary>Agent \"run\" finished</summary>\n"
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

UNKNOWN_TASK_ID = "a0unknown00000000"


def test_unresolvable_notification_records_loss(tmp_path):
    """#34 AC1 + #36 P2-1: one logical notification (queue-operation + user
    duplicate pair, ~15ms apart) is one loss, not two. Claude Code writes
    every notification twice; the dedup for unresolved notifications must
    happen before the loss counter is incremented.
    """
    agentlog = _split_stops(tmp_path, with_meta=False, notif_task_id=UNKNOWN_TASK_ID)
    rows = [r for r in _rows(agentlog) if r["dispatch_id"].endswith(":" + TUID)]
    assert [r["status"] for r in rows] == ["async_launched"]
    state = json.loads((agentlog / "state.json").read_text())
    assert state.get("losses", 0) == 1, "#36 P2-1: duplicate notification pair must count as one loss"


# --- #36 P2-1: two distinct unknown task_ids are independent losses --------

def test_two_different_unresolvable_task_ids_count_separately(tmp_path):
    """Dedup must be keyed by task_id (not a blanket skip) — two genuinely
    different unresolved notifications still count as two losses."""
    proj = tmp_path / "proj"
    proj.mkdir()
    agentlog = proj / ".agentlog"
    logs = tmp_path / "logs"
    logs.mkdir()
    main = logs / f"{SESSION}.jsonl"
    main.write_text("\n".join(_launch_lines()) + "\n")
    _stop(main, agentlog, proj)
    lines = _notification_lines("a0unknown00000001") + _notification_lines("a0unknown00000002")
    with open(main, "a") as f:
        f.write("\n".join(lines) + "\n")
    _stop(main, agentlog, proj)
    state = json.loads((agentlog / "state.json").read_text())
    assert state.get("losses", 0) == 2, "#36: two distinct unresolved task_ids must not be deduped together"


# --- #36 P2-1 in-process: repro straight from bug-hunter's report ----------

def test_extract_dispatches_dedups_unresolved_notification_pair():
    """In-process equivalent of the end-to-end split-Stop test above: the
    (queue-operation, user) duplicate pair for the same unknown task_id must
    only bump `unresolved_notifications` once."""
    raw = "\n".join(_notification_lines(UNKNOWN_TASK_ID))
    out = zprof_collect._extract_dispatches_from_text(SESSION, raw, {}, set(), agent_index={})
    assert out["unresolved_notifications"] == 1
    assert out["unresolved_task_ids"] == [UNKNOWN_TASK_ID]


# --- #36 P2-1: dedup key includes status, so a status transition is NOT
# collapsed into the same loss as its predecessor ---------------------------

def test_same_task_id_different_status_counts_separately():
    """The dedup key is `("task:" + task_id, status)`, not just task_id.
    A task_id that shows up unresolved under two different statuses (e.g.
    the notification fired once mid-flight and again on completion) is two
    distinct real events, not a duplicate pair, and must count as two
    losses -- guards against over-eager dedup collapsing by task_id alone.
    """
    raw = "\n".join(_notification_lines(UNKNOWN_TASK_ID, status="running")
                     + _notification_lines(UNKNOWN_TASK_ID, status="completed"))
    out = zprof_collect._extract_dispatches_from_text(SESSION, raw, {}, set(), agent_index={})
    assert out["unresolved_notifications"] == 2
    assert out["unresolved_task_ids"] == [UNKNOWN_TASK_ID]


# --- #36 P2-2: unresolved task_id must be traceable in collect.log ---------

def test_unresolvable_notification_logs_task_id(tmp_path):
    agentlog = _split_stops(tmp_path, with_meta=False, notif_task_id=UNKNOWN_TASK_ID)
    log = agentlog / "collect.log"
    assert log.exists(), "#36 P2-2: unresolved notification must be logged to collect.log"
    assert UNKNOWN_TASK_ID in log.read_text()


# --- AC1 priority: source (a) meta.json wins over source (b) launch map ----

def test_meta_index_priority_over_launch_map():
    """A caller-seeded (meta.json) entry must not be clobbered by the async
    launch found later in the same chunk (root-cause doc: "(a) takes
    priority by being applied on top of (b)"). Regression guard for the
    `if agent_id and agent_id not in agent_index` seed-priority check in
    the async branch of _extract_dispatches_from_text.
    """
    seeded_tuid = "toolu_01MetaSeeded"
    agent_index = {AID: {"tool_use_id": seeded_tuid, "role": "task-runner"}}
    raw = "\n".join(_launch_lines() + _notification_lines())
    out = zprof_collect._extract_dispatches_from_text(SESSION, raw, {}, set(),
                                                       agent_index=agent_index)
    dispatches = out["dispatches"]

    launch = next(d for d in dispatches if d["status"] == "async_launched")
    assert launch["dispatch_id"] == TUID, "the launch's own dispatch_id is always its tool_use_id"

    notif = next(d for d in dispatches if d.get("status") == "completed")
    assert notif["dispatch_id"] == seeded_tuid, (
        "#34: source (b), the in-chunk launch, must not overwrite the "
        "meta.json-seeded (a) entry already in agent_index")
    assert notif["role"] == "task-runner"
    # The pre-seeded entry itself must be left untouched (not repointed at TUID).
    assert agent_index[AID]["tool_use_id"] == seeded_tuid
