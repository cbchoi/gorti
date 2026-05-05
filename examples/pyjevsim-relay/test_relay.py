"""Smoke test for the 3-federate relay example.

Imports the runner module and asserts the documented invariants:

  1. Accounting closes: forwarded + dropped + residual == published.
  2. No double-counting: forwarded ∩ dropped == ∅.
  3. Pipeline integrity: received == forwarded.
  4. At default config the buffer saturates → drops > 0
     (verifies the pedagogical point of the example).

Run from the repo root::

    python3 -m pytest examples/pyjevsim-relay/test_relay.py

Or via the existing pysdk pytest config (this file is also picked up
when running ``pytest`` from the repo root because the runner sets
``sys.path`` to include both ``examples/pyjevsim-relay/`` and
``pysdk/``).
"""

from __future__ import annotations

import asyncio
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
from runner import run_once, verify


def test_relay_default_config() -> None:
    """Default knobs: 50 messages, capacity 5, service-period 2,
    30-tick drain. Expected: ~29 forwarded, ~21 dropped, 0 residual.
    """
    result = asyncio.run(run_once())

    ok, msg = verify(result)
    assert ok, msg

    assert len(result["published"]) == 50
    assert len(result["received"]) == len(result["forwarded"])
    assert len(result["forwarded"]) + len(result["dropped"]) + len(
        result["queue_residual"]
    ) == len(result["published"])

    # Pedagogical point: at default config the queue saturates and we
    # exercise the drop-on-overflow path.
    assert len(result["dropped"]) > 0, (
        "expected non-zero drops at default config (service-period 2, "
        f"capacity 5); got result={result}"
    )


def test_relay_no_drops_when_service_keeps_up() -> None:
    """service_period=1 means the buffer drains as fast as the
    generator fills it — no drops expected."""
    result = asyncio.run(
        run_once(gen_messages=20, capacity=5, service_period=1, drain_ticks=10)
    )

    ok, msg = verify(result)
    assert ok, msg

    assert len(result["dropped"]) == 0
    assert len(result["received"]) == len(result["published"])


def test_relay_residual_when_drain_too_short() -> None:
    """If the drain phase is shorter than what's needed to flush the
    queue, the residual is what's still in the buffer at exit.
    Accounting must still close even with non-zero residual."""
    result = asyncio.run(
        run_once(gen_messages=10, capacity=10, service_period=4, drain_ticks=2)
    )

    ok, msg = verify(result)
    assert ok, msg


@pytest.mark.parametrize("capacity", [1, 3, 10])
def test_relay_capacity_variants(capacity: int) -> None:
    """Verifier holds across a range of buffer capacities."""
    result = asyncio.run(
        run_once(
            gen_messages=30, capacity=capacity, service_period=2, drain_ticks=20
        )
    )
    ok, msg = verify(result)
    assert ok, msg
