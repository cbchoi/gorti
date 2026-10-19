import asyncio
from types import SimpleNamespace
from unittest.mock import AsyncMock

from rti1516e._transport import GrpcTransport, _ensure_generated_path
from rti1516e.events import AttributeOwnershipAcquisitionNotification
from rti1516e.ownership import OwnershipClient
from rti1516e.standard import Rti1516eAmbassador

_ensure_generated_path()
from rti.v1 import stream_pb2  # noqa: E402

TAG = b"\x00\x80\xff"


def client():
    c = OwnershipClient.__new__(OwnershipClient)
    c._federation_name = "fed"
    c._federate_handle = 7
    c._stub = SimpleNamespace(
        ConfirmDivestiture=AsyncMock(),
        AttributeOwnershipDivestitureIfWantedWithResult=AsyncMock(
            return_value=SimpleNamespace(divested_attribute_handles=[9])
        ),
        QueryAttributeOwnership=AsyncMock(
            return_value=SimpleNamespace(owner_federate_handle=7, owned=True)
        ),
    )
    return c


def test_n04_confirm_tag_and_atomic_result():
    c = client()
    asyncio.run(c.confirm_divestiture(100, [9], tag=TAG))
    assert c._stub.ConfirmDivestiture.call_args.args[0].tag == TAG
    assert asyncio.run(c.divest_if_wanted_with_result(100, [9, 10])) == (9,)
    c._stub.QueryAttributeOwnership.assert_not_called()


def test_n04_query_supplies_caller():
    c = client()
    assert asyncio.run(c.query_attribute_ownership(100, 9)) == (7, True)
    assert c._stub.QueryAttributeOwnership.call_args.args[0].federate_handle == 7


def test_n04_acquisition_callback_binary_tag():
    wire = stream_pb2.FederateEvent(
        ownership_acquired=stream_pb2.AttributeOwnershipAcquisitionNotification(
            object_handle=100, attribute_handles=[9], owning_federate=7, tag=TAG
        )
    )
    event = GrpcTransport.__new__(GrpcTransport)._translate_event(wire)
    assert event.tag == TAG
    assert (event.object_handle, event.attribute_handles, event.owning_federate) == (100, (9,), 7)


def test_n04_tag_callback_preserves_legacy_override():
    class Legacy(Rti1516eAmbassador):
        def attributeOwnershipAcquisitionNotification(self, obj, attrs, owner):  # noqa: N802
            self.seen = (obj, attrs, owner)

    amb = Legacy()
    try:
        amb._dispatch_event(AttributeOwnershipAcquisitionNotification(100, (9,), 7, TAG))
        assert amb.seen == (100, (9,), 7)
    finally:
        amb.disconnect()


def test_n04_tag_callback_dispatch():
    class Tagged(Rti1516eAmbassador):
        def attributeOwnershipAcquisitionNotificationWithTag(  # noqa: N802
            self, obj, attrs, owner, tag
        ):
            self.seen = (obj, attrs, owner, tag)

    amb = Tagged()
    try:
        amb._dispatch_event(AttributeOwnershipAcquisitionNotification(100, (9,), 7, TAG))
        assert amb.seen == (100, (9,), 7, TAG)
    finally:
        amb.disconnect()
