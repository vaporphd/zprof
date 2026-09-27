# Per-task Scorecard (Phase 1) Implementation Plan — Part A — коллектор: schema v2, C1–C4

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After every task-runner run, `zprof score` prints a 0–100 card with per-role breakdown, appends a row to `.agentlog/scores.jsonl` and a `## Score` section to the run log — computed deterministically from `.agentlog/`.

**Architecture:** The Python collector (`profiles/base/zprof-collect.py`, stdlib only) extracts new facts into `.agentlog/`: full rows for nested dispatches (C1), the `verdict` value and `ext.{next,artifact,run_log}` (C2), an ordered `tool-events.jsonl` with `is_error` (C3), `ext.run_id` (C4). A new Go package `cli/internal/score` reads `.agentlog/` only, groups dispatches into runs, computes penalties P1–P7, renders the card and persists it. `zprof score` wires it; the Stop hook runs it after the collector; `AGENT_LOOP.md` tells main to paste the card into `followup.md`.

**Tech Stack:** Go 1.22 (cobra, testify/require, yaml.v3, stdlib `regexp`/`crypto/sha1`), Python 3.10+ stdlib only (pytest for tests).

**Spec:** `docs/superpowers/specs/2026-09-26-task-scorecard-design.md`
## Место в плане

**Зависит от:** Ничего. Первая часть.
**Даёт дальше:** `.agentlog/dispatches.jsonl` с `verdict`, `ext.{next,artifact,run_log,run_id}` и полными строками вложенных dispatch'ей; новый `.agentlog/tool-events.jsonl`; `telemetry.yaml v2`. Всё это читает Part B1.
**Индекс всех частей:** `2026-09-26-task-scorecard-phase1.md`

## Global Constraints

- Python collector: **stdlib only**, `exit(0)` always, every new branch inside existing try/except → `collect.log`; `fcntl.flock` + `os.fsync()` on writes (spec §5, project CLAUDE.md).
- Go: `internal/` packages, table-driven tests with `testify/require`, error wrapping `fmt.Errorf("...: %w", err)`.
- Go `score` package reads **only** `.agentlog/` (`dispatches.jsonl`, `tool-events.jsonl`, `schema.json`) plus `.zprof.yaml`. No raw session JSONL parsing in Go (spec §4, §8).
- Boundary: **Python extracts facts, Go interprets.** No metric computed in Python (spec §4).
- Weights sum to 100: P1 20, P2 15, P3 10, P4 20, P5 10, P6 15, P7 10. Saturation: P1 0.20, P2 3, P3 0.5, P4 2, P5 2, P6 0.30, P7 4. Thresholds: Ideal ≥ 85, Solid 60–84, Lucky < 60 (spec §6).
- Tiers: verdict `done`/`approve*` → Ideal/Solid/Lucky by score; `blocked` → `Blocked`; `failed` → `Failed` (spec §6).
- `verdict_exempt_roles: [auditor, auditor-deep]` are excluded from P6-by-`return_parsed` and from P7 (spec §6, §9).
- `tool-events.jsonl` excludes `Agent`/`Task` tool calls (spec §5 C3). `input_hash` = sha1 of `json.dumps(input, sort_keys=True, separators=(",",":"), ensure_ascii=False)`, first 12 hex.
- `dispatch_id` in `tool-events.jsonl` uses the same composite form as `dispatches.jsonl`: `claude-code:<session>:<toolu_id>`.
- Commits: `feat(base):` for collector, `feat(cli):` for Go, `test(...)`, `docs:` (project CLAUDE.md).
- Prompt files (`agent-loop-router.md`): Russian text, English keys.

## Review Focus (этой части)

2. **`swift test` / `cargo test` repeated after an error with only a build in between.** Expected: counted (build/test commands are **not** mutating). → Task 1 amends the spec; Task 7 test `TestBlindRetries_BuildBetweenDoesNotReset`.
3. **Child agent finished while its parent task-runner is still running (async runner).** Expected: child is deferred, not written as a thin meta-only row that later blocks the full row via dedup. → Task 4 test `test_child_of_running_parent_is_deferred`.

---

## File Structure (этой части)

**Python (collector) — `profiles/base/`**
- Modify `zprof-collect.py`:
  - `_class_a_checks` → also returns `verdict_value`, `next_value`, `artifact_path`, `run_log` (C2).
  - `_normalize_dispatch` → maps them to `verdict`, `ext.next`, `ext.artifact`, `ext.run_log` (C2).
  - New `_extract_tool_events(jsonl_path) -> list[dict]`, `_write_tool_events(agentlog, dispatch_id, events, redaction_patterns)` (C3).
  - New `_extract_dispatches_from_text(session_id, raw, notify_seq, seen_notifications) -> dict` factored out of `_extract_main_log`; `_extract_main_log` calls it (C1 prerequisite).
  - `_collect_subagent_transcripts` → pass 1 nested extraction + `_merge_dispatch`, defer rule, tool-events write (C1, C3).
  - New `_assign_run_ids(dispatches)` called from `_normalize_and_write` (C4).
- Modify `telemetry.yaml` → `version: 2`, sections `tool_events`, `mutating_bash_patterns`, `verdict_exempt_roles`, `score_defaults`.
- Modify `telemetry_test.py` → parses the new sections.
- Tests: `tests/test_tool_events.py` (new), `tests/test_nested_dispatches.py` (new), `tests/test_normalization.py` (extend), `tests/test_e2e_score.py` (new, cross-language smoke).

**Docs**
- Amend `docs/superpowers/specs/2026-09-26-task-scorecard-design.md` §6 (mutating patterns exclude build/test; check-then-reset order).

---

---

### Task 1: Schema v2 + spec amendment (telemetry.yaml)

**Files:**
- Modify: `profiles/base/telemetry.yaml`
- Modify: `profiles/base/telemetry_test.py`
- Modify: `docs/superpowers/specs/2026-09-26-task-scorecard-design.md` (§6 «Мутирующее событие»)

**Interfaces:**
- Produces: `telemetry.yaml` sections consumed by Go `LoadConfig` via the deployed `.agentlog/schema.json` (Task 6): `score_defaults.{weights,saturation,thresholds}`, `mutating_bash_patterns` (list of regex strings), `verdict_exempt_roles` (list), `tool_events` (field list, documentation for the Python writer in Task 3).

- [ ] **Step 1: Amend the spec §6 (the build/test-as-mutating rule is wrong)**

In `docs/superpowers/specs/2026-09-26-task-scorecard-design.md`, replace the paragraph starting with `**Мутирующее событие** (для P2, P3):` with:

```markdown
**Мутирующее событие** (для P2, P3): `tool ∈ {Edit, Write, MultiEdit, NotebookEdit}` или `Bash`, чья команда матчит `mutating_bash_patterns` (` > `, ` >> `, `sed -i`, `tee`, `mv`/`cp`/`rm`/`touch`/`mkdir`, `git commit|checkout|stash|reset|apply|cherry-pick|merge|rebase`, `xcodegen`, форматтеры `cargo fmt`/`gofmt -w`/`swiftformat`). Список — в `telemetry.yaml`, расширяется без релиза Go.
Сборка и тесты (`swift test`, `cargo build`, `go test`, `pytest`, `make`) **не** мутирующие: «упал тест → пересобрал → запустил тот же тест» без правки — это и есть слепой повтор. Порядок обработки одного события: (1) проверить, не повтор ли оно ранее упавшего ключа; (2) если событие мутирующее — сбросить память ключей и прочитанных путей; (3) записать событие. Так `sed -i`, упавший дважды подряд, считается повтором и затем сбрасывает состояние.
```

- [ ] **Step 2: Write the failing schema test**

In `profiles/base/telemetry_test.py`, change `LIST_SECTIONS` and `test_schema`:

```python
LIST_SECTIONS = ("core_fields", "tool_events", "redaction_patterns",
                 "mutating_bash_patterns", "verdict_exempt_roles")
```

In `load_schema`, initialise all list sections:

```python
    schema = {s: [] for s in LIST_SECTIONS}
```

and make the flow-mapping / quoted-string matching generic:

```python
        if section in LIST_SECTIONS:
            if section in ("core_fields", "tool_events"):
                field_match = _FIELD_LINE.match(raw_line)
                if field_match:
                    schema[section].append(_parse_field_body(field_match.group("body")))
                    continue
            else:
                pattern_match = _PATTERN_LINE.match(raw_line)
                if pattern_match:
                    schema[section].append(_unescape_dq(pattern_match.group("body")))
                    continue
```

Replace the `version` assertion and append new checks at the end of `test_schema` (before the `print`):

```python
    assert schema.get("version") == 2, f"unexpected/missing top-level version: {schema.get('version')!r}"
```

```python
    # tool_events schema (spec §5 C3)
    te_names = [f["name"] for f in schema["tool_events"]]
    assert te_names == ["schema_version", "dispatch_id", "seq", "ts", "tool",
                        "input_hash", "target", "is_error", "result_chars"], te_names
    for f in schema["tool_events"]:
        assert f["type"] in valid_types, f"{f['name']}: unknown type {f['type']}"

    # mutating bash patterns compile and do NOT match build/test commands
    assert schema["mutating_bash_patterns"], "mutating_bash_patterns is empty"
    compiled = [re.compile(p) for p in schema["mutating_bash_patterns"]]
    for cmd in ("swift test --package-path Packages/Core", "cargo build --release",
                "go test ./...", "pytest -q", "make test", "git status", "cat foo.txt"):
        assert not any(c.search(cmd) for c in compiled), f"build/test/read command matched as mutating: {cmd}"
    for cmd in ("cat > f.txt <<'EOF'", "sed -i 's/a/b/' f", "git commit -m x",
                "rm -rf build", "xcodegen generate", "echo hi | tee out.log"):
        assert any(c.search(cmd) for c in compiled), f"mutating command not matched: {cmd}"

    # exempt roles
    assert schema["verdict_exempt_roles"] == ["auditor", "auditor-deep"]

    # score_defaults present (nested mapping — checked textually, parser is list-only)
    text = SCHEMA_PATH.read_text()
    for key in ("score_defaults:", "weights:", "saturation:", "thresholds:"):
        assert key in text, f"missing {key} in telemetry.yaml"
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `python3 -m pytest profiles/base/telemetry_test.py -v`
Expected: FAIL — `unexpected/missing top-level version: 1`.

- [ ] **Step 4: Update `telemetry.yaml`**

Change `version: 1` → `version: 2`. Change the `verdict` field comment line to:

```yaml
  - {name: verdict,             type: string, required: false} # first `verdict:` value from the return text (collector ≥ 0.2)
```

Append after `redaction_patterns:` list (end of file):

```yaml

# --- tool-events.jsonl (collector ≥ 0.2, spec 2026-09-26-task-scorecard §5 C3) ---
# One row per leaf tool call inside a subagent transcript. Agent/Task calls are
# dispatches and live in dispatches.jsonl instead.
tool_events:
  - {name: schema_version,      type: int,    required: true}
  - {name: dispatch_id,         type: string, required: true}   # same composite id as dispatches.jsonl
  - {name: seq,                 type: int,    required: true}   # order inside the dispatch, from 1
  - {name: ts,                  type: string, required: false}  # timestamp of the tool_use record
  - {name: tool,                type: string, required: true}   # Read | Edit | Bash | Grep | ...
  - {name: input_hash,          type: string, required: true}   # sha1(canonical json input)[:12]
  - {name: target,              type: string, required: false}  # file_path | command[:60] | pattern, redacted
  - {name: is_error,            type: bool,   required: false}  # null when the tool_result never arrived
  - {name: result_chars,        type: int,    required: false}

# Bash commands that change files/state. Used by Go `zprof score` to decide
# whether a repeated failing command is a blind retry (P2) and whether a
# re-read (P3) is legitimate. Build/test/read commands are deliberately absent.
mutating_bash_patterns:
  - "\\s>>?\\s"
  - "\\bsed\\s+-i\\b"
  - "\\btee\\b"
  - "\\b(mv|cp|rm|touch|mkdir)\\b"
  - "\\bgit\\s+(commit|checkout|stash|reset|apply|cherry-pick|merge|rebase)\\b"
  - "\\bxcodegen\\b"
  - "\\b(cargo|go|swift)\\s+fmt\\b"
  - "\\b(gofmt\\s+-w|swiftformat|rustfmt)\\b"

# Roles whose contract does not start with `verdict:` (they return `completion:`).
# Excluded from P6-by-return_parsed and from P7.
verdict_exempt_roles:
  - "auditor"
  - "auditor-deep"

# Defaults for `zprof score`; `.zprof.yaml: score:` overrides any key.
score_defaults:
  weights:    {P1: 20, P2: 15, P3: 10, P4: 20, P5: 10, P6: 15, P7: 10}
  saturation: {P1: 0.20, P2: 3, P3: 0.5, P4: 2, P5: 2, P6: 0.30, P7: 4}
  thresholds: {ideal: 85, solid: 60}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `python3 -m pytest profiles/base/telemetry_test.py -v`
Expected: PASS, output `OK: 34 fields, 8 redaction patterns`.

- [ ] **Step 6: Verify the existing Go apply still converts the file**

Run: `cd cli && go test ./internal/apply/ -run Collector -count=1`
Expected: PASS (yaml.v3 handles nested `score_defaults`).

- [ ] **Step 7: Commit**

```bash
git add profiles/base/telemetry.yaml profiles/base/telemetry_test.py docs/superpowers/specs/2026-09-26-task-scorecard-design.md
git commit -m "feat(base): telemetry schema v2 — tool_events, mutating patterns, score defaults"
```

---

### Task 2: C2 — persist `verdict`, `ext.next`, `ext.artifact`, `ext.run_log`

**Files:**
- Modify: `profiles/base/zprof-collect.py` (`_class_a_checks` ≈ line 1092, `_normalize_dispatch` ≈ line 1170)
- Test: `profiles/base/tests/test_normalization.py`

**Interfaces:**
- Consumes: return text in `raw["returned"]` (already populated by main-log and transcript extraction).
- Produces: normalized row fields `verdict: str`, `ext.next: str`, `ext.artifact: str`, `ext.run_log: str` (each only when the corresponding line exists). Task 5 reads `ext.run_log` to seed run identity; Go Task 6 reads `Verdict` and `Ext["run_log"]`.

- [ ] **Step 1: Write the failing tests**

Append to `profiles/base/tests/test_normalization.py`:

```python
def _norm(raw):
    norm, _ = _normalize_dispatch(
        raw, session_id="s1", harness_version="2.1.220", machine_id="m",
        project_id="p", project_id_provisional=False, redaction_patterns=[],
        known_roles=frozenset({"reviewer", "implementer"}),
    )
    return norm


def test_verdict_value_persisted():
    raw = {"dispatch_id": "toolu_1", "role": "implementer", "status": "completed",
           "returned": "verdict: done\nartifact: commit abc\nnext: reviewer\none_line: ok"}
    norm = _norm(raw)
    assert norm["verdict"] == "done"
    assert norm["ext"]["next"] == "reviewer"
    assert norm["ext"]["artifact"] == "commit abc"
    assert "run_log" not in norm["ext"]


def test_run_log_persisted_for_task_runner():
    raw = {"dispatch_id": "toolu_r", "role": "task-runner", "status": "completed",
           "returned": "verdict: blocked\nartifact: none\nrun_log: .zprof/runs/2026-09-26-x.md\none_line: q\nquestion: which?"}
    norm = _norm(raw)
    assert norm["verdict"] == "blocked"
    assert norm["ext"]["run_log"] == ".zprof/runs/2026-09-26-x.md"


def test_verdict_absent_when_no_verdict_line():
    raw = {"dispatch_id": "toolu_a", "role": "auditor", "status": "completed",
           "returned": "completion: complete\nintegrity: clean\nevidence: x.md"}
    norm = _norm(raw)
    assert "verdict" not in norm
    assert norm["return_parsed"] is False
    assert norm.get("ext") in (None, {})


def test_verdict_value_is_lowercased_first_token():
    raw = {"dispatch_id": "toolu_b", "role": "reviewer", "status": "completed",
           "returned": "verdict: Approve-With-Fixes   \nartifact: docs/reviews/r.md"}
    norm = _norm(raw)
    assert norm["verdict"] == "approve-with-fixes"
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `python3 -m pytest profiles/base/tests/test_normalization.py -k "verdict or run_log" -v`
Expected: FAIL — `KeyError: 'verdict'` / `TypeError: 'NoneType' object is not subscriptable` (ext is None today).

- [ ] **Step 3: Extend `_class_a_checks`**

In `_class_a_checks`, extend the initial `result` dict and capture values. Replace the function body's `result = {...}` with:

```python
    result = {
        "has_preamble": None,
        "return_parsed": None,
        "artifact_exists": None,
        "next_is_reachable": None,
        # C2 — raw values, popped by _normalize_dispatch before norm.update()
        "verdict_value": None,
        "next_value": None,
        "artifact_path": None,
        "run_log": None,
    }
```

Right after `verdict_idx` is determined (after the `for i, line in enumerate(lines)` loop), add:

```python
    if verdict_idx is not None:
        raw_v = lines[verdict_idx].strip().split(":", 1)[1].strip()
        # first whitespace-delimited token, lowercased: "Approve-With-Fixes   " -> "approve-with-fixes"
        result["verdict_value"] = raw_v.split()[0].lower() if raw_v else None
```

In the `artifact:` loop, after `artifact_path = ...` add `result["artifact_path"] = artifact_path or None`. In the `next:` loop, after `next_val = ...` add `result["next_value"] = next_val or None`. Add a third loop for `run_log:`:

```python
    for line in lines:
        stripped = line.strip()
        if stripped.lower().startswith("run_log:"):
            rl = stripped.split(":", 1)[1].strip()
            result["run_log"] = rl or None
            break
```

- [ ] **Step 4: Map the values in `_normalize_dispatch`**

Replace the two lines

```python
    checks = _class_a_checks(returned, cwd, known_roles=known_roles)
    norm.update(checks)
```

with:

```python
    checks = _class_a_checks(returned, cwd, known_roles=known_roles)
    verdict_value = checks.pop("verdict_value", None)
    next_value = checks.pop("next_value", None)
    artifact_path = checks.pop("artifact_path", None)
    run_log = checks.pop("run_log", None)
    norm.update(checks)
    if verdict_value and not norm.get("verdict"):
        norm["verdict"] = verdict_value
    if next_value or artifact_path or run_log:
        ext = dict(norm.get("ext") or {})
        if next_value:
            ext["next"] = next_value
        if artifact_path:
            ext["artifact"] = artifact_path
        if run_log:
            ext["run_log"] = run_log
        norm["ext"] = ext
```

Note: `norm["verdict"] = raw.get("verdict")` earlier stays; `verdict_value` only fills when raw had none.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `python3 -m pytest profiles/base/tests/test_normalization.py -v`
Expected: all PASS, including the pre-existing Class-A tests (the popped keys never reach `norm`).

- [ ] **Step 6: Run the whole Python suite**

Run: `python3 -m pytest profiles/base/tests/ -q`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add profiles/base/zprof-collect.py profiles/base/tests/test_normalization.py
git commit -m "feat(base): persist verdict value and ext.next/artifact/run_log from return text"
```

---

### Task 3: C3 — `tool-events.jsonl` from subagent transcripts

**Files:**
- Modify: `profiles/base/zprof-collect.py` (new helpers before `_gzip_copy` ≈ line 727; call site inside `_collect_subagent_transcripts` ≈ line 800)
- Test: `profiles/base/tests/test_tool_events.py` (new)

**Interfaces:**
- Consumes: subagent transcript JSONL (`message.content[]` with `tool_use` / `tool_result` blocks), `_make_composite_id(session_id, raw_id)`, `_redact_secrets(value, patterns)`, `_load_redaction_patterns(cwd)`.
- Produces: `_extract_tool_events(jsonl_path: Path) -> list[dict]` (keys `seq, ts, tool, input_hash, target, is_error, result_chars`), `_write_tool_events(agentlog: Path, dispatch_id: str, events: list[dict], redaction_patterns) -> int`, and the file `.agentlog/tool-events.jsonl` (one row per event, `schema_version: 1`, `dispatch_id` composite). Go Task 6 reads it.

- [ ] **Step 1: Write the failing tests**

Create `profiles/base/tests/test_tool_events.py`:

```python
#!/usr/bin/env python3
"""Tests for tool-events extraction (spec §5 C3)."""
import json, pathlib, sys
import pytest

sys.path.insert(0, str(pathlib.Path(__file__).parent.parent))
import importlib
_mod_path = pathlib.Path(__file__).parent.parent / "zprof-collect.py"
_spec = importlib.util.spec_from_file_location("zprof_collect", _mod_path)
zprof_collect = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(zprof_collect)

_extract_tool_events = zprof_collect._extract_tool_events
_write_tool_events = zprof_collect._write_tool_events
_input_hash = zprof_collect._input_hash
_collect_subagent_transcripts = zprof_collect._collect_subagent_transcripts


def _assistant(ts, *blocks):
    return json.dumps({"type": "assistant", "timestamp": ts,
                       "message": {"role": "assistant", "model": "claude-sonnet-5",
                                   "content": list(blocks),
                                   "usage": {"input_tokens": 10, "output_tokens": 5,
                                             "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0}}})


def _user_result(ts, tool_use_id, content, is_error=False):
    block = {"type": "tool_result", "tool_use_id": tool_use_id, "content": content}
    if is_error:
        block["is_error"] = True
    return json.dumps({"type": "user", "timestamp": ts,
                       "message": {"role": "user", "content": [block]}})


def _write_transcript(path, lines):
    path.write_text("\n".join(lines) + "\n")


def test_extract_tool_events_basic(tmp_path):
    t = tmp_path / "agent-abc.jsonl"
    _write_transcript(t, [
        _assistant("2026-09-26T10:00:01Z",
                   {"type": "tool_use", "id": "tu1", "name": "Read", "input": {"file_path": "/p/a.swift"}}),
        _user_result("2026-09-26T10:00:02Z", "tu1", "contents"),
        _assistant("2026-09-26T10:00:03Z",
                   {"type": "tool_use", "id": "tu2", "name": "Bash", "input": {"command": "swift test --package-path Packages/Core"}}),
        _user_result("2026-09-26T10:00:09Z", "tu2", "error: build failed", is_error=True),
        _assistant("2026-09-26T10:00:10Z",
                   {"type": "tool_use", "id": "tu3", "name": "Agent", "input": {"subagent_type": "tester", "prompt": "x"}}),
        _user_result("2026-09-26T10:00:20Z", "tu3", "verdict: done"),
        _assistant("2026-09-26T10:00:21Z",
                   {"type": "tool_use", "id": "tu4", "name": "Grep", "input": {"pattern": "TODO", "path": "/p"}}),
        # tu4 never gets a tool_result (truncated transcript)
    ])
    events = _extract_tool_events(t)
    assert [e["tool"] for e in events] == ["Read", "Bash", "Grep"], "Agent call must be excluded"
    assert [e["seq"] for e in events] == [1, 2, 3]
    assert events[0]["target"] == "/p/a.swift" and events[0]["is_error"] is False
    assert events[0]["result_chars"] == len("contents")
    assert events[1]["target"] == "swift test --package-path Packages/Core"
    assert events[1]["is_error"] is True
    assert events[2]["is_error"] is None and events[2]["result_chars"] is None
    assert events[2]["target"] == "/p", "path wins over pattern for target"
    assert events[0]["ts"] == "2026-09-26T10:00:01Z"


def test_input_hash_is_canonical_and_stable():
    a = _input_hash({"command": "swift test", "timeout": 5})
    b = _input_hash({"timeout": 5, "command": "swift test"})
    assert a == b and len(a) == 12
    assert _input_hash({"command": "swift build"}) != a


def test_target_truncates_long_commands_and_flattens_newlines():
    t = pathlib.Path(__file__).parent / "_tmp_cmd.jsonl"
    try:
        _write_transcript(t, [_assistant("ts", {"type": "tool_use", "id": "x", "name": "Bash",
                                               "input": {"command": "cat > f.txt <<'EOF'\n" + "y" * 100 + "\nEOF"}})])
        ev = _extract_tool_events(t)[0]
        assert "\n" not in ev["target"] and len(ev["target"]) == 60
    finally:
        t.unlink(missing_ok=True)


def test_write_tool_events_rows_and_redaction(tmp_path):
    agentlog = tmp_path / ".agentlog"
    agentlog.mkdir()
    import re
    pats = [("sk-", re.compile(r"sk-[a-zA-Z0-9]{20,}"))]
    events = [{"seq": 1, "ts": "t", "tool": "Bash", "input_hash": "abc", "target": "curl -H sk-AAAAAAAAAAAAAAAAAAAAAAAA", "is_error": False, "result_chars": 3},
              {"seq": 2, "ts": "t", "tool": "Read", "input_hash": "def", "target": "/p", "is_error": None, "result_chars": None}]
    n = _write_tool_events(agentlog, "claude-code:s1:tu9", events, pats)
    assert n == 2
    rows = [json.loads(l) for l in (agentlog / "tool-events.jsonl").read_text().splitlines()]
    assert rows[0]["dispatch_id"] == "claude-code:s1:tu9" and rows[0]["schema_version"] == 1
    assert "sk-AAAA" not in rows[0]["target"] and "redacted" in rows[0]["target"]
    assert "is_error" not in rows[1] and "result_chars" not in rows[1], "None values are dropped"


def test_collect_writes_tool_events_with_composite_id(tmp_path):
    session_id = "sess-te"
    main = tmp_path / f"{session_id}.jsonl"
    main.write_text("")
    sub = tmp_path / session_id / "subagents"
    sub.mkdir(parents=True)
    (sub / "agent-a1.meta.json").write_text(json.dumps({"agentType": "implementer", "toolUseId": "toolu_X", "spawnDepth": 1}))
    _write_transcript(sub / "agent-a1.jsonl", [
        _assistant("2026-09-26T10:00:01Z", {"type": "tool_use", "id": "tu1", "name": "Edit",
                                             "input": {"file_path": "/p/a.swift", "old_string": "a", "new_string": "b"}}),
        _user_result("2026-09-26T10:00:02Z", "tu1", "ok"),
        _assistant("2026-09-26T10:00:03Z", {"type": "text", "text": "verdict: done\nartifact: abc"}),
    ])
    agentlog = tmp_path / ".agentlog"
    agentlog.mkdir()
    sess = {"main_log_offset": 0, "main_log_size": 0, "main_log_head_sha": "", "agents_done": []}
    dispatches = [{"dispatch_id": "toolu_X", "session_id": session_id, "role": "implementer",
                   "status": "completed", "dispatch_complete": True, "seq": 0, "ts_utc": "t"}]
    _collect_subagent_transcripts(agentlog, session_id, str(main), set(), sess, dispatches)
    rows = [json.loads(l) for l in (agentlog / "tool-events.jsonl").read_text().splitlines()]
    assert len(rows) == 1
    assert rows[0]["dispatch_id"] == f"claude-code:{session_id}:toolu_X"
    assert rows[0]["tool"] == "Edit" and rows[0]["target"] == "/p/a.swift"
    # second pass: agent already done → no duplicate rows
    _collect_subagent_transcripts(agentlog, session_id, str(main), set(), sess, dispatches)
    assert len((agentlog / "tool-events.jsonl").read_text().splitlines()) == 1
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `python3 -m pytest profiles/base/tests/test_tool_events.py -v`
Expected: FAIL — `AttributeError: module 'zprof_collect' has no attribute '_extract_tool_events'`.

- [ ] **Step 3: Add the helpers**

Insert before `def _gzip_copy(` in `zprof-collect.py`:

```python
# Tools that are dispatches, not leaf tool calls — they live in dispatches.jsonl.
_DISPATCH_TOOLS = frozenset({"Agent", "Task"})

_TARGET_MAX = 60


def _input_hash(inp) -> str:
    """sha1 of the canonical JSON of a tool input, first 12 hex chars."""
    try:
        canon = json.dumps(inp, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
    except (TypeError, ValueError):
        canon = repr(inp)
    return hashlib.sha1(canon.encode("utf-8")).hexdigest()[:12]


def _tool_target(inp) -> str:
    """Human-readable target of a tool call: file path, command head, or pattern."""
    if not isinstance(inp, dict):
        return ""
    for key in ("file_path", "path", "notebook_path"):
        v = inp.get(key)
        if isinstance(v, str) and v:
            return v
    cmd = inp.get("command")
    if isinstance(cmd, str) and cmd:
        return " ".join(cmd.split())[:_TARGET_MAX]
    pat = inp.get("pattern")
    if isinstance(pat, str) and pat:
        return pat[:_TARGET_MAX]
    return ""


def _extract_tool_events(jsonl_path: Path) -> list[dict]:
    """Ordered leaf tool calls of one subagent transcript (spec §5 C3).

    Each event: seq (1-based, transcript order), ts, tool, input_hash, target,
    is_error (None when no tool_result arrived), result_chars.
    Agent/Task calls are skipped — they are dispatches.
    """
    events: list[dict] = []
    by_id: dict[str, dict] = {}
    if not jsonl_path.exists():
        return events
    try:
        raw = jsonl_path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return events

    seq = 0
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
        content = msg.get("content", [])
        if not isinstance(content, list):
            continue
        role = msg.get("role", "")
        for item in content:
            if not isinstance(item, dict):
                continue
            itype = item.get("type")
            if role == "assistant" and itype == "tool_use":
                name = item.get("name", "")
                if not name or name in _DISPATCH_TOOLS:
                    continue
                seq += 1
                inp = item.get("input", {})
                ev = {
                    "seq": seq,
                    "ts": record.get("timestamp", ""),
                    "tool": name,
                    "input_hash": _input_hash(inp),
                    "target": _tool_target(inp),
                    "is_error": None,
                    "result_chars": None,
                }
                events.append(ev)
                tid = item.get("id", "")
                if tid:
                    by_id[tid] = ev
            elif role == "user" and itype == "tool_result":
                ev = by_id.get(item.get("tool_use_id", ""))
                if ev is None:
                    continue
                ev["is_error"] = bool(item.get("is_error", False))
                rc = item.get("content", "")
                if isinstance(rc, str):
                    ev["result_chars"] = len(rc)
                elif isinstance(rc, list):
                    ev["result_chars"] = sum(
                        len(b.get("text", "")) for b in rc if isinstance(b, dict))
    return events


def _write_tool_events(agentlog: Path, dispatch_id: str, events: list[dict],
                       redaction_patterns) -> int:
    """Append events for one dispatch to .agentlog/tool-events.jsonl. Returns rows written."""
    if not events or not dispatch_id:
        return 0
    path = agentlog / "tool-events.jsonl"
    written = 0
    with open(path, "a") as f:
        for ev in events:
            row = {"schema_version": 1, "dispatch_id": dispatch_id}
            row.update(ev)
            row = {k: v for k, v in row.items() if v is not None}
            row, _ = _redact_secrets(row, redaction_patterns)
            f.write(json.dumps(row, ensure_ascii=False) + "\n")
            written += 1
        f.flush()
        os.fsync(f.fileno())
    return written
```

- [ ] **Step 4: Call the helpers from `_collect_subagent_transcripts`**

Change the signature to accept optional patterns:

```python
def _collect_subagent_transcripts(
    agentlog: Path,
    session_id: str,
    transcript_path: str,
    running_agents: set,
    sess: dict,
    dispatches: list[dict],
    redaction_patterns=None,
):
```

At the top of the body add:

```python
    if redaction_patterns is None:
        # agentlog is <cwd>/.agentlog — the project dir is its parent
        redaction_patterns = _load_redaction_patterns(str(agentlog.parent))
```

Inside the per-meta loop, right after the block that gzip-copies the transcript (after `transcript_ref = f"transcripts/{gz_name}"` / its `except`), add:

```python
            # C3: leaf tool calls of this agent → tool-events.jsonl
            if transcript_file.exists():
                try:
                    events = _extract_tool_events(transcript_file)
                    composite = _make_composite_id(session_id, tool_use_id or f"meta:{agent_id}")
                    _write_tool_events(agentlog, composite, events, redaction_patterns)
                except Exception:
                    _log_error(agentlog, f"tool-events failed for agent {agent_id}: {traceback.format_exc()}")
```

(`_make_composite_id` is defined later in the file; Python resolves it at call time, no reordering needed.)

- [ ] **Step 5: Run the tests to verify they pass**

Run: `python3 -m pytest profiles/base/tests/test_tool_events.py profiles/base/tests/test_subagent_transcripts.py -v`
Expected: PASS (existing transcript tests still pass — the call site only adds a file).

- [ ] **Step 6: Commit**

```bash
git add profiles/base/zprof-collect.py profiles/base/tests/test_tool_events.py
git commit -m "feat(base): write .agentlog/tool-events.jsonl from subagent transcripts"
```

---

### Task 4: C1 — full rows for nested dispatches (children of task-runner)

**Files:**
- Modify: `profiles/base/zprof-collect.py` (`_extract_main_log` ≈ line 400–630, `_collect_subagent_transcripts` ≈ line 760–870)
- Test: `profiles/base/tests/test_nested_dispatches.py` (new); existing `test_main_log_extraction.py`, `test_subagent_transcripts.py` must stay green.

**Interfaces:**
- Consumes: `_extract_tool_uses_from_assistant`, `_parse_task_notification_xml`, `_IN_FLIGHT_STATUSES`.
- Produces: `_extract_dispatches_from_text(session_id: str, raw: str, notify_seq: dict, seen_notifications: set) -> dict` with keys `dispatches: list[dict]`, `unparsed_lines: int`, `truncated: bool`, `harness_version: str`; `_merge_dispatch(existing: dict, fresh: dict) -> None`. Nested child rows carry `parent_dispatch_id` (raw toolUseId of the parent), `spawn_depth`, `status`, `ts_utc`, `tool_uses`, `duration_ms`, tokens.

- [ ] **Step 1: Write the failing tests**

Create `profiles/base/tests/test_nested_dispatches.py`:

```python
#!/usr/bin/env python3
"""Tests for nested dispatch extraction (spec §5 C1): children of task-runner get full rows."""
import json, pathlib, sys
import pytest

sys.path.insert(0, str(pathlib.Path(__file__).parent.parent))
import importlib
_mod_path = pathlib.Path(__file__).parent.parent / "zprof-collect.py"
_spec = importlib.util.spec_from_file_location("zprof_collect", _mod_path)
zprof_collect = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(zprof_collect)

_extract_dispatches_from_text = zprof_collect._extract_dispatches_from_text
_collect_subagent_transcripts = zprof_collect._collect_subagent_transcripts
_merge_dispatch = zprof_collect._merge_dispatch


def _usage_turn(ts, model="claude-sonnet-5", inp=1000, out=100):
    return json.dumps({"type": "assistant", "timestamp": ts,
                       "message": {"role": "assistant", "model": model,
                                   "content": [{"type": "text", "text": "working"}],
                                   "usage": {"input_tokens": inp, "output_tokens": out,
                                             "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0}}})


def _agent_call(ts, tool_use_id, subagent_type):
    return json.dumps({"type": "assistant", "timestamp": ts,
                       "message": {"role": "assistant", "model": "claude-sonnet-5",
                                   "content": [{"type": "tool_use", "id": tool_use_id, "name": "Agent",
                                                "input": {"description": "d", "subagent_type": subagent_type,
                                                          "run_in_background": False, "prompt": "p"}}]}})


def _agent_result(ts, tool_use_id, agent_type, agent_id, status="completed",
                  tool_uses=5, duration=1500, returned="verdict: done\nartifact: c1"):
    return json.dumps({"type": "user", "timestamp": ts,
                       "message": {"role": "user",
                                   "content": [{"type": "tool_result", "tool_use_id": tool_use_id,
                                                "content": [{"type": "text", "text": returned}]}]},
                       "toolUseResult": {"status": status, "agentId": agent_id, "agentType": agent_type,
                                         "resolvedModel": "claude-sonnet-5", "totalDurationMs": duration,
                                         "totalTokens": 3000, "totalToolUseCount": tool_uses,
                                         "usage": {"input_tokens": 2500, "output_tokens": 500,
                                                   "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0}}})


def _runner_tree(tmp_path, session_id="sess-nested", runner_id="zzzz01", child_id="aaaa01"):
    """Main log with one task-runner dispatch; runner transcript dispatches one implementer child."""
    main = tmp_path / f"{session_id}.jsonl"
    main.write_text("\n".join([
        _agent_call("2026-09-26T10:00:00Z", "toolu_R", "task-runner"),
        _agent_result("2026-09-26T10:30:00Z", "toolu_R", "task-runner", runner_id, tool_uses=3, duration=1800000,
                      returned="verdict: done\nartifact: PR #1\nrun_log: .zprof/runs/2026-09-26-x.md\none_line: ok"),
    ]) + "\n")
    sub = tmp_path / session_id / "subagents"
    sub.mkdir(parents=True)
    (sub / f"agent-{runner_id}.meta.json").write_text(json.dumps(
        {"agentType": "task-runner", "toolUseId": "toolu_R", "spawnDepth": 1}))
    (sub / f"agent-{runner_id}.jsonl").write_text("\n".join([
        _usage_turn("2026-09-26T10:00:01Z"),
        _agent_call("2026-09-26T10:01:00Z", "toolu_C", "implementer"),
        _agent_result("2026-09-26T10:05:00Z", "toolu_C", "implementer", child_id),
        _usage_turn("2026-09-26T10:06:00Z"),
    ]) + "\n")
    (sub / f"agent-{child_id}.meta.json").write_text(json.dumps(
        {"agentType": "implementer", "toolUseId": "toolu_C", "parentAgentId": runner_id, "spawnDepth": 2}))
    (sub / f"agent-{child_id}.jsonl").write_text("\n".join([
        _usage_turn("2026-09-26T10:02:00Z", inp=7000, out=700),
        _usage_turn("2026-09-26T10:04:00Z", inp=8000, out=800),
    ]) + "\n")
    return main, sub


def _sess():
    return {"main_log_offset": 0, "main_log_size": 0, "main_log_head_sha": "", "agents_done": []}


def _run(tmp_path, running=frozenset(), **kw):
    main, _ = _runner_tree(tmp_path, **kw)
    session_id = main.stem
    sess = _sess()
    dispatches, _meta = zprof_collect._extract_main_log(session_id, main, sess)
    agentlog = tmp_path / ".agentlog"
    agentlog.mkdir(exist_ok=True)
    _collect_subagent_transcripts(agentlog, session_id, str(main), set(running), sess, dispatches)
    return {d["dispatch_id"]: d for d in dispatches}, sess


def test_extract_dispatches_from_text_matches_main_log_shape():
    raw = "\n".join([_agent_call("t1", "toolu_A", "tester"),
                     _agent_result("t2", "toolu_A", "tester", "ag1", status="completed", tool_uses=2, duration=99)])
    out = _extract_dispatches_from_text("s", raw, {}, set())
    assert out["truncated"] is False and out["unparsed_lines"] == 0
    d = out["dispatches"][0]
    assert d["dispatch_id"] == "toolu_A" and d["role"] == "tester"
    assert d["status"] == "completed" and d["tool_uses"] == 2 and d["duration_ms"] == 99
    assert d["ts_utc"] == "t2" and d["returned"].startswith("verdict: done")


def test_nested_children_get_full_rows(tmp_path):
    rows, sess = _run(tmp_path)
    child = rows["toolu_C"]
    assert child["role"] == "implementer"
    assert child["status"] == "completed" and child["dispatch_complete"] is True
    assert child["tool_uses"] == 5 and child["duration_ms"] == 1500
    assert child["ts_utc"] == "2026-09-26T10:05:00Z"
    assert child["parent_dispatch_id"] == "toolu_R" and child["spawn_depth"] == 2
    assert child["transcript_captured"] is True
    # tokens come from the child's own transcript (7000+8000), not from toolUseResult (2500)
    assert child["tokens_input"] == 15000 and child["tokens_output"] == 1500
    assert "aaaa01" in sess["agents_done"] and "zzzz01" in sess["agents_done"]


def test_child_sorted_before_parent_still_gets_full_row(tmp_path):
    # agent ids chosen so the child's meta.json sorts BEFORE the runner's
    rows, _ = _run(tmp_path, runner_id="zzzz09", child_id="aaaa09")
    child = rows["toolu_C"]
    assert child["status"] == "completed" and child["tool_uses"] == 5 and child["ts_utc"] != ""


def test_child_of_running_parent_is_deferred(tmp_path):
    rows, sess = _run(tmp_path, running={"zzzz01"})
    assert "toolu_C" not in rows, "child must wait until its parent finishes"
    assert "aaaa01" not in sess["agents_done"]
    assert "zzzz01" not in sess["agents_done"]


def test_merge_dispatch_prefers_fresh_status_but_keeps_transcript_tokens():
    existing = {"dispatch_id": "x", "status": "unknown", "dispatch_complete": False, "ts_utc": "",
                "tokens_input": 15000, "tokens_output": 1500}
    fresh = {"dispatch_id": "x", "status": "completed", "dispatch_complete": True, "ts_utc": "t9",
             "tool_uses": 4, "duration_ms": 10, "tokens_input": 2500, "tokens_output": 500}
    _merge_dispatch(existing, fresh)
    assert existing["status"] == "completed" and existing["dispatch_complete"] is True
    assert existing["ts_utc"] == "t9" and existing["tool_uses"] == 4
    assert existing["tokens_input"] == 15000, "transcript tokens are more accurate than toolUseResult"
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `python3 -m pytest profiles/base/tests/test_nested_dispatches.py -v`
Expected: FAIL — `AttributeError: ... has no attribute '_extract_dispatches_from_text'`.

- [ ] **Step 3: Factor the per-line loop out of `_extract_main_log`**

In `_extract_main_log`, everything from `# Track pending tool_use dispatches from assistant messages` down to (but excluding) `# Update meta` moves into a new function placed directly above `_extract_main_log`:

```python
def _extract_dispatches_from_text(session_id: str, raw: str, notify_seq: dict,
                                  seen_notifications: set) -> dict:
    """Extract dispatch records from JSONL text (main log or a subagent transcript).

    Handles the sync path (Agent tool_use + toolUseResult), the async path
    (<task-notification>), and legacy notification fields. Returns
    {"dispatches", "unparsed_lines", "truncated", "harness_version"}.
    `notify_seq` and `seen_notifications` are mutated in place so the main-log
    caller can persist them; nested callers pass fresh containers.
    """
    pending_dispatches: dict[str, dict] = {}
    dispatches: list[dict] = []
    harness_version = ""
    truncated = False
    unparsed = 0

    for line in raw.split("\n"):
        ...  # the existing loop body, verbatim, with these two edits:
        #   1. `meta["unparsed_lines"] += 1`  →  `unparsed += 1`
        #   2. nothing else changes
    return {"dispatches": dispatches, "unparsed_lines": unparsed,
            "truncated": truncated, "harness_version": harness_version}
```

Then `_extract_main_log` becomes:

```python
    notify_seq: dict[str, int] = dict(sess.get("notify_seq", {}))
    seen_notifications: set[tuple[str, str]] = set()
    out = _extract_dispatches_from_text(session_id, raw, notify_seq, seen_notifications)
    dispatches = out["dispatches"]
    meta["unparsed_lines"] += out["unparsed_lines"]

    # Update meta
    meta["offset"] = file_size
    meta["size"] = file_size
    meta["head_sha"] = _sha256_head(path, min(_HEAD_BYTES, file_size))
    meta["harness_version"] = out["harness_version"] or meta["harness_version"]
    meta["truncated"] = out["truncated"]
    meta["notify_seq"] = notify_seq

    return dispatches, meta
```

- [ ] **Step 4: Run the existing extraction tests to prove the refactor is behavior-preserving**

Run: `python3 -m pytest profiles/base/tests/test_main_log_extraction.py profiles/base/tests/test_e2e.py -q`
Expected: PASS, same counts as before the change.

- [ ] **Step 5: Add `_merge_dispatch` and the nested pass to `_collect_subagent_transcripts`**

Add above `_collect_subagent_transcripts`:

```python
_TRANSCRIPT_TOKEN_FIELDS = frozenset({
    "tokens_input", "tokens_output", "tokens_cache_read", "tokens_cache_creation"})


def _merge_dispatch(existing: dict, fresh: dict) -> None:
    """Upgrade a thin (meta-only) dispatch dict with a fuller record for the same id.

    Fresh non-empty values win, except token fields already derived from the
    subagent's own transcript, which are more accurate than toolUseResult.
    """
    for k, v in fresh.items():
        if v is None or v == "" or v == []:
            continue
        if k in _TRANSCRIPT_TOKEN_FIELDS and existing.get(k) not in (None, 0):
            continue
        if k == "dispatch_complete":
            existing[k] = bool(existing.get(k, False) or v)
            continue
        existing[k] = v
```

In `_collect_subagent_transcripts`, replace the block from `if subagents_dir.is_dir():` through `for meta_file in sorted(subagents_dir.glob("agent-*.meta.json")):` … `# Read meta.json` … `continue` with a two-pass structure:

```python
    # Read every meta.json once.
    metas: dict[str, dict] = {}
    if subagents_dir.is_dir():
        for meta_file in sorted(subagents_dir.glob("agent-*.meta.json")):
            agent_id = meta_file.name[len("agent-"):-len(".meta.json")]
            if not agent_id:
                continue
            try:
                metas[agent_id] = json.loads(meta_file.read_text())
            except (OSError, json.JSONDecodeError):
                _log_error(agentlog, f"failed to read meta.json for agent {agent_id}")

    # Defer children whose parent is still running: their full row comes from
    # the parent's transcript, and a thin row written now would block it via
    # (dispatch_id, seq) dedup on the next pass.
    deferred: set[str] = set()
    for agent_id, meta in metas.items():
        parent = meta.get("parentAgentId", "")
        if parent and parent in running_agents:
            deferred.add(agent_id)

    def _skip(agent_id: str) -> bool:
        return agent_id in agents_done or agent_id in running_agents or agent_id in deferred

    # Pass 1 (C1): nested dispatches. A task-runner's transcript holds the same
    # Agent tool_use + toolUseResult records as the main log.
    for agent_id, meta in metas.items():
        if _skip(agent_id):
            continue
        transcript_file = subagents_dir / f"agent-{agent_id}.jsonl"
        if not transcript_file.exists():
            continue
        try:
            raw = transcript_file.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        try:
            nested = _extract_dispatches_from_text(session_id, raw, {}, set())
        except Exception:
            _log_error(agentlog, f"nested extraction failed for agent {agent_id}: {traceback.format_exc()}")
            continue
        parent_tool_use_id = meta.get("toolUseId", "")
        depth = (meta.get("spawnDepth") or 1) + 1
        for d in nested["dispatches"]:
            if parent_tool_use_id:
                d["parent_dispatch_id"] = parent_tool_use_id
            d["spawn_depth"] = depth
            did = d.get("dispatch_id", "")
            if did in dispatch_by_id:
                for idx in dispatch_by_id[did]:
                    _merge_dispatch(dispatches[idx], d)
            else:
                dispatches.append(d)
                dispatch_by_id.setdefault(did, []).append(len(dispatches) - 1)

    # Pass 2: per-agent enrichment (unchanged logic, now iterating `metas`).
    for agent_id, meta in metas.items():
        if _skip(agent_id):
            continue
        tool_use_id = meta.get("toolUseId", "")
        ...  # existing body from `agent_type = meta.get("agentType", "")` onward, unchanged
```

Remove the now-duplicated `agent_id = ...`, `if agent_id in agents_done`, `if agent_id in running_agents`, and `# Read meta.json` lines from the old loop; keep everything from `agent_type = meta.get("agentType", "")` to `agents_done.add(agent_id)`.

- [ ] **Step 6: Run the new and the neighbouring tests**

Run: `python3 -m pytest profiles/base/tests/test_nested_dispatches.py profiles/base/tests/test_subagent_transcripts.py profiles/base/tests/test_tool_events.py -v`
Expected: PASS. If `test_child_of_running_parent_is_deferred` fails on `agents_done`, check that the `agents_done.add(agent_id)` line is inside the pass-2 loop *after* the `_skip` check.

- [ ] **Step 7: Run the whole Python suite**

Run: `python3 -m pytest profiles/base/tests/ -q`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add profiles/base/zprof-collect.py profiles/base/tests/test_nested_dispatches.py
git commit -m "feat(base): extract nested dispatches from task-runner transcripts (full child rows)"
```

---

### Task 5: C4 — `ext.run_id` via the in-memory parent chain

**Files:**
- Modify: `profiles/base/zprof-collect.py` (`_normalize_and_write` ≈ line 1270, `_normalize_dispatch` ext block)
- Test: `profiles/base/tests/test_normalization.py`

**Interfaces:**
- Consumes: raw dispatch dicts with `dispatch_id`, `parent_dispatch_id`, `role`.
- Produces: `_assign_run_ids(dispatches: list[dict]) -> None` (sets raw `ext.run_id` = raw dispatch_id of the nearest `task-runner` ancestor, including the runner itself); normalized rows carry `ext.run_id` in composite form. Go Task 6 uses it as a shortcut and falls back to walking `parent_dispatch_id`.

- [ ] **Step 1: Write the failing tests**

Append to `profiles/base/tests/test_normalization.py`:

```python
_assign_run_ids = zprof_collect._assign_run_ids


def test_assign_run_ids_walks_parent_chain():
    ds = [
        {"dispatch_id": "toolu_R", "role": "task-runner"},
        {"dispatch_id": "toolu_I", "role": "implementer", "parent_dispatch_id": "toolu_R"},
        {"dispatch_id": "toolu_A", "role": "auditor", "parent_dispatch_id": "toolu_I"},
        {"dispatch_id": "toolu_X", "role": "explorer"},  # dispatched by main, no runner ancestor
    ]
    _assign_run_ids(ds)
    assert ds[0]["ext"]["run_id"] == "toolu_R", "the runner is its own run"
    assert ds[1]["ext"]["run_id"] == "toolu_R"
    assert ds[2]["ext"]["run_id"] == "toolu_R"
    assert "ext" not in ds[3] or "run_id" not in ds[3]["ext"]


def test_run_id_is_composite_after_normalization():
    raw = {"dispatch_id": "toolu_I", "role": "implementer", "parent_dispatch_id": "toolu_R",
           "status": "completed", "ext": {"run_id": "toolu_R"}}
    norm = _norm(raw)
    assert norm["ext"]["run_id"] == "claude-code:s1:toolu_R"
    assert norm["parent_dispatch_id"] == "claude-code:s1:toolu_R"
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `python3 -m pytest profiles/base/tests/test_normalization.py -k run_id -v`
Expected: FAIL — `AttributeError: ... '_assign_run_ids'`.

- [ ] **Step 3: Implement**

Add above `_normalize_and_write`:

```python
_RUN_CHAIN_MAX_HOPS = 16


def _assign_run_ids(dispatches: list[dict]) -> None:
    """C4: ext.run_id = dispatch_id of the nearest task-runner ancestor (in-memory chain).

    Best effort: only dispatches whose chain is fully present in this batch get
    a run_id. `zprof score` re-derives membership from parent_dispatch_id
    anyway, so a missing run_id is a slower path, not a wrong answer.
    """
    by_id = {d.get("dispatch_id", ""): d for d in dispatches if d.get("dispatch_id")}
    for d in dispatches:
        cur, hops = d, 0
        while cur is not None and hops < _RUN_CHAIN_MAX_HOPS:
            if cur.get("role") == "task-runner":
                ext = dict(d.get("ext") or {})
                ext["run_id"] = cur["dispatch_id"]
                d["ext"] = ext
                break
            cur = by_id.get(cur.get("parent_dispatch_id", ""))
            hops += 1
```

In `_normalize_and_write`, right after `cwd = payload.get("cwd", os.getcwd())` add `_assign_run_ids(dispatches)`.

In `_normalize_dispatch`, in the extension block, make `ext.run_id` composite. Replace

```python
    ext = raw.get("ext")
    if project_id_provisional:
```

with

```python
    ext = raw.get("ext")
    if ext and ext.get("run_id"):
        ext = dict(ext)
        ext["run_id"] = _make_composite_id(session_id, ext["run_id"])
    if project_id_provisional:
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `python3 -m pytest profiles/base/tests/ -q`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add profiles/base/zprof-collect.py profiles/base/tests/test_normalization.py
git commit -m "feat(base): ext.run_id from task-runner ancestor chain"
```

---
