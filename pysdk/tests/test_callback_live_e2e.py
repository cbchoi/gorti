"""Real localhost callback-model and completed service coverage."""

import os
import struct
import threading
import time

import pytest

from rti1516e._transport import resign_action_to_proto
from rti1516e.events import (
    AttributeOwnershipUnavailable,
    ReceiveInteraction,
)
from rti1516e.standard import Rti1516eAmbassador

from .test_live_service_e2e import BASE_SIGNAL, live_artifacts, live_server  # noqa: F401

pytestmark = pytest.mark.skipif(
    os.environ.get("GORTI_RUN_LIVE_E2E") != "1", reason="production localhost tests are opt-in"
)


class Callbacks(Rti1516eAmbassador):
    def __init__(self):
        super().__init__()
        self.received = []
        self.times = []
        self.completed = threading.Event()

    def receiveInteraction(self, name, parameters, timestamp):  # noqa: N802
        self.received.append(parameters["Payload"])
        self.times.append(self.queryLogicalTime())
        if len(self.received) == 2:
            self.completed.set()


@pytest.mark.parametrize("model", ["HLA_EVOKED", "HLA_IMMEDIATE"])
@pytest.mark.parametrize("direct", [False, True])
def test_real_wire_callback_model_and_reentrant_service(live_server, model, direct):  # noqa: F811
    url, base, _ = live_server
    publisher, subscriber = Rti1516eAmbassador(), Callbacks()
    publisher.connect(publisher, url)
    subscriber.connect(subscriber, url, callback_model=model)
    try:
        publisher.createFederationExecution("callback-model", [str(base)])
        publisher.joinFederationExecution("publisher", "callback-model")
        subscriber.setDirectCallbackDelivery(direct)
        subscriber.joinFederationExecution("subscriber", "callback-model")
        assert subscriber._ownership_receipt_profile == "actual-invocation"
        publisher.publishInteractionClass(BASE_SIGNAL)
        subscriber.subscribeInteractionClass(BASE_SIGNAL)
        subscriber.setAutomaticResignDirective("NO_ACTION")
        assert subscriber.getAutomaticResignDirective() == resign_action_to_proto("NO_ACTION")
        assert subscriber.getFederateName(subscriber.getFederateHandle("publisher")) == "publisher"
        assert isinstance(subscriber.queryGALT()[1], bool)
        assert subscriber.queryLITS()[1] is False
        payloads = [struct.pack(">d", 1.0), struct.pack(">d", 2.0)]
        publisher.sendInteraction(BASE_SIGNAL, {"Payload": payloads[0]})
        publisher.sendInteraction(BASE_SIGNAL, {"Payload": payloads[1]})
        if model == "HLA_EVOKED":
            deadline = time.monotonic() + 5
            while True:
                with subscriber._callback_dispatcher._condition:
                    queued = sum(
                        isinstance(item.event, ReceiveInteraction)
                        for item in subscriber._callback_dispatcher._queue
                    )
                if queued == 2:
                    break
                assert time.monotonic() < deadline, "callbacks did not reach the evoked queue"
                time.sleep(0.01)
            assert subscriber.received == []
            pending = subscriber.queryFederateAttributes(subscriber._fed().handle)
            assert "HLAinteractionsReceived" in pending.attribute_values
            assert "HLAinteractionsReceived" not in pending.unavailable_attributes
            assert struct.unpack(">I", pending.attribute_values["HLAinteractionsReceived"])[0] == 0
            subscriber.evokeMultipleCallbacks(0, 1)
        assert subscriber.completed.wait(5)
        assert subscriber.received == payloads
        assert subscriber.times == [0.0, 0.0]
        # Drain any in-flight report, without invoking the callback a second time.
        with subscriber._callback_dispatcher._delivery_lock:
            assert not subscriber._callback_report_failures
        measured = subscriber.queryFederateAttributes(subscriber._fed().handle)
        assert "HLAinteractionsReceived" in measured.attribute_values, (
            measured.unavailable_attributes
        )
        assert struct.unpack(">I", measured.attribute_values["HLAinteractionsReceived"])[0] == 2
        subscriber.unsubscribeInteractionClass(BASE_SIGNAL)
        publisher.unpublishInteractionClass(BASE_SIGNAL)
        subscriber.resignFederationExecution()
        publisher.resignFederationExecution()
    finally:
        subscriber.disconnect()
        publisher.disconnect()


def test_standard_default_negotiates_terminal_invocation_and_cancellation(live_server):  # noqa: F811
    url, base, _ = live_server

    class OwnershipTarget(Rti1516eAmbassador):
        def __init__(self):
            super().__init__()
            self.unavailable = []
            self.cancelled = []

        def attributeOwnershipUnavailable(self, object_handle, attributes):  # noqa: N802
            self.unavailable.append((object_handle, attributes))

        def confirmAttributeOwnershipAcquisitionCancellation(self, object_handle, attributes):  # noqa: N802
            self.cancelled.append((object_handle, attributes))

    owner, peer = Rti1516eAmbassador(), OwnershipTarget()
    owner.connect(owner, url, callback_model="HLA_EVOKED")
    peer.connect(peer, url, callback_model="HLA_EVOKED")
    try:
        owner.createFederationExecution("strict-terminals", [str(base)])
        owner.joinFederationExecution("owner", "strict-terminals")
        peer.joinFederationExecution("peer", "strict-terminals")
        assert (
            owner._ownership_receipt_profile
            == peer._ownership_receipt_profile
            == "actual-invocation"
        )
        for participant in (owner, peer):
            participant.publishObjectClassAttributes("Vehicle", ["Value"])
            participant.subscribeObjectClassAttributes("Vehicle", ["Value"])
        obj = owner.registerObjectInstance("Vehicle")
        deadline = time.monotonic() + 5
        while True:
            peer.evokeMultipleCallbacks(0, 0.01)
            try:
                peer.getKnownObjectClassHandle(obj)
                break
            except Exception:
                assert time.monotonic() < deadline
        attr = peer.getAttributeHandle(peer.getObjectClassHandle("Vehicle"), "Value")
        peer.attributeOwnershipAcquisitionIfAvailable(obj, [attr])
        deadline = time.monotonic() + 5
        while True:
            with peer._callback_dispatcher._condition:
                queued = [
                    item.event
                    for item in peer._callback_dispatcher._queue
                    if isinstance(item.event, AttributeOwnershipUnavailable)
                ]
            if queued:
                break
            assert time.monotonic() < deadline
            time.sleep(0.01)
        assert peer.unavailable == []
        assert peer._fed()._ownership_callback_state().pending() == []
        assert queued[0]._ownership_receipt.callback_id > 0
        peer.evokeMultipleCallbacks(0, 0.01)
        assert len(peer.unavailable) == 1
        peer.retryOwnershipCallbacks()
        peer.evokeMultipleCallbacks(0, 0.01)
        assert len(peer.unavailable) == 1
        peer.attributeOwnershipAcquisition(obj, [attr])
        peer.cancelAttributeOwnershipAcquisition(obj, [attr])
        deadline = time.monotonic() + 5
        while not peer.cancelled:
            peer.evokeMultipleCallbacks(0, 0.01)
            assert time.monotonic() < deadline
        assert peer.cancelled == [(obj, (int(attr),))]
        assert not peer._callback_report_failures
    finally:
        peer.disconnect()
        owner.disconnect()
