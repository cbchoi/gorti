"""Smoke tests for the sync-points bootstrap example.

Asserts the documented invariants:

  1. Every federate achieved every label exactly once, in order.
  2. Every federate observed federationSynchronized for every
     label exactly once.
  3. Each federate emitted exactly ``running_ticks`` Tick
     interactions during the running phase.
  4. The phase log shows the canonical ordering.
  5. No Tick interactions outside the running phase
     (output_handler returns {} when ``running`` is False).

Run from the repo root::

    python3 -m pytest examples/pyjevsim-sync-points/test_sync_points.py -v
"""

from __future__ import annotations

import asyncio
import sys
from pathlib import Path

import pytest

_HERE = Path(__file__).resolve().parent
_PYSDK = _HERE.parents[1] / "pysdk"
if str(_PYSDK) not in sys.path:
    sys.path.insert(0, str(_PYSDK))

# Load this example's runner.py under a UNIQUE module name to avoid
# the bare-name shadow collision when pytest collects multiple
# examples/pyjevsim*/runner.py files in one invocation.
import importlib.util as _il  # noqa: E402
_spec = _il.spec_from_file_location("_pyjevsim_sync_points_runner", _HERE / "runner.py")
_runner = _il.module_from_spec(_spec)
sys.modules.setdefault("_pyjevsim_sync_points_runner", _runner)
_spec.loader.exec_module(_runner)
run_once, verify = _runner.run_once, _runner.verify


def test_sync_points_default_config() -> None:
    """Default: 3 federates, 10 ticks between rendezvous points."""
    result = asyncio.run(run_once())

    ok, msg = verify(result)
    assert ok, msg

    assert set(result["achieved"]) == {"alpha", "beta", "gamma"}
    assert result["running_ticks"] == 10
    assert result["send_interaction_count"] == 30  # 3 federates * 10 ticks


def test_sync_points_each_federate_achieves_each_label() -> None:
    """Every federate's achieved list equals every label, in order."""
    result = asyncio.run(run_once(running_ticks=5))

    expected = ["start_simulation", "end_simulation"]
    for name in result["achieved"]:
        assert result["achieved"][name] == expected, (
            f"{name}.achieved={result['achieved'][name]!r}"
        )


def test_sync_points_synchronized_callback_after_all_achieve() -> None:
    """The runner-driven mark_synchronized fires AFTER every required
    peer voted achieved on a label. Test that every federate received
    both labels' synchronized callbacks."""
    result = asyncio.run(run_once())
    expected = ["start_simulation", "end_simulation"]
    for name, syncs in result["synchronized"].items():
        assert syncs == expected, f"{name}.synchronized={syncs!r}"


def test_sync_points_phase_ordering_is_canonical() -> None:
    """The phase log strictly follows the canonical bootstrap order:
    register → achieve_loop → synchronized → running → register →
    achieve_loop → synchronized → resign."""
    result = asyncio.run(run_once(running_ticks=2))

    expected = [
        ("register", "start_simulation"),
        ("achieve_loop", "start_simulation"),
        ("synchronized", "start_simulation"),
        ("running_start", ""),
        ("running_end", ""),
        ("register", "end_simulation"),
        ("achieve_loop", "end_simulation"),
        ("synchronized", "end_simulation"),
        ("resign_all", ""),
    ]
    assert result["phase_log"] == expected


@pytest.mark.parametrize("n", [0, 1, 25])
def test_sync_points_running_ticks_variants(n: int) -> None:
    """Bootstrap + teardown work cleanly even for 0-tick and longer runs.
    The 0-tick case verifies that NO Tick interactions happen outside
    the running phase — the federates never emit a Tick because
    ``running`` is False whenever output_handler is called."""
    result = asyncio.run(run_once(running_ticks=n))

    ok, msg = verify(result)
    assert ok, msg

    assert result["send_interaction_count"] == 3 * n
    if n == 0:
        # No federate should have any sent ticks recorded.
        assert all(t == [] for t in result["sent_ticks"].values())
