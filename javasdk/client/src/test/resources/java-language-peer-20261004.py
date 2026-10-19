"""Product Python API peer for the Java mapping's simultaneous live exchange."""

import sys
import threading
import time
from pathlib import Path

from rti1516e import HLA_EVOKED, Rti1516eAmbassador


class Observer(Rti1516eAmbassador):
    def __init__(self):
        super().__init__()
        self.received = []
        self.evoking = False
        self.thread = threading.get_ident()

    def receiveInteraction(self, class_name, parameters, timestamp, tag=b"", metadata=None):
        self.received.append((class_name, parameters, timestamp, tag, metadata,
                              self.evoking and threading.get_ident() == self.thread))


endpoint, federation, ready = sys.argv[1:]
observer = Observer()
rti = observer
connected = joined = False
try:
    rti.connect(observer, endpoint, callback_model=HLA_EVOKED)
    connected = True
    rti.joinFederationExecution("python", federation, federate_type="language-peer")
    joined = True
    interaction = "HLAinteractionRoot.LanguageProbe"
    handle = rti.getInteractionClassHandle(interaction)
    assert rti.getInteractionClassName(handle) == interaction
    parameter = rti.getParameterHandle(handle, "payload")
    assert rti.getParameterName(handle, parameter) == "payload"
    java = rti.getFederateHandle("java")
    assert java != rti.getFederateHandle("python")
    rti.publishInteractionClass(interaction)
    rti.subscribeInteractionClass(interaction)
    Path(ready).write_text("READY", encoding="ascii")
    deadline = time.monotonic() + 20
    while not observer.received and time.monotonic() < deadline:
        observer.evoking = True
        try:
            rti.evokeMultipleCallbacks(0.0, 0.05)
        finally:
            observer.evoking = False
    assert len(observer.received) == 1, observer.received
    cls, params, timestamp, tag, metadata, evoked = observer.received[0]
    # Python callbacks use the accepted short FOM alias; compare neutral identity.
    assert rti.getInteractionClassHandle(cls) == handle, cls
    assert params == {"payload": bytes([0, 128, 255, 127])}, params
    assert timestamp is None, timestamp
    assert tag == bytes([254, 0, 33]), tag
    assert metadata is not None
    assert metadata.sent_order == 1 and metadata.received_order == 1, metadata
    assert metadata.transportation_type == rti.getTransportationType("HLAreliable"), metadata
    assert metadata.producing_federate == java, metadata
    assert evoked, "Callback must execute within the EVOKED caller boundary"
    print("JAVA_TO_PYTHON_ASSERTIONS_PASSED", flush=True)
    rti.sendInteraction(interaction, {"payload": bytes([127, 255, 128, 0])})
    assert sys.stdin.readline().strip() == "FINISH", "Java did not acknowledge its callback assertions"
finally:
    primary = sys.exception()
    cleanup = []
    if joined:
        try:
            rti.resignFederationExecution("CANCEL_THEN_DELETE_THEN_DIVEST")
        except Exception as error:
            cleanup.append(error)
    if connected:
        try:
            rti.disconnect()
        except Exception as error:
            cleanup.append(error)
    if cleanup:
        print("CLEANUP_FAILURES", repr(cleanup), file=sys.stderr, flush=True)
        if primary is None:
            raise ExceptionGroup("Python peer cleanup", cleanup)
        primary.add_note("Cleanup also failed: " + repr(cleanup))
