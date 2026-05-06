"""End-to-end smoke test for the cross-process relay example.

Spawns rtid + 3 federate subprocesses + verifies the same accounting
invariants as ``examples/pyjevsim-relay/test_relay.py``:

  1. Accounting closes: forwarded + dropped + residual == published.
  2. No double-counting: forwarded & dropped == empty.
  3. Pipeline integrity: received == forwarded.

What's *different* from the in-process test:

  - The runner shells out to ``go build`` (idempotent) + the rtid
    binary, so the test takes a few wall-clock seconds longer.
  - Drop counts vary across runs because the cross-process timing is
    racy (no in-process fan-out lockstep, no time-managed advance --
    rtid's TimeService is not yet wired). The invariants still hold,
    so the parametrised tests assert the conservation law without
    pinning specific drop counts.

Run from the repo root::

    python3 -m pytest examples/pyjevsim-relay-cross-process/

Or directly::

    python3 examples/pyjevsim-relay-cross-process/runner.py
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
_PYSDK = _HERE.parents[1] / "pysdk"
if str(_PYSDK) not in sys.path:
    sys.path.insert(0, str(_PYSDK))

# ruff: noqa: E402
from runner import run_once, verify  # type: ignore[import-not-found]


def _skip_without_go() -> None:
    """Skip every test when the go toolchain is unavailable.

    Mirrors ``examples/pyjevsim/cross_lang_test.py`` -- the cross-
    process example needs to build rtid, and CI minus go can't do
    that.
    """
    if shutil.which("go") is None:
        pytest.skip(
            "go toolchain not on PATH; cannot build rtid for the "
            "cross-process smoke"
        )


def test_relay_cross_process_default_config() -> None:
    """Default knobs: 50 messages, capacity 5, service-period 2,
    30-tick drain. Expected (approximate, varies by wall-clock):
    ~25-35 forwarded, ~15-25 dropped, 0 residual.

    Asserts the accounting invariant + the "drops happened" pedagogical
    point. Does NOT pin exact counts -- those drift with kernel
    scheduling on a hot CI host.
    """
    _skip_without_go()
    result = asyncio.run(run_once(federate_timeout=60.0))

    ok, msg = verify(result)
    assert ok, msg

    assert len(result["published"]) == 50, (
        f"generator should have emitted 50 messages, got "
        f"{len(result['published'])}"
    )
    assert len(result["received"]) == len(result["forwarded"]), (
        "every forwarded seq should be received"
    )
    assert (
        len(result["forwarded"])
        + len(result["dropped"])
        + len(result["queue_residual"])
        == 50
    ), "conservation: forwarded + dropped + residual = published"

    # Pedagogical anchor: at default config the buffer should saturate
    # and exercise the drop path. If this fails on CI consistently it
    # likely means the runner accidentally let the buffer drain
    # faster than the generator filled (e.g. tick_period changed).
    assert len(result["dropped"]) > 0, (
        "expected non-zero drops at default config (capacity 5, "
        f"service-period 2); got result={result}"
    )


def test_relay_cross_process_no_drops_when_service_keeps_up() -> None:
    """service_period=1 means the buffer drains as fast as the
    generator fills. With capacity 5 and a fast tick the queue stays
    near-empty and we expect zero drops -- but the residual can be
    non-zero if the run ends mid-cycle, so the strong assertion is
    only "every published was either forwarded or in residual".
    """
    _skip_without_go()
    result = asyncio.run(
        run_once(
            gen_messages=20,
            capacity=5,
            service_period=1,
            drain_ticks=20,
            federate_timeout=60.0,
        )
    )

    ok, msg = verify(result)
    assert ok, msg

    assert len(result["dropped"]) == 0, (
        f"capacity 5, service_period 1 should never drop; got "
        f"{len(result['dropped'])} drops"
    )


def test_relay_cross_process_residual_when_drain_too_short() -> None:
    """A short drain leaves residual in the queue. Conservation must
    still hold."""
    _skip_without_go()
    result = asyncio.run(
        run_once(
            gen_messages=10,
            capacity=10,
            service_period=4,
            drain_ticks=2,
            federate_timeout=60.0,
        )
    )
    ok, msg = verify(result)
    assert ok, msg


@pytest.mark.parametrize("capacity", [1, 3, 10])
def test_relay_cross_process_capacity_variants(capacity: int) -> None:
    """Verifier holds across a range of buffer capacities."""
    _skip_without_go()
    result = asyncio.run(
        run_once(
            gen_messages=20,
            capacity=capacity,
            service_period=2,
            drain_ticks=20,
            federate_timeout=60.0,
        )
    )
    ok, msg = verify(result)
    assert ok, msg
