# ruff: noqa: S101
"""End-to-end coverage for cross-process pyjevsim time advance."""

from __future__ import annotations

import asyncio
import importlib.util
import shutil
from pathlib import Path

import pytest

_HERE = Path(__file__).resolve().parent
_SPEC = importlib.util.spec_from_file_location(
    "_pyjevsim_time_advance_runner", _HERE / "runner.py"
)
assert _SPEC is not None and _SPEC.loader is not None
_RUNNER = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(_RUNNER)


@pytest.mark.integration
def test_pyjevsim_time_advance_end_to_end() -> None:
    """Three Python federates complete ten grant-driven model cycles."""
    if not _RUNNER._default_rtid().is_file() and shutil.which("go") is None:
        pytest.skip("rtid is unavailable and Go is not on PATH")

    result = asyncio.run(
        _RUNNER.run_once(cycles=10, tick_step=3.0, keep_workdir=False)
    )
    ok, message = _RUNNER.verify(result)
    assert ok, message
    assert len(result["per_federate"]) == 3

    expected_times = [float(i * 3) for i in range(1, 11)]
    for name, federate in result["per_federate"].items():
        assert federate["requested"] == expected_times, name
        assert federate["grants"] == expected_times, name
        assert federate["model_cycles"] == 10, name
        assert federate["output_calls"] == 10, name
