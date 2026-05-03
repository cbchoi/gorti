"""M4 determinism gate — examples/pyjevsim/ runs 10× same seed produce
sha256-identical RTI-side event logs.

Wired by W7 (TASK-074) once examples/pyjevsim/runner.py landed: the
runner exposes ``run_once`` returning a deterministic by-product dict
(consumer.received + producer.published + send_interaction count); we
sha256 a stable repr of the dict and assert all 10 runs hash identically.

Implements: NFR-DET-1, NFR-DET-2; M4 exit criterion #2.
"""

from __future__ import annotations

import asyncio
import hashlib
import sys
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[4]
EXAMPLE_DIR = REPO_ROOT / "examples" / "pyjevsim"
if str(EXAMPLE_DIR) not in sys.path:
    sys.path.insert(0, str(EXAMPLE_DIR))


def _witness(result: dict[str, object]) -> str:
    """sha256 over the deterministic byproducts of one ``run_once`` call.

    Hashing ``repr`` of a tuple of (consumer.received, producer.published,
    send_interaction_count) catches three independent failure modes:
    drift in the wire payload bytes, drift in the producer's internal
    counter, and drops/duplicates in the fake-RTI fan-out.
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
def test_spec_m4_determinism_10x_same_seed() -> None:
    """Run examples/pyjevsim 10× with the same seed; assert all hashes match."""
    # Imported lazily — the example dir is appended to sys.path above so
    # this resolves without installing the package.
    from runner import run_once  # type: ignore[import-not-found]

    sigs = [_witness(asyncio.run(run_once(ticks=10, seed=42))) for _ in range(10)]
    assert all(sig == sigs[0] for sig in sigs), (
        "non-deterministic across 10 runs; distinct sha256s observed:\n  "
        + "\n  ".join(sorted(set(sigs)))
    )
