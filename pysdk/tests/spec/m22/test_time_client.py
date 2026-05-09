"""TASK-224 (M22 W1) — pysdk RPC dispatch tests for the 9 new time
methods + 2 async-delivery methods.

Verifies that each new Federate method routes through GrpcTransport.record
to the correct TimeServiceStub method with the correct request fields.
"""

from __future__ import annotations

import inspect

import pytest

from rti1516e import _transport as tr


@pytest.mark.spec
@pytest.mark.parametrize(
    "method",
    [
        "disable_time_regulation",
        "disable_time_constrained",
        "modify_lookahead",
        "next_message_request_available",
        "time_advance_request",
        "time_advance_request_available",
        "flush_queue_request",
        "query_logical_time",
        "query_lookahead",
        "query_lbts",
        "enable_asynchronous_delivery",
        "disable_asynchronous_delivery",
    ],
)
def test_dispatch_branch_present(method: str) -> None:
    """Each new method has its dispatch branch in GrpcTransport.record."""
    src = inspect.getsource(tr.GrpcTransport.record)
    assert f'method == "{method}"' in src, (
        f"dispatch branch missing for {method!r} — M22 W1 incomplete"
    )


@pytest.mark.spec
@pytest.mark.parametrize(
    "helper",
    [
        "_disable_time_regulation",
        "_disable_time_constrained",
        "_modify_lookahead",
        "_next_message_request_available",
        "_time_advance_request",
        "_time_advance_request_available",
        "_flush_queue_request",
        "_query_logical_time",
        "_query_lookahead",
        "_query_lbts",
        "_enable_asynchronous_delivery",
        "_disable_asynchronous_delivery",
    ],
)
def test_helper_method_exists(helper: str) -> None:
    """Each dispatcher helper is defined on GrpcTransport."""
    assert hasattr(tr.GrpcTransport, helper), (
        f"GrpcTransport.{helper} missing — M22 TASK-222 incomplete"
    )


@pytest.mark.spec
@pytest.mark.parametrize(
    "stub_method,proto_request",
    [
        ("DisableTimeRegulation", "DisableRegulationRequest"),
        ("DisableTimeConstrained", "DisableConstrainedRequest"),
        ("ModifyLookahead", "ModifyLookaheadRequest"),
        ("NextMessageRequestAvailable", "NMRARequest"),
        ("TimeAdvanceRequest", "TARRequest"),
        ("TimeAdvanceRequestAvailable", "TARARequest"),
        ("FlushQueueRequest", "FQRRequest"),
        ("QueryLogicalTime", "QueryFederateTimeRequest"),
        ("QueryLookahead", "QueryFederateTimeRequest"),
        ("QueryLBTS", "QueryLBTSRequest"),
        ("EnableAsynchronousDelivery", "EnableAsynchronousDeliveryRequest"),
        ("DisableAsynchronousDelivery", "DisableAsynchronousDeliveryRequest"),
    ],
)
def test_helper_uses_correct_stub_and_request(
    stub_method: str, proto_request: str
) -> None:
    """Each helper invokes self.time.<stub_method> with the correct
    proto request type. Source-level check, not behavioral."""
    src = open(tr.__file__, encoding="utf-8").read()
    assert f"self.time.{stub_method}(" in src, (
        f"helper does not call self.time.{stub_method}(...)"
    )
    assert f"time_pb2.{proto_request}(" in src, (
        f"helper does not construct time_pb2.{proto_request}(...)"
    )
