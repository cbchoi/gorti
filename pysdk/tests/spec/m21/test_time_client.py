"""TASK-209 (M21) — Python SDK time-RPC test suite.

Verifies the W3B flip: pysdk's _transport.GrpcTransport now dispatches
TimeService RPCs (was no-op short-circuit pre-M21). The 3 method
bindings exposed in connection.py — enable_time_regulation,
enable_time_constrained, next_message_request — are tested here.

End-to-end cases that need a real rtid subprocess (cross-language
Go-regulator + Python-constrained, etc.) move to TASK-215 where the
spawn-rtid harness is set up.
"""

from __future__ import annotations

import pytest

from rti1516e import _grpc_errors as ge
from rti1516e import _transport as tr


@pytest.mark.spec
def test_209_typed_exceptions_exposed() -> None:
    """209-typed — the 8 time-mgmt typed exceptions are importable
    and inherit from RtiError (so callers can catch the base)."""
    from rti1516e.errors import RtiError

    classes = [
        ge.TimeRegulationAlreadyEnabled,
        ge.TimeRegulationNotEnabled,
        ge.TimeConstrainedAlreadyEnabled,
        ge.TimeConstrainedNotEnabled,
        ge.InvalidLookahead,
        ge.LogicalTimeAlreadyPassed,
        ge.TimeAdvancingState,
        ge.FederationHaltedError,
    ]
    for cls in classes:
        assert issubclass(cls, RtiError), f"{cls.__name__} not RtiError subclass"
        assert cls.error_code is not None
        assert 700 <= cls.error_code <= 708, f"{cls.__name__} code outside 700-708"


@pytest.mark.spec
def test_209_dispatch_branches_present() -> None:
    """209-dispatch — _transport.py's GrpcTransport.record dispatches
    each of the 3 wired time methods (the previous no-op short-circuit
    is gone). Verified by scanning the module source rather than
    invoking against a real channel."""
    import inspect

    src = inspect.getsource(tr.GrpcTransport.record)
    for method in ("enable_time_regulation", "enable_time_constrained", "next_message_request"):
        assert f'method == "{method}"' in src, (
            f"dispatch branch missing for {method!r} — "
            f"M21 TASK-208 flip not applied"
        )
    # Sanity: ensure the old no-op short-circuit comment is gone.
    assert "TimeService is nil at M2" not in src, (
        "stale TimeService=nil caveat still present in dispatcher"
    )


@pytest.mark.spec
def test_209_time_stub_constructed() -> None:
    """209-stub — GrpcTransport.__init__ wires self.time as a TimeServiceStub."""
    # We don't dial a real channel — just verify the construction path
    # references the time service stub. Static check via import + AST scan.
    with open(tr.__file__, encoding="utf-8") as fh:
        src = fh.read()
    assert "self.time = time_pb2_grpc.TimeServiceStub" in src, (
        "GrpcTransport doesn't construct time stub — TASK-208 incomplete"
    )


@pytest.mark.spec
def test_209_translate_recognizes_time_details() -> None:
    """209-translate — _time_class_for() returns the right typed class
    for each time-mgmt detail string. Indirectly tested through the
    full translate_rpc_error path with a fake exc."""
    # Synthesize a minimal exc-like object with code() + details().
    class FakeExc(Exception):  # noqa: N818 — test fixture, not a real error type
        def __init__(self, code_name: str, detail: str) -> None:
            super().__init__(detail)
            self._code_name = code_name
            self._detail = detail

        def code(self) -> object:
            class C:
                def __init__(self, n: str) -> None:
                    self.name = n
            return C(self._code_name)

        def details(self) -> str:
            return self._detail

    cases = {
        "federate is already time-regulating": ge.TimeRegulationAlreadyEnabled,
        "federate is not time-regulating": ge.TimeRegulationNotEnabled,
        "federate is already time-constrained": ge.TimeConstrainedAlreadyEnabled,
        "federate is not time-constrained": ge.TimeConstrainedNotEnabled,
        "lookahead must be non-negative and finite": ge.InvalidLookahead,
        "requested time is not greater than current logical time": ge.LogicalTimeAlreadyPassed,
        "federate has an outstanding advance request": ge.TimeAdvancingState,
        "federation halted": ge.FederationHaltedError,
    }
    for detail, expected_cls in cases.items():
        exc = FakeExc("FAILED_PRECONDITION", detail)
        with pytest.raises(expected_cls):
            ge.translate_rpc_error(exc)


# Cross-language end-to-end cases (Python ↔ Go rtid) live in TASK-215;
# they require a real rtid subprocess + cross-process orchestration.
# Placeholders kept here for traceability:


@pytest.mark.spec
@pytest.mark.integration
def test_209_2_go_regulator_python_constrained() -> None:
    pytest.skip("Cross-language end-to-end — coverage at TASK-215 (cross_language test).")


@pytest.mark.spec
@pytest.mark.integration
def test_209_3_python_regulator_go_constrained() -> None:
    pytest.skip("Cross-language end-to-end — coverage at TASK-215.")


@pytest.mark.spec
@pytest.mark.integration
def test_209_4_mixed_primitives_cross_language() -> None:
    pytest.skip("Cross-language end-to-end — coverage at TASK-215.")
