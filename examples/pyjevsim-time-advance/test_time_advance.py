"""TASK-213 (M21) — end-to-end test for the cross-process pyjevsim-time-advance example.

Drives runner.run_once and asserts the verify() invariant. Skipped
when the Go toolchain isn't on PATH (rtid binary build is required).
"""

from __future__ import annotations

import asyncio
import shutil
import sys
from pathlib import Path

import pytest

_HERE = Path(__file__).resolve().parent
if str(_HERE) not in sys.path:
    sys.path.insert(0, str(_HERE))

# ruff: noqa: E402
import importlib.util as _il

_spec = _il.spec_from_file_location("_pyjevsim_time_advance_runner", _HERE / "runner.py")
assert _spec is not None
_runner = _il.module_from_spec(_spec)
assert _spec.loader is not None
_spec.loader.exec_module(_runner)


@pytest.mark.spec
@pytest.mark.integration
def test_pyjevsim_time_advance_end_to_end() -> None:
    """3 Python federates × 10 NER cycles → verify() passes."""
    if shutil.which("go") is None:
        pytest.skip("go toolchain required to build rtid")
    result = asyncio.run(_runner.run_once(cycles=10, tick_step=3.0))
    ok, msg = _runner.verify(result)
    assert ok, msg
    assert len(result["per_federate"]) == 3
    for name, r in result["per_federate"].items():
        assert len(r["grants"]) == 10, f"{name}: {len(r['grants'])} grants, want 10"
