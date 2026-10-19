"""RO eligibility and saved pending messages through production Python connections.

Source: active IEEE 1516.1-2010 8.1.6-8.1.8, 8.14-8.15, supplied
redline PDF pp. 186-189, 191 and 209-210. RO messages wait while a constrained
federate is granted with asynchronous delivery disabled; control callbacks
are not RO messages. All five advance modes deliver queued RO before TAG.

The two federates use separate connections in one Python interpreter.
Save/restore uses the public federation services and stable federate names
within the same server process; recipient rejoin is not a server restart.
No test-only server API or implementation-specific bundle reader is used.
"""

from __future__ import annotations

import asyncio
import xml.etree.ElementTree as ET
from contextlib import AsyncExitStack, asynccontextmanager
from copy import deepcopy

import pytest

from rti1516e._grpc_errors import TimeAdvancingState
from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.events import (
    DiscoverObjectInstance,
    FederationRestored,
    FederationSaved,
    InitiateFederateRestore,
    InitiateFederateSave,
    OrderType,
    ReceiveInteraction,
    ReflectAttributeValues,
    RemoveObjectInstance,
    SynchronizationPointAnnounced,
    TimeAdvanceGrant,
)
from tests.test_live_service_e2e import (
    BASE_SIGNAL,
    VEHICLE,
    LiveCallbacks,
    live_artifacts,  # noqa: F401
    live_server,  # noqa: F401
)

pytestmark = [pytest.mark.spec, pytest.mark.integration]

_KINDS = {
    "reflect": ReflectAttributeValues,
    "interaction": ReceiveInteraction,
    "remove": RemoveObjectInstance,
}
_MESSAGE_TYPES = tuple(_KINDS.values())
_ADVANCES = (
    "time_advance_request",
    "time_advance_request_available",
    "next_message_request",
    "next_message_request_available",
    "flush_queue_request",
)


@asynccontextmanager
async def _participants(server, *, regulating_sender=False, attributes=("Value",)):
    url, base, _ = server
    async with asyncio.timeout(70), AsyncExitStack() as stack:
        left = await stack.enter_async_context(RtiConnection.connect(url))
        right = await stack.enter_async_context(RtiConnection.connect(url))
        spec = FederationSpec("ieee2010-ro-delivery", [str(base)])
        sender = await stack.enter_async_context(
            left.join_federation(spec, federate_name="sender", federate_type="ro-live")
        )
        reader_context = right.join_federation(
            spec, federate_name="reader", federate_type="ro-live"
        )
        reader = await stack.enter_async_context(reader_context)
        callbacks = [LiveCallbacks(sender), LiveCallbacks(reader)]
        for log in callbacks:
            stack.push_async_callback(log.close)
        assert left.transport is not right.transport
        await sender.publish_object_class(VEHICLE, attributes=list(attributes))
        await reader.subscribe_object_class(VEHICLE, attributes=list(attributes))
        await sender.publish_interaction_class(BASE_SIGNAL)
        await reader.subscribe_interaction_class(BASE_SIGNAL)
        await reader.enable_time_constrained()
        if regulating_sender:
            await sender.enable_time_regulation(0.5)
        cls = await sender.support.get_object_class_handle(VEHICLE)
        attr = await sender.support.get_attribute_handle(cls, "Value")
        # Discovery must remain deliverable while the constrained reader is idle.
        obj = await sender.register_object_instance(VEHICLE)
        await callbacks[1].wait(DiscoverObjectInstance, lambda event: event.object_handle == obj)
        # Set the preferred order through services, independently of server profiles.
        await sender.change_attribute_order_type(obj, [attr], OrderType.RECEIVE)
        signal = await sender.support.get_interaction_class_handle(BASE_SIGNAL)
        await sender.change_interaction_order_type(signal, OrderType.RECEIVE)
        await _barrier(sender, callbacks, "participants-ready")
        assert await reader.query_logical_time() == 0
        state = await reader.mom.query_federate_attributes(reader.handle)
        assert state.found and state.time_constrained
        yield sender, reader, callbacks, obj, attr, stack, right, spec, reader_context


async def _barrier(sender, callbacks, label):
    # The same callback stream fences admitted messages without wall-clock sleeps.
    await sender.sync.register_synchronization_point(label)
    for log in callbacks:
        await log.wait(SynchronizationPointAnnounced, lambda event: event.label == label)
        assert log.failure is None


def _messages(log, *, after=0):
    return [event for event in log.events[after:] if isinstance(event, _MESSAGE_TYPES)]


async def _send(sender, log, obj, kind, tag, *, timestamp=None):
    if kind == "reflect":
        await sender.update_attributes(
            obj, {"Value": b"\x00\xffvalue:" + tag}, timestamp=timestamp, tag=tag,
        )
    elif kind == "interaction":
        await sender.send_interaction(
            BASE_SIGNAL, {"Payload": b"\x00\xffpayload:" + tag}, timestamp=timestamp, tag=tag,
        )
    else:
        assert kind == "remove" and timestamp is None
        obj = await sender.register_object_instance(VEHICLE)
        await log.wait(DiscoverObjectInstance, lambda event: event.object_handle == obj)
        await sender.delete_object_instance(obj, tag=tag)
    return kind, obj, tag, timestamp


async def _send_all(sender, log, obj, prefix):
    return [
        await _send(sender, log, obj, kind, prefix + b":" + kind.encode())
        for kind in _KINDS
    ]


async def _expect(log, expected, sender_handle, attr, *, after=0):
    for kind, obj, tag, timestamp in expected:
        event = await log.wait(_KINDS[kind], lambda event, tag=tag: event.tag == tag, after=after)
        assert event.timestamp == timestamp
        assert event.metadata is not None
        assert event.metadata.sent_order == OrderType.RECEIVE
        assert event.metadata.received_order == OrderType.RECEIVE
        assert event.metadata.producing_federate == sender_handle
        if kind == "reflect":
            assert event.object_handle == obj
            assert event.attribute_values == {attr: b"\x00\xffvalue:" + tag}
        elif kind == "interaction":
            assert event.parameters == {"Payload": b"\x00\xffpayload:" + tag}
        else:
            assert event.object_handle == obj
    # Do not impose a total order on distinct receive-order messages.
    assert sorted(event.tag for event in _messages(log, after=after)) == sorted(
        tag for _, _, tag, _ in expected
    )


@pytest.mark.parametrize("kind", tuple(_KINDS))
def test_ro_async_enable_drains_once_and_disable_holds_new_messages(
    live_server, kind,  # noqa: F811
):
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attr, *_):
            first = await _send(sender, logs[1], obj, kind, b"first")
            await _barrier(sender, logs, "first-held")
            assert _messages(logs[1]) == [], "idle constrained reader must hold RO"

            await reader.enable_asynchronous_delivery()
            await _expect(logs[1], [first], sender.handle, attr)
            await _barrier(sender, logs, "first-drained")
            assert len(_messages(logs[1])) == 1
            assert await reader.query_logical_time() == 0

            await reader.disable_asynchronous_delivery()
            second = await _send(sender, logs[1], obj, kind, b"second")
            await _barrier(sender, logs, "second-held")
            assert [event.tag for event in _messages(logs[1])] == [b"first"]
            await reader.enable_asynchronous_delivery()
            await _expect(logs[1], [first, second], sender.handle, attr)
            await _barrier(sender, logs, "second-drained")
            assert len(_messages(logs[1])) == 2
            assert await reader.query_logical_time() == 0

    asyncio.run(exercise())


async def _release_ro(reader, release):
    if release == "async":
        await reader.enable_asynchronous_delivery()
    else:
        assert release == "tar"
        await reader.time_advance_request(5.0)


def _two_attribute_server(server):
    url, base, extra = server
    namespace = {"h": "http://standards.ieee.org/IEEE1516-2010"}
    # The input is the local XML generated by the existing LIVE fixture.
    tree = ET.parse(base)  # noqa: S314
    vehicle = tree.find(".//h:objectClass[h:name='Vehicle']", namespace)
    assert vehicle is not None
    value = vehicle.find("h:attribute", namespace)
    assert value is not None
    other = deepcopy(value)
    name = other.find("h:name", namespace)
    assert name is not None
    name.text = "Other"
    vehicle.append(other)
    expanded = base.parent / "ro-two-attributes.xml"
    tree.write(expanded, encoding="utf-8", xml_declaration=True)
    return url, expanded, extra


@pytest.mark.parametrize("release", ["async", "tar"])
def test_ro_release_discards_interaction_after_unsubscribe(live_server, release):  # noqa: F811
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attr, *_):
            await _send(sender, logs[1], obj, "interaction", b"unsubscribed")
            await _barrier(sender, logs, "interaction-held-before-unsubscribe")
            assert _messages(logs[1]) == []
            await reader.unsubscribe_interaction_class(BASE_SIGNAL)
            await _release_ro(reader, release)
            if release == "tar":
                await logs[1].wait(TimeAdvanceGrant, lambda event: event.time == 5.0)
            await _barrier(sender, logs, "unsubscribed-interaction-released")
            assert _messages(logs[1]) == []

            # A later subscription must not revive a discarded queued message.
            await reader.subscribe_interaction_class(BASE_SIGNAL)
            expected = await _send(sender, logs[1], obj, "interaction", b"resubscribed")
            if release == "tar":
                await reader.time_advance_request(6.0)
                await logs[1].wait(TimeAdvanceGrant, lambda event: event.time == 6.0)
            await _expect(logs[1], [expected], sender.handle, attr)
            await _barrier(sender, logs, "resubscribed-without-old-message")
            assert len(_messages(logs[1])) == 1

    asyncio.run(exercise())


@pytest.mark.parametrize("release", ["async", "tar"])
def test_ro_release_trims_reflection_to_current_attribute_subscription(
    live_server, release,  # noqa: F811
):
    async def exercise():
        async with _participants(
            _two_attribute_server(live_server), attributes=("Value", "Other"),
        ) as (sender, reader, logs, obj, attr, *_):
            tag = b"partial-subscription"
            await sender.update_attributes(
                obj, {"Value": b"\x00\xffvalue:" + tag, "Other": b"must-be-filtered"}, tag=tag,
            )
            await _barrier(sender, logs, "reflection-held-before-partial-unsubscribe")
            assert _messages(logs[1]) == []
            await reader.unsubscribe_object_class(VEHICLE, attributes=["Other"])
            await _release_ro(reader, release)
            if release == "tar":
                await logs[1].wait(TimeAdvanceGrant, lambda event: event.time == 5.0)
            await _expect(logs[1], [("reflect", obj, tag, None)], sender.handle, attr)
            await _barrier(sender, logs, "partial-reflection-released")
            assert len(_messages(logs[1])) == 1

    asyncio.run(exercise())


@pytest.mark.parametrize("release", ["async", "tar"])
def test_ro_release_retains_removal_for_known_object_after_unsubscribe(
    live_server, release,  # noqa: F811
):
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attr, *_):
            expected = await _send(sender, logs[1], obj, "remove", b"known-removed")
            await _barrier(sender, logs, "removal-held-before-unsubscribe")
            assert _messages(logs[1]) == []
            await reader.unsubscribe_object_class(VEHICLE)
            await _release_ro(reader, release)
            if release == "tar":
                await logs[1].wait(TimeAdvanceGrant, lambda event: event.time == 5.0)
            await _expect(logs[1], [expected], sender.handle, attr)
            await _barrier(sender, logs, "known-removal-released")
            assert len(_messages(logs[1])) == 1

    asyncio.run(exercise())


def test_ro_mom_queue_length_counts_domain_pending_messages(live_server):  # noqa: F811
    async def queue_length(reader):
        state = await reader.mom.query_federate_attributes(reader.handle)
        assert state.found
        assert "HLAROlength" not in state.unavailable_attributes
        encoded = state.attribute_values["HLAROlength"]
        assert len(encoded) == 4
        return int.from_bytes(encoded, byteorder="big", signed=True)

    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attr, *_):
            assert await queue_length(reader) == 0
            expected = await _send_all(sender, logs[1], obj, b"mom-pending")
            await _barrier(sender, logs, "mom-control-callbacks-consumed")
            assert _messages(logs[1]) == []
            assert await queue_length(reader) == 3
            await reader.enable_asynchronous_delivery()
            await _expect(logs[1], expected, sender.handle, attr)
            await _barrier(sender, logs, "mom-domain-queue-drained")
            assert await queue_length(reader) == 3
            messages = _messages(logs[1])
            entered = []
            for event in messages:
                await reader.dispatch_event(event, entered.append)
            assert len(entered) == 3
            assert all(left is right for left, right in zip(entered, messages, strict=True))
            assert await queue_length(reader) == 0

    asyncio.run(exercise())


def test_ro_resign_delete_holds_removal_and_preserves_departed_producer(live_server):  # noqa: F811
    async def exercise():
        url, base, _ = live_server
        async with asyncio.timeout(45), AsyncExitStack() as stack:
            publisher_connection = await stack.enter_async_context(RtiConnection.connect(url))
            reader_connection = await stack.enter_async_context(RtiConnection.connect(url))
            assert publisher_connection.transport is not reader_connection.transport
            spec = FederationSpec("ieee2010-ro-resign", [str(base)])
            reader = await stack.enter_async_context(
                reader_connection.join_federation(spec, federate_name="reader")
            )
            log = LiveCallbacks(reader)
            stack.push_async_callback(log.close)
            await reader.subscribe_object_class(VEHICLE, attributes=["Value"])
            await reader.enable_time_constrained()
            original_class = await reader.support.get_object_class_handle(VEHICLE)

            publisher_context = publisher_connection.join_federation(
                spec, federate_name="departing-publisher",
            )
            publisher_context.resign_action = "DELETE_OBJECTS"
            # Lexical context exit performs exactly one public resign operation.
            async with publisher_context as publisher:
                departed_handle = publisher.handle
                await publisher.publish_object_class(VEHICLE, attributes=["Value"])
                original_object = await publisher.register_object_instance(VEHICLE)
                discovery = await log.wait(
                    DiscoverObjectInstance,
                    lambda event: event.object_handle == original_object,
                )
                assert discovery.object_class == original_class
                assert discovery.producing_federate == departed_handle
                assert await reader.support.get_known_object_class_handle(
                    original_object
                ) == original_class

            await _barrier(reader, [log], "after-publisher-resign")
            assert _messages(log) == [], "resign-generated RO removal must remain queued"
            roster = await reader.mom.query_federation_attributes()
            assert departed_handle not in roster.federate_handles
            assert reader.handle in roster.federate_handles

            # A new producer can still trigger discovery while the old removal waits.
            async with publisher_connection.join_federation(
                spec, federate_name="replacement-publisher",
            ) as replacement:
                assert replacement.handle != departed_handle
                await replacement.publish_object_class(VEHICLE, attributes=["Value"])
                replacement_object = await replacement.register_object_instance(VEHICLE)
                assert replacement_object != original_object
                replacement_discovery = await log.wait(
                    DiscoverObjectInstance,
                    lambda event: event.object_handle == replacement_object,
                )
                assert replacement_discovery.object_class == original_class
                assert replacement_discovery.producing_federate == replacement.handle
                await _barrier(reader, [log], "discovery-while-resign-removal-held")
                assert _messages(log) == []

                await reader.enable_asynchronous_delivery()
                removal = await log.wait(
                    RemoveObjectInstance, lambda event: event.object_handle == original_object,
                )
                assert removal.timestamp is None
                assert removal.metadata is not None
                assert removal.metadata.sent_order == OrderType.RECEIVE
                assert removal.metadata.received_order == OrderType.RECEIVE
                assert removal.metadata.producing_federate == departed_handle
                await _barrier(reader, [log], "resign-removal-released")
                assert _messages(log) == [removal]
                await reader.disable_asynchronous_delivery()
                await reader.enable_asynchronous_delivery()
                await _barrier(reader, [log], "resign-removal-not-repeated")
                assert _messages(log) == [removal]
                assert await reader.query_logical_time() == 0

    asyncio.run(exercise())


@pytest.mark.parametrize("advance", _ADVANCES)
def test_ro_all_advance_modes_deliver_queued_messages_before_grant(
    live_server, advance,  # noqa: F811
):
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attr, *_):
            expected = await _send_all(sender, logs[1], obj, b"before-grant")
            await _barrier(sender, logs, "advance-held")
            assert _messages(logs[1]) == []
            await getattr(reader, advance)(5.0)
            grant = await logs[1].wait(TimeAdvanceGrant, lambda event: event.time == 5.0)
            await _expect(logs[1], expected, sender.handle, attr)
            grant_index = logs[1].events.index(grant)
            assert all(logs[1].events.index(event) < grant_index for event in _messages(logs[1]))
            assert await reader.query_logical_time() == 5.0
            await _barrier(sender, logs, "after-grant")
            assert len(_messages(logs[1])) == 3

    asyncio.run(exercise())


def test_ro_blocked_tar_releases_existing_and_new_messages_before_grant(live_server):  # noqa: F811
    async def exercise():
        async with _participants(live_server, regulating_sender=True) as (
            sender, reader, logs, obj, attr, *_,
        ):
            expected = await _send_all(sender, logs[1], obj, b"before-blocked-tar")
            await _barrier(sender, logs, "tar-initial-hold")
            assert _messages(logs[1]) == []
            await reader.time_advance_request(10.0)
            await _expect(logs[1], expected, sender.handle, attr)
            with pytest.raises(TimeAdvancingState):
                await reader.time_advance_request(10.0)
            assert await reader.query_logical_time() == 0
            assert not any(isinstance(event, TimeAdvanceGrant) for event in logs[1].events)

            expected += await _send_all(sender, logs[1], obj, b"during-blocked-tar")
            await _expect(logs[1], expected, sender.handle, attr)
            await _barrier(sender, logs, "tar-still-blocked")
            assert not any(isinstance(event, TimeAdvanceGrant) for event in logs[1].events)
            await sender.time_advance_request(10.0)
            grant = await logs[1].wait(TimeAdvanceGrant, lambda event: event.time == 10.0)
            grant_index = logs[1].events.index(grant)
            assert all(logs[1].events.index(event) < grant_index for event in _messages(logs[1]))
            await _barrier(sender, logs, "tar-released")
            assert len(_messages(logs[1])) == 6

    asyncio.run(exercise())


def test_ro_disable_constraint_drains_without_time_advance(live_server):  # noqa: F811
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attr, *_):
            expected = await _send_all(sender, logs[1], obj, b"before-disable")
            await _barrier(sender, logs, "constraint-held")
            assert _messages(logs[1]) == []
            await reader.disable_time_constrained()
            await _expect(logs[1], expected, sender.handle, attr)
            expected += await _send_all(sender, logs[1], obj, b"unconstrained")
            await _expect(logs[1], expected, sender.handle, attr)
            await _barrier(sender, logs, "constraint-drained")
            assert len(_messages(logs[1])) == 6
            assert await reader.query_logical_time() == 0
            assert not any(isinstance(event, TimeAdvanceGrant) for event in logs[1].events)

    asyncio.run(exercise())


@pytest.mark.parametrize("kind", ["reflect", "interaction"])
def test_ro_preferred_receive_order_preserves_supplied_timestamp(live_server, kind):  # noqa: F811
    async def exercise():
        async with _participants(live_server, regulating_sender=True) as (
            sender, reader, logs, obj, attr, *_,
        ):
            # Explicit Receive preference also applies to this timestamped call.
            expected = await _send(sender, logs[1], obj, kind, b"timed-ro", timestamp=3.25)
            await _barrier(sender, logs, "timestamped-ro-held")
            assert _messages(logs[1]) == []
            await reader.enable_asynchronous_delivery()
            await _expect(logs[1], [expected], sender.handle, attr)
            await _barrier(sender, logs, "timestamped-ro-drained")
            assert len(_messages(logs[1])) == 1
            assert await reader.query_logical_time() == 0

    asyncio.run(exercise())


def test_ro_async_release_does_not_release_future_tso(live_server):  # noqa: F811
    async def exercise():
        async with _participants(live_server, regulating_sender=True) as (
            sender, reader, logs, obj, attr, *_,
        ):
            ro = await _send(sender, logs[1], obj, "interaction", b"receive-order")
            signal = await sender.support.get_interaction_class_handle(BASE_SIGNAL)
            await sender.change_interaction_order_type(signal, OrderType.TIMESTAMP)
            await sender.send_interaction(
                BASE_SIGNAL, {"Payload": b"future-tso"}, timestamp=3.25, tag=b"timestamp-order",
            )
            await _barrier(sender, logs, "mixed-queues-held")
            assert _messages(logs[1]) == []
            await reader.enable_asynchronous_delivery()
            await _expect(logs[1], [ro], sender.handle, attr)
            await _barrier(sender, logs, "only-ro-released")
            assert len(_messages(logs[1])) == 1
            await reader.time_advance_request(5.0)
            with pytest.raises(TimeAdvancingState):
                await reader.time_advance_request(5.0)
            await _barrier(sender, logs, "mixed-tar-blocked")
            assert len(_messages(logs[1])) == 1
            await sender.time_advance_request(5.0)
            tso = await logs[1].wait(
                ReceiveInteraction, lambda event: event.tag == b"timestamp-order",
            )
            grant = await logs[1].wait(TimeAdvanceGrant, lambda event: event.time == 5.0)
            assert tso.parameters == {"Payload": b"future-tso"}
            assert tso.timestamp == 3.25
            assert tso.metadata is not None
            assert tso.metadata.sent_order == OrderType.TIMESTAMP
            assert tso.metadata.received_order == OrderType.TIMESTAMP
            assert tso.metadata.producing_federate == sender.handle
            assert logs[1].events.index(tso) < logs[1].events.index(grant)
            await _barrier(sender, logs, "mixed-queues-drained")
            assert len(_messages(logs[1])) == 2

    asyncio.run(exercise())


async def _save(federates, logs, label):
    marks = [len(log.events) for log in logs]
    await federates[0].savepoint.request_federation_save(label)
    for log, mark in zip(logs, marks, strict=True):
        await log.wait(InitiateFederateSave, lambda event: event.label == label, after=mark)
    for federate in federates:
        await federate.savepoint.federate_save_begun()
        await federate.savepoint.federate_save_complete()
    for log, mark in zip(logs, marks, strict=True):
        await log.wait(FederationSaved, lambda event: event.label == label, after=mark)


async def _restore(federates, logs, label, saved_reader_handle):
    marks = [len(log.events) for log in logs]
    await federates[0].savepoint.request_federation_restore(label)
    for log, mark in zip(logs, marks, strict=True):
        event = await log.wait(
            InitiateFederateRestore, lambda event: event.label == label, after=mark,
        )
        if log is logs[1]:
            assert event.federate_handle == saved_reader_handle
    for federate in federates:
        await federate.savepoint.federate_restore_complete()
    for log, mark in zip(logs, marks, strict=True):
        await log.wait(FederationRestored, lambda event: event.label == label, after=mark)
    assert federates[1].handle == saved_reader_handle


@pytest.mark.parametrize("rejoin", [False, True], ids=["same-connections", "reader-rejoined"])
def test_ro_save_restore_recovers_pending_messages_not_postsave_state(
    live_server, rejoin,  # noqa: F811
):
    async def exercise():
        async with _participants(live_server, regulating_sender=True) as (
            sender, reader, logs, obj, attr, stack, connection, spec, reader_context,
        ):
            saved_reader_handle = reader.handle
            expected = await _send_all(sender, logs[1], obj, b"saved")
            for kind in ("reflect", "interaction"):
                expected.append(await _send(
                    sender, logs[1], obj, kind, b"saved-timed-" + kind.encode(), timestamp=3.25,
                ))
            await _barrier(sender, logs, "save-queue-held")
            assert _messages(logs[1]) == []
            await _save([sender, reader], logs, "pending-ro")
            assert _messages(logs[1]) == [], "save control callbacks must not release RO"

            # Distinguish restored pending state from the current queue and flags.
            await reader.enable_asynchronous_delivery()
            await _expect(logs[1], expected, sender.handle, attr)
            await reader.disable_asynchronous_delivery()
            await _send_all(sender, logs[1], obj, b"postsave-discarded")
            await _barrier(sender, logs, "postsave-held")
            assert len(_messages(logs[1])) == len(expected)
            if rejoin:
                await logs[1].close()
                await reader_context.__aexit__(None, None, None)
                reader = await stack.enter_async_context(
                    connection.join_federation(
                        spec, federate_name="reader", federate_type="ro-live",
                    )
                )
                assert reader.handle != saved_reader_handle
                logs[1] = LiveCallbacks(reader)
                stack.push_async_callback(logs[1].close)
            else:
                # Save recorded async OFF; restoring must undo this live ON state.
                await reader.enable_asynchronous_delivery()
                await _barrier(sender, logs, "postsave-live-async-on")
                assert len(_messages(logs[1])) == len(expected) + 3

            before_restore = len(logs[1].events)
            await _restore([sender, reader], logs, "pending-ro", saved_reader_handle)
            await _barrier(sender, logs, "restored-queue-held")
            assert _messages(logs[1], after=before_restore) == []
            assert await reader.query_logical_time() == 0
            await reader.enable_asynchronous_delivery()
            await _expect(logs[1], expected, sender.handle, attr, after=before_restore)
            await _barrier(sender, logs, "restored-queue-drained")
            await reader.disable_asynchronous_delivery()
            await reader.enable_asynchronous_delivery()
            await _barrier(sender, logs, "restored-queue-not-replayed")
            assert len(_messages(logs[1], after=before_restore)) == len(expected)

    asyncio.run(exercise())
