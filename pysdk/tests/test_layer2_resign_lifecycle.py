"""Layer-2 rejection retains callback delivery until resignation succeeds."""

import asyncio
import threading

import pytest

from rti1516e import Rti1516eAmbassador
from rti1516e._inprocess import InProcessTransport
from rti1516e.errors import OwnershipAcquisitionPending
from rti1516e.events import SynchronizationPointAnnounced
from tests.spec.m39.test_m39_layer2_api import _Recorder
from tests.test_live_service_e2e import live_artifacts  # noqa: F401 - pytest fixture
from tests.test_live_service_e2e import live_server as _live_server  # noqa: F401 - named fixture


@pytest.mark.parametrize("direct", [False, True], ids=["pump", "direct"])
def test_layer2_rejected_resign_callbacks_and_retry(direct):
    class Transport(InProcessTransport):
        def __init__(self):
            super().__init__()
            self.sinks = {}

        def set_event_sink(self, handle, sink):
            if sink is None:
                self.sinks.pop(handle, None)
            else:
                self.sinks[handle] = sink

        def push_event(self, handle, event):
            sink = self.sinks.get(handle)
            if sink is None:
                super().push_event(handle, event)
            else:
                sink(event)

    entries = []
    entered = threading.Event()

    class Ambassador(Rti1516eAmbassador):
        def announceSynchronizationPoint(self, label, tag):  # noqa: N802
            entries.append((label, tag, threading.current_thread()))
            entered.set()

    transport = Transport()
    ambassador = Ambassador()
    try:
        ambassador.setDirectCallbackDelivery(direct)
        ambassador.connect(ambassador, "memory://fake-rti")
        ambassador.joinFederationExecution("caller", "resign-layer2")
        federate = ambassador._fed()
        context = ambassador._federate_cm
        pump = ambassador._event_pump_task
        failure = OwnershipAcquisitionPending("pending acquisition")
        transport.canned_responses["resign_federation"] = failure
        with pytest.raises(OwnershipAcquisitionPending) as caught:
            ambassador.resignFederationExecution()
        assert caught.value is failure
        assert ambassador._fed() is federate
        assert ambassador._federate_cm is context

        async def deliver():
            transport.push_event(
                federate.handle,
                SynchronizationPointAnnounced(label="after-rejection", tag=b"usable"),
            )
            await asyncio.sleep(0)

        ambassador._run(deliver())
        assert entered.wait(0.5), "rejected resignation stopped the callback body"
        assert [(label, tag) for label, tag, _ in entries] == [("after-rejection", b"usable")]
        assert entries[0][2] is not ambassador._loop_thread
        if direct:
            assert ambassador._direct_event_sink_installed
            assert transport.sinks[federate.handle] == ambassador._dispatch_event
        else:
            assert ambassador._event_pump_task is pump
            assert not pump.done()
        transport.canned_responses.pop("resign_federation")
        ambassador.resignFederationExecution("CANCEL_THEN_DELETE_THEN_DIVEST")
        assert ambassador._federate is None
        assert ambassador._federate_cm is None
        assert ambassador._event_pump_task is None
        assert not ambassador._direct_event_sink_installed
        assert federate.handle not in transport.sinks
        assert [call.args["action"] for call in transport.calls_for("resign_federation")] == [
            "UNCONDITIONALLY_DIVEST_ATTRIBUTES",
            "CANCEL_THEN_DELETE_THEN_DIVEST",
        ]
    finally:
        transport.canned_responses.pop("resign_federation", None)
        ambassador.disconnect()


@pytest.mark.integration
@pytest.mark.parametrize("direct", [False, True], ids=["pump", "direct"])
def test_live_layer2_rejected_resign_keeps_callback_entries_and_retry(live_server, direct):
    class Recorder(_Recorder):
        def announceSynchronizationPoint(self, label, tag):  # noqa: N802
            self._record("announce", label=label, tag=tag)

    url, base, _ = live_server
    owner, acquirer = Recorder(), Recorder()
    federation = "layer2-resign-review"
    try:
        owner.connect(owner, url)
        owner.createFederationExecution(federation, [str(base)])
        owner.joinFederationExecution("owner", federation)
        acquirer.setDirectCallbackDelivery(direct)
        acquirer.connect(acquirer, url)
        acquirer.joinFederationExecution("acquirer", federation)
        assert acquirer._ownership_receipt_profile == "actual-invocation"
        assert acquirer._fed().callback_invocation_entry_supported
        cls = owner.getObjectClassHandle("HLAobjectRoot.Vehicle")
        attr = owner.getAttributeHandle(cls, "Value")
        owner.publishObjectClassAttributes(cls, [attr])
        acquirer.publishObjectClassAttributes(cls, [attr])
        acquirer.subscribeObjectClassAttributes(cls, [attr])
        obj = owner.registerObjectInstance(cls)
        acquirer.wait_for("discover", object_handle=obj)
        acquirer.attributeOwnershipAcquisition(obj, [attr], b"pending-resign")
        owner.wait_for("releaseRequested", object_handle=obj, tag=b"pending-resign")
        federate, context = acquirer._fed(), acquirer._federate_cm
        transport = federate._transport
        task, token = (
            transport._stream_tasks[federate.handle],
            transport.session_token(federate.handle),
        )
        pump = acquirer._event_pump_task
        with pytest.raises(OwnershipAcquisitionPending) as caught:
            acquirer.resignFederationExecution()
        assert type(caught.value) is OwnershipAcquisitionPending
        assert dict(caught.value.__cause__.trailing_metadata())["rti-spec-exception"] == (
            "OwnershipAcquisitionPending"
        )
        assert acquirer._fed() is federate
        assert acquirer._federate_cm is context
        assert transport._stream_tasks[federate.handle] is task and not task.done()
        assert transport.session_token(federate.handle) == token and token
        if direct:
            assert acquirer._direct_event_sink_installed
            assert transport._event_sinks[federate.handle] == acquirer._dispatch_event
        else:
            assert acquirer._event_pump_task is pump and not pump.done()
        owner.registerFederationSynchronizationPoint("after-rejected-layer2", b"usable")
        for ambassador in (owner, acquirer):
            ambassador.wait_for("announce", label="after-rejected-layer2", tag=b"usable")
        assert (
            sum(
                name == "announce" and payload["label"] == "after-rejected-layer2"
                for name, payload in acquirer.calls
            )
            == 1
        )
        assert owner.isAttributeOwnedByFederate(obj, attr)
        assert not acquirer.isAttributeOwnedByFederate(obj, attr)
        acquirer.resignFederationExecution("CANCEL_THEN_DELETE_THEN_DIVEST")
        assert acquirer._federate is None and acquirer._federate_cm is None
        assert federate.handle not in transport._stream_tasks
        assert transport.session_token(federate.handle) == b""
        assert getattr(acquirer, "_callback_error", None) is None
    finally:
        for ambassador in (acquirer, owner):
            if ambassador._federate is not None:
                ambassador.resignFederationExecution("CANCEL_THEN_DELETE_THEN_DIVEST")
            ambassador.disconnect()
