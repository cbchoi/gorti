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

    Hashes the (consumer.received, producer.published) pair. The
    M27 Phase E cleanup removed a speculative third element
    ``send_interactions`` that never landed on the runner contract;
    ``len(published)`` is the canonical send count, so the two-tuple
    catches every determinism-failure mode this test ought to detect:
    drift in the wire payload bytes, drift in the producer counter,
    and drops/duplicates in the RTI fan-out.
    """
    blob = repr((result["received"], result["published"])).encode()
    return hashlib.sha256(blob).hexdigest()


@pytest.mark.spec
def test_spec_m4_determinism_10x_same_seed() -> None:
    """Run examples/pyjevsim 10× with the same inputs; assert all hashes match.

    M27 Phase E: the test name retains "same seed" for the
    stable contract identifier, but ``run_once`` takes no
    seed parameter — the runner is deterministic by construction
    (pyjevsim has no random source the bridge exposes). The original
    ``seed=`` kwarg was speculative API that never landed; passing
    identical ``ticks`` already satisfies the M4 exit criterion's
    "same inputs → same outputs" property.
    """
    # Imported lazily — the example dir is appended to sys.path above so
    # this resolves without installing the package.
    from runner import run_once  # type: ignore[import-not-found]

    sigs = [_witness(asyncio.run(run_once(ticks=10))) for _ in range(10)]
    assert all(sig == sigs[0] for sig in sigs), (
        "non-deterministic across 10 runs; distinct sha256s observed:\n  "
        + "\n  ".join(sorted(set(sigs)))
    )
