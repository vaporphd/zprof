# Telemetry Stage 1: Collection — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every Claude Code dispatch in every zprof-managed project writes a normalized record into `<project>/.agentlog/` — immediately, without zprof on the machine, surviving crashes and parallel sessions.

**Architecture:** A single Python script (`zprof-collect.py`) is installed into each project by `zprof apply`. Claude Code calls it via three hooks (`SubagentStop`, `Stop`, `SessionStart`) in `settings.local.json`. The script reads from `~/.claude/projects/` (via `transcript_path` in the hook payload, never by reconstructing slugs), normalizes into a versioned JSONL schema, and writes to `<project>/.agentlog/`. Go code only adds the plumbing: gitignore entry, settings.local.json upsert, doctor checks, and schema definition.

**Tech Stack:** Python 3 (system), Go 1.22 (cli), cobra (commands), testify (assertions)

## Global Constraints

- Python 3 — must work with `/usr/bin/python3` on macOS (Xcode CLT shim) and `python3` on Linux. No pip dependencies.
- Go 1.22 — `cli/go.mod`.
- Tests: `go test -race -count=1 ./...` must pass. CI at `.github/workflows/ci.yml`.
- All doctor messages in English (`TestDoctorMessagesAreEnglish` enforces this).
- `ensureGitignore` list is hardcoded at `cli/internal/apply/engine.go:288` — no `gitignore:` field in base manifest.
- `fsutil.WriteFileAtomic` for all crash-safe writes in Go.
- Collector never writes outside `<project>/.agentlog/`. Test enforces this.
- Collector always exits 0. Errors go to `collect.log`.
- Paths from `transcript_path` in payload, never reconstructed from `cwd` (§4.2.1 — slug encoding is lossy and undocumented).

---

## Epic 1: Schema and collector scaffold

### Task 1: Core schema definition

**Files:**
- Create: `profiles/base/telemetry.yaml`
- Create: `profiles/base/telemetry_test.py` (validates schema self-consistency)
- Test: `python3 profiles/base/telemetry_test.py`

**Interfaces:**
- Produces: canonical field list consumed by Tasks 3–5

The schema is the contract between the collector (writes) and `zprof stats` (reads). Getting it wrong means a migration.

- [ ] **Step 1: Write the schema YAML**

```yaml
# profiles/base/telemetry.yaml
# Core schema for .agentlog/dispatches.jsonl
# All projects write these fields; cross-project comparison uses only these.
version: 1

core_fields:
  # identity
  - {name: schema_version,      type: int,    required: true}
  - {name: harness,             type: string, required: true}  # "claude-code"
  - {name: harness_version,     type: string, required: true}  # e.g. "2.1.220"
  - {name: protocol_version,    type: string, required: false} # reserved for kimi etc
  - {name: machine_id,          type: string, required: true}
  - {name: project_id,          type: string, required: true}  # hash of root commit
  - {name: ts_utc,              type: string, required: true}  # ISO 8601 with offset

  # dispatch tree
  - {name: session_id,          type: string, required: true}
  - {name: dispatch_id,         type: string, required: true}  # <harness>:<session>:<native>
  - {name: seq,                 type: int,    required: true,  default: 0}
  - {name: parent_dispatch_id,  type: string, required: false}
  - {name: spawn_depth,         type: int,    required: true,  default: 1}

  # execution
  - {name: role,                type: string, required: true}
  - {name: overlay,             type: string, required: false}
  - {name: config_hash,         type: string, required: false}
  - {name: model_requested,     type: string, required: false}
  - {name: model_resolved,      type: string, required: false}

  # outcome
  - {name: verdict,             type: string, required: false}
  - {name: status,              type: string, required: false}
  - {name: dispatch_complete,   type: bool,   required: true,  default: false}

  # cost (four fields — no totalTokens shortcut)
  - {name: tokens_input,        type: int,    required: false}
  - {name: tokens_output,       type: int,    required: false}
  - {name: tokens_cache_read,   type: int,    required: false}
  - {name: tokens_cache_creation, type: int,  required: false}
  - {name: tool_uses,           type: int,    required: false}
  - {name: duration_ms,         type: int,    required: false}

  # contract compliance (class A)
  - {name: artifact_exists,     type: bool,   required: false}
  - {name: has_preamble,        type: bool,   required: false}
  - {name: next_is_reachable,   type: bool,   required: false}
  - {name: return_parsed,       type: bool,   required: false}

  # provenance
  - {name: transcript_ref,      type: string, required: false}
  - {name: transcript_captured, type: bool,   required: true,  default: false}
  - {name: transcript_truncated,type: bool,   required: false}

  # extension
  - {name: ext,                 type: object, required: false}

redaction_patterns:
  - "AWS_[A-Z_]*=[^\\s]+"
  - "GITHUB_TOKEN=[^\\s]+"
  - "sk-[a-zA-Z0-9]{20,}"
  - "ghp_[a-zA-Z0-9]{36,}"
  - "Bearer [a-zA-Z0-9._\\-]+"
  - "-----BEGIN [A-Z ]+ PRIVATE KEY-----"
  - "password=[^\\s&]+"
  - "api_key=[^\\s&]+"
```

- [ ] **Step 2: Write validation test**

```python
#!/usr/bin/env python3
"""Validate telemetry.yaml self-consistency."""
import yaml, sys, pathlib

SCHEMA_PATH = pathlib.Path(__file__).parent / "telemetry.yaml"

def test_schema():
    schema = yaml.safe_load(SCHEMA_PATH.read_text())
    fields = schema["core_fields"]
    names = [f["name"] for f in fields]

    # no duplicates
    assert len(names) == len(set(names)), f"duplicate fields: {[n for n in names if names.count(n) > 1]}"

    # required fields have no default (they must always be provided)
    for f in fields:
        if f["required"] and "default" not in f:
            pass  # fine — must be provided
        if not f["required"] and f["type"] == "bool" and "default" not in f:
            pass  # optional bool without default is fine

    # types are known
    valid_types = {"string", "int", "bool", "object"}
    for f in fields:
        assert f["type"] in valid_types, f"{f['name']}: unknown type {f['type']}"

    # redaction patterns compile
    import re
    for p in schema["redaction_patterns"]:
        re.compile(p)

    print(f"OK: {len(fields)} fields, {len(schema['redaction_patterns'])} redaction patterns")

if __name__ == "__main__":
    test_schema()
```

- [ ] **Step 3: Run validation**

Run: `python3 profiles/base/telemetry_test.py`
Expected: `OK: 28 fields, 8 redaction patterns`

- [ ] **Step 4: Commit**

```bash
git add profiles/base/telemetry.yaml profiles/base/telemetry_test.py
git commit -m "feat(telemetry): core schema definition (telemetry.yaml)"
```

---

### Task 2: Collector scaffold — payload parsing, state, locking

**Files:**
- Create: `profiles/base/zprof-collect.py` (executable, `chmod +x`)
- Create: `profiles/base/tests/test_collector_scaffold.py`
- Test: `python3 -m pytest profiles/base/tests/ -v`

**Interfaces:**
- Consumes: `telemetry.yaml` field list from Task 1
- Produces: `Collector` class with `run(mode, payload)`, `State` class, `Lock` context manager — used by Tasks 3–5

This task builds the skeleton: CLI entry point, payload parsing, state.json read/write, flock, collect.log, and the three-mode dispatch. No actual extraction yet.

- [ ] **Step 1: Write failing tests**

```python
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
```

- [ ] **Step 2: Write the collector scaffold**

```python
#!/usr/bin/env python3
"""zprof telemetry collector — runs as Claude Code hook.

Usage: zprof-collect.py <mode>
Modes: subagent-stop | stop | session-start

Reads JSON payload from stdin. Writes to $ZPROF_AGENTLOG or <cwd>/.agentlog/.
Always exits 0. Errors go to collect.log.
"""
import fcntl, json, os, sys, tempfile, traceback
from pathlib import Path
from datetime import datetime, timezone

VERSION = "0.1.0"


def main():
    mode = sys.argv[1] if len(sys.argv) > 1 else "stop"
    try:
        raw = sys.stdin.read()
        payload = json.loads(raw)
    except Exception:
        agentlog = Path(os.environ.get("ZPROF_AGENTLOG", ".agentlog"))
        agentlog.mkdir(parents=True, exist_ok=True)
        _log_error(agentlog, f"payload parse failed: {raw[:200] if 'raw' in dir() else 'no stdin'}")
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
        except BlockingIOError:
            import time
            deadline = time.monotonic() + self.timeout
            while time.monotonic() < deadline:
                try:
                    fcntl.flock(self._fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    return self
                except BlockingIOError:
                    time.sleep(0.05)
            raise TimeoutError(f"could not acquire lock within {self.timeout}s")
        return self

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
        tmp.write_text(json.dumps(self.data, indent=2, ensure_ascii=False))
        os.fsync(tmp.open().fileno())
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
        tp = Path(transcript_path)
        if not tp.exists():
            _log_error(self.agentlog, f"transcript not found: {transcript_path}")
            self.state.increment_losses()
            return
        sess = self.state.session(session_id)
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
```

- [ ] **Step 3: Make executable**

```bash
chmod +x profiles/base/zprof-collect.py
```

- [ ] **Step 4: Run tests**

Run: `cd /Volumes/mydata/projects/zprof && python3 -m pytest profiles/base/tests/test_collector_scaffold.py -v`
Expected: all pass

- [ ] **Step 5: Commit**

```bash
git add profiles/base/zprof-collect.py profiles/base/tests/test_collector_scaffold.py
git commit -m "feat(telemetry): collector scaffold — payload, state, locking"
```

---

## Epic 2: Extraction and normalization

### Task 3: Main log extraction

**Files:**
- Modify: `profiles/base/zprof-collect.py` — fill in `_collect_session` main log reading
- Create: `profiles/base/tests/test_main_log_extraction.py`
- Create: `profiles/base/tests/fixtures/` — sample JSONL fragments

**Interfaces:**
- Consumes: `Collector._collect_session()` stub from Task 2
- Consumes: schema from Task 1
- Produces: `_extract_main_log(session_id, transcript_path)` → list of raw dispatch dicts

Extracts dispatches from main session JSONL. Handles both `toolUseResult` (new path) and `task-notification` (old path). Handles async dispatches (`async_launched`). Uses offset + size+hash verification from §4.2.

- [ ] **Step 1: Create JSONL fixtures**

Create minimal JSONL fragments in `profiles/base/tests/fixtures/`:
- `sync_dispatch.jsonl` — assistant with tool_use + user with toolUseResult (completed)
- `async_dispatch.jsonl` — assistant with tool_use + user with toolUseResult (async_launched) + user with task-notification (completed)
- `task_notification_legacy.jsonl` — queue-operation with task-notification XML
- `truncated_session.jsonl` — file ending mid-line (simulates kill -9)
- `with_compact_boundary.jsonl` — contains compact_boundary marker

Each fixture is a valid subset of a real JSONL, minimal but complete enough to parse.

- [ ] **Step 2: Write failing tests**

Tests covering:
- Extract dispatches from sync JSONL → correct `dispatch_id`, `model`, `status`
- Extract dispatches from async JSONL → `dispatch_complete: false` on launch, `true` on notification
- Extract from legacy task-notification → same fields
- Truncated file → parse up to last complete line, `transcript_truncated: true`
- Offset verification: tampered file (changed head) → re-read from zero
- Offset works: second pass with unchanged file → reads only new lines
- `seq` increments for same `dispatch_id` notified twice

- [ ] **Step 3: Implement `_extract_main_log`**

Core extraction logic:
1. Read `transcript_path` from saved offset (after size+hash check)
2. Parse each JSONL line by type: `assistant` → tool_use dispatch, `user` → toolUseResult or task-notification
3. Build dispatch dict with fields from schema
4. Track `harness_version` from `version` field in any message
5. Count `unparsed_lines` for §13
6. Return list of raw dispatch dicts + updated offset

- [ ] **Step 4: Run tests, iterate**

Run: `python3 -m pytest profiles/base/tests/test_main_log_extraction.py -v`

- [ ] **Step 5: Commit**

```bash
git add profiles/base/zprof-collect.py profiles/base/tests/
git commit -m "feat(telemetry): main log extraction — sync, async, legacy paths"
```

---

### Task 4: Subagent transcript capture

**Files:**
- Modify: `profiles/base/zprof-collect.py` — add `_capture_transcripts`
- Create: `profiles/base/tests/test_transcript_capture.py`
- Extend fixtures

**Interfaces:**
- Consumes: `running_agents` set from payload's `background_tasks`
- Consumes: `state.session(sid)["agents_done"]`
- Produces: copied transcripts in `.agentlog/transcripts/<agent_id>.jsonl.gz`, metadata in dispatch dicts

Copies subagent transcripts and meta.json. Skips agents with `status: running`. Extracts `usage` with four-field token breakdown from transcript. Extracts `model` per turn. Builds tree via `parentAgentId`/`spawnDepth`.

- [ ] **Step 1: Create transcript fixtures**

- `agent-test123.jsonl` — minimal subagent transcript with 3 turns, `usage` fields
- `agent-test123.meta.json` — `{"agentType":"implementer","toolUseId":"toolu_test","spawnDepth":1}`
- `agent-nested456.meta.json` — with `parentAgentId` and `spawnDepth: 2`

- [ ] **Step 2: Write failing tests**

Tests covering:
- Transcript copied to `.agentlog/transcripts/` as `.jsonl.gz`
- `meta.json` read → `role`, `parent_dispatch_id`, `spawn_depth` in dispatch dict
- Running agents skipped (their id in `running_agents` set)
- Already-captured agents skipped (`agents_done` in state)
- Token breakdown extracted from `usage` in transcript (sum of all turns)
- `model` extracted from transcript
- Missing transcript → `transcript_captured: false`, loss counted
- `tool-results/` files copied to `.agentlog/tool-results/`

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run tests**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(telemetry): subagent transcript capture with token breakdown"
```

---

### Task 5: Normalization and secret redaction

**Files:**
- Modify: `profiles/base/zprof-collect.py` — add `_normalize` and `_redact`
- Create: `profiles/base/tests/test_normalization.py`

**Interfaces:**
- Consumes: raw dispatch dicts from Tasks 3–4
- Consumes: `redaction_patterns` from `telemetry.yaml` (Task 1)
- Produces: normalized rows appended to `.agentlog/dispatches.jsonl`

Transforms raw extractions into schema-compliant JSONL rows. Applies Class A checks. Redacts secrets. Counts unparsed and reports losses.

- [ ] **Step 1: Write failing tests**

Tests covering:
- Normalized row has all required fields from schema
- `dispatch_id` is composite: `claude-code:<session>:<toolu_...>`
- Class A checks: `has_preamble` detects text before `verdict:`, `artifact_exists` checks disk, `return_parsed` true when schema matches
- Secret redaction: `AWS_SECRET_ACCESS_KEY=AKIA...` → `⟦redacted:AWS_⟧`
- Redaction counter in `collect.log`
- Dedup: same `(dispatch_id, seq)` not written twice
- `ext` field preserved as-is
- `project_id`: reads git root commit hash; fallback to `machine_id:cwd` with `project_id_provisional: true`
- `machine_id`: stable across calls (platform.node() or /etc/machine-id)

- [ ] **Step 2: Implement normalization**

- [ ] **Step 3: Implement redaction**

Load patterns from `telemetry.yaml` + project's `.zprof.yaml` `redaction_patterns`.

- [ ] **Step 4: Run tests**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(telemetry): normalization, class A checks, secret redaction"
```

---

## Epic 3: Apply integration

### Task 6: Gitignore and collector deployment

**Files:**
- Modify: `cli/internal/apply/engine.go:288` — add `.agentlog/` to gitignore list
- Modify: `cli/internal/apply/engine.go` — add step to copy `zprof-collect.py` into project
- Modify: `cli/internal/overlay/loader.go` — load `zprof-collect.py` into `Base`
- Create: `cli/internal/apply/engine_test.go` — new test case
- Test: `cd cli && go test -race -count=1 ./internal/apply/...`

**Interfaces:**
- Consumes: `profiles/base/zprof-collect.py` from Task 2
- Consumes: `profiles/base/telemetry.yaml` from Task 1
- Produces: `<project>/.claude/zprof-collect.py` (executable) and `<project>/.agentlog/schema.json` on `zprof apply`

- [ ] **Step 1: Write failing test**

In `engine_test.go`, add test:
- After `Apply()`, `.gitignore` contains `.agentlog/`
- After `Apply()`, `.claude/zprof-collect.py` exists and is executable
- After `Apply()`, `.agentlog/schema.json` exists with `version: 1`
- Idempotent: second `Apply()` doesn't duplicate gitignore entry or break script

- [ ] **Step 2: Add `.agentlog/` to gitignore list**

In `engine.go:288`, add `".agentlog/"` to the `entries` slice:
```go
entries := []string{"thoughts/", ".zprof/runs/", "*.zprof.bak-*", ".zprof.yaml.bak-*", ".agentlog/"}
```

- [ ] **Step 3: Add collector deployment step to Apply**

Between steps 5 (state files) and 6 (gitignore), add:
```go
// 5.5. Deploy telemetry collector
if err := deployCollector(opts); err != nil {
    return nil, fmt.Errorf("deploy collector: %w", err)
}
```

`deployCollector` writes `zprof-collect.py` from `opts.Base.CollectorScript` (loaded in loader.go) to `<project>/.claude/zprof-collect.py` with `0o755` permissions, and writes `schema.json` (generated from `opts.Base.TelemetrySchema`) to `<project>/.agentlog/schema.json`.

- [ ] **Step 4: Load collector in overlay/loader.go**

Add `CollectorScript []byte` and `TelemetrySchema []byte` fields to `Base` struct. Load from `profiles/base/zprof-collect.py` and `profiles/base/telemetry.yaml` in `LoadBase()`.

- [ ] **Step 5: Run tests**

Run: `cd cli && go test -race -count=1 ./internal/apply/... ./internal/overlay/...`

- [ ] **Step 6: Commit**

```bash
git commit -m "feat(apply): deploy collector and schema on zprof apply"
```

---

### Task 7: JSON upsert for settings.local.json

**Files:**
- Create: `cli/internal/apply/settings.go` — `EnsureHooks(projectDir string) error`
- Create: `cli/internal/apply/settings_test.go`
- Modify: `cli/internal/apply/engine.go` — call `EnsureHooks` in `Apply()`
- Test: `cd cli && go test -race -count=1 ./internal/apply/...`

**Interfaces:**
- Consumes: `Apply()` orchestration from Task 6
- Produces: `<project>/.claude/settings.local.json` with three hooks

This is the JSON-upsert the spec calls out as a separate piece of work (§4.1, §15). Idempotent: insert hooks without clobbering existing keys. Guard: `test -x … || true`. Remove on rollback.

- [ ] **Step 1: Write failing tests**

```go
func TestEnsureHooks_CreatesFile(t *testing.T) {
    dir := t.TempDir()
    require.NoError(t, EnsureHooks(dir))

    data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
    require.NoError(t, err)

    var settings map[string]any
    require.NoError(t, json.Unmarshal(data, &settings))

    hooks, ok := settings["hooks"].(map[string]any)
    require.True(t, ok, "hooks key missing")
    require.Contains(t, hooks, "SubagentStop")
    require.Contains(t, hooks, "Stop")
    require.Contains(t, hooks, "SessionStart")
}

func TestEnsureHooks_PreservesExisting(t *testing.T) {
    dir := t.TempDir()
    claudeDir := filepath.Join(dir, ".claude")
    require.NoError(t, os.MkdirAll(claudeDir, 0o755))

    existing := map[string]any{
        "hooks": map[string]any{
            "UserPromptSubmit": []any{map[string]any{
                "hooks": []any{map[string]any{"type": "command", "command": "echo hi"}},
            }},
        },
        "permissions": map[string]any{"allow": []string{"Read"}},
    }
    data, _ := json.MarshalIndent(existing, "", "  ")
    require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), data, 0o644))

    require.NoError(t, EnsureHooks(dir))

    data, _ = os.ReadFile(filepath.Join(claudeDir, "settings.local.json"))
    var result map[string]any
    require.NoError(t, json.Unmarshal(data, &result))

    hooks := result["hooks"].(map[string]any)
    require.Contains(t, hooks, "UserPromptSubmit", "existing hook clobbered")
    require.Contains(t, hooks, "SubagentStop", "new hook not added")
    require.Contains(t, result, "permissions", "non-hook key clobbered")
}

func TestEnsureHooks_Idempotent(t *testing.T) {
    dir := t.TempDir()
    require.NoError(t, EnsureHooks(dir))
    require.NoError(t, EnsureHooks(dir))  // second call

    data, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
    // Check no duplicate entries in hook arrays
    count := strings.Count(string(data), "zprof-collect.py")
    require.Equal(t, 3, count, "should have exactly 3 references (one per hook)")
}

func TestEnsureHooks_GuardCommand(t *testing.T) {
    dir := t.TempDir()
    require.NoError(t, EnsureHooks(dir))

    data, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
    require.Contains(t, string(data), `test -x`, "guard missing")
    require.Contains(t, string(data), `|| true`, "fallback missing")
}
```

- [ ] **Step 2: Implement `EnsureHooks`**

```go
package apply

import (
    "encoding/json"
    "os"
    "path/filepath"

    "github.com/vaporphd/zprof/cli/internal/fsutil"
)

const hookGuardTemplate = `test -x "$CLAUDE_PROJECT_DIR/.claude/zprof-collect.py" && "$CLAUDE_PROJECT_DIR/.claude/zprof-collect.py" %s || true`

var telemetryHooks = map[string]string{
    "SubagentStop": "subagent-stop",
    "Stop":         "stop",
    "SessionStart": "session-start",
}

func EnsureHooks(projectDir string) error {
    claudeDir := filepath.Join(projectDir, ".claude")
    if err := os.MkdirAll(claudeDir, 0o755); err != nil {
        return err
    }
    settingsPath := filepath.Join(claudeDir, "settings.local.json")

    var settings map[string]any
    if data, err := os.ReadFile(settingsPath); err == nil {
        if err := json.Unmarshal(data, &settings); err != nil {
            settings = make(map[string]any)
        }
    } else {
        settings = make(map[string]any)
    }

    hooks, _ := settings["hooks"].(map[string]any)
    if hooks == nil {
        hooks = make(map[string]any)
    }

    for event, mode := range telemetryHooks {
        command := fmt.Sprintf(hookGuardTemplate, mode)
        hookEntry := map[string]any{
            "hooks": []any{
                map[string]any{"type": "command", "command": command},
            },
        }

        existing, ok := hooks[event].([]any)
        if !ok {
            hooks[event] = []any{hookEntry}
            continue
        }
        if containsZprofHook(existing) {
            continue  // already installed
        }
        hooks[event] = append(existing, hookEntry)
    }

    settings["hooks"] = hooks
    data, err := json.MarshalIndent(settings, "", "  ")
    if err != nil {
        return err
    }
    return fsutil.WriteFileAtomic(settingsPath, data, 0o644)
}

func containsZprofHook(entries []any) bool {
    for _, e := range entries {
        data, _ := json.Marshal(e)
        if strings.Contains(string(data), "zprof-collect.py") {
            return true
        }
    }
    return false
}
```

- [ ] **Step 3: Wire into Apply**

Add `EnsureHooks(opts.ProjectDir)` call in `Apply()` after step 5 (state files).

- [ ] **Step 4: Run tests**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(apply): JSON upsert for telemetry hooks in settings.local.json"
```

---

## Epic 4: Doctor checks and validation

### Task 8: Doctor checks for telemetry

**Files:**
- Modify: `cli/internal/doctor/diagnostics.go` — add 5 checks
- Modify: `cli/internal/doctor/diagnostics_test.go` — tests
- Test: `cd cli && go test -race -count=1 ./internal/doctor/...`

**Interfaces:**
- Consumes: `Diagnose(projectDir, repoDir)` pattern
- Produces: 5 new checks in doctor output

Checks (all messages in English per `TestDoctorMessagesAreEnglish`):

1. `.agentlog/` present in `.gitignore` (like existing `checkRunsGitignored`)
2. `.agentlog/` not tracked by git (separate from gitignore — a file added before gitignore entry is still tracked)
3. Hooks present in `settings.local.json` for all three events
4. `python3` available and runs `python3 -c 'pass'` without error
5. Warning about `git clean -xdf` vulnerability when `.agentlog/` exists and has data

- [ ] **Step 1: Write failing tests**

Follow the pattern in `diagnostics_test.go`: use `writeRepoFixture`, create project dirs, check `hasLevel`/`findIssue`.

- [ ] **Step 2: Implement checks**

Each as a standalone function returning `[]Issue`, appended in `Diagnose()`.

```go
func checkAgentlogGitignored(projectDir string) []Issue { ... }
func checkAgentlogNotTracked(projectDir string) []Issue { ... }
func checkTelemetryHooks(projectDir string) []Issue { ... }
func checkPython3Available() []Issue { ... }
func checkAgentlogCleanVulnerability(projectDir string) []Issue { ... }
```

- [ ] **Step 3: Run tests**

- [ ] **Step 4: Commit**

```bash
git commit -m "feat(doctor): telemetry health checks — gitignore, hooks, python3"
```

---

## Epic 5: Integration test

### Task 9: End-to-end test with real hooks

**Files:**
- Create: `profiles/base/tests/test_e2e.py`
- Create: `profiles/base/tests/fixtures/real_session/` — anonymized real session data

**Interfaces:**
- Consumes: everything from Tasks 1–8
- Produces: confidence that the full pipeline works

Full pipeline test: create a mock `~/.claude/projects/` with real-shaped data, run collector in all three modes, verify `.agentlog/` contents match expected schema, verify no writes outside `.agentlog/`, verify losses counted correctly.

- [ ] **Step 1: Create anonymized session fixture**

From the grilling session experiments, create a minimal but realistic fixture:
- Main JSONL with 3 dispatches (1 sync, 1 async, 1 with compact_boundary)
- 2 subagent transcripts with meta.json (one with spawnDepth 2)
- 1 tool-result

Strip all real content, replace with synthetic but structurally identical data.

- [ ] **Step 2: Write e2e test**

```python
def test_full_pipeline(tmp_path):
    """Complete pipeline: SubagentStop → Stop → verify .agentlog/."""
    # 1. Set up mock ~/.claude/projects/ structure
    # 2. Run subagent-stop with pointer payload
    # 3. Run stop with full payload
    # 4. Verify dispatches.jsonl has 3 rows
    # 5. Verify each row matches schema
    # 6. Verify transcripts copied and gzipped
    # 7. Verify state.json watermarks advanced
    # 8. Verify collect.log has no errors
    # 9. Verify no files outside .agentlog/
```

- [ ] **Step 3: Write session-start recovery test**

```python
def test_session_start_recovers_killed(tmp_path):
    """SessionStart picks up a session that never got Stop."""
    # 1. Set up session data but don't run stop
    # 2. Run session-start for a new session in same slug dir
    # 3. Verify old session's data was collected
    # 4. Verify transcript_truncated flag if appropriate
```

- [ ] **Step 4: Run, iterate**

- [ ] **Step 5: Commit**

```bash
git commit -m "test(telemetry): end-to-end pipeline test with realistic fixtures"
```

---

## Task dependency graph

```
Task 1 (schema) ──┐
                   ├──> Task 3 (main log) ──> Task 5 (normalize) ──> Task 9 (e2e)
Task 2 (scaffold) ─┤                                                    ↑
                   └──> Task 4 (transcripts) ──────────────────────────┘
                                                                        ↑
Task 6 (gitignore + deploy) ────────────────────────────────────────────┤
Task 7 (JSON upsert) ──────────────────────────────────────────────────┤
Task 8 (doctor) ────────────────────────────────────────────────────────┘
```

Tasks 1+2 are independent and can run in parallel.
Tasks 3+4 depend on 2 and can run in parallel.
Task 5 depends on 3+4.
Tasks 6+7+8 depend only on the Go codebase, not on Python tasks — can start after Task 1.
Task 9 depends on everything.

---

## What this plan does NOT cover (stages 2 and 3)

- `zprof stats` command and reports (stage 2)
- Rebuilding `zprof eval` over `.agentlog/` and deleting `parser.go` (stage 2)
- A/B tier rotation in `task-runner` (stage 3)
- General shakedown fixture (stage 3)
- Evaluator diffs to contracts (stage 3)
- `zprof-collect.py pick-arm` mode (stage 3)

These get their own plans after stage 1 is accumulating data.
