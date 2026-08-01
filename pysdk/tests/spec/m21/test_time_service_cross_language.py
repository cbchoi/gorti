"""TASK-215 (M21) — Python-side acceptance gate for AC §3.

Most cross-language invariants are proven in the runner-driven
example (examples/pyjevsim-time-advance/test_time_advance.py at
TASK-213) which spawns rtid + 3 Python regulators and verifies
end-to-end. This file binds the higher-level AC rows to surface
checks; the heavy lifting is in the example test.
"""

from __future__ import annotations

import inspect

import pytest

from rti1516e import _grpc_errors as ge
from rti1516e import _transport as tr


@pytest.mark.spec
def test_ac_3_6_python_sdk_time_works() -> None:
    """AC §3.6 — pysdk time RPCs flip from no-op to real.

    Verified by:
      - test_time_client.py::test_209_dispatch_branches_present
        (the no-op short-circuit is gone)
      - test_time_client.py::test_209_time_stub_constructed
        (TimeServiceStub is wired)
      - test_time_client.py::test_209_typed_exceptions_exposed
        (typed errors importable)

    Here we record the dependencies + smoke-check the surface is intact.
    """
    src = inspect.getsource(tr.GrpcTransport.record)
    assert "TimeService is nil at M2" not in src, (
        "stale caveat present in dispatcher source"
    )
    assert hasattr(tr.GrpcTransport, "_enable_time_regulation")
    assert hasattr(tr.GrpcTransport, "_enable_time_constrained")
    assert hasattr(tr.GrpcTransport, "_next_message_request")


@pytest.mark.spec
@pytest.mark.integration
def test_ac_3_8_pyjevsim_time_advance_runs() -> None:
    """AC §3.8 — examples/pyjevsim-time-advance/ runs cross-process.

    Driven end-to-end by examples/pyjevsim-time-advance/test_time_advance.py
    (TASK-213) which spawns rtid + 3 Python regulators. Skipping here
    avoids running the same heavy E2E twice in one CI invocation.
    """
    pytest.skip(
        "End-to-end run covered by examples/pyjevsim-time-advance/test_time_advance.py "
        "(TASK-213). Run that test directly: "
        "pytest examples/pyjevsim-time-advance/test_time_advance.py"
    )


@pytest.mark.spec
def test_ac_3_3_typed_errors_exhaustive() -> None:
    """AC §3.3 ↔ §3.4 — every row of plan §2.3.1 has a typed exception.

    Cross-references plan §2.3.1's manager-error-to-detail mapping
    against the typed exceptions exposed in pysdk/_grpc_errors.py.
    """
    expected = {
        "TimeRegulationAlreadyEnabled",
        "TimeRegulationNotEnabled",
        "TimeConstrainedAlreadyEnabled",
        "TimeConstrainedNotEnabled",
        "InvalidLookahead",
        "LogicalTimeAlreadyPassed",
        "TimeAdvancingState",
        "FederationHaltedError",
    }
    actual = {n for n in dir(ge) if not n.startswith("_")}
    missing = expected - actual
    assert not missing, f"typed exceptions missing: {missing}"
