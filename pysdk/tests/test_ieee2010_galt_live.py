"""GALT queries with genuinely pending peer requests on a production RTI.

IEEE 1516.1-2010: 8.1.5, 8.1.6, 8.8-8.11, 8.16 and 8.18.
The expected bounds come from the request and queued message timestamps,
not from agreement between GALT, LITS and MOM implementations.
"""

from __future__ import annotations

import asyncio
import os
from contextlib import AsyncExitStack, asynccontextmanager

import pytest

from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.events import (
    OrderType,
    ReceiveInteraction,
    SynchronizationPointAnnounced,
    TimeAdvanceGrant,
)

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
async def _pending_peer(server, mode, queued):
    url, base, _ = server
    async with asyncio.timeout(45), AsyncExitStack() as stack:
        left = await stack.enter_async_context(RtiConnection.connect(url))
        right = await stack.enter_async_context(RtiConnection.connect(url))
        spec = FederationSpec("ieee2010-galt-live", [str(base)])
        caller = await stack.enter_async_context(
            left.join_federation(spec, federate_name="caller")
        )
        peer = await stack.enter_async_context(right.join_federation(spec, federate_name="peer"))
        callbacks = LiveCallbacks(peer)
        stack.push_async_callback(callbacks.close)
        assert left.transport is not right.transport
        await caller.enable_time_regulation(0)
        await peer.enable_time_regulation(2)
        await peer.enable_time_constrained()
        assert await caller.query_lookahead() == 0
        assert await peer.query_lookahead() == 2
        await caller.publish_interaction_class(BASE_SIGNAL)
        await peer.subscribe_interaction_class(BASE_SIGNAL)
        signal = await caller.support.get_interaction_class_handle(BASE_SIGNAL)
        await caller.change_interaction_order_type(signal, OrderType.TIMESTAMP)
        if queued:
            await caller.send_interaction(
                BASE_SIGNAL, {"Payload": b"early"}, timestamp=4, retraction_handle=42
            )
        await getattr(peer, mode)(10)
        # A control callback proves stream progress without enabling RO delivery.
        await caller.sync.register_synchronization_point("pending-request")
        await callbacks.wait(
            SynchronizationPointAnnounced, lambda event: event.label == "pending-request"
        )
        assert await peer.query_logical_time() == 0
        assert not any(isinstance(event, TimeAdvanceGrant) for event in callbacks.events)
        assert not any(isinstance(event, ReceiveInteraction) for event in callbacks.events)
        yield caller, peer, callbacks


@pytest.mark.parametrize(
    "mode",
    [
        "time_advance_request",
        "time_advance_request_available",
        "next_message_request",
        "next_message_request_available",
    ],
    ids=["TAR", "TARA", "NMR", "NMRA"],
)
@pytest.mark.parametrize("queued", [False, True], ids=["empty", "queued"])
def test_galt_pending_peer_uses_available_not_current_horizon(
    live_server, mode, queued  # noqa: F811
):
    async def exercise():
        async with _pending_peer(live_server, mode, queued) as (caller, peer, callbacks):
            early = queued and mode.startswith("next_message")
            expected = 6 if early else 12
            # Caller is itself a regulator at zero; it must be excluded.
            assert await caller.query_galt() == (expected, True)
            assert await caller.query_lits() == (expected, True)
            assert await peer.query_galt() == (0, True)
            await caller.modify_lookahead(20)
            granted = 4 if early else 10
            tag = await callbacks.wait(TimeAdvanceGrant, lambda event: event.time == granted)
            if queued:
                message = await callbacks.wait(
                    ReceiveInteraction, lambda event: event.timestamp == 4
                )
                assert callbacks.events.index(message) < callbacks.events.index(tag)
            assert await peer.query_logical_time() == granted
            assert await caller.query_galt() == (expected, True)

    asyncio.run(exercise())


@pytest.mark.parametrize(
    "mode", ["next_message_request", "next_message_request_available"], ids=["NMR", "NMRA"]
)
def test_galt_retraction_removes_early_grant_cap(live_server, mode):  # noqa: F811
    async def exercise():
        async with _pending_peer(live_server, mode, True) as (caller, peer, callbacks):
            assert await caller.query_galt() == (6, True)
            await caller.retract(42)
            assert await caller.query_galt() == (12, True)
            assert await caller.query_lits() == (12, True)
            await caller.modify_lookahead(20)
            await callbacks.wait(TimeAdvanceGrant, lambda event: event.time == 10)
            assert not any(isinstance(event, ReceiveInteraction) for event in callbacks.events)
            assert await peer.query_logical_time() == 10

    asyncio.run(exercise())


@pytest.mark.parametrize(
    "mode", ["time_advance_request", "time_advance_request_available"], ids=["TAR", "TARA"]
)
def test_galt_bound_prevents_tar_overtaking_early_peer(live_server, mode):  # noqa: F811
    async def exercise():
        async with _pending_peer(live_server, "next_message_request", True) as (
            caller, peer, peer_callbacks
        ):
            own_callbacks = LiveCallbacks(caller)
            try:
                await caller.enable_time_constrained()
                assert await caller.query_galt() == (6, True)
                await getattr(caller, mode)(9)
                await peer_callbacks.wait(TimeAdvanceGrant, lambda event: event.time == 4)
                await peer.sync.register_synchronization_point("early-peer-granted")
                await own_callbacks.wait(
                    SynchronizationPointAnnounced,
                    lambda event: event.label == "early-peer-granted",
                )
                assert await caller.query_logical_time() == 0
                assert not any(
                    isinstance(event, TimeAdvanceGrant) for event in own_callbacks.events
                )
                assert await caller.query_galt() == (6, True)
                await peer.modify_lookahead(20)
                await own_callbacks.wait(TimeAdvanceGrant, lambda event: event.time == 9)
                assert await caller.query_logical_time() == 9
            finally:
                await own_callbacks.close()

    asyncio.run(exercise())
