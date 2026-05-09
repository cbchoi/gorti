"""Scaffold owned by TASK-215 (M21) — see docs/M21_DISPATCH_PLAN.md §6.

Cross-language M21 spec test suite, Python side. Mirrors the Go-side
acceptance gates in rti/spec/M21/test_time_service.go.
"""

from __future__ import annotations

import pytest


@pytest.mark.spec
@pytest.mark.integration
@pytest.mark.asyncio
async def test_ac_3_6_python_sdk_time_works() -> None:
    """AC §3.6 — pysdk time RPCs flip from no-op to real."""
    pytest.skip("TODO: TASK-215")


@pytest.mark.spec
@pytest.mark.integration
@pytest.mark.asyncio
async def test_ac_3_8_pyjevsim_time_advance_runs() -> None:
    """AC §3.8 — examples/pyjevsim-time-advance/ runs cross-process."""
    pytest.skip("TODO: TASK-215 — invokes the example's runner.py")


@pytest.mark.spec
@pytest.mark.integration
@pytest.mark.asyncio
async def test_ac_3_3_all_primitives_cross_language() -> None:
    """AC §3.3 — all 5 advance primitives produce grants on the wire,
    Python-side. Mirrors rti/spec/M21/test_time_service.go's Go check."""
    pytest.skip("TODO: TASK-215")
