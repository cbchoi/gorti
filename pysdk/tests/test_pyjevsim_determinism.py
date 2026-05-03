"""Agent-owned harness for the pyjevsim example determinism property.

Mirrors the spec gate at ``pysdk/tests/spec/m4/test_spec_m4_determinism.py``
but lives outside the spec tree so it stays editable without orchestrator
sign-off. Both tests share the same ``run_once`` entry point, so a
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
    blob = repr(
        (
            result["received"],
            result["published"],
            result["send_interactions"],
        )
    ).encode()
    return hashlib.sha256(blob).hexdigest()


def test_pyjevsim_runner_is_deterministic_across_10_runs() -> None:
    """Same seed -> identical sha256 across 10 runs."""
    run_once = _import_run_once()
    sigs = [_witness(asyncio.run(run_once(ticks=8, seed=1))) for _ in range(10)]
    assert all(sig == sigs[0] for sig in sigs), (
        "non-deterministic — distinct sha256s observed:\n  "
        + "\n  ".join(sorted(set(sigs)))
    )


def test_pyjevsim_runner_witness_distinguishes_tick_counts() -> None:
    """Witness function must depend on the workload — guards against
    collapsing to a constant in a future refactor."""
    run_once = _import_run_once()
    short = _witness(asyncio.run(run_once(ticks=2, seed=1)))
    longer = _witness(asyncio.run(run_once(ticks=4, seed=1)))
    assert short != longer


@pytest.mark.parametrize("ticks", [1, 3, 7])
def test_pyjevsim_runner_consumer_count_matches_producer(ticks: int) -> None:
    """Every ``Producer.published`` element shows up exactly once in
    ``Consumer.received`` (no drops, no duplicates)."""
    run_once = _import_run_once()
    result = asyncio.run(run_once(ticks=ticks, seed=0))
    assert len(result["received"]) == ticks
    assert result["published"] == list(range(1, ticks + 1))
