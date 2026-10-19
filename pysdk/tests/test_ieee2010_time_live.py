"""IEEE 1516.1-2010 time semantics across two real Python federates.

Opt in with GORTI_RUN_LIVE_E2E=1. Reuse the live-service fixtures so rtid,
FOMs and server logs remain outside the repository. Source: 8.1.2 Tables
1/2, 8.1.5, 8.1.7, 8.6-8.8, 8.13, 8.14, and 8.18 (active redline text only).
"""

from __future__ import annotations

import asyncio
import os
import struct
from contextlib import AsyncExitStack, asynccontextmanager

import pytest

from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.events import (
    OrderType,
    ReceiveInteraction,
    SynchronizationPointAnnounced,
    TimeAdvanceGrant,
)
from rti1516e.standard import Rti1516eAmbassador

from .test_live_service_e2e import (
    BASE_SIGNAL,
    LiveCallbacks,
    live_artifacts,  # noqa: F401
    live_server,  # noqa: F401
)

pytestmark = pytest.mark.skipif(
    os.environ.get("GORTI_RUN_LIVE_E2E") != "1", reason="production localhost tests are opt-in"
)


@asynccontextmanager
async def _participants(server, *, constrained):
    url, base, _ = server
    async with asyncio.timeout(45), AsyncExitStack() as stack:
        left = await stack.enter_async_context(RtiConnection.connect(url))
        right = await stack.enter_async_context(RtiConnection.connect(url))
        spec = FederationSpec("ieee2010-time-live", [str(base)])
        sender = await stack.enter_async_context(left.join_federation(spec, federate_name="sender"))
        receiver = await stack.enter_async_context(
            right.join_federation(spec, federate_name="receiver")
        )
        sent, received = LiveCallbacks(sender), LiveCallbacks(receiver)
        stack.push_async_callback(sent.close)
        stack.push_async_callback(received.close)
        assert left.transport is not right.transport
        await sender.publish_interaction_class(BASE_SIGNAL)
        await receiver.subscribe_interaction_class(BASE_SIGNAL)
        signal = await sender.support.get_interaction_class_handle(BASE_SIGNAL)
        await sender.change_interaction_order_type(signal, OrderType.TIMESTAMP)
        await sender.enable_time_regulation(0.5)
        assert await sender.query_lookahead() == 0.5
        if constrained:
            await receiver.enable_time_constrained()
        else:
            await receiver.enable_time_regulation(0.5)
            assert await receiver.query_lookahead() == 0.5
        state = await receiver.mom.query_federate_attributes(receiver.handle)
        assert state.found
        assert state.time_constrained is constrained
        assert state.time_regulating is (not constrained)
        assert await receiver.query_logical_time() == 0
        yield sender, receiver, sent, received


def _assert_timestamped(event, *, sender, payload, tag, timestamp, handle, received_order):
    assert event.class_name == "BaseSignal"
    assert event.parameters == {"Payload": payload}
    assert event.tag == tag
    assert event.timestamp == timestamp
    assert event.metadata is not None
    assert event.metadata.sent_order == OrderType.TIMESTAMP
    assert event.metadata.received_order == received_order
    assert event.metadata.producing_federate == sender.handle
    assert event.metadata.message_retraction_handle == handle


@pytest.mark.parametrize(
    "enable_before_send", [True, False], ids=["already-enabled", "queued-first"]
)
def test_constrained_async_keeps_tso_until_tar_and_delivers_before_tag(
    live_server,  # noqa: F811
    enable_before_send,
):
    async def exercise():
        async with _participants(live_server, constrained=True) as (
            sender,
            receiver,
            sent,
            received,
        ):
            if enable_before_send:
                await receiver.enable_asynchronous_delivery()
            messages = [
                (10.0, 41, struct.pack(">d", 10.25), b"\x00\xfffuture-ten"),
                (5.0, 42, struct.pack(">d", 5.25), b"\xff\x00future-five"),
            ]
            for timestamp, handle, payload, tag in messages:
                await sender.send_interaction(
                    BASE_SIGNAL,
                    {"Payload": payload},
                    timestamp=timestamp,
                    retraction_handle=handle,
                    tag=tag,
                )
            if not enable_before_send:
                await receiver.enable_asynchronous_delivery()

            # Establish receiver-stream progress both before and after GALT advances.
            markers = (b"\x00before-sender-advance\xff", b"\x00after-sender-advance\xff")
            for index, marker in enumerate(markers):
                await sender.send_interaction(BASE_SIGNAL, {"Payload": b"barrier"}, tag=marker)
                await received.wait(ReceiveInteraction, lambda event, tag=marker: event.tag == tag)
                assert await receiver.query_logical_time() == 0
                early = [
                    event
                    for event in received.events
                    if isinstance(event, ReceiveInteraction) and event.tag not in markers
                ]
                assert early == [], f"future TSO escaped before receiver TAR: {early!r}"
                if index == 0:
                    await sender.time_advance_request(10)
                    await sent.wait(TimeAdvanceGrant, lambda event: event.time == 10)

            await receiver.time_advance_request(10)
            grant = await received.wait(TimeAdvanceGrant, lambda event: event.time == 10)
            deliveries = [
                event
                for event in received.events
                if isinstance(event, ReceiveInteraction) and event.tag not in markers
            ]
            assert [event.timestamp for event in deliveries] == [5, 10]
            for event, (timestamp, handle, payload, tag) in zip(
                deliveries, reversed(messages), strict=True
            ):
                _assert_timestamped(
                    event,
                    sender=sender,
                    payload=payload,
                    tag=tag,
                    timestamp=timestamp,
                    handle=handle,
                    received_order=OrderType.TIMESTAMP,
                )
                assert received.events.index(event) < received.events.index(grant)
            assert await receiver.query_logical_time() == 10

    asyncio.run(exercise())


def test_regulating_only_receiver_gets_timestamped_message_as_ro(live_server):  # noqa: F811
    async def exercise():
        async with _participants(live_server, constrained=False) as (sender, receiver, _, received):
            payload, tag = struct.pack(">d", 123.5), b"\x00\xffregulating-only"
            await sender.send_interaction(
                BASE_SIGNAL,
                {"Payload": payload},
                timestamp=10,
                retraction_handle=57,
                tag=tag,
            )
            marker = b"ro-after-timestamped"
            await sender.send_interaction(BASE_SIGNAL, {"Payload": b"barrier"}, tag=marker)
            await received.wait(ReceiveInteraction, lambda event: event.tag == marker)
            # No TAR or asynchronous-delivery enable: unconstrained means RO eligibility.
            event = await received.wait(ReceiveInteraction, lambda event: event.tag == tag)
            _assert_timestamped(
                event,
                sender=sender,
                payload=payload,
                tag=tag,
                timestamp=10,
                handle=57,
                received_order=OrderType.RECEIVE,
            )
            assert await receiver.query_logical_time() == 0
            assert not any(isinstance(event, TimeAdvanceGrant) for event in received.events)

    asyncio.run(exercise())


def test_disable_constrained_releases_queued_and_subsequent_messages_as_ro(
    live_server,  # noqa: F811
):
    async def exercise():
        async with _participants(live_server, constrained=True) as (sender, receiver, _, received):
            payload, tag = struct.pack(">d", 71.5), b"\x00\xffqueued-before-disable"
            await sender.send_interaction(
                BASE_SIGNAL, {"Payload": payload}, timestamp=10, retraction_handle=71, tag=tag
            )
            # Synchronization announcements progress even while RO/TSO delivery is gated.
            label = "timestamped-message-queued"
            await sender.sync.register_synchronization_point(label)
            await received.wait(SynchronizationPointAnnounced, lambda event: event.label == label)
            assert not any(isinstance(event, ReceiveInteraction) for event in received.events)

            await receiver.disable_time_constrained()
            state = await receiver.mom.query_federate_attributes(receiver.handle)
            assert state.found and state.time_constrained is False
            queued = await received.wait(ReceiveInteraction, lambda event: event.tag == tag)
            _assert_timestamped(
                queued,
                sender=sender,
                payload=payload,
                tag=tag,
                timestamp=10,
                handle=71,
                received_order=OrderType.RECEIVE,
            )
            later_tag = b"\xff\x00sent-after-disable"
            await sender.send_interaction(
                BASE_SIGNAL,
                {"Payload": payload},
                timestamp=11,
                retraction_handle=72,
                tag=later_tag,
            )
            later = await received.wait(ReceiveInteraction, lambda event: event.tag == later_tag)
            _assert_timestamped(
                later,
                sender=sender,
                payload=payload,
                tag=later_tag,
                timestamp=11,
                handle=72,
                received_order=OrderType.RECEIVE,
            )
            assert await receiver.query_logical_time() == 0
            assert not any(isinstance(event, TimeAdvanceGrant) for event in received.events)

    asyncio.run(exercise())


def test_standard_query_lits_includes_galt_with_empty_tso_buffer(live_server):  # noqa: F811
    url, base, _ = live_server
    sender, receiver = Rti1516eAmbassador(), Rti1516eAmbassador()
    try:
        sender.connect(sender, url)
        receiver.connect(receiver, url)
        sender.createFederationExecution("ieee2010-lits-live", [str(base)])
        sender.joinFederationExecution("sender", "ieee2010-lits-live")
        receiver.joinFederationExecution("receiver", "ieee2010-lits-live")
        sender.enableTimeRegulation(0.5)
        assert sender.queryLookahead() == 0.5
        receiver.enableTimeConstrained()
        galt, galt_valid = receiver.queryGALT()
        assert galt_valid is True and galt == 0.5
        lits, lits_valid = receiver.queryLITS()
        assert lits_valid is True, "empty TSO buffer still has the finite GALT input bound"
        assert lits == galt
    finally:
        receiver.disconnect()
        sender.disconnect()
