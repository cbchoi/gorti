"""IEEE 1516.1-2010 9.8, PDF 238-239: additive regional subscriptions.

Uses the existing opt-in production server fixture; no C++ dependency.
"""

from __future__ import annotations

import asyncio
import os
from contextlib import AsyncExitStack, asynccontextmanager
from xml.etree import ElementTree as ET

import grpc
import pytest

from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.ddm import AttributeRegions
from rti1516e.errors import InvalidRegion, RegionNotCreatedByThisFederate, RTIinternalError
from rti1516e.events import (
    DiscoverObjectInstance,
    FederationRestored,
    FederationSaved,
    InitiateFederateRestore,
    InitiateFederateSave,
    ReflectAttributeValues,
    TurnUpdatesOffForObjectInstance,
    TurnUpdatesOnForObjectInstance,
)
from tests import test_live_service_e2e as live_fixtures
from tests.test_live_service_e2e import VEHICLE, LiveCallbacks

live_artifacts = live_fixtures.live_artifacts
live_server = live_fixtures.live_server

pytestmark = pytest.mark.skipif(
    os.environ.get("GORTI_RUN_LIVE_E2E") != "1", reason="production localhost tests are opt-in"
)


@asynccontextmanager
async def _regional_federates(live_server, name):
    url, base, _ = live_server
    ns = "{http://standards.ieee.org/IEEE1516-2010}"
    root = ET.parse(base).getroot()  # noqa: S314 - locally generated test fixture
    attribute = root.find(f".//{ns}attribute")
    assert attribute is not None
    ET.SubElement(ET.SubElement(attribute, f"{ns}dimensions"), f"{ns}dimension").text = "X"
    dimension = ET.SubElement(ET.SubElement(root, f"{ns}dimensions"), f"{ns}dimension")
    ET.SubElement(dimension, f"{ns}name").text = "X"
    ET.SubElement(dimension, f"{ns}upperBound").text = "100"
    rates = root.find(f"{ns}updateRates")
    assert rates is not None
    rate = ET.SubElement(rates, f"{ns}updateRate")
    ET.SubElement(rate, f"{ns}name").text = "Fast"
    ET.SubElement(rate, f"{ns}rate").text = "10"
    path = base.with_name(f"{name}.xml")
    ET.ElementTree(root).write(path, encoding="utf-8", xml_declaration=True)
    async with asyncio.timeout(70), AsyncExitStack() as stack:
        left = await stack.enter_async_context(RtiConnection.connect(url))
        right = await stack.enter_async_context(RtiConnection.connect(url))
        spec = FederationSpec(name, [str(path)])
        publisher = await stack.enter_async_context(
            left.join_federation(spec, federate_name="publisher")
        )
        subscriber = await stack.enter_async_context(
            right.join_federation(spec, federate_name="subscriber")
        )
        pub_log, sub_log = LiveCallbacks(publisher), LiveCallbacks(subscriber)
        stack.push_async_callback(pub_log.close)
        stack.push_async_callback(sub_log.close)
        await publisher.publish_object_class(VEHICLE, attributes=["Value"])
        cls = await publisher.support.get_object_class_handle(VEHICLE)
        attr = await publisher.support.get_attribute_handle(cls, "Value")
        space = await publisher.ddm.lookup_routing_space("default")
        dim = await publisher.ddm.lookup_dimension(space, "X")

        async def region(federate, low, high):
            handle = await federate.ddm.create_region(space, [dim])
            await federate.ddm.set_range_bounds(handle, dim, lower=low, upper=high)
            await federate.ddm.commit_region_modifications([handle])
            return handle

        r1 = await region(subscriber, 0, 10)
        r2 = await region(subscriber, 20, 30)
        objects, pub_regions = [], []
        for low, high in ((0, 10), (20, 30), (0, 30)):
            pr = await region(publisher, low, high)
            obj = await publisher.register_object_instance(VEHICLE)
            await publisher.ddm.associate_regions_for_updates(obj, [AttributeRegions(attr, [pr])])
            objects.append(obj)
            pub_regions.append(pr)
        yield publisher, subscriber, pub_log, sub_log, cls, attr, (r1, r2), objects, pub_regions


async def _rates(publisher, objects, attr):
    rates = []
    for obj in objects:
        try:
            rates.append(await publisher.support.get_update_rate_value_for_attribute(obj, attr))
        except RTIinternalError as exc:
            # The current query reports absence instead of a numeric default.
            cause = exc.__cause__
            assert isinstance(cause, grpc.aio.AioRpcError)
            assert cause.code() == grpc.StatusCode.INTERNAL
            assert cause.details() == "update-rate value unavailable: no named active demand"
            rates.append(None)
    return rates


def test_ieee2010_ddm_additive_delivery_and_ordinary_independence(live_server):
    async def exercise():
        async with _regional_federates(live_server, "ddm-additive") as setup:
            a, b, pub_log, sub_log, cls, attr, (r1, r2), objects, pub_regions = setup
            subscribe = b.ddm.subscribe_object_class_attributes_with_regions
            await subscribe(cls, [attr], [r1], update_rate_designator="Slow")
            await pub_log.wait(
                TurnUpdatesOnForObjectInstance, lambda e: e.object_handle == objects[0]
            )
            await subscribe(cls, [attr], [r2], update_rate_designator="Slow")
            assert await _rates(a, objects, attr) == [2.5, 2.5, 2.5]
            for obj in objects:
                tag = f"additive-{obj}".encode()
                await a.update_attributes(
                    obj, {"Value": b"\x00\x00\x00\x00\x00\x00\xf0\x3f"}, tag=tag
                )
                await sub_log.wait(
                    DiscoverObjectInstance, lambda e, obj=obj: e.object_handle == obj
                )
                event = await sub_log.wait(ReflectAttributeValues, lambda e, tag=tag: e.tag == tag)
                assert event.object_handle == obj
                assert event.attribute_values == {attr: b"\x00\x00\x00\x00\x00\x00\xf0\x3f"}
            for invalid, error in (
                (999999, InvalidRegion),
                (pub_regions[0], RegionNotCreatedByThisFederate),
            ):
                with pytest.raises(error):
                    await subscribe(cls, [attr], [r1, invalid], active=False)
                assert await _rates(a, objects, attr) == [2.5, 2.5, 2.5]
            await subscribe(cls, [attr], [], active=False, update_rate_designator="Fast")
            await subscribe(cls, [attr, attr], [r2, r2], update_rate_designator="Slow")
            assert await _rates(a, objects, attr) == [2.5, 2.5, 2.5]
            await b.subscribe_object_class(
                VEHICLE, attributes=["Value"], update_rate_designator="Fast"
            )
            assert await _rates(a, objects, attr) == [10, 10, 10]
            await subscribe(cls, [attr], [r2], active=False)
            assert await _rates(a, objects, attr) == [10, 10, 10]
            await b.unsubscribe_object_class(cls, attributes=[attr])
            assert await _rates(a, objects, attr) == [2.5, None, 2.5]
            await subscribe(cls, [attr], [r2], update_rate_designator="Fast")
            assert await _rates(a, objects, attr) == [2.5, 10, 10]
            await b.ddm.unsubscribe_object_class_attributes_with_regions(cls, [attr], [r2])
            assert await _rates(a, objects, attr) == [2.5, None, 2.5]

    asyncio.run(exercise())


def test_ieee2010_ddm_per_region_options_save_restore(live_server):
    async def exercise():
        async with _regional_federates(live_server, "ddm-options") as setup:
            a, b, pub_log, sub_log, cls, attr, (r1, r2), objects, _ = setup
            subscribe = b.ddm.subscribe_object_class_attributes_with_regions
            await subscribe(cls, [attr], [r1], update_rate_designator="Slow")
            first = await pub_log.wait(
                TurnUpdatesOnForObjectInstance, lambda e: e.object_handle == objects[0]
            )
            assert first.update_rate_designator == "Slow"
            await subscribe(cls, [attr], [r2], active=False, update_rate_designator="Fast")
            assert await _rates(a, objects, attr) == [2.5, None, 2.5]
            await a.savepoint.request_federation_save("mixed-regions")
            for log in (pub_log, sub_log):
                await log.wait(InitiateFederateSave, lambda e: e.label == "mixed-regions")
            for fed in (a, b):
                await fed.savepoint.federate_save_begun()
                await fed.savepoint.federate_save_complete()
            for log in (pub_log, sub_log):
                await log.wait(FederationSaved, lambda e: e.label == "mixed-regions")
            await subscribe(cls, [attr], [r1], active=False)
            await subscribe(cls, [attr], [r2], update_rate_designator="Fast")
            assert await _rates(a, objects, attr) == [None, 10, 10]
            await a.savepoint.request_federation_restore("mixed-regions")
            for log in (pub_log, sub_log):
                await log.wait(InitiateFederateRestore, lambda e: e.label == "mixed-regions")
            for fed in (a, b):
                await fed.savepoint.federate_restore_complete()
            for log in (pub_log, sub_log):
                await log.wait(FederationRestored, lambda e: e.label == "mixed-regions")
            assert await _rates(a, objects, attr) == [2.5, None, 2.5]
            # Passive subscription still receives data; it contributes no rate demand.
            await a.update_attributes(objects[1], {"Value": b"passive"}, tag=b"restored-passive")
            event = await sub_log.wait(
                ReflectAttributeValues, lambda e: e.tag == b"restored-passive"
            )
            assert event.attribute_values == {attr: b"passive"}
            mark = len(pub_log.events)
            await subscribe(cls, [attr], [r2], update_rate_designator="Fast")
            on = await pub_log.wait(
                TurnUpdatesOnForObjectInstance, lambda e: e.object_handle == objects[1], after=mark
            )
            assert on.update_rate_designator == "Fast"
            assert await _rates(a, objects, attr) == [2.5, 10, 10]
            mark = len(pub_log.events)
            await subscribe(cls, [attr], [r2], active=False, update_rate_designator="Fast")
            await pub_log.wait(
                TurnUpdatesOffForObjectInstance, lambda e: e.object_handle == objects[1], after=mark
            )
            on = await pub_log.wait(
                TurnUpdatesOnForObjectInstance, lambda e: e.object_handle == objects[2], after=mark
            )
            assert on.update_rate_designator == "Slow"
            assert await _rates(a, objects, attr) == [2.5, None, 2.5]
            assert not any(
                isinstance(e, TurnUpdatesOffForObjectInstance) and e.object_handle == objects[0]
                for e in pub_log.events[mark:]
            )

    asyncio.run(exercise())
