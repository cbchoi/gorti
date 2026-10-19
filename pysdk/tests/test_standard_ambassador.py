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


@pytest.mark.parametrize("enqueue", ["push_event", "queue"], ids=["transport", "queue"])
def test_cross_thread_enqueue_wakes_parked_event_pump(fake_rti, enqueue):
    import asyncio
    import threading

    delivered = threading.Event()
    received = []

    class Sub(Rti1516eAmbassador):
        def timeAdvanceGrant(self, time):  # noqa: N802
            received.append(time)
            if len(received) == 3:
                delivered.set()

    amb = Sub()
    try:
        amb.connect(amb, "memory://fake-rti")
        amb.joinFederationExecution("caller", "demo")
        handle = amb._fed().handle
        queue = fake_rti.events_for(handle)

        async def parked_reader():
            for _ in range(10):
                if queue._getters:
                    return
                await asyncio.sleep(0)
            raise AssertionError("event pump did not park on the empty queue")

        amb._run(parked_reader())
        for time in (1, 2, 3):
            event = TimeAdvanceGrant(time=time)
            if enqueue == "push_event":
                fake_rti.push_event(handle, event)
            else:
                queue.put_nowait(event)
        assert delivered.wait(0.5), "cross-thread enqueue did not wake the parked reader"
        assert received == [1, 2, 3]
    finally:
        amb.disconnect()


@pytest.mark.asyncio
async def test_event_queue_preserves_prequeued_same_loop_and_cancelled_reads(fake_rti):
    import asyncio

    queue = fake_rti.events_for(42)
    fake_rti.push_event(42, TimeAdvanceGrant(time=1))
    assert await queue.get() == TimeAdvanceGrant(time=1)
    queue.put_nowait(TimeAdvanceGrant(time=2))
    assert await queue.get() == TimeAdvanceGrant(time=2)
    reader = asyncio.create_task(queue.get())
    await asyncio.sleep(0)
    reader.cancel()
    with pytest.raises(asyncio.CancelledError):
        await reader
    fake_rti.push_event(42, TimeAdvanceGrant(time=3))
    assert await queue.get() == TimeAdvanceGrant(time=3)
