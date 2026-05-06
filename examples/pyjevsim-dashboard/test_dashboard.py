"""Smoke tests for the Sensor → Dashboard object-instance example.

Asserts the documented invariants:

  1. Sensor's published sequence equals dashboard's received sequence.
  2. Exactly one DiscoverObjectInstance event reached the dashboard.
  3. The number of update_attributes wire calls equals the number of
     values published.
  4. ``mode="sine"`` produces a non-monotonic sequence with both
     positive and negative values (verifies the sine path actually
     fires; the bridge can verify any deterministic sequence).

Run from the repo root::

    python3 -m pytest examples/pyjevsim-dashboard/test_dashboard.py -v
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
_spec = _il.spec_from_file_location("_pyjevsim_dashboard_runner", _HERE / "runner.py")
_runner = _il.module_from_spec(_spec)
sys.modules.setdefault("_pyjevsim_dashboard_runner", _runner)
_spec.loader.exec_module(_runner)
run_once, verify = _runner.run_once, _runner.verify


def test_dashboard_default_config() -> None:
    """Default: 10 ticks, sequence mode. Dashboard sees 0..9."""
    result = asyncio.run(run_once())

    ok, msg = verify(result)
    assert ok, msg

    assert result["published"] == list(range(10))
    assert result["received"] == list(range(10))
    assert len(result["discovered"]) == 1
    assert result["discovered"][0][1] == "sensor-1"


def test_dashboard_no_drops_invariant() -> None:
    """The pedagogical promise of this example: every update lands."""
    result = asyncio.run(run_once(ticks=25))

    ok, msg = verify(result)
    assert ok, msg

    assert len(result["published"]) == len(result["received"])
    assert result["update_attribute_calls"] == 25


def test_dashboard_sine_mode() -> None:
    """Sine mode publishes a periodic, non-monotonic sequence."""
    result = asyncio.run(run_once(ticks=16, mode="sine", amplitude=100))

    ok, msg = verify(result)
    assert ok, msg

    # Period 8, amplitude 100. tick 0 = 0, tick 2 = 100, tick 4 = 0,
    # tick 6 = -100. The dashboard sees the same sequence.
    received = result["received"]
    assert received[0] == 0
    assert received[2] == 100
    assert received[6] == -100
    # 16 ticks = 2 full periods → at least one negative and one
    # positive value.
    assert any(v > 0 for v in received)
    assert any(v < 0 for v in received)


@pytest.mark.parametrize("n", [1, 5, 50])
def test_dashboard_tick_count_variants(n: int) -> None:
    """End-to-end invariant holds across tick-count variants."""
    result = asyncio.run(run_once(ticks=n))

    ok, msg = verify(result)
    assert ok, msg

    assert len(result["published"]) == n
    assert len(result["received"]) == n


def test_dashboard_one_discover_event() -> None:
    """The sensor registers exactly one instance, so the dashboard
    must observe exactly one DiscoverObjectInstance — even across
    long runs."""
    result = asyncio.run(run_once(ticks=20))
    assert len(result["discovered"]) == 1
