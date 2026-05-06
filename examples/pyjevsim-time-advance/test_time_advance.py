"""Smoke tests for the 3-federate time-advance example.

Asserts the documented invariants:

  1. LBTS is monotonic non-decreasing over the trace.
  2. The federate identified as ``earliest_federate`` has the
     smallest ``current + lookahead``.
  3. With default lookaheads {0.5, 1.0, 2.0} starting from now=0
     for every federate, ``"fast"`` is the earliest at tick 0.
  4. Each federate emits exactly ``ticks`` heartbeats and issues
     exactly ``ticks`` next_message_request calls.
  5. Swapping the lookaheads makes a different federate the
     tick-0 earliest (the rule is symmetric under relabelling).

Run from the repo root::

    python3 -m pytest examples/pyjevsim-time-advance/test_time_advance.py -v
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
_spec = _il.spec_from_file_location("_pyjevsim_time_advance_runner", _HERE / "runner.py")
_runner = _il.module_from_spec(_spec)
sys.modules.setdefault("_pyjevsim_time_advance_runner", _runner)
_spec.loader.exec_module(_runner)
run_once, verify = _runner.run_once, _runner.verify


def test_time_advance_default_config() -> None:
    """Default {0.5, 1.0, 2.0} lookaheads. Verify invariants pass and
    'fast' is the tick-0 earliest."""
    result = asyncio.run(run_once())

    ok, msg = verify(result)
    assert ok, msg

    assert len(result["trace"]) == 6
    assert result["trace"][0]["earliest_federate"] == "fast"
    assert result["ner_count"] == 18  # 3 federates * 6 ticks
    assert result["send_interaction_count"] == 18


def test_time_advance_lbts_monotonic() -> None:
    """LBTS must be non-decreasing across all ticks."""
    result = asyncio.run(run_once(ticks=20))

    lbts_values = [row["lbts"] for row in result["trace"]]
    assert lbts_values == sorted(lbts_values)


def test_time_advance_earliest_picks_smallest_contribution() -> None:
    """The federate flagged as earliest_federate must have the
    minimum current+lookahead in every row."""
    result = asyncio.run(run_once(ticks=10))

    for row in result["trace"]:
        contribs = row["contributions"]
        true_earliest = min(contribs, key=contribs.get)
        assert row["earliest_federate"] == true_earliest, (
            f"tick {row['tick']}: earliest mislabelled "
            f"{row['earliest_federate']!r} vs {true_earliest!r}"
        )


def test_time_advance_swap_makes_slow_earliest() -> None:
    """If we swap the lookaheads — slow=0.1, fast=2.0 — the federate
    NAMED ``slow`` (with the actually-smallest lookahead) is now
    the earliest at tick 0. Pedagogical confirmation that the rule
    is contribution-driven, not name-driven."""
    result = asyncio.run(
        run_once(ticks=3, la_fast=2.0, la_mid=1.0, la_slow=0.1)
    )

    ok, msg = verify(result)
    # Verify still flags trace[0]['earliest_federate'] == 'fast'
    # because that test is hardcoded to default lookaheads — but the
    # contribution sanity check (rule #2 in verify) DOES still pass,
    # and the trace itself shows ``slow`` as the smallest contribution.
    assert not ok, "swapped-lookahead config should fail the default verify()"
    assert "expected 'fast'" in msg, msg

    # The trace data itself is correct — slow IS the earliest:
    assert result["trace"][0]["earliest_federate"] == "slow"
    assert result["trace"][0]["contributions"]["slow"] == pytest.approx(0.1)


def test_time_advance_heartbeat_payload_decodes_to_send_time() -> None:
    """The Heartbeat payload carries the federate's logical time at
    send. Verify the wire log matches the regulator's own ``sent``
    list. Useful when researchers wire alt-strategy comparisons —
    the wire log is the source of truth."""
    result = asyncio.run(run_once(ticks=4))

    by_name: dict[str, list[float]] = {"fast": [], "mid": [], "slow": []}
    for name, t in result["heartbeats"]:
        if name in by_name:
            by_name[name].append(t)

    # 4 ticks → 4 heartbeats per federate.
    assert len(by_name["fast"]) == 4
    assert len(by_name["mid"]) == 4
    assert len(by_name["slow"]) == 4

    # Every regulator starts at now=0 and advances by step=1.0 per
    # cycle, so heartbeat times are [0.0, 1.0, 2.0, 3.0] for each.
    assert by_name["fast"] == [0.0, 1.0, 2.0, 3.0]
    assert by_name["mid"] == [0.0, 1.0, 2.0, 3.0]
    assert by_name["slow"] == [0.0, 1.0, 2.0, 3.0]


@pytest.mark.parametrize("n", [1, 4, 12])
def test_time_advance_tick_variants(n: int) -> None:
    """Invariants hold across run lengths."""
    result = asyncio.run(run_once(ticks=n))
    ok, msg = verify(result)
    assert ok, msg
