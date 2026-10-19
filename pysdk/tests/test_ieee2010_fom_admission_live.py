"""Bounded live FDD admission: IEEE 1516.1-2010 4.1.4/4.1.4.1, PDF 47-49."""

from __future__ import annotations

import asyncio
import copy
import os
import struct
import xml.etree.ElementTree as ET
from contextlib import AsyncExitStack
from pathlib import Path

import grpc
import pytest

from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.events import ReceiveInteraction

from .test_live_service_e2e import (
    BASE_SIGNAL,
    ROOT,
    VEHICLE,
    LiveCallbacks,
    live_artifacts,  # noqa: F401
    live_server,  # noqa: F401
)
from .test_query_mim_live import _unicode

pytestmark = pytest.mark.skipif(
    os.environ.get("GORTI_RUN_LIVE_E2E") != "1", reason="production localhost tests are opt-in"
)


def test_live_fom_member_admission_and_first_descriptions(live_server):  # noqa: F811
    url, fixture, _ = live_server
    ns = {"h": "http://standards.ieee.org/IEEE1516-2010"}
    prefix = "{" + ns["h"] + "}"
    object_path = "h:objects/h:objectClass/h:objectClass"
    interaction_path = "h:interactions/h:interactionClass/h:interactionClass"

    def element(root, path):
        result = root.find(path, ns)
        assert result is not None, path
        return result

    def write_module(name, root):
        path = fixture.with_name(name + ".xml")
        ET.ElementTree(root).write(path, encoding="utf-8", xml_declaration=True)
        return str(path)

    base = ET.parse(fixture).getroot()  # noqa: S314 - owned local fixture
    retained = ET.parse(  # noqa: S314 - checked-in exporter fixture
        ROOT / "rti/pkg/fom/exporter/testdata/retained.xml"
    ).getroot()
    base.append(copy.deepcopy(element(retained, "h:switches")))
    for path, text in ((object_path, "First vehicle"), (interaction_path, "First signal")):
        ET.SubElement(element(base, path), prefix + "semantics").text = text
    base_path = write_module("admission-base", base)
    rejected = []
    for name, path, member in (
        ("attribute-growth", object_path, "attribute"),
        ("parameter-growth", interaction_path, "parameter"),
    ):
        changed = copy.deepcopy(base)
        owner = element(changed, path)
        added = copy.deepcopy(element(owner, "h:" + member))
        element(added, "h:name").text = "ForbiddenMember"
        owner.append(added)
        rejected.append(write_module(name, changed))
    changed = copy.deepcopy(base)
    element(changed, object_path + "/h:attribute/h:order").text = "TimeStamp"
    rejected.append(write_module("operational-conflict", changed))

    compatible = copy.deepcopy(base)
    for path in (
        object_path + "/h:semantics",
        object_path + "/h:attribute/h:semantics",
        object_path + "/h:attribute/h:updateCondition",
        interaction_path + "/h:semantics",
        interaction_path + "/h:parameter/h:semantics",
    ):
        element(compatible, path).text = " Different description "
    for path, name, member, member_name in (
        (object_path, "ExtendedVehicle", "attribute", "ExtraValue"),
        (interaction_path, "ExtendedSignal", "parameter", "ExtraPayload"),
    ):
        owner = element(compatible, path)
        child = copy.deepcopy(owner)
        element(child, "h:name").text = name
        element(child, "h:" + member + "/h:name").text = member_name
        owner.append(child)
    compatible_path = write_module("compatible-subclasses", compatible)
    rejected_sources = [(path, Path(path).read_bytes()) for path in rejected]

    async def exercise():
        async with asyncio.timeout(50), AsyncExitStack() as stack:
            left = await stack.enter_async_context(RtiConnection.connect(url))
            right = await stack.enter_async_context(RtiConnection.connect(url))
            spec = FederationSpec("ieee2010-fom-admission", [base_path])
            observer = await stack.enter_async_context(
                left.join_federation(spec, federate_name="observer")
            )
            log = LiveCallbacks(observer)
            stack.push_async_callback(log.close)
            from rti.v1 import common_pb2, federation_pb2

            request = federation_pb2.GetFOMViewRequest(
                wire_version=1, federation_name=spec.name, federate_handle=observer.handle
            )
            before = await left.transport.federation.GetFOMView(request)
            vehicle = await observer.support.get_object_class_handle(VEHICLE)
            signal = await observer.support.get_interaction_class_handle(BASE_SIGNAL)
            value = await observer.support.get_attribute_handle(vehicle, "Value")
            payload = await observer.support.get_parameter_handle(signal, "Payload")
            generation = await right.transport._resolve_federation_generation(spec.name)
            for path, source in rejected_sources:
                # Observe server admission directly; the SDK's commit-unknown
                # classification of failed joins is a separate open obligation.
                with pytest.raises(grpc.aio.AioRpcError, match="FOM-014"):
                    await right.transport.federation.JoinFederation(
                        federation_pb2.JoinFederationRequest(
                            wire_version=1,
                            federation_name=spec.name,
                            federate_name="extender",
                            expected_federation_generation=generation,
                            additional_fom_modules=[
                                common_pb2.FOMModule(path=path, xml=source)
                            ],
                        )
                    )
                assert await left.transport.federation.GetFOMView(request) == before
                roster = await observer.mom.query_federation_attributes()
                assert roster.federate_handles == (observer.handle,)
            extender = await stack.enter_async_context(
                right.join_federation(
                    FederationSpec(spec.name, additional_fom_modules=[compatible_path]),
                    federate_name="extender",
                )
            )
            assert left.transport is not right.transport
            await observer.refresh_fom()
            after = await left.transport.federation.GetFOMView(request)
            assert after.revision == before.revision + 1
            assert len(after.modules) == len(before.modules) + 1
            for old_classes, new_classes in (
                (before.object_classes, after.object_classes),
                (before.interaction_classes, after.interaction_classes),
            ):
                by_name = {item.name: item for item in new_classes}
                for old in old_classes:
                    assert by_name[old.name] == old
            assert await observer.support.get_object_class_handle(VEHICLE) == vehicle
            assert await observer.support.get_interaction_class_handle(BASE_SIGNAL) == signal
            assert await observer.support.get_attribute_handle(vehicle, "Value") == value
            assert await observer.support.get_parameter_handle(signal, "Payload") == payload
            roster = await observer.mom.query_federation_attributes()
            assert set(roster.federate_handles) == {observer.handle, extender.handle}
            fdd = ET.fromstring(  # noqa: S314 - owned localhost server and FOMs
                _unicode(roster.attribute_values["HLAcurrentFDD"])
            )
            for kind, name, fields in (
                ("objectClass", "Vehicle", {
                    "h:semantics": "First vehicle",
                    "h:attribute/h:semantics": "Test value.",
                    "h:attribute/h:updateCondition": "NA",
                }),
                ("interactionClass", "BaseSignal", {
                    "h:semantics": "First signal",
                    "h:parameter/h:semantics": "Binary test payload.",
                }),
            ):
                owner = next(
                    item for item in fdd.iter(prefix + kind)
                    if element(item, "h:name").text == name
                )
                for path, expected in fields.items():
                    assert element(owner, path).text == expected
            extended = BASE_SIGNAL + ".ExtendedSignal"
            await observer.subscribe_interaction_class(extended)
            await extender.publish_interaction_class(extended)
            values = {"Payload": struct.pack(">d", 1.5), "ExtraPayload": struct.pack(">d", 2.5)}
            await extender.send_interaction(extended, values, tag=b"admitted-subclass")
            received = await log.wait(ReceiveInteraction)
            assert received.parameters == values
            assert received.tag == b"admitted-subclass"
            assert received.metadata.producing_federate == extender.handle

    asyncio.run(exercise())
