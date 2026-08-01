# ruff: noqa: S101
"""Cross-process integration coverage for time-managed object tracking."""

from __future__ import annotations

import asyncio
import importlib.util
import shutil
from pathlib import Path

import pytest

_HERE = Path(__file__).resolve().parent
_SPEC = importlib.util.spec_from_file_location(
    "_pyjevsim_object_tracking_runner", _HERE / "runner.py"
)
assert _SPEC is not None and _SPEC.loader is not None
_RUNNER = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(_RUNNER)


@pytest.mark.integration
def test_object_tracking_default_config() -> None:
    if not _RUNNER._default_rtid().is_file() and shutil.which("go") is None:
        pytest.skip("rtid is unavailable and Go is not on PATH")

    result = asyncio.run(_RUNNER.run_once(keep_workdir=False))
    ok, message = _RUNNER.verify(result)
    assert ok, message

    producer = result["per_federate"]["producer"]
    assert [item["position"] for item in producer["published"]] == [
        2.0,
        4.0,
        6.0,
        8.0,
        10.0,
    ]
    for name in ("tracker-A", "tracker-B"):
        assert result["per_federate"][name]["received"] == producer["published"]


def test_tso_step_rejects_timestamp_before_lookahead_boundary() -> None:
    with pytest.raises(ValueError, match="at least the producer lookahead"):
        _RUNNER._validate_tso_step(0.5, 1.0)


@pytest.mark.parametrize("tick_step", [1.0, 1.1, 3.0])
def test_tso_step_accepts_boundary_and_later_timestamps(tick_step: float) -> None:
    _RUNNER._validate_tso_step(tick_step, 1.0)
