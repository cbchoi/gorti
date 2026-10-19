"""Persist confirmation state over production save/restore, IEEE 7.6.3(g-h)."""

import asyncio
from types import SimpleNamespace

import pytest

from rti1516e.errors import AttributeAlreadyBeingDivested, NoAcquisitionPending
from rti1516e.events import (
    FederationRestored,
    FederationSaved,
    InitiateFederateRestore,
    InitiateFederateSave,
    RequestAttributeOwnershipAssumption,
    RequestDivestitureConfirmation,
)
from tests.spec.m27.test_ieee2010_divest_followup import (
    _barrier,
    _participants,
    _prepare_confirmation,
    _transfer_events,
    _valid_transfer,
)
from tests.test_live_service_e2e import LiveCallbacks, live_artifacts, live_server  # noqa: F401

pytestmark = [pytest.mark.spec, pytest.mark.integration]


class InvokedCallbacks(LiveCallbacks):
    """Track wire traffic separately from authenticated user-handler entry."""

    def __init__(self, federate):
        self.invocations = []
        self.defer_kind = None
        super().__init__(federate)

    async def _collect(self, federate):
        try:
            assert federate.callback_invocation_entry_supported
            assert len(federate._session_token) == 32
            async for event in federate.events():
                if not (self.defer_kind is not None
                        and isinstance(event, self.defer_kind)
                        and not getattr(event, "_callback_recovered", False)):
                    await federate.dispatch_event(event, self.invocations.append)
                self.events.append(event)
                self.changed.set()
        except asyncio.CancelledError:
            raise
        except Exception as exc:
            self.failure = exc
            self.changed.set()


def _invoked_transfer_events(callbacks):
    return _transfer_events([SimpleNamespace(events=log.invocations) for log in callbacks])


def _assert_recovery(log, original, *, invoked):
    identity = original._callback_invocation_identity
    assert len(identity) == 32
    bodies = [event for event in log.invocations
              if event._callback_invocation_identity == identity]
    assert len(bodies) == 1
    if invoked:
        assert bodies[0] is original
    else:
        recovered = bodies[0]
        assert recovered is not original
        assert type(recovered) is type(original)
        assert recovered == original
        assert recovered._callback_recovered
        assert recovered._callback_receipt != original._callback_receipt
        assert recovered._callback_invocation_identity == identity
        assert not recovered._callback_outcome_only
        assert recovered in log.events


async def _save(federates, callbacks, label):
    marks = [len(log.events) for log in callbacks]
    await federates[0].savepoint.request_federation_save(label)
    for log, mark in zip(callbacks, marks, strict=True):
        await log.wait(InitiateFederateSave, lambda event: event.label == label, after=mark)
    for federate in federates:
        await federate.savepoint.federate_save_begun()
        await federate.savepoint.federate_save_complete()
    for log, mark in zip(callbacks, marks, strict=True):
        await log.wait(FederationSaved, lambda event: event.label == label, after=mark)


async def _restore(federates, callbacks, label):
    marks = [len(log.events) for log in callbacks]
    await federates[0].savepoint.request_federation_restore(label)
    for log, mark in zip(callbacks, marks, strict=True):
        await log.wait(InitiateFederateRestore, lambda event: event.label == label, after=mark)
    for federate in federates:
        await federate.savepoint.federate_restore_complete()
    for log, mark in zip(callbacks, marks, strict=True):
        await log.wait(FederationRestored, lambda event: event.label == label, after=mark)


async def _acquire_and_wait(acquirer, owner_log, obj, attr):
    mark = len(owner_log.events)
    await acquirer.ownership.acquire(obj, [attr], tag=b"replacement")
    await owner_log.wait(
        RequestDivestitureConfirmation,
        lambda event: event.object_handle == obj,
        after=mark,
    )


async def _assert_still_owned(owner, acquirer, obj, attr):
    assert await owner.ownership.is_attribute_owned_by_federate(obj, attr)
    assert not await acquirer.ownership.is_attribute_owned_by_federate(obj, attr)


@pytest.mark.parametrize("invoked", [True, False], ids=["invoked", "uninvoked"])
def test_requested_confirmation_survives_save_restore(live_server, invoked):  # noqa: F811
    async def exercise():
        async with _participants(live_server, callback_factory=InvokedCallbacks) as result:
            federates, callbacks, obj, attr = result
            owner, acquirer, _ = federates
            if not invoked:
                callbacks[0].defer_kind = RequestDivestitureConfirmation
            await _prepare_confirmation(owner, acquirer, callbacks, obj, attr)
            await _barrier(owner, callbacks, "requested-before-save")
            original = next(event for event in callbacks[0].events
                            if isinstance(event, RequestDivestitureConfirmation))
            assert sum(event is original for event in callbacks[0].invocations) == int(invoked)
            await _save(federates, callbacks, "requested")

            # Restore must recover the saved request, not retain current live state.
            await owner.ownership.cancel_negotiated_divest(obj, [attr])
            before = _invoked_transfer_events(callbacks)
            await _restore(federates, callbacks, "requested")
            await _barrier(owner, callbacks, "requested-after-restore")
            expected = [list(events) for events in before]
            if not invoked:
                expected[0].append(original)
            assert _invoked_transfer_events(callbacks) == expected
            _assert_recovery(callbacks[0], original, invoked=invoked)
            await _assert_still_owned(owner, acquirer, obj, attr)
            # No new negotiation, acquisition, or confirmation callback is needed.
            await _valid_transfer("confirm", owner, acquirer, callbacks, obj, attr)
            _assert_recovery(callbacks[0], original, invoked=invoked)

    asyncio.run(exercise())


@pytest.mark.parametrize("invoked", [True, False], ids=["invoked", "uninvoked"])
def test_waiting_offer_survives_save_restore(live_server, invoked):  # noqa: F811
    async def exercise():
        async with _participants(live_server, callback_factory=InvokedCallbacks) as result:
            federates, callbacks, obj, attr = result
            owner, acquirer, _ = federates
            if not invoked:
                callbacks[1].defer_kind = RequestAttributeOwnershipAssumption
            await owner.ownership.negotiated_divest(obj, [attr], tag=b"offer", two_phase=True)
            await _barrier(owner, callbacks, "waiting-before-save")
            assert not any(isinstance(event, RequestDivestitureConfirmation)
                           for event in callbacks[0].events)
            original = next(event for event in callbacks[1].events
                            if isinstance(event, RequestAttributeOwnershipAssumption))
            assert sum(event is original for event in callbacks[1].invocations) == int(invoked)
            await _save(federates, callbacks, "waiting")

            await _acquire_and_wait(acquirer, callbacks[0], obj, attr)
            await owner.ownership.cancel_negotiated_divest(obj, [attr])
            await _barrier(owner, callbacks, "waiting-before-restore")
            before = _invoked_transfer_events(callbacks)
            await _restore(federates, callbacks, "waiting")
            await _barrier(owner, callbacks, "waiting-after-restore")
            expected = [list(events) for events in before]
            if not invoked:
                expected[1].append(original)
            assert _invoked_transfer_events(callbacks) == expected
            _assert_recovery(callbacks[1], original, invoked=invoked)
            await _assert_still_owned(owner, acquirer, obj, attr)
            with pytest.raises(AttributeAlreadyBeingDivested):
                await owner.ownership.negotiated_divest(obj, [attr], two_phase=True)
            # Do not assume precedence between absent confirmation and absent acquirer.
            # A fresh acquisition must now request confirmation for the restored offer.
            await _acquire_and_wait(acquirer, callbacks[0], obj, attr)
            await _valid_transfer("confirm", owner, acquirer, callbacks, obj, attr)
            _assert_recovery(callbacks[1], original, invoked=invoked)

    asyncio.run(exercise())


def test_withdrawn_acquirer_preserves_confirmation_after_restore(live_server):  # noqa: F811
    async def exercise():
        async with _participants(live_server, callback_factory=InvokedCallbacks) as result:
            federates, callbacks, obj, attr = result
            owner, acquirer, _ = federates
            await _prepare_confirmation(owner, acquirer, callbacks, obj, attr)
            await acquirer.ownership.cancel_acquire(obj, [attr])
            await _barrier(owner, callbacks, "withdrawn-before-save")
            await _save(federates, callbacks, "withdrawn")

            # Reintroduce a live candidate so restoring its absence is observable.
            await _acquire_and_wait(acquirer, callbacks[0], obj, attr)
            await _restore(federates, callbacks, "withdrawn")
            await _barrier(owner, callbacks, "withdrawn-after-restore")
            before = _invoked_transfer_events(callbacks)
            with pytest.raises(NoAcquisitionPending) as caught:
                await owner.ownership.confirm_divestiture(obj, [attr], tag=b"rejected")
            assert type(caught.value) is NoAcquisitionPending
            metadata = dict(caught.value.__cause__.trailing_metadata())
            assert metadata["rti-spec-exception"] == "NoAcquisitionPending"
            await _assert_still_owned(owner, acquirer, obj, attr)
            await _barrier(owner, callbacks, "withdrawn-after-rejection")
            assert _invoked_transfer_events(callbacks) == before

            await _acquire_and_wait(acquirer, callbacks[0], obj, attr)
            await _valid_transfer("confirm", owner, acquirer, callbacks, obj, attr)

    asyncio.run(exercise())
