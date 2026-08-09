#!/usr/bin/env python3
"""zprof telemetry collector — runs as Claude Code hook.

Usage: zprof-collect.py <mode>
Modes: subagent-stop | stop | session-start | pick-arm

Reads JSON payload from stdin. Writes to $ZPROF_AGENTLOG or <cwd>/.agentlog/.
Always exits 0. Errors go to collect.log.
"""
import fcntl, gzip, hashlib, json, os, platform, re, shutil, subprocess, sys, time, traceback
from pathlib import Path
from datetime import datetime, timezone

VERSION = "0.1.0"


def main():
    mode = sys.argv[1] if len(sys.argv) > 1 else "stop"

    if mode == "pick-arm":
        _handle_pick_arm()
        return

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


def _handle_pick_arm():
    """Deterministic A/B arm selection.

    Reads JSON from stdin: {"role": "implementer", "task": "fix the bug"}
    Reads .zprof.yaml for ab_experiments config.
    Returns JSON to stdout: {"model": "sonnet", "arm": "control"} or {"model": "opus", "arm": "candidate"}

    Deterministic: hash(project_id + role + task) mod 2.
    Same task on same project always gets the same arm.
    """
    try:
        raw = sys.stdin.read()
        req = json.loads(raw)
    except Exception:
        print(json.dumps({"error": "invalid input", "model": None, "arm": None}))
        sys.exit(0)

    role = req.get("role", "")
    task = req.get("task", "")
    cwd = req.get("cwd", os.getcwd())

    project_id = _get_project_id(cwd)
    config = _load_ab_config(cwd)

    if not config or role not in config:
        print(json.dumps({"model": None, "arm": None, "reason": "no experiment for this role"}))
        sys.exit(0)

    experiment = config[role]
    control = experiment.get("control", "")
    candidate = experiment.get("candidate", "")

    if not control or not candidate:
        print(json.dumps({"model": None, "arm": None, "reason": "incomplete experiment config"}))
        sys.exit(0)

    key = f"{project_id}:{role}:{task}"
    h = hashlib.sha256(key.encode()).hexdigest()
    arm_index = int(h[:8], 16) % 2

    if arm_index == 0:
        print(json.dumps({"model": control, "arm": "control"}))
    else:
        print(json.dumps({"model": candidate, "arm": "candidate"}))
    sys.exit(0)


def _load_ab_config(cwd):
    """Load ab_experiments from .zprof.yaml."""
    zprof_yaml = Path(cwd) / ".zprof.yaml"
    if not zprof_yaml.exists():
        return None
    try:
        text = zprof_yaml.read_text()
        in_ab = False
        current_role = None
        config = {}
        for line in text.splitlines():
            stripped = line.strip()
            if stripped == "ab_experiments:":
                in_ab = True
                continue
            if in_ab:
                if not line.startswith(" ") and not line.startswith("\t") and stripped:
                    break
                if stripped.endswith(":") and not stripped.startswith("-") and not stripped.startswith("control") and not stripped.startswith("candidate"):
                    current_role = stripped[:-1].strip()
                    config[current_role] = {}
                elif current_role and stripped.startswith("control:"):
                    config[current_role]["control"] = stripped.split(":", 1)[1].strip()
                elif current_role and stripped.startswith("candidate:"):
                    config[current_role]["candidate"] = stripped.split(":", 1)[1].strip()
        return config if config else None
    except Exception:
        return None


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
        # Update watermarks
        sess["main_log_offset"] = meta["offset"]
        sess["main_log_size"] = meta["size"]
        sess["main_log_head_sha"] = meta["head_sha"]
        if meta.get("harness_version"):
            sess["harness_version"] = meta["harness_version"]
        if meta.get("notify_seq"):
            sess["notify_seq"] = meta["notify_seq"]
        if meta.get("unparsed_lines", 0) > 0:
            _log_error(self.agentlog,
                       f"session {session_id}: {meta['unparsed_lines']} unparsed lines (format drift?)")
        if meta.get("truncated"):
            sess["transcript_truncated"] = True
        # Task 4: copy subagent transcripts and enrich dispatch dicts
        _collect_subagent_transcripts(
            self.agentlog, session_id, transcript_path,
            running_agents, sess, dispatches,
        )
        # Store dispatches as raw JSONL for later normalization (Task 5).
        # Written AFTER transcript enrichment so the raw file has the
        # most complete version of each dispatch dict.
        if dispatches:
            raw_path = self.agentlog / "raw" / f"{session_id}.jsonl"
            raw_path.parent.mkdir(parents=True, exist_ok=True)
            with open(raw_path, "a") as f:
                for d in dispatches:
                    f.write(json.dumps(d, ensure_ascii=False) + "\n")
                f.flush()
                os.fsync(f.fileno())
        # Task 5: normalize and write to dispatches.jsonl
        _normalize_and_write(
            self.agentlog, dispatches,
            session_id=session_id,
            harness_version=meta.get("harness_version", sess.get("harness_version", "")),
            payload=self.payload,
        )


# ---------------------------------------------------------------------------
# Main log extraction (Task 3)
# ---------------------------------------------------------------------------

_HEAD_BYTES = 4096  # bytes to hash for file-identity check

# Statuses that mean the dispatch is still in flight.
# Everything else (completed, failed, killed, stopped, ...) is terminal.
_IN_FLIGHT_STATUSES = frozenset({"async_launched", "running", "pending", "in_progress"})


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
    for tag in ("task-id", "tool-use-id", "status", "summary", "result",
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
    # Track notification sequence per dispatch_id for multi-notify.
    # Persisted across hook invocations so a SendMessage resume in
    # invocation 2 gets seq=2, not seq=1.
    notify_seq: dict[str, int] = dict(sess.get("notify_seq", {}))
    # Dedup set: (dispatch_id, status) pairs already emitted in THIS pass.
    # Claude Code writes every task-notification twice (~15ms apart) —
    # once as queue-operation, once as user message.  Without dedup
    # every async dispatch produces seq=1 AND seq=2.
    seen_notifications: set[tuple[str, str]] = set()
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

            # Find the matching tool_use_id and returned text from the
            # tool_result in content.  The item's "content" field holds the
            # subagent's return text (first text block only — trailing blocks
            # are harness bookkeeping per parser.go:300).
            tool_use_id = ""
            returned_text = ""
            if isinstance(content, list):
                for item in content:
                    if isinstance(item, dict) and item.get("type") == "tool_result":
                        tool_use_id = item.get("tool_use_id", "")
                        # Extract returned text from tool_result content
                        tr_content = item.get("content", "")
                        if isinstance(tr_content, str) and tr_content:
                            returned_text = tr_content
                        elif isinstance(tr_content, list) and tr_content:
                            first = tr_content[0]
                            if isinstance(first, dict) and first.get("type") == "text":
                                returned_text = first.get("text", "")
                            elif isinstance(first, str):
                                returned_text = first
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
                "dispatch_complete": status not in _IN_FLIGHT_STATUSES,
                "ts_utc": record.get("timestamp", ""),
                "seq": 0,
            }

            # Attach returned text for Class A checks (sync path)
            if returned_text:
                dispatch["returned"] = returned_text

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
                notif_status = notif.get("status", "completed")

                # Dedup: Claude Code writes every notification twice
                # (~15ms apart) — once as queue-operation, once as user
                # message.  Skip the duplicate.
                dedup_key = (tool_use_id, notif_status)
                if dedup_key in seen_notifications:
                    continue
                seen_notifications.add(dedup_key)

                # Increment seq for genuinely new notifications
                notify_seq[tool_use_id] = notify_seq.get(tool_use_id, 0) + 1
                seq = notify_seq[tool_use_id]

                dispatch = {
                    "dispatch_id": tool_use_id,
                    "session_id": session_id,
                    "status": notif_status,
                    "dispatch_complete": notif_status not in _IN_FLIGHT_STATUSES,
                    "ts_utc": record.get("timestamp", ""),
                    "seq": seq,
                    "agent_id": notif.get("task_id", ""),
                }
                # Attach returned text from <result> for Class A checks
                if "result" in notif:
                    dispatch["returned"] = notif["result"]
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
    meta["notify_seq"] = notify_seq

    return dispatches, meta


# ---------------------------------------------------------------------------
# Subagent transcript extraction (Task 4)
# ---------------------------------------------------------------------------


def _extract_subagent_transcript(jsonl_path: Path) -> dict:
    """Read a subagent transcript and sum token usage across assistant turns.

    Returns dict with keys: tokens_input, tokens_output, tokens_cache_read,
    tokens_cache_creation, model (from last assistant turn), truncated (bool),
    returned (text content of last assistant message — the subagent's return).
    """
    result = {
        "tokens_input": 0,
        "tokens_output": 0,
        "tokens_cache_read": 0,
        "tokens_cache_creation": 0,
        "model": "",
        "truncated": False,
        "returned": "",
    }
    if not jsonl_path.exists():
        return result

    try:
        raw = jsonl_path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return result

    # Check if the last non-empty line is valid JSON (truncation detection)
    last_line = ""
    for line in reversed(raw.split("\n")):
        line = line.strip()
        if line:
            last_line = line
            break
    if last_line:
        try:
            json.loads(last_line)
        except json.JSONDecodeError:
            result["truncated"] = True

    for line in raw.split("\n"):
        line = line.strip()
        if not line:
            continue
        try:
            record = json.loads(line)
        except json.JSONDecodeError:
            continue

        msg = record.get("message", {})
        if not isinstance(msg, dict):
            continue
        if msg.get("role") != "assistant":
            continue

        # Extract model — keep the last one seen
        m = msg.get("model", "")
        if m:
            result["model"] = m

        # Extract text content — keep the last assistant message's text
        # (this is the subagent's return text for Class A checks)
        msg_content = msg.get("content", [])
        if isinstance(msg_content, str) and msg_content:
            result["returned"] = msg_content
        elif isinstance(msg_content, list):
            for ci in msg_content:
                if isinstance(ci, dict) and ci.get("type") == "text":
                    text = ci.get("text", "")
                    if text:
                        result["returned"] = text
                        break  # first text block only

        # Sum usage
        usage = msg.get("usage", {})
        if not isinstance(usage, dict):
            continue
        result["tokens_input"] += usage.get("input_tokens", 0)
        result["tokens_output"] += usage.get("output_tokens", 0)
        result["tokens_cache_read"] += usage.get("cache_read_input_tokens", 0)
        result["tokens_cache_creation"] += usage.get("cache_creation_input_tokens", 0)

    return result


def _gzip_copy(src: Path, dst: Path):
    """Copy src file to dst, gzip-compressing the content."""
    dst.parent.mkdir(parents=True, exist_ok=True)
    with open(src, "rb") as f_in, gzip.open(dst, "wb") as f_out:
        shutil.copyfileobj(f_in, f_out)


def _collect_subagent_transcripts(
    agentlog: Path,
    session_id: str,
    transcript_path: str,
    running_agents: set,
    sess: dict,
    dispatches: list[dict],
):
    """Copy subagent transcripts and enrich dispatch dicts with transcript data.

    Finds the subagents/ directory next to the main transcript, reads
    meta.json files to correlate with dispatch_id (toolUseId), extracts
    token breakdown and model from the transcript JSONL, gzip-copies the
    transcript to .agentlog/transcripts/, and copies new tool-results.
    """
    tp = Path(transcript_path)
    # subagents dir: transcript path without .jsonl extension + /subagents/
    subagents_dir = tp.with_suffix("") / "subagents"
    agents_done = set(sess.get("agents_done", []))

    # Build lookup: dispatch_id -> dispatch dict index for enrichment
    dispatch_by_id: dict[str, list[int]] = {}
    for i, d in enumerate(dispatches):
        did = d.get("dispatch_id", "")
        if did:
            dispatch_by_id.setdefault(did, []).append(i)

    if subagents_dir.is_dir():
        for meta_file in sorted(subagents_dir.glob("agent-*.meta.json")):
            # agent-<agentId>.meta.json → agentId
            agent_id = meta_file.name[len("agent-"):-len(".meta.json")]
            if not agent_id:
                continue
            if agent_id in agents_done:
                continue
            if agent_id in running_agents:
                continue

            # Read meta.json
            try:
                meta = json.loads(meta_file.read_text())
            except (OSError, json.JSONDecodeError):
                _log_error(agentlog, f"failed to read meta.json for agent {agent_id}")
                continue

            tool_use_id = meta.get("toolUseId", "")
            agent_type = meta.get("agentType", "")
            parent_agent_id = meta.get("parentAgentId", "")
            spawn_depth = meta.get("spawnDepth")

            # Resolve parent_dispatch_id: parentAgentId is an agent_id (hex),
            # not a toolUseId.  Read the parent's meta.json to get its
            # toolUseId so parent_dispatch_id is joinable with dispatch_id.
            parent_dispatch_id = ""
            if parent_agent_id:
                parent_meta_file = subagents_dir / f"agent-{parent_agent_id}.meta.json"
                try:
                    parent_meta = json.loads(parent_meta_file.read_text())
                    parent_dispatch_id = parent_meta.get("toolUseId", "")
                except (OSError, json.JSONDecodeError):
                    parent_dispatch_id = f"unresolved:{parent_agent_id}"

            # Read the corresponding transcript JSONL
            transcript_file = subagents_dir / f"agent-{agent_id}.jsonl"
            transcript_data = _extract_subagent_transcript(transcript_file)

            # Gzip-copy transcript to .agentlog/transcripts/
            transcript_ref = ""
            if transcript_file.exists():
                gz_name = f"{agent_id}.jsonl.gz"
                gz_path = agentlog / "transcripts" / gz_name
                try:
                    _gzip_copy(transcript_file, gz_path)
                    transcript_ref = f"transcripts/{gz_name}"
                except OSError:
                    _log_error(agentlog, f"failed to gzip-copy transcript for agent {agent_id}")

            # Enrich matching dispatch dicts — only set fields when
            # the value is meaningful to avoid blanking correct data
            # from Task 3's main-log extraction.
            enrichment = {
                "agent_id": agent_id,
                "transcript_ref": transcript_ref,
                "transcript_captured": bool(transcript_ref),
            }
            if agent_type:
                enrichment["role"] = agent_type
            if spawn_depth is not None and spawn_depth > 0:
                enrichment["spawn_depth"] = spawn_depth
            if parent_dispatch_id:
                enrichment["parent_dispatch_id"] = parent_dispatch_id
            if transcript_data["truncated"]:
                enrichment["transcript_truncated"] = True

            # Token data from transcript (more accurate than main log)
            if transcript_data["tokens_input"] or transcript_data["tokens_output"]:
                enrichment["tokens_input"] = transcript_data["tokens_input"]
                enrichment["tokens_output"] = transcript_data["tokens_output"]
                enrichment["tokens_cache_read"] = transcript_data["tokens_cache_read"]
                enrichment["tokens_cache_creation"] = transcript_data["tokens_cache_creation"]

            # Model from transcript (actual model used, more accurate)
            if transcript_data["model"]:
                enrichment["model_resolved"] = transcript_data["model"]

            if tool_use_id and tool_use_id in dispatch_by_id:
                for idx in dispatch_by_id[tool_use_id]:
                    dispatches[idx].update(enrichment)
                    # Return text from transcript's last assistant message.
                    # Only backfill if sync path didn't already populate it.
                    if not dispatches[idx].get("returned") and transcript_data.get("returned"):
                        dispatches[idx]["returned"] = transcript_data["returned"]
            else:
                # No matching dispatch from main log — create one from meta
                # alone.  Outcome is unknown (killed session recovery path),
                # so mark dispatch_complete based on transcript truncation.
                is_truncated = transcript_data["truncated"]
                dispatch = {
                    "dispatch_id": tool_use_id or f"meta:{agent_id}",
                    "session_id": session_id,
                    "agent_id": agent_id,
                    "status": "completed" if not is_truncated else "unknown",
                    "dispatch_complete": not is_truncated,
                    "seq": 0,
                    "ts_utc": "",
                }
                dispatch.update(enrichment)
                if transcript_data.get("returned"):
                    dispatch["returned"] = transcript_data["returned"]
                dispatches.append(dispatch)

            agents_done.add(agent_id)

    # Mark dispatches that have no transcript as transcript_captured=false
    for d in dispatches:
        if "transcript_captured" not in d:
            d["transcript_captured"] = False

    # Copy tool-results
    tool_results_dir = tp.with_suffix("") / "tool-results"
    if tool_results_dir.is_dir():
        dest_tr = agentlog / "tool-results"
        dest_tr.mkdir(parents=True, exist_ok=True)
        for f in tool_results_dir.iterdir():
            if f.is_file():
                dest_file = dest_tr / f.name
                if not dest_file.exists():
                    try:
                        shutil.copy2(f, dest_file)
                    except OSError:
                        pass

    # Update state
    sess["agents_done"] = sorted(agents_done)


# ---------------------------------------------------------------------------
# Normalization, redaction, Class A checks (Task 5)
# ---------------------------------------------------------------------------

# Schema fields that every normalized row must carry.
_SCHEMA_FIELDS = [
    "schema_version", "harness", "harness_version", "protocol_version",
    "machine_id", "project_id", "ts_utc",
    "session_id", "dispatch_id", "seq", "parent_dispatch_id", "spawn_depth",
    "role", "overlay", "config_hash", "model_requested", "model_resolved",
    "verdict", "status", "dispatch_complete",
    "tokens_input", "tokens_output", "tokens_cache_read",
    "tokens_cache_creation", "tool_uses", "duration_ms",
    "artifact_exists", "has_preamble", "next_is_reachable", "return_parsed",
    "transcript_ref", "transcript_captured", "transcript_truncated",
    "ext",
]

# Hardcoded fallback roles for next_is_reachable checks.
# Known limitation: this set is incomplete.  _get_known_roles() augments it
# dynamically from .claude/agents/*.md filenames in the project directory.
_BUILTIN_ROLES = frozenset({
    "implementer", "reviewer", "architect", "planner", "tester",
    "debugger", "explorer", "Explore", "Plan", "code-reviewer",
    "general-purpose", "claude", "python-pro", "typescript-pro",
    "frontend-developer", "backend-developer", "fullstack-developer",
    "feature-dev:code-architect", "feature-dev:code-explorer",
    "feature-dev:code-reviewer",
})


def _get_known_roles(cwd: str) -> frozenset[str]:
    """Return the set of known roles, augmented by .claude/agents/*.md.

    Scans the project's .claude/agents/ directory for agent definition
    files and adds their stem names (without .md) to the builtin set.
    """
    roles = set(_BUILTIN_ROLES)
    agents_dir = Path(cwd) / ".claude" / "agents"
    if agents_dir.is_dir():
        try:
            for entry in agents_dir.iterdir():
                if entry.suffix == ".md" and entry.is_file():
                    roles.add(entry.stem)
        except OSError:
            pass
    return frozenset(roles)

_machine_id_cache: str | None = None


def _get_machine_id() -> str:
    """Stable machine identifier.  macOS: platform.node().
    Linux: /etc/machine-id.  Fallback: hostname.
    """
    global _machine_id_cache
    if _machine_id_cache is not None:
        return _machine_id_cache
    mid = ""
    if sys.platform == "linux":
        try:
            mid = Path("/etc/machine-id").read_text().strip()
        except OSError:
            pass
    if not mid:
        mid = platform.node() or "unknown"
    _machine_id_cache = mid
    return mid


def _get_project_id(cwd: str) -> tuple[str, bool]:
    """Derive project_id from git root commit hash.

    Returns (project_id, provisional).  If git fails, falls back to
    hash of machine_id:cwd with provisional=True.
    """
    try:
        result = subprocess.run(
            ["git", "rev-list", "--max-parents=0", "HEAD"],
            capture_output=True, text=True, timeout=5,
            cwd=cwd,
        )
        if result.returncode == 0 and result.stdout.strip():
            roots = sorted(result.stdout.strip().split())
            root_sha = roots[0]
            pid = hashlib.sha256(root_sha.encode()).hexdigest()[:16]
            return pid, False
    except (OSError, subprocess.TimeoutExpired):
        pass
    # Fallback: hash of machine_id:cwd
    fallback = f"{_get_machine_id()}:{cwd}"
    pid = hashlib.sha256(fallback.encode()).hexdigest()[:16]
    return pid, True


def _load_redaction_patterns(project_cwd: str | None = None) -> list[tuple[str, "re.Pattern[str]"]]:
    """Load redaction patterns from telemetry.yaml and optional .zprof.yaml.

    Returns list of (pattern_name, compiled_regex) tuples.
    """
    patterns: list[tuple[str, "re.Pattern[str]"]] = []

    # Load from telemetry.yaml (bundled next to this script)
    telemetry_yaml = Path(__file__).parent / "telemetry.yaml"
    if telemetry_yaml.exists():
        patterns.extend(_parse_redaction_patterns_from_yaml(telemetry_yaml))

    # Load from project's .zprof.yaml if present
    if project_cwd:
        zprof_yaml = Path(project_cwd) / ".zprof.yaml"
        if zprof_yaml.exists():
            patterns.extend(_parse_redaction_patterns_from_yaml(zprof_yaml))

    return patterns


def _parse_redaction_patterns_from_yaml(path: Path) -> list[tuple[str, "re.Pattern[str]"]]:
    """Parse redaction_patterns from a YAML file (stdlib-only parser)."""
    results = []
    try:
        text = path.read_text()
    except OSError:
        return results

    in_section = False
    for line in text.split("\n"):
        stripped = line.strip()
        if stripped.startswith("redaction_patterns:"):
            in_section = True
            continue
        if in_section:
            if stripped.startswith("- "):
                # Extract the pattern string (YAML list item)
                raw_pat = stripped[2:].strip()
                # Remove quotes if present
                if (raw_pat.startswith('"') and raw_pat.endswith('"')) or \
                   (raw_pat.startswith("'") and raw_pat.endswith("'")):
                    raw_pat = raw_pat[1:-1]
                # Unescape YAML double-quoted backslashes
                raw_pat = raw_pat.replace("\\\\", "\\")
                # Derive a short name from the pattern
                name = _pattern_name(raw_pat)
                try:
                    compiled = re.compile(raw_pat)
                    results.append((name, compiled))
                except re.error:
                    pass
            elif stripped and not stripped.startswith("#"):
                # Next YAML key — end of redaction_patterns section
                in_section = False

    return results


def _pattern_name(pattern: str) -> str:
    """Derive a short human-readable name from a regex pattern."""
    # Take the first recognisable prefix
    for prefix in ("AWS_", "GITHUB_TOKEN", "sk-", "ghp_", "Bearer",
                   "BEGIN", "password", "api_key"):
        if prefix in pattern:
            return prefix
    # Fallback: first 20 chars
    return pattern[:20]


def _redact_secrets(value, patterns: list[tuple[str, "re.Pattern[str]"]]) -> tuple:
    """Recursively redact secrets from strings/dicts/lists.

    Returns (redacted_value, total_count).
    """
    if not patterns:
        return value, 0

    if isinstance(value, str):
        count = 0
        result = value
        for name, pat in patterns:
            new_result, n = pat.subn(f"⟦redacted:{name}⟧", result)
            count += n
            result = new_result
        return result, count

    if isinstance(value, dict):
        total = 0
        out = {}
        for k, v in value.items():
            rv, c = _redact_secrets(v, patterns)
            out[k] = rv
            total += c
        return out, total

    if isinstance(value, list):
        total = 0
        out = []
        for item in value:
            rv, c = _redact_secrets(item, patterns)
            out.append(rv)
            total += c
        return out, total

    return value, 0


def _class_a_checks(returned: str | None, cwd: str,
                     known_roles: frozenset[str] | None = None) -> dict:
    """Run Class A contract compliance checks on return text.

    Returns dict with has_preamble, return_parsed, artifact_exists,
    next_is_reachable.
    """
    result = {
        "has_preamble": None,
        "return_parsed": None,
        "artifact_exists": None,
        "next_is_reachable": None,
    }
    if returned is None:
        return result

    lines = returned.split("\n")

    # Find first verdict: line
    verdict_idx = None
    for i, line in enumerate(lines):
        if line.strip().lower().startswith("verdict:"):
            verdict_idx = i
            break

    # return_parsed: true if verdict: line exists
    result["return_parsed"] = verdict_idx is not None

    # has_preamble: true if non-whitespace text before first verdict: line
    if verdict_idx is not None:
        preamble = "\n".join(lines[:verdict_idx]).strip()
        result["has_preamble"] = len(preamble) > 0
    else:
        result["has_preamble"] = None

    # artifact_exists: check if referenced artifact path exists
    for line in lines:
        stripped = line.strip()
        if stripped.lower().startswith("artifact:"):
            artifact_path = stripped.split(":", 1)[1].strip()
            if artifact_path:
                # Try absolute path first, then relative to cwd
                p = Path(artifact_path)
                if not p.is_absolute():
                    p = Path(cwd) / artifact_path
                result["artifact_exists"] = p.exists()
            break

    # next_is_reachable: check if next: value is a known role
    roles = known_roles if known_roles is not None else _BUILTIN_ROLES
    for line in lines:
        stripped = line.strip()
        if stripped.lower().startswith("next:"):
            next_val = stripped.split(":", 1)[1].strip()
            if next_val:
                result["next_is_reachable"] = next_val in roles
            break

    return result


def _make_composite_id(session_id: str, raw_id: str) -> str:
    """Prefix a raw dispatch/parent ID with claude-code:<session>:."""
    if not raw_id:
        return ""
    if raw_id.startswith("unresolved:") or raw_id.startswith("meta:"):
        return raw_id
    if raw_id.startswith("claude-code:"):
        return raw_id
    return f"claude-code:{session_id}:{raw_id}"


def _normalize_dispatch(
    raw: dict,
    session_id: str,
    harness_version: str,
    machine_id: str,
    project_id: str,
    project_id_provisional: bool,
    redaction_patterns: list[tuple[str, "re.Pattern[str]"]],
    known_roles: frozenset[str] | None = None,
) -> tuple[dict, int]:
    """Transform a raw dispatch dict into a schema-compliant row.

    Returns (normalized_dict, redaction_count).
    """
    # Start with identity fields
    norm = {
        "schema_version": 1,
        "harness": "claude-code",
        "harness_version": harness_version,
        "machine_id": machine_id,
        "project_id": project_id,
    }
    # Copy known fields from raw, applying defaults
    norm["protocol_version"] = raw.get("protocol_version")
    norm["ts_utc"] = raw.get("ts_utc", "")
    norm["session_id"] = session_id
    norm["dispatch_id"] = _make_composite_id(session_id, raw.get("dispatch_id", ""))
    norm["seq"] = raw.get("seq", 0)

    parent_did = raw.get("parent_dispatch_id", "")
    if parent_did:
        norm["parent_dispatch_id"] = _make_composite_id(session_id, parent_did)

    norm["spawn_depth"] = raw.get("spawn_depth", 1)
    norm["role"] = raw.get("role", "")
    norm["overlay"] = raw.get("overlay")
    norm["config_hash"] = raw.get("config_hash")
    norm["model_requested"] = raw.get("model_requested")
    norm["model_resolved"] = raw.get("model_resolved")
    norm["verdict"] = raw.get("verdict")
    norm["status"] = raw.get("status")
    norm["dispatch_complete"] = raw.get("dispatch_complete", False)

    # Cost fields
    norm["tokens_input"] = raw.get("tokens_input")
    norm["tokens_output"] = raw.get("tokens_output")
    norm["tokens_cache_read"] = raw.get("tokens_cache_read")
    norm["tokens_cache_creation"] = raw.get("tokens_cache_creation")
    norm["tool_uses"] = raw.get("tool_uses")
    norm["duration_ms"] = raw.get("duration_ms")

    # Provenance
    norm["transcript_ref"] = raw.get("transcript_ref")
    norm["transcript_captured"] = raw.get("transcript_captured", False)
    norm["transcript_truncated"] = raw.get("transcript_truncated")

    # Extension — merge project_id_provisional into ext (not a core field)
    ext = raw.get("ext")
    if project_id_provisional:
        if ext is None:
            ext = {}
        ext = dict(ext)  # copy to avoid mutating raw
        ext["project_id_provisional"] = True
    norm["ext"] = ext

    # Class A checks
    returned = raw.get("returned")
    cwd = raw.get("cwd", "")
    checks = _class_a_checks(returned, cwd, known_roles=known_roles)
    norm.update(checks)

    # Remove None values for cleaner JSONL (optional fields)
    norm = {k: v for k, v in norm.items() if v is not None}

    # Redact secrets from all string values
    norm, redaction_count = _redact_secrets(norm, redaction_patterns)

    return norm, redaction_count


def _load_dedup_set(dispatches_path: Path) -> set[tuple[str, int]]:
    """Load existing (dispatch_id, seq) pairs from dispatches.jsonl."""
    seen = set()
    if not dispatches_path.exists():
        return seen
    try:
        with open(dispatches_path, "r") as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    row = json.loads(line)
                    did = row.get("dispatch_id", "")
                    seq = row.get("seq", 0)
                    seen.add((did, seq))
                except json.JSONDecodeError:
                    continue
    except OSError:
        pass
    return seen


def _normalize_and_write(
    agentlog: Path,
    dispatches: list[dict],
    session_id: str,
    harness_version: str,
    payload: dict,
):
    """Normalize raw dispatches and append to dispatches.jsonl.

    Fills identity fields, builds composite dispatch_id, runs Class A
    checks, redacts secrets, dedup-checks, and appends new rows with fsync.
    """
    if not dispatches:
        return

    cwd = payload.get("cwd", os.getcwd())

    # Identity
    machine_id = _get_machine_id()
    project_id, provisional = _get_project_id(cwd)

    # Redaction patterns
    redaction_patterns = _load_redaction_patterns(cwd)

    # Known roles (dynamic from .claude/agents/*.md + builtins)
    known_roles = _get_known_roles(cwd)

    # Dedup set
    dispatches_path = agentlog / "dispatches.jsonl"
    seen = _load_dedup_set(dispatches_path)

    total_redactions = 0
    rows_written = 0

    with open(dispatches_path, "a") as f:
        for raw in dispatches:
            # Inject cwd for artifact_exists checks
            raw_with_cwd = dict(raw)
            if "cwd" not in raw_with_cwd:
                raw_with_cwd["cwd"] = cwd

            norm, redact_count = _normalize_dispatch(
                raw_with_cwd,
                session_id=session_id,
                harness_version=harness_version,
                machine_id=machine_id,
                project_id=project_id,
                project_id_provisional=provisional,
                redaction_patterns=redaction_patterns,
                known_roles=known_roles,
            )
            total_redactions += redact_count

            # Dedup
            dedup_key = (norm.get("dispatch_id", ""), norm.get("seq", 0))
            if dedup_key in seen:
                continue
            seen.add(dedup_key)

            f.write(json.dumps(norm, ensure_ascii=False) + "\n")
            rows_written += 1

        f.flush()
        os.fsync(f.fileno())

    if total_redactions > 0:
        _log_info(agentlog,
                  f"session {session_id}: redacted {total_redactions} secret(s) "
                  f"across {rows_written} dispatch(es)")


def _log_info(agentlog: Path, msg: str):
    log = agentlog / "collect.log"
    ts = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    with open(log, "a") as f:
        f.write(f"{ts} INFO {msg}\n")
        f.flush()
        os.fsync(f.fileno())


def _log_error(agentlog: Path, msg: str):
    log = agentlog / "collect.log"
    ts = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    with open(log, "a") as f:
        f.write(f"{ts} ERROR {msg}\n")
        f.flush()
        os.fsync(f.fileno())


if __name__ == "__main__":
    main()
