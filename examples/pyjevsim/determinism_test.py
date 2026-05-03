"""Determinism harness for ``examples/pyjevsim/runner.py``.

Runs ``run_once`` 10 consecutive times with the same seed and asserts
the sha256 of the consumer-side witness (received list + producer-side
published list + recorded send_interaction count) is identical across
all 10 runs.

Run as part of the spec suite via
``pysdk/tests/spec/m4/test_spec_m4_determinism.py``; can also be run
standalone with::

    pytest examples/pyjevsim/determinism_test.py

Implements: NFR-DET-1, NFR-DET-2; M4 exit criterion #2.
"""

from __future__ import annotations

import asyncio
import hashlib
import sys
from pathlib import Path

import pytest

# Make the runner importable when this file is collected from outside
# ``examples/pyjevsim/`` (the spec test wrapper relies on this so it can
# call ``run_once`` without having to manually fix up sys.path).
_HERE = Path(__file__).resolve().parent
if str(_HERE) not in sys.path:
    sys.path.insert(0, str(_HERE))

from runner import run_once  # noqa: E402  (sys.path tweak above)


def _witness(result: dict[str, object]) -> str:
    """Hash the deterministic byproducts of one run.

    Includes the consumer's ``received`` list (port + payload bytes), the
    producer's ``published`` sequence numbers, and the count of
    ``send_interaction`` calls the FakeRtiServer recorded. The first two
    catch payload-content drift; the count catches dropped or duplicated
    sends that would still hash the same payload-list-wise.

    ``repr`` is used because dict ordering is insertion-stable in
    Python 3.7+ and the parameters dict in ``ReceiveInteraction`` is
    constructed from ``send_interaction`` args in fixed order.
    """
    blob = repr(
        (
            result["received"],
            result["published"],
            result["send_interactions"],
        )
    ).encode()
    return hashlib.sha256(blob).hexdigest()


@pytest.mark.spec
def test_determinism_10x_same_seed() -> None:
    """10 consecutive runs of the example with seed=42 hash identically."""
    sigs = [_witness(asyncio.run(run_once(ticks=10, seed=42))) for _ in range(10)]
    assert all(sig == sigs[0] for sig in sigs), (
        "non-deterministic — observed sha256 set:\n  " + "\n  ".join(sorted(set(sigs)))
    )


@pytest.mark.spec
def test_determinism_witness_is_payload_sensitive() -> None:
    """Sanity check: a different ``ticks`` count produces a different witness.

    Guards against the witness collapsing to a constant (e.g. by a future
    refactor that drops the payload bytes). If this test ever passes by
    accident, the determinism test above no longer proves anything.
    """
    short = _witness(asyncio.run(run_once(ticks=2, seed=42)))
    long = _witness(asyncio.run(run_once(ticks=5, seed=42)))
    assert short != long, "witness function must distinguish workloads"
