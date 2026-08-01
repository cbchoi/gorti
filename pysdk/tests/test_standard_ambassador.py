"""Unit tests for ``rti1516e.standard.Rti1516eAmbassador`` (TASK-068).

Spec-level surface checks live under ``tests/spec/m4/``. This file exercises
the ambassador's actual delegation into
Layer 1 — the spec only verifies method presence + override-ability,
but the implementation must round-trip through RtiConnection /
FederateContextManager / Federate against the FakeRtiServer so end-to-end
ports from Java/C++ aren't broken.
"""

from __future__ import annotations

import pytest

from rti1516e import (
    DiscoverObjectInstance,
    ReceiveInteraction,
    Rti1516eAmbassador,
    TimeAdvanceGrant,
)

# Spec-test-only fake; component-owned tests may consume it the same way.
from tests.spec.m4._fakes import FakeRtiServer


@pytest.fixture
def fake_rti() -> FakeRtiServer:
    """Auto-registers under memory://fake-rti via FakeRtiServer.__init__."""
    return FakeRtiServer()


def test_ambassador_full_lifecycle_records_calls(fake_rti: FakeRtiServer) -> None:
    """Connect/create/join/declare/send/resign/disconnect end-to-end records
    each step via the FakeRtiServer.record() boundary."""
    amb = Rti1516eAmbassador()
    try:
        amb.connect(amb, "memory://fake-rti")
        amb.createFederationExecution("demo", ["demo.fom.xml"])
        amb.joinFederationExecution("alice", "demo")

        amb.publishObjectClassAttributes("Vehicle", ["pos", "vel"])
        amb.subscribeObjectClassAttributes("Vehicle", ["pos"])
        amb.publishInteractionClass("Ping")
        amb.subscribeInteractionClass("Ping")

        handle = amb.registerObjectInstance("Vehicle", "V1")
        assert isinstance(handle, int)
        assert handle > 0

        amb.updateAttributeValues(handle, {"pos": b"\x00\x00\x00\x05"})
        amb.sendInteraction("Ping", {"seq": b"\x00\x00\x00\x01"})

        amb.enableTimeRegulation(1.0)
        amb.enableTimeConstrained()
        amb.nextMessageRequest(5.0)

        amb.resignFederationExecution()
    finally:
        amb.disconnect()

    methods = [c.method for c in fake_rti.calls]
    # Lifecycle bookends
    assert "create_federation" in methods
    assert "join_federation" in methods
    assert "resign_federation" in methods
    # Declaration management
    assert "publish_object_class" in methods
    assert "subscribe_object_class" in methods
    assert "publish_interaction_class" in methods
    assert "subscribe_interaction_class" in methods
    # Object + interaction
    assert "register_object_instance" in methods
    assert "update_attributes" in methods
    assert "send_interaction" in methods
    # Time
    assert "enable_time_regulation" in methods
    assert "enable_time_constrained" in methods
    assert "next_message_request" in methods


def test_ambassador_callbacks_dispatch_from_event_pump(fake_rti: FakeRtiServer) -> None:
    """Push events through the fake; the ambassador's event pump must call
    the right callback on the registered target."""
    received: dict[str, object] = {}

    class Sub(Rti1516eAmbassador):
        def discoverObjectInstance(  # noqa: N802
            self,
            object_handle: int,
            class_name: str,
            instance_name: str,
            object_class: int | None = None,  # M39 typed-handle parity (§6.9)
        ) -> None:
            received["discover"] = (object_handle, class_name, instance_name)

        def receiveInteraction(  # noqa: N802
            self,
            class_name: str,
            parameters: dict[str, object],
            timestamp: float | None,
        ) -> None:
            received["interaction"] = (class_name, dict(parameters), timestamp)

        def timeAdvanceGrant(self, time: float) -> None:  # noqa: N802
            received["grant"] = time

    amb = Sub()
    try:
        amb.connect(amb, "memory://fake-rti")
        amb.joinFederationExecution("alice", "demo")
        fed_handle = amb._fed().handle  # noqa: SLF001 — internal seam for test

        fake_rti.push_event(
            fed_handle,
            DiscoverObjectInstance(object_handle=42, class_name="V", instance_name="V1"),
        )
        fake_rti.push_event(
            fed_handle,
            ReceiveInteraction(class_name="Ping", parameters={"seq": 1}, timestamp=None),
        )
        fake_rti.push_event(fed_handle, TimeAdvanceGrant(time=5.0))

        # Wait for the background event pump to drain. Three events should
        # land within a few hundred ms; poll briefly to avoid flakes.
        import time as _time

        deadline = _time.monotonic() + 1.0
        while _time.monotonic() < deadline and len(received) < 3:
            _time.sleep(0.01)

        amb.resignFederationExecution()
    finally:
        amb.disconnect()

    assert received.get("discover") == (42, "V", "V1")
    assert received.get("interaction") == ("Ping", {"seq": 1}, None)
    assert received.get("grant") == 5.0
