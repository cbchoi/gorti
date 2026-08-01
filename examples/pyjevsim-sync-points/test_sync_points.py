# ruff: noqa: S101
"""Cross-process integration coverage for the sync-point example."""

from __future__ import annotations

import asyncio
import importlib.util
import shutil
import sys
from pathlib import Path

import pytest

_HERE = Path(__file__).resolve().parent
_PYSDK = _HERE.parents[1] / "pysdk"
if str(_PYSDK) not in sys.path:
    sys.path.insert(0, str(_PYSDK))

_SPEC = importlib.util.spec_from_file_location(
    "_pyjevsim_sync_points_runner", _HERE / "runner.py"
)
assert _SPEC is not None and _SPEC.loader is not None
_RUNNER = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(_RUNNER)


def _skip_without_rtid() -> None:
    if not _RUNNER._default_rtid().is_file() and shutil.which("go") is None:
        pytest.skip("rtid is unavailable and Go is not on PATH")


@pytest.mark.integration
def test_sync_points_default_config() -> None:
    _skip_without_rtid()
    result = asyncio.run(_RUNNER.run_once(keep_workdir=False))

    ok, message = _RUNNER.verify(result)
    assert ok, message
    assert result["running_ticks"] == 10
    for participant in result["per_federate"].values():
        assert participant["achieved"] == result["labels"]
        assert participant["synchronized"] == result["labels"]
        assert participant["sent_ticks"] == list(range(1, 11))
        assert sorted(participant["received_ticks"]) == [
            sequence for sequence in range(1, 11) for _ in range(2)
        ]
        assert participant["ticks_during_sync"] == []


def test_verify_rejects_a_missing_peer_tick() -> None:
    participant = {
        "achieved": ["start_simulation", "end_simulation"],
        "synchronized": ["start_simulation", "end_simulation"],
        "sent_ticks": [1],
        "received_ticks": [1],
    }
    result = {
        "labels": ["start_simulation", "end_simulation"],
        "running_ticks": 1,
        "exit_codes": dict.fromkeys(_RUNNER.PARTICIPANT_NAMES, 0),
        "per_federate": {
            name: dict(participant) for name in _RUNNER.PARTICIPANT_NAMES
        },
    }

    ok, message = _RUNNER.verify(result)
    assert not ok
    assert "expected two copies" in message


def test_verify_rejects_a_tick_during_synchronization() -> None:
    labels = ["start_simulation", "end_simulation"]
    participant = {
        "achieved": labels.copy(),
        "synchronized": labels.copy(),
        "sent_ticks": [1],
        "received_ticks": [1, 1],
        "ticks_during_sync": [],
    }
    result = {
        "labels": labels,
        "running_ticks": 1,
        "exit_codes": dict.fromkeys(_RUNNER.PARTICIPANT_NAMES, 0),
        "per_federate": {
            name: dict(participant) for name in _RUNNER.PARTICIPANT_NAMES
        },
    }
    result["per_federate"]["beta"]["ticks_during_sync"] = [99]

    ok, message = _RUNNER.verify(result)

    assert not ok
    assert "beta.ticks_during_sync" in message
