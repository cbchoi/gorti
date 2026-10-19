"""FOM orders, unengaged advances, and granted-state TSO on a real RTI.

Oracle: supplied active IEEE 1516.1-2010 JSONL, PDF pp. 182-189
(8.1.2 Tables 1/2, 8.1.3, 8.1.5-8.1.7), and pp. 196-208
(8.6 and 8.8-8.12), plus pp. 215-217 (8.21-8.22 retraction).
FOM order is independent of transport profiles;
only advancing constrained recipients may receive TSO, including equality.

The existing live fixture owns a production server with its default profile.
Two independent public Python connections share one interpreter. Control
callback fences, never sleep-based absence, delimit all delivery assertions.
Generated FOMs, server logs, and saves use external GORTI_LIVE_ARTIFACTS.
"""

from __future__ import annotations

import asyncio
import xml.etree.ElementTree as ET
from contextlib import AsyncExitStack, asynccontextmanager
from copy import deepcopy

import pytest

from rti1516e._grpc_errors import LogicalTimeAlreadyPassed
from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.errors import InvalidLogicalTime
from rti1516e.events import (
    DiscoverObjectInstance,
    OrderType,
    ReceiveInteraction,
    ReflectAttributeValues,
    RemoveObjectInstance,
    TimeAdvanceGrant,
)
from tests.spec.m21.test_ieee2010_ro_delivery_live import _barrier, _restore, _save
from tests.test_live_service_e2e import (
    BASE_SIGNAL,
    VEHICLE,
    LiveCallbacks,
    live_artifacts,  # noqa: F401
)
from tests.test_live_service_e2e import live_server as _live_server  # noqa: F401 - named fixture

pytestmark = [pytest.mark.spec, pytest.mark.integration]

_ADVANCES = [
    pytest.param("time_advance_request", id="TAR"),
    pytest.param("time_advance_request_available", id="TARA"),
    pytest.param("next_message_request", id="NMR"),
    pytest.param("next_message_request_available", id="NMRA"),
    pytest.param("flush_queue_request", id="FQR"),
]
_NEXT = [
    pytest.param("next_message_request", id="NMR"),
    pytest.param("next_message_request_available", id="NMRA"),
]
_KINDS = {
    "reflect": ReflectAttributeValues,
    "interaction": ReceiveInteraction,
    "remove": RemoveObjectInstance,
}
_MESSAGE_TYPES = tuple(_KINDS.values())
_PRIVILEGE = "HLAprivilegeToDeleteObject"
_TIMED_SIGNAL = "HLAinteractionRoot.TimedSignal"


def _mixed_fom(base, *, regional=False):
    ns = {"h": "http://standards.ieee.org/IEEE1516-2010"}
    tree = ET.parse(base)  # noqa: S314 - trusted local fixture XML
    vehicle = tree.find(".//h:objectClass[h:name='Vehicle']", ns)
    assert vehicle is not None
    value = vehicle.find("h:attribute", ns)
    assert value is not None
    other = deepcopy(value)
    other.find("h:name", ns).text = "Other"
    other.find("h:order", ns).text = "TimeStamp"
    vehicle.append(other)
    root = tree.find(".//h:interactionClass[h:name='HLAinteractionRoot']", ns)
    assert root is not None
    timed = deepcopy(root.find("h:interactionClass", ns))
    timed.find("h:name", ns).text = "TimedSignal"
    timed.find("h:order", ns).text = "TimeStamp"
    root.append(timed)
    if regional:
        namespace = "{" + ns["h"] + "}"
        signal = root.find("h:interactionClass[h:name='BaseSignal']", ns)
        dims = ET.SubElement(signal, namespace + "dimensions")
        ET.SubElement(dims, namespace + "dimension").text = "X"
        dim = ET.SubElement(
            ET.SubElement(tree.getroot(), namespace + "dimensions"),
            namespace + "dimension",
        )
        ET.SubElement(dim, namespace + "name").text = "X"
        ET.SubElement(dim, namespace + "upperBound").text = "100"
    output = base.parent / ("order-advance-regional.xml" if regional else "order-advance-mixed.xml")
    tree.write(output, encoding="utf-8", xml_declaration=True)
    return output


@asynccontextmanager
async def _participants(server, *, regional=False):
    url, base, _ = server
    async with asyncio.timeout(65), AsyncExitStack() as stack:
        left = await stack.enter_async_context(RtiConnection.connect(url))
        right = await stack.enter_async_context(RtiConnection.connect(url))
        assert left.transport is not right.transport
        spec = FederationSpec(
            "ieee2010-order-advance",
            [str(_mixed_fom(base, regional=regional))],
        )
        sender = await stack.enter_async_context(
            left.join_federation(spec, federate_name="sender", federate_type="order-advance")
        )
        reader = await stack.enter_async_context(
            right.join_federation(spec, federate_name="reader", federate_type="order-advance")
        )
        logs = [LiveCallbacks(sender), LiveCallbacks(reader)]
        for log in logs:
            stack.push_async_callback(log.close)
        await sender.publish_object_class(VEHICLE, attributes=["Value", "Other"])
        await reader.subscribe_object_class(VEHICLE, attributes=["Value", "Other"])
        for signal in (BASE_SIGNAL, _TIMED_SIGNAL):
            await sender.publish_interaction_class(signal)
            await reader.subscribe_interaction_class(signal)
        cls = await sender.support.get_object_class_handle(VEHICLE)
        attrs = {
            name: await sender.support.get_attribute_handle(cls, name)
            for name in ("Value", "Other", _PRIVILEGE)
        }
        obj = await sender.register_object_instance(VEHICLE)
        await logs[1].wait(DiscoverObjectInstance, lambda event: event.object_handle == obj)
        await _barrier(sender, logs, "participants-ready")
        yield sender, reader, logs, obj, attrs


def _messages(log, *, after=0):
    return [event for event in log.events[after:] if isinstance(event, _MESSAGE_TYPES)]


def _grants(log, *, after=0):
    return [event.time for event in log.events[after:] if isinstance(event, TimeAdvanceGrant)]


async def _grant(federate, log, method, target, *, expected=None):
    mark = len(log.events)
    await getattr(federate, method)(target)
    grant = await log.wait(TimeAdvanceGrant, after=mark)
    assert grant.time == (target if expected is None else expected)
    assert await federate.query_logical_time() == grant.time
    assert _grants(log, after=mark) == [grant.time]
    return grant


async def _send(sender, log, obj, attrs, kind, tag, *, timestamp=5.0, order=None, handle=0):
    payload = b"\x00\xffpayload:" + tag
    if kind == "remove":
        obj = await sender.register_object_instance(VEHICLE)
        await log.wait(DiscoverObjectInstance, lambda event: event.object_handle == obj)
        if order is not None:
            await sender.change_attribute_order_type(obj, [attrs[_PRIVILEGE]], order)
        await sender.delete_object_instance(
            obj,
            timestamp=timestamp,
            tag=tag,
            retraction_handle=handle,
        )
    elif kind == "reflect":
        if order is not None:
            await sender.change_attribute_order_type(obj, [attrs["Value"]], order)
        await sender.update_attributes(
            obj,
            {"Value": payload},
            timestamp=timestamp,
            tag=tag,
            retraction_handle=handle,
        )
    else:
        assert kind == "interaction"
        if order is not None:
            signal = await sender.support.get_interaction_class_handle(BASE_SIGNAL)
            await sender.change_interaction_order_type(signal, order)
        await sender.send_interaction(
            BASE_SIGNAL,
            {"Payload": payload},
            timestamp=timestamp,
            tag=tag,
            retraction_handle=handle,
        )
    return kind, obj, tag, timestamp, payload


def _assert_message(event, expected, sender_handle, attrs, sent_order, received_order):
    kind, obj, tag, timestamp, payload = expected
    assert isinstance(event, _KINDS[kind])
    assert event.tag == tag
    assert event.timestamp == timestamp
    assert event.metadata is not None
    assert event.metadata.sent_order == sent_order
    assert event.metadata.received_order == received_order
    assert event.metadata.producing_federate == sender_handle
    if kind == "reflect":
        assert event.object_handle == obj
        assert event.attribute_values == {attrs["Value"]: payload}
    elif kind == "interaction":
        assert event.parameters == {"Payload": payload}
    else:
        assert event.object_handle == obj


@pytest.mark.parametrize("kind", ["reflect", "interaction"])
@pytest.mark.parametrize("constrained", [False, True], ids=["unconstrained", "constrained"])
def test_fom_receive_default_preserves_timestamp_without_order_override(
    live_server,
    kind,
    constrained,  # noqa: F811
):
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attrs):
            await sender.enable_time_regulation(0.5)
            if constrained:
                await reader.enable_time_constrained()
                await reader.enable_asynchronous_delivery()
            expected = await _send(
                sender,
                logs[1],
                obj,
                attrs,
                kind,
                b"fom-default-receive",
                timestamp=3.25,
            )
            await _barrier(sender, logs, "default-order-delivered")
            messages = _messages(logs[1])
            assert len(messages) == 1, "FOM Receive must apply without Change Order Type"
            _assert_message(
                messages[0],
                expected,
                sender.handle,
                attrs,
                OrderType.RECEIVE,
                OrderType.RECEIVE,
            )
            assert await reader.query_logical_time() == 0
            assert _grants(logs[1]) == []

    asyncio.run(exercise())


@pytest.mark.parametrize("constrained", [False, True], ids=["unconstrained", "constrained"])
def test_fom_mixed_attributes_split_sent_and_received_orders(live_server, constrained):  # noqa: F811
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attrs):
            await sender.enable_time_regulation(0.5)
            if constrained:
                await reader.enable_time_constrained()
                await reader.enable_asynchronous_delivery()
            tag = b"\x00\xffmixed-orders"
            await sender.update_attributes(
                obj,
                {"Value": b"receive-value", "Other": b"timestamp-value"},
                timestamp=3.25,
                tag=tag,
                retraction_handle=101,
            )
            await _barrier(sender, logs, "mixed-defaults-fenced")
            immediate = _messages(logs[1])
            if constrained:
                assert len(immediate) == 1, "only the FOM Receive passel is eligible while granted"
                assert immediate[0].attribute_values == {attrs["Value"]: b"receive-value"}
                await sender.modify_lookahead(10.0)
                grant = await _grant(reader, logs[1], "time_advance_request", 5.0)
            await _barrier(sender, logs, "mixed-defaults-complete")
            messages = _messages(logs[1])
            assert len(messages) == 2, "different sent orders require separate reflections"
            by_attribute = {}
            for event in messages:
                assert isinstance(event, ReflectAttributeValues)
                assert event.object_handle == obj and event.tag == tag and event.timestamp == 3.25
                assert len(event.attribute_values) == 1
                assert event.metadata.producing_federate == sender.handle
                by_attribute[next(iter(event.attribute_values))] = event
            ro, tso = by_attribute[attrs["Value"]], by_attribute[attrs["Other"]]
            assert ro.attribute_values == {attrs["Value"]: b"receive-value"}
            assert tso.attribute_values == {attrs["Other"]: b"timestamp-value"}
            assert ro.metadata.sent_order == ro.metadata.received_order == OrderType.RECEIVE
            assert ro.metadata.message_retraction_handle is None
            assert tso.metadata.sent_order == OrderType.TIMESTAMP
            # Table 2 retains the designator even when a receiver converts TSO to RO.
            assert tso.metadata.message_retraction_handle == 101
            assert tso.metadata.received_order == (
                OrderType.TIMESTAMP if constrained else OrderType.RECEIVE
            )
            if constrained:
                assert logs[1].events.index(tso) < logs[1].events.index(grant)

    asyncio.run(exercise())


@pytest.mark.parametrize("override", [False, True], ids=["mim-timestamp", "override-receive"])
def test_delete_uses_privilege_default_order_and_explicit_override(live_server, override):  # noqa: F811
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attrs):
            await sender.enable_time_regulation(0.5)
            await reader.enable_time_constrained()
            await reader.enable_asynchronous_delivery()
            # Value is Receive in the FOM; inherited delete privilege is TimeStamp in the MIM.
            expected = await _send(
                sender,
                logs[1],
                obj,
                attrs,
                "remove",
                b"privilege-order",
                timestamp=3.25,
                order=OrderType.RECEIVE if override else None,
            )
            await _barrier(sender, logs, "delete-order-fenced")
            assert len(_messages(logs[1])) == int(override)
            if not override:
                await sender.modify_lookahead(10.0)
                grant = await _grant(reader, logs[1], "time_advance_request", 5.0)
            await _barrier(sender, logs, "delete-order-complete")
            messages = _messages(logs[1])
            assert len(messages) == 1
            order = OrderType.RECEIVE if override else OrderType.TIMESTAMP
            _assert_message(messages[0], expected, sender.handle, attrs, order, order)
            if not override:
                assert logs[1].events.index(messages[0]) < logs[1].events.index(grant)

    asyncio.run(exercise())


@pytest.mark.parametrize("kind", ["reflect", "interaction"])
@pytest.mark.parametrize(
    "to_timestamp",
    [False, True],
    ids=["timestamp-to-receive", "receive-to-timestamp"],
)
def test_explicit_order_override_precedes_fom_default(live_server, kind, to_timestamp):  # noqa: F811
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attrs):
            await sender.enable_time_regulation(0.5)
            await reader.enable_time_constrained()
            await reader.enable_asynchronous_delivery()
            order = OrderType.TIMESTAMP if to_timestamp else OrderType.RECEIVE
            tag = b"explicit-preference"
            if kind == "reflect":
                name = "Value" if to_timestamp else "Other"
                await sender.change_attribute_order_type(obj, [attrs[name]], order)
                await sender.update_attributes(obj, {name: b"override"}, timestamp=3.25, tag=tag)
            else:
                name = BASE_SIGNAL if to_timestamp else _TIMED_SIGNAL
                handle = await sender.support.get_interaction_class_handle(name)
                await sender.change_interaction_order_type(handle, order)
                await sender.send_interaction(
                    name,
                    {"Payload": b"override"},
                    timestamp=3.25,
                    tag=tag,
                )
            await _barrier(sender, logs, "override-fenced")
            assert len(_messages(logs[1])) == int(not to_timestamp)
            if to_timestamp:
                await sender.modify_lookahead(10.0)
                grant = await _grant(reader, logs[1], "time_advance_request", 5.0)
            await _barrier(sender, logs, "override-complete")
            messages = _messages(logs[1])
            assert len(messages) == 1
            event = messages[0]
            assert isinstance(event, _KINDS[kind])
            assert event.timestamp == 3.25 and event.tag == tag
            assert event.metadata.sent_order == event.metadata.received_order == order
            assert event.metadata.producing_federate == sender.handle
            if kind == "reflect":
                assert event.object_handle == obj
                assert event.attribute_values == {attrs[name]: b"override"}
            else:
                assert event.parameters == {"Payload": b"override"}
            if to_timestamp:
                assert logs[1].events.index(event) < logs[1].events.index(grant)

    asyncio.run(exercise())


@pytest.mark.parametrize("method", _ADVANCES)
@pytest.mark.parametrize("peer_regulating", [False, True], ids=["no-regulator", "peer-regulator"])
def test_unengaged_all_advance_modes_query_time_and_reject_backwards(
    live_server,
    method,
    peer_regulating,  # noqa: F811
):
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, *_):
            if peer_regulating:
                await sender.enable_time_regulation(0.5)
            state = await reader.mom.query_federate_attributes(reader.handle)
            assert state.found and not state.time_regulating and not state.time_constrained
            assert await reader.query_logical_time() == 0
            await _grant(reader, logs[1], method, 5.0)
            with pytest.raises(LogicalTimeAlreadyPassed):
                await getattr(reader, method)(4.0)
            await _barrier(sender, logs, "backwards-rejected")
            assert await reader.query_logical_time() == 5.0
            assert _grants(logs[1]) == [5.0]
            # Equality is permitted, and must produce a fresh grant.
            await _grant(reader, logs[1], method, 5.0)
            await _grant(reader, logs[1], method, 9.0)
            await _barrier(sender, logs, "unengaged-advances-complete")
            assert _grants(logs[1]) == [5.0, 5.0, 9.0]
            assert await sender.query_logical_time() == 0
            state = await reader.mom.query_federate_attributes(reader.handle)
            assert not state.time_regulating and not state.time_constrained

    asyncio.run(exercise())


@pytest.mark.parametrize("engagement", ["regulating", "constrained", "both"])
def test_later_time_engagement_and_disengagement_preserve_unengaged_time(
    live_server,
    engagement,  # noqa: F811
):
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, *_):
            await _grant(reader, logs[1], "time_advance_request", 5.0)
            for cycle, target in enumerate((8.0, 12.0)):
                if engagement in ("regulating", "both"):
                    await reader.enable_time_regulation(0.5)
                if engagement in ("constrained", "both"):
                    await reader.enable_time_constrained()
                await _barrier(sender, logs, f"enabled-{cycle}")
                assert await reader.query_logical_time() == (5.0 if cycle == 0 else 10.0)
                state = await reader.mom.query_federate_attributes(reader.handle)
                assert state.time_regulating == (engagement in ("regulating", "both"))
                assert state.time_constrained == (engagement in ("constrained", "both"))
                await _grant(reader, logs[1], "time_advance_request", target)
                if engagement in ("constrained", "both"):
                    await reader.disable_time_constrained()
                if engagement in ("regulating", "both"):
                    await reader.disable_time_regulation()
                assert await reader.query_logical_time() == target
                await _grant(reader, logs[1], "time_advance_request_available", target + 2.0)
            await _barrier(sender, logs, "engagement-cycles-complete")
            assert _grants(logs[1]) == [5.0, 8.0, 10.0, 12.0, 14.0]

    asyncio.run(exercise())


async def _granted_at_five(sender, reader, logs):
    await sender.enable_time_regulation(5.0)
    await reader.enable_time_constrained()
    # GALT equals five. Available permits later incoming TSO at exactly five.
    await _grant(reader, logs[1], "time_advance_request_available", 5.0)
    await _barrier(sender, logs, "available-grant-five")


@pytest.mark.parametrize("kind", tuple(_KINDS))
@pytest.mark.parametrize("async_before", [False, True], ids=["enable-after-send", "already-async"])
def test_granted_same_time_tso_stays_buffered_even_with_async(
    live_server,
    kind,
    async_before,  # noqa: F811
):
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attrs):
            await _granted_at_five(sender, reader, logs)
            if async_before:
                await reader.enable_asynchronous_delivery()
            expected = await _send(
                sender,
                logs[1],
                obj,
                attrs,
                kind,
                b"same-time-held",
                order=OrderType.TIMESTAMP,
            )
            await _barrier(sender, logs, "same-time-send-fenced")
            assert _messages(logs[1]) == [], "ts == current does not authorize granted-state TSO"
            if not async_before:
                await reader.enable_asynchronous_delivery()
            # A positive RO callback proves async delivery and the live stream work.
            await sender.send_interaction(
                BASE_SIGNAL,
                {"Payload": b"ro-control"},
                tag=b"ro-control",
            )
            await _barrier(sender, logs, "async-same-time-fenced")
            messages = _messages(logs[1])
            assert [event.tag for event in messages] == [b"ro-control"]
            assert messages[0].metadata.received_order == OrderType.RECEIVE
            assert await reader.query_logical_time() == 5.0
            await sender.modify_lookahead(20.0)
            grant = await _grant(reader, logs[1], "time_advance_request", 6.0)
            await _barrier(sender, logs, "same-time-released")
            delivered = [event for event in _messages(logs[1]) if event.tag == expected[2]]
            assert len(delivered) == 1
            _assert_message(
                delivered[0],
                expected,
                sender.handle,
                attrs,
                OrderType.TIMESTAMP,
                OrderType.TIMESTAMP,
            )
            assert logs[1].events.index(delivered[0]) < logs[1].events.index(grant)

    asyncio.run(exercise())


@pytest.mark.parametrize("kind", tuple(_KINDS))
@pytest.mark.parametrize("method", _NEXT)
def test_next_message_higher_request_grants_queued_current_time(live_server, kind, method):  # noqa: F811
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attrs):
            await _granted_at_five(sender, reader, logs)
            await reader.enable_asynchronous_delivery()
            expected = []
            for index, timestamp in enumerate((5.0, 5.0, 7.0)):
                expected.append(
                    await _send(
                        sender,
                        logs[1],
                        obj,
                        attrs,
                        kind,
                        f"queued-{index}".encode(),
                        timestamp=timestamp,
                        order=OrderType.TIMESTAMP,
                    )
                )
            await sender.modify_lookahead(20.0)
            await _barrier(sender, logs, "next-message-queue-fenced")
            before_request = len(logs[1].events)
            # Assert the selected grant first so an early-delivery bug cannot mask a skip.
            grant = await _grant(reader, logs[1], method, 10.0, expected=5.0)
            await _barrier(sender, logs, "next-message-current-granted")
            assert _messages(logs[1], after=0) == _messages(logs[1], after=before_request)
            messages = _messages(logs[1], after=before_request)
            assert sorted(event.tag for event in messages) == [b"queued-0", b"queued-1"]
            for item in expected[:2]:
                event = next(event for event in messages if event.tag == item[2])
                _assert_message(
                    event,
                    item,
                    sender.handle,
                    attrs,
                    OrderType.TIMESTAMP,
                    OrderType.TIMESTAMP,
                )
                assert logs[1].events.index(event) < logs[1].events.index(grant, before_request)
            grant = await _grant(reader, logs[1], method, 10.0, expected=7.0)
            await _barrier(sender, logs, "next-message-future-granted")
            future = [event for event in _messages(logs[1]) if event.tag == b"queued-2"]
            assert len(future) == 1
            _assert_message(
                future[0],
                expected[2],
                sender.handle,
                attrs,
                OrderType.TIMESTAMP,
                OrderType.TIMESTAMP,
            )
            assert logs[1].events.index(future[0]) < logs[1].events.index(grant)
            await _grant(reader, logs[1], method, 10.0)
            await _barrier(sender, logs, "next-message-empty")
            assert _grants(logs[1]) == [5.0, 5.0, 7.0, 10.0]
            assert len(_messages(logs[1])) == 3

    asyncio.run(exercise())


@pytest.mark.parametrize("kind", tuple(_KINDS))
def test_retraction_preserves_same_time_tso_and_removes_future_message(
    live_server,
    kind,  # noqa: F811
):
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attrs):
            await _granted_at_five(sender, reader, logs)
            expected = await _send(
                sender,
                logs[1],
                obj,
                attrs,
                kind,
                b"retained-same-time",
                order=OrderType.TIMESTAMP,
                handle=101,
            )
            retracted = await _send(
                sender,
                logs[1],
                obj,
                attrs,
                kind,
                b"retracted-future",
                timestamp=7.0,
                order=OrderType.TIMESTAMP,
                handle=102,
            )
            await _barrier(sender, logs, "before-same-time-retraction")
            assert _messages(logs[1]) == []
            # 8.21 requires ts > sender time + actual lookahead, not equality.
            # The future message is retractable; the same-time message is not.
            await sender.retract(102)
            await reader.enable_asynchronous_delivery()
            await _barrier(sender, logs, "retraction-does-not-release-same-time")
            assert _messages(logs[1]) == []
            await sender.modify_lookahead(20.0)
            mark = len(logs[1].events)
            grant = await _grant(reader, logs[1], "next_message_request", 10.0, expected=5.0)
            await _barrier(sender, logs, "after-same-time-retraction")
            messages = _messages(logs[1])
            assert len(messages) == 1
            _assert_message(
                messages[0],
                expected,
                sender.handle,
                attrs,
                OrderType.TIMESTAMP,
                OrderType.TIMESTAMP,
            )
            assert messages[0].metadata.message_retraction_handle == 101
            assert logs[1].events.index(messages[0]) < logs[1].events.index(grant, mark)
            await _grant(reader, logs[1], "next_message_request", 10.0)
            await _barrier(sender, logs, "retracted-future-not-delivered")
            assert _messages(logs[1]) == messages
            if kind == "remove":
                # Canceling a buffered timestamped deletion must leave the object usable.
                await sender.update_attributes(retracted[1], {"Value": b"still-live"}, tag=b"alive")
                await _barrier(sender, logs, "retracted-delete-object-alive")
                messages = _messages(logs[1])
                assert len(messages) == 2 and isinstance(messages[1], ReflectAttributeValues)
                assert messages[1].object_handle == retracted[1]
                assert messages[1].attribute_values == {attrs["Value"]: b"still-live"}

    asyncio.run(exercise())


@pytest.mark.parametrize("method", _NEXT)
def test_save_restore_retains_same_time_tso_queue_and_granted_state(live_server, method):  # noqa: F811
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attrs):
            await _granted_at_five(sender, reader, logs)
            await reader.enable_asynchronous_delivery()
            expected = await _send(
                sender,
                logs[1],
                obj,
                attrs,
                "interaction",
                b"saved-same-time",
                order=OrderType.TIMESTAMP,
                handle=101,
            )
            await _barrier(sender, logs, "before-same-time-save")
            assert _messages(logs[1]) == []
            await _save([sender, reader], logs, "same-time-queue")
            await sender.modify_lookahead(20.0)
            await _grant(reader, logs[1], "time_advance_request", 8.0)
            await _barrier(sender, logs, "postsave-queue-consumed")
            assert len(_messages(logs[1])) == 1
            mark = len(logs[1].events)
            await _restore([sender, reader], logs, "same-time-queue", reader.handle)
            await _barrier(sender, logs, "same-time-restored-granted")
            assert await reader.query_logical_time() == 5.0
            assert await sender.query_lookahead() == 5.0
            assert _messages(logs[1], after=mark) == []
            await reader.disable_asynchronous_delivery()
            await reader.enable_asynchronous_delivery()
            await _barrier(sender, logs, "restored-async-does-not-release-tso")
            assert _messages(logs[1], after=mark) == []
            await sender.modify_lookahead(20.0)
            request_mark = len(logs[1].events)
            grant = await _grant(reader, logs[1], method, 10.0, expected=5.0)
            await _barrier(sender, logs, "restored-same-time-delivered")
            messages = _messages(logs[1], after=mark)
            assert len(messages) == 1
            _assert_message(
                messages[0],
                expected,
                sender.handle,
                attrs,
                OrderType.TIMESTAMP,
                OrderType.TIMESTAMP,
            )
            assert messages[0].metadata.message_retraction_handle == 101
            message_index = next(
                i for i, event in enumerate(logs[1].events) if event is messages[0]
            )
            grant_index = next(i for i, event in enumerate(logs[1].events) if event is grant)
            assert request_mark <= message_index < grant_index
            await _grant(reader, logs[1], method, 10.0)
            await _barrier(sender, logs, "restored-queue-exhausted")
            assert len(_messages(logs[1], after=mark)) == 1

    asyncio.run(exercise())


async def _ro_queue_length(reader):
    state = await reader.mom.query_federate_attributes(reader.handle)
    assert state.found
    assert "HLAROlength" not in state.unavailable_attributes
    value = state.attribute_values["HLAROlength"]
    assert len(value) == 4
    return int.from_bytes(value, byteorder="big", signed=True)


async def _message_counters(federate):
    state = await federate.mom.query_federate_attributes(federate.handle)
    assert state.found
    return (
        state.updates_sent,
        state.reflections_received,
        state.interactions_sent,
        state.interactions_received,
    )


async def _regional_interaction_route(sender, reader):
    signal = await sender.support.get_interaction_class_handle(BASE_SIGNAL)
    parameter = await sender.support.get_parameter_handle(signal, "Payload")
    space = await sender.ddm.lookup_routing_space("default")
    dimension = await sender.ddm.lookup_dimension(space, "X")
    regions = []
    for federate in (sender, reader):
        region = await federate.ddm.create_region(space, [dimension])
        await federate.ddm.set_range_bounds(region, dimension, lower=10, upper=20)
        await federate.ddm.commit_region_modifications([region])
        regions.append(region)
    await reader.unsubscribe_interaction_class(BASE_SIGNAL)
    await reader.ddm.subscribe_interaction_class_with_regions(signal, [regions[1]])
    return signal, parameter, regions[0]


@pytest.mark.parametrize("kind", ["reflect", "interaction", "regional"])
@pytest.mark.parametrize(
    "timestamp",
    [float("nan"), -1.0, float("-inf")],
    ids=["nan", "negative", "negative-infinity"],
)
def test_ro_malformed_original_timestamp_rejected_without_effects(
    live_server,
    kind,
    timestamp,  # noqa: F811
):
    """6.10.5(a), 6.12.5(a), 9.12.5(a): RO still validates the original timestamp.

    This legacy protocol accepts positive infinity for these services; that
    separate representation issue is deliberately not treated as malformed here.
    """

    async def exercise():
        async with _participants(live_server, regional=kind == "regional") as (
            sender,
            reader,
            logs,
            obj,
            attrs,
        ):
            route = (
                await _regional_interaction_route(sender, reader) if kind == "regional" else None
            )
            await sender.enable_time_regulation(0.5)
            await _grant(sender, logs[0], "time_advance_request", 10.0)
            await reader.enable_time_constrained()
            await _grant(reader, logs[1], "time_advance_request", 8.0)
            await _barrier(sender, logs, "malformed-timestamp-ready")
            assert await _ro_queue_length(reader) == 0
            assert await reader.query_lits() == (10.5, True)
            before_counters = [await _message_counters(fed) for fed in (sender, reader)]

            async def send(value, tag):
                if route is None:
                    return await _send(
                        sender,
                        logs[1],
                        obj,
                        attrs,
                        kind,
                        tag,
                        timestamp=value,
                    )
                signal, parameter, region = route
                payload = b"\x00\xffpayload:" + tag
                await sender.ddm.send_interaction_with_regions(
                    signal,
                    {parameter: payload},
                    [region],
                    timestamp=value,
                    tag=tag,
                )
                return "interaction", obj, tag, value, payload

            with pytest.raises(InvalidLogicalTime):
                await send(timestamp, b"malformed-rejected")
            await _barrier(sender, logs, "malformed-timestamp-rejected")
            assert _messages(logs[1]) == []
            assert await _ro_queue_length(reader) == 0
            assert await reader.query_lits() == (10.5, True)
            assert [await _message_counters(fed) for fed in (sender, reader)] == before_counters

            # A valid RO timestamp may predate both logical times and the TSO send floor.
            expected = await send(3.25, b"valid-past-ro")
            await _barrier(sender, logs, "valid-past-ro-held")
            assert _messages(logs[1]) == []
            assert await _ro_queue_length(reader) == 1
            assert await reader.query_lits() == (10.5, True)
            await reader.enable_asynchronous_delivery()
            await _barrier(sender, logs, "valid-past-ro-delivered")
            messages = _messages(logs[1])
            assert len(messages) == 1
            _assert_message(
                messages[0],
                expected,
                sender.handle,
                attrs,
                OrderType.RECEIVE,
                OrderType.RECEIVE,
            )
            assert messages[0].metadata.message_retraction_handle is None
            assert await _ro_queue_length(reader) == 1
            entered = []

            def invoke(event):
                _assert_message(
                    event,
                    expected,
                    sender.handle,
                    attrs,
                    OrderType.RECEIVE,
                    OrderType.RECEIVE,
                )
                entered.append(event)

            await reader.dispatch_event(messages[0], invoke)
            assert len(entered) == 1 and entered[0] is messages[0]
            assert await _ro_queue_length(reader) == 0
            assert await sender.query_logical_time() == 10.0
            assert await reader.query_logical_time() == 8.0
            await _grant(reader, logs[1], "next_message_request", 9.0)
            await _barrier(sender, logs, "no-malformed-message-after-advance")
            assert _messages(logs[1]) == messages
            assert await reader.query_lits() == (10.5, True)

    asyncio.run(exercise())
