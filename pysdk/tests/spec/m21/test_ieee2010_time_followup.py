"""W1 time boundaries through the real server and two Python connections.

Source: S1516.1 active 2010 text, 8.1.3-8.1.4/8.1.7 (PDF 184/189),
8.19/8.19.3(e)/8.19.5(b) (PDF 213-214), and 8.20 (PDF 214).
Source identities and reviewed locators are recorded in
engineering/verification/ieee1516-2010-normative-review-20260921.md.

Reuse the existing live fixtures, without their module's opt-in marker.
The normal pytest runner builds a fresh rtid and owns each server process;
GORTI_LIVE_ARTIFACTS selects an external directory for all server artifacts.
These are independent connections, not independent federate OS processes.
"""

from __future__ import annotations

import asyncio
import math
from contextlib import AsyncExitStack, asynccontextmanager

import pytest

from rti1516e._grpc_errors import TimeAdvancingState
from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.errors import InvalidLogicalTime
from rti1516e.events import (
    OrderType,
    ReceiveInteraction,
    SynchronizationPointAnnounced,
    TimeAdvanceGrant,
)
from tests.test_live_service_e2e import (
    BASE_SIGNAL,
    LiveCallbacks,
    live_artifacts,  # noqa: F401
    live_server,  # noqa: F401
)

pytestmark = [pytest.mark.spec, pytest.mark.integration]


@asynccontextmanager
async def _participants(server, *, lookahead):
    url, base, _ = server
    async with asyncio.timeout(45), AsyncExitStack() as stack:
        left = await stack.enter_async_context(RtiConnection.connect(url))
        right = await stack.enter_async_context(RtiConnection.connect(url))
        spec = FederationSpec("ieee2010-time-followup", [str(base)])
        sender = await stack.enter_async_context(left.join_federation(spec, federate_name="sender"))
        blocker = await stack.enter_async_context(
            right.join_federation(spec, federate_name="blocker")
        )
        sent, received = LiveCallbacks(sender), LiveCallbacks(blocker)
        stack.push_async_callback(sent.close)
        stack.push_async_callback(received.close)
        assert left.transport is not right.transport
        await sender.publish_interaction_class(BASE_SIGNAL)
        await blocker.subscribe_interaction_class(BASE_SIGNAL)
        signal = await sender.support.get_interaction_class_handle(BASE_SIGNAL)
        await sender.change_interaction_order_type(signal, OrderType.TIMESTAMP)
        await sender.enable_time_regulation(lookahead)
        await sender.enable_time_constrained()
        await blocker.enable_time_regulation(0.5)
        assert await sender.query_lookahead() == lookahead
        assert await blocker.query_lookahead() == 0.5
        for federate in (sender, blocker):
            assert await federate.query_logical_time() == 0
        yield sender, blocker, sent, received


async def _barrier(sender, sent, received, label):
    # Sync announcements are not gated by RO/TSO time-delivery eligibility.
    await sender.sync.register_synchronization_point(label)
    for callbacks in (sent, received):
        await callbacks.wait(SynchronizationPointAnnounced, lambda event: event.label == label)


async def _assert_pending(sender, blocker, sent, received, target, label):
    state = await blocker.mom.query_federate_attributes(blocker.handle)
    assert state.found and state.time_regulating
    assert state.logical_time == 0 and state.lookahead == 0.5
    assert await sender.query_logical_time() == 0
    # A rejected second TAR proves the server still has an outstanding request.
    with pytest.raises(TimeAdvancingState):
        await sender.time_advance_request(target)
    await _barrier(sender, sent, received, label)
    assert not any(isinstance(event, TimeAdvanceGrant) for event in sent.events)


async def _finish_advance(sender, blocker, sent, received, target):
    await blocker.time_advance_request(target)
    for federate, callbacks in ((sender, sent), (blocker, received)):
        await callbacks.wait(TimeAdvanceGrant, lambda event: event.time == target)
        assert await federate.query_logical_time() == target


@pytest.mark.parametrize("requested", [0.25, 4.0], ids=["decrease", "increase"])
def test_modify_lookahead_rejected_during_proven_pending_tar_without_mutation(
    live_server, requested,  # noqa: F811
):
    async def exercise():
        async with _participants(live_server, lookahead=2.0) as (
            sender, blocker, sent, received,
        ):
            await sender.time_advance_request(10.0)
            await _assert_pending(sender, blocker, sent, received, 10.0, "before-modify")
            with pytest.raises(TimeAdvancingState):
                await sender.modify_lookahead(requested)
            assert await sender.query_lookahead() == 2.0
            await _assert_pending(sender, blocker, sent, received, 10.0, "after-rejection")

            # Also exclude a rejected request silently changing the desired lookahead.
            await _finish_advance(sender, blocker, sent, received, 10.0)
            assert await sender.query_lookahead() == 2.0
            await sender.time_advance_request(12.0)
            await _finish_advance(sender, blocker, sent, received, 12.0)
            assert await sender.query_lookahead() == 2.0

    asyncio.run(exercise())


@pytest.mark.parametrize("pending", [False, True], ids=["idle", "pending-tar"])
@pytest.mark.parametrize("below_floor", [True, False], ids=["nextafter-below", "exact-floor"])
def test_positive_lookahead_send_floor_is_exact(
    live_server, pending, below_floor,  # noqa: F811
):
    async def exercise():
        async with _participants(live_server, lookahead=0.5) as (
            sender, blocker, sent, received,
        ):
            if pending:
                await sender.time_advance_request(8.0)
                await _assert_pending(sender, blocker, sent, received, 8.0, "pending-send")
            floor = 8.5 if pending else 0.5
            timestamp = math.nextafter(floor, -math.inf) if below_floor else floor
            tag, payload = b"exact-positive-floor", b"\x00\xffboundary"

            async def send():
                await sender.send_interaction(
                    BASE_SIGNAL, {"Payload": payload}, timestamp=timestamp,
                    retraction_handle=101, tag=tag,
                )

            if below_floor:
                assert 0 < floor - timestamp < 1e-9
                with pytest.raises(InvalidLogicalTime):
                    await send()
            else:
                await send()
                event = await received.wait(ReceiveInteraction, lambda event: event.tag == tag)
                assert event.timestamp == floor
                assert event.parameters == {"Payload": payload}
                assert event.metadata is not None
                assert event.metadata.sent_order == OrderType.TIMESTAMP
                assert event.metadata.received_order == OrderType.RECEIVE
                assert event.metadata.producing_federate == sender.handle
                assert event.metadata.message_retraction_handle == 101

            await _barrier(sender, sent, received, "after-send")
            deliveries = [
                event for event in received.events if isinstance(event, ReceiveInteraction)
            ]
            assert len(deliveries) == (0 if below_floor else 1)
            assert await sender.query_lookahead() == 0.5
            assert await sender.query_logical_time() == 0
            if pending:
                await _assert_pending(sender, blocker, sent, received, 8.0, "send-still-pending")
                await _finish_advance(sender, blocker, sent, received, 8.0)

    asyncio.run(exercise())


def test_lookahead_decrease_tracks_granted_time_and_stops_at_requested_value(
    live_server,  # noqa: F811
):
    async def exercise():
        async with _participants(live_server, lookahead=4.0) as (
            sender, blocker, sent, received,
        ):
            await sender.modify_lookahead(1.0)
            assert await sender.query_lookahead() == 4.0, (
                "a decrease must initially retain the actual lookahead"
            )
            # 8.19: actual decreases by the granted time delta, stopping at requested.
            for target, expected in (
                (0.0, 4.0), (1.0, 3.0), (2.0, 2.0), (4.0, 1.0), (6.0, 1.0),
            ):
                await sender.time_advance_request(target)
                await _finish_advance(sender, blocker, sent, received, target)
                assert await sender.query_lookahead() == expected
                floor = target + expected
                with pytest.raises(InvalidLogicalTime):
                    await sender.send_interaction(
                        BASE_SIGNAL, {"Payload": b"below-actual"}, timestamp=floor - 0.5,
                    )
                tag = f"actual-floor-{target}".encode()
                await sender.send_interaction(
                    BASE_SIGNAL, {"Payload": b"actual-floor"}, timestamp=floor, tag=tag,
                )
                event = await received.wait(
                    ReceiveInteraction, lambda event, tag=tag: event.tag == tag
                )
                assert event.timestamp == floor
            assert [
                event.parameters for event in received.events
                if isinstance(event, ReceiveInteraction)
            ] == [{"Payload": b"actual-floor"}] * 5

    asyncio.run(exercise())


@pytest.mark.parametrize(
    ("target", "floor", "granted_lookahead"),
    [(2.0, 5.0, 3.0), (4.0, 6.0, 2.0)],
    ids=["partial-decrease", "requested-value-reached"],
)
def test_pending_tar_send_floor_projects_gradual_lookahead_decrease(
    live_server, target, floor, granted_lookahead,  # noqa: F811
):
    async def exercise():
        async with _participants(live_server, lookahead=5.0) as (
            sender, blocker, sent, received,
        ):
            await sender.modify_lookahead(2.0)
            assert await sender.query_lookahead() == 5.0
            await sender.time_advance_request(target)
            await _assert_pending(sender, blocker, sent, received, target, "projected-pending")
            assert await sender.query_lookahead() == 5.0

            # 8.19's reduction at requested time R combines with 8.1.7's pending
            # send base: R + max(requested lookahead, actual - (R - current)).
            tag = b"projected-lookahead-floor"
            await sender.send_interaction(
                BASE_SIGNAL, {"Payload": b"projected-floor"}, timestamp=floor, tag=tag,
            )
            event = await received.wait(ReceiveInteraction, lambda event: event.tag == tag)
            assert event.timestamp == floor
            with pytest.raises(InvalidLogicalTime):
                await sender.send_interaction(
                    BASE_SIGNAL, {"Payload": b"below-projected-floor"},
                    timestamp=math.nextafter(floor, -math.inf),
                )
            await _barrier(sender, sent, received, "projected-boundary-checked")
            assert [
                event.parameters for event in received.events
                if isinstance(event, ReceiveInteraction)
            ] == [{"Payload": b"projected-floor"}]
            await _assert_pending(
                sender, blocker, sent, received, target, "projection-still-pending"
            )
            await _finish_advance(sender, blocker, sent, received, target)
            assert await sender.query_lookahead() == granted_lookahead

    asyncio.run(exercise())


@pytest.mark.parametrize("requested", [4.0, 6.0], ids=["unchanged", "increase"])
def test_lookahead_at_least_actual_takes_effect_immediately(
    live_server, requested,  # noqa: F811
):
    async def exercise():
        async with _participants(live_server, lookahead=4.0) as (sender, _, _, _):
            await sender.modify_lookahead(requested)
            assert await sender.query_lookahead() == requested
            assert await sender.query_logical_time() == 0

    asyncio.run(exercise())
