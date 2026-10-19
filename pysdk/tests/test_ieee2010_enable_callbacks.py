"""Wire and ambassador contracts for IEEE 1516.1-2010 sections 8.3 and 8.6."""

from __future__ import annotations

import asyncio
import contextlib
import sys
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest

import rti1516e
from rti1516e import events
from rti1516e._grpc_errors import (
    RequestForTimeConstrainedPending,
    RequestForTimeRegulationPending,
    _time_class_for,
)
from rti1516e._transport import GrpcTransport
from rti1516e.standard import Rti1516eAmbassador

sys.path.insert(0, str(Path(rti1516e.__file__).parent / "_generated"))


@pytest.mark.parametrize(
    ("wire_name", "event_name", "callback_name"),
    [
        ("time_regulation_enabled", "TimeRegulationEnabled", "timeRegulationEnabled"),
        ("time_constrained_enabled", "TimeConstrainedEnabled", "timeConstrainedEnabled"),
    ],
)
@pytest.mark.parametrize("logical_time", [0.0, 7.25])
def test_enable_callback_wire_translation_and_ambassador(
    wire_name, event_name, callback_name, logical_time
):
    from rti.v1 import stream_pb2

    event_type = getattr(events, event_name, None)
    assert event_type is not None, "activation must have a typed SDK event"
    assert getattr(rti1516e, event_name) is event_type
    encoded = stream_pb2.FederateEvent(
        **{wire_name: {"logical_time": logical_time}}, callback_receipt=b"receipt"
    ).SerializeToString()
    wire = stream_pb2.FederateEvent.FromString(encoded)
    translated = object.__new__(GrpcTransport)._translate_event(wire)
    assert isinstance(translated, event_type)
    assert translated.time == logical_time
    calls = []
    ambassador = Rti1516eAmbassador()
    ambassador._callback_target = SimpleNamespace(
        **{callback_name: lambda t: calls.append(t)}
    )
    assert ambassador._invoke_event(translated)
    assert calls == [logical_time]
    assert callable(getattr(Rti1516eAmbassador, callback_name, None))


@pytest.mark.parametrize(
    ("detail", "error_type"),
    [
        ("request for time-regulation pending", RequestForTimeRegulationPending),
        ("request for time-constrained pending", RequestForTimeConstrainedPending),
    ],
)
def test_pending_enable_fallback_error_identity(detail, error_type):
    assert _time_class_for(detail) is error_type


@pytest.mark.asyncio
@pytest.mark.parametrize("regulation", [True, False])
async def test_enable_rpc_acceptance_does_not_synthesize_callback(regulation):
    transport = object.__new__(GrpcTransport)
    transport._federation_name = "test"
    transport.time = SimpleNamespace(
        EnableTimeRegulation=AsyncMock(), EnableTimeConstrained=AsyncMock()
    )
    delivered = []
    transport._deliver_event = lambda *args: delivered.append(args)
    if regulation:
        await transport._enable_time_regulation(7, 2)
        transport.time.EnableTimeRegulation.assert_awaited_once()
    else:
        await transport._enable_time_constrained(7)
        transport.time.EnableTimeConstrained.assert_awaited_once()
    assert delivered == []


def callback_race_transport():
    transport = object.__new__(GrpcTransport)
    transport._federation_name = "test"
    transport._event_queues = {}
    transport._event_sinks = {}
    transport._stream_tasks = {}
    transport._session_tokens = {}
    transport._identity_bindings = {}
    return transport


@pytest.mark.asyncio
@pytest.mark.parametrize("regulation", [True, False])
@pytest.mark.parametrize("delivery", ["queue", "direct", "after-response"])
async def test_real_enable_event_before_or_after_rpc_response(regulation, delivery):
    from rti.v1 import common_pb2, stream_pb2, time_pb2

    transport = callback_race_transport()
    incoming = asyncio.Queue()
    observed = asyncio.Event()
    response_allowed = asyncio.Event()
    current_time = [3.0]
    direct = []
    original_delivery = transport._deliver_event

    def deliver(handle, event):
        original_delivery(handle, event)
        observed.set()

    transport._deliver_event = deliver

    async def wire_events(_request):
        while True:
            yield await incoming.get()

    wire_name = "time_regulation_enabled" if regulation else "time_constrained_enabled"
    event_type = events.TimeRegulationEnabled if regulation else events.TimeConstrainedEnabled
    wire = stream_pb2.FederateEvent(**{wire_name: {"logical_time": 7.25}})

    async def enable(_request):
        assert 7 in transport._stream_tasks
        assert 7 in transport._event_queues
        if delivery != "after-response":
            current_time[0] = 7.25
            incoming.put_nowait(wire)
            await response_allowed.wait()
        return common_pb2.Empty()

    async def query(_request):
        return time_pb2.QueryFederateTimeResponse(logical_time=current_time[0])

    transport.streams = SimpleNamespace(Events=wire_events)
    transport.time = SimpleNamespace(
        EnableTimeRegulation=enable,
        EnableTimeConstrained=enable,
        QueryLogicalTime=query,
    )
    transport._start_event_stream(7)
    if delivery == "direct":
        transport.set_event_sink(7, direct.append)
    operation = asyncio.create_task(
        transport._enable_time_regulation(7, 2)
        if regulation else transport._enable_time_constrained(7)
    )
    try:
        if delivery == "after-response":
            await asyncio.wait_for(operation, 1)
            assert not observed.is_set()
            assert await transport._query_logical_time(7) == 3.0
            current_time[0] = 7.25
            incoming.put_nowait(wire)
        await asyncio.wait_for(observed.wait(), 1)
        if delivery != "after-response":
            assert not operation.done(), "callback must be observable before unary completion"
        assert await transport._query_logical_time(7) == 7.25
        event = direct[0] if delivery == "direct" else transport.events_for(7).get_nowait()
        assert isinstance(event, event_type) and event.time == 7.25
        response_allowed.set()
        await asyncio.wait_for(operation, 1)
        assert transport.events_for(7).empty()
        assert len(direct) == (1 if delivery == "direct" else 0)
    finally:
        operation.cancel()
        with contextlib.suppress(asyncio.CancelledError):
            await operation
        task = transport._stream_tasks[7]
        task.cancel()
        with contextlib.suppress(asyncio.CancelledError):
            await task


@pytest.mark.asyncio
@pytest.mark.parametrize("stale_epoch", [False, True])
async def test_direct_callback_handoff_drains_current_epoch_backlog(stale_epoch):
    transport = callback_race_transport()
    transport._identity_bindings[7] = lambda _: None
    transport._restore_serial = 0
    if stale_epoch:
        transport._deliver_event(7, events.TimeRegulationEnabled(1.0))
        transport._restore_serial = 1
    transport._deliver_event(7, events.TimeRegulationEnabled(7.25))
    transport._deliver_event(7, events.TimeConstrainedEnabled(7.25))
    received = []
    transport.set_event_sink(7, received.append)
    transport._deliver_event(7, events.TimeAdvanceGrant(8.0))
    assert received == [
        events.TimeRegulationEnabled(7.25),
        events.TimeConstrainedEnabled(7.25),
        events.TimeAdvanceGrant(8.0),
    ]
    assert transport.events_for(7).empty()
