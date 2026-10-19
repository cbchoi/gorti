"""Source-derived zero-lookahead boundaries over the production Python API.

IEEE 1516.1-2010 current text: 8.1.3-8.1.4 (supplied PDF pp184-185),
8.1.6 Table 3 and footnote 8 (pp186-188), and 8.1.7 (p189). The reviewed
pages are in ieee2010-source-intake-20260921 next to the repository.
TAR/NMR with zero actual lookahead exclude equality, both pending and
granted; Available includes equality. A same-time Available grant does
not lift the strict restriction of an earlier TAR/NMR grant.

Reuse the time-followup two-connection setup and existing live fixtures,
not the opt-in marker of test_live_service_e2e. These tests run by default.
Connections share one Python interpreter; only the RTI is a child process.
GORTI_LIVE_ARTIFACTS and GORTI_LIVE_RTID select external evidence/binaries.
"""

from __future__ import annotations

import asyncio
import math

import pytest

from rti1516e.errors import InvalidLogicalTime
from rti1516e.events import OrderType, ReceiveInteraction, TimeAdvanceGrant
from tests.spec.m21.test_ieee2010_time_followup import (
    _assert_pending,
    _barrier,
    _participants,
)
from tests.test_live_service_e2e import (
    BASE_SIGNAL,
    live_artifacts,  # noqa: F401
    live_server,  # noqa: F401
)

pytestmark = [pytest.mark.spec, pytest.mark.integration]

ADVANCES = [
    pytest.param("time_advance_request", True, id="TAR"),
    pytest.param("next_message_request", True, id="NMR"),
    pytest.param("time_advance_request_available", False, id="TARA"),
    pytest.param("next_message_request_available", False, id="NMRA"),
]
STRICT_ADVANCES = [
    pytest.param("time_advance_request", id="TAR"),
    pytest.param("next_message_request", id="NMR"),
]
AVAILABLE_ADVANCES = [
    pytest.param("time_advance_request_available", id="TARA"),
    pytest.param("next_message_request_available", id="NMRA"),
]


def _grant_history(callbacks):
    return [event.time for event in callbacks.events if isinstance(event, TimeAdvanceGrant)]


async def _grant(sender, sent, method, target):
    # A fresh callback index is essential when consecutive grants have equal time.
    mark = len(sent.events)
    previous = _grant_history(sent)
    await getattr(sender, method)(target)
    await sent.wait(TimeAdvanceGrant, lambda event: event.time == target, after=mark)
    assert await sender.query_logical_time() == target
    assert _grant_history(sent) == [*previous, target]


async def _send_boundary(sender, sent, received, floor, *, strict, label):
    mark = len(received.events)
    payload = b"\x00\xffzero-lookahead"
    accepted = []

    async def send(timestamp, suffix, *, allowed):
        tag = f"{label}-{suffix}".encode()

        async def invoke():
            await sender.send_interaction(
                BASE_SIGNAL, {"Payload": payload}, timestamp=timestamp, tag=tag,
            )

        if not allowed:
            with pytest.raises(InvalidLogicalTime):
                await invoke()
            return

        await invoke()
        event = await received.wait(ReceiveInteraction, lambda event: event.tag == tag, after=mark)
        assert event.timestamp == timestamp
        assert event.parameters == {"Payload": payload}
        assert event.metadata is not None
        assert event.metadata.sent_order == OrderType.TIMESTAMP
        assert event.metadata.received_order == OrderType.RECEIVE
        assert event.metadata.producing_federate == sender.handle
        accepted.append((tag, timestamp))

    # The immediately adjacent float proves that strict does not mean +epsilon.
    await send(math.nextafter(floor, math.inf), "above", allowed=True)
    if floor > 0:
        await send(math.nextafter(floor, -math.inf), "below", allowed=False)
    await send(floor, "equal", allowed=not strict)
    await _barrier(sender, sent, received, f"{label}-send-barrier")
    delivered = [
        (event.tag, event.timestamp)
        for event in received.events[mark:]
        if isinstance(event, ReceiveInteraction)
    ]
    # The receiver is unconstrained: rejected TSO sends cannot hide in its queue.
    # RO reception has no ordering guarantee, so compare contents, not arrival order.
    assert sorted(delivered) == sorted(accepted)


def test_zero_initial_regulation_allows_equality(live_server):  # noqa: F811
    async def exercise():
        async with _participants(live_server, lookahead=0.0) as (
            sender, _, sent, received,
        ):
            # Table 3 footnote 8: newly enabled regulation is Available-like.
            await _send_boundary(sender, sent, received, 0.0, strict=False, label="initial")
            assert await sender.query_logical_time() == 0.0
            assert await sender.query_lookahead() == 0.0
            assert _grant_history(sent) == []

    asyncio.run(exercise())


@pytest.mark.parametrize(("method", "strict"), ADVANCES)
def test_zero_pending_send_boundary(live_server, method, strict):  # noqa: F811
    async def exercise():
        async with _participants(live_server, lookahead=0.0) as (
            sender, blocker, sent, received,
        ):
            await getattr(sender, method)(8.0)
            # Peer at zero with lookahead .5, unchanged sender time, duplicate
            # advance rejection, and callback barrier together establish pending.
            await _assert_pending(sender, blocker, sent, received, 8.0, "before-boundary")
            await _send_boundary(sender, sent, received, 8.0, strict=strict, label="pending")
            assert await sender.query_lookahead() == 0.0
            await _assert_pending(sender, blocker, sent, received, 8.0, "after-boundary")

            # Raising the peer's bound must release the original request once.
            await blocker.modify_lookahead(16.0)
            await sent.wait(TimeAdvanceGrant, lambda event: event.time == 8.0)
            await _barrier(sender, sent, received, "released")
            assert await sender.query_logical_time() == 8.0
            assert _grant_history(sent) == [8.0]

    asyncio.run(exercise())


@pytest.mark.parametrize(("method", "strict"), ADVANCES)
def test_zero_granted_send_boundary(live_server, method, strict):  # noqa: F811
    async def exercise():
        async with _participants(live_server, lookahead=0.0) as (
            sender, blocker, sent, received,
        ):
            await blocker.modify_lookahead(16.0)
            await _grant(sender, sent, method, 4.0)
            await _send_boundary(sender, sent, received, 4.0, strict=strict, label="granted")
            assert await sender.query_lookahead() == 0.0
            assert await sender.query_logical_time() == 4.0
            assert _grant_history(sent) == [4.0]

    asyncio.run(exercise())


@pytest.mark.parametrize("strict_method", STRICT_ADVANCES)
@pytest.mark.parametrize("available_method", AVAILABLE_ADVANCES)
def test_zero_same_time_available_preserves_strict_grant_history(
    live_server, strict_method, available_method,  # noqa: F811
):
    async def exercise():
        async with _participants(live_server, lookahead=0.0) as (
            sender, blocker, sent, received,
        ):
            await blocker.modify_lookahead(16.0)
            await _grant(sender, sent, strict_method, 4.0)
            await _grant(sender, sent, available_method, 4.0)
            assert _grant_history(sent) == [4.0, 4.0]
            # 8.1.4's explicit same-time example retains the earlier strict bound.
            await _send_boundary(sender, sent, received, 4.0, strict=True, label="same-time")

            await _grant(sender, sent, available_method, 6.0)
            await _send_boundary(sender, sent, received, 6.0, strict=False, label="forward")
            assert await sender.query_lookahead() == 0.0
            assert _grant_history(sent) == [4.0, 4.0, 6.0]

    asyncio.run(exercise())


@pytest.mark.parametrize(("method", "zero_strict"), ADVANCES)
@pytest.mark.parametrize("pending", [True, False], ids=["pending", "granted"])
def test_positive_lookahead_keeps_inclusive_send_floor(
    live_server, method, zero_strict, pending,  # noqa: F811
):
    async def exercise():
        async with _participants(live_server, lookahead=0.5) as (
            sender, blocker, sent, received,
        ):
            if pending:
                await getattr(sender, method)(8.0)
                await _assert_pending(sender, blocker, sent, received, 8.0, "positive-pending")
            else:
                await blocker.modify_lookahead(16.0)
                await _grant(sender, sent, method, 8.0)

            # Positive actual lookahead admits equality for all four services.
            await _send_boundary(sender, sent, received, 8.5, strict=False, label="positive")
            assert await sender.query_lookahead() == 0.5
            if pending:
                await _assert_pending(sender, blocker, sent, received, 8.0, "positive-checked")
                await blocker.modify_lookahead(16.0)
                await sent.wait(TimeAdvanceGrant, lambda event: event.time == 8.0)
            await _barrier(sender, sent, received, "positive-completed")
            assert await sender.query_logical_time() == 8.0
            assert _grant_history(sent) == [8.0]

    asyncio.run(exercise())
