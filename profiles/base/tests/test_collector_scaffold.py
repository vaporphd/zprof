#!/usr/bin/env python3
"""Tests for collector scaffold: payload, state, locking."""
import json, os, tempfile, pathlib, subprocess, sys
import pytest

COLLECTOR = pathlib.Path(__file__).parent.parent / "zprof-collect.py"

def run_collector(mode, payload, agentlog_dir, env_extra=None):
    """Run collector as subprocess, return (exit_code, collect_log_text)."""
    env = os.environ.copy()
    env["ZPROF_AGENTLOG"] = str(agentlog_dir)
    if env_extra:
        env.update(env_extra)
    p = subprocess.run(
        [sys.executable, str(COLLECTOR), mode],
        input=json.dumps(payload),
        capture_output=True, text=True, env=env, timeout=10,
    )
    log = (agentlog_dir / "collect.log").read_text() if (agentlog_dir / "collect.log").exists() else ""
    return p.returncode, log


class TestPayloadParsing:
    def test_stop_payload_parsed(self, tmp_path):
        payload = {
            "session_id": "abc-123",
            "transcript_path": "/fake/path/abc-123.jsonl",
            "cwd": "/some/project",
            "hook_event_name": "Stop",
            "stop_hook_active": False,
            "background_tasks": [],
        }
        rc, _ = run_collector("stop", payload, tmp_path)
        assert rc == 0

    def test_subagent_stop_payload_parsed(self, tmp_path):
        payload = {
            "session_id": "abc-123",
            "transcript_path": "/fake/path/abc-123.jsonl",
            "cwd": "/some/project",
            "hook_event_name": "SubagentStop",
            "agent_id": "a54a62add7dc3a394",
            "agent_type": "implementer",
            "agent_transcript_path": "/fake/path/abc-123/subagents/agent-a54a62add7dc3a394.jsonl",
            "background_tasks": [],
        }
        rc, _ = run_collector("subagent-stop", payload, tmp_path)
        assert rc == 0
        state = json.loads((tmp_path / "state.json").read_text())
        assert "a54a62add7dc3a394" in json.dumps(state)

    def test_invalid_json_exits_zero(self, tmp_path):
        """Collector must never crash — exit 0 even on garbage."""
        p = subprocess.run(
            [sys.executable, str(COLLECTOR), "stop"],
            input="not json",
            capture_output=True, text=True,
            env={**os.environ, "ZPROF_AGENTLOG": str(tmp_path)},
            timeout=10,
        )
        assert p.returncode == 0
        assert (tmp_path / "collect.log").exists()


class TestState:
    def test_state_created_on_first_run(self, tmp_path):
        payload = {
            "session_id": "abc-123",
            "transcript_path": "/fake/path/abc-123.jsonl",
            "cwd": "/some/project",
            "hook_event_name": "Stop",
            "background_tasks": [],
        }
        run_collector("stop", payload, tmp_path)
        state_path = tmp_path / "state.json"
        assert state_path.exists()
        state = json.loads(state_path.read_text())
        assert "sessions" in state

    def test_state_survives_rewrite(self, tmp_path):
        """State written via temp+rename is atomic."""
        payload = {
            "session_id": "sess-1",
            "transcript_path": "/fake/sess-1.jsonl",
            "cwd": "/p",
            "hook_event_name": "Stop",
            "background_tasks": [],
        }
        run_collector("stop", payload, tmp_path)
        payload["session_id"] = "sess-2"
        payload["transcript_path"] = "/fake/sess-2.jsonl"
        run_collector("stop", payload, tmp_path)
        state = json.loads((tmp_path / "state.json").read_text())
        assert "sess-1" in state["sessions"]
        assert "sess-2" in state["sessions"]


class TestLocking:
    def test_lock_file_created(self, tmp_path):
        payload = {
            "session_id": "abc-123",
            "transcript_path": "/fake/abc-123.jsonl",
            "cwd": "/p",
            "hook_event_name": "Stop",
            "background_tasks": [],
        }
        run_collector("stop", payload, tmp_path)
        assert (tmp_path / ".lock").exists()


class TestNoEscape:
    def test_writes_only_inside_agentlog(self, tmp_path):
        """Collector must not create files outside .agentlog/."""
        agentlog = tmp_path / "project" / ".agentlog"
        agentlog.mkdir(parents=True)
        payload = {
            "session_id": "abc-123",
            "transcript_path": "/fake/abc-123.jsonl",
            "cwd": str(tmp_path / "project"),
            "hook_event_name": "Stop",
            "background_tasks": [],
        }
        before = set()
        for root, dirs, files in os.walk(tmp_path):
            for f in files:
                before.add(os.path.join(root, f))

        run_collector("stop", payload, agentlog)

        after = set()
        for root, dirs, files in os.walk(tmp_path):
            for f in files:
                after.add(os.path.join(root, f))

        new_outside = {f for f in (after - before) if not f.startswith(str(agentlog))}
        assert new_outside == set(), f"files created outside .agentlog/: {new_outside}"
