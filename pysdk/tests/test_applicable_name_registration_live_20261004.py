"""Object-name preconditions and exceptions over the production connection."""

import asyncio
import os
from contextlib import AsyncExitStack

import pytest

from rti1516e import NameSetWasEmpty
from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.errors import ObjectInstanceNameNotReserved
from rti1516e.events import ObjectInstanceNameReservationSucceeded
from tests.test_live_service_e2e import (
    VEHICLE,
    LiveCallbacks,
    live_artifacts,  # noqa: F401
    live_server,  # noqa: F401
)

pytestmark = pytest.mark.skipif(
    os.environ.get("GORTI_RUN_LIVE_E2E") != "1",
    reason="production localhost tests are opt-in",
)


def test_live_empty_name_set_preserves_spec_exception(live_server):  # noqa: F811
    async def exercise():
        url, base, _ = live_server
        async with asyncio.timeout(15), RtiConnection.connect(url) as connection:
            async with connection.join_federation(
                FederationSpec("empty-name-set", [str(base)]), federate_name="owner"
            ) as fed:
                with pytest.raises(NameSetWasEmpty) as caught:
                    await fed.reservation.reserve_multiple([])
                assert dict(caught.value.__cause__.trailing_metadata())[
                    "rti-spec-exception"
                ] == "NameSetWasEmpty"

    asyncio.run(exercise())


def test_live_explicit_name_requires_reservation_by_registering_federate(live_server):  # noqa: F811
    async def exercise():
        url, base, _ = live_server
        async with asyncio.timeout(15), AsyncExitStack() as stack:
            federates = []
            for name in ("owner", "other"):
                connection = await stack.enter_async_context(RtiConnection.connect(url))
                fed = await stack.enter_async_context(connection.join_federation(
                    FederationSpec("explicit-name", [str(base)]), federate_name=name
                ))
                await fed.publish_object_class(VEHICLE, attributes=["Value"])
                federates.append(fed)
            owner, other = federates
            callbacks = LiveCallbacks(owner)
            stack.push_async_callback(callbacks.close)
            with pytest.raises(ObjectInstanceNameNotReserved):
                await owner.register_object_instance(VEHICLE, instance_name="reserved-car")
            await owner.reservation.reserve("reserved-car")
            await callbacks.wait(
                ObjectInstanceNameReservationSucceeded,
                lambda event: event.object_name == "reserved-car",
            )
            with pytest.raises(ObjectInstanceNameNotReserved):
                await other.register_object_instance(VEHICLE, instance_name="reserved-car")
            obj = await owner.register_object_instance(VEHICLE, instance_name="reserved-car")
            assert await owner.support.get_object_instance_name(obj) == "reserved-car"

    asyncio.run(exercise())
