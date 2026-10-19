"""W4 DDM behavior through a real server and same-process Python connections.

Sources: S1516.1 active 2010 text, 9.1.3-9.1.4 (PDF 224-225),
9.6.3(i)/9.8.3(h) (PDF 235/239), 9.10-9.11 (PDF 241-244), and
5.12-5.13 (PDF 105-106) for the observable interaction advisory state.
The source identity is in the 20260921 normative review. These tests use
independent RTI connections, not independent federate OS processes.

Existing live fixtures own the server/build and external artifacts. These cases
are listed in the HLA inventory and require no opt-in skip or shared SDK change.
"""

from __future__ import annotations

import asyncio
from contextlib import AsyncExitStack, asynccontextmanager
from xml.etree import ElementTree as ET

import pytest

from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.ddm import AttributeRegions
from rti1516e.declaration import AdvisorySwitchKind
from rti1516e.errors import InvalidRegion, InvalidRegionContext, RegionNotCreatedByThisFederate
from rti1516e.events import (
    ReceiveInteraction,
    ReflectAttributeValues,
    SynchronizationPointAnnounced,
    TurnInteractionsOff,
    TurnInteractionsOn,
)
from tests import test_live_service_e2e as live_fixtures
from tests.test_live_service_e2e import BASE_SIGNAL, VEHICLE, LiveCallbacks

live_artifacts = live_fixtures.live_artifacts
live_server = live_fixtures.live_server
pytestmark = [pytest.mark.spec, pytest.mark.integration]


@asynccontextmanager
async def _participants(server, *, dimension_default="Excluded"):
    url, base, _ = server
    ns = "{http://standards.ieee.org/IEEE1516-2010}"
    root = ET.parse(base).getroot()  # noqa: S314 - locally generated fixture
    attribute = root.find(f".//{ns}attribute")
    interaction = next(
        node
        for node in root.iter(f"{ns}interactionClass")
        if node.findtext(f"{ns}name") == "BaseSignal"
    )
    assert attribute is not None
    for node in (attribute, interaction):
        dimensions = ET.SubElement(node, f"{ns}dimensions")
        for name in ("X", "Y"):
            ET.SubElement(dimensions, f"{ns}dimension").text = name
    dimensions = ET.SubElement(root, f"{ns}dimensions")
    for name in ("X", "Y", "Z"):
        dimension = ET.SubElement(dimensions, f"{ns}dimension")
        ET.SubElement(dimension, f"{ns}name").text = name
        ET.SubElement(dimension, f"{ns}upperBound").text = "100"
        if dimension_default is not None:
            ET.SubElement(dimension, f"{ns}value").text = dimension_default
    fom = base.with_name("ieee2010-ddm-followup.xml")
    ET.ElementTree(root).write(fom, encoding="utf-8", xml_declaration=True)

    async with asyncio.timeout(50), AsyncExitStack() as stack:
        left = await stack.enter_async_context(RtiConnection.connect(url))
        right = await stack.enter_async_context(RtiConnection.connect(url))
        spec = FederationSpec("ieee2010-ddm-followup", [str(fom)])
        sender = await stack.enter_async_context(left.join_federation(spec, federate_name="sender"))
        receiver = await stack.enter_async_context(
            right.join_federation(spec, federate_name="receiver")
        )
        sent, received = LiveCallbacks(sender), LiveCallbacks(receiver)
        stack.push_async_callback(sent.close)
        stack.push_async_callback(received.close)
        assert left.transport is not right.transport
        await sender.publish_interaction_class(BASE_SIGNAL)
        await sender.publish_object_class(VEHICLE, attributes=["Value"])
        await sender.set_advisory_switch(AdvisorySwitchKind.INTERACTION_RELEVANCE, False)
        await sender.set_advisory_switch(AdvisorySwitchKind.INTERACTION_RELEVANCE, True)
        signal = await sender.support.get_interaction_class_handle(BASE_SIGNAL)
        parameter = await sender.support.get_parameter_handle(signal, "Payload")
        cls = await sender.support.get_object_class_handle(VEHICLE)
        attr = await sender.support.get_attribute_handle(cls, "Value")
        space = await sender.ddm.lookup_routing_space("default")
        handles = {name: await sender.ddm.lookup_dimension(space, name) for name in ("X", "Y", "Z")}
        assert 0 not in handles.values() and len(set(handles.values())) == 3

        async def region(federate, bounds):
            handle = await federate.ddm.create_region(space, [handles[name] for name in bounds])
            for name, (lower, upper) in bounds.items():
                await federate.ddm.set_range_bounds(handle, handles[name], lower=lower, upper=upper)
            await federate.ddm.commit_region_modifications([handle])
            return handle

        await _barrier(sender, sent, received, "setup-complete")
        yield sender, receiver, sent, received, signal, parameter, cls, attr, region


async def _barrier(sender, sent, received, label):
    await sender.sync.register_synchronization_point(label)
    for callbacks in (sent, received):
        await callbacks.wait(SynchronizationPointAnnounced, lambda event: event.label == label)


def _advisories(callbacks, signal, after):
    return [
        event
        for event in callbacks.events[after:]
        if isinstance(event, (TurnInteractionsOn, TurnInteractionsOff))
        and event.interaction_class_handle == signal
    ]


async def _probe_interactions(sender, sent, received, signal, parameter, cases, label):
    after = len(received.events)
    expected = set()
    for name, regions, delivered in cases:
        tag = f"{label}:{name}".encode()
        if delivered:
            expected.add(tag)
        await sender.ddm.send_interaction_with_regions(signal, {parameter: tag}, regions, tag=tag)
    await _barrier(sender, sent, received, label)
    deliveries = [
        event for event in received.events[after:] if isinstance(event, ReceiveInteraction)
    ]
    assert {event.tag for event in deliveries} == expected
    assert len(deliveries) == len(expected), "overlapping regions must not duplicate a recipient"
    for event in deliveries:
        assert event.parameters == {"Payload": event.tag}
        assert event.timestamp is None


@pytest.mark.parametrize("subset_side", ["subscription", "send", "no-shared-axis"])
def test_interaction_proper_subset_dimensions_and_shared_axis_routing(live_server, subset_side):
    async def exercise():
        async with _participants(live_server) as setup:
            a, b, sent, received, signal, parameter, _, _, region = setup
            bounds = {"X": (10, 20)}
            if subset_side == "send":
                bounds["Y"] = (80, 90)
                vectors = [
                    ("x-hit", {"X": (12, 18)}, True),
                    ("x-miss", {"X": (40, 50)}, False),
                    ("y-hit", {"Y": (82, 88)}, True),
                    ("y-miss", {"Y": (20, 30)}, False),
                ]
            elif subset_side == "subscription":
                vectors = [
                    ("xy-hit", {"X": (12, 18), "Y": (80, 90)}, True),
                    ("xy-miss", {"X": (40, 50), "Y": (80, 90)}, False),
                ]
            else:
                vectors = [("y-only", {"Y": (10, 20)}, False), ("x-hit", bounds, True)]
            subscribed = await region(b, bounds)
            await b.ddm.subscribe_interaction_class_with_regions(signal, [subscribed])
            cases = [(name, [await region(a, ranges)], hit) for name, ranges, hit in vectors]
            await _probe_interactions(a, sent, received, signal, parameter, cases, "subset")

    asyncio.run(exercise())


@pytest.mark.parametrize(
    ("dimension_default", "y_only_delivered"),
    [(None, True), ("[0..100)", True), ("Excluded", False), ("[40..50)", False)],
    ids=["legacy-omitted-full", "explicit-full", "explicit-excluded", "explicit-narrow"],
)
def test_disjoint_specification_axes_use_contextual_defaults(
    live_server, dimension_default, y_only_delivered
):
    """9.1.3.1(f)(2), PDF 222: realize default ranges before testing overlap.

    The omitted value covers the existing product fallback, not a normative
    default for an incomplete FDD. Explicit values cover declared defaults.
    """
    async def exercise():
        async with _participants(live_server, dimension_default=dimension_default) as setup:
            a, b, sent, received, signal, parameter, _, _, region = setup
            subscribed = await region(b, {"X": (10, 20)})
            await b.ddm.subscribe_interaction_class_with_regions(signal, [subscribed])
            y_only = await region(a, {"Y": (10, 20)})
            x_hit = await region(a, {"X": (12, 18)})
            x_miss = await region(a, {"X": (40, 50)})
            await _probe_interactions(
                a, sent, received, signal, parameter,
                [("y-only", [y_only], y_only_delivered),
                 ("x-hit", [x_hit], True), ("x-miss", [x_miss], False)],
                "contextual-defaults",
            )

    asyncio.run(exercise())


@pytest.mark.parametrize("subset_side", ["subscription", "update"])
def test_object_proper_subset_dimensions_route_attribute_updates(live_server, subset_side):
    async def exercise():
        async with _participants(live_server) as setup:
            a, b, sent, received, _, _, cls, attr, region = setup
            subscribed_bounds = {"X": (10, 20)}
            if subset_side == "update":
                subscribed_bounds["Y"] = (80, 90)
            subscribed = await region(b, subscribed_bounds)
            await b.ddm.subscribe_object_class_attributes_with_regions(cls, [attr], [subscribed])
            objects = {}
            for name, x_bounds in (("hit", (12, 18)), ("miss", (40, 50))):
                bounds = {"X": x_bounds}
                if subset_side == "subscription":
                    bounds["Y"] = (80, 90)
                published = await region(a, bounds)
                obj = await a.ddm.register_object_instance_with_regions(
                    cls,
                    [AttributeRegions(attr, [published])],
                )
                objects[name] = obj
                await a.update_attributes(obj, {"Value": name.encode()}, tag=name.encode())
            await _barrier(a, sent, received, "object-subsets")
            reflections = [
                event for event in received.events if isinstance(event, ReflectAttributeValues)
            ]
            assert len(reflections) == 1
            assert reflections[0].object_handle == objects["hit"]
            assert reflections[0].tag == b"hit"
            assert reflections[0].attribute_values == {attr: b"hit"}

    asyncio.run(exercise())


def test_interaction_regions_add_options_independently_and_unsubscribe_only_target(live_server):
    async def exercise():
        async with _participants(live_server) as setup:
            a, b, sent, received, signal, parameter, _, _, region = setup
            bounds = ({"X": (10, 20), "Y": (10, 20)}, {"X": (70, 80), "Y": (10, 20)})
            r1, r2 = [await region(b, value) for value in bounds]
            p1, p2 = [await region(a, value) for value in bounds]
            wide = await region(a, {"X": (0, 90), "Y": (0, 90)})
            assert len({r1, r2, p1, p2, wide}) == 5
            subscribe = b.ddm.subscribe_interaction_class_with_regions
            unsubscribe = b.ddm.unsubscribe_interaction_class_with_regions
            mark = len(sent.events)
            await subscribe(signal, [r1], active=True)
            await sent.wait(
                TurnInteractionsOn,
                lambda event: event.interaction_class_handle == signal,
                after=mark,
            )
            mark = len(sent.events)
            await subscribe(signal, [r2], active=False)
            await _probe_interactions(
                a,
                sent,
                received,
                signal,
                parameter,
                [
                    ("first-active", [p1], True),
                    ("second-passive", [p2], True),
                    ("both", [wide], True),
                    ("multi-region", [p1, p2], True),
                ],
                "additive",
            )
            assert _advisories(sent, signal, mark) == [], "passive addition erased active region"

            await subscribe(signal, [], active=False)
            await subscribe(signal, [r2, r2], active=False)
            await unsubscribe(signal, [])
            await _probe_interactions(
                a,
                sent,
                received,
                signal,
                parameter,
                [
                    ("first", [p1], True),
                    ("second", [p2], True),
                ],
                "empty-and-duplicate",
            )
            assert _advisories(sent, signal, mark) == []

            mark = len(sent.events)
            await subscribe(signal, [r1], active=False)
            await sent.wait(
                TurnInteractionsOff,
                lambda event: event.interaction_class_handle == signal,
                after=mark,
            )
            await _probe_interactions(
                a,
                sent,
                received,
                signal,
                parameter,
                [
                    ("first-passive", [p1], True),
                    ("second-passive", [p2], True),
                ],
                "all-passive",
            )
            mark = len(sent.events)
            await subscribe(signal, [r2], active=True)
            await sent.wait(
                TurnInteractionsOn,
                lambda event: event.interaction_class_handle == signal,
                after=mark,
            )
            mark = len(sent.events)
            await unsubscribe(signal, [r1])
            await _probe_interactions(
                a,
                sent,
                received,
                signal,
                parameter,
                [
                    ("removed", [p1], False),
                    ("retained", [p2], True),
                ],
                "targeted-unsubscribe",
            )
            assert _advisories(sent, signal, mark) == [], "retained active region lost its options"
            await b.ddm.delete_region(r1)
            mark = len(sent.events)
            await unsubscribe(signal, [r2])
            await sent.wait(
                TurnInteractionsOff,
                lambda event: event.interaction_class_handle == signal,
                after=mark,
            )
            await _probe_interactions(
                a,
                sent,
                received,
                signal,
                parameter,
                [
                    ("first", [p1], False),
                    ("second", [p2], False),
                ],
                "unsubscribed",
            )
            await b.ddm.delete_region(r2)

    asyncio.run(exercise())


@pytest.mark.parametrize(
    ("operation", "invalid_kind", "error"),
    [
        ("subscribe", "missing", InvalidRegion),
        ("subscribe", "foreign", RegionNotCreatedByThisFederate),
        ("subscribe", "outside-dimensions", InvalidRegionContext),
        ("unsubscribe", "missing", InvalidRegion),
        ("unsubscribe", "foreign", RegionNotCreatedByThisFederate),
    ],
    ids=[
        "subscribe-missing",
        "subscribe-foreign",
        "subscribe-context",
        "unsubscribe-missing",
        "unsubscribe-foreign",
    ],
)
def test_invalid_mixed_region_call_preserves_subscription_and_active_option(
    live_server,
    operation,
    invalid_kind,
    error,
):
    async def exercise():
        async with _participants(live_server) as setup:
            a, b, sent, received, signal, parameter, _, _, region = setup
            first = {"X": (10, 20), "Y": (10, 20)}
            second = {"X": (70, 80), "Y": (10, 20)}
            r1, r2 = await region(b, first), await region(b, second)
            p1, p2 = await region(a, first), await region(a, second)
            invalid = {"missing": 999999, "foreign": p1}.get(invalid_kind)
            if invalid is None:
                invalid = await region(b, {"Z": (10, 20)})
            mark = len(sent.events)
            await b.ddm.subscribe_interaction_class_with_regions(signal, [r1], active=True)
            await sent.wait(
                TurnInteractionsOn,
                lambda event: event.interaction_class_handle == signal,
                after=mark,
            )
            mark = len(sent.events)
            with pytest.raises(error):
                if operation == "subscribe":
                    await b.ddm.subscribe_interaction_class_with_regions(
                        signal,
                        [r1, r2, invalid],
                        active=False,
                    )
                else:
                    await b.ddm.unsubscribe_interaction_class_with_regions(
                        signal, [r1, r2, invalid]
                    )
            await _probe_interactions(
                a,
                sent,
                received,
                signal,
                parameter,
                [
                    ("still-subscribed", [p1], True),
                    ("never-subscribed", [p2], False),
                ],
                "after-rejection",
            )
            assert _advisories(sent, signal, mark) == [], "invalid call changed active interest"
            mark = len(sent.events)
            await b.ddm.unsubscribe_interaction_class_with_regions(signal, [r1])
            await sent.wait(
                TurnInteractionsOff,
                lambda event: event.interaction_class_handle == signal,
                after=mark,
            )
            await _probe_interactions(
                a,
                sent,
                received,
                signal,
                parameter,
                [
                    ("removed", [p1], False),
                    ("still-absent", [p2], False),
                ],
                "after-valid-unsubscribe",
            )

    asyncio.run(exercise())
