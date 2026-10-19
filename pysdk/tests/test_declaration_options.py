"""Declaration options must survive the generated protobuf wire."""

import asyncio
from contextlib import asynccontextmanager
from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest

from rti1516e._transport import GrpcTransport, _ensure_generated_path
from rti1516e.standard import Rti1516eAmbassador

_ensure_generated_path()
from rti.v1 import declaration_pb2  # noqa: E402


@pytest.mark.asyncio
async def test_declaration_passive_wire_options():
    transport = object.__new__(GrpcTransport)
    transport._federation_name = "f"
    transport.declaration = SimpleNamespace(
        SubscribeObjectClassAttributes=AsyncMock(),
        SubscribeInteractionClass=AsyncMock(), SetAdvisorySwitch=AsyncMock(),
    )
    await transport._subscribe_object_class(3, 10, [1], active=False, update_rate_designator="slow")
    request = transport.declaration.SubscribeObjectClassAttributes.call_args.args[0]
    decoded = declaration_pb2.SubObjAttrsRequest.FromString(request.SerializeToString())
    assert decoded.HasField("active") and decoded.active is False
    await transport._set_advisory_switch(3, 3, False)
    request = transport.declaration.SetAdvisorySwitch.call_args.args[0]
    assert request.switch_kind == 3 and request.enabled is False
    assert decoded.update_rate_designator == "slow"
    await transport._subscribe_interaction(3, 20, active=False)
    request = transport.declaration.SubscribeInteractionClass.call_args.args[0]
    decoded = declaration_pb2.SubInterRequest.FromString(request.SerializeToString())
    assert decoded.HasField("active") and decoded.active is False


def test_declaration_standard_options_forwarding():
    federate = SimpleNamespace(
        subscribe_object_class=AsyncMock(), subscribe_interaction_class=AsyncMock(),
        set_advisory_switch=AsyncMock(),
        ddm=SimpleNamespace(subscribe_object_class_attributes_with_regions=AsyncMock(),
                            subscribe_interaction_class_with_regions=AsyncMock()),
    )
    ambassador = object.__new__(Rti1516eAmbassador)
    ambassador._fed = lambda: federate
    ambassador._run = asyncio.run
    ambassador.subscribeObjectClassAttributes(10, [1], active=False, updateRateDesignator="Slow")
    federate.subscribe_object_class.assert_awaited_once_with(
        10, attributes=[1], active=False, update_rate_designator="Slow"
    )
    ambassador.subscribeInteractionClass(20, active=False)
    federate.subscribe_interaction_class.assert_awaited_once_with(20, active=False)
    ambassador.disableAttributeScopeAdvisorySwitch()
    federate.set_advisory_switch.assert_awaited_once_with(3, False)
    ambassador.subscribeObjectClassAttributesWithRegions(10, [1], [2], False, "Slow")
    federate.ddm.subscribe_object_class_attributes_with_regions.assert_awaited_once_with(
        10, [1], [2], active=False, update_rate_designator="Slow"
    )
    ambassador.subscribeInteractionClassWithRegions(20, [2], False)
    federate.ddm.subscribe_interaction_class_with_regions.assert_awaited_once_with(
        20, [2], active=False
    )
    for kind, name in enumerate(
        ["ObjectClassRelevance", "AttributeRelevance", "AttributeScope", "InteractionRelevance"],
        start=1,
    ):
        for enabled, verb in [(True, "enable"), (False, "disable")]:
            getattr(ambassador, f"{verb}{name}AdvisorySwitch")()
            federate.set_advisory_switch.assert_awaited_with(kind, enabled)


@pytest.mark.asyncio
async def test_declaration_public_options_validation():
    from rti1516e.connection import Federate

    @asynccontextmanager
    async def admission():
        yield

    transport = SimpleNamespace(record=AsyncMock())
    fed = Federate(connection=SimpleNamespace(_admit_work=admission),
                   transport=transport, handle=3, name="alice")
    with pytest.raises(TypeError):
        await fed.subscribe_object_class("Vehicle", attributes=["pos"], active="false")
    with pytest.raises(TypeError):
        await fed.subscribe_object_class("Vehicle", attributes=["pos"],
                                         update_rate_designator=None)
    with pytest.raises(TypeError):
        await fed.subscribe_interaction_class("Signal", active=0)
    transport.record.assert_not_called()
    await fed.subscribe_object_class("Vehicle", attributes=["pos"], active=False,
                                     update_rate_designator="Slow")
    await fed.subscribe_interaction_class("Signal", active=False)
    assert transport.record.call_args_list[0].kwargs["active"] is False
    assert transport.record.call_args_list[0].kwargs["update_rate_designator"] == "Slow"
    assert transport.record.call_args_list[1].kwargs["active"] is False


@pytest.mark.asyncio
async def test_declaration_ddm_passive_wire_options():
    from rti.v1 import ddm_pb2

    from rti1516e.ddm import DDMClient

    client = object.__new__(DDMClient)
    client._federation_name = "f"
    client._federate_handle = 3
    client._stub = SimpleNamespace(
        SubscribeObjectClassAttributesWithRegions=AsyncMock(),
        SubscribeInteractionClassWithRegions=AsyncMock(),
    )
    await client.subscribe_object_class_attributes_with_regions(
        10, [1], [2], active=False, update_rate_designator="Slow"
    )
    req = client._stub.SubscribeObjectClassAttributesWithRegions.call_args.args[0]
    decoded = ddm_pb2.SubscribeOCAWithRegionsRequest.FromString(req.SerializeToString())
    assert decoded.HasField("active") and decoded.active is False
    assert decoded.update_rate_designator == "Slow"
    await client.subscribe_interaction_class_with_regions(20, [2], active=False)
    req = client._stub.SubscribeInteractionClassWithRegions.call_args.args[0]
    decoded = ddm_pb2.SubscribeICWithRegionsRequest.FromString(req.SerializeToString())
    assert decoded.HasField("active") and decoded.active is False
