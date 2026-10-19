"""W4 divest validation through a freshly built production rtid.

Source: active IEEE 1516.1-2010 7.2.5(a-c), 7.3.5(b-d), 7.6.5(c-e),
supplied redline PDF pp. 161-162, 165-166; source identities are recorded in
engineering/verification/ieee1516-2010-normative-review-20260921.md.
Three Python connections share one interpreter, not three federate processes.
"""

from __future__ import annotations

import asyncio
from contextlib import AsyncExitStack, asynccontextmanager

import pytest

from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.errors import AttributeNotDefined, AttributeNotOwned, ObjectInstanceNotKnown
from rti1516e.events import (
    AttributeOwnershipAcquisitionNotification,
    AttributeOwnershipUnavailable,
    DiscoverObjectInstance,
    RequestAttributeOwnershipAssumption,
    RequestAttributeOwnershipRelease,
    RequestDivestitureConfirmation,
    SynchronizationPointAnnounced,
)
from tests.test_live_service_e2e import (
    VEHICLE,
    LiveCallbacks,
    live_artifacts,  # noqa: F401
    live_server,  # noqa: F401
)

pytestmark = [pytest.mark.spec, pytest.mark.integration]

_TRANSFER_CALLBACKS = (
    AttributeOwnershipAcquisitionNotification,
    AttributeOwnershipUnavailable,
    RequestAttributeOwnershipAssumption,
    RequestAttributeOwnershipRelease,
    RequestDivestitureConfirmation,
)
_TAG = b"\x00\xffconfirm"


@asynccontextmanager
async def _participants(server, *, callback_factory=LiveCallbacks, join_contexts=None):
    url, base, _ = server
    async with asyncio.timeout(45), AsyncExitStack() as stack:
        federates, callbacks, connections = [], [], []
        spec = FederationSpec("ieee2010-divest-followup", [str(base)])
        for name in ("owner", "acquirer", "stranger"):
            connection = await stack.enter_async_context(RtiConnection.connect(url))
            context = connection.join_federation(spec, federate_name=name)
            context.resign_action = "CANCEL_THEN_DELETE_THEN_DIVEST"
            if join_contexts is not None:
                join_contexts.append(context)
            federate = await stack.enter_async_context(context)
            log = callback_factory(federate)
            stack.push_async_callback(log.close)
            connections.append(connection)
            federates.append(federate)
            callbacks.append(log)
        assert len({id(connection.transport) for connection in connections}) == 3
        owner, acquirer, _ = federates
        for federate in (owner, acquirer):
            await federate.publish_object_class(VEHICLE, attributes=["Value"])
        await acquirer.subscribe_object_class(VEHICLE, attributes=["Value"])
        cls = await owner.support.get_object_class_handle(VEHICLE)
        attr = await owner.support.get_attribute_handle(cls, "Value")
        obj = await owner.register_object_instance(VEHICLE)
        await callbacks[1].wait(DiscoverObjectInstance, lambda event: event.object_handle == obj)
        yield federates, callbacks, obj, attr


async def _barrier(owner, callbacks, label):
    # Same-stream sync announcements fence earlier ownership callback delivery.
    await owner.sync.register_synchronization_point(label)
    for log in callbacks:
        await log.wait(SynchronizationPointAnnounced, lambda event: event.label == label)


def _transfer_events(callbacks):
    return [
        [event for event in log.events if isinstance(event, _TRANSFER_CALLBACKS)]
        for log in callbacks
    ]


async def _invoke(service, federate, obj, attrs):
    if service == "unconditional":
        await federate.ownership.unconditional_divest(obj, attrs)
    elif service == "negotiated":
        await federate.ownership.negotiated_divest(obj, attrs, tag=b"offer", two_phase=True)
    else:
        assert service == "confirm"
        await federate.ownership.confirm_divestiture(obj, attrs, tag=_TAG)


async def _prepare_confirmation(owner, acquirer, callbacks, obj, attr):
    await _invoke("negotiated", owner, obj, [attr])
    await callbacks[1].wait(
        RequestAttributeOwnershipAssumption, lambda event: event.object_handle == obj
    )
    await acquirer.ownership.acquire(obj, [attr], tag=b"pending")
    await callbacks[0].wait(
        RequestDivestitureConfirmation, lambda event: event.object_handle == obj
    )
    assert await owner.ownership.is_attribute_owned_by_federate(obj, attr)
    assert not await acquirer.ownership.is_attribute_owned_by_federate(obj, attr)


async def _valid_transfer(service, owner, acquirer, callbacks, obj, attr):
    if service == "unconditional":
        await _invoke(service, owner, obj, [attr])
        assert await owner.ownership.query_attribute_ownership(obj, attr) == (0, False)
        await acquirer.ownership.acquire(obj, [attr], tag=_TAG)
    else:
        if service == "negotiated":
            await _prepare_confirmation(owner, acquirer, callbacks, obj, attr)
        await _invoke("confirm", owner, obj, [attr])
    acquired = await callbacks[1].wait(
        AttributeOwnershipAcquisitionNotification, lambda event: event.object_handle == obj
    )
    assert acquired.attribute_handles == (attr,)
    assert acquired.owning_federate == acquirer.handle
    assert acquired.tag == _TAG
    assert not await owner.ownership.is_attribute_owned_by_federate(obj, attr)
    assert await acquirer.ownership.is_attribute_owned_by_federate(obj, attr)
    await _barrier(owner, callbacks, "after-valid-transfer")
    assert sum(
        isinstance(event, AttributeOwnershipAcquisitionNotification)
        for log in callbacks for event in log.events
    ) == 1


@pytest.mark.parametrize("service", ["unconditional", "negotiated", "confirm"])
@pytest.mark.parametrize(
    "invalid",
    [
        "missing-object", "unknown-caller", "unknown-empty-set", "undefined",
        "mixed-undefined", "undefined-first", "nonowner",
    ],
)
def test_divest_rejects_without_mutation_or_callbacks(
    live_server, service, invalid,  # noqa: F811
):
    async def exercise():
        async with _participants(live_server) as (federates, callbacks, obj, attr):
            owner, acquirer, stranger = federates
            if service == "confirm":
                await _prepare_confirmation(owner, acquirer, callbacks, obj, attr)
            await _barrier(owner, callbacks, "before-invalid")
            before = _transfer_events(callbacks)
            caller, target, attrs, exception = owner, obj, [attr], ObjectInstanceNotKnown
            if invalid == "missing-object":
                target = (1 << 64) - 1
            elif invalid == "unknown-caller":
                caller = stranger
            elif invalid == "unknown-empty-set":
                caller, attrs = stranger, []
            elif invalid == "undefined":
                attrs, exception = [999999], AttributeNotDefined
            elif invalid == "mixed-undefined":
                attrs, exception = [attr, 999999], AttributeNotDefined
            elif invalid == "undefined-first":
                attrs, exception = [999999, attr], AttributeNotDefined
            else:
                assert invalid == "nonowner"
                caller, exception = acquirer, AttributeNotOwned
            with pytest.raises(exception):
                await _invoke(service, caller, target, attrs)
            assert await owner.ownership.is_attribute_owned_by_federate(obj, attr)
            assert not await acquirer.ownership.is_attribute_owned_by_federate(obj, attr)
            await _barrier(owner, callbacks, "after-invalid")
            assert _transfer_events(callbacks) == before
            await _valid_transfer(service, owner, acquirer, callbacks, obj, attr)

    asyncio.run(exercise())


@pytest.mark.parametrize("service", ["unconditional", "negotiated", "confirm"])
def test_divest_valid_control(live_server, service):  # noqa: F811
    async def exercise():
        async with _participants(live_server) as (federates, callbacks, obj, attr):
            owner, acquirer, _ = federates
            if service == "confirm":
                await _prepare_confirmation(owner, acquirer, callbacks, obj, attr)
            await _valid_transfer(service, owner, acquirer, callbacks, obj, attr)

    asyncio.run(exercise())
