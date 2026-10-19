"""Selected Integer64 transport values never pass through a double."""

import asyncio
from unittest.mock import AsyncMock, Mock

import pytest

from rti1516e._logical_time import INTEGER64_TIME, read_time, validate_time, write_time
from rti1516e._transport import GrpcTransport, _ensure_generated_path
from rti1516e.errors import InvalidLogicalTime

_ensure_generated_path()
from rti.v1 import object_pb2, stream_pb2, time_pb2  # noqa: E402


@pytest.mark.parametrize("value", [0, (1 << 53) + 1, (1 << 63) - 1])
def test_integer_request_round_trip(value):
    request = object_pb2.SendInteractionRequest()
    write_time(request, "logical_time", value, INTEGER64_TIME)
    assert not request.HasField("logical_time")
    parsed = object_pb2.SendInteractionRequest.FromString(request.SerializeToString())
    assert read_time(parsed, "logical_time", INTEGER64_TIME, optional=True) == value


@pytest.mark.parametrize("value", [1.0, float((1 << 53) + 1), True, -1, 1 << 63])
def test_integer_rejects_mixed_or_invalid_values(value):
    with pytest.raises(InvalidLogicalTime):
        validate_time(value, INTEGER64_TIME)


def test_missing_and_conflicting_integer_fields_rejected():
    request = object_pb2.SendInteractionRequest(logical_time=0)
    with pytest.raises(InvalidLogicalTime):
        read_time(request, "logical_time", INTEGER64_TIME)
    request.logical_time_value.integer64_value = 0
    with pytest.raises(InvalidLogicalTime):
        read_time(request, "logical_time", INTEGER64_TIME)
    request.ClearField("logical_time")
    request.logical_time_value.float64_value = 0
    with pytest.raises(InvalidLogicalTime):
        read_time(request, "logical_time", INTEGER64_TIME)


@pytest.mark.parametrize(
    "method",
    [
        "_next_message_request",
        "_next_message_request_available",
        "_time_advance_request",
        "_time_advance_request_available",
        "_flush_queue_request",
    ],
)
def test_advance_exact_and_fail_before_rpc(method):
    transport = GrpcTransport(Mock(), "grpc://unused")
    transport._federation_name = "integer"
    transport._logical_time_by_federation["integer"] = INTEGER64_TIME
    transport.time = Mock()
    for name in [
        "NextMessageRequest",
        "NextMessageRequestAvailable",
        "TimeAdvanceRequest",
        "TimeAdvanceRequestAvailable",
        "FlushQueueRequest",
    ]:
        setattr(transport.time, name, AsyncMock())
    asyncio.run(getattr(transport, method)(1, (1 << 53) + 1))
    called = [
        getattr(transport.time, name)
        for name in [
            "NextMessageRequest",
            "NextMessageRequestAvailable",
            "TimeAdvanceRequest",
            "TimeAdvanceRequestAvailable",
            "FlushQueueRequest",
        ]
        if getattr(transport.time, name).await_count
    ]
    assert len(called) == 1
    request = called[0].await_args.args[0]
    assert request.logical_time == 0
    assert request.logical_time_value.integer64_value == (1 << 53) + 1
    with pytest.raises(InvalidLogicalTime):
        asyncio.run(getattr(transport, method)(1, 1.0))
    assert called[0].await_count == 1


def test_grant_and_query_preserve_integer():
    transport = GrpcTransport(Mock(), "grpc://unused")
    transport._federation_name = "integer"
    transport._logical_time_by_federation["integer"] = INTEGER64_TIME
    grant = stream_pb2.FederateEvent()
    grant.grant.logical_time_value.integer64_value = (1 << 53) + 1
    assert transport._translate_event(grant).time == (1 << 53) + 1
    response = time_pb2.QueryFederateTimeResponse()
    response.logical_time_value.integer64_value = (1 << 63) - 1
    transport.time.QueryLogicalTime = AsyncMock(return_value=response)
    result = asyncio.run(transport._query_logical_time(1))
    assert type(result) is int and result == (1 << 63) - 1


def test_legacy_float_final_time_remains_legacy_only():
    from rti.v1 import time_pb2

    from rti1516e._logical_time import FLOAT64_TIME, read_time, write_time

    request = time_pb2.TARRequest()
    write_time(request, "logical_time", float("inf"), FLOAT64_TIME)
    assert request.logical_time == float("inf")
    assert not request.HasField("logical_time_value")
    assert read_time(request, "logical_time", FLOAT64_TIME) == float("inf")


def test_mom_nested_integer_time_and_lookahead_are_exact():
    from rti.v1 import mom_pb2

    from rti1516e.mom import MomClient

    client = MomClient(
        Mock(),
        federation_name="integer",
        federate_handle=1,
        logical_time_implementation_name=INTEGER64_TIME,
    )
    response = mom_pb2.QueryFederateAttributesResponse(found=True)
    response.logical_time.logical_time_value.integer64_value = (1 << 53) + 1
    response.lookahead.logical_time_value.integer64_value = (1 << 63) - 1
    client._stub = Mock()
    client._stub.QueryFederateAttributes = AsyncMock(return_value=response)
    result = asyncio.run(client.query_federate_attributes(1))
    assert result.logical_time_value == result.logical_time == (1 << 53) + 1
    assert result.lookahead_value == result.lookahead == (1 << 63) - 1
    assert type(result.logical_time_value) is int
