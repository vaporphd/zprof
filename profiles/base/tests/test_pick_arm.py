"""Tests for pick-arm mode."""
import json
import os
import subprocess
import sys
import pathlib

COLLECTOR = pathlib.Path(__file__).parent.parent / "zprof-collect.py"


def run_pick_arm(role, task, cwd, zprof_yaml_content=None):
    """Run collector in pick-arm mode, return parsed JSON."""
    if zprof_yaml_content:
        (pathlib.Path(cwd) / ".zprof.yaml").write_text(zprof_yaml_content)

    payload = json.dumps({"role": role, "task": task, "cwd": str(cwd)})
    p = subprocess.run(
        [sys.executable, str(COLLECTOR), "pick-arm"],
        input=payload, capture_output=True, text=True, timeout=10,
    )
    assert p.returncode == 0
    return json.loads(p.stdout)


class TestPickArm:
    def test_no_config_returns_null(self, tmp_path):
        result = run_pick_arm("implementer", "fix bug", tmp_path)
        assert result["model"] is None
        assert result["arm"] is None

    def test_with_experiment(self, tmp_path):
        config = """overlays:
  - test
ab_experiments:
  implementer:
    control: sonnet
    candidate: opus
"""
        result = run_pick_arm("implementer", "fix the bug", tmp_path, config)
        assert result["model"] in ("sonnet", "opus")
        assert result["arm"] in ("control", "candidate")
        if result["model"] == "sonnet":
            assert result["arm"] == "control"
        else:
            assert result["arm"] == "candidate"

    def test_deterministic(self, tmp_path):
        config = """overlays:
  - test
ab_experiments:
  implementer:
    control: sonnet
    candidate: opus
"""
        r1 = run_pick_arm("implementer", "fix the bug", tmp_path, config)
        r2 = run_pick_arm("implementer", "fix the bug", tmp_path, config)
        assert r1 == r2, "same input must produce same arm"

    def test_different_task_can_differ(self, tmp_path):
        config = """overlays:
  - test
ab_experiments:
  implementer:
    control: sonnet
    candidate: opus
"""
        results = set()
        for i in range(20):
            r = run_pick_arm("implementer", f"task variant {i}", tmp_path, config)
            results.add(r["arm"])
        assert len(results) == 2, "20 different tasks should hit both arms"

    def test_role_not_in_experiment(self, tmp_path):
        config = """overlays:
  - test
ab_experiments:
  implementer:
    control: sonnet
    candidate: opus
"""
        result = run_pick_arm("reviewer", "review code", tmp_path, config)
        assert result["model"] is None

    def test_invalid_input_exits_zero(self, tmp_path):
        p = subprocess.run(
            [sys.executable, str(COLLECTOR), "pick-arm"],
            input="not json", capture_output=True, text=True, timeout=10,
        )
        assert p.returncode == 0
        result = json.loads(p.stdout)
        assert "error" in result
