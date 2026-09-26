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
