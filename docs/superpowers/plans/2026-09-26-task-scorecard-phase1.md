# Per-task Scorecard (Phase 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After every task-runner run, `zprof score` prints a 0–100 card with per-role breakdown, appends a row to `.agentlog/scores.jsonl` and a `## Score` section to the run log — computed deterministically from `.agentlog/`.

**Architecture:** The Python collector (`profiles/base/zprof-collect.py`, stdlib only) extracts new facts into `.agentlog/`: full rows for nested dispatches (C1), the `verdict` value and `ext.{next,artifact,run_log}` (C2), an ordered `tool-events.jsonl` with `is_error` (C3), `ext.run_id` (C4). A new Go package `cli/internal/score` reads `.agentlog/` only, groups dispatches into runs, computes penalties P1–P7, renders the card and persists it. `zprof score` wires it; the Stop hook runs it after the collector; `AGENT_LOOP.md` tells main to paste the card into `followup.md`.

**Tech Stack:** Go 1.22 (cobra, testify/require, yaml.v3, stdlib `regexp`/`crypto/sha1`), Python 3.10+ stdlib only (pytest for tests).

**Spec:** `docs/superpowers/specs/2026-09-26-task-scorecard-design.md`

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

## Review Focus

1. **Blind retry where the retried command is itself a "mutating" Bash pattern** (e.g. `sed -i` fails twice). Expected: counted as a blind retry, then state resets. → Task 7 test `TestBlindRetries_MutatingCommandStillCounts`.
2. **`swift test` / `cargo test` repeated after an error with only a build in between.** Expected: counted (build/test commands are **not** mutating). → Task 1 amends the spec; Task 7 test `TestBlindRetries_BuildBetweenDoesNotReset`.
3. **Child agent finished while its parent task-runner is still running (async runner).** Expected: child is deferred, not written as a thin meta-only row that later blocks the full row via dedup. → Task 4 test `test_child_of_running_parent_is_deferred`.
4. **A run whose root task-runner returned `blocked` with an `artifact:` that does not exist.** Expected: tier `Blocked`, P7 does not count `artifact_exists=false` for blocked. → Task 7 test `TestP7_BlockedDoesNotCountMissingArtifact`.
5. **Second `zprof score` on the same run with unchanged weights.** Expected: no duplicate row is *needed* by readers (readers take the latest per `(run_id, weights_hash)`), run-log section replaced in place, not appended twice. → Task 8 tests `TestWriteRunLogSection_Idempotent`, `TestReadScoredKeys`.

---

## File Structure

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

**Go — `cli/internal/`**
- Create `score/config.go` — `Config`, `Defaults()`, `LoadConfig(projectDir, agentlogDir)`, `WeightsHash()`.
- Create `score/reader.go` — `ToolEvent`, `ReadToolEvents(path)`, `Run`, `BuildRuns(dispatches, events)`, `LatestRun`, `FindRun`.
- Create `score/metrics.go` — `Penalty`, `computeP1…P7`.
- Create `score/scoring.go` — `Card`, `Facts`, `Inputs`, `Compute(run, cfg) Card`, `tierFor`.
- Create `score/render.go` — `RenderCard(Card) string`, `findingText`.
- Create `score/persist.go` — `AppendScore`, `ReadScoredKeys`, `WriteRunLogSection`.
- Create `score/testdata/run1/{dispatches.jsonl,tool-events.jsonl}` + `score/*_test.go`.
- Modify `manifest/project.go` — `ScoreConfig`, `CarryOverFrom`.
- Create `cmd/score.go` — `NewScoreCmd()`; modify `cli/cmd/zprof/main.go` to register.
- Modify `apply/settings.go` — Stop hook chains `zprof score`; upgrade path for old Stop command; `apply/settings_test.go`.

**Prompt layer**
- Modify `profiles/base/agent-loop-router.md` — rule: after task-runner returns, run `zprof score`, paste 3–4 card lines into `followup.md`.

**Docs**
- Amend `docs/superpowers/specs/2026-09-26-task-scorecard-design.md` §6 (mutating patterns exclude build/test; check-then-reset order).

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

### Task 6: Go `score` package — config, manifest `score:` section, reader, run grouping, fixture

**Files:**
- Create: `cli/internal/score/config.go`, `cli/internal/score/config_test.go`
- Create: `cli/internal/score/reader.go`, `cli/internal/score/reader_test.go`
- Create: `cli/internal/score/testdata/run1/dispatches.jsonl`, `cli/internal/score/testdata/run1/tool-events.jsonl`, `cli/internal/score/testdata/run1/schema.json`
- Modify: `cli/internal/manifest/project.go` (add `ScoreConfig`, `Score` field, `CarryOverFrom`), `cli/internal/manifest/project_test.go`

**Interfaces:**
- Consumes: `stats.Dispatch`, `stats.ReadDispatches(path)` from `cli/internal/stats`; `manifest.LoadProject(path)`.
- Produces (used by Tasks 7–9):
  ```go
  package score
  type Config struct {
      Enabled      bool
      Weights      map[string]float64 // P1..P7
      Saturation   map[string]float64 // P1..P7
      Thresholds   Thresholds
      MutatingBash []*regexp.Regexp
      MutatingTools map[string]bool   // Edit, Write, MultiEdit, NotebookEdit
      ExemptRoles  map[string]bool    // auditor, auditor-deep
  }
  type Thresholds struct{ Ideal, Solid int }
  func Defaults() Config
  func LoadConfig(projectDir, agentlogDir string) (Config, error) // defaults ← schema.json ← .zprof.yaml
  func (c Config) WeightsHash() string                             // sha1(canonical json)[:12]

  type ToolEvent struct {
      SchemaVersion int    `json:"schema_version"`
      DispatchID    string `json:"dispatch_id"`
      Seq           int    `json:"seq"`
      Ts            string `json:"ts"`
      Tool          string `json:"tool"`
      InputHash     string `json:"input_hash"`
      Target        string `json:"target"`
      IsError       *bool  `json:"is_error,omitempty"`
      ResultChars   int    `json:"result_chars"`
  }
  func ReadToolEvents(path string) ([]ToolEvent, error) // missing file → nil, nil; dedup by (dispatch_id, seq), later row wins

  type Run struct {
      ID         string
      Root       stats.Dispatch            // the task-runner dispatch
      Dispatches []stats.Dispatch          // root + all descendants
      Steps      []stats.Dispatch          // direct children of root, sorted by Timestamp then DispatchID
      Events     map[string][]ToolEvent    // dispatch_id → events sorted by Seq
      RunLog     string                    // Root.Ext["run_log"] or ""
  }
  func BuildRuns(ds []stats.Dispatch, evs []ToolEvent) []Run // one Run per complete task-runner dispatch, sorted by Root.Timestamp asc
  func LatestRun(runs []Run) *Run
  func FindRun(runs []Run, key string) *Run // exact ID, or RunLog equal / suffix, or ID suffix
  ```
- `manifest.ScoreConfig{Enabled *bool; Weights, Saturation map[string]float64; Thresholds *ScoreThresholds{Ideal, Solid int}}` on `ProjectManifest.Score`.

- [ ] **Step 1: Write the fixture — `testdata/run1/dispatches.jsonl`**

Eight rows, one run. Tokens sum to 400 000; `t7` is a failed implementer dispatch (wasted 40 000); `t2` has a preamble; `t7` lacks `ext.run_id` on purpose (exercises the parent walk).

```jsonl
{"schema_version":2,"harness":"claude-code","harness_version":"2.1.220","machine_id":"m","project_id":"p1","ts_utc":"2026-09-26T10:00:00Z","session_id":"s1","dispatch_id":"claude-code:s1:t0","seq":0,"spawn_depth":1,"role":"task-runner","model_resolved":"claude-sonnet-5","verdict":"done","status":"completed","dispatch_complete":true,"tokens_input":15000,"tokens_output":5000,"tokens_cache_read":0,"tokens_cache_creation":0,"tool_uses":7,"duration_ms":1800000,"has_preamble":false,"return_parsed":true,"transcript_captured":true,"ext":{"run_log":".zprof/runs/2026-09-26-fixture.md","run_id":"claude-code:s1:t0"}}
{"schema_version":2,"harness":"claude-code","harness_version":"2.1.220","machine_id":"m","project_id":"p1","ts_utc":"2026-09-26T10:01:00Z","session_id":"s1","dispatch_id":"claude-code:s1:t1","seq":0,"parent_dispatch_id":"claude-code:s1:t0","spawn_depth":2,"role":"planner","model_resolved":"claude-sonnet-5","verdict":"done","status":"completed","dispatch_complete":true,"tokens_input":15000,"tokens_output":5000,"tokens_cache_read":0,"tokens_cache_creation":0,"tool_uses":2,"duration_ms":60000,"has_preamble":false,"return_parsed":true,"artifact_exists":true,"transcript_captured":true,"ext":{"run_id":"claude-code:s1:t0"}}
{"schema_version":2,"harness":"claude-code","harness_version":"2.1.220","machine_id":"m","project_id":"p1","ts_utc":"2026-09-26T10:03:00Z","session_id":"s1","dispatch_id":"claude-code:s1:t7","seq":0,"parent_dispatch_id":"claude-code:s1:t0","spawn_depth":2,"role":"implementer","model_resolved":"claude-sonnet-5","status":"failed","dispatch_complete":true,"tokens_input":30000,"tokens_output":10000,"tokens_cache_read":0,"tokens_cache_creation":0,"tool_uses":0,"duration_ms":30000,"return_parsed":false,"transcript_captured":true}
{"schema_version":2,"harness":"claude-code","harness_version":"2.1.220","machine_id":"m","project_id":"p1","ts_utc":"2026-09-26T10:05:00Z","session_id":"s1","dispatch_id":"claude-code:s1:t2","seq":0,"parent_dispatch_id":"claude-code:s1:t0","spawn_depth":2,"role":"implementer","model_resolved":"claude-sonnet-5","verdict":"done","status":"completed","dispatch_complete":true,"tokens_input":100000,"tokens_output":10000,"tokens_cache_read":10000,"tokens_cache_creation":0,"tool_uses":10,"duration_ms":600000,"has_preamble":true,"return_parsed":true,"artifact_exists":true,"transcript_captured":true,"ext":{"run_id":"claude-code:s1:t0"}}
{"schema_version":2,"harness":"claude-code","harness_version":"2.1.220","machine_id":"m","project_id":"p1","ts_utc":"2026-09-26T10:20:00Z","session_id":"s1","dispatch_id":"claude-code:s1:t3","seq":0,"parent_dispatch_id":"claude-code:s1:t0","spawn_depth":2,"role":"tester","model_resolved":"claude-sonnet-5","verdict":"failed","status":"completed","dispatch_complete":true,"tokens_input":30000,"tokens_output":10000,"tokens_cache_read":0,"tokens_cache_creation":0,"tool_uses":2,"duration_ms":120000,"has_preamble":false,"return_parsed":true,"transcript_captured":true,"ext":{"run_id":"claude-code:s1:t0"}}
{"schema_version":2,"harness":"claude-code","harness_version":"2.1.220","machine_id":"m","project_id":"p1","ts_utc":"2026-09-26T10:25:00Z","session_id":"s1","dispatch_id":"claude-code:s1:t4","seq":0,"parent_dispatch_id":"claude-code:s1:t0","spawn_depth":2,"role":"implementer","model_resolved":"claude-sonnet-5","verdict":"done","status":"completed","dispatch_complete":true,"tokens_input":60000,"tokens_output":10000,"tokens_cache_read":10000,"tokens_cache_creation":0,"tool_uses":4,"duration_ms":300000,"has_preamble":false,"return_parsed":true,"artifact_exists":true,"transcript_captured":true,"ext":{"run_id":"claude-code:s1:t0"}}
{"schema_version":2,"harness":"claude-code","harness_version":"2.1.220","machine_id":"m","project_id":"p1","ts_utc":"2026-09-26T10:35:00Z","session_id":"s1","dispatch_id":"claude-code:s1:t5","seq":0,"parent_dispatch_id":"claude-code:s1:t0","spawn_depth":2,"role":"tester","model_resolved":"claude-sonnet-5","verdict":"done","status":"completed","dispatch_complete":true,"tokens_input":30000,"tokens_output":10000,"tokens_cache_read":0,"tokens_cache_creation":0,"tool_uses":1,"duration_ms":90000,"has_preamble":false,"return_parsed":true,"transcript_captured":true,"ext":{"run_id":"claude-code:s1:t0"}}
{"schema_version":2,"harness":"claude-code","harness_version":"2.1.220","machine_id":"m","project_id":"p1","ts_utc":"2026-09-26T10:40:00Z","session_id":"s1","dispatch_id":"claude-code:s1:t6","seq":0,"parent_dispatch_id":"claude-code:s1:t0","spawn_depth":2,"role":"reviewer","model_resolved":"claude-opus-5-5","verdict":"approve","status":"completed","dispatch_complete":true,"tokens_input":30000,"tokens_output":10000,"tokens_cache_read":0,"tokens_cache_creation":0,"tool_uses":1,"duration_ms":180000,"has_preamble":false,"return_parsed":true,"artifact_exists":true,"transcript_captured":true,"ext":{"run_id":"claude-code:s1:t0"}}
```

- [ ] **Step 2: Write the fixture — `testdata/run1/tool-events.jsonl`**

Twenty events, four errors (rate exactly 0.20). `t2`: one re-read (`a.swift` at seq 3 before any edit), then `swift test` fails three times in a row (two blind retries), an edit, then passes.

```jsonl
{"schema_version":1,"dispatch_id":"claude-code:s1:t1","seq":1,"ts":"2026-09-26T10:00:30Z","tool":"Read","input_hash":"h-spec","target":"/p/SPEC.md","is_error":false,"result_chars":900}
{"schema_version":1,"dispatch_id":"claude-code:s1:t1","seq":2,"ts":"2026-09-26T10:00:50Z","tool":"Write","input_hash":"h-plan","target":"/p/plan-1.md","is_error":false,"result_chars":10}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":1,"ts":"2026-09-26T10:03:10Z","tool":"Read","input_hash":"h-ra","target":"/p/a.swift","is_error":false,"result_chars":500}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":2,"ts":"2026-09-26T10:03:20Z","tool":"Read","input_hash":"h-rb","target":"/p/b.swift","is_error":false,"result_chars":500}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":3,"ts":"2026-09-26T10:03:30Z","tool":"Read","input_hash":"h-ra","target":"/p/a.swift","is_error":false,"result_chars":500}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":4,"ts":"2026-09-26T10:03:40Z","tool":"Read","input_hash":"h-rc","target":"/p/c.swift","is_error":false,"result_chars":500}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":5,"ts":"2026-09-26T10:04:00Z","tool":"Edit","input_hash":"h-ea1","target":"/p/a.swift","is_error":false,"result_chars":10}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":6,"ts":"2026-09-26T10:04:10Z","tool":"Bash","input_hash":"h-test","target":"swift test --package-path Packages/Core","is_error":true,"result_chars":4000}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":7,"ts":"2026-09-26T10:04:30Z","tool":"Bash","input_hash":"h-test","target":"swift test --package-path Packages/Core","is_error":true,"result_chars":4000}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":8,"ts":"2026-09-26T10:04:50Z","tool":"Bash","input_hash":"h-test","target":"swift test --package-path Packages/Core","is_error":true,"result_chars":4000}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":9,"ts":"2026-09-26T10:04:55Z","tool":"Edit","input_hash":"h-ea2","target":"/p/a.swift","is_error":false,"result_chars":10}
{"schema_version":1,"dispatch_id":"claude-code:s1:t2","seq":10,"ts":"2026-09-26T10:04:59Z","tool":"Bash","input_hash":"h-test","target":"swift test --package-path Packages/Core","is_error":false,"result_chars":200}
{"schema_version":1,"dispatch_id":"claude-code:s1:t3","seq":1,"ts":"2026-09-26T10:19:00Z","tool":"Bash","input_hash":"h-test","target":"swift test --package-path Packages/Core","is_error":true,"result_chars":4000}
{"schema_version":1,"dispatch_id":"claude-code:s1:t3","seq":2,"ts":"2026-09-26T10:19:30Z","tool":"Read","input_hash":"h-rt","target":"/p/Tests/x.swift","is_error":false,"result_chars":300}
{"schema_version":1,"dispatch_id":"claude-code:s1:t4","seq":1,"ts":"2026-09-26T10:21:00Z","tool":"Read","input_hash":"h-ra","target":"/p/a.swift","is_error":false,"result_chars":500}
{"schema_version":1,"dispatch_id":"claude-code:s1:t4","seq":2,"ts":"2026-09-26T10:22:00Z","tool":"Edit","input_hash":"h-ea3","target":"/p/a.swift","is_error":false,"result_chars":10}
{"schema_version":1,"dispatch_id":"claude-code:s1:t4","seq":3,"ts":"2026-09-26T10:23:00Z","tool":"Bash","input_hash":"h-test","target":"swift test --package-path Packages/Core","is_error":false,"result_chars":200}
{"schema_version":1,"dispatch_id":"claude-code:s1:t4","seq":4,"ts":"2026-09-26T10:24:00Z","tool":"Bash","input_hash":"h-commit","target":"git commit -m fix","is_error":false,"result_chars":80}
{"schema_version":1,"dispatch_id":"claude-code:s1:t5","seq":1,"ts":"2026-09-26T10:34:00Z","tool":"Bash","input_hash":"h-test","target":"swift test --package-path Packages/Core","is_error":false,"result_chars":200}
{"schema_version":1,"dispatch_id":"claude-code:s1:t6","seq":1,"ts":"2026-09-26T10:39:00Z","tool":"Bash","input_hash":"h-diff","target":"git diff HEAD~1","is_error":false,"result_chars":3000}
```

- [ ] **Step 3: Write the fixture — `testdata/run1/schema.json`**

A trimmed deployed schema that overrides one saturation value, so the test proves schema.json is read:

```json
{
  "version": 2,
  "mutating_bash_patterns": ["\\s>>?\\s", "\\bsed\\s+-i\\b", "\\btee\\b", "\\b(mv|cp|rm|touch|mkdir)\\b", "\\bgit\\s+(commit|checkout|stash|reset|apply|cherry-pick|merge|rebase)\\b", "\\bxcodegen\\b"],
  "verdict_exempt_roles": ["auditor", "auditor-deep"],
  "score_defaults": {
    "weights": {"P1": 20, "P2": 15, "P3": 10, "P4": 20, "P5": 10, "P6": 15, "P7": 10},
    "saturation": {"P1": 0.20, "P2": 3, "P3": 0.5, "P4": 2, "P5": 2, "P6": 0.30, "P7": 4},
    "thresholds": {"ideal": 85, "solid": 60}
  }
}
```

- [ ] **Step 4: Write the failing config tests**

Create `cli/internal/score/config_test.go`:

```go
package score

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func fixtureDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "testdata", "run1")
}

func TestDefaults_WeightsSumTo100(t *testing.T) {
	c := Defaults()
	sum := 0.0
	for _, w := range c.Weights {
		sum += w
	}
	require.Equal(t, 100.0, sum)
	require.Len(t, c.Saturation, 7)
	require.Equal(t, Thresholds{Ideal: 85, Solid: 60}, c.Thresholds)
	require.True(t, c.Enabled)
	require.True(t, c.MutatingTools["Edit"])
	require.True(t, c.ExemptRoles["auditor-deep"])
}

func TestDefaults_MutatingBashExcludesBuildAndTest(t *testing.T) {
	c := Defaults()
	for _, cmd := range []string{"swift test --package-path Packages/Core", "cargo build --release", "go test ./...", "pytest -q", "make test", "git status", "cat foo.txt"} {
		require.False(t, c.IsMutatingBash(cmd), cmd)
	}
	for _, cmd := range []string{"cat > f.txt <<'EOF'", "sed -i 's/a/b/' f", "git commit -m x", "rm -rf build", "xcodegen generate", "echo hi | tee out.log"} {
		require.True(t, c.IsMutatingBash(cmd), cmd)
	}
}

func TestLoadConfig_SchemaJsonThenZprofYaml(t *testing.T) {
	proj := t.TempDir()
	agentlog := filepath.Join(proj, ".agentlog")
	require.NoError(t, os.MkdirAll(agentlog, 0o755))
	schema, err := os.ReadFile(filepath.Join(fixtureDir(), "schema.json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(agentlog, "schema.json"), schema, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"), []byte(
		"overlays: [ios-swift]\nscore:\n  weights: {P1: 30, P2: 5}\n  thresholds: {ideal: 90}\n"), 0o644))

	c, err := LoadConfig(proj, agentlog)
	require.NoError(t, err)
	require.Equal(t, 30.0, c.Weights["P1"], ".zprof.yaml overrides")
	require.Equal(t, 5.0, c.Weights["P2"])
	require.Equal(t, 10.0, c.Weights["P3"], "untouched keys keep defaults")
	require.Equal(t, 90, c.Thresholds.Ideal)
	require.Equal(t, 60, c.Thresholds.Solid, "partial thresholds override only what is set")
	require.True(t, c.Enabled)
}

func TestLoadConfig_NoFilesUsesDefaults(t *testing.T) {
	c, err := LoadConfig(t.TempDir(), filepath.Join(t.TempDir(), ".agentlog"))
	require.NoError(t, err)
	require.Equal(t, Defaults().Weights, c.Weights)
}

func TestLoadConfig_EnabledFalse(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"), []byte("overlays: []\nscore:\n  enabled: false\n"), 0o644))
	c, err := LoadConfig(proj, filepath.Join(proj, ".agentlog"))
	require.NoError(t, err)
	require.False(t, c.Enabled)
}

func TestWeightsHash_StableAndSensitive(t *testing.T) {
	a := Defaults()
	b := Defaults()
	require.Equal(t, a.WeightsHash(), b.WeightsHash())
	require.Len(t, a.WeightsHash(), 12)
	b.Weights["P1"] = 21
	require.NotEqual(t, a.WeightsHash(), b.WeightsHash())
	c := Defaults()
	c.Thresholds.Solid = 61
	require.NotEqual(t, a.WeightsHash(), c.WeightsHash())
}
```

- [ ] **Step 5: Run to verify failure**

Run: `cd cli && go test ./internal/score/ -count=1`
Expected: FAIL — `undefined: Defaults` (package does not compile yet).

- [ ] **Step 6: Add `ScoreConfig` to the manifest**

In `cli/internal/manifest/project.go`, add after the `Audit` field:

```go
	// Score configures `zprof score` (per-task scorecard). Nil = defaults
	// from telemetry.yaml / the compiled-in table; enabled unless
	// `enabled: false` is set explicitly.
	Score *ScoreConfig `yaml:"score,omitempty"`
```

and after `ABExperiment`:

```go
// ScoreConfig overrides scorecard weights, saturation points and tier thresholds.
// Only keys present override; the rest fall back to defaults.
type ScoreConfig struct {
	Enabled    *bool              `yaml:"enabled,omitempty"`
	Weights    map[string]float64 `yaml:"weights,omitempty"`
	Saturation map[string]float64 `yaml:"saturation,omitempty"`
	Thresholds *ScoreThresholds   `yaml:"thresholds,omitempty"`
}

// ScoreThresholds are tier cut-offs; zero means "not set".
type ScoreThresholds struct {
	Ideal int `yaml:"ideal,omitempty"`
	Solid int `yaml:"solid,omitempty"`
}
```

In `CarryOverFrom` add:

```go
	if m.Score == nil {
		m.Score = prev.Score
	}
```

Append to `cli/internal/manifest/project_test.go`:

```go
func TestLoadProjectManifest_ScoreSection(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".zprof.yaml")
	require.NoError(t, os.WriteFile(p, []byte("overlays: [x]\nscore:\n  enabled: false\n  weights: {P1: 30}\n  thresholds: {ideal: 90}\n"), 0o644))
	m, err := LoadProject(p)
	require.NoError(t, err)
	require.NotNil(t, m.Score)
	require.NotNil(t, m.Score.Enabled)
	require.False(t, *m.Score.Enabled)
	require.Equal(t, 30.0, m.Score.Weights["P1"])
	require.Equal(t, 90, m.Score.Thresholds.Ideal)
	require.Equal(t, 0, m.Score.Thresholds.Solid)
}

func TestCarryOverFrom_KeepsScore(t *testing.T) {
	enabled := false
	prev := &ProjectManifest{Score: &ScoreConfig{Enabled: &enabled}}
	m := &ProjectManifest{}
	m.CarryOverFrom(prev)
	require.Same(t, prev.Score, m.Score)
}
```

(Add `"os"` and `"path/filepath"` to that test file's imports if missing.)

Run: `cd cli && go test ./internal/manifest/ -count=1` → PASS.

- [ ] **Step 7: Implement `config.go`**

```go
// Package score computes the per-task scorecard from .agentlog/ data
// (spec: docs/superpowers/specs/2026-09-26-task-scorecard-design.md).
// It never reads raw Claude Code session logs — only what the Python
// collector already normalized.
package score

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/vaporphd/zprof/internal/manifest"
)

// PenaltyIDs is the fixed order of the seven penalties.
var PenaltyIDs = []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7"}

type Thresholds struct {
	Ideal int `json:"ideal"`
	Solid int `json:"solid"`
}

type Config struct {
	Enabled       bool
	Weights       map[string]float64
	Saturation    map[string]float64
	Thresholds    Thresholds
	MutatingBash  []*regexp.Regexp
	MutatingTools map[string]bool
	ExemptRoles   map[string]bool
}

var defaultMutatingBash = []string{
	`\s>>?\s`,
	`\bsed\s+-i\b`,
	`\btee\b`,
	`\b(mv|cp|rm|touch|mkdir)\b`,
	`\bgit\s+(commit|checkout|stash|reset|apply|cherry-pick|merge|rebase)\b`,
	`\bxcodegen\b`,
	`\b(cargo|go|swift)\s+fmt\b`,
	`\b(gofmt\s+-w|swiftformat|rustfmt)\b`,
}

// Defaults mirrors telemetry.yaml `score_defaults`; keep the two in sync.
func Defaults() Config {
	c := Config{
		Enabled:    true,
		Weights:    map[string]float64{"P1": 20, "P2": 15, "P3": 10, "P4": 20, "P5": 10, "P6": 15, "P7": 10},
		Saturation: map[string]float64{"P1": 0.20, "P2": 3, "P3": 0.5, "P4": 2, "P5": 2, "P6": 0.30, "P7": 4},
		Thresholds: Thresholds{Ideal: 85, Solid: 60},
		MutatingTools: map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true},
		ExemptRoles:   map[string]bool{"auditor": true, "auditor-deep": true},
	}
	c.MutatingBash = compilePatterns(defaultMutatingBash)
	return c
}

func compilePatterns(pats []string) []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, p := range pats {
		if re, err := regexp.Compile(p); err == nil {
			out = append(out, re)
		}
	}
	return out
}

// IsMutatingBash reports whether a Bash command changes files/state.
func (c Config) IsMutatingBash(command string) bool {
	for _, re := range c.MutatingBash {
		if re.MatchString(command) {
			return true
		}
	}
	return false
}

// schemaFile is the subset of .agentlog/schema.json (telemetry.yaml as JSON) we read.
type schemaFile struct {
	MutatingBashPatterns []string `json:"mutating_bash_patterns"`
	VerdictExemptRoles   []string `json:"verdict_exempt_roles"`
	ScoreDefaults        *struct {
		Weights    map[string]float64 `json:"weights"`
		Saturation map[string]float64 `json:"saturation"`
		Thresholds *Thresholds        `json:"thresholds"`
	} `json:"score_defaults"`
}

// LoadConfig layers: compiled defaults ← <agentlogDir>/schema.json ← <projectDir>/.zprof.yaml.
// Missing files are not errors; malformed ones are.
func LoadConfig(projectDir, agentlogDir string) (Config, error) {
	c := Defaults()

	if data, err := os.ReadFile(filepath.Join(agentlogDir, "schema.json")); err == nil {
		var s schemaFile
		if err := json.Unmarshal(data, &s); err != nil {
			return c, fmt.Errorf("parse schema.json: %w", err)
		}
		if len(s.MutatingBashPatterns) > 0 {
			c.MutatingBash = compilePatterns(s.MutatingBashPatterns)
		}
		if len(s.VerdictExemptRoles) > 0 {
			c.ExemptRoles = map[string]bool{}
			for _, r := range s.VerdictExemptRoles {
				c.ExemptRoles[r] = true
			}
		}
		if s.ScoreDefaults != nil {
			mergeFloats(c.Weights, s.ScoreDefaults.Weights)
			mergeFloats(c.Saturation, s.ScoreDefaults.Saturation)
			if s.ScoreDefaults.Thresholds != nil {
				mergeThresholds(&c.Thresholds, *s.ScoreDefaults.Thresholds)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return c, fmt.Errorf("read schema.json: %w", err)
	}

	manifestPath := filepath.Join(projectDir, ".zprof.yaml")
	if _, err := os.Stat(manifestPath); err == nil {
		m, err := manifest.LoadProject(manifestPath)
		if err != nil {
			return c, fmt.Errorf("load .zprof.yaml: %w", err)
		}
		if m.Score != nil {
			if m.Score.Enabled != nil {
				c.Enabled = *m.Score.Enabled
			}
			mergeFloats(c.Weights, m.Score.Weights)
			mergeFloats(c.Saturation, m.Score.Saturation)
			if m.Score.Thresholds != nil {
				mergeThresholds(&c.Thresholds, Thresholds{Ideal: m.Score.Thresholds.Ideal, Solid: m.Score.Thresholds.Solid})
			}
		}
	}
	return c, nil
}

func mergeFloats(dst, src map[string]float64) {
	for k, v := range src {
		if _, known := dst[k]; known {
			dst[k] = v
		}
	}
}

func mergeThresholds(dst *Thresholds, src Thresholds) {
	if src.Ideal > 0 {
		dst.Ideal = src.Ideal
	}
	if src.Solid > 0 {
		dst.Solid = src.Solid
	}
}

// WeightsHash identifies the scoring parameters so historical rows stay comparable.
// encoding/json sorts map keys, so the encoding is canonical.
func (c Config) WeightsHash() string {
	keys := make([]string, 0, len(c.Weights))
	for k := range c.Weights {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	payload := struct {
		Weights    map[string]float64 `json:"weights"`
		Saturation map[string]float64 `json:"saturation"`
		Thresholds Thresholds         `json:"thresholds"`
	}{c.Weights, c.Saturation, c.Thresholds}
	data, _ := json.Marshal(payload)
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])[:12]
}
```

- [ ] **Step 8: Run the config tests**

Run: `cd cli && go test ./internal/score/ -run 'Defaults|LoadConfig|WeightsHash' -count=1`
Expected: PASS.

- [ ] **Step 9: Write the failing reader tests**

Create `cli/internal/score/reader_test.go`:

```go
package score

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vaporphd/zprof/internal/stats"
)

func loadRun1(t *testing.T) []Run {
	t.Helper()
	ds, _, err := stats.ReadDispatches(filepath.Join(fixtureDir(), "dispatches.jsonl"))
	require.NoError(t, err)
	evs, err := ReadToolEvents(filepath.Join(fixtureDir(), "tool-events.jsonl"))
	require.NoError(t, err)
	return BuildRuns(ds, evs)
}

func TestReadToolEvents_MissingFileIsEmpty(t *testing.T) {
	evs, err := ReadToolEvents(filepath.Join(t.TempDir(), "nope.jsonl"))
	require.NoError(t, err)
	require.Empty(t, evs)
}

func TestReadToolEvents_DedupBySeqLaterWins(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tool-events.jsonl")
	require.NoError(t, writeFile(p, `{"schema_version":1,"dispatch_id":"d","seq":1,"tool":"Read","input_hash":"a","is_error":false}
{"schema_version":1,"dispatch_id":"d","seq":1,"tool":"Read","input_hash":"a","is_error":true}
not json
{"schema_version":1,"dispatch_id":"d","seq":2,"tool":"Bash","input_hash":"b"}
`))
	evs, err := ReadToolEvents(p)
	require.NoError(t, err)
	require.Len(t, evs, 2)
	require.True(t, *evs[0].IsError)
	require.Nil(t, evs[1].IsError, "missing is_error stays nil")
}

func TestBuildRuns_GroupsRootAndDescendants(t *testing.T) {
	runs := loadRun1(t)
	require.Len(t, runs, 1)
	r := runs[0]
	require.Equal(t, "claude-code:s1:t0", r.ID)
	require.Equal(t, "task-runner", r.Root.Role)
	require.Len(t, r.Dispatches, 8, "root + 7 children (t7 has no ext.run_id and is found via parent walk)")
	require.Len(t, r.Steps, 7)
	roles := make([]string, 0, len(r.Steps))
	for _, s := range r.Steps {
		roles = append(roles, s.Role)
	}
	require.Equal(t, []string{"planner", "implementer", "implementer", "tester", "implementer", "tester", "reviewer"}, roles)
	require.Equal(t, ".zprof/runs/2026-09-26-fixture.md", r.RunLog)
	require.Len(t, r.Events["claude-code:s1:t2"], 10)
	require.Equal(t, 1, r.Events["claude-code:s1:t2"][0].Seq)
}

func TestBuildRuns_IgnoresIncompleteRunnerAndOrphans(t *testing.T) {
	ds := []stats.Dispatch{
		{DispatchID: "claude-code:s:r1", Role: "task-runner", DispatchComplete: false, TsUTC: "2026-09-26T10:00:00Z"},
		{DispatchID: "claude-code:s:x", Role: "explorer", DispatchComplete: true, TsUTC: "2026-09-26T10:01:00Z"},
		{DispatchID: "claude-code:s:r2", Role: "task-runner", DispatchComplete: true, TsUTC: "2026-09-26T11:00:00Z"},
		{DispatchID: "claude-code:s:c", Role: "tester", ParentDispatchID: "claude-code:s:r2", DispatchComplete: true, TsUTC: "2026-09-26T11:05:00Z"},
	}
	runs := BuildRuns(ds, nil)
	require.Len(t, runs, 1)
	require.Equal(t, "claude-code:s:r2", runs[0].ID)
	require.Len(t, runs[0].Dispatches, 2)
}

func TestLatestAndFindRun(t *testing.T) {
	ds := []stats.Dispatch{
		{DispatchID: "claude-code:s:r1", Role: "task-runner", DispatchComplete: true, TsUTC: "2026-09-26T10:00:00Z", Ext: map[string]any{"run_log": ".zprof/runs/2026-09-26-a.md"}},
		{DispatchID: "claude-code:s:r2", Role: "task-runner", DispatchComplete: true, TsUTC: "2026-09-26T11:00:00Z", Ext: map[string]any{"run_log": ".zprof/runs/2026-09-26-b.md"}},
	}
	runs := BuildRuns(ds, nil)
	require.Equal(t, "claude-code:s:r2", LatestRun(runs).ID)
	require.Equal(t, "claude-code:s:r1", FindRun(runs, "claude-code:s:r1").ID, "exact id")
	require.Equal(t, "claude-code:s:r1", FindRun(runs, "2026-09-26-a").ID, "run_log suffix")
	require.Equal(t, "claude-code:s:r2", FindRun(runs, ":r2").ID, "id suffix")
	require.Nil(t, FindRun(runs, "zzz"))
	require.Nil(t, LatestRun(nil))
}
```

Add a tiny helper at the bottom of `reader_test.go`:

```go
func writeFile(path, content string) error { return os.WriteFile(path, []byte(content), 0o644) }
```

(and import `"os"`).

- [ ] **Step 10: Implement `reader.go`**

```go
package score

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/vaporphd/zprof/internal/stats"
)

// ToolEvent is one row of .agentlog/tool-events.jsonl (telemetry.yaml `tool_events`).
type ToolEvent struct {
	SchemaVersion int    `json:"schema_version"`
	DispatchID    string `json:"dispatch_id"`
	Seq           int    `json:"seq"`
	Ts            string `json:"ts"`
	Tool          string `json:"tool"`
	InputHash     string `json:"input_hash"`
	Target        string `json:"target"`
	IsError       *bool  `json:"is_error,omitempty"`
	ResultChars   int    `json:"result_chars"`
}

// ReadToolEvents parses tool-events.jsonl. A missing file yields (nil, nil).
// Malformed lines are skipped. Duplicate (dispatch_id, seq) keys keep the
// later row — the collector may re-append after a crash between write and
// state.json save.
func ReadToolEvents(path string) ([]ToolEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	type key struct {
		id  string
		seq int
	}
	index := map[key]int{}
	var out []ToolEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)
	for sc.Scan() {
		var ev ToolEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil || ev.DispatchID == "" {
			continue
		}
		k := key{ev.DispatchID, ev.Seq}
		if i, dup := index[k]; dup {
			out[i] = ev
			continue
		}
		index[k] = len(out)
		out = append(out, ev)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return out, nil
}

// Run is one task-runner dispatch with everything it spawned.
type Run struct {
	ID         string
	Root       stats.Dispatch
	Dispatches []stats.Dispatch
	Steps      []stats.Dispatch
	Events     map[string][]ToolEvent
	RunLog     string
}

const maxChainHops = 16

// runIDFor resolves the run a dispatch belongs to: ext.run_id when present,
// otherwise the nearest complete task-runner ancestor via parent_dispatch_id.
func runIDFor(d stats.Dispatch, byID map[string]stats.Dispatch, roots map[string]bool) string {
	if v, ok := d.Ext["run_id"].(string); ok && roots[v] {
		return v
	}
	cur, hops := d, 0
	for hops < maxChainHops {
		if roots[cur.DispatchID] {
			return cur.DispatchID
		}
		parent, ok := byID[cur.ParentDispatchID]
		if !ok {
			return ""
		}
		cur = parent
		hops++
	}
	return ""
}

// BuildRuns groups dispatches into runs. Only complete task-runner dispatches
// are roots; dispatches with no runner ancestor are dropped. Runs are sorted
// by root timestamp ascending; Steps by timestamp then id; Events by Seq.
func BuildRuns(ds []stats.Dispatch, evs []ToolEvent) []Run {
	byID := make(map[string]stats.Dispatch, len(ds))
	roots := map[string]bool{}
	for _, d := range ds {
		// later rows (higher seq, or re-collection) win
		byID[d.DispatchID] = d
		if d.Role == "task-runner" && d.DispatchComplete {
			roots[d.DispatchID] = true
		}
	}
	runs := map[string]*Run{}
	for id := range roots {
		root := byID[id]
		r := &Run{ID: id, Root: root, Events: map[string][]ToolEvent{}}
		if rl, ok := root.Ext["run_log"].(string); ok {
			r.RunLog = rl
		}
		runs[id] = r
	}
	for _, d := range byID {
		rid := runIDFor(d, byID, roots)
		if rid == "" {
			continue
		}
		r := runs[rid]
		r.Dispatches = append(r.Dispatches, d)
		if d.ParentDispatchID == rid {
			r.Steps = append(r.Steps, d)
		}
	}
	for _, ev := range evs {
		for _, r := range runs {
			if _, in := indexOf(r.Dispatches, ev.DispatchID); in {
				r.Events[ev.DispatchID] = append(r.Events[ev.DispatchID], ev)
				break
			}
		}
	}
	out := make([]Run, 0, len(runs))
	for _, r := range runs {
		sort.Slice(r.Dispatches, func(i, j int) bool { return lessDispatch(r.Dispatches[i], r.Dispatches[j]) })
		sort.Slice(r.Steps, func(i, j int) bool { return lessDispatch(r.Steps[i], r.Steps[j]) })
		for id := range r.Events {
			evs := r.Events[id]
			sort.Slice(evs, func(i, j int) bool { return evs[i].Seq < evs[j].Seq })
		}
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return lessDispatch(out[i].Root, out[j].Root) })
	return out
}

func lessDispatch(a, b stats.Dispatch) bool {
	if !a.Timestamp.Equal(b.Timestamp) {
		return a.Timestamp.Before(b.Timestamp)
	}
	return a.DispatchID < b.DispatchID
}

func indexOf(ds []stats.Dispatch, id string) (int, bool) {
	for i, d := range ds {
		if d.DispatchID == id {
			return i, true
		}
	}
	return -1, false
}

// LatestRun returns the run with the newest root timestamp, or nil.
func LatestRun(runs []Run) *Run {
	if len(runs) == 0 {
		return nil
	}
	r := runs[len(runs)-1]
	return &r
}

// FindRun matches key against run ID (exact or suffix) or run_log (exact or suffix).
func FindRun(runs []Run, key string) *Run {
	if key == "" {
		return nil
	}
	for i := range runs {
		if runs[i].ID == key {
			return &runs[i]
		}
	}
	for i := range runs {
		r := runs[i]
		if r.RunLog != "" && (r.RunLog == key || strings.HasSuffix(strings.TrimSuffix(r.RunLog, ".md"), key)) {
			return &r
		}
		if strings.HasSuffix(r.ID, key) {
			return &r
		}
	}
	return nil
}
```

- [ ] **Step 11: Run all package tests**

Run: `cd cli && go test ./internal/score/ ./internal/manifest/ -count=1`
Expected: PASS.

- [ ] **Step 12: Commit**

```bash
git add cli/internal/score cli/internal/manifest
git commit -m "feat(cli): score package — config layering, tool-events reader, run grouping"
```

---

### Task 7: Go metrics P1–P7, scoring, tiers, attribution

**Files:**
- Create: `cli/internal/score/metrics.go`, `cli/internal/score/scoring.go`
- Test: `cli/internal/score/metrics_test.go`, `cli/internal/score/scoring_test.go`

**Interfaces:**
- Consumes: `Run`, `Config`, `ToolEvent` (Task 6); `stats.Dispatch`.
- Produces:
  ```go
  type Penalty struct {
      ID     string             `json:"id"`
      Value  float64            `json:"value"`   // raw metric: rate, count or share
      Points float64            `json:"points"`  // weight × min(1, value/saturation)
      ByRole map[string]float64 `json:"by_role"` // points attributed per role, sums to Points
      Detail string             `json:"detail,omitempty"`
  }
  type Tokens struct { Input, Output, CacheRead, CacheCreation int }  // json: input, output, cache_read, cache_creation
  func (t Tokens) Total() int
  type ToolCount struct { Tool string `json:"tool"`; Count int `json:"count"` }
  type RoleRow struct { Role string; Tokens int; Calls int; Errors int; Penalty float64; Model string } // json snake_case
  type Facts struct { Tokens Tokens; Dispatches int; ToolCalls int; ToolsTop []ToolCount; DurationMs int64; Route []string; Models map[string]string; ModelCounts map[string]int }
  type Inputs struct { Dispatches int; ToolEvents int; TranscriptsMissing []string; Confidence string }
  type Card struct { ScoreSchema int; ZprofVersion string; RunID, RunLog, SessionID, ProjectID, TsUTC, Verdict, Tier string; Score int; Penalties []Penalty; Roles []RoleRow; Facts Facts; Inputs Inputs; WeightsHash string }
  func Compute(run Run, cfg Config, zprofVersion string) Card
  func TierFor(verdict string, score int, th Thresholds) string // Ideal|Solid|Lucky|Blocked|Failed|Unknown
  func ShortModel(model string) string // "claude-sonnet-5" → "sonnet"
  ```
  JSON tags are snake_case versions of the field names (`score_schema`, `zprof_version`, `run_id`, `run_log`, `session_id`, `project_id`, `ts_utc`, `weights_hash`, `tool_calls`, `tools_top`, `duration_ms`, `model_counts`, `tool_events`, `transcripts_missing`).

- [ ] **Step 1: Write the failing golden test on the fixture**

Create `cli/internal/score/scoring_test.go`:

```go
package score

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func pointsByID(c Card) map[string]float64 {
	m := map[string]float64{}
	for _, p := range c.Penalties {
		m[p.ID] = p.Points
	}
	return m
}

func TestCompute_Run1Golden(t *testing.T) {
	runs := loadRun1(t)
	c := Compute(runs[0], Defaults(), "test")

	pts := pointsByID(c)
	require.InDelta(t, 20, pts["P1"], 0.01, "4/20 tool errors = 0.20 → saturated")
	require.InDelta(t, 10, pts["P2"], 0.01, "2 blind retries / 3")
	require.InDelta(t, 5, pts["P3"], 0.01, "1 reread / 4 reads = 0.25 → half of 10")
	require.InDelta(t, 10, pts["P4"], 0.01, "1 extra tester→implementer round / 2")
	require.InDelta(t, 0, pts["P5"], 0.01)
	require.InDelta(t, 5, pts["P6"], 0.01, "40k wasted / 400k = 0.10 → third of 15")
	require.InDelta(t, 5, pts["P7"], 0.01, "preamble + unparsed = 2 / 4")
	require.Equal(t, 45, c.Score)
	require.Equal(t, "Lucky", c.Tier)
	require.Equal(t, "done", c.Verdict)

	require.Equal(t, 310000, c.Facts.Tokens.Input)
	require.Equal(t, 70000, c.Facts.Tokens.Output)
	require.Equal(t, 20000, c.Facts.Tokens.CacheRead)
	require.Equal(t, 400000, c.Facts.Tokens.Total())
	require.Equal(t, 7, c.Facts.Dispatches, "children only")
	require.Equal(t, 20, c.Facts.ToolCalls)
	require.Equal(t, int64(1800000), c.Facts.DurationMs)
	require.Equal(t, []string{"planner", "implementer", "implementer", "tester", "implementer", "tester", "reviewer"}, c.Facts.Route)
	require.Equal(t, "claude-opus-5-5", c.Facts.Models["reviewer"])
	require.Equal(t, 7, c.Facts.ModelCounts["sonnet"])
	require.Equal(t, 1, c.Facts.ModelCounts["opus"])
	require.Equal(t, ToolCount{"Bash", 9}, c.Facts.ToolsTop[0])

	require.Equal(t, "full", c.Inputs.Confidence)
	require.Equal(t, 8, c.Inputs.Dispatches)
	require.Equal(t, 20, c.Inputs.ToolEvents)

	byRole := map[string]float64{}
	for _, r := range c.Roles {
		byRole[r.Role] = r.Penalty
	}
	require.InDelta(t, 50, byRole["implementer"], 0.01)
	require.InDelta(t, 5, byRole["tester"], 0.01)
	require.InDelta(t, 0, byRole["planner"], 0.01)
	require.Equal(t, "implementer", c.Roles[0].Role, "roles sorted by penalty desc")
	require.Equal(t, 1, c.ScoreSchema)
	require.Equal(t, Defaults().WeightsHash(), c.WeightsHash)
	require.Equal(t, ".zprof/runs/2026-09-26-fixture.md", c.RunLog)
}

func TestPenaltyAttributionSumsToPoints(t *testing.T) {
	runs := loadRun1(t)
	c := Compute(runs[0], Defaults(), "test")
	for _, p := range c.Penalties {
		sum := 0.0
		for _, v := range p.ByRole {
			sum += v
		}
		if p.Points > 0 {
			require.InDelta(t, p.Points, sum, 0.001, p.ID)
		}
	}
}

func TestTierFor(t *testing.T) {
	th := Thresholds{Ideal: 85, Solid: 60}
	cases := []struct {
		verdict string
		score   int
		want    string
	}{
		{"done", 85, "Ideal"}, {"done", 84, "Solid"}, {"done", 60, "Solid"}, {"done", 59, "Lucky"},
		{"approve", 90, "Ideal"}, {"approve-with-fixes", 70, "Solid"},
		{"blocked", 95, "Blocked"}, {"failed", 95, "Failed"}, {"", 95, "Unknown"}, {"weird", 10, "Unknown"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, TierFor(tc.verdict, tc.score, th), "%s/%d", tc.verdict, tc.score)
	}
}

func TestShortModel(t *testing.T) {
	require.Equal(t, "sonnet", ShortModel("claude-sonnet-5"))
	require.Equal(t, "opus", ShortModel("claude-opus-5-5"))
	require.Equal(t, "haiku", ShortModel("claude-haiku-4-5-20251001"))
	require.Equal(t, "gpt-5", ShortModel("gpt-5"))
	require.Equal(t, "?", ShortModel(""))
}

func TestCompute_ConfidencePartialListsRoles(t *testing.T) {
	root := mkDispatch("r", "task-runner", "", "done", "completed", "2026-09-26T10:00:00Z")
	tester := mkDispatch("t", "tester", "r", "done", "completed", "2026-09-26T10:05:00Z")
	tester.TranscriptCaptured = false
	run := BuildRuns([]stats.Dispatch{root, tester}, nil)[0]
	c := Compute(run, Defaults(), "test")
	require.Equal(t, "partial", c.Inputs.Confidence)
	require.Equal(t, []string{"tester"}, c.Inputs.TranscriptsMissing)
	require.Equal(t, 100, c.Score)
	require.Equal(t, "Ideal", c.Tier)
}
```

Add `"github.com/vaporphd/zprof/internal/stats"` to the imports and this helper (shared with `metrics_test.go`) at the bottom of `scoring_test.go`:

```go
// mkDispatch builds a complete, captured dispatch. parent "" = no parent.
func mkDispatch(id, role, parent, verdict, status, ts string) stats.Dispatch {
	d := stats.Dispatch{
		DispatchID: "claude-code:s:" + id, Role: role, Verdict: verdict, Status: status,
		DispatchComplete: true, TsUTC: ts, TranscriptCaptured: true, ModelResolved: "claude-sonnet-5",
		TokensInput: 1000, TokensOutput: 100,
	}
	if parent != "" {
		d.ParentDispatchID = "claude-code:s:" + parent
	}
	d.Timestamp, _ = time.Parse(time.RFC3339, ts)
	return d
}

func bptr(b bool) *bool { return &b }

func ev(seq int, tool, hash, target string, isErr bool) ToolEvent {
	return ToolEvent{DispatchID: "claude-code:s:x", Seq: seq, Tool: tool, InputHash: hash, Target: target, IsError: bptr(isErr)}
}
```

(import `"time"` too.)

- [ ] **Step 2: Write the failing metric edge-case tests**

Create `cli/internal/score/metrics_test.go`:

```go
package score

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vaporphd/zprof/internal/stats"
)

// oneDispatchRun: root + one implementer "x" carrying the given events.
func oneDispatchRun(events ...ToolEvent) Run {
	root := mkDispatch("r", "task-runner", "", "done", "completed", "2026-09-26T10:00:00Z")
	x := mkDispatch("x", "implementer", "r", "done", "completed", "2026-09-26T10:05:00Z")
	return BuildRuns([]stats.Dispatch{root, x}, events)[0]
}

func TestBlindRetries_CountsRepeatsAfterErrorWithoutMutation(t *testing.T) {
	run := oneDispatchRun(
		ev(1, "Bash", "h-test", "swift test", true),
		ev(2, "Bash", "h-test", "swift test", true),
		ev(3, "Bash", "h-test", "swift test", false),
	)
	p := computeP2(run, Defaults())
	require.Equal(t, 2.0, p.value)
	require.Equal(t, 2.0, p.byRole["implementer"])
}

func TestBlindRetries_BuildBetweenDoesNotReset(t *testing.T) {
	run := oneDispatchRun(
		ev(1, "Bash", "h-test", "swift test", true),
		ev(2, "Bash", "h-build", "swift build", false),
		ev(3, "Bash", "h-test", "swift test", true),
	)
	require.Equal(t, 1.0, computeP2(run, Defaults()).value)
}

func TestBlindRetries_EditBetweenResets(t *testing.T) {
	run := oneDispatchRun(
		ev(1, "Bash", "h-test", "swift test", true),
		ev(2, "Edit", "h-e", "/p/a.swift", false),
		ev(3, "Bash", "h-test", "swift test", true),
	)
	require.Equal(t, 0.0, computeP2(run, Defaults()).value)
}

func TestBlindRetries_MutatingCommandStillCounts(t *testing.T) {
	run := oneDispatchRun(
		ev(1, "Bash", "h-sed", "sed -i 's/a/b/' f", true),
		ev(2, "Bash", "h-sed", "sed -i 's/a/b/' f", true),
	)
	require.Equal(t, 1.0, computeP2(run, Defaults()).value, "check happens before the mutating reset")
}

func TestBlindRetries_MutatingBashResetsOthers(t *testing.T) {
	run := oneDispatchRun(
		ev(1, "Bash", "h-test", "swift test", true),
		ev(2, "Bash", "h-w", "cat > f.swift <<'EOF'", false),
		ev(3, "Bash", "h-test", "swift test", true),
	)
	require.Equal(t, 0.0, computeP2(run, Defaults()).value)
}

func TestRereads_SkipsDispatchesWithFewerThanFourReads(t *testing.T) {
	run := oneDispatchRun(
		ev(1, "Read", "a", "/a", false), ev(2, "Read", "a", "/a", false), ev(3, "Read", "b", "/b", false),
	)
	require.Equal(t, 0.0, computeP3(run, Defaults()).value)
}

func TestRereads_MutationResetsSeenPaths(t *testing.T) {
	run := oneDispatchRun(
		ev(1, "Read", "a", "/a", false), ev(2, "Read", "b", "/b", false),
		ev(3, "Edit", "e", "/a", false),
		ev(4, "Read", "a", "/a", false), ev(5, "Read", "c", "/c", false),
	)
	require.Equal(t, 0.0, computeP3(run, Defaults()).value, "4 reads, no reread after the edit")
}

func TestRereads_CountsRepeatWithoutMutation(t *testing.T) {
	run := oneDispatchRun(
		ev(1, "Read", "a", "/a", false), ev(2, "Read", "b", "/b", false),
		ev(3, "Read", "a", "/a", false), ev(4, "Read", "c", "/c", false),
	)
	p := computeP3(run, Defaults())
	require.InDelta(t, 0.25, p.value, 0.001)
	require.Equal(t, 1.0, p.byRole["implementer"])
}

func TestToolErrors_RateAndAttribution(t *testing.T) {
	root := mkDispatch("r", "task-runner", "", "done", "completed", "2026-09-26T10:00:00Z")
	a := mkDispatch("a", "implementer", "r", "done", "completed", "2026-09-26T10:01:00Z")
	b := mkDispatch("b", "tester", "r", "done", "completed", "2026-09-26T10:02:00Z")
	evs := []ToolEvent{
		{DispatchID: a.DispatchID, Seq: 1, Tool: "Bash", InputHash: "1", IsError: bptr(true)},
		{DispatchID: a.DispatchID, Seq: 2, Tool: "Bash", InputHash: "2", IsError: bptr(false)},
		{DispatchID: b.DispatchID, Seq: 1, Tool: "Bash", InputHash: "3", IsError: bptr(true)},
		{DispatchID: b.DispatchID, Seq: 2, Tool: "Grep", InputHash: "4"}, // no result → excluded from denominator
	}
	run := BuildRuns([]stats.Dispatch{root, a, b}, evs)[0]
	p := computeP1(run, Defaults())
	require.InDelta(t, 2.0/3.0, p.value, 0.001)
	require.Equal(t, 1.0, p.byRole["implementer"])
	require.Equal(t, 1.0, p.byRole["tester"])
}

func TestLoopRounds_OnlyFailedTesterFollowedByImplementer(t *testing.T) {
	root := mkDispatch("r", "task-runner", "", "done", "completed", "2026-09-26T10:00:00Z")
	steps := []stats.Dispatch{
		mkDispatch("1", "implementer", "r", "done", "completed", "2026-09-26T10:01:00Z"),
		mkDispatch("2", "tester", "r", "failed", "completed", "2026-09-26T10:02:00Z"),
		mkDispatch("3", "implementer", "r", "done", "completed", "2026-09-26T10:03:00Z"),
		mkDispatch("4", "tester", "r", "failed", "completed", "2026-09-26T10:04:00Z"),
		mkDispatch("5", "implementer", "r", "done", "completed", "2026-09-26T10:05:00Z"),
		mkDispatch("6", "tester", "r", "failed", "completed", "2026-09-26T10:06:00Z"),
		mkDispatch("7", "reviewer", "r", "block", "completed", "2026-09-26T10:07:00Z"), // failed tester → reviewer: not a round
	}
	run := BuildRuns(append([]stats.Dispatch{root}, steps...), nil)[0]
	p4 := computeP4(run, Defaults())
	require.Equal(t, 2.0, p4.value)
	require.Equal(t, 2.0, p4.byRole["implementer"])
	p5 := computeP5(run, Defaults())
	require.Equal(t, 1.0, p5.value)
	require.Equal(t, 1.0, p5.byRole["implementer"])
	c := Compute(run, Defaults(), "t")
	pts := pointsByID(c)
	require.InDelta(t, 20, pts["P4"], 0.01, "2 extra rounds saturate")
	require.InDelta(t, 5, pts["P5"], 0.01)
}

func TestWastedTokens_ExemptRolesAndKilled(t *testing.T) {
	root := mkDispatch("r", "task-runner", "", "done", "completed", "2026-09-26T10:00:00Z")
	root.TokensInput, root.TokensOutput = 0, 0
	impl := mkDispatch("i", "implementer", "r", "", "killed", "2026-09-26T10:01:00Z")
	impl.TokensInput, impl.TokensOutput = 3000, 0
	aud := mkDispatch("a", "auditor", "r", "", "completed", "2026-09-26T10:02:00Z")
	aud.ReturnParsed = bptr(false) // auditor contract starts with completion:, not verdict:
	aud.TokensInput, aud.TokensOutput = 5000, 0
	ok := mkDispatch("o", "tester", "r", "done", "completed", "2026-09-26T10:03:00Z")
	ok.TokensInput, ok.TokensOutput = 2000, 0
	run := BuildRuns([]stats.Dispatch{root, impl, aud, ok}, nil)[0]
	p := computeP6(run, Defaults())
	require.InDelta(t, 0.30, p.value, 0.001, "3000 of 10000")
	require.Equal(t, 3000.0, p.byRole["implementer"])
	_, hasAud := p.byRole["auditor"]
	require.False(t, hasAud)
}

func TestP7_BlockedDoesNotCountMissingArtifact(t *testing.T) {
	root := mkDispatch("r", "task-runner", "", "blocked", "completed", "2026-09-26T10:00:00Z")
	root.ArtifactExists = bptr(false)
	impl := mkDispatch("i", "implementer", "r", "blocked", "completed", "2026-09-26T10:01:00Z")
	impl.ArtifactExists = bptr(false)
	aud := mkDispatch("a", "auditor", "r", "", "completed", "2026-09-26T10:02:00Z")
	aud.HasPreamble = bptr(true) // exempt role → ignored
	run := BuildRuns([]stats.Dispatch{root, impl, aud}, nil)[0]
	require.Equal(t, 0.0, computeP7(run, Defaults()).value)
	c := Compute(run, Defaults(), "t")
	require.Equal(t, "Blocked", c.Tier)
	require.Equal(t, 100, c.Score)
}

func TestP7_CountsFourKinds(t *testing.T) {
	root := mkDispatch("r", "task-runner", "", "done", "completed", "2026-09-26T10:00:00Z")
	d := mkDispatch("d", "implementer", "r", "done", "completed", "2026-09-26T10:01:00Z")
	d.HasPreamble = bptr(true)
	d.ArtifactExists = bptr(false)
	d.NextIsReachable = bptr(false)
	d.ReturnParsed = bptr(false)
	run := BuildRuns([]stats.Dispatch{root, d}, nil)[0]
	p := computeP7(run, Defaults())
	require.Equal(t, 4.0, p.value)
	require.Equal(t, 4.0, p.byRole["implementer"])
	require.InDelta(t, 10, pointsByID(Compute(run, Defaults(), "t"))["P7"], 0.01)
}
```

- [ ] **Step 3: Run to verify failure**

Run: `cd cli && go test ./internal/score/ -count=1`
Expected: FAIL — `undefined: computeP2`, `undefined: Compute`, …

- [ ] **Step 4: Implement `metrics.go`**

```go
package score

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vaporphd/zprof/internal/stats"
)

// metric is the raw outcome of one P-check before weights are applied.
type metric struct {
	value  float64            // compared against Saturation[id]
	byRole map[string]float64 // numerator share per role (errors, retries, tokens, …)
	detail string             // human text for the card (RU)
}

func newMetric() metric { return metric{byRole: map[string]float64{}} }

// Penalty is one scored P-check.
type Penalty struct {
	ID     string             `json:"id"`
	Value  float64            `json:"value"`
	Points float64            `json:"points"`
	ByRole map[string]float64 `json:"by_role"`
	Detail string             `json:"detail,omitempty"`
}

// penaltyFrom applies weight × min(1, value/saturation) and splits the points
// across roles proportionally to their numerator share.
func penaltyFrom(id string, m metric, cfg Config) Penalty {
	p := Penalty{ID: id, Value: m.value, ByRole: map[string]float64{}, Detail: m.detail}
	sat := cfg.Saturation[id]
	if sat <= 0 || m.value <= 0 {
		return p
	}
	frac := m.value / sat
	if frac > 1 {
		frac = 1
	}
	p.Points = cfg.Weights[id] * frac
	total := 0.0
	for _, v := range m.byRole {
		total += v
	}
	if total > 0 {
		for r, v := range m.byRole {
			p.ByRole[r] = p.Points * v / total
		}
	}
	return p
}

func roleIndex(run Run) map[string]string {
	m := make(map[string]string, len(run.Dispatches))
	for _, d := range run.Dispatches {
		m[d.DispatchID] = d.Role
	}
	return m
}

func isMutating(e ToolEvent, cfg Config) bool {
	if cfg.MutatingTools[e.Tool] {
		return true
	}
	return e.Tool == "Bash" && cfg.IsMutatingBash(e.Target)
}

func isErr(e ToolEvent) bool { return e.IsError != nil && *e.IsError }

func tokens(d stats.Dispatch) int {
	return d.TokensInput + d.TokensOutput + d.TokensCacheRead + d.TokensCacheCreation
}

// P1 — tool error rate: errored / events that got a result.
func computeP1(run Run, cfg Config) metric {
	m := newMetric()
	roles := roleIndex(run)
	total, errs := 0, 0
	for id, evs := range run.Events {
		for _, e := range evs {
			if e.IsError == nil {
				continue
			}
			total++
			if *e.IsError {
				errs++
				m.byRole[roles[id]]++
			}
		}
	}
	if total > 0 {
		m.value = float64(errs) / float64(total)
	}
	m.detail = fmt.Sprintf("%d/%d tool errors (%.0f%%)", errs, total, m.value*100)
	return m
}

// P2 — blind retries: same (tool, input_hash) repeated after an error with no
// mutating event in between. Order per event: check, then reset-if-mutating, then record.
func computeP2(run Run, cfg Config) metric {
	m := newMetric()
	roles := roleIndex(run)
	targets := map[string]int{}
	for id, evs := range run.Events {
		lastErr := map[string]bool{}
		for _, e := range evs {
			key := e.Tool + "|" + e.InputHash
			if lastErr[key] {
				m.value++
				m.byRole[roles[id]]++
				targets[e.Target]++
			}
			if isMutating(e, cfg) {
				lastErr = map[string]bool{}
			}
			lastErr[key] = isErr(e)
		}
	}
	if m.value > 0 {
		m.detail = fmt.Sprintf("%s: %d× `%s` без правок между", topRole(m.byRole), int(m.value), truncate(topKey(targets), 40))
	}
	return m
}

// P3 — re-reads: Read of a path already read in this dispatch with no mutating
// event since. Only dispatches with ≥ 4 Reads count (noise floor).
func computeP3(run Run, cfg Config) metric {
	m := newMetric()
	roles := roleIndex(run)
	sumReads, sumRereads := 0, 0
	for id, evs := range run.Events {
		reads, rereads := 0, 0
		seen := map[string]bool{}
		for _, e := range evs {
			if isMutating(e, cfg) {
				seen = map[string]bool{}
				continue
			}
			if e.Tool != "Read" {
				continue
			}
			reads++
			if seen[e.Target] {
				rereads++
			}
			seen[e.Target] = true
		}
		if reads >= 4 {
			sumReads += reads
			sumRereads += rereads
			if rereads > 0 {
				m.byRole[roles[id]] += float64(rereads)
			}
		}
	}
	if sumReads > 0 {
		m.value = float64(sumRereads) / float64(sumReads)
	}
	if sumRereads > 0 {
		m.detail = fmt.Sprintf("%s: %d перечитываний из %d Read", topRole(m.byRole), sumRereads, sumReads)
	}
	return m
}

// P4 — tester `failed` immediately followed by an implementer step.
func computeP4(run Run, cfg Config) metric {
	m := newMetric()
	for i := 0; i+1 < len(run.Steps); i++ {
		s := run.Steps[i]
		if s.Role == "tester" && s.Verdict == "failed" && run.Steps[i+1].Role == "implementer" {
			m.value++
		}
	}
	if m.value > 0 {
		m.byRole["implementer"] = m.value
		m.detail = fmt.Sprintf("tester→implementer: %d лишн. раунд(ов)", int(m.value))
	}
	return m
}

// P5 — reviewer `block` verdicts.
func computeP5(run Run, cfg Config) metric {
	m := newMetric()
	for _, s := range run.Steps {
		if s.Role == "reviewer" && s.Verdict == "block" {
			m.value++
		}
	}
	if m.value > 0 {
		m.byRole["implementer"] = m.value
		m.detail = fmt.Sprintf("reviewer block ×%d", int(m.value))
	}
	return m
}

// P6 — share of run tokens spent in dispatches that produced nothing:
// status failed/killed, or an unparsable return (exempt roles excluded).
func computeP6(run Run, cfg Config) metric {
	m := newMetric()
	total, wasted := 0, 0
	for _, d := range run.Dispatches {
		tk := tokens(d)
		total += tk
		failed := d.Status == "failed" || d.Status == "killed"
		unparsed := d.ReturnParsed != nil && !*d.ReturnParsed && !cfg.ExemptRoles[d.Role]
		if failed || unparsed {
			wasted += tk
			m.byRole[d.Role] += float64(tk)
		}
	}
	if total > 0 {
		m.value = float64(wasted) / float64(total)
	}
	if wasted > 0 {
		m.detail = fmt.Sprintf("%.0f%% токенов в dispatch'ах без результата (%s)", m.value*100, joinRoles(m.byRole))
	}
	return m
}

// P7 — Class-A contract violations, exempt roles excluded; a missing artifact
// on a `blocked` verdict is not a violation.
func computeP7(run Run, cfg Config) metric {
	m := newMetric()
	for _, d := range run.Dispatches {
		if cfg.ExemptRoles[d.Role] {
			continue
		}
		n := 0
		if d.HasPreamble != nil && *d.HasPreamble {
			n++
		}
		if d.ArtifactExists != nil && !*d.ArtifactExists && d.Verdict != "blocked" {
			n++
		}
		if d.NextIsReachable != nil && !*d.NextIsReachable {
			n++
		}
		if d.ReturnParsed != nil && !*d.ReturnParsed {
			n++
		}
		if n > 0 {
			m.value += float64(n)
			m.byRole[d.Role] += float64(n)
		}
	}
	if m.value > 0 {
		m.detail = fmt.Sprintf("%d нарушений контракта (%s)", int(m.value), joinRoles(m.byRole))
	}
	return m
}

func topRole(byRole map[string]float64) string { return topKey(toIntMap(byRole)) }

func toIntMap(m map[string]float64) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = int(v)
	}
	return out
}

func topKey(counts map[string]int) string {
	best, bestN := "", -1
	for k, n := range counts {
		if n > bestN || (n == bestN && k < best) {
			best, bestN = k, n
		}
	}
	return best
}

func joinRoles(byRole map[string]float64) string {
	keys := make([]string, 0, len(byRole))
	for k := range byRole {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
```

- [ ] **Step 5: Implement `scoring.go`**

```go
package score

import (
	"math"
	"sort"
	"strings"

	"github.com/vaporphd/zprof/internal/stats"
)

const ScoreSchema = 1

type Tokens struct {
	Input         int `json:"input"`
	Output        int `json:"output"`
	CacheRead     int `json:"cache_read"`
	CacheCreation int `json:"cache_creation"`
}

func (t Tokens) Total() int { return t.Input + t.Output + t.CacheRead + t.CacheCreation }

type ToolCount struct {
	Tool  string `json:"tool"`
	Count int    `json:"count"`
}

type RoleRow struct {
	Role    string  `json:"role"`
	Tokens  int     `json:"tokens"`
	Calls   int     `json:"calls"`
	Errors  int     `json:"errors"`
	Penalty float64 `json:"penalty"`
	Model   string  `json:"model"`
}

type Facts struct {
	Tokens      Tokens            `json:"tokens"`
	Dispatches  int               `json:"dispatches"`
	ToolCalls   int               `json:"tool_calls"`
	ToolsTop    []ToolCount       `json:"tools_top"`
	DurationMs  int64             `json:"duration_ms"`
	Route       []string          `json:"route"`
	Models      map[string]string `json:"models"`
	ModelCounts map[string]int    `json:"model_counts"`
}

type Inputs struct {
	Dispatches         int      `json:"dispatches"`
	ToolEvents         int      `json:"tool_events"`
	TranscriptsMissing []string `json:"transcripts_missing"`
	Confidence         string   `json:"confidence"`
}

// Card is one row of .agentlog/scores.jsonl.
type Card struct {
	ScoreSchema  int       `json:"score_schema"`
	ZprofVersion string    `json:"zprof_version"`
	RunID        string    `json:"run_id"`
	RunLog       string    `json:"run_log,omitempty"`
	SessionID    string    `json:"session_id"`
	ProjectID    string    `json:"project_id"`
	TsUTC        string    `json:"ts_utc"`
	Verdict      string    `json:"verdict"`
	Tier         string    `json:"tier"`
	Score        int       `json:"score"`
	Penalties    []Penalty `json:"penalties"`
	Roles        []RoleRow `json:"roles"`
	Facts        Facts     `json:"facts"`
	Inputs       Inputs    `json:"inputs"`
	WeightsHash  string    `json:"weights_hash"`
}

// TierFor maps verdict + score onto the spec §6 tiers.
func TierFor(verdict string, score int, th Thresholds) string {
	switch {
	case verdict == "blocked":
		return "Blocked"
	case verdict == "failed":
		return "Failed"
	case verdict == "done" || verdict == "ok" || strings.HasPrefix(verdict, "approve"):
		if score >= th.Ideal {
			return "Ideal"
		}
		if score >= th.Solid {
			return "Solid"
		}
		return "Lucky"
	}
	return "Unknown"
}

// ShortModel: "claude-sonnet-5" → "sonnet", "claude-opus-5-5" → "opus"; unknown vendors pass through.
func ShortModel(model string) string {
	if model == "" {
		return "?"
	}
	parts := strings.Split(model, "-")
	if parts[0] == "claude" && len(parts) > 1 {
		return parts[1]
	}
	return model
}

// Compute scores one run under cfg.
func Compute(run Run, cfg Config, zprofVersion string) Card {
	checks := []func(Run, Config) metric{computeP1, computeP2, computeP3, computeP4, computeP5, computeP6, computeP7}
	penalties := make([]Penalty, 0, len(checks))
	sum := 0.0
	rolePenalty := map[string]float64{}
	for i, fn := range checks {
		p := penaltyFrom(PenaltyIDs[i], fn(run, cfg), cfg)
		penalties = append(penalties, p)
		sum += p.Points
		for r, v := range p.ByRole {
			rolePenalty[r] += v
		}
	}
	score := int(math.Round(100 - sum))
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	card := Card{
		ScoreSchema:  ScoreSchema,
		ZprofVersion: zprofVersion,
		RunID:        run.ID,
		RunLog:       run.RunLog,
		SessionID:    run.Root.SessionID,
		ProjectID:    run.Root.ProjectID,
		TsUTC:        run.Root.TsUTC,
		Verdict:      run.Root.Verdict,
		Score:        score,
		Tier:         TierFor(run.Root.Verdict, score, cfg.Thresholds),
		Penalties:    penalties,
		WeightsHash:  cfg.WeightsHash(),
	}
	card.Facts, card.Roles = facts(run, rolePenalty)
	card.Inputs = inputs(run)
	return card
}

func facts(run Run, rolePenalty map[string]float64) (Facts, []RoleRow) {
	f := Facts{Models: map[string]string{}, ModelCounts: map[string]int{}}
	rows := map[string]*RoleRow{}
	toolCounts := map[string]int{}
	var childDuration int64

	for _, d := range run.Dispatches {
		f.Tokens.Input += d.TokensInput
		f.Tokens.Output += d.TokensOutput
		f.Tokens.CacheRead += d.TokensCacheRead
		f.Tokens.CacheCreation += d.TokensCacheCreation
		if d.ModelResolved != "" {
			f.ModelCounts[ShortModel(d.ModelResolved)]++
		}
		if d.DispatchID != run.ID {
			f.Dispatches++
			childDuration += d.DurationMs
			if d.ModelResolved != "" {
				f.Models[d.Role] = d.ModelResolved
			}
		}
		row := rows[d.Role]
		if row == nil {
			row = &RoleRow{Role: d.Role}
			rows[d.Role] = row
		}
		row.Tokens += tokens(d)
		if d.ModelResolved != "" {
			row.Model = ShortModel(d.ModelResolved)
		}
		for _, e := range run.Events[d.DispatchID] {
			f.ToolCalls++
			toolCounts[e.Tool]++
			row.Calls++
			if isErr(e) {
				row.Errors++
			}
		}
	}
	for _, s := range run.Steps {
		f.Route = append(f.Route, s.Role)
	}
	f.DurationMs = run.Root.DurationMs
	if f.DurationMs == 0 {
		f.DurationMs = childDuration
	}
	for tool, n := range toolCounts {
		f.ToolsTop = append(f.ToolsTop, ToolCount{tool, n})
	}
	sort.Slice(f.ToolsTop, func(i, j int) bool {
		if f.ToolsTop[i].Count != f.ToolsTop[j].Count {
			return f.ToolsTop[i].Count > f.ToolsTop[j].Count
		}
		return f.ToolsTop[i].Tool < f.ToolsTop[j].Tool
	})
	if len(f.ToolsTop) > 5 {
		f.ToolsTop = f.ToolsTop[:5]
	}

	out := make([]RoleRow, 0, len(rows))
	for role, row := range rows {
		row.Penalty = rolePenalty[role]
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Penalty != out[j].Penalty {
			return out[i].Penalty > out[j].Penalty
		}
		if out[i].Tokens != out[j].Tokens {
			return out[i].Tokens > out[j].Tokens
		}
		return out[i].Role < out[j].Role
	})
	return f, out
}

func inputs(run Run) Inputs {
	in := Inputs{Dispatches: len(run.Dispatches), Confidence: "full", TranscriptsMissing: []string{}}
	for _, evs := range run.Events {
		in.ToolEvents += len(evs)
	}
	seen := map[string]bool{}
	for _, d := range run.Dispatches {
		truncated := d.TranscriptTruncated != nil && *d.TranscriptTruncated
		if (!d.TranscriptCaptured || truncated) && !seen[d.Role] {
			seen[d.Role] = true
			in.TranscriptsMissing = append(in.TranscriptsMissing, d.Role)
		}
	}
	sort.Strings(in.TranscriptsMissing)
	if len(in.TranscriptsMissing) > 0 {
		in.Confidence = "partial"
	}
	return in
}

// unused-import guard for stats in this file
var _ stats.Dispatch
```

- [ ] **Step 6: Run the package tests**

Run: `cd cli && go test ./internal/score/ -count=1 -v -run 'Compute|Blind|Rereads|ToolErrors|LoopRounds|Wasted|P7|Tier|ShortModel|Attribution'`
Expected: PASS. If `TestCompute_Run1Golden` fails on `ToolsTop[0]`, count Bash events in the fixture (9) — the assertion is correct; the sort must be count-desc then name-asc.

- [ ] **Step 7: Commit**

```bash
git add cli/internal/score
git commit -m "feat(cli): scorecard metrics P1-P7, tiers, per-role attribution"
```

---

### Task 8: Card rendering and persistence (`scores.jsonl`, run-log section)

**Files:**
- Create: `cli/internal/score/render.go`, `cli/internal/score/persist.go`
- Test: `cli/internal/score/render_test.go`, `cli/internal/score/persist_test.go`

**Interfaces:**
- Consumes: `Card` (Task 7), `fsutil.WriteFileAtomic(path, data, perm)`.
- Produces:
  ```go
  func RenderCard(c Card) string                              // multi-line text, spec §7
  func AppendScore(path string, c Card) error                 // append one JSON line + fsync
  func ReadScoredKeys(path string) (map[string]bool, error)   // "<run_id>|<weights_hash>" → true; missing file → empty
  func ScoreKey(c Card) string                                // c.RunID + "|" + c.WeightsHash
  func WriteRunLogSection(runLogPath, card string) error      // replace/append between markers; os.ErrNotExist if no run log
  const MarkerBegin = "<!-- zprof:score:begin -->"
  const MarkerEnd   = "<!-- zprof:score:end -->"
  ```

- [ ] **Step 1: Write the failing render test**

Create `cli/internal/score/render_test.go`:

```go
package score

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderCard_Run1(t *testing.T) {
	c := Compute(loadRun1(t)[0], Defaults(), "test")
	out := RenderCard(c)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	require.Equal(t, "Score 45/100 · Lucky · done · 2026-09-26-fixture · confidence full", lines[0])
	require.Equal(t, "400k tok (in 310k · out 70k · cache 20k) · 7 dispatch · 20 tool calls · 30 min · sonnet×7 opus×1", lines[1])
	// top-3 findings by points, desc
	require.True(t, strings.HasPrefix(lines[2], "−20 P1 "), lines[2])
	require.Contains(t, lines[2], "4/20 tool errors (20%)")
	require.True(t, strings.HasPrefix(lines[3], "−10 P"), lines[3]) // P2 or P4 (both 10) — order by id
	require.Contains(t, lines[3], "P2 implementer: 2× `swift test --package-path Packages/Core` без правок между")
	require.True(t, strings.HasPrefix(lines[4], "−10 P4 "), lines[4])
	require.Equal(t, "", lines[5])
	require.Equal(t, []string{"role", "tokens", "calls", "err", "penalty", "model"}, strings.Fields(lines[6]))
	require.Equal(t, []string{"implementer", "240k", "14", "3", "−50", "sonnet"}, strings.Fields(lines[7]))
	require.Equal(t, []string{"tester", "80k", "3", "1", "−5", "sonnet"}, strings.Fields(lines[8]))
	require.Contains(t, out, "tools: Bash 9 · Read 7 · Edit 3 · Write 1")
	require.NotContains(t, out, "missing transcripts")
	require.NotContains(t, out, "run log: missing")
}

func TestRenderCard_PartialAndNoRunLog(t *testing.T) {
	c := Card{Score: 100, Tier: "Ideal", Verdict: "done", RunID: "claude-code:s:abcdefgh",
		Inputs: Inputs{Confidence: "partial", TranscriptsMissing: []string{"tester"}},
		Facts:  Facts{ModelCounts: map[string]int{}}}
	out := RenderCard(c)
	require.Contains(t, out, "Score 100/100 · Ideal · done · abcdefgh · confidence partial")
	require.Contains(t, out, "missing transcripts: tester")
	require.Contains(t, out, "run log: missing")
	require.Contains(t, out, "no penalties")
}

func TestHumanTokens(t *testing.T) {
	require.Equal(t, "0", humanTokens(0))
	require.Equal(t, "999", humanTokens(999))
	require.Equal(t, "1k", humanTokens(1000))
	require.Equal(t, "20k", humanTokens(20000))
	require.Equal(t, "1.2M", humanTokens(1_234_000))
}
```

- [ ] **Step 2: Implement `render.go`**

```go
package score

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// RenderCard produces the terminal card (spec §7). The first four lines are
// what main pastes into followup.md.
func RenderCard(c Card) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Score %d/100 · %s · %s · %s · confidence %s\n",
		c.Score, c.Tier, orDash(c.Verdict), slug(c), orDash(c.Inputs.Confidence))
	fmt.Fprintf(&b, "%s tok (in %s · out %s · cache %s) · %d dispatch · %d tool calls · %s · %s\n",
		humanTokens(c.Facts.Tokens.Total()), humanTokens(c.Facts.Tokens.Input), humanTokens(c.Facts.Tokens.Output),
		humanTokens(c.Facts.Tokens.CacheRead+c.Facts.Tokens.CacheCreation),
		c.Facts.Dispatches, c.Facts.ToolCalls, humanDuration(c.Facts.DurationMs), modelSummary(c.Facts.ModelCounts))

	findings := make([]Penalty, 0, len(c.Penalties))
	for _, p := range c.Penalties {
		if p.Points > 0 {
			findings = append(findings, p)
		}
	}
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Points > findings[j].Points })
	if len(findings) > 3 {
		findings = findings[:3]
	}
	if len(findings) == 0 {
		b.WriteString("no penalties\n")
	}
	for _, p := range findings {
		fmt.Fprintf(&b, "−%d %s %s\n", roundInt(p.Points), p.ID, p.Detail)
	}
	b.WriteString("\n")

	if len(c.Roles) > 0 {
		b.WriteString("role         tokens  calls  err  penalty  model\n")
		for _, r := range c.Roles {
			fmt.Fprintf(&b, "%-12s %6s %6d %4d  %7s  %s\n",
				r.Role, humanTokens(r.Tokens), r.Calls, r.Errors, penaltyCell(r.Penalty), orDash(r.Model))
		}
	}
	if len(c.Facts.ToolsTop) > 0 {
		parts := make([]string, 0, len(c.Facts.ToolsTop))
		for _, tc := range c.Facts.ToolsTop {
			parts = append(parts, fmt.Sprintf("%s %d", tc.Tool, tc.Count))
		}
		fmt.Fprintf(&b, "tools: %s\n", strings.Join(parts, " · "))
	}
	if len(c.Inputs.TranscriptsMissing) > 0 {
		fmt.Fprintf(&b, "missing transcripts: %s\n", strings.Join(c.Inputs.TranscriptsMissing, ", "))
	}
	if c.RunLog == "" {
		b.WriteString("run log: missing\n")
	}
	return b.String()
}

func slug(c Card) string {
	if c.RunLog != "" {
		return strings.TrimSuffix(filepath.Base(c.RunLog), ".md")
	}
	if n := len(c.RunID); n > 8 {
		return c.RunID[n-8:]
	}
	return orDash(c.RunID)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func roundInt(f float64) int { return int(f + 0.5) }

func penaltyCell(p float64) string {
	if p <= 0 {
		return "0"
	}
	return fmt.Sprintf("−%d", roundInt(p))
}

func humanTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e6), ".0") + "M"
	case n >= 1000:
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

func humanDuration(ms int64) string {
	if ms <= 0 {
		return "—"
	}
	min := ms / 60000
	if min < 1 {
		return fmt.Sprintf("%ds", ms/1000)
	}
	if min >= 120 {
		return fmt.Sprintf("%.1fh", float64(min)/60)
	}
	return fmt.Sprintf("%d min", min)
}

func modelSummary(counts map[string]int) string {
	if len(counts) == 0 {
		return "—"
	}
	type kv struct {
		k string
		v int
	}
	items := make([]kv, 0, len(counts))
	for k, v := range counts {
		items = append(items, kv{k, v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].v != items[j].v {
			return items[i].v > items[j].v
		}
		return items[i].k < items[j].k
	})
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%s×%d", it.k, it.v))
	}
	return strings.Join(parts, " ")
}
```

- [ ] **Step 3: Run the render tests**

Run: `cd cli && go test ./internal/score/ -run 'Render|HumanTokens' -count=1`
Expected: PASS. (Table rows are compared column-by-column via `strings.Fields`, so padding width is free to change; column order and values are not.)

- [ ] **Step 4: Write the failing persist tests**

Create `cli/internal/score/persist_test.go`:

```go
package score

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAppendScoreAndReadScoredKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nested", "scores.jsonl")
	keys, err := ReadScoredKeys(p)
	require.NoError(t, err)
	require.Empty(t, keys, "missing file is not an error")

	c := Compute(loadRun1(t)[0], Defaults(), "test")
	require.NoError(t, AppendScore(p, c))
	require.NoError(t, AppendScore(p, c))
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	require.Len(t, strings.Split(strings.TrimRight(string(data), "\n"), "\n"), 2)
	require.Contains(t, string(data), `"score":45`)

	keys, err = ReadScoredKeys(p)
	require.NoError(t, err)
	require.True(t, keys[ScoreKey(c)])
	require.Equal(t, c.RunID+"|"+c.WeightsHash, ScoreKey(c))
}

func TestWriteRunLogSection_Idempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "run.md")
	require.NoError(t, os.WriteFile(p, []byte("# task\n| t | a | v | x |\n\n## Итог\nverdict: done · artifact: PR #1\n"), 0o644))
	require.NoError(t, WriteRunLogSection(p, "Score 45/100 · Lucky\n"))
	require.NoError(t, WriteRunLogSection(p, "Score 72/100 · Solid\n"))
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	s := string(data)
	require.Equal(t, 1, strings.Count(s, MarkerBegin))
	require.Equal(t, 1, strings.Count(s, MarkerEnd))
	require.Contains(t, s, "Score 72/100")
	require.NotContains(t, s, "Score 45/100")
	require.True(t, strings.HasPrefix(s, "# task\n"), "original content preserved")
	require.Contains(t, s, "## Итог\nverdict: done")
	require.True(t, strings.Index(s, MarkerBegin) > strings.Index(s, "## Итог"), "section goes after ## Итог")
}

func TestWriteRunLogSection_MissingFile(t *testing.T) {
	err := WriteRunLogSection(filepath.Join(t.TempDir(), "nope.md"), "x")
	require.ErrorIs(t, err, os.ErrNotExist)
}
```

- [ ] **Step 5: Implement `persist.go`**

```go
package score

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vaporphd/zprof/internal/fsutil"
)

const (
	MarkerBegin = "<!-- zprof:score:begin -->"
	MarkerEnd   = "<!-- zprof:score:end -->"
)

// ScoreKey identifies a scored (run, parameters) pair.
func ScoreKey(c Card) string { return c.RunID + "|" + c.WeightsHash }

// AppendScore appends one JSON line to scores.jsonl (append-only; readers
// take the latest row per ScoreKey).
func AppendScore(path string, c Card) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal score: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return f.Sync()
}

// ReadScoredKeys returns the set of ScoreKey values already present.
func ReadScoredKeys(path string) (map[string]bool, error) {
	keys := map[string]bool{}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return keys, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)
	for sc.Scan() {
		var row struct {
			RunID       string `json:"run_id"`
			WeightsHash string `json:"weights_hash"`
		}
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil || row.RunID == "" {
			continue
		}
		keys[row.RunID+"|"+row.WeightsHash] = true
	}
	return keys, sc.Err()
}

// WriteRunLogSection puts the card between markers in the run log: replaces
// an existing section in place, otherwise appends one at the end.
func WriteRunLogSection(runLogPath, card string) error {
	data, err := os.ReadFile(runLogPath)
	if err != nil {
		return fmt.Errorf("read run log: %w", err)
	}
	s := string(data)
	section := MarkerBegin + "\n## Score\n```\n" + strings.TrimRight(card, "\n") + "\n```\n" + MarkerEnd
	if i := strings.Index(s, MarkerBegin); i >= 0 {
		if j := strings.Index(s[i:], MarkerEnd); j >= 0 {
			s = s[:i] + section + s[i+j+len(MarkerEnd):]
		} else {
			s = s[:i] + section + "\n"
		}
	} else {
		if !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		s += "\n" + section + "\n"
	}
	return fsutil.WriteFileAtomic(runLogPath, []byte(s), 0o644)
}
```

- [ ] **Step 6: Run the whole package**

Run: `cd cli && go test ./internal/score/ -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cli/internal/score
git commit -m "feat(cli): scorecard card rendering, scores.jsonl append, run-log section"
```

---

### Task 9: `zprof score` command

**Files:**
- Create: `cli/internal/cmd/score.go`, `cli/internal/cmd/score_test.go`
- Modify: `cli/cmd/zprof/main.go` (register `cmd.NewScoreCmd(version)`)

**Interfaces:**
- Consumes: `score.LoadConfig`, `stats.ReadDispatches`, `score.ReadToolEvents`, `score.BuildRuns`, `score.LatestRun`, `score.FindRun`, `score.Compute`, `score.RenderCard`, `score.AppendScore`, `score.ReadScoredKeys`, `score.ScoreKey`, `score.WriteRunLogSection`, `eval.LocateSession`.
- Produces: `func NewScoreCmd(version string) *cobra.Command`. Flags: `--latest` (default true when no other selector), `--run <id|run_log|slug>`, `--all-missing`, `--json`, `--no-collect`, `--quiet`, `--project <dir>` (default cwd), `--agentlog <dir>` (default `<project>/.agentlog`). Exit code 0 on "no run to score" (prints `unscored: …`).

- [ ] **Step 1: Write the failing command test**

Create `cli/internal/cmd/score_test.go`:

```go
package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func scoreFixtureDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "score", "testdata", "run1")
}

func setupScoreProject(t *testing.T) (proj, agentlog string) {
	t.Helper()
	proj = t.TempDir()
	agentlog = filepath.Join(proj, ".agentlog")
	require.NoError(t, os.MkdirAll(agentlog, 0o755))
	for _, name := range []string{"dispatches.jsonl", "tool-events.jsonl", "schema.json"} {
		data, err := os.ReadFile(filepath.Join(scoreFixtureDir(), name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(agentlog, name), data, 0o644))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(proj, ".zprof", "runs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof", "runs", "2026-09-26-fixture.md"),
		[]byte("# fixture\n\n## Итог\nverdict: done\n"), 0o644))
	return proj, agentlog
}

func runScore(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := NewScoreCmd("test")
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(args)
	err := c.Execute()
	return out.String(), err
}

func TestScoreCmd_LatestPrintsCardAndPersists(t *testing.T) {
	proj, agentlog := setupScoreProject(t)
	out, err := runScore(t, "--project", proj, "--no-collect")
	require.NoError(t, err)
	require.Contains(t, out, "Score 45/100 · Lucky · done · 2026-09-26-fixture")

	scores, err := os.ReadFile(filepath.Join(agentlog, "scores.jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(scores), `"run_id":"claude-code:s1:t0"`)

	runLog, err := os.ReadFile(filepath.Join(proj, ".zprof", "runs", "2026-09-26-fixture.md"))
	require.NoError(t, err)
	require.Contains(t, string(runLog), "<!-- zprof:score:begin -->")
	require.Contains(t, string(runLog), "Score 45/100")
}

func TestScoreCmd_JSON(t *testing.T) {
	proj, _ := setupScoreProject(t)
	out, err := runScore(t, "--project", proj, "--no-collect", "--json")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(strings.TrimSpace(out), "{"), out)
	require.Contains(t, out, `"tier":"Lucky"`)
}

func TestScoreCmd_RunSelectorAndQuiet(t *testing.T) {
	proj, agentlog := setupScoreProject(t)
	out, err := runScore(t, "--project", proj, "--no-collect", "--quiet", "--run", "2026-09-26-fixture")
	require.NoError(t, err)
	require.Equal(t, "", out)
	_, err = os.Stat(filepath.Join(agentlog, "scores.jsonl"))
	require.NoError(t, err)
}

func TestScoreCmd_AllMissingIsIdempotent(t *testing.T) {
	proj, agentlog := setupScoreProject(t)
	_, err := runScore(t, "--project", proj, "--no-collect", "--all-missing", "--quiet")
	require.NoError(t, err)
	out, err := runScore(t, "--project", proj, "--no-collect", "--all-missing")
	require.NoError(t, err)
	require.Contains(t, out, "nothing to score")
	data, _ := os.ReadFile(filepath.Join(agentlog, "scores.jsonl"))
	require.Len(t, strings.Split(strings.TrimRight(string(data), "\n"), "\n"), 1)
}

func TestScoreCmd_NoRunIsNotAnError(t *testing.T) {
	proj := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(proj, ".agentlog"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".agentlog", "dispatches.jsonl"), []byte(""), 0o644))
	out, err := runScore(t, "--project", proj, "--no-collect")
	require.NoError(t, err)
	require.Contains(t, out, "unscored: no task-runner dispatch")
}

func TestScoreCmd_DisabledInManifest(t *testing.T) {
	proj, _ := setupScoreProject(t)
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".zprof.yaml"), []byte("overlays: []\nscore:\n  enabled: false\n"), 0o644))
	out, err := runScore(t, "--project", proj, "--no-collect")
	require.NoError(t, err)
	require.Contains(t, out, "score disabled")
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd cli && go test ./internal/cmd/ -run ScoreCmd -count=1`
Expected: FAIL — `undefined: NewScoreCmd`.

- [ ] **Step 3: Implement `cmd/score.go`**

```go
// cli/internal/cmd/score.go
package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vaporphd/zprof/internal/eval"
	"github.com/vaporphd/zprof/internal/score"
	"github.com/vaporphd/zprof/internal/stats"
)

// NewScoreCmd returns `zprof score` — the per-task scorecard (spec
// docs/superpowers/specs/2026-09-26-task-scorecard-design.md §8).
func NewScoreCmd(version string) *cobra.Command {
	var (
		latest     bool
		runKey     string
		allMissing bool
		asJSON     bool
		noCollect  bool
		quiet      bool
		projectDir string
		agentlog   string
	)
	c := &cobra.Command{
		Use:   "score",
		Short: "Per-task scorecard (0–100) for the latest task-runner run",
		Long: `Reads .agentlog/dispatches.jsonl and tool-events.jsonl, groups dispatches
into task-runner runs, scores penalties P1–P7 and prints a card. Appends the
result to .agentlog/scores.jsonl and writes a ## Score section into the run
log. Deterministic — no LLM, zero tokens.

Unless --no-collect is given, the collector is flushed first so the current
session's finished dispatches are visible immediately.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			var err error
			if projectDir == "" {
				if projectDir, err = os.Getwd(); err != nil {
					return fmt.Errorf("getwd: %w", err)
				}
			}
			if agentlog == "" {
				agentlog = filepath.Join(projectDir, ".agentlog")
			}

			cfg, err := score.LoadConfig(projectDir, agentlog)
			if err != nil {
				return err
			}
			if !cfg.Enabled {
				if !quiet {
					fmt.Fprintln(out, "score disabled in .zprof.yaml (score.enabled: false)")
				}
				return nil
			}

			if !noCollect {
				if err := flushCollector(projectDir); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warn: collector flush skipped: %v\n", err)
				}
			}

			ds, _, err := stats.ReadDispatches(filepath.Join(agentlog, "dispatches.jsonl"))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("read dispatches: %w", err)
			}
			evs, err := score.ReadToolEvents(filepath.Join(agentlog, "tool-events.jsonl"))
			if err != nil {
				return err
			}
			runs := score.BuildRuns(ds, evs)

			var targets []score.Run
			switch {
			case allMissing:
				keys, err := score.ReadScoredKeys(filepath.Join(agentlog, "scores.jsonl"))
				if err != nil {
					return err
				}
				hash := cfg.WeightsHash()
				for _, r := range runs {
					if !keys[r.ID+"|"+hash] {
						targets = append(targets, r)
					}
				}
				if len(targets) == 0 {
					if !quiet {
						fmt.Fprintln(out, "nothing to score: every complete run already has a row for the current weights")
					}
					return nil
				}
			case runKey != "":
				r := score.FindRun(runs, runKey)
				if r == nil {
					return fmt.Errorf("no run matches %q (have %d runs)", runKey, len(runs))
				}
				targets = []score.Run{*r}
			default:
				_ = latest
				r := score.LatestRun(runs)
				if r == nil {
					if !quiet {
						fmt.Fprintln(out, "unscored: no task-runner dispatch in .agentlog (main dispatched directly, or run not finished)")
					}
					return nil
				}
				targets = []score.Run{*r}
			}

			for i, r := range targets {
				card := score.Compute(r, cfg, version)
				if err := score.AppendScore(filepath.Join(agentlog, "scores.jsonl"), card); err != nil {
					return err
				}
				rendered := score.RenderCard(card)
				if card.RunLog != "" {
					runLogPath := card.RunLog
					if !filepath.IsAbs(runLogPath) {
						runLogPath = filepath.Join(projectDir, runLogPath)
					}
					if err := score.WriteRunLogSection(runLogPath, rendered); err != nil && !errors.Is(err, os.ErrNotExist) {
						return err
					}
				}
				if quiet {
					continue
				}
				if asJSON {
					data, _ := json.Marshal(card)
					fmt.Fprintln(out, string(data))
				} else {
					if i > 0 {
						fmt.Fprintln(out, strings.Repeat("─", 60))
					}
					fmt.Fprint(out, rendered)
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&latest, "latest", true, "Score the most recent complete run (default)")
	c.Flags().StringVar(&runKey, "run", "", "Score a specific run: run_id, run log path, or slug suffix")
	c.Flags().BoolVar(&allMissing, "all-missing", false, "Score every complete run without a row for the current weights")
	c.Flags().BoolVar(&asJSON, "json", false, "Print the scores.jsonl row instead of the card")
	c.Flags().BoolVar(&noCollect, "no-collect", false, "Do not flush the collector first")
	c.Flags().BoolVar(&quiet, "quiet", false, "Persist only, print nothing (for hooks)")
	c.Flags().StringVar(&projectDir, "project", "", "Project directory (default: cwd)")
	c.Flags().StringVar(&agentlog, "agentlog", "", "Telemetry directory (default: <project>/.agentlog)")
	return c
}

// flushCollector runs the deployed collector in `stop` mode against the most
// recent session log for projectDir, feeding it the same JSON payload the
// Claude Code hook would. Missing collector or session is reported, not fatal.
func flushCollector(projectDir string) error {
	script := filepath.Join(projectDir, ".claude", "zprof-collect.py")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("no collector at %s", script)
	}
	transcript, err := eval.LocateSession("", projectDir)
	if err != nil {
		return err
	}
	sessionID := strings.TrimSuffix(filepath.Base(transcript), ".jsonl")
	payload, _ := json.Marshal(map[string]any{
		"session_id":      sessionID,
		"transcript_path": transcript,
		"cwd":             projectDir,
	})
	cmd := exec.Command("python3", script, "stop")
	cmd.Dir = projectDir
	cmd.Stdin = bytes.NewReader(payload)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("collector: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
```

- [ ] **Step 4: Register the command**

In `cli/cmd/zprof/main.go` add after `root.AddCommand(cmd.NewStatsCmd())`:

```go
	root.AddCommand(cmd.NewScoreCmd(version))
```

- [ ] **Step 5: Run tests and a real invocation**

Run: `cd cli && go test ./internal/cmd/ -run ScoreCmd -count=1 && go build ./... && go run ./cmd/zprof score --help | head -5`
Expected: tests PASS; help text prints.

- [ ] **Step 6: Commit**

```bash
git add cli/internal/cmd/score.go cli/internal/cmd/score_test.go cli/cmd/zprof/main.go
git commit -m "feat(cli): zprof score — per-task scorecard command"
```

---

### Task 10: Stop hook chains `zprof score` after the collector

**Files:**
- Modify: `cli/internal/apply/settings.go`
- Test: `cli/internal/apply/settings_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `.claude/settings.local.json` `Stop` hook command = collector guard command, then `; command -v zprof >/dev/null 2>&1 && cd "$CLAUDE_PROJECT_DIR" && zprof score --latest --quiet --no-collect || true`. Old installs (Stop command without `zprof score`) are upgraded in place on the next `zprof apply` / `zprof sync`.

Why one chained command instead of a second hook entry: Claude Code runs the hooks of one event in parallel; `zprof score --no-collect` must run *after* the collector has written `.agentlog/`.

- [ ] **Step 1: Write the failing tests**

Append to `cli/internal/apply/settings_test.go`:

```go
func stopCommands(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	require.NoError(t, err)
	var settings map[string]any
	require.NoError(t, json.Unmarshal(data, &settings))
	entries := settings["hooks"].(map[string]any)["Stop"].([]any)
	var cmds []string
	for _, e := range entries {
		for _, h := range e.(map[string]any)["hooks"].([]any) {
			cmds = append(cmds, h.(map[string]any)["command"].(string))
		}
	}
	return cmds
}

func TestEnsureHooksStopChainsScore(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, EnsureHooks(dir))
	cmds := stopCommands(t, dir)
	require.Len(t, cmds, 1, "one chained command, not two parallel hooks")
	c := cmds[0]
	require.Contains(t, c, `zprof-collect.py" stop`)
	require.Contains(t, c, "zprof score --latest --quiet --no-collect")
	require.Less(t, strings.Index(c, "zprof-collect.py"), strings.Index(c, "zprof score"), "collector runs first")
	require.Contains(t, c, `command -v zprof >/dev/null 2>&1 &&`, "score is skipped when zprof is not installed")
	require.True(t, strings.HasSuffix(c, "|| true"))

	// other events do not get the score chain
	data, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	require.Equal(t, 1, strings.Count(string(data), "zprof score"))
}

func TestEnsureHooksUpgradesOldStopCommand(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))
	old := map[string]any{"hooks": map[string]any{
		"Stop": []any{map[string]any{"hooks": []any{map[string]any{
			"type":    "command",
			"command": `test -x "$CLAUDE_PROJECT_DIR/.claude/zprof-collect.py" && "$CLAUDE_PROJECT_DIR/.claude/zprof-collect.py" stop || true`,
		}}}},
	}}
	data, _ := json.MarshalIndent(old, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), data, 0o644))

	require.NoError(t, EnsureHooks(dir))
	cmds := stopCommands(t, dir)
	require.Len(t, cmds, 1, "old entry replaced, not duplicated")
	require.Contains(t, cmds[0], "zprof score --latest --quiet --no-collect")

	require.NoError(t, EnsureHooks(dir)) // and it stays stable
	require.Len(t, stopCommands(t, dir), 1)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd cli && go test ./internal/apply/ -run 'EnsureHooks' -count=1`
Expected: FAIL — `TestEnsureHooksStopChainsScore`: command lacks "zprof score".

- [ ] **Step 3: Implement**

In `settings.go`, replace the `telemetryHooks` map and the loop body with an ordered spec list plus an upsert that replaces a stale zprof entry:

```go
// scoreHookCommand runs the per-task scorecard after the collector. It is
// chained into the same Stop command because Claude Code runs an event's
// hooks in parallel, and `zprof score --no-collect` must see the collector's
// output. `command -v zprof` keeps projects without the binary silent.
const scoreHookCommand = `command -v zprof >/dev/null 2>&1 && cd "$CLAUDE_PROJECT_DIR" && zprof score --latest --quiet --no-collect || true`

type hookSpec struct {
	event   string
	command string
}

// telemetryHooks lists, in install order, the command each hook event runs.
var telemetryHooks = []hookSpec{
	{"SubagentStop", fmt.Sprintf(hookGuardTemplate, "subagent-stop")},
	{"Stop", fmt.Sprintf(hookGuardTemplate, "stop") + "; " + scoreHookCommand},
	{"SessionStart", fmt.Sprintf(hookGuardTemplate, "session-start")},
}
```

Replace the `for event, mode := range telemetryHooks { … }` loop with:

```go
	for _, spec := range telemetryHooks {
		entry := map[string]any{
			"hooks": []any{
				map[string]any{"type": "command", "command": spec.command},
			},
		}
		existing, _ := hooks[spec.event].([]any)
		if idx := zprofHookIndex(existing); idx >= 0 {
			if hookCommand(existing[idx]) != spec.command {
				existing[idx] = entry // stale zprof hook (older command shape) → upgrade in place
				hooks[spec.event] = existing
			}
			continue
		}
		hooks[spec.event] = append(existing, entry)
	}
```

Replace `hasZprofHook` with:

```go
// zprofHookIndex returns the position of the entry that invokes
// zprof-collect.py, or -1. Used both to skip duplicates and to upgrade a
// stale command in place.
func zprofHookIndex(entries []any) int {
	for i, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "zprof-collect.py") {
			return i
		}
	}
	return -1
}

// hookCommand extracts the first command string of a hook entry ("" if malformed).
func hookCommand(entry any) string {
	m, _ := entry.(map[string]any)
	hs, _ := m["hooks"].([]any)
	if len(hs) == 0 {
		return ""
	}
	h, _ := hs[0].(map[string]any)
	s, _ := h["command"].(string)
	return s
}
```

Update the doc comment on `EnsureHooks` to mention the Stop chain and the in-place upgrade.

- [ ] **Step 4: Run the apply tests**

Run: `cd cli && go test ./internal/apply/ -count=1`
Expected: PASS. `TestEnsureHooksIdempotent` still expects 6 occurrences of `zprof-collect.py` — the Stop chain keeps exactly two.

- [ ] **Step 5: Verify on a real project (dry, no commit there)**

Run: `cd /Volumes/mydata/projects/apple-health-sync && zprof sync --help >/dev/null && go run /Volumes/mydata/projects/zprof/cli/cmd/zprof apply ios-swift --dry-run`
Expected: exit 0. (Actual hook rewrite happens on the next real `zprof apply`/`sync`, outside this plan.)

- [ ] **Step 6: Commit**

```bash
git add cli/internal/apply/settings.go cli/internal/apply/settings_test.go
git commit -m "feat(cli): Stop hook chains zprof score after the collector; upgrade stale hook in place"
```

---

### Task 11: AGENT_LOOP rule + cross-language smoke test

**Files:**
- Modify: `profiles/base/agent-loop-router.md` (result table row `done`, isolation rule 2)
- Test: `profiles/base/tests/test_e2e_score.py` (new; runs the collector on a nested fixture, then `go run ./cmd/zprof score`)

**Interfaces:**
- Consumes: everything above.
- Produces: the main-session rule that closes the loop (spec §7 «followup.md»).

- [ ] **Step 1: Edit the router**

In `profiles/base/agent-loop-router.md`, replace the `done` row of the «Что делать с результатом» table:

```markdown
| `done` | Сообщи пользователю `one_line` и `artifact`. Выполни `zprof score` и покажи карточку. Ничего не цитируй сверх этого. |
```

Replace isolation rule 2:

```markdown
2. После каждого dispatch раннера выполни `zprof score` (без аргументов) и
   впиши первые 4 строки карточки в `followup.md` вместо строк предыдущего
   run. Если `zprof` недоступен — ≤3 строки статуса, как раньше. Ответ
   раннера выброси из рабочей памяти. Балл — детерминированный, из
   `.agentlog/`; не пересчитывай его словами и не спорь с ним в followup.
```

- [ ] **Step 2: Write the smoke test**

Create `profiles/base/tests/test_e2e_score.py`:

```python
#!/usr/bin/env python3
"""Cross-language smoke: collector → .agentlog → `zprof score` card.

Skipped when `go` is not on PATH. Builds the nested-runner fixture from
test_nested_dispatches, runs the collector in `stop` mode via subprocess
(exactly like the hook), then runs the Go command against the result.
"""
import json, os, pathlib, shutil, subprocess, sys
import importlib
import pytest

HERE = pathlib.Path(__file__).parent
REPO = HERE.parent.parent.parent          # …/zprof
COLLECTOR = HERE.parent / "zprof-collect.py"

_spec = importlib.util.spec_from_file_location("test_nested_dispatches", HERE / "test_nested_dispatches.py")
nested = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(nested)


@pytest.mark.skipif(shutil.which("go") is None, reason="go toolchain not installed")
def test_collector_then_zprof_score(tmp_path):
    proj = tmp_path / "proj"
    proj.mkdir()
    logs = tmp_path / "logs"
    logs.mkdir()
    main, sub = nested._runner_tree(logs)
    # give the implementer child a failing test loop so the card has a finding
    child_transcript = sub / "agent-aaaa01.jsonl"
    lines = child_transcript.read_text().splitlines()
    lines.insert(1, json.dumps({"type": "assistant", "timestamp": "2026-09-26T10:03:00Z",
                                "message": {"role": "assistant", "model": "claude-sonnet-5",
                                            "content": [{"type": "tool_use", "id": "tuA", "name": "Bash",
                                                         "input": {"command": "swift test"}}],
                                            "usage": {"input_tokens": 1, "output_tokens": 1}}}))
    lines.insert(2, json.dumps({"type": "user", "timestamp": "2026-09-26T10:03:05Z",
                                "message": {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tuA",
                                                                          "content": "error", "is_error": True}]}}))
    lines.insert(3, json.dumps({"type": "assistant", "timestamp": "2026-09-26T10:03:10Z",
                                "message": {"role": "assistant", "model": "claude-sonnet-5",
                                            "content": [{"type": "tool_use", "id": "tuB", "name": "Bash",
                                                         "input": {"command": "swift test"}}],
                                            "usage": {"input_tokens": 1, "output_tokens": 1}}}))
    lines.insert(4, json.dumps({"type": "user", "timestamp": "2026-09-26T10:03:15Z",
                                "message": {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tuB",
                                                                          "content": "error", "is_error": True}]}}))
    child_transcript.write_text("\n".join(lines) + "\n")

    payload = json.dumps({"session_id": main.stem, "transcript_path": str(main), "cwd": str(proj)})
    r = subprocess.run([sys.executable, str(COLLECTOR), "stop"], input=payload, text=True,
                       capture_output=True, cwd=str(proj), timeout=60)
    assert r.returncode == 0, r.stderr
    agentlog = proj / ".agentlog"
    assert (agentlog / "dispatches.jsonl").exists()
    assert (agentlog / "tool-events.jsonl").exists(), (agentlog / "collect.log").read_text() if (agentlog / "collect.log").exists() else "no log"

    r = subprocess.run(["go", "run", "./cmd/zprof", "score", "--project", str(proj), "--no-collect"],
                       cwd=str(REPO / "cli"), capture_output=True, text=True, timeout=300)
    assert r.returncode == 0, r.stderr
    out = r.stdout
    assert out.startswith("Score "), out
    assert "/100 ·" in out and "· done ·" in out
    assert "P2 implementer: 1×" in out, out          # the blind retry we injected
    assert "implementer" in out and "task-runner" in out
    rows = [json.loads(l) for l in (agentlog / "scores.jsonl").read_text().splitlines()]
    assert rows[-1]["tier"] in ("Ideal", "Solid", "Lucky")
    assert rows[-1]["penalties"][1]["id"] == "P2" and rows[-1]["penalties"][1]["value"] == 1
```

- [ ] **Step 3: Run it**

Run: `python3 -m pytest profiles/base/tests/test_e2e_score.py -v`
Expected: PASS (first run compiles Go; allow a minute). If `tool-events.jsonl` is missing, read the printed `collect.log` — the C3 call site is inside the pass-2 loop and must run before `agents_done.add`.

- [ ] **Step 4: Run everything**

Run: `python3 -m pytest profiles/base/tests/ profiles/base/telemetry_test.py -q && cd cli && go test ./... -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add profiles/base/agent-loop-router.md profiles/base/tests/test_e2e_score.py
git commit -m "feat(base): AGENT_LOOP runs zprof score after each runner dispatch; e2e smoke"
```

---

## Out of scope for this plan (spec phases 2–3)

`zprof stats` «Runs» card, repeated-finding signal for `eval-telemetry`, `$` on the card (needs prices in the model registry), `is_error` allowlist, `cat`/`sed -n` in P3, and the pr-shepherd gate. Rolling the new hook out to existing projects is a normal `zprof sync` in each project (jarvis-in-hermes, apple-health-sync), done by Alex after the release. Existing `.agentlog/` data there was written by the old collector (no `tool-events.jsonl`, thin nested rows), so `zprof score --all-missing` on it yields `confidence: partial` cards at best; the first real cards come from runs made after the sync. The spec's «ручная проверка на живом проекте» therefore happens on the first post-sync task-runner run, not inside this plan.
