#!/usr/bin/env python3
"""zprof telemetry collector — runs as Claude Code hook.

Usage: zprof-collect.py <mode>
Modes: subagent-stop | stop | session-start

Reads JSON payload from stdin. Writes to $ZPROF_AGENTLOG or <cwd>/.agentlog/.
Always exits 0. Errors go to collect.log.
"""
import fcntl, json, os, sys, time, traceback
from pathlib import Path
from datetime import datetime, timezone

VERSION = "0.1.0"


def main():
    mode = sys.argv[1] if len(sys.argv) > 1 else "stop"
    raw = ""
    try:
        raw = sys.stdin.read()
        payload = json.loads(raw)
    except Exception:
        agentlog = Path(os.environ.get("ZPROF_AGENTLOG", ".agentlog"))
        agentlog.mkdir(parents=True, exist_ok=True)
        _log_error(agentlog, f"payload parse failed: {raw[:200]}")
        sys.exit(0)

    cwd = payload.get("cwd", os.getcwd())
    agentlog = Path(os.environ.get("ZPROF_AGENTLOG", os.path.join(cwd, ".agentlog")))
    agentlog.mkdir(parents=True, exist_ok=True)

    try:
        with AgentlogLock(agentlog):
            collector = Collector(agentlog, payload)
            collector.run(mode)
    except Exception:
        _log_error(agentlog, traceback.format_exc())

    sys.exit(0)


class AgentlogLock:
    """flock-based mutual exclusion for .agentlog/."""

    def __init__(self, agentlog: Path, timeout: float = 5.0):
        self.path = agentlog / ".lock"
        self.timeout = timeout
        self._fd = None

    def __enter__(self):
        self.path.touch(exist_ok=True)
        self._fd = open(self.path, "w")
        try:
            fcntl.flock(self._fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            return self
        except BlockingIOError:
            pass

        deadline = time.monotonic() + self.timeout
        while time.monotonic() < deadline:
            try:
                fcntl.flock(self._fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
                return self
            except BlockingIOError:
                time.sleep(0.05)

        # Never acquired the lock — close the fd before giving up so we
        # don't leak it (a bare context-manager __enter__ raising means
        # __exit__ is never called).
        self._fd.close()
        self._fd = None
        raise TimeoutError(f"could not acquire lock within {self.timeout}s")

    def __exit__(self, *exc):
        if self._fd:
            fcntl.flock(self._fd, fcntl.LOCK_UN)
            self._fd.close()


class State:
    """Watermarks and pointers for incremental collection."""

    def __init__(self, agentlog: Path):
        self.path = agentlog / "state.json"
        self.data = {"sessions": {}, "pointers": {}, "losses": 0}
        if self.path.exists():
            try:
                self.data = json.loads(self.path.read_text())
            except Exception:
                pass

    def session(self, session_id: str) -> dict:
        if session_id not in self.data["sessions"]:
            self.data["sessions"][session_id] = {
                "main_log_offset": 0,
                "main_log_size": 0,
                "main_log_head_sha": "",
                "agents_done": [],
            }
        return self.data["sessions"][session_id]

    def add_pointer(self, agent_id: str, info: dict):
        self.data.setdefault("pointers", {})[agent_id] = info

    def increment_losses(self, n: int = 1):
        self.data["losses"] = self.data.get("losses", 0) + n

    def save(self, agentlog: Path):
        """Atomic write via temp+rename."""
        tmp = agentlog / "state.json.tmp"
        with open(tmp, "w") as f:
            f.write(json.dumps(self.data, indent=2, ensure_ascii=False))
            f.flush()
            os.fsync(f.fileno())
        tmp.rename(self.path)


class Collector:
    """Main collection logic."""

    def __init__(self, agentlog: Path, payload: dict):
        self.agentlog = agentlog
        self.payload = payload
        self.state = State(agentlog)

    def run(self, mode: str):
        if mode == "subagent-stop":
            self._handle_subagent_stop()
        elif mode == "stop":
            self._handle_stop()
        elif mode == "session-start":
            self._handle_session_start()
        else:
            _log_error(self.agentlog, f"unknown mode: {mode}")
        self.state.save(self.agentlog)

    def _handle_subagent_stop(self):
        agent_id = self.payload.get("agent_id", "")
        if not agent_id:
            return
        self.state.add_pointer(agent_id, {
            "agent_transcript_path": self.payload.get("agent_transcript_path", ""),
            "agent_type": self.payload.get("agent_type", ""),
            "ts": datetime.now(timezone.utc).isoformat(),
        })

    def _handle_stop(self):
        session_id = self.payload.get("session_id", "")
        transcript_path = self.payload.get("transcript_path", "")
        if not session_id or not transcript_path:
            _log_error(self.agentlog, "stop: missing session_id or transcript_path")
            self.state.increment_losses()
            return
        running = {t["id"] for t in self.payload.get("background_tasks", [])
                   if t.get("status") == "running"}
        self._collect_session(session_id, transcript_path, running)

    def _handle_session_start(self):
        transcript_path = self.payload.get("transcript_path", "")
        if not transcript_path:
            return
        slug_dir = Path(transcript_path).parent
        if not slug_dir.is_dir():
            return
        for entry in slug_dir.iterdir():
            if entry.suffix == ".jsonl" and entry.is_file():
                sid = entry.stem
                sess_state = self.state.session(sid)
                if sess_state["main_log_offset"] > 0:
                    continue  # already collected
                if sid == self.payload.get("session_id"):
                    continue  # current session, will be collected on Stop
                self._collect_session(sid, str(entry), set())

    def _collect_session(self, session_id: str, transcript_path: str, running_agents: set):
        """Collect new data from a session. Stub — implemented in Task 3."""
        # Register the session watermark first (even if the transcript turns
        # out to be missing) so state.json always reflects every session
        # we've been asked to collect, and later Stop/SessionStart calls for
        # the same session_id land on the same entry instead of losing it.
        sess = self.state.session(session_id)
        tp = Path(transcript_path)
        if not tp.exists():
            _log_error(self.agentlog, f"transcript not found: {transcript_path}")
            self.state.increment_losses()
            return
        # Task 3 fills this in: read main log from offset
        # Task 4 fills this in: copy subagent transcripts
        # Task 5 fills this in: normalize to dispatches.jsonl


def _log_error(agentlog: Path, msg: str):
    log = agentlog / "collect.log"
    ts = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    with open(log, "a") as f:
        f.write(f"{ts} ERROR {msg}\n")
        f.flush()
        os.fsync(f.fileno())


if __name__ == "__main__":
    main()
