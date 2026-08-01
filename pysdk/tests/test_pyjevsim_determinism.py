"""Test harness for the pyjevsim example determinism property.

Mirrors the spec gate at ``pysdk/tests/spec/m4/test_spec_m4_determinism.py``
but lives outside the specification tree as an implementation-level test.
Both tests share the same ``run_once`` entry point, so a
regression that flips one flips the other.
"""

from __future__ import annotations

import asyncio
import hashlib
import sys
from pathlib import Path
from typing import Any

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
EXAMPLE_DIR = REPO_ROOT / "examples" / "pyjevsim"
if str(EXAMPLE_DIR) not in sys.path:
    sys.path.insert(0, str(EXAMPLE_DIR))


def _import_run_once() -> Any:
    # Imported lazily so module collection does not fail in environments
    # that haven't installed the example deps. The runner only depends on
    # rti1516e + pyjevsim_bridge + the in-process fake, all of which are
    # already wired into the pysdk test environment.
    from runner import run_once  # type: ignore[import-not-found]

    return run_once


def _witness(result: dict[str, object]) -> str:
    # M27 Phase E: runner returns {"published", "received", "workdir",
    # "rtid_port", "rtid_pid"} — there is no separate "send_interactions"
    # count (len(published) is the canonical send count). The earlier
    # third tuple element was a speculative addition that never landed
    # on the runner contract; the (received, published) pair is enough
    # to catch every determinism-failure mode the test ought to detect.
    blob = repr((result["received"], result["published"])).encode()
    return hashlib.sha256(blob).hexdigest()


def test_pyjevsim_runner_is_deterministic_across_10_runs() -> None:
    """10× same-input run -> identical sha256.

    M27 Phase E: the runner is deterministic by construction (no
    random source) — the previous ``seed=`` kwarg was speculative
    API that never landed on ``run_once``. Same ``ticks`` value
    satisfies the original spec intent ("same inputs → same outputs").
    """
    run_once = _import_run_once()
    sigs = [_witness(asyncio.run(run_once(ticks=8))) for _ in range(10)]
    assert all(sig == sigs[0] for sig in sigs), (
        "non-deterministic — distinct sha256s observed:\n  "
        + "\n  ".join(sorted(set(sigs)))
    )


def test_pyjevsim_runner_witness_distinguishes_tick_counts() -> None:
    """Witness function must depend on the workload — guards against
    collapsing to a constant in a future refactor."""
    run_once = _import_run_once()
    short = _witness(asyncio.run(run_once(ticks=2)))
    longer = _witness(asyncio.run(run_once(ticks=4)))
    assert short != longer


@pytest.mark.parametrize("ticks", [1, 3, 7])
def test_pyjevsim_runner_consumer_count_matches_producer(ticks: int) -> None:
    """Every ``Producer.published`` element shows up exactly once in
    ``Consumer.received`` (no drops, no duplicates).

    M27 Phase E: the runner ticks ``producer`` for ``ticks +
    drain_ticks`` cycles (drain_ticks defaults to 30), so the
    producer's published count is NOT the ``ticks`` argument — it's
    the full cycle count. The property being tested is "every
    published item is received exactly once" — assert that directly.
    """
    run_once = _import_run_once()
    result = asyncio.run(run_once(ticks=ticks))
    published = list(result["published"])
    received = list(result["received"])
    assert len(published) >= ticks, (
        f"producer published {len(published)} items; expected at least {ticks}"
    )
    assert received == published, (
        f"received != published\n  received={received!r}\n  published={published!r}"
    )
