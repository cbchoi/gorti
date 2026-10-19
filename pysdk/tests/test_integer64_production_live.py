"""Exact Integer64 time through real Python SDK/server connections."""

from __future__ import annotations

import asyncio
import os
from contextlib import AsyncExitStack

import pytest

from rti1516e import errors
from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.events import OrderType, ReceiveInteraction, TimeAdvanceGrant

from .test_live_service_e2e import (
    BASE_SIGNAL,
    LiveCallbacks,
    live_artifacts,  # noqa: F401
    live_server,  # noqa: F401
)

pytestmark = pytest.mark.skipif(
    os.environ.get("GORTI_RUN_LIVE_E2E") != "1", reason="production localhost tests are opt-in"
)


@pytest.mark.parametrize(
    "advance",
    [
        "time_advance_request",
        "time_advance_request_available",
        "next_message_request",
        "next_message_request_available",
        "flush_queue_request",
    ],
)
def test_integer64_exact_advance_and_wrong_representation(live_server, advance):  # noqa: F811
    async def exercise():
        url, base, _ = live_server
        async with asyncio.timeout(30), AsyncExitStack() as stack:
            conn = await stack.enter_async_context(RtiConnection.connect(url))
            spec = FederationSpec(
                "integer-advance", [str(base)], logical_time_implementation_name="HLAinteger64Time"
            )
            member = await stack.enter_async_context(
                conn.join_federation(spec, federate_name="exact")
            )
            callbacks = LiveCallbacks(member)
            stack.push_async_callback(callbacks.close)
            assert member.logical_time_implementation_name == "HLAinteger64Time"
            initial = await member.query_logical_time()
            assert type(initial) is int and initial == 0
            for target in ((1 << 53) + 1, (1 << 53) + 2, (1 << 63) - 1):
                await getattr(member, advance)(target)
                event = await callbacks.wait(
                    TimeAdvanceGrant, lambda event, target=target: event.time == target
                )
                assert type(event.time) is int
                observed = await member.query_logical_time()
                assert type(observed) is int and observed == target
            for invalid in (float((1 << 53) + 2), -1, 1 << 63, True):
                with pytest.raises(errors.InvalidLogicalTime):
                    await member.time_advance_request(invalid)
                assert await member.query_logical_time() == (1 << 63) - 1

    asyncio.run(exercise())


def test_integer64_two_federates_preserve_adjacent_tso_before_grant(live_server):  # noqa: F811
    async def exercise():
        url, base, _ = live_server
        async with asyncio.timeout(30), AsyncExitStack() as stack:
            left = await stack.enter_async_context(RtiConnection.connect(url))
            right = await stack.enter_async_context(RtiConnection.connect(url))
            spec = FederationSpec(
                "integer-tso", [str(base)], logical_time_implementation_name="HLAinteger64Time"
            )
            sender = await stack.enter_async_context(
                left.join_federation(spec, federate_name="sender")
            )
            receiver = await stack.enter_async_context(
                right.join_federation(spec, federate_name="receiver")
            )
            sent, received = LiveCallbacks(sender), LiveCallbacks(receiver)
            stack.push_async_callback(sent.close)
            stack.push_async_callback(received.close)
            await sender.publish_interaction_class(BASE_SIGNAL)
            await receiver.subscribe_interaction_class(BASE_SIGNAL)
            signal = await sender.support.get_interaction_class_handle(BASE_SIGNAL)
            await sender.change_interaction_order_type(signal, OrderType.TIMESTAMP)
            await sender.enable_time_regulation(1)
            await receiver.enable_time_constrained()
            targets = ((1 << 53) + 1, (1 << 53) + 2)
            for target in reversed(targets):
                await sender.send_interaction(
                    BASE_SIGNAL, {"Payload": str(target).encode("ascii")}, timestamp=target
                )
            await sender.time_advance_request(targets[-1])
            await sent.wait(TimeAdvanceGrant, lambda event: event.time == targets[-1])
            await receiver.time_advance_request(targets[-1])
            grant = await received.wait(TimeAdvanceGrant, lambda event: event.time == targets[-1])
            messages = [event for event in received.events if isinstance(event, ReceiveInteraction)]
            assert [event.timestamp for event in messages] == list(targets)
            assert all(type(event.timestamp) is int for event in messages)
            assert [event.parameters["Payload"] for event in messages] == [
                str(target).encode("ascii") for target in targets
            ]
            assert all(
                received.events.index(event) < received.events.index(grant) for event in messages
            )
            assert await receiver.query_logical_time() == targets[-1]

    asyncio.run(exercise())
