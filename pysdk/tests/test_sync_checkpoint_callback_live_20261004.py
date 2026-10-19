"""Real TCP save-entry deferral, not restore-graph or whole-binding conformance.

S1516.1 4.12.3 and Figure 4: a queued synchronization confirmation cannot
enter while its recipient is saving. Hold one actual streamed confirmation
to exercise that ordering; all admission and completion reports use real RPCs.
"""

import threading
import time

import pytest

from rti1516e import events
from rti1516e.errors import SaveInProgress
from rti1516e.standard import Rti1516eAmbassador
from tests.test_ieee2010_independent_contract_live import (  # noqa: F401
    independent_artifacts,
    independent_server,
)


class SaveObserver(Rti1516eAmbassador):
    def __init__(self):
        super().__init__()
        self.condition = threading.Condition()
        self.held = None
        self.calls = []

    def _dispatch_event(self, event):
        if (isinstance(event, events.SynchronizationPointRegistrationSucceeded)
                and event.label == "held"):
            with self.condition:
                assert self.held is None, "the live stream duplicated the held confirmation"
                self.held = event
                self.condition.notify_all()
            return False
        return super()._dispatch_event(event)

    def synchronizationPointRegistrationSucceeded(self, label):  # noqa: N802
        self.calls.append(("sync", label))

    def initiateFederateSave(self, label, save_time):  # noqa: N802
        self.calls.append(("initiate", label))

    def federationSaved(self, label):  # noqa: N802
        self.calls.append(("saved", label))

    def federationNotSaved(self, label):  # noqa: N802
        self.calls.append(("not-saved", label))


def evoke_until(actor, expected):
    deadline = time.monotonic() + 8
    while expected not in actor.calls:
        assert time.monotonic() < deadline, actor.calls
        actor.evokeCallback(0.05)


@pytest.mark.parametrize("scalar", ["HLAfloat64Time", "HLAinteger64Time"])
@pytest.mark.parametrize("complete", [False, True], ids=["not-saved", "saved"])
def test_tcp_sync_confirmation_defers_until_save_terminal_exactly_once(
    independent_server, monkeypatch, scalar, complete,  # noqa: F811 - imported pytest fixture
):
    url, base, _, _ = independent_server
    actor = SaveObserver()
    actor.connect(actor, url, callback_model="HLA_EVOKED")
    try:
        actor.createFederationExecution(
            "sync-save-retry", [str(base)], logical_time_implementation_name=scalar,
        )
        actor.joinFederationExecution("member", "sync-save-retry")
        assert actor._fed().callback_invocation_entry_supported
        reports = []
        real_report = actor._fed().report_callback_invocation

        async def observe_report(receipt, success, exception="", *, invocation_entry=False):
            try:
                result = await real_report(
                    receipt, success, exception, invocation_entry=invocation_entry,
                )
            except SaveInProgress:
                reports.append((receipt, invocation_entry, "SaveInProgress"))
                raise
            reports.append((receipt, invocation_entry, "accepted"))
            return result

        monkeypatch.setattr(actor._fed(), "report_callback_invocation", observe_report)
        actor.registerFederationSynchronizationPoint("held", b"\x00save-retry\xff")
        with actor.condition:
            assert actor.condition.wait_for(lambda: actor.held is not None, timeout=8)
        held = actor.held
        receipt = held._callback_receipt
        assert receipt, "the server must issue a real invocation receipt"
        assert actor.calls == []

        actor.requestFederationSave("active")
        evoke_until(actor, ("initiate", "active"))
        # Reintroduce the real event after initiation without replacing its identity.
        Rti1516eAmbassador._dispatch_event(actor, held)
        for attempt in (1, 2):
            assert not actor.evokeMultipleCallbacks(0, 0.05)
            assert reports.count((receipt, True, "SaveInProgress")) == attempt
            pending = actor._callback_dispatcher.snapshot_pending()
            assert sum(event is held for event in pending) == 1
            assert ("sync", "held") not in actor.calls
            assert (receipt, False, "accepted") not in reports

        actor.federateSaveBegun()
        if complete:
            actor.federateSaveComplete()
        else:
            actor.federateSaveNotComplete()
        terminal_type = events.FederationSaved if complete else events.FederationNotSaved
        dispatcher = actor._callback_dispatcher
        with dispatcher._condition:
            assert dispatcher._condition.wait_for(
                lambda: any(isinstance(item.event, terminal_type) for item in dispatcher._queue),
                timeout=8,
            )
        assert actor.evokeCallback(0)
        terminal = ("saved" if complete else "not-saved", "active")
        assert actor.calls == [("initiate", "active"), terminal]
        assert reports.count((receipt, True, "SaveInProgress")) == 3
        assert actor.evokeCallback(0)
        assert actor.calls == [("initiate", "active"), terminal, ("sync", "held")]
        assert reports.count((receipt, True, "accepted")) == 1
        assert reports.count((receipt, False, "accepted")) == 1

        Rti1516eAmbassador._dispatch_event(actor, held)
        actor.evokeMultipleCallbacks(0, 0.05)
        assert actor.calls.count(("sync", "held")) == 1
        assert reports.count((receipt, False, "accepted")) == 1
        assert all(event is not held for event in dispatcher.snapshot_pending())
        actor.registerFederationSynchronizationPoint("after-terminal", b"poststate")
        evoke_until(actor, ("sync", "after-terminal"))
        assert actor.calls.count(("sync", "after-terminal")) == 1
        assert not actor._callback_report_failures
        actor.resignFederationExecution("CANCEL_THEN_DELETE_THEN_DIVEST")
    finally:
        actor.disconnect()
