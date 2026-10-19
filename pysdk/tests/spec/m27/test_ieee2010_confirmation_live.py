"""Confirmation exception distinctions over production gRPC, IEEE 7.6.5(a-b)."""

import asyncio

import pytest

from rti1516e.errors import AttributeDivestitureWasNotRequested, NoAcquisitionPending
from rti1516e.events import RequestDivestitureConfirmation
from tests.spec.m27.test_ieee2010_divest_followup import (
    _barrier,
    _participants,
    _prepare_confirmation,
    _transfer_events,
    _valid_transfer,
)
from tests.test_live_service_e2e import live_artifacts, live_server  # noqa: F401

pytestmark = [pytest.mark.spec, pytest.mark.integration]


@pytest.mark.parametrize("withdrawn", [False, True], ids=["not-requested", "no-acquirer"])
def test_confirmation_distinct_exception_and_recovery(live_server, withdrawn):  # noqa: F811
    async def exercise():
        async with _participants(live_server) as (federates, callbacks, obj, attr):
            owner, acquirer, _ = federates
            if withdrawn:
                await _prepare_confirmation(owner, acquirer, callbacks, obj, attr)
                await acquirer.ownership.cancel_acquire(obj, [attr])
            else:
                # A willing acquirer exists, isolating the missing-confirmation condition.
                await acquirer.ownership.acquire(obj, [attr], tag=b"pending")
            await _barrier(owner, callbacks, "before-rejected-confirmation")
            before = _transfer_events(callbacks)
            expected = NoAcquisitionPending if withdrawn else AttributeDivestitureWasNotRequested
            with pytest.raises(expected) as caught:
                await owner.ownership.confirm_divestiture(obj, [attr], tag=b"rejected")
            assert type(caught.value) is expected
            assert (
                dict(caught.value.__cause__.trailing_metadata())["rti-spec-exception"]
                == expected.__name__
            )
            assert await owner.ownership.is_attribute_owned_by_federate(obj, attr)
            assert not await acquirer.ownership.is_attribute_owned_by_federate(obj, attr)
            await _barrier(owner, callbacks, "after-rejected-confirmation")
            assert _transfer_events(callbacks) == before

            after = len(callbacks[0].events)
            if withdrawn:
                # Recovery uses the original offer, with no second negotiated divestiture.
                await acquirer.ownership.acquire(obj, [attr], tag=b"returned")
            else:
                await owner.ownership.negotiated_divest(obj, [attr], tag=b"offer", two_phase=True)
            await callbacks[0].wait(
                RequestDivestitureConfirmation,
                lambda event: event.object_handle == obj,
                after=after,
            )
            await _valid_transfer("confirm", owner, acquirer, callbacks, obj, attr)

    asyncio.run(exercise())
