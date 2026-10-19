"""Real-server DDM association activation, known-class mapping, and cleanup."""

from __future__ import annotations

import asyncio
import copy
import os
import struct
from contextlib import AsyncExitStack, asynccontextmanager
from xml.etree import ElementTree as ET

import pytest

from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.ddm import AttributeRegions
from rti1516e.errors import InvalidRegion, RegionInUseForUpdateOrSubscription
from rti1516e.events import (
    AttributeOwnershipAcquisitionNotification,
    DiscoverObjectInstance,
    ReflectAttributeValues,
    SynchronizationPointAnnounced,
)
from tests import test_live_service_e2e as live_fixtures
from tests.test_live_service_e2e import LiveCallbacks

live_artifacts = live_fixtures.live_artifacts
live_server = live_fixtures.live_server

pytestmark = pytest.mark.skipif(
    os.environ.get("GORTI_RUN_LIVE_E2E") != "1", reason="production localhost tests are opt-in"
)

_PARENT = "HLAobjectRoot.Base"
_CHILD = f"{_PARENT}.Child"
_POSITION = struct.pack(">d", 12.5)
_CHILD_VALUE = struct.pack(">d", 99.0)


def _lifecycle_fom(base, name):
    ns = "{http://standards.ieee.org/IEEE1516-2010}"
    root = ET.parse(base).getroot()  # noqa: S314 - locally generated test fixture
    objects = root.find(f"{ns}objects")
    assert objects is not None
    original = objects.find(f".//{ns}attribute")
    assert original is not None
    template = copy.deepcopy(original)
    objects.clear()
    object_root = ET.SubElement(objects, f"{ns}objectClass")
    ET.SubElement(object_root, f"{ns}name").text = "HLAobjectRoot"
    parent = ET.SubElement(object_root, f"{ns}objectClass")
    ET.SubElement(parent, f"{ns}name").text = "Base"
    ET.SubElement(parent, f"{ns}sharing").text = "PublishSubscribe"
    child = ET.SubElement(parent, f"{ns}objectClass")
    ET.SubElement(child, f"{ns}name").text = "Child"
    ET.SubElement(child, f"{ns}sharing").text = "PublishSubscribe"
    for cls, attribute_name in ((parent, "position"), (child, "childValue")):
        attribute = copy.deepcopy(template)
        name_element = attribute.find(f"{ns}name")
        assert name_element is not None
        name_element.text = attribute_name
        ET.SubElement(ET.SubElement(attribute, f"{ns}dimensions"), f"{ns}dimension").text = "X"
        cls.insert(2, attribute)
    dimension = ET.SubElement(ET.SubElement(root, f"{ns}dimensions"), f"{ns}dimension")
    ET.SubElement(dimension, f"{ns}name").text = "X"
    ET.SubElement(dimension, f"{ns}upperBound").text = "100"
    path = base.with_name(f"{name}.xml")
    ET.ElementTree(root).write(path, encoding="utf-8", xml_declaration=True)
    return path


async def _region(federate, low, high):
    space = await federate.ddm.lookup_routing_space("default")
    dimension = await federate.ddm.lookup_dimension(space, "X")
    region = await federate.ddm.create_region(space, [dimension])
    await federate.ddm.set_range_bounds(region, dimension, lower=low, upper=high)
    await federate.ddm.commit_region_modifications([region])
    return region


@asynccontextmanager
async def _participants(server, name):
    url, base, _ = server
    path = _lifecycle_fom(base, name)
    async with asyncio.timeout(90), AsyncExitStack() as stack:
        connections = [
            await stack.enter_async_context(RtiConnection.connect(url)) for _ in range(4)
        ]
        spec = FederationSpec(name, [str(path)])
        await connections[0].create_federation(spec)
        # Runs after all joined contexts resign, before their connections close.
        stack.push_async_callback(connections[0].destroy_federation, name)
        federates, logs = [], []
        for connection, member_name in zip(
            connections, ("owner", "acquirer", "near", "far"), strict=True
        ):
            federate = await stack.enter_async_context(
                connection.join_federation(spec, federate_name=member_name)
            )
            log = LiveCallbacks(federate)
            stack.push_async_callback(log.close)
            federates.append(federate)
            logs.append(log)
        owner, acquirer, near, far = federates
        parent = await owner.support.get_object_class_handle(_PARENT)
        child = await owner.support.get_object_class_handle(_CHILD)
        known_attr = await owner.support.get_attribute_handle(parent, "position")
        actual_attr = await owner.support.get_attribute_handle(child, "position")
        child_attr = await owner.support.get_attribute_handle(child, "childValue")
        assert known_attr != actual_attr, "fixture must remap inherited attribute handles"
        assert known_attr == child_attr, "fixture must exercise a numeric attribute collision"
        await owner.publish_object_class(_CHILD, attributes=["position", "childValue"])
        await acquirer.publish_object_class(_PARENT, attributes=["position"])
        await acquirer.subscribe_object_class(_PARENT, attributes=["position"])
        for observer, low, high in ((near, 0, 10), (far, 20, 30)):
            region = await _region(observer, low, high)
            await observer.ddm.subscribe_object_class_attributes_with_regions(
                child, [actual_attr, child_attr], [region]
            )
        owner_region = await _region(owner, 0, 10)
        child_region = await _region(owner, 20, 30)
        candidate_region = await _region(acquirer, 20, 30)
        obj = await owner.register_object_instance(_CHILD)
        for index, cls in ((1, parent), (2, child), (3, child)):
            discovered = await logs[index].wait(
                DiscoverObjectInstance, lambda event: event.object_handle == obj
            )
            assert int(discovered.object_class) == cls
            assert await federates[index].support.get_known_object_class_handle(obj) == cls
        await owner.ddm.associate_regions_for_updates(
            obj,
            [
                AttributeRegions(actual_attr, [owner_region]),
                AttributeRegions(child_attr, [child_region]),
            ],
        )
        yield (
            federates, logs, obj, (known_attr, actual_attr, child_attr),
            (owner_region, candidate_region, child_region),
        )


async def _fence(owner, logs, label):
    # A same-stream announcement fences earlier reliable reflection delivery.
    await owner.sync.register_synchronization_point(label)
    for log in logs:
        await log.wait(SynchronizationPointAnnounced, lambda event: event.label == label)


async def _update_and_expect(federates, logs, sender, obj, values, tag, near, far):
    marks = [len(log.events) for log in logs]
    await sender.update_attributes(obj, values, tag=tag)
    await _fence(federates[0], logs, tag.decode("ascii"))
    for index, expected in ((2, near), (3, far)):
        reflections = [
            event for event in logs[index].events[marks[index]:]
            if isinstance(event, ReflectAttributeValues)
            and event.object_handle == obj and event.tag == tag
        ]
        assert [event.attribute_values for event in reflections] == ([expected] if expected else [])
        assert logs[index].failure is None


async def _transfer(source, target, target_log, obj, source_attr, target_attr, tag):
    mark = len(target_log.events)
    await source.ownership.unconditional_divest(obj, [source_attr])
    await target.ownership.acquire(obj, [target_attr], tag=tag)
    acquired = await target_log.wait(
        AttributeOwnershipAcquisitionNotification,
        lambda event: event.object_handle == obj and event.tag == tag,
        after=mark,
    )
    assert acquired.attribute_handles == (target_attr,)
    assert acquired.owning_federate == target.handle
    assert await target.ownership.is_attribute_owned_by_federate(obj, target_attr)
    assert not await source.ownership.is_attribute_owned_by_federate(obj, source_attr)


@pytest.mark.parametrize(
    "attribute_keys", ["string", "numeric"], ids=["string-keys", "numeric-keys"]
)
def test_ddm_preacquisition_known_attribute_activation_and_loss(live_server, attribute_keys):
    async def exercise():
        async with _participants(live_server, "ddm-association-ownership") as setup:
            federates, logs, obj, attrs, regions = setup
            owner, acquirer, _, _ = federates
            known_attr, actual_attr, child_attr = attrs
            owner_position = "position" if attribute_keys == "string" else actual_attr
            acquirer_position = "position" if attribute_keys == "string" else known_attr
            child_value = "childValue" if attribute_keys == "string" else child_attr
            owner_region, candidate_region, _ = regions
            await acquirer.ddm.associate_regions_for_updates(
                obj, [AttributeRegions(known_attr, [candidate_region])]
            )
            assert not await acquirer.ownership.is_attribute_owned_by_federate(obj, known_attr)
            await _update_and_expect(
                federates, logs, owner, obj,
                {owner_position: _POSITION, child_value: _CHILD_VALUE}, b"inactive-association",
                {actual_attr: _POSITION}, {child_attr: _CHILD_VALUE},
            )
            await _transfer(owner, acquirer, logs[1], obj, actual_attr, known_attr, b"acquire")
            assert await owner.ownership.is_attribute_owned_by_federate(obj, child_attr)
            await _update_and_expect(
                federates, logs, acquirer, obj, {acquirer_position: _POSITION}, b"activated",
                {}, {actual_attr: _POSITION},
            )
            with pytest.raises(RegionInUseForUpdateOrSubscription):
                await acquirer.ddm.delete_region(candidate_region)
            await owner.ddm.delete_region(owner_region)
            await _transfer(acquirer, owner, logs[0], obj, known_attr, actual_attr, b"return")
            await _update_and_expect(
                federates, logs, owner, obj, {owner_position: _POSITION}, b"after-loss",
                {actual_attr: _POSITION}, {actual_attr: _POSITION},
            )
            # Keep the old region alive across reacquisition: an obsolete inactive
            # association must not reactivate simply because ownership returns.
            await _transfer(owner, acquirer, logs[1], obj, actual_attr, known_attr, b"reacquire")
            await _update_and_expect(
                federates, logs, acquirer, obj, {acquirer_position: _POSITION}, b"no-resurrection",
                {actual_attr: _POSITION}, {actual_attr: _POSITION},
            )
            await acquirer.ddm.delete_region(candidate_region)
            await _update_and_expect(
                federates, logs, owner, obj, {child_value: _CHILD_VALUE}, b"child-unchanged",
                {}, {child_attr: _CHILD_VALUE},
            )

    asyncio.run(exercise())


@pytest.mark.parametrize("cleanup", ["region-delete", "local-delete"])
@pytest.mark.parametrize(
    "attribute_keys", ["string", "numeric"], ids=["string-keys", "numeric-keys"]
)
def test_ddm_inactive_association_cleanup_before_acquisition(live_server, cleanup, attribute_keys):
    async def exercise():
        async with _participants(live_server, f"ddm-inactive-{cleanup}") as setup:
            federates, logs, obj, attrs, regions = setup
            owner, acquirer, _, _ = federates
            known_attr, actual_attr, child_attr = attrs
            owner_position = "position" if attribute_keys == "string" else actual_attr
            acquirer_position = "position" if attribute_keys == "string" else known_attr
            child_value = "childValue" if attribute_keys == "string" else child_attr
            _, candidate_region, _ = regions
            await acquirer.ddm.associate_regions_for_updates(
                obj, [AttributeRegions(known_attr, [candidate_region])]
            )
            if cleanup == "region-delete":
                await acquirer.ddm.delete_region(candidate_region)
                with pytest.raises(InvalidRegion):
                    await acquirer.ddm.delete_region(candidate_region)
            else:
                mark = len(logs[1].events)
                await acquirer.local_delete_object_instance(obj)
                await logs[1].wait(
                    DiscoverObjectInstance, lambda event: event.object_handle == obj, after=mark
                )
                parent = await acquirer.support.get_object_class_handle(_PARENT)
                assert await acquirer.support.get_known_object_class_handle(obj) == parent
            await _update_and_expect(
                federates, logs, owner, obj,
                {owner_position: _POSITION, child_value: _CHILD_VALUE}, b"cleanup-preserves-owner",
                {actual_attr: _POSITION}, {child_attr: _CHILD_VALUE},
            )
            await _transfer(
                owner, acquirer, logs[1], obj, actual_attr, known_attr, b"after-cleanup"
            )
            await _update_and_expect(
                federates, logs, acquirer, obj, {acquirer_position: _POSITION},
                b"cleanup-stays-default",
                {actual_attr: _POSITION}, {actual_attr: _POSITION},
            )
            if cleanup == "local-delete":
                # The still-existing region must not have become active after
                # local deletion, rediscovery, and the later ownership acquisition.
                await acquirer.ddm.delete_region(candidate_region)
            assert await owner.ownership.is_attribute_owned_by_federate(obj, child_attr)

    asyncio.run(exercise())
