"""Smoke tests for the Path-B (bridged) Sensor → Dashboard example.

Same shape as ``examples/pyjevsim-dashboard/test_dashboard.py`` plus
extra invariants pinning the bridge-specific guarantees:

  1. Sensor's published sequence equals dashboard's received sequence.
  2. Exactly one ``DiscoverObjectInstance`` reached the dashboard.
  3. ``update_attributes`` wire-call count equals the number of
     values published.
  4. The bridge issued exactly one ``publish_object_class`` and one
     ``subscribe_object_class`` (declarations fire once at startup).
  5. The bridge issued exactly one ``register_object_instance``
     (drives ``register_instances`` exactly once).
  6. Sine mode publishes a periodic non-monotonic sequence.

Run from the repo root::

    python3 -m pytest examples/pyjevsim-dashboard-bridged/test_dashboard_bridged.py -v
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
# examples/pyjevsim*/runner.py files in one invocation. (Same pattern
# the sibling examples use after the dcefea5 housekeeping fix.)
import importlib.util as _il  # noqa: E402
_spec = _il.spec_from_file_location(
    "_pyjevsim_dashboard_bridged_runner", _HERE / "runner.py"
)
_runner = _il.module_from_spec(_spec)
sys.modules.setdefault("_pyjevsim_dashboard_bridged_runner", _runner)
_spec.loader.exec_module(_runner)
run_once, verify = _runner.run_once, _runner.verify


def test_dashboard_bridged_default_config() -> None:
    """Default: 10 ticks, sequence mode. Dashboard sees 0..9 and
    the bridge fires the declaration RPCs exactly once each."""
    result = asyncio.run(run_once())

    ok, msg = verify(result)
    assert ok, msg

    assert result["published"] == list(range(10))
    assert result["received"] == list(range(10))
    assert len(result["discovered"]) == 1
    assert result["discovered"][0][1] == "sensor-1"
    # Bridge wiring invariants: declarations fire once each.
    assert result["publish_object_calls"] == 1
    assert result["subscribe_object_calls"] == 1
    assert result["register_object_calls"] == 1


def test_dashboard_bridged_no_drops_invariant() -> None:
    """Every update lands. Identical to bypass variant's invariant
    so a researcher can A/B compare."""
    result = asyncio.run(run_once(ticks=25))

    ok, msg = verify(result)
    assert ok, msg

    assert len(result["published"]) == len(result["received"])
    assert result["update_attribute_calls"] == 25


def test_dashboard_bridged_sine_mode() -> None:
    """Sine mode publishes a periodic, non-monotonic sequence."""
    result = asyncio.run(run_once(ticks=16, mode="sine", amplitude=100))

    ok, msg = verify(result)
    assert ok, msg

    received = result["received"]
    assert received[0] == 0
    assert received[2] == 100
    assert received[6] == -100
    assert any(v > 0 for v in received)
    assert any(v < 0 for v in received)


@pytest.mark.parametrize("n", [1, 5, 50])
def test_dashboard_bridged_tick_count_variants(n: int) -> None:
    """End-to-end invariant holds across tick-count variants."""
    result = asyncio.run(run_once(ticks=n))

    ok, msg = verify(result)
    assert ok, msg

    assert len(result["published"]) == n
    assert len(result["received"]) == n


def test_dashboard_bridged_one_discover_event() -> None:
    """The sensor's bridge registers one instance, so the dashboard
    must observe exactly one DiscoverObjectInstance — even across
    long runs."""
    result = asyncio.run(run_once(ticks=20))
    assert len(result["discovered"]) == 1


def test_dashboard_bridged_declarations_fire_once_across_long_runs() -> None:
    """Bridge-specific invariant: ``publish_object_class`` /
    ``subscribe_object_class`` / ``register_object_instance`` are
    each emitted exactly once at startup, regardless of tick count.

    This is the back-stop against a future regression that re-fires
    declarations every cycle (which would silently work end-to-end
    on the in-process transport but burn wire bandwidth in
    production)."""
    result = asyncio.run(run_once(ticks=50))
    assert result["publish_object_calls"] == 1
    assert result["subscribe_object_calls"] == 1
    assert result["register_object_calls"] == 1


def test_dashboard_bridged_matches_bypass_variant_signature() -> None:
    """Equivalence guarantee: the Path-B (bridged) run produces the
    same canonical sequence as Path-A under the same flags.

    Both runners encode/decode 4-byte big-endian signed; both
    produce ``0..N-1`` for sequence mode and the same quantised
    sine for sine mode; both observe exactly one Discover. So a
    pinned sequence-mode result is sufficient witness — there is
    no observable behavioural difference at the ``run_once``
    return level.

    (We deliberately do NOT cross-import the bypass runner here —
    its module name "dashboard" / "sensor" collides with this
    example's modules in ``sys.modules`` and would shadow whichever
    loaded first. The pedagogical equivalence is documented in the
    README; the per-flag pinned sequences below are the
    machine-checkable witness.)
    """
    bridged_seq = asyncio.run(run_once(ticks=12, mode="sequence"))
    assert bridged_seq["published"] == list(range(12))
    assert bridged_seq["received"] == list(range(12))
    assert bridged_seq["update_attribute_calls"] == 12

    bridged_sine = asyncio.run(run_once(ticks=8, mode="sine", amplitude=10))
    # Period-8 sine, amplitude 10. tick 0=0, 1=7, 2=10, 3=7, 4=0,
    # 5=-7, 6=-10, 7=-7. (round(10*sin(2pi*i/8)).)
    assert bridged_sine["published"] == [0, 7, 10, 7, 0, -7, -10, -7]
    assert bridged_sine["received"] == bridged_sine["published"]
