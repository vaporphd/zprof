"""Pattern lists load in both the source layout (telemetry.yaml next to the
script) and the deployed layout (.claude/zprof-collect.py + .agentlog/schema.json)."""
import importlib.util
import json
import pathlib
import shutil

_SRC = pathlib.Path(__file__).parent.parent
_SCRIPT = _SRC / "zprof-collect.py"


def _load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


src_mod = _load(_SCRIPT, "zprof_collect_src")
_KEYS = ("redaction_patterns", "mutating_bash_patterns")


def _schema_from_yaml():
    """Build schema.json content with the stdlib quoted-list parser (no yaml dependency)."""
    tel = _SRC / "telemetry.yaml"
    return {k: src_mod._parse_quoted_list_from_yaml(tel, k) for k in _KEYS}


def _deployed(tmp_path):
    (tmp_path / ".claude").mkdir()
    shutil.copy(_SCRIPT, tmp_path / ".claude" / "zprof-collect.py")
    (tmp_path / ".agentlog").mkdir()
    (tmp_path / ".agentlog" / "schema.json").write_text(json.dumps(_schema_from_yaml()))
    return _load(tmp_path / ".claude" / "zprof-collect.py", "zprof_collect_deployed")


def test_quoted_list_parser_reads_any_key():
    data = _schema_from_yaml()
    assert len(data["redaction_patterns"]) == 8
    # 10, not 8: issue #75 split the single `git ...` pattern into a
    # hyphen-suffix-safe alternation plus a dedicated `stash list`/`show`
    # exclusion, mirroring guard.yaml's own `stash_in_worktree` pattern.
    # Issue #73 then added one more dedicated pattern for standalone `ln`.
    assert len(data["mutating_bash_patterns"]) == 10
    assert "\\bsed\\s+-i\\b" in data["mutating_bash_patterns"]


def test_deployed_layout_falls_back_to_schema_json(tmp_path):
    mod = _deployed(tmp_path)
    assert not (tmp_path / ".claude" / "telemetry.yaml").exists()
    assert len(mod._load_redaction_patterns(str(tmp_path))) >= 8
    assert len(mod._load_pattern_list("mutating_bash_patterns", str(tmp_path))) == 10


def test_source_layout_counts_match(tmp_path):
    assert len(src_mod._load_redaction_patterns(str(tmp_path))) >= 8
    assert len(src_mod._load_pattern_list("mutating_bash_patterns", str(tmp_path))) == 10


def test_deployed_redaction_actually_redacts(tmp_path):
    mod = _deployed(tmp_path)
    pats = mod._load_redaction_patterns(str(tmp_path))
    out, n = mod._redact_secrets({"target": "export GITHUB_TOKEN=abc123"}, pats)
    assert n >= 1 and "abc123" not in out["target"]


def test_missing_everything_yields_empty_list(tmp_path):
    (tmp_path / ".claude").mkdir()
    shutil.copy(_SCRIPT, tmp_path / ".claude" / "zprof-collect.py")
    mod = _load(tmp_path / ".claude" / "zprof-collect.py", "zprof_collect_bare")
    assert mod._load_pattern_list("mutating_bash_patterns", str(tmp_path)) == []


def test_invalid_pattern_in_schema_json_is_skipped(tmp_path):
    mod = _deployed(tmp_path)
    data = _schema_from_yaml()
    data["mutating_bash_patterns"].append("([unclosed")
    (tmp_path / ".agentlog" / "schema.json").write_text(json.dumps(data))
    assert len(mod._load_pattern_list("mutating_bash_patterns", str(tmp_path))) == 10
