import asyncio
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "rti1516e" / "_generated"))

from rti.v1 import stream_pb2

from rti1516e._transport import GrpcTransport, _object_callback_metadata
from rti1516e.events import TurnUpdatesOnForObjectInstance


def test_callback_presence_is_not_invented():
    old = stream_pb2.ReceiveInteraction(interaction_class_handle=1)
    assert _object_callback_metadata(old) is None
    old.metadata.sent_order = 1
    old.metadata.producing_federate = 4
    old.metadata.received_regions.SetInParent()
    got = _object_callback_metadata(old)
    assert got.sent_order == 1
    assert got.received_order is None
    assert got.message_retraction_handle is None
    assert got.received_regions == ()


def test_rate_callback_preserves_empty_designator_overload():
    transport = object.__new__(GrpcTransport)
    for rate in (None, "", "fast"):
        event = stream_pb2.FederateEvent()
        event.turn_updates_on.object_handle = 7
        event.turn_updates_on.attribute_handles.append(2)
        if rate is not None:
            event.turn_updates_on.update_rate_designator = rate
        result = transport._translate_event(event)
        assert isinstance(result, TurnUpdatesOnForObjectInstance)
        assert result.update_rate_designator == rate


def test_object_and_regional_send_forward_binary_tags_and_handles():
    from rti1516e.ddm import DDMClient

    calls = []

    class Stub:
        def __getattr__(self, name):
            async def call(request):
                calls.append((name, request))

            return call

    transport = object.__new__(GrpcTransport)
    transport._federation_name = "fed"
    transport.objects = Stub()
    regional = object.__new__(DDMClient)
    regional._federation_name = "fed"
    regional._federate_handle = 7
    regional._stub = Stub()
    tag = b"\x00\xfftag"

    async def exercise():
        await transport._send_interaction(7, 9, {1: b"v"}, 10, tag=tag, retraction_handle=41)
        await transport._update_attributes(7, 3, {1: b"v"}, 10, tag=tag, retraction_handle=42)
        await regional.send_interaction_with_regions(
            9, {1: b"v"}, [4], 10, tag=tag, retraction_handle=43
        )
        await transport.record(
            "change_attribute_order_type",
            federate_handle=7,
            object_handle=3,
            attribute_handles=[1],
            order_type=1,
        )
        await transport.record(
            "change_interaction_order_type",
            federate_handle=7,
            interaction_class_handle=9,
            order_type=2,
        )
        await transport.record("retract", federate_handle=7, retraction_handle=43)

    asyncio.run(exercise())
    for index, (_, request) in enumerate(calls[:3]):
        assert request.user_supplied_tag == tag
        assert request.message_retraction_handle == 41 + index
    assert calls[3][0] == "ChangeAttributeOrderType"
    assert calls[4][0] == "ChangeInteractionOrderType"
    assert calls[5][0] == "Retract"
    assert calls[5][1].message_retraction_handle == 43
