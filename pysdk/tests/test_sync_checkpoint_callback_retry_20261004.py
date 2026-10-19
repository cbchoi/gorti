"""Synchronization entry deferral through the real SDK callback dispatcher."""

import asyncio
from collections import Counter
from types import SimpleNamespace

import pytest

from rti1516e import events
from rti1516e._callbacks import CallbackDispatcher
from rti1516e._generic_callbacks import callback_state
from rti1516e.errors import RestoreInProgress, SaveInProgress
from rti1516e.standard import Rti1516eAmbassador


class CheckpointQueue:
    def __init__(self, model, phase, rejected=False):
        self.amb = Rti1516eAmbassador()
        self.amb._callback_dispatcher.close()
        self.dispatcher = CallbackDispatcher(self.amb._dispatch_callback, model)
        self.amb._callback_dispatcher = self.dispatcher
        self.amb._run = asyncio.run
        self.phase = phase
        self.active = False
        self.calls = []
        self.entries = Counter()
        self.completions = []
        self.by_receipt = {}
        self.error = SaveInProgress if phase == "save" else RestoreInProgress
        self.confirmation = (
            events.SynchronizationPointRegistrationFailed(
                "ready",
                events.SynchronizationPointFailureReason.SYNCHRONIZATION_POINT_LABEL_NOT_UNIQUE,
            ) if rejected else events.SynchronizationPointRegistrationSucceeded("ready")
        )
        self.control = (events.InitiateFederateSave("checkpoint", None) if phase == "save"
                        else events.FederationRestoreBegun())
        self.amb._federate = SimpleNamespace(
            callback_invocation_entry_supported=True, report_callback_invocation=self.report
        )
        self.amb._callback_target = SimpleNamespace(
            synchronizationPointRegistrationSucceeded=lambda *args: self.calls.append("sync"),
            synchronizationPointRegistrationFailed=lambda *args: self.calls.append("sync"),
            initiateFederateSave=lambda *args: self.calls.append("control"),
            federationRestoreBegun=lambda *args: self.calls.append("control"),
            initiateFederateRestore=lambda *args: self.calls.append("restore-initiate"),
            federationSaved=lambda *args: self.calls.append("terminal"),
            federationNotSaved=lambda *args: self.calls.append("terminal"),
            federationRestored=lambda *args: self.calls.append("terminal"),
            federationNotRestored=lambda *args: self.calls.append("terminal"),
            announceSynchronizationPoint=lambda *args: self.calls.append("ordinary"),
        )

    def tagged(self, event):
        receipt = getattr(event, "_callback_receipt", b"")
        if not receipt:
            receipt = bytes([len(self.by_receipt) + 1]) * 32
            object.__setattr__(event, "_callback_receipt", receipt)
            self.by_receipt[receipt] = event
        return event

    async def report(self, receipt, success, exception="", *, invocation_entry=False):
        event = self.by_receipt[receipt]
        if not invocation_entry:
            self.completions.append((receipt, success, exception))
            return
        self.entries[receipt] += 1
        if event is self.confirmation and self.active:
            raise self.error("checkpoint callback has entered")
        if event is self.control:
            self.active = True
        if isinstance(event, (events.FederationSaved, events.FederationNotSaved,
                              events.FederationRestored, events.FederationNotRestored)):
            self.active = False

    def terminal(self, success):
        kind = ((events.FederationSaved if success else events.FederationNotSaved)
                if self.phase == "save" else
                (events.FederationRestored if success else events.FederationNotRestored))
        return kind("checkpoint")

    def submit(self, *values):
        self.dispatcher.submit_many([self.tagged(value) for value in values], wait=False)

    def idle(self):
        with self.dispatcher._condition:
            assert self.dispatcher._condition.wait_for(
                lambda: self.dispatcher._worker is None, timeout=2
            )


@pytest.mark.parametrize("phase", ["save", "restore"])
@pytest.mark.parametrize("rejected", [False, True])
def test_evoked_confirmation_before_checkpoint_entry_preserves_fifo(phase, rejected):
    queue = CheckpointQueue("HLA_EVOKED", phase, rejected)
    try:
        queue.submit(queue.confirmation, queue.control)
        assert queue.calls == []
        assert queue.amb.evokeCallback(0)
        assert queue.calls == ["sync"] and not queue.active
        assert queue.amb.evokeCallback(0)
        assert queue.calls == ["sync", "control"] and queue.active
        assert queue.entries[queue.confirmation._callback_receipt] == 1
        assert queue.dispatcher.snapshot_pending() == []
    finally:
        queue.dispatcher.close()


@pytest.mark.parametrize("model", ["HLA_EVOKED", "HLA_IMMEDIATE"])
@pytest.mark.parametrize("phase", ["save", "restore"])
@pytest.mark.parametrize("rejected", [False, True])
@pytest.mark.parametrize("success", [False, True])
def test_confirmation_defers_without_spin_and_retries_after_terminal(
    model, phase, rejected, success
):
    queue = CheckpointQueue(model, phase, rejected)
    try:
        queue.submit(queue.control, queue.confirmation)
        if model == "HLA_EVOKED":
            assert queue.amb.evokeCallback(0)
            assert not queue.amb.evokeMultipleCallbacks(0, 0.02)
        else:
            queue.idle()
        assert queue.calls == ["control"]
        receipt = queue.confirmation._callback_receipt
        assert queue.entries[receipt] == 1
        assert queue.dispatcher.snapshot_pending() == [queue.confirmation]
        assert callback_state(queue.amb).checkpoint_outcomes() == []
        assert not any(ticket == receipt for ticket, _, _ in queue.completions)
        if model == "HLA_EVOKED":
            assert not queue.amb.evokeMultipleCallbacks(0, 0.02)
            assert queue.entries[receipt] == 2
            assert queue.dispatcher.snapshot_pending() == [queue.confirmation]
        ordinary = events.SynchronizationPointAnnounced("other", b"tag", ())
        queue.submit(ordinary, queue.terminal(success))
        if model == "HLA_EVOKED":
            assert queue.amb.evokeCallback(0)
            assert queue.calls == ["control", "terminal"]
            assert queue.amb.evokeCallback(0)
            assert queue.amb.evokeCallback(0)
        else:
            queue.idle()
        assert queue.calls == ["control", "terminal", "sync", "ordinary"]
        queue.submit(queue.confirmation)
        if model == "HLA_EVOKED":
            queue.amb.evokeMultipleCallbacks(0, 0.01)
        else:
            queue.idle()
        assert queue.calls.count("sync") == 1
        assert [value for value in queue.completions if value[0] == receipt] == [
            (receipt, True, "")
        ]
        assert queue.dispatcher.snapshot_pending() == []
    finally:
        queue.dispatcher.close()


def test_restore_epoch_discards_deferred_confirmation_without_reentry():
    queue = CheckpointQueue("HLA_EVOKED", "restore")
    try:
        queue.submit(queue.control, queue.confirmation)
        assert queue.amb.evokeCallback(0)
        assert not queue.amb.evokeCallback(0)
        assert queue.dispatcher.snapshot_pending() == [queue.confirmation]
        terminal = queue.tagged(events.FederationRestored("checkpoint", 7, 9))
        queue.amb._dispatch_event(terminal)
        assert queue.dispatcher.snapshot_pending() == [terminal]
        queue.amb.evokeMultipleCallbacks(0, 0.01)
        assert queue.calls == ["control", "terminal"]
        assert queue.entries[queue.confirmation._callback_receipt] == 1
    finally:
        queue.dispatcher.close()


@pytest.mark.parametrize("scope", ["other-entry", "unqualified-entry", "user-body"])
def test_only_sync_checkpoint_entry_errors_are_deferred(scope):
    queue = CheckpointQueue("HLA_EVOKED", "save")
    failure = (RuntimeError("entry failed") if scope == "unqualified-entry"
               else SaveInProgress("body"))
    try:
        original = queue.report

        async def report(*args, invocation_entry=False, **kwargs):
            if invocation_entry and scope != "user-body":
                raise failure
            return await original(*args, invocation_entry=invocation_entry, **kwargs)

        queue.amb._federate.report_callback_invocation = report

        def body(*args):
            queue.calls.append("sync")
            raise failure

        if scope == "user-body":
            queue.amb._callback_target.synchronizationPointRegistrationSucceeded = body
        value = (events.SynchronizationPointAnnounced("other", b"", ())
                 if scope == "other-entry" else queue.confirmation)
        queue.submit(value)
        with pytest.raises(type(failure)) as caught:
            queue.amb.evokeCallback(0)
        assert caught.value is failure
        assert queue.dispatcher.snapshot_pending() == []
        assert not queue.amb.evokeCallback(0)
        assert queue.calls == (["sync"] if scope == "user-body" else [])
    finally:
        queue.dispatcher.close()


def test_epoch_change_during_rejected_entry_does_not_requeue_old_callback():
    queue = CheckpointQueue("HLA_EVOKED", "save")
    try:
        async def report(*args, **kwargs):
            queue.dispatcher.clear()
            raise SaveInProgress("old epoch")

        queue.amb._federate.report_callback_invocation = report
        queue.submit(queue.confirmation)
        assert not queue.amb.evokeCallback(0)
        assert queue.dispatcher.snapshot_pending() == []
        assert queue.calls == []
    finally:
        queue.dispatcher.close()


def test_restore_initiation_can_pass_deferred_confirmation_before_terminal():
    queue = CheckpointQueue("HLA_EVOKED", "restore")
    try:
        queue.submit(queue.control, queue.confirmation)
        assert queue.amb.evokeCallback(0)
        assert not queue.amb.evokeCallback(0)
        ordinary = events.SynchronizationPointAnnounced("other", b"", ())
        initiate = events.InitiateFederateRestore("checkpoint", 7, "member")
        queue.submit(ordinary, initiate, queue.terminal(False))
        assert queue.amb.evokeCallback(0)
        assert queue.calls == ["control", "restore-initiate"]
        assert queue.amb.evokeCallback(0)
        assert queue.calls == ["control", "restore-initiate", "terminal"]
        assert queue.amb.evokeCallback(0)
        assert queue.amb.evokeCallback(0)
        assert queue.calls == ["control", "restore-initiate", "terminal", "sync", "ordinary"]
        assert queue.dispatcher.snapshot_pending() == []
    finally:
        queue.dispatcher.close()


def test_historical_control_does_not_bypass_deferred_confirmation():
    queue = CheckpointQueue("HLA_EVOKED", "save")
    try:
        queue.submit(queue.control, queue.confirmation)
        assert queue.amb.evokeCallback(0)
        assert not queue.amb.evokeCallback(0)
        historical = queue.tagged(events.FederationSaved("old-checkpoint"))
        object.__setattr__(historical, "_callback_recovered", True)
        queue.submit(historical)
        assert not queue.amb.evokeMultipleCallbacks(0, 0.01)
        assert queue.calls == ["control"]
        assert queue.entries[historical._callback_receipt] == 0
        assert queue.dispatcher.snapshot_pending() == [queue.confirmation, historical]
        queue.submit(queue.terminal(False))
        assert queue.amb.evokeCallback(0)
        assert queue.amb.evokeCallback(0)
        assert queue.amb.evokeCallback(0)
        assert queue.calls == ["control", "terminal", "sync", "terminal"]
        assert queue.dispatcher.snapshot_pending() == []
    finally:
        queue.dispatcher.close()
