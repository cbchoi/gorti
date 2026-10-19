"""TASK-215 (M21) — Python-side acceptance gate for AC §3.

The adapter acceptance case runs the existing example model through
HLAFederate in a child process alongside two separate live regulators.
The remaining cases retain their historical SDK surface checks.
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
    """The existing example model runs through HLAFederate with two live peers."""
    from tests.spec.m21._processes import assert_pyjevsim_adapter_cycles

    assert_pyjevsim_adapter_cycles()


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
