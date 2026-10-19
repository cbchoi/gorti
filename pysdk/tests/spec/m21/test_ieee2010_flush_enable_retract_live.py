"""Real-server oracles for flush, safe time engagement, and retraction.

Source: supplied active IEEE 1516.1-2010 text, 8.2-8.6 (PDF pp.
192-198), 8.12 (206-208), and 8.21-8.22 (215-217). The two federates
use independent public Python connections, not mock transports. Callback
fences and successful control operations delimit assertions without sleeps.
Object-knowledge checks follow 6.15.4 (PDF p. 135).

Engagement tests observe real enabled callbacks, public logical time, MOM
state, pending exceptions, and message order. No SDK or server implementation
is supplied by this test module.
"""

from __future__ import annotations

import asyncio
import math
import xml.etree.ElementTree as ET

import pytest

from rti1516e._grpc_errors import (
    RequestForTimeConstrainedPending,
    TimeAdvancingState,
    TimeRegulationNotEnabled,
)
from rti1516e.errors import (
    InvalidMessageRetractionHandle,
    MessageCanNoLongerBeRetracted,
    ObjectInstanceNotKnown,
)
from rti1516e.events import (
    OrderType,
    ReflectAttributeValues,
    RequestRetraction,
    TimeAdvanceGrant,
    TimeConstrainedEnabled,
    TimeRegulationEnabled,
)
from tests.spec.m21.test_ieee2010_order_advance_live import (
    _TIMED_SIGNAL,
    _assert_message,
    _barrier,
    _grant,
    _grants,
    _messages,
    _participants,
    _send,
)
from tests.test_live_service_e2e import (
    BASE_SIGNAL,
    live_artifacts,  # noqa: F401
    live_server,  # noqa: F401
)

pytestmark = [pytest.mark.spec, pytest.mark.integration]

_KINDS = ["reflect", "interaction", "remove"]


def _retractions(log, *, after=0):
    return [event for event in log.events[after:] if isinstance(event, RequestRetraction)]


async def _state(federate):
    state = await federate.mom.query_federate_attributes(federate.handle)
    assert state.found
    assert state.logical_time == await federate.query_logical_time()
    return state


def _enabled(log, kind, *, after=0):
    return [event for event in log.events[after:] if isinstance(event, kind)]


async def _ro_control(sender, logs, tag):
    await sender.send_interaction(BASE_SIGNAL, {"Payload": b"control"}, tag=tag)
    await _barrier(sender, logs, tag.decode("ascii") + "-fence")
    controls = [event for event in _messages(logs[1]) if event.tag == tag]
    assert len(controls) == 1
    assert controls[0].metadata.received_order == OrderType.RECEIVE
    return controls[0]


@pytest.mark.parametrize("kind", _KINDS)
@pytest.mark.parametrize("limiter", ["request", "galt", "earliest-message"])
def test_fqr_flushes_all_orders_and_uses_smallest_grant(live_server, kind, limiter):  # noqa: F811
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attrs):
            await sender.enable_time_regulation(1.0)
            await reader.enable_time_constrained()
            expected = []
            for index, timestamp in enumerate((20.0, 5.0, 7.0)):
                expected.append(
                    await _send(
                        sender,
                        logs[1],
                        obj,
                        attrs,
                        kind,
                        f"flush-{index}".encode(),
                        timestamp=timestamp,
                        order=OrderType.TIMESTAMP,
                        handle=101 + index,
                    )
                )
            await sender.send_interaction(BASE_SIGNAL, {"Payload": b"queued-ro"}, tag=b"queued-ro")
            if limiter != "galt":
                await sender.modify_lookahead(30.0)
            await _barrier(sender, logs, "flush-input-fenced")
            assert _messages(logs[1]) == []
            target = 3.0 if limiter == "request" else 10.0
            expected_time = {"request": 3.0, "galt": 1.0, "earliest-message": 5.0}[limiter]
            mark = len(logs[1].events)
            grant = await _grant(
                reader, logs[1], "flush_queue_request", target, expected=expected_time
            )
            await _barrier(sender, logs, "flush-complete")
            messages = _messages(logs[1], after=mark)
            assert sorted(event.tag for event in messages) == [
                b"flush-0",
                b"flush-1",
                b"flush-2",
                b"queued-ro",
            ], "FQR must include queued TSO beyond both the request and grant"
            for index, item in enumerate(expected):
                event = next(event for event in messages if event.tag == item[2])
                _assert_message(
                    event, item, sender.handle, attrs, OrderType.TIMESTAMP, OrderType.TIMESTAMP
                )
                assert event.metadata.message_retraction_handle == 101 + index
            for event in messages:
                assert logs[1].events.index(event, mark) < logs[1].events.index(grant, mark)
            ro = next(event for event in messages if event.tag == b"queued-ro")
            assert ro.metadata.received_order == OrderType.RECEIVE
            # The second grant is a positive completion witness for an empty queue.
            await _grant(
                reader,
                logs[1],
                "flush_queue_request",
                10.0,
                expected=1.0 if limiter == "galt" else 10.0,
            )
            await _barrier(sender, logs, "flush-empty-complete")
            assert _messages(logs[1], after=mark) == messages

    asyncio.run(exercise())


@pytest.mark.parametrize("queued", [False, True], ids=["empty", "future-tso"])
def test_fqr_completes_at_current_time_when_galt_cannot_advance(live_server, queued):  # noqa: F811
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attrs):
            await sender.enable_time_regulation(0.0)
            await reader.enable_time_constrained()
            if queued:
                await _send(
                    sender,
                    logs[1],
                    obj,
                    attrs,
                    "interaction",
                    b"future-flush",
                    timestamp=5.0,
                    order=OrderType.TIMESTAMP,
                    handle=101,
                )
            await sender.send_interaction(BASE_SIGNAL, {"Payload": b"control"}, tag=b"ro-control")
            await _barrier(sender, logs, "zero-galt-input")
            assert await reader.query_logical_time() == 0.0
            grant = await _grant(reader, logs[1], "flush_queue_request", 10.0, expected=0.0)
            await _barrier(sender, logs, "zero-galt-completed")
            messages = _messages(logs[1])
            assert sorted(event.tag for event in messages) == (
                [b"future-flush", b"ro-control"] if queued else [b"ro-control"]
            )
            assert all(
                logs[1].events.index(event) < logs[1].events.index(grant) for event in messages
            )
            await _grant(reader, logs[1], "flush_queue_request", 10.0, expected=0.0)
            await _barrier(sender, logs, "zero-galt-second-completed")
            assert _grants(logs[1]) == [0.0, 0.0]
            assert _messages(logs[1]) == messages

    asyncio.run(exercise())


@pytest.mark.parametrize("delayed", [False, True], ids=["DSE-false", "DSE-true"])
@pytest.mark.parametrize("retained", [False, True], ids=["all-filtered", "later-retained"])
def test_fqr_grant_ignores_tso_discarded_by_current_subscription(
    live_server, retained, delayed,  # noqa: F811
):
    url, base, extra = live_server
    tree = ET.parse(base)  # noqa: S314 - trusted local fixture XML
    namespace = "{http://standards.ieee.org/IEEE1516-2010}"
    switches = ET.SubElement(tree.getroot(), namespace + "switches")
    ET.SubElement(
        switches, namespace + "delaySubscriptionEvaluation", isEnabled=str(delayed).lower(),
    )
    configured = base.with_name(f"subscription-{str(delayed).lower()}.xml")
    tree.write(configured, encoding="utf-8", xml_declaration=True)

    async def exercise():
        # 8.1.8 requires current-subscription evaluation at delivery in both
        # modes. All messages here had matching subscriptions when sent.
        async with _participants((url, configured, extra)) as (sender, reader, logs, obj, attrs):
            await sender.enable_time_regulation(1.0)
            await reader.enable_time_constrained()
            await _send(
                sender, logs[1], obj, attrs, "interaction", b"unsubscribed-earliest",
                timestamp=3.0, order=OrderType.TIMESTAMP, handle=1401,
            )
            if retained:
                await sender.send_interaction(
                    _TIMED_SIGNAL, {"Payload": b"retained"}, timestamp=7.0,
                    tag=b"retained-later", retraction_handle=1402,
                )
            await sender.update_attributes(obj, {"Value": b"control"}, tag=b"ro-control")
            await _barrier(sender, logs, "filter-flush-queue-ready")
            assert _messages(logs[1]) == []
            await reader.unsubscribe_interaction_class(BASE_SIGNAL)
            await sender.modify_lookahead(30.0)
            grant = await _grant(
                reader, logs[1], "flush_queue_request", 10.0, expected=7.0 if retained else 10.0,
            )
            await _barrier(sender, logs, "filtered-flush-completed")
            delivered = _messages(logs[1])
            assert sorted(event.tag for event in delivered) == (
                [b"retained-later", b"ro-control"] if retained else [b"ro-control"]
            )
            assert all(logs[1].events.index(event) < logs[1].events.index(grant)
                       for event in delivered)
            control = next(event for event in delivered if event.tag == b"ro-control")
            assert control.metadata.received_order == OrderType.RECEIVE
            assert control.attribute_values == {attrs["Value"]: b"control"}
            if retained:
                kept = next(event for event in delivered if event.tag == b"retained-later")
                assert kept.timestamp == 7.0 and kept.parameters == {"Payload": b"retained"}
                assert kept.metadata.received_order == OrderType.TIMESTAMP
                assert kept.metadata.message_retraction_handle == 1402
            await reader.subscribe_interaction_class(BASE_SIGNAL)
            await reader.enable_asynchronous_delivery()
            await _ro_control(sender, logs, b"resubscribed-control")
            await _grant(reader, logs[1], "flush_queue_request", 10.0)
            await _barrier(sender, logs, "filtered-message-not-revived")
            assert sorted(event.tag for event in _messages(logs[1])) == sorted(
                [event.tag for event in delivered] + [b"resubscribed-control"]
            )

    asyncio.run(exercise())


@pytest.mark.parametrize("kind", _KINDS)
@pytest.mark.parametrize("delivery", ["flush-tso", "unconstrained-ro"])
def test_delivered_retractable_message_notifies_recipient(live_server, kind, delivery):  # noqa: F811
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attrs):
            await sender.enable_time_regulation(1.0)
            constrained = delivery == "flush-tso"
            if constrained:
                await reader.enable_time_constrained()
            if kind == "remove":
                known_class = await reader.support.get_known_object_class_handle(obj)
                await sender.delete_object_instance(
                    obj,
                    timestamp=5.0,
                    tag=b"delivered-retractable",
                    retraction_handle=501,
                )
                expected = (kind, obj, b"delivered-retractable", 5.0, b"")
            else:
                expected = await _send(
                    sender,
                    logs[1],
                    obj,
                    attrs,
                    kind,
                    b"delivered-retractable",
                    timestamp=5.0,
                    order=OrderType.TIMESTAMP,
                    handle=501,
                )
            if constrained:
                await _grant(reader, logs[1], "flush_queue_request", 10.0, expected=1.0)
            await _barrier(sender, logs, "retractable-message-delivered")
            original = _messages(logs[1])
            assert len(original) == 1
            _assert_message(
                original[0],
                expected,
                sender.handle,
                attrs,
                OrderType.TIMESTAMP,
                OrderType.TIMESTAMP if constrained else OrderType.RECEIVE,
            )
            assert original[0].metadata.message_retraction_handle == 501
            if kind == "remove":
                # 6.15.4: FQR TSO removal is provisional until time reaches 5;
                # received RO removal makes the object unknown immediately.
                if constrained:
                    assert await reader.support.get_known_object_class_handle(obj) == known_class
                else:
                    with pytest.raises(ObjectInstanceNotKnown):
                        await reader.support.get_known_object_class_handle(obj)
            mark = len(logs[1].events)
            await sender.retract(501)
            await _barrier(sender, logs, "delivered-retraction-complete")
            callbacks = _retractions(logs[1], after=mark)
            assert len(callbacks) == 1, "a delivered retractable message requires RequestRetraction"
            assert callbacks[0].sender_federate == sender.handle
            assert callbacks[0].retraction_handle == 501
            assert logs[1].events.index(original[0]) < logs[1].events.index(callbacks[0], mark)
            if constrained:
                await reader.enable_asynchronous_delivery()
            await _ro_control(sender, logs, b"after-retraction-control")
            if kind == "remove":
                restored = expected[1]
                assert await reader.support.get_known_object_class_handle(restored) == known_class
                for name in ("Value", "Other", "HLAprivilegeToDeleteObject"):
                    assert await sender.ownership.is_attribute_owned_by_federate(
                        restored, attrs[name]
                    )
                await sender.update_attributes(restored, {"Value": b"restored"}, tag=b"restored")
                await _barrier(sender, logs, "retracted-deletion-restored")
                updates = [event for event in _messages(logs[1]) if event.tag == b"restored"]
                assert len(updates) == 1 and isinstance(updates[0], ReflectAttributeValues)
                assert updates[0].object_handle == restored
                assert updates[0].attribute_values == {attrs["Value"]: b"restored"}
            assert len(_retractions(logs[1], after=mark)) == 1

    asyncio.run(exercise())


@pytest.mark.parametrize("kind", _KINDS)
@pytest.mark.parametrize("advancing", [False, True], ids=["granted", "advancing"])
@pytest.mark.parametrize("reduce_lookahead", [False, True], ids=["fixed", "pending-reduction"])
def test_retract_rejects_exact_floor_but_accepts_later_message(
    live_server,  # noqa: F811
    kind,
    advancing,
    reduce_lookahead,
):
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attrs):
            actual = 4.0 if reduce_lookahead else 1.0
            await sender.enable_time_regulation(actual)
            if reduce_lookahead:
                await sender.modify_lookahead(1.0)
            assert await sender.query_lookahead() == actual
            await reader.enable_time_constrained()
            if advancing:
                await reader.enable_time_regulation(1.0)
                await sender.enable_time_constrained()
            timestamp = (5.0 if advancing else 0.0) + actual
            retained = await _send(
                sender,
                logs[1],
                obj,
                attrs,
                kind,
                b"strict-boundary",
                timestamp=timestamp,
                order=OrderType.TIMESTAMP,
                handle=601,
            )
            await _send(
                sender,
                logs[1],
                obj,
                attrs,
                kind,
                b"legal-later-retraction",
                timestamp=timestamp + 1.0,
                order=OrderType.TIMESTAMP,
                handle=602,
            )
            if advancing:
                await sender.time_advance_request(5.0)
                await _barrier(sender, logs, "advancing-retraction-ready")
                assert await sender.query_logical_time() == 0.0
                with pytest.raises(TimeAdvancingState):
                    await sender.time_advance_request(5.0)
            assert await sender.query_lookahead() == actual
            with pytest.raises(MessageCanNoLongerBeRetracted):
                await sender.retract(601)
            await sender.retract(602)
            if advancing:
                mark = len(logs[0].events)
                await reader.modify_lookahead(20.0)
                grant = await logs[0].wait(TimeAdvanceGrant, after=mark)
                assert grant.time == 5.0
            await sender.modify_lookahead(20.0)
            await _grant(reader, logs[1], "time_advance_request", 10.0)
            await _barrier(sender, logs, "boundary-retraction-complete")
            delivered = _messages(logs[1])
            assert len(delivered) == 1
            _assert_message(
                delivered[0],
                retained,
                sender.handle,
                attrs,
                OrderType.TIMESTAMP,
                OrderType.TIMESTAMP,
            )
            assert delivered[0].metadata.message_retraction_handle == 601
            # No absence requirement for RequestRetraction of never-delivered data:
            # 8.22 permits that notification; delivery after retraction is forbidden.

    asyncio.run(exercise())


@pytest.mark.parametrize("kind", _KINDS)
def test_retract_after_disabling_regulation_fails_without_losing_message(live_server, kind):  # noqa: F811
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attrs):
            await sender.enable_time_regulation(1.0)
            await reader.enable_time_constrained()
            retained = await _send(
                sender,
                logs[1],
                obj,
                attrs,
                kind,
                b"disabled-retained",
                timestamp=5.0,
                order=OrderType.TIMESTAMP,
                handle=701,
            )
            await sender.disable_time_regulation()
            with pytest.raises(TimeRegulationNotEnabled):
                await sender.retract(701)
            await _grant(reader, logs[1], "time_advance_request", 8.0)
            await _barrier(sender, logs, "disabled-retract-rejected")
            delivered = _messages(logs[1])
            assert len(delivered) == 1
            _assert_message(
                delivered[0],
                retained,
                sender.handle,
                attrs,
                OrderType.TIMESTAMP,
                OrderType.TIMESTAMP,
            )

    asyncio.run(exercise())


@pytest.mark.parametrize("kind", _KINDS)
def test_retract_cannot_use_another_senders_designator(live_server, kind):  # noqa: F811
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attrs):
            await sender.enable_time_regulation(1.0)
            await reader.enable_time_regulation(1.0)
            await reader.enable_time_constrained()
            retained = await _send(
                sender,
                logs[1],
                obj,
                attrs,
                kind,
                b"other-sender-retained",
                timestamp=5.0,
                order=OrderType.TIMESTAMP,
                handle=801,
            )
            await _send(
                sender,
                logs[1],
                obj,
                attrs,
                kind,
                b"sender-retracts-own",
                timestamp=6.0,
                order=OrderType.TIMESTAMP,
                handle=802,
            )
            with pytest.raises(InvalidMessageRetractionHandle):
                await reader.retract(801)
            with pytest.raises(InvalidMessageRetractionHandle):
                await sender.retract(99999)
            await sender.retract(802)
            await sender.modify_lookahead(20.0)
            await _grant(reader, logs[1], "time_advance_request", 10.0)
            await _barrier(sender, logs, "sender-specific-retraction-complete")
            delivered = _messages(logs[1])
            assert len(delivered) == 1
            _assert_message(
                delivered[0],
                retained,
                sender.handle,
                attrs,
                OrderType.TIMESTAMP,
                OrderType.TIMESTAMP,
            )

    asyncio.run(exercise())


def test_sent_ro_has_no_valid_retraction_designator(live_server):  # noqa: F811
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attrs):
            await sender.enable_time_regulation(1.0)
            await _send(
                sender,
                logs[1],
                obj,
                attrs,
                "interaction",
                b"not-sent-tso",
                timestamp=5.0,
                order=OrderType.RECEIVE,
                handle=901,
            )
            await _barrier(sender, logs, "sent-ro-complete")
            messages = _messages(logs[1])
            assert len(messages) == 1
            assert messages[0].metadata.sent_order == OrderType.RECEIVE
            assert messages[0].metadata.message_retraction_handle is None
            with pytest.raises(InvalidMessageRetractionHandle):
                await sender.retract(901)
            await _ro_control(sender, logs, b"after-invalid-ro-retract")

    asyncio.run(exercise())


@pytest.mark.parametrize("finish", ["advance", "disable-constraint"])
def test_fqr_provisional_removal_expires_at_time_or_constraint_disable(
    live_server,  # noqa: F811
    finish,
):
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attrs):
            await sender.enable_time_regulation(1.0)
            await reader.enable_time_constrained()
            known_class = await reader.support.get_known_object_class_handle(obj)
            await sender.delete_object_instance(
                obj,
                timestamp=5.0,
                tag=b"provisional-delete",
                retraction_handle=951,
            )
            await _grant(reader, logs[1], "flush_queue_request", 10.0, expected=1.0)
            await _barrier(sender, logs, "provisional-delete-delivered")
            messages = _messages(logs[1])
            assert len(messages) == 1
            _assert_message(
                messages[0],
                ("remove", obj, b"provisional-delete", 5.0, b""),
                sender.handle,
                attrs,
                OrderType.TIMESTAMP,
                OrderType.TIMESTAMP,
            )
            assert await reader.support.get_known_object_class_handle(obj) == known_class
            if finish == "advance":
                await sender.modify_lookahead(20.0)
                await _grant(reader, logs[1], "time_advance_request_available", 5.0)
            else:
                await reader.disable_time_constrained()
                assert not (await _state(reader)).time_constrained
            await _barrier(sender, logs, "provisional-delete-now-final")
            with pytest.raises(ObjectInstanceNotKnown):
                await reader.support.get_known_object_class_handle(obj)
            assert _messages(logs[1]) == messages, "finalizing knowledge must not repeat Remove"

    asyncio.run(exercise())


@pytest.mark.parametrize("available", [False, True], ids=["strict-peer", "available-peer"])
def test_enable_regulation_chooses_time_safe_for_advanced_constrained_peer(
    live_server,  # noqa: F811
    available,
):
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attrs):
            await reader.enable_time_constrained()
            constrained = await logs[1].wait(TimeConstrainedEnabled)
            assert constrained.time == 0.0
            method = "time_advance_request_available" if available else "time_advance_request"
            await _grant(reader, logs[1], method, 10.0)
            mark = len(logs[0].events)
            await sender.enable_time_regulation(2.0)
            enabled = await logs[0].wait(TimeRegulationEnabled, after=mark)
            await _barrier(sender, logs, "safe-regulation-enabled")
            state = await _state(sender)
            assert state.time_regulating
            assert enabled.time == state.logical_time
            assert _enabled(logs[0], TimeRegulationEnabled, after=mark) == [enabled]
            assert await sender.query_lookahead() == 2.0
            if available:
                # The predecessor of 8 also rounds to 10 when adding 2;
                # section 8.2 requires the minimum legal Float64 time.
                assert state.logical_time == math.nextafter(8.0, 0.0)
                assert state.logical_time + 2.0 == 10.0
                assert math.nextafter(state.logical_time, 0.0) + 2.0 < 10.0
            else:
                assert state.logical_time + 2.0 > 10.0, "strict grant forbids later TSO at its time"
                assert state.logical_time == math.nextafter(8.0, math.inf)
            expected = await _send(
                sender,
                logs[1],
                obj,
                attrs,
                "interaction",
                b"safe-new-regulator",
                timestamp=state.logical_time + 2.0,
                order=OrderType.TIMESTAMP,
                handle=1001,
            )
            await sender.modify_lookahead(20.0)
            grant = await _grant(reader, logs[1], "time_advance_request", 15.0)
            await _barrier(sender, logs, "new-regulator-message-delivered")
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
            assert logs[1].events.index(messages[0]) < logs[1].events.index(grant)

    asyncio.run(exercise())


@pytest.mark.parametrize(
    "advance", ["time_advance_request", "flush_queue_request"], ids=["TAR", "FQR"]
)
def test_enable_constrained_waits_for_safe_peer_time(live_server, advance):  # noqa: F811
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attrs):
            await sender.enable_time_regulation(1.0)
            assert (await logs[0].wait(TimeRegulationEnabled)).time == 0.0
            await _grant(reader, logs[1], "time_advance_request", 10.0)
            mark = len(logs[1].events)
            await reader.enable_time_constrained()
            await _barrier(sender, logs, "unsafe-constraint-request-pending")
            state = await _state(reader)
            assert not state.time_constrained, "current=10 must wait while peer bound is 1"
            assert state.logical_time == 10.0
            assert _enabled(logs[1], TimeConstrainedEnabled, after=mark) == []
            with pytest.raises(RequestForTimeConstrainedPending):
                await reader.enable_time_constrained()
            with pytest.raises(RequestForTimeConstrainedPending):
                await getattr(reader, advance)(12.0)
            # Before engagement, sent TSO is still received as RO, even in the past.
            early = await _send(
                sender,
                logs[1],
                obj,
                attrs,
                "interaction",
                b"pending-is-ro",
                timestamp=5.0,
                order=OrderType.TIMESTAMP,
                handle=1101,
            )
            await _barrier(sender, logs, "pending-constraint-ro-delivered")
            messages = _messages(logs[1])
            assert len(messages) == 1
            _assert_message(
                messages[0], early, sender.handle, attrs, OrderType.TIMESTAMP, OrderType.RECEIVE
            )
            await _grant(sender, logs[0], "time_advance_request", 10.0)
            enabled = await logs[1].wait(TimeConstrainedEnabled, after=mark)
            assert enabled.time == 10.0
            assert logs[1].events.index(messages[0]) < logs[1].events.index(enabled, mark)
            await _barrier(sender, logs, "peer-made-constraint-safe")
            assert _enabled(logs[1], TimeConstrainedEnabled, after=mark) == [enabled]
            state = await _state(reader)
            assert state.time_constrained and state.logical_time == 10.0
            late = await _send(
                sender,
                logs[1],
                obj,
                attrs,
                "interaction",
                b"enabled-is-tso",
                timestamp=12.0,
                order=OrderType.TIMESTAMP,
                handle=1102,
            )
            await reader.enable_asynchronous_delivery()
            await _ro_control(sender, logs, b"enabled-ro-control")
            assert [event.tag for event in _messages(logs[1])] == [
                b"pending-is-ro",
                b"enabled-ro-control",
            ]
            await sender.modify_lookahead(20.0)
            grant = await _grant(reader, logs[1], "time_advance_request", 12.0)
            await _barrier(sender, logs, "safe-constraint-tso-delivered")
            delivered = [event for event in _messages(logs[1]) if event.tag == late[2]]
            assert len(delivered) == 1
            _assert_message(
                delivered[0], late, sender.handle, attrs, OrderType.TIMESTAMP, OrderType.TIMESTAMP
            )
            assert logs[1].events.index(delivered[0]) < logs[1].events.index(grant)

    asyncio.run(exercise())


def test_enable_regulation_for_constrained_federate_performs_implicit_available_advance(
    live_server,  # noqa: F811
):
    async def exercise():
        async with _participants(live_server) as (sender, reader, logs, obj, attrs):
            await sender.enable_time_regulation(1.0)
            assert (await logs[0].wait(TimeRegulationEnabled)).time == 0.0
            await sender.enable_time_constrained()
            assert (await logs[0].wait(TimeConstrainedEnabled)).time == 0.0
            await reader.enable_time_constrained()
            assert (await logs[1].wait(TimeConstrainedEnabled)).time == 0.0
            early = await _send(
                sender,
                logs[1],
                obj,
                attrs,
                "interaction",
                b"implicit-advance-early",
                timestamp=5.0,
                order=OrderType.TIMESTAMP,
                handle=1201,
            )
            await _send(
                sender,
                logs[1],
                obj,
                attrs,
                "interaction",
                b"implicit-advance-future",
                timestamp=15.0,
                order=OrderType.TIMESTAMP,
                handle=1202,
            )
            await sender.send_interaction(
                BASE_SIGNAL, {"Payload": b"queued-ro"}, tag=b"implicit-ro"
            )
            await _barrier(sender, logs, "implicit-advance-queue-ready")
            assert _messages(logs[1]) == []
            await _grant(sender, logs[0], "time_advance_request", 10.0)
            mark = len(logs[1].events)
            await reader.enable_time_regulation(2.0)
            enabled = await logs[1].wait(TimeRegulationEnabled, after=mark)
            await _barrier(sender, logs, "implicit-advance-completed")
            state = await _state(reader)
            assert state.time_regulating and state.time_constrained
            assert state.logical_time + 2.0 > 10.0
            assert state.logical_time == math.nextafter(8.0, math.inf)
            assert enabled.time == state.logical_time
            assert _enabled(logs[1], TimeRegulationEnabled, after=mark) == [enabled]
            assert _grants(logs[1], after=mark) == [], (
                "activation completes via its enabled callback"
            )
            messages = _messages(logs[1])
            assert sorted(event.tag for event in messages) == [
                b"implicit-advance-early",
                b"implicit-ro",
            ]
            event = next(event for event in messages if event.tag == early[2])
            _assert_message(
                event, early, sender.handle, attrs, OrderType.TIMESTAMP, OrderType.TIMESTAMP
            )
            assert all(
                logs[1].events.index(event, mark) < logs[1].events.index(enabled, mark)
                for event in messages
            )
            await reader.modify_lookahead(30.0)
            await _grant(sender, logs[0], "time_advance_request", 20.0)
            await _grant(reader, logs[1], "time_advance_request", 16.0)
            await _barrier(sender, logs, "implicit-future-delivered-on-real-advance")
            assert sorted(event.tag for event in _messages(logs[1])) == [
                b"implicit-advance-early",
                b"implicit-advance-future",
                b"implicit-ro",
            ]

    asyncio.run(exercise())
