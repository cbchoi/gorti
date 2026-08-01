"""TASK-245 (M22 W4) — Python-side acceptance gate for AC §3.

Per AC §3.1-3.4: pysdk surface parity, dispatch correctness, typed
exceptions for the new errors. Most assertions delegate to the
W1/W2 spec tests; this file binds the AC rows to surface checks.
"""

from __future__ import annotations

import inspect

import pytest

from rti1516e import _grpc_errors as ge
from rti1516e import _transport as tr
from rti1516e.connection import Federate
from rti1516e.standard import Rti1516eAmbassador


@pytest.mark.spec
def test_ac_3_1_pysdk_federate_surface_complete() -> None:
    """AC §3.1 — Federate exposes all 15 time methods.

    Detailed list-and-arity check in test_pysdk_time_surface.py;
    this is the cross-reference asserting the W1 surface plus the
    W2 async-delivery additions are all present.
    """
    must_have = [
        # M21 + M22 W1 (mechanical surface parity)
        "enable_time_regulation", "disable_time_regulation",
        "enable_time_constrained", "disable_time_constrained",
        "modify_lookahead",
        "next_message_request", "next_message_request_available",
        "time_advance_request", "time_advance_request_available",
        "flush_queue_request",
        "query_logical_time", "query_lookahead", "query_lbts",
        # M22 W2 (async delivery)
        "enable_asynchronous_delivery", "disable_asynchronous_delivery",
    ]
    missing = [m for m in must_have if not hasattr(Federate, m)]
    assert not missing, f"Federate missing methods: {missing}"
    assert len(must_have) == 15


@pytest.mark.spec
def test_ac_3_2_ambassador_surface_complete() -> None:
    """AC §3.2 — Rti1516eAmbassador exposes 15 camelCase methods."""
    must_have = [
        "enableTimeRegulation", "disableTimeRegulation",
        "enableTimeConstrained", "disableTimeConstrained",
        "modifyLookahead",
        "nextMessageRequest", "nextMessageRequestAvailable",
        "timeAdvanceRequest", "timeAdvanceRequestAvailable",
        "flushQueueRequest",
        "queryLogicalTime", "queryLookahead", "queryLBTS",
        "enableAsynchronousDelivery", "disableAsynchronousDelivery",
    ]
    missing = [m for m in must_have if not hasattr(Rti1516eAmbassador, m)]
    assert not missing, f"Ambassador missing methods: {missing}"
    assert len(must_have) == 15


@pytest.mark.spec
def test_ac_3_3_workaround_removed() -> None:
    """AC §3.3 — pyjevsim-time-advance regulator no longer carries
    the retry-on-TimeAdvancingState backoff loop.

    This source-level check prevents the removed retry workaround from
    returning unnoticed.
    """
    from pathlib import Path
    src = Path(
        __file__
    ).resolve().parents[4] / "examples" / "pyjevsim-time-advance" / "regulator_main.py"
    text = src.read_text(encoding="utf-8")
    # The fix uses wait_for_full_grant; the workaround pattern was a
    # retry loop catching TimeAdvancingState. Check the *behavior*
    # markers — "TimeAdvancingState" alone may appear in docstrings
    # describing what was removed.
    assert "for attempt in range(8)" not in text, (
        "retry-with-backoff workaround is still present"
    )
    assert "except TimeAdvancingState" not in text, (
        "TimeAdvancingState retry handler still present"
    )
    # The fix's helper must be in use.
    assert "wait_for_full_grant" in text, (
        "wait_for_full_grant not invoked — W3 SDK fix not adopted by example"
    )


@pytest.mark.spec
def test_ac_3_4_typed_exceptions_async_delivery() -> None:
    """AC §3.4 — pysdk has typed exceptions for the new async-delivery errors."""
    expected = {"TimeAlreadyAsynchronous", "TimeNotAsynchronous"}
    actual = {n for n in dir(ge) if not n.startswith("_")}
    missing = expected - actual
    assert not missing, f"typed exceptions missing: {missing}"
    # Verify code allocation continues the M21 700-707 range.
    assert ge.TimeAlreadyAsynchronous.error_code == 708
    assert ge.TimeNotAsynchronous.error_code == 709


@pytest.mark.spec
def test_ac_3_5_dispatch_branches_complete() -> None:
    """AC §3.5 — every public Federate time method has a dispatch
    branch in GrpcTransport.record. Catches surface-only-no-dispatch
    drift.
    """
    src = inspect.getsource(tr.GrpcTransport.record)
    # The methods that must be dispatched:
    methods = [
        "enable_time_regulation", "disable_time_regulation",
        "enable_time_constrained", "disable_time_constrained",
        "modify_lookahead",
        "next_message_request", "next_message_request_available",
        "time_advance_request", "time_advance_request_available",
        "flush_queue_request",
        "query_logical_time", "query_lookahead", "query_lbts",
        "enable_asynchronous_delivery", "disable_asynchronous_delivery",
    ]
    missing = [m for m in methods if f'method == "{m}"' not in src]
    assert not missing, f"dispatch branches missing for: {missing}"
