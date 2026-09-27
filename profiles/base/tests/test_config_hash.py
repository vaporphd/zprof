"""Tests for config_hash resolution (AC1/AC3, ADR 0001).

Covers:
- _agent_frontmatter_name / _agent_file_index: frontmatter `name:` indexing,
  stem fallback, symlink-escape and backup-file skipping.
- _resolve_agent_file / _agent_config_hash: single match, no match,
  ambiguous match, hash stability and invalidation.
- SubagentStop snapshots config_hash into the pointer.
- _collect_subagent_transcripts: pointer consumed and used when present,
  computed fresh (ext.config_hash_source == "collect") otherwise, and
  ext.config_hash_ambiguous set on ambiguous resolution.
"""
import importlib
import json
import os
import pathlib
import re
import subprocess
import sys

sys.path.insert(0, str(pathlib.Path(__file__).parent.parent))

_mod_path = pathlib.Path(__file__).parent.parent / "zprof-collect.py"
_spec = importlib.util.spec_from_file_location("zprof_collect", _mod_path)
zprof_collect = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(zprof_collect)

_agent_frontmatter_name = zprof_collect._agent_frontmatter_name
_agent_file_index = zprof_collect._agent_file_index
_resolve_agent_file = zprof_collect._resolve_agent_file
_agent_config_hash = zprof_collect._agent_config_hash
_collect_subagent_transcripts = zprof_collect._collect_subagent_transcripts

COLLECTOR = _mod_path
_HEX12 = re.compile(r"^[0-9a-f]{12}$")


def _agent_md(name=None, model="sonnet", body="Body text.\n"):
    """Return Markdown content for an agent file, with or without frontmatter."""
    if name is None:
        return f"# No frontmatter\n\n{body}"
    return f"---\nname: {name}\nmodel: {model}\ndescription: test agent\n---\n\n{body}"


def _write_agent(agents_dir: pathlib.Path, rel_path: str, content: str) -> pathlib.Path:
    p = agents_dir / rel_path
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(content)
    return p


def run_collector(mode, payload, agentlog_dir):
    """Run collector as a subprocess, return (exit_code, state_dict)."""
    env = os.environ.copy()
    env["ZPROF_AGENTLOG"] = str(agentlog_dir)
    p = subprocess.run(
        [sys.executable, str(COLLECTOR), mode],
        input=json.dumps(payload),
        capture_output=True, text=True, env=env, timeout=10, check=False,
    )
    state_path = agentlog_dir / "state.json"
    state = json.loads(state_path.read_text()) if state_path.exists() else {}
    return p.returncode, state


# ---------------------------------------------------------------------------
# _agent_frontmatter_name / _agent_file_index
# ---------------------------------------------------------------------------

class TestAgentFrontmatterName:
    def test_extracts_name(self, tmp_path):
        f = tmp_path / "implementer.md"
        f.write_text(_agent_md(name="implementer"))
        assert _agent_frontmatter_name(f) == "implementer"

    def test_quoted_name(self, tmp_path):
        f = tmp_path / "a.md"
        f.write_text('---\nname: "north-star-auditor"\nmodel: opus\n---\nBody\n')
        assert _agent_frontmatter_name(f) == "north-star-auditor"

    def test_no_frontmatter_returns_none(self, tmp_path):
        f = tmp_path / "a.md"
        f.write_text(_agent_md(name=None))
        assert _agent_frontmatter_name(f) is None

    def test_missing_file_returns_none(self, tmp_path):
        assert _agent_frontmatter_name(tmp_path / "nonexistent.md") is None

    def test_frontmatter_without_name_key(self, tmp_path):
        f = tmp_path / "a.md"
        f.write_text("---\nmodel: sonnet\n---\nBody\n")
        assert _agent_frontmatter_name(f) is None


class TestAgentFileIndex:
    def test_indexes_by_frontmatter_name(self, tmp_path):
        agents_dir = tmp_path / ".claude" / "agents"
        _write_agent(agents_dir, "implementer.md", _agent_md(name="implementer"))
        name_index, _ = _agent_file_index(str(tmp_path))
        assert [p.name for p in name_index["implementer"]] == ["implementer.md"]

    def test_gate_in_subfolder_indexed_by_name(self, tmp_path):
        agents_dir = tmp_path / ".claude" / "agents"
        _write_agent(agents_dir, "gates/north-star-auditor.md",
                     _agent_md(name="north-star-auditor", model="opus"))
        name_index, _ = _agent_file_index(str(tmp_path))
        assert len(name_index["north-star-auditor"]) == 1
        assert name_index["north-star-auditor"][0].name == "north-star-auditor.md"

    def test_stem_fallback_for_no_frontmatter(self, tmp_path):
        agents_dir = tmp_path / ".claude" / "agents"
        _write_agent(agents_dir, "custom.md", _agent_md(name=None))
        name_index, stem_index = _agent_file_index(str(tmp_path))
        assert name_index == {}
        assert [p.name for p in stem_index["custom"]] == ["custom.md"]

    def test_missing_agents_dir_returns_empty_indexes(self, tmp_path):
        name_index, stem_index = _agent_file_index(str(tmp_path))
        assert name_index == {}
        assert stem_index == {}

    def test_symlink_escaping_root_is_ignored(self, tmp_path):
        outside = tmp_path / "outside.md"
        outside.write_text(_agent_md(name="implementer"))
        agents_dir = tmp_path / ".claude" / "agents"
        agents_dir.mkdir(parents=True)
        link = agents_dir / "implementer.md"
        try:
            link.symlink_to(outside)
        except OSError:
            import pytest
            pytest.skip("symlinks not supported in this environment")
        name_index, stem_index = _agent_file_index(str(tmp_path))
        assert name_index == {}
        assert stem_index == {}

    def test_backup_file_skipped(self, tmp_path):
        agents_dir = tmp_path / ".claude" / "agents"
        agents_dir.mkdir(parents=True)
        # Backup files carry a non-.md suffix so rglob("*.md") already
        # excludes them; _is_agent_backup_name is a defensive second layer.
        (agents_dir / "implementer.md").write_text(_agent_md(name="implementer"))
        (agents_dir / "implementer.md.zprof.bak-20260101120000.md").write_text(
            _agent_md(name="implementer"))
        name_index, _ = _agent_file_index(str(tmp_path))
        assert len(name_index["implementer"]) == 1


# ---------------------------------------------------------------------------
# _resolve_agent_file / _agent_config_hash
# ---------------------------------------------------------------------------

class TestResolveAgentFile:
    def test_base_role_single_match(self, tmp_path):
        agents_dir = tmp_path / ".claude" / "agents"
        p = _write_agent(agents_dir, "implementer.md", _agent_md(name="implementer"))
        path, count = _resolve_agent_file(str(tmp_path), "implementer")
        assert path == p
        assert count == 1

    def test_gate_role_by_frontmatter_name(self, tmp_path):
        agents_dir = tmp_path / ".claude" / "agents"
        p = _write_agent(agents_dir, "gates/north-star-auditor.md",
                         _agent_md(name="north-star-auditor", model="opus"))
        path, count = _resolve_agent_file(str(tmp_path), "north-star-auditor")
        assert path == p
        assert count == 1

    def test_namespaced_file_with_matching_name(self, tmp_path):
        agents_dir = tmp_path / ".claude" / "agents"
        p = _write_agent(agents_dir, "implementer-ios.md", _agent_md(name="implementer-ios"))
        path, count = _resolve_agent_file(str(tmp_path), "implementer-ios")
        assert path == p
        assert count == 1

    def test_empty_agent_type_returns_none(self, tmp_path):
        path, count = _resolve_agent_file(str(tmp_path), "")
        assert path is None
        assert count == 0

    def test_unknown_agent_type_returns_none(self, tmp_path):
        agents_dir = tmp_path / ".claude" / "agents"
        _write_agent(agents_dir, "implementer.md", _agent_md(name="implementer"))
        path, count = _resolve_agent_file(str(tmp_path), "general-purpose")
        assert path is None
        assert count == 0

    def test_no_agents_dir_returns_none(self, tmp_path):
        path, count = _resolve_agent_file(str(tmp_path), "implementer")
        assert path is None
        assert count == 0

    def test_ambiguous_name_returns_none_with_count(self, tmp_path):
        """Two files sharing the same frontmatter `name:` -> None + count=2."""
        agents_dir = tmp_path / ".claude" / "agents"
        _write_agent(agents_dir, "implementer.md", _agent_md(name="implementer"))
        _write_agent(agents_dir, "implementer-ios.md", _agent_md(name="implementer"))
        path, count = _resolve_agent_file(str(tmp_path), "implementer")
        assert path is None
        assert count == 2

    def test_fallback_to_stem_when_no_frontmatter(self, tmp_path):
        agents_dir = tmp_path / ".claude" / "agents"
        p = _write_agent(agents_dir, "legacy.md", _agent_md(name=None))
        path, count = _resolve_agent_file(str(tmp_path), "legacy")
        assert path == p
        assert count == 1


class TestAgentConfigHash:
    def test_hash_is_12_hex_chars(self, tmp_path):
        agents_dir = tmp_path / ".claude" / "agents"
        _write_agent(agents_dir, "implementer.md", _agent_md(name="implementer"))
        h, count = _agent_config_hash(str(tmp_path), "implementer")
        assert _HEX12.match(h)
        assert count == 1

    def test_hash_stable_across_calls(self, tmp_path):
        agents_dir = tmp_path / ".claude" / "agents"
        _write_agent(agents_dir, "implementer.md", _agent_md(name="implementer"))
        h1, _ = _agent_config_hash(str(tmp_path), "implementer")
        h2, _ = _agent_config_hash(str(tmp_path), "implementer")
        assert h1 == h2

    def test_hash_changes_when_body_edited(self, tmp_path):
        agents_dir = tmp_path / ".claude" / "agents"
        f = _write_agent(agents_dir, "implementer.md",
                         _agent_md(name="implementer", body="Version 1.\n"))
        h1, _ = _agent_config_hash(str(tmp_path), "implementer")
        f.write_text(_agent_md(name="implementer", body="Version 2.\n"))
        h2, _ = _agent_config_hash(str(tmp_path), "implementer")
        assert h1 != h2

    def test_hash_changes_when_model_edited(self, tmp_path):
        """Model resolution lives in the agent file, so a model change must
        also change the hash (ADR: 'смена модели тоже меняет хеш')."""
        agents_dir = tmp_path / ".claude" / "agents"
        f = _write_agent(agents_dir, "implementer.md",
                         _agent_md(name="implementer", model="sonnet"))
        h1, _ = _agent_config_hash(str(tmp_path), "implementer")
        f.write_text(_agent_md(name="implementer", model="opus"))
        h2, _ = _agent_config_hash(str(tmp_path), "implementer")
        assert h1 != h2

    def test_missing_agent_type_returns_none_no_exception(self, tmp_path):
        h, count = _agent_config_hash(str(tmp_path), "")
        assert h is None
        assert count == 0

    def test_missing_agents_dir_returns_none(self, tmp_path):
        h, count = _agent_config_hash(str(tmp_path), "implementer")
        assert h is None
        assert count == 0

    def test_ambiguous_returns_none_with_candidate_count(self, tmp_path):
        agents_dir = tmp_path / ".claude" / "agents"
        _write_agent(agents_dir, "implementer.md", _agent_md(name="implementer"))
        _write_agent(agents_dir, "sub/implementer.md", _agent_md(name="implementer"))
        h, count = _agent_config_hash(str(tmp_path), "implementer")
        assert h is None
        assert count == 2

    def test_builtin_agent_type_returns_none(self, tmp_path):
        """Explore/general-purpose have no .claude/agents/*.md file at all."""
        h, count = _agent_config_hash(str(tmp_path), "Explore")
        assert h is None
        assert count == 0


# ---------------------------------------------------------------------------
# SubagentStop: pointer carries config_hash
# ---------------------------------------------------------------------------

class TestSubagentStopPointer:
    def test_pointer_has_config_hash_for_known_agent(self, tmp_path):
        agents_dir = tmp_path / ".claude" / "agents"
        _write_agent(agents_dir, "implementer.md", _agent_md(name="implementer"))
        expected_hash, _ = _agent_config_hash(str(tmp_path), "implementer")

        payload = {
            "session_id": "abc-123",
            "transcript_path": str(tmp_path / "abc-123.jsonl"),
            "cwd": str(tmp_path),
            "hook_event_name": "SubagentStop",
            "agent_id": "deadbeef01234567",
            "agent_type": "implementer",
            "agent_transcript_path": str(tmp_path / "abc-123" / "subagents" / "agent-deadbeef01234567.jsonl"),
            "background_tasks": [],
        }
        agentlog = tmp_path / ".agentlog"
        agentlog.mkdir()
        rc, state = run_collector("subagent-stop", payload, agentlog)
        assert rc == 0
        pointer = state["pointers"]["deadbeef01234567"]
        assert pointer["config_hash"] == expected_hash
        assert _HEX12.match(pointer["config_hash"])

    def test_pointer_config_hash_null_for_unresolvable_agent(self, tmp_path):
        payload = {
            "session_id": "abc-123",
            "transcript_path": str(tmp_path / "abc-123.jsonl"),
            "cwd": str(tmp_path),
            "hook_event_name": "SubagentStop",
            "agent_id": "deadbeef01234567",
            "agent_type": "general-purpose",
            "agent_transcript_path": "/fake/agent-deadbeef01234567.jsonl",
            "background_tasks": [],
        }
        agentlog = tmp_path / ".agentlog"
        agentlog.mkdir()
        rc, state = run_collector("subagent-stop", payload, agentlog)
        assert rc == 0
        pointer = state["pointers"]["deadbeef01234567"]
        assert pointer["config_hash"] is None

    def test_pointer_marks_ambiguous_candidates(self, tmp_path):
        agents_dir = tmp_path / ".claude" / "agents"
        _write_agent(agents_dir, "implementer.md", _agent_md(name="implementer"))
        _write_agent(agents_dir, "implementer-ios.md", _agent_md(name="implementer"))

        payload = {
            "session_id": "abc-123",
            "transcript_path": str(tmp_path / "abc-123.jsonl"),
            "cwd": str(tmp_path),
            "hook_event_name": "SubagentStop",
            "agent_id": "deadbeef01234567",
            "agent_type": "implementer",
            "agent_transcript_path": "/fake/agent-deadbeef01234567.jsonl",
            "background_tasks": [],
        }
        agentlog = tmp_path / ".agentlog"
        agentlog.mkdir()
        rc, state = run_collector("subagent-stop", payload, agentlog)
        assert rc == 0
        pointer = state["pointers"]["deadbeef01234567"]
        assert pointer["config_hash"] is None
        assert pointer["config_hash_candidates"] == 2


# ---------------------------------------------------------------------------
# _collect_subagent_transcripts: pointer consumption / fresh resolution
# ---------------------------------------------------------------------------

def _empty_sess():
    return {
        "main_log_offset": 0,
        "main_log_size": 0,
        "main_log_head_sha": "",
        "agents_done": [],
    }


def _make_meta_json(agent_type="implementer", tool_use_id="toolu_01ABC", spawn_depth=1):
    return {"agentType": agent_type, "toolUseId": tool_use_id, "spawnDepth": spawn_depth}


def _setup_subagents_dir(project_dir, agent_id, meta):
    """Create a minimal session dir with one meta.json (no transcript file)."""
    session_dir = project_dir / "session-id"
    transcript_path = project_dir / "session-id.jsonl"
    transcript_path.write_text(json.dumps({
        "type": "summary", "sessionId": "session-id",
        "timestamp": "2026-08-01T10:00:00Z", "version": "2.1.220",
    }) + "\n")
    subagents_dir = session_dir / "subagents"
    subagents_dir.mkdir(parents=True, exist_ok=True)
    (subagents_dir / f"agent-{agent_id}.meta.json").write_text(json.dumps(meta))
    agentlog = project_dir / ".agentlog"
    agentlog.mkdir(parents=True, exist_ok=True)
    return str(transcript_path), agentlog


class TestCollectSubagentTranscriptsConfigHash:
    def test_uses_pointer_hash_and_consumes_pointer(self, tmp_path):
        agent_id = "deadbeef01234567"
        tool_use_id = "toolu_01Pointer"
        meta = _make_meta_json(agent_type="implementer", tool_use_id=tool_use_id)
        tp, agentlog = _setup_subagents_dir(tmp_path, agent_id, meta)

        dispatches = [{
            "dispatch_id": tool_use_id, "session_id": "session-id",
            "role": "implementer", "status": "completed",
            "dispatch_complete": True, "seq": 1, "ts_utc": "2026-08-01T11:05:00Z",
        }]
        pointers = {agent_id: {"config_hash": "abc123abc123", "ts": "2026-01-01T00:00:00Z"}}

        _collect_subagent_transcripts(
            agentlog, "session-id", tp, set(), _empty_sess(), dispatches,
            pointers=pointers)

        d = dispatches[0]
        assert d["config_hash"] == "abc123abc123"
        assert d["ext"]["config_hash_source"] == "subagent-stop"
        assert agent_id not in pointers  # consumed

    def test_no_pointer_resolves_fresh_from_disk(self, tmp_path):
        agents_dir = tmp_path / ".claude" / "agents"
        _write_agent(agents_dir, "implementer.md", _agent_md(name="implementer"))
        expected_hash, _ = _agent_config_hash(str(tmp_path), "implementer")

        agent_id = "deadbeef01234567"
        tool_use_id = "toolu_01Fresh"
        meta = _make_meta_json(agent_type="implementer", tool_use_id=tool_use_id)
        tp, agentlog = _setup_subagents_dir(tmp_path, agent_id, meta)

        dispatches = [{
            "dispatch_id": tool_use_id, "session_id": "session-id",
            "role": "implementer", "status": "completed",
            "dispatch_complete": True, "seq": 1, "ts_utc": "2026-08-01T11:05:00Z",
        }]

        _collect_subagent_transcripts(
            agentlog, "session-id", tp, set(), _empty_sess(), dispatches)

        d = dispatches[0]
        assert d["config_hash"] == expected_hash
        assert d["ext"]["config_hash_source"] == "collect"

    def test_no_pointer_missing_agent_file_is_null(self, tmp_path):
        agent_id = "deadbeef01234567"
        tool_use_id = "toolu_01Missing"
        meta = _make_meta_json(agent_type="general-purpose", tool_use_id=tool_use_id)
        tp, agentlog = _setup_subagents_dir(tmp_path, agent_id, meta)

        dispatches = [{
            "dispatch_id": tool_use_id, "session_id": "session-id",
            "role": "general-purpose", "status": "completed",
            "dispatch_complete": True, "seq": 1, "ts_utc": "2026-08-01T11:05:00Z",
        }]

        _collect_subagent_transcripts(
            agentlog, "session-id", tp, set(), _empty_sess(), dispatches,
            pointers={})

        d = dispatches[0]
        assert d["config_hash"] is None
        assert d["ext"]["config_hash_source"] == "collect"

    def test_ambiguous_resolution_sets_ext_flag(self, tmp_path):
        agents_dir = tmp_path / ".claude" / "agents"
        _write_agent(agents_dir, "implementer.md", _agent_md(name="implementer"))
        _write_agent(agents_dir, "implementer-ios.md", _agent_md(name="implementer"))

        agent_id = "deadbeef01234567"
        tool_use_id = "toolu_01Ambiguous"
        meta = _make_meta_json(agent_type="implementer", tool_use_id=tool_use_id)
        tp, agentlog = _setup_subagents_dir(tmp_path, agent_id, meta)

        dispatches = [{
            "dispatch_id": tool_use_id, "session_id": "session-id",
            "role": "implementer", "status": "completed",
            "dispatch_complete": True, "seq": 1, "ts_utc": "2026-08-01T11:05:00Z",
        }]

        _collect_subagent_transcripts(
            agentlog, "session-id", tp, set(), _empty_sess(), dispatches,
            pointers={})

        d = dispatches[0]
        assert d["config_hash"] is None
        assert d["ext"]["config_hash_ambiguous"] == 2

    def test_pointer_ambiguous_flag_propagates_to_ext(self, tmp_path):
        agent_id = "deadbeef01234567"
        tool_use_id = "toolu_01PointerAmbiguous"
        meta = _make_meta_json(agent_type="implementer", tool_use_id=tool_use_id)
        tp, agentlog = _setup_subagents_dir(tmp_path, agent_id, meta)

        dispatches = [{
            "dispatch_id": tool_use_id, "session_id": "session-id",
            "role": "implementer", "status": "completed",
            "dispatch_complete": True, "seq": 1, "ts_utc": "2026-08-01T11:05:00Z",
        }]
        pointers = {agent_id: {"config_hash": None, "config_hash_candidates": 2,
                               "ts": "2026-01-01T00:00:00Z"}}

        _collect_subagent_transcripts(
            agentlog, "session-id", tp, set(), _empty_sess(), dispatches,
            pointers=pointers)

        d = dispatches[0]
        assert d["config_hash"] is None
        assert d["ext"]["config_hash_ambiguous"] == 2
        assert d["ext"]["config_hash_source"] == "subagent-stop"
