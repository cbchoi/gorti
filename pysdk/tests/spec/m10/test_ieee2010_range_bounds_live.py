"""IEEE 1516.1-2010 10.30.3(h-i), through both Python service surfaces."""

from __future__ import annotations

import asyncio
import os
from xml.etree import ElementTree as ET

import grpc
import pytest

from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.errors import InvalidRangeBound
from rti1516e.standard import Rti1516eAmbassador
from tests.test_live_service_e2e import live_artifacts, live_server  # noqa: F401

pytestmark = [
    pytest.mark.spec,
    pytest.mark.integration,
    pytest.mark.skipif(
        os.environ.get("GORTI_RUN_LIVE_E2E") != "1",
        reason="production localhost tests are opt-in",
    ),
]

MAXIMUM = (1 << 64) - 1


def _fom(base, limit):
    ns = "{http://standards.ieee.org/IEEE1516-2010}"
    root = ET.parse(base).getroot()  # noqa: S314 - locally generated fixture
    dimensions = ET.SubElement(root, f"{ns}dimensions")
    for name, upper in (("X", limit), ("Y", 200)):
        dimension = ET.SubElement(dimensions, f"{ns}dimension")
        ET.SubElement(dimension, f"{ns}name").text = name
        ET.SubElement(dimension, f"{ns}upperBound").text = str(upper)
    path = base.with_name(f"range-bounds-{limit}.xml")
    ET.ElementTree(root).write(path, encoding="utf-8", xml_declaration=True)
    return path


def _invalid(limit):
    bounds = [(0, 0), (50, 50), (limit, limit), (2, 1), (MAXIMUM, 0)]
    if limit < MAXIMUM:
        bounds.extend([(0, limit + 1), (limit, MAXIMUM)])
    return bounds


def _assert_wire_error(caught):
    assert type(caught.value) is InvalidRangeBound
    cause = caught.value.__cause__
    assert cause.code() == grpc.StatusCode.INVALID_ARGUMENT
    assert dict(cause.trailing_metadata())["rti-spec-exception"] == "InvalidRangeBound"


@pytest.mark.parametrize("limit", [100, MAXIMUM], ids=["fom-limit", "uint64-limit"])
def test_ieee2010_async_range_bounds(live_server, limit):  # noqa: F811
    url, base, _ = live_server

    async def exercise():
        async with asyncio.timeout(30), RtiConnection.connect(url) as connection:
            spec = FederationSpec("async-range-bounds", [str(_fom(base, limit))])
            async with connection.join_federation(spec, federate_name="owner") as federate:
                service = federate.ddm
                space = await service.lookup_routing_space("default")
                x = await service.lookup_dimension(space, "X")
                y = await service.lookup_dimension(space, "Y")
                assert await federate.support.get_dimension_handle("X") == x
                assert await federate.support.get_dimension_upper_bound(x) == limit
                assert await federate.support.get_dimension_handle("Y") == y
                assert await federate.support.get_dimension_upper_bound(y) == 200
                region = await service.create_region(space, [x, y])
                await service.set_range_bounds(region, x, lower=0, upper=limit)
                await service.set_range_bounds(region, y, lower=0, upper=200)
                await service.commit_region_modifications([region])
                await service.set_range_bounds(region, x, lower=0, upper=limit)
                await service.set_range_bounds(region, y, lower=0, upper=200)
                await service.commit_region_modifications([region])
                for pending in (False, True):
                    before = await service.query_bounds(region, x)
                    if pending:
                        await service.set_range_bounds(region, x, lower=10, upper=20)
                        await service.set_range_bounds(region, y, lower=30, upper=40)
                    for lower, upper in _invalid(limit):
                        with pytest.raises(InvalidRangeBound) as caught:
                            await service.set_range_bounds(region, x, lower=lower, upper=upper)
                        _assert_wire_error(caught)
                        assert await service.query_bounds(region, x) == before
                    await service.commit_region_modifications([region])
                    expected_x = (10, 20) if pending else before
                    expected_y = (30, 40) if pending else (0, 200)
                    assert await service.query_bounds(region, x) == expected_x
                    assert await service.query_bounds(region, y) == expected_y
                for lower, upper in ((0, 1), (0, limit), (limit - 1, limit)):
                    before = await service.query_bounds(region, x)
                    await service.set_range_bounds(region, x, lower=lower, upper=upper)
                    assert await service.query_bounds(region, x) == before
                    await service.commit_region_modifications([region])
                    assert await service.query_bounds(region, x) == (lower, upper)

    asyncio.run(exercise())


@pytest.mark.parametrize("limit", [100, MAXIMUM], ids=["fom-limit", "uint64-limit"])
def test_ieee2010_ambassador_range_bounds(live_server, limit):  # noqa: F811
    url, base, _ = live_server
    ambassador = Rti1516eAmbassador()
    ambassador.connect(ambassador, url)
    try:
        ambassador.createFederationExecution("ambassador-range-bounds", [str(_fom(base, limit))])
        ambassador.joinFederationExecution("owner", "ambassador-range-bounds")
        try:
            x = ambassador.getDimensionHandle("X")
            assert ambassador.getDimensionUpperBound(x) == limit
            region = ambassador.createRegion(1, [x])  # The 2010 flat model has one implicit space.
            ambassador.setRangeBounds(region, x, 0, limit)
            ambassador.commitRegionModifications([region])
            ambassador.setRangeBounds(region, x, 0, limit)
            ambassador.commitRegionModifications([region])
            ambassador.setRangeBounds(region, x, 10, 20)
            for lower, upper in _invalid(limit):
                with pytest.raises(InvalidRangeBound) as caught:
                    ambassador.setRangeBounds(region, x, lower, upper)
                _assert_wire_error(caught)
                assert ambassador.getRangeBounds(region, x) == (0, limit)
            ambassador.commitRegionModifications([region])
            assert ambassador.getRangeBounds(region, x) == (10, 20)
            ambassador.setRangeBounds(region, x, limit - 1, limit)
            ambassador.commitRegionModifications([region])
            assert ambassador.getRangeBounds(region, x) == (limit - 1, limit)
        finally:
            ambassador.resignFederationExecution()
    finally:
        ambassador.disconnect()
