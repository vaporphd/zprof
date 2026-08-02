#!/usr/bin/env python3
"""zprof telemetry collector — runs as Claude Code hook.

Usage: zprof-collect.py <mode>
Modes: subagent-stop | stop | session-start

Reads JSON payload from stdin. Writes to $ZPROF_AGENTLOG or <cwd>/.agentlog/.
Always exits 0. Errors go to collect.log.
"""
import fcntl, hashlib, json, os, re, sys, time, traceback
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
        """Collect new data from a session's main JSONL log."""
        sess = self.state.session(session_id)
        tp = Path(transcript_path)
        if not tp.exists():
            _log_error(self.agentlog, f"transcript not found: {transcript_path}")
            self.state.increment_losses()
            return
        dispatches, meta = _extract_main_log(session_id, tp, sess)
        # Store dispatches as raw JSONL for later normalization (Task 5)
        if dispatches:
            raw_path = self.agentlog / "raw" / f"{session_id}.jsonl"
            raw_path.parent.mkdir(parents=True, exist_ok=True)
            with open(raw_path, "a") as f:
                for d in dispatches:
                    f.write(json.dumps(d, ensure_ascii=False) + "\n")
                f.flush()
                os.fsync(f.fileno())
        # Update watermarks
        sess["main_log_offset"] = meta["offset"]
        sess["main_log_size"] = meta["size"]
        sess["main_log_head_sha"] = meta["head_sha"]
        if meta.get("harness_version"):
            sess["harness_version"] = meta["harness_version"]
        if meta.get("unparsed_lines", 0) > 0:
            _log_error(self.agentlog,
                       f"session {session_id}: {meta['unparsed_lines']} unparsed lines (format drift?)")
        if meta.get("truncated"):
            sess["transcript_truncated"] = True
        # Task 4 fills this in: copy subagent transcripts
        # Task 5 fills this in: normalize to dispatches.jsonl


# ---------------------------------------------------------------------------
# Main log extraction (Task 3)
# ---------------------------------------------------------------------------

_HEAD_BYTES = 4096  # bytes to hash for file-identity check


def _sha256_head(path: Path, nbytes: int = _HEAD_BYTES) -> str:
    """SHA-256 of the first `nbytes` bytes of a file.

    When the file is smaller than `nbytes`, hashes all available bytes.
    The caller must store the file size alongside the hash so verification
    reads the same count.
    """
    h = hashlib.sha256()
    with open(path, "rb") as f:
        h.update(f.read(nbytes))
    return h.hexdigest()


def _check_offset(path: Path, sess: dict) -> int:
    """Return the byte offset to start reading from.

    If the file head hash and size are consistent with the stored state,
    resume from the stored offset.  Otherwise re-read from zero (safe
    because dispatch_id dedup happens downstream).
    """
    stored_offset = sess.get("main_log_offset", 0)
    stored_size = sess.get("main_log_size", 0)
    stored_sha = sess.get("main_log_head_sha", "")
    if stored_offset == 0 or stored_size == 0 or not stored_sha:
        return 0
    current_size = path.stat().st_size
    if current_size < stored_size:
        # File shrank — compaction or replacement, re-read
        return 0
    # Hash the same number of head bytes as when the hash was stored
    head_cap = min(_HEAD_BYTES, stored_size)
    current_sha = _sha256_head(path, head_cap)
    if current_sha != stored_sha:
        return 0
    return stored_offset


def _parse_task_notification_xml(text: str) -> dict | None:
    """Extract fields from <task-notification> XML using regex.

    Returns dict with task_id, tool_use_id, status, and optional
    subagent_tokens, tool_uses, duration_ms fields.  Returns None if
    the XML is not found or cannot be parsed.
    """
    m = re.search(r"<task-notification>(.*?)</task-notification>", text, re.DOTALL)
    if not m:
        return None
    body = m.group(1)
    result = {}
    for tag in ("task-id", "tool-use-id", "status", "summary",
                "subagent_tokens", "tool_uses", "duration_ms"):
        tm = re.search(rf"<{re.escape(tag)}>(.*?)</{re.escape(tag)}>", body, re.DOTALL)
        if tm:
            key = tag.replace("-", "_")
            val = tm.group(1).strip()
            # Convert numeric fields
            if tag in ("subagent_tokens", "tool_uses", "duration_ms"):
                try:
                    val = int(val)
                except ValueError:
                    continue
            result[key] = val
    return result if "tool_use_id" in result else None


def _extract_tool_uses_from_assistant(content: list) -> list[dict]:
    """Extract Agent/Task tool_use blocks from assistant message content."""
    results = []
    if not isinstance(content, list):
        return results
    for item in content:
        if not isinstance(item, dict):
            continue
        if item.get("type") == "tool_use" and item.get("name") in ("Agent", "Task"):
            inp = item.get("input", {})
            results.append({
                "tool_use_id": item.get("id", ""),
                "name": item.get("name", ""),
                "description": inp.get("description", ""),
                "subagent_type": inp.get("subagent_type", ""),
                "run_in_background": inp.get("run_in_background", False),
                "prompt": inp.get("prompt", ""),
            })
    return results


def _extract_main_log(session_id: str, path: Path, sess: dict) -> tuple[list[dict], dict]:
    """Read a session JSONL and extract dispatch records.

    Returns (dispatches, meta) where dispatches is a list of raw dicts
    and meta contains offset/size/hash/harness_version/unparsed_lines.
    """
    meta = {
        "offset": sess.get("main_log_offset", 0),
        "size": sess.get("main_log_size", 0),
        "head_sha": sess.get("main_log_head_sha", ""),
        "harness_version": sess.get("harness_version", ""),
        "unparsed_lines": 0,
        "truncated": False,
    }

    if not path.exists():
        return [], meta

    start_offset = _check_offset(path, sess)
    file_size = path.stat().st_size

    if start_offset >= file_size:
        # No new data
        return [], meta

    # Read new bytes (use binary seek for exact offset, then decode)
    try:
        with open(path, "rb") as f:
            f.seek(start_offset)
            raw = f.read().decode("utf-8", errors="replace")
    except OSError:
        return [], meta

    # Track pending tool_use dispatches from assistant messages
    # key = tool_use_id, value = info from the assistant's tool_use block
    pending_dispatches: dict[str, dict] = {}
    # Track notification sequence per dispatch_id for multi-notify
    notify_seq: dict[str, int] = {}
    dispatches: list[dict] = []
    harness_version = meta["harness_version"]
    truncated = False

    lines = raw.split("\n")
    for line in lines:
        line = line.strip()
        if not line:
            continue

        try:
            record = json.loads(line)
        except json.JSONDecodeError:
            # Could be a truncated last line
            truncated = True
            continue

        # Track harness version from any message that carries it
        v = record.get("version")
        if v and isinstance(v, str):
            harness_version = v

        rec_type = record.get("type", "")
        msg = record.get("message", {})
        if not isinstance(msg, dict):
            msg = {}
        role = msg.get("role", "")
        content = msg.get("content", [])

        # --- Path 1 & 2: assistant message with Agent/Task tool_use ---
        if role == "assistant" and isinstance(content, list):
            tool_uses = _extract_tool_uses_from_assistant(content)
            for tu in tool_uses:
                tid = tu["tool_use_id"]
                if tid:
                    pending_dispatches[tid] = tu

        # --- Path 1 & 2: user message with toolUseResult ---
        if "toolUseResult" in record and role == "user":
            tur = record["toolUseResult"]
            if not isinstance(tur, dict):
                continue

            # Find the matching tool_use_id from the tool_result in content
            tool_use_id = ""
            if isinstance(content, list):
                for item in content:
                    if isinstance(item, dict) and item.get("type") == "tool_result":
                        tool_use_id = item.get("tool_use_id", "")
                        if tool_use_id:
                            break

            if not tool_use_id:
                continue

            # Only process if this tool_use was Agent/Task (tracked in pending)
            # or if the toolUseResult has agent-specific fields
            is_agent_result = (
                tool_use_id in pending_dispatches
                or tur.get("agentType")
                or tur.get("agentId")
                or tur.get("isAsync")
                or tur.get("resolvedModel")
                or tur.get("totalTokens") is not None
            )
            if not is_agent_result:
                continue

            is_async = tur.get("isAsync", False)
            status = tur.get("status", "")

            # Get info from the pending assistant tool_use
            pending = pending_dispatches.pop(tool_use_id, {})

            dispatch = {
                "dispatch_id": tool_use_id,
                "session_id": session_id,
                "role": tur.get("agentType") or pending.get("subagent_type", ""),
                "model_resolved": tur.get("resolvedModel", ""),
                "status": status,
                "dispatch_complete": (status == "completed"),
                "ts_utc": record.get("timestamp", ""),
                "seq": 0,
            }

            if is_async:
                dispatch["dispatch_complete"] = False
                dispatch["agent_id"] = tur.get("agentId", "")
                dispatch["description"] = tur.get("description", pending.get("description", ""))
            else:
                # Sync completion — extract cost/duration from toolUseResult
                dispatch["tool_uses"] = tur.get("totalToolUseCount")
                dispatch["duration_ms"] = tur.get("totalDurationMs")
                dispatch["total_tokens"] = tur.get("totalTokens")
                # Extract detailed token breakdown if available
                usage = tur.get("usage", {})
                if isinstance(usage, dict):
                    dispatch["tokens_input"] = usage.get("input_tokens")
                    dispatch["tokens_output"] = usage.get("output_tokens")
                    dispatch["tokens_cache_read"] = usage.get("cache_read_input_tokens")
                    dispatch["tokens_cache_creation"] = usage.get("cache_creation_input_tokens")

            dispatches.append(dispatch)
            continue

        # --- Path 2 & 3: task-notification in user message or queue-operation ---
        notification_text = None
        if rec_type == "queue-operation":
            c = record.get("content", "")
            if isinstance(c, str) and "<task-notification>" in c:
                notification_text = c
        elif role == "user" and isinstance(content, str) and "<task-notification>" in content:
            notification_text = content
        elif role == "user" and isinstance(content, list):
            for item in content:
                if isinstance(item, dict):
                    ic = item.get("content", "")
                    if isinstance(ic, str) and "<task-notification>" in ic:
                        notification_text = ic
                        break

        if notification_text:
            notif = _parse_task_notification_xml(notification_text)
            if notif:
                tool_use_id = notif["tool_use_id"]
                # Increment seq for repeated notifications on same dispatch
                notify_seq[tool_use_id] = notify_seq.get(tool_use_id, 0) + 1
                seq = notify_seq[tool_use_id]

                dispatch = {
                    "dispatch_id": tool_use_id,
                    "session_id": session_id,
                    "status": notif.get("status", "completed"),
                    "dispatch_complete": (notif.get("status", "") == "completed"),
                    "ts_utc": record.get("timestamp", ""),
                    "seq": seq,
                    "agent_id": notif.get("task_id", ""),
                }
                # Legacy fields from older task-notification format
                if "subagent_tokens" in notif:
                    dispatch["total_tokens"] = notif["subagent_tokens"]
                if "tool_uses" in notif:
                    dispatch["tool_uses"] = notif["tool_uses"]
                if "duration_ms" in notif:
                    dispatch["duration_ms"] = notif["duration_ms"]

                dispatches.append(dispatch)
            else:
                meta["unparsed_lines"] += 1

    # Update meta
    meta["offset"] = file_size
    meta["size"] = file_size
    meta["head_sha"] = _sha256_head(path, min(_HEAD_BYTES, file_size))
    meta["harness_version"] = harness_version
    meta["truncated"] = truncated

    return dispatches, meta


def _log_error(agentlog: Path, msg: str):
    log = agentlog / "collect.log"
    ts = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    with open(log, "a") as f:
        f.write(f"{ts} ERROR {msg}\n")
        f.flush()
        os.fsync(f.fileno())


if __name__ == "__main__":
    main()
