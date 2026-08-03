#!/usr/bin/env python3
"""End-to-end integration tests for the telemetry collector pipeline.

Exercises the full pipeline via subprocess: fixture setup -> collector invocation
-> output verification.  Each test builds a realistic mock Claude Code session
directory structure, invokes the collector, and verifies .agentlog/ contents.

Test 1: Full pipeline  -- SubagentStop -> Stop -> verify
Test 2: SessionStart recovery -- previous session data collected on start
Test 3: Secret redaction in pipeline -- AWS key redacted, count logged
"""
import gzip, json, os, pathlib, subprocess, sys
import pytest

COLLECTOR = pathlib.Path(__file__).parent.parent / "zprof-collect.py"

# Required fields from telemetry.yaml with required: true
REQUIRED_FIELDS = [
    "schema_version", "harness", "harness_version", "machine_id",
    "project_id", "ts_utc", "session_id", "dispatch_id", "seq",
    "spawn_depth", "role", "dispatch_complete", "transcript_captured",
]


def run_collector(mode, payload, agentlog_dir, timeout=30):
    """Run collector as subprocess, return (exit_code, stdout, stderr, log_text)."""
    env = os.environ.copy()
    env["ZPROF_AGENTLOG"] = str(agentlog_dir)
    p = subprocess.run(
        [sys.executable, str(COLLECTOR), mode],
        input=json.dumps(payload),
        capture_output=True, text=True, env=env, timeout=timeout,
    )
    log_path = agentlog_dir / "collect.log"
    log_text = log_path.read_text() if log_path.exists() else ""
    return p.returncode, p.stdout, p.stderr, log_text


def read_dispatches(agentlog_dir):
    """Read all rows from dispatches.jsonl."""
    dispatches_file = agentlog_dir / "dispatches.jsonl"
    if not dispatches_file.exists():
        return []
    rows = []
    for line in dispatches_file.read_text().splitlines():
        line = line.strip()
        if line:
            rows.append(json.loads(line))
    return rows


def _make_main_log_lines(session_id):
    """Build a main JSONL log with 3 dispatches:
      1. Sync completed (Explore)
      2. Async (launch + notification)
      3. Sync with verdict text
    """
    lines = []

    # Summary line
    lines.append(json.dumps({
        "type": "summary", "sessionId": session_id,
        "timestamp": "2026-08-02T10:00:00.000Z", "version": "2.1.220",
    }))

    # --- Dispatch 1: sync completed (Explore) ---
    lines.append(json.dumps({
        "type": "assistant",
        "message": {
            "role": "assistant", "model": "claude-sonnet-5",
            "content": [
                {"type": "text", "text": "Let me explore the codebase."},
                {"type": "tool_use", "id": "toolu_01E2ESyncExplore00001",
                 "name": "Agent",
                 "input": {"description": "Explore code",
                           "subagent_type": "Explore",
                           "run_in_background": False,
                           "prompt": "Explore the project structure."}},
            ],
        },
        "uuid": "uuid-sync-1-a", "timestamp": "2026-08-02T10:00:01.000Z",
        "sessionId": session_id, "version": "2.1.220",
    }))
    lines.append(json.dumps({
        "type": "user",
        "message": {
            "role": "user",
            "content": [{"type": "tool_result",
                         "tool_use_id": "toolu_01E2ESyncExplore00001",
                         "content": "Found 42 files in the project."}],
        },
        "uuid": "uuid-sync-1-b", "timestamp": "2026-08-02T10:00:30.000Z",
        "toolUseResult": {
            "status": "completed",
            "agentType": "Explore",
            "resolvedModel": "claude-sonnet-5",
            "totalDurationMs": 28000,
            "totalTokens": 45000,
            "totalToolUseCount": 12,
            "usage": {"input_tokens": 30000, "output_tokens": 8000,
                      "cache_read_input_tokens": 5000,
                      "cache_creation_input_tokens": 2000},
        },
        "sessionId": session_id, "version": "2.1.220",
    }))

    # --- Dispatch 2: async launch + notification ---
    lines.append(json.dumps({
        "type": "assistant",
        "message": {
            "role": "assistant", "model": "claude-sonnet-5",
            "content": [
                {"type": "text", "text": "I will launch a background task."},
                {"type": "tool_use", "id": "toolu_01E2EAsyncImpl000001",
                 "name": "Agent",
                 "input": {"description": "Implement feature X",
                           "subagent_type": "implementer",
                           "run_in_background": True,
                           "prompt": "Implement feature X end-to-end."}},
            ],
        },
        "uuid": "uuid-async-2-a", "timestamp": "2026-08-02T10:01:00.000Z",
        "sessionId": session_id, "version": "2.1.220",
    }))
    lines.append(json.dumps({
        "type": "user",
        "message": {
            "role": "user",
            "content": [{"type": "tool_result",
                         "tool_use_id": "toolu_01E2EAsyncImpl000001",
                         "content": "Agent launched in background."}],
        },
        "uuid": "uuid-async-2-b", "timestamp": "2026-08-02T10:01:01.000Z",
        "toolUseResult": {
            "isAsync": True,
            "status": "async_launched",
            "agentId": "e2eagent_async_impl1",
            "description": "Implement feature X",
            "resolvedModel": "claude-sonnet-5",
            "prompt": "Implement feature X end-to-end.",
        },
        "sessionId": session_id, "version": "2.1.220",
    }))
    # Notification (queue-operation + user -- duplicate pair)
    notif_xml = (
        "<task-notification>\n"
        "<task-id>e2eagent_async_impl1</task-id>\n"
        "<tool-use-id>toolu_01E2EAsyncImpl000001</tool-use-id>\n"
        "<output-file>/tmp/tasks/e2eagent_async_impl1.output</output-file>\n"
        "<status>completed</status>\n"
        "<summary>Agent finished feature X</summary>\n"
        "<result>verdict: done\nartifact: commit e2eabc123</result>\n"
        "</task-notification>"
    )
    lines.append(json.dumps({
        "type": "queue-operation", "operation": "enqueue",
        "timestamp": "2026-08-02T10:05:00.000Z",
        "sessionId": session_id, "content": notif_xml,
    }))
    lines.append(json.dumps({
        "type": "user",
        "message": {"role": "user", "content": notif_xml},
        "uuid": "uuid-async-2-notif", "timestamp": "2026-08-02T10:05:01.000Z",
        "sessionId": session_id, "version": "2.1.220",
    }))

    # --- Dispatch 3: sync with verdict text ---
    lines.append(json.dumps({
        "type": "assistant",
        "message": {
            "role": "assistant", "model": "claude-sonnet-5",
            "content": [
                {"type": "text", "text": "Implementing the auth feature."},
                {"type": "tool_use", "id": "toolu_01E2ESyncVerdict0001",
                 "name": "Agent",
                 "input": {"description": "Implement auth",
                           "subagent_type": "implementer",
                           "run_in_background": False,
                           "prompt": "Implement the auth feature."}},
            ],
        },
        "uuid": "uuid-sync-3-a", "timestamp": "2026-08-02T10:06:00.000Z",
        "sessionId": session_id, "version": "2.1.220",
    }))
    lines.append(json.dumps({
        "type": "user",
        "message": {
            "role": "user",
            "content": [{"type": "tool_result",
                         "tool_use_id": "toolu_01E2ESyncVerdict0001",
                         "content": [{"type": "text",
                                      "text": "verdict: done\nartifact: commit auth789\nnext: reviewer"}]}],
        },
        "uuid": "uuid-sync-3-b", "timestamp": "2026-08-02T10:06:30.000Z",
        "toolUseResult": {
            "status": "completed",
            "agentType": "implementer",
            "resolvedModel": "claude-sonnet-5",
            "totalDurationMs": 15000,
            "totalTokens": 22000,
            "totalToolUseCount": 5,
            "usage": {"input_tokens": 15000, "output_tokens": 4000,
                      "cache_read_input_tokens": 2000,
                      "cache_creation_input_tokens": 1000},
        },
        "sessionId": session_id, "version": "2.1.220",
    }))

    return "\n".join(lines) + "\n"


def _make_subagent_transcript(model="claude-sonnet-5", turns=2,
                              input_tokens=5000, output_tokens=1500,
                              cache_read=40000, cache_creation=2000,
                              last_text="verdict: done\nartifact: commit abc123"):
    """Generate a realistic subagent transcript JSONL."""
    lines = [
        json.dumps({"type": "summary", "sessionId": "agent-sub",
                    "timestamp": "2026-08-02T10:01:30Z", "version": "2.1.220"}),
    ]
    for i in range(turns):
        text = f"Working on turn {i+1}." if i < turns - 1 else last_text
        lines.append(json.dumps({
            "type": "assistant",
            "message": {
                "role": "assistant", "model": model,
                "content": [{"type": "text", "text": text}],
                "usage": {
                    "input_tokens": input_tokens,
                    "output_tokens": output_tokens,
                    "cache_read_input_tokens": cache_read,
                    "cache_creation_input_tokens": cache_creation,
                },
            },
            "uuid": f"sub-uuid-{i}",
            "timestamp": f"2026-08-02T10:01:3{i}.000Z",
            "sessionId": "agent-sub", "version": "2.1.220",
        }))
        if i < turns - 1:
            lines.append(json.dumps({
                "type": "user",
                "message": {"role": "user", "content": "Continue."},
                "timestamp": f"2026-08-02T10:01:3{i}.500Z",
            }))
    return "\n".join(lines) + "\n"


def _setup_full_session(tmp_path, session_id="e2e-session-001"):
    """Create a complete mock Claude Code session structure.

    Layout:
      tmp_path/slug/<session_id>.jsonl          -- main log
      tmp_path/slug/<session_id>/subagents/     -- 2 subagent transcripts
      tmp_path/slug/<session_id>/tool-results/  -- 1 tool result file
      tmp_path/.agentlog/                       -- output directory

    Returns (slug_dir, agentlog_dir, session_id).
    """
    slug_dir = tmp_path / "slug"
    slug_dir.mkdir(parents=True)

    # Main JSONL log
    main_log = slug_dir / f"{session_id}.jsonl"
    main_log.write_text(_make_main_log_lines(session_id))

    # Session data directory (alongside main log, without .jsonl extension)
    session_dir = slug_dir / session_id
    subagents_dir = session_dir / "subagents"
    subagents_dir.mkdir(parents=True)

    # --- Subagent 1: matches async dispatch, spawnDepth=1 ---
    agent1_id = "e2eagent_async_impl1"
    meta1 = {
        "agentType": "implementer",
        "toolUseId": "toolu_01E2EAsyncImpl000001",
        "spawnDepth": 1,
    }
    (subagents_dir / f"agent-{agent1_id}.meta.json").write_text(json.dumps(meta1))
    (subagents_dir / f"agent-{agent1_id}.jsonl").write_text(
        _make_subagent_transcript(
            model="claude-sonnet-5", turns=3,
            input_tokens=8000, output_tokens=2000,
            cache_read=50000, cache_creation=3000,
            last_text="verdict: done\nartifact: commit e2eabc123",
        ))

    # --- Subagent 2: child of agent1, spawnDepth=2, parentAgentId ---
    agent2_id = "e2eagent_child_review1"
    meta2 = {
        "agentType": "code-reviewer",
        "toolUseId": "toolu_01E2EChildReview00001",
        "parentAgentId": agent1_id,
        "spawnDepth": 2,
    }
    (subagents_dir / f"agent-{agent2_id}.meta.json").write_text(json.dumps(meta2))
    (subagents_dir / f"agent-{agent2_id}.jsonl").write_text(
        _make_subagent_transcript(
            model="claude-opus-5", turns=2,
            input_tokens=12000, output_tokens=3000,
            cache_read=80000, cache_creation=5000,
            last_text="verdict: approve\nnext: implementer",
        ))

    # --- Tool results ---
    tool_results_dir = session_dir / "tool-results"
    tool_results_dir.mkdir(parents=True)
    (tool_results_dir / "result-e2e-001.txt").write_text(
        "Build output: 0 errors, 0 warnings\nTests: 42 passed")

    # agentlog output directory
    agentlog_dir = tmp_path / ".agentlog"
    agentlog_dir.mkdir(parents=True)

    return slug_dir, agentlog_dir, session_id


# ---------------------------------------------------------------------------
# Test 1: Full pipeline -- SubagentStop -> Stop -> verify
# ---------------------------------------------------------------------------

class TestFullPipeline:
    """SubagentStop -> Stop -> verify .agentlog/ contents."""

    def test_full_pipeline(self, tmp_path):
        slug_dir, agentlog_dir, session_id = _setup_full_session(tmp_path)
        transcript_path = str(slug_dir / f"{session_id}.jsonl")

        # Step 1: SubagentStop for async agent
        subagent_payload = {
            "session_id": session_id,
            "transcript_path": transcript_path,
            "cwd": str(tmp_path),
            "hook_event_name": "SubagentStop",
            "agent_id": "e2eagent_async_impl1",
            "agent_type": "implementer",
            "agent_transcript_path": str(
                slug_dir / session_id / "subagents" / "agent-e2eagent_async_impl1.jsonl"),
            "background_tasks": [],
        }
        rc, _, _, _ = run_collector("subagent-stop", subagent_payload, agentlog_dir)
        assert rc == 0

        # Verify pointer saved in state
        state = json.loads((agentlog_dir / "state.json").read_text())
        assert "e2eagent_async_impl1" in state.get("pointers", {})

        # Step 2: Stop -- one agent still running (should be skipped)
        running_agent_id = "e2eagent_still_running"
        # Create a meta for the running agent so it would be found
        subagents_dir = slug_dir / session_id / "subagents"
        running_meta = {
            "agentType": "debugger",
            "toolUseId": "toolu_01E2ERunningAgent001",
            "spawnDepth": 1,
        }
        (subagents_dir / f"agent-{running_agent_id}.meta.json").write_text(
            json.dumps(running_meta))
        (subagents_dir / f"agent-{running_agent_id}.jsonl").write_text(
            _make_subagent_transcript(turns=1))

        stop_payload = {
            "session_id": session_id,
            "transcript_path": transcript_path,
            "cwd": str(tmp_path),
            "hook_event_name": "Stop",
            "stop_hook_active": False,
            "background_tasks": [
                {"id": running_agent_id, "status": "running",
                 "description": "Still working"},
            ],
        }
        rc, _, _, log_text = run_collector("stop", stop_payload, agentlog_dir)
        assert rc == 0

        # --- Verify dispatches.jsonl ---
        rows = read_dispatches(agentlog_dir)
        assert len(rows) >= 3, f"expected at least 3 dispatches, got {len(rows)}"

        # Verify required schema fields on every row
        for row in rows:
            for field in REQUIRED_FIELDS:
                assert field in row, (
                    f"row missing required field '{field}': "
                    f"dispatch_id={row.get('dispatch_id')}")
            assert row["schema_version"] == 1
            assert row["harness"] == "claude-code"

        # Verify composite dispatch_id format
        for row in rows:
            did = row["dispatch_id"]
            assert did.startswith("claude-code:") or did.startswith("meta:"), (
                f"dispatch_id should be composite, got: {did}")

        # Check specific dispatches exist
        dispatch_ids = [r["dispatch_id"] for r in rows]
        assert any("toolu_01E2ESyncExplore00001" in d for d in dispatch_ids), \
            "sync Explore dispatch not found"
        assert any("toolu_01E2EAsyncImpl000001" in d for d in dispatch_ids), \
            "async impl dispatch not found"
        assert any("toolu_01E2ESyncVerdict0001" in d for d in dispatch_ids), \
            "sync verdict dispatch not found"

        # Token fields populated for agents with transcripts
        async_rows = [r for r in rows
                      if "toolu_01E2EAsyncImpl000001" in r.get("dispatch_id", "")
                      and r.get("transcript_captured")]
        assert len(async_rows) >= 1, "async dispatch should have transcript data"
        for ar in async_rows:
            # 3 turns * 8000 = 24000
            assert ar.get("tokens_input") == 24000
            # 3 turns * 2000 = 6000
            assert ar.get("tokens_output") == 6000

        # parent_dispatch_id joinable (child reviewer -> parent impl)
        child_rows = [r for r in rows
                      if "toolu_01E2EChildReview00001" in r.get("dispatch_id", "")]
        if child_rows:
            cr = child_rows[0]
            pdid = cr.get("parent_dispatch_id", "")
            # parent_dispatch_id should reference the async impl's toolUseId
            assert "toolu_01E2EAsyncImpl000001" in pdid, \
                f"parent_dispatch_id should reference parent toolUseId, got: {pdid}"

        # transcript_captured: true for copied agents
        captured_agents = [r for r in rows if r.get("transcript_captured") is True]
        assert len(captured_agents) >= 2, "at least 2 agents should have transcripts captured"

        # Running agent NOT captured
        running_dispatch_ids = [r["dispatch_id"] for r in rows
                                if "toolu_01E2ERunningAgent001" in r.get("dispatch_id", "")]
        assert len(running_dispatch_ids) == 0, \
            "running agent should NOT have dispatch in output"

        # --- Verify transcripts/ dir ---
        transcripts_dir = agentlog_dir / "transcripts"
        assert transcripts_dir.is_dir()
        gz_files = list(transcripts_dir.glob("*.jsonl.gz"))
        assert len(gz_files) >= 2, f"expected >= 2 gzipped transcripts, got {len(gz_files)}"

        # Verify gzip files are valid
        for gz in gz_files:
            with gzip.open(gz, "rb") as f:
                content = f.read().decode("utf-8")
                assert "assistant" in content

        # Running agent should NOT have transcript
        running_gz = transcripts_dir / f"{running_agent_id}.jsonl.gz"
        assert not running_gz.exists(), "running agent should not have transcript copied"

        # --- Verify tool-results/ copied ---
        assert (agentlog_dir / "tool-results" / "result-e2e-001.txt").exists()
        assert "42 passed" in (agentlog_dir / "tool-results" / "result-e2e-001.txt").read_text()

        # --- Verify state.json watermarks ---
        state = json.loads((agentlog_dir / "state.json").read_text())
        sess_state = state["sessions"][session_id]
        assert sess_state["main_log_offset"] > 0
        assert sess_state["main_log_size"] > 0
        assert sess_state["main_log_head_sha"] != ""
        # agents_done should include captured agents but NOT running agent
        assert "e2eagent_async_impl1" in sess_state["agents_done"]
        assert "e2eagent_child_review1" in sess_state["agents_done"]
        assert running_agent_id not in sess_state["agents_done"]

        # --- Verify collect.log has no ERROR lines ---
        assert "ERROR" not in log_text, f"collect.log has errors: {log_text}"

        # --- Verify no files outside .agentlog/ ---
        before_agentlog = str(agentlog_dir)
        for root, dirs, files in os.walk(tmp_path):
            root_path = str(root)
            # Skip slug dir (that's our input) and .agentlog/ (that's output)
            if root_path.startswith(str(slug_dir)):
                continue
            if root_path.startswith(before_agentlog):
                continue
            for f in files:
                fpath = os.path.join(root, f)
                if not fpath.startswith(before_agentlog) and not fpath.startswith(str(slug_dir)):
                    pytest.fail(f"file created outside .agentlog/: {fpath}")


# ---------------------------------------------------------------------------
# Test 2: SessionStart recovery
# ---------------------------------------------------------------------------

class TestSessionStartRecovery:
    """session-start mode collects previous uncollected sessions."""

    def test_previous_session_collected_on_start(self, tmp_path):
        slug_dir = tmp_path / "slug"
        slug_dir.mkdir(parents=True)

        # First session: write main log but do NOT run stop
        session1_id = "e2e-old-session-001"
        main_log1 = slug_dir / f"{session1_id}.jsonl"
        main_log1.write_text(_make_main_log_lines(session1_id))

        # Create subagents dir for session 1
        session1_dir = slug_dir / session1_id
        subagents_dir1 = session1_dir / "subagents"
        subagents_dir1.mkdir(parents=True)

        agent1_id = "e2eagent_async_impl1"
        meta1 = {
            "agentType": "implementer",
            "toolUseId": "toolu_01E2EAsyncImpl000001",
            "spawnDepth": 1,
        }
        (subagents_dir1 / f"agent-{agent1_id}.meta.json").write_text(json.dumps(meta1))
        (subagents_dir1 / f"agent-{agent1_id}.jsonl").write_text(
            _make_subagent_transcript(turns=2))

        # Second session: create log file (will be current session)
        session2_id = "e2e-new-session-002"
        main_log2 = slug_dir / f"{session2_id}.jsonl"
        main_log2.write_text(json.dumps({
            "type": "summary", "sessionId": session2_id,
            "timestamp": "2026-08-02T12:00:00.000Z", "version": "2.1.220",
        }) + "\n")

        # agentlog output directory
        agentlog_dir = tmp_path / ".agentlog"
        agentlog_dir.mkdir(parents=True)

        # Run session-start for the new session
        start_payload = {
            "session_id": session2_id,
            "transcript_path": str(main_log2),
            "cwd": str(tmp_path),
            "hook_event_name": "SessionStart",
            "background_tasks": [],
        }
        rc, _, _, log_text = run_collector("session-start", start_payload, agentlog_dir)
        assert rc == 0

        # Verify old session's dispatches were collected
        rows = read_dispatches(agentlog_dir)
        assert len(rows) >= 1, "old session dispatches should be collected"

        # All rows should reference the old session
        old_session_rows = [r for r in rows if r["session_id"] == session1_id]
        assert len(old_session_rows) >= 1, (
            "expected dispatches from old session in dispatches.jsonl")

        # The new (current) session should NOT have been collected yet
        new_session_rows = [r for r in rows if r["session_id"] == session2_id]
        assert len(new_session_rows) == 0, (
            "current session should not be collected on session-start")

        # State should show old session was processed
        state = json.loads((agentlog_dir / "state.json").read_text())
        assert session1_id in state["sessions"]
        assert state["sessions"][session1_id]["main_log_offset"] > 0


# ---------------------------------------------------------------------------
# Test 3: Secret redaction in pipeline
# ---------------------------------------------------------------------------

class TestSecretRedactionPipeline:
    """Secrets in dispatch data are redacted in dispatches.jsonl.

    The pipeline has two layers of protection:
    1. `returned` text (tool_result content) is consumed for Class A boolean
       checks but intentionally NOT persisted in dispatches.jsonl.
    2. Any secret that reaches a persisted field (ext, role, model_resolved)
       is replaced by a redaction placeholder.

    This test verifies both layers.
    """

    def test_returned_text_secret_not_leaked(self, tmp_path):
        """Secret in returned text does not leak into dispatches.jsonl.

        Architecture note: `returned` is not a schema field -- it is only
        used for Class A checks (has_preamble, return_parsed, etc.) which
        produce booleans.  This is the primary security boundary.
        """
        slug_dir = tmp_path / "slug"
        slug_dir.mkdir(parents=True)

        session_id = "e2e-redact-001"

        # Build a main log where tool_result content contains an AWS key
        lines = [
            json.dumps({
                "type": "summary", "sessionId": session_id,
                "timestamp": "2026-08-02T10:00:00.000Z", "version": "2.1.220",
            }),
            json.dumps({
                "type": "assistant",
                "message": {
                    "role": "assistant", "model": "claude-sonnet-5",
                    "content": [
                        {"type": "text", "text": "Checking env vars."},
                        {"type": "tool_use", "id": "toolu_01E2ERedactTest0001",
                         "name": "Agent",
                         "input": {"description": "Check env",
                                   "subagent_type": "Explore",
                                   "run_in_background": False,
                                   "prompt": "Check environment variables."}},
                    ],
                },
                "uuid": "uuid-redact-a", "timestamp": "2026-08-02T10:00:01.000Z",
                "sessionId": session_id, "version": "2.1.220",
            }),
            json.dumps({
                "type": "user",
                "message": {
                    "role": "user",
                    "content": [{
                        "type": "tool_result",
                        "tool_use_id": "toolu_01E2ERedactTest0001",
                        "content": [{
                            "type": "text",
                            "text": "Found secret: AWS_SECRET_ACCESS_KEY=AKIAIOSFODNN7EXAMPLE in .env file",
                        }],
                    }],
                },
                "uuid": "uuid-redact-b", "timestamp": "2026-08-02T10:00:30.000Z",
                "toolUseResult": {
                    "status": "completed",
                    "agentType": "Explore",
                    "resolvedModel": "claude-sonnet-5",
                    "totalDurationMs": 5000,
                    "totalTokens": 10000,
                    "totalToolUseCount": 3,
                },
                "sessionId": session_id, "version": "2.1.220",
            }),
        ]

        main_log = slug_dir / f"{session_id}.jsonl"
        main_log.write_text("\n".join(lines) + "\n")

        agentlog_dir = tmp_path / ".agentlog"
        agentlog_dir.mkdir(parents=True)

        stop_payload = {
            "session_id": session_id,
            "transcript_path": str(main_log),
            "cwd": str(tmp_path),
            "hook_event_name": "Stop",
            "stop_hook_active": False,
            "background_tasks": [],
        }
        rc, _, _, log_text = run_collector("stop", stop_payload, agentlog_dir)
        assert rc == 0

        # dispatches.jsonl must NOT contain the raw secret
        dispatches_raw = (agentlog_dir / "dispatches.jsonl").read_text()
        assert "AKIAIOSFODNN7EXAMPLE" not in dispatches_raw, \
            "AWS secret key should not leak into dispatches.jsonl"

        # The raw file preserves original data for debugging
        raw_path = agentlog_dir / "raw" / f"{session_id}.jsonl"
        assert raw_path.exists(), "raw file should exist"
        raw_content = raw_path.read_text()
        assert "AKIAIOSFODNN7EXAMPLE" in raw_content, \
            "raw file should have the original returned text"

        # Normalized row is clean
        rows = read_dispatches(agentlog_dir)
        assert len(rows) == 1
        row_json = json.dumps(rows[0])
        assert "AKIAIOSFODNN7EXAMPLE" not in row_json

    def test_secret_in_persisted_field_is_redacted(self, tmp_path):
        """Secret that reaches a persisted schema field IS redacted.

        Uses the normalization module directly (via subprocess + crafted
        fixture) to verify _redact_secrets works on dispatches.jsonl output.
        We embed a GITHUB_TOKEN in the async dispatch's model_resolved field
        (artificial but exercises the full path).
        """
        slug_dir = tmp_path / "slug"
        slug_dir.mkdir(parents=True)

        session_id = "e2e-redact-002"

        # Build a main log where model_resolved contains a secret
        # This is artificial but tests the redaction pipeline end-to-end
        secret_model = "ghp_AAAAAAAAAA1234567890BBBBBBBBBBBBCCCCCC"
        lines = [
            json.dumps({
                "type": "summary", "sessionId": session_id,
                "timestamp": "2026-08-02T10:00:00.000Z", "version": "2.1.220",
            }),
            json.dumps({
                "type": "assistant",
                "message": {
                    "role": "assistant", "model": "claude-sonnet-5",
                    "content": [
                        {"type": "tool_use", "id": "toolu_01E2ERedactField001",
                         "name": "Agent",
                         "input": {"description": "test",
                                   "subagent_type": "Explore",
                                   "prompt": "test"}},
                    ],
                },
                "uuid": "uuid-rp-a", "timestamp": "2026-08-02T10:00:01.000Z",
                "sessionId": session_id, "version": "2.1.220",
            }),
            json.dumps({
                "type": "user",
                "message": {
                    "role": "user",
                    "content": [{"type": "tool_result",
                                 "tool_use_id": "toolu_01E2ERedactField001",
                                 "content": "Done."}],
                },
                "uuid": "uuid-rp-b", "timestamp": "2026-08-02T10:00:30.000Z",
                "toolUseResult": {
                    "status": "completed",
                    "agentType": "Explore",
                    "resolvedModel": secret_model,
                    "totalDurationMs": 1000,
                    "totalTokens": 5000,
                    "totalToolUseCount": 1,
                },
                "sessionId": session_id, "version": "2.1.220",
            }),
        ]

        main_log = slug_dir / f"{session_id}.jsonl"
        main_log.write_text("\n".join(lines) + "\n")

        agentlog_dir = tmp_path / ".agentlog"
        agentlog_dir.mkdir(parents=True)

        stop_payload = {
            "session_id": session_id,
            "transcript_path": str(main_log),
            "cwd": str(tmp_path),
            "hook_event_name": "Stop",
            "stop_hook_active": False,
            "background_tasks": [],
        }
        rc, _, _, log_text = run_collector("stop", stop_payload, agentlog_dir)
        assert rc == 0

        # dispatches.jsonl must contain the redaction placeholder
        dispatches_raw = (agentlog_dir / "dispatches.jsonl").read_text()
        assert secret_model not in dispatches_raw, \
            "GitHub token should be redacted from dispatches.jsonl"
        assert "redacted:" in dispatches_raw, \
            "redaction placeholder should be present"

        # collect.log should mention redaction count
        assert "redact" in log_text.lower(), \
            f"collect.log should mention redaction, got: {log_text}"

        # Parse the row and verify the model_resolved field
        rows = read_dispatches(agentlog_dir)
        assert len(rows) == 1
        assert secret_model not in json.dumps(rows[0])


# ---------------------------------------------------------------------------
# Test: collector always exits 0
# ---------------------------------------------------------------------------

class TestCollectorExitsZero:
    """Collector must always exit 0, even with missing/invalid data."""

    def test_exits_zero_on_missing_transcript(self, tmp_path):
        agentlog = tmp_path / ".agentlog"
        agentlog.mkdir()
        payload = {
            "session_id": "nonexistent",
            "transcript_path": "/nonexistent/path.jsonl",
            "cwd": str(tmp_path),
            "hook_event_name": "Stop",
            "background_tasks": [],
        }
        rc, _, _, _ = run_collector("stop", payload, agentlog)
        assert rc == 0

    def test_exits_zero_on_empty_payload(self, tmp_path):
        agentlog = tmp_path / ".agentlog"
        agentlog.mkdir()
        payload = {}
        rc, _, _, _ = run_collector("stop", payload, agentlog)
        assert rc == 0


# ---------------------------------------------------------------------------
# Test: idempotent re-run
# ---------------------------------------------------------------------------

class TestIdempotentRerun:
    """Running stop twice on the same session should not duplicate rows."""

    def test_no_duplicate_rows_on_rerun(self, tmp_path):
        slug_dir, agentlog_dir, session_id = _setup_full_session(tmp_path)
        transcript_path = str(slug_dir / f"{session_id}.jsonl")

        stop_payload = {
            "session_id": session_id,
            "transcript_path": transcript_path,
            "cwd": str(tmp_path),
            "hook_event_name": "Stop",
            "stop_hook_active": False,
            "background_tasks": [],
        }

        # First run
        rc1, _, _, _ = run_collector("stop", stop_payload, agentlog_dir)
        assert rc1 == 0
        rows1 = read_dispatches(agentlog_dir)
        count1 = len(rows1)
        assert count1 >= 3

        # Second run (same session, same data)
        rc2, _, _, _ = run_collector("stop", stop_payload, agentlog_dir)
        assert rc2 == 0
        rows2 = read_dispatches(agentlog_dir)
        count2 = len(rows2)

        assert count2 == count1, (
            f"re-run should not add duplicates: first={count1}, second={count2}")


# ---------------------------------------------------------------------------
# Test: no files outside .agentlog/
# ---------------------------------------------------------------------------

class TestNoEscapeE2E:
    """E2E variant: collector must not write outside .agentlog/."""

    def test_writes_only_inside_agentlog(self, tmp_path):
        slug_dir, agentlog_dir, session_id = _setup_full_session(tmp_path)
        transcript_path = str(slug_dir / f"{session_id}.jsonl")

        # Snapshot all files before
        before = set()
        for root, dirs, files in os.walk(tmp_path):
            for f in files:
                before.add(os.path.join(root, f))

        stop_payload = {
            "session_id": session_id,
            "transcript_path": transcript_path,
            "cwd": str(tmp_path),
            "hook_event_name": "Stop",
            "stop_hook_active": False,
            "background_tasks": [],
        }
        run_collector("stop", stop_payload, agentlog_dir)

        # Snapshot after
        after = set()
        for root, dirs, files in os.walk(tmp_path):
            for f in files:
                after.add(os.path.join(root, f))

        new_outside = {
            f for f in (after - before)
            if not f.startswith(str(agentlog_dir))
        }
        assert new_outside == set(), f"files created outside .agentlog/: {new_outside}"
