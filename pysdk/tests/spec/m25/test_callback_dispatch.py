"""M25 Phase D — Rti1516eAmbassador callback dispatch.

Drives `_pump_events` directly against a hand-built async generator
of the M25 event set, asserting every callback the base class exposes
fires with the correct payload. No federation / no RTI subprocess
required — this is a unit test of the dispatch wiring.
"""

from __future__ import annotations

import asyncio
from typing import Any

import pytest

from rti1516e.events import (
    AttributeOwnershipAcquisitionNotification,
    DiscoverObjectInstance,
    FederationHalted,
    FederationNotSaved,
    FederationSaved,
    FederationSynchronized,
    InitiateFederateSave,
    ProvideAttributeValueUpdate,
    ReceiveInteraction,
    ReflectAttributeValues,
    RemoveObjectInstance,
    RequestAttributeOwnershipAssumption,
    RequestDivestitureConfirmation,
    SynchronizationPointAnnounced,
    TimeAdvanceGrant,
)
from rti1516e.standard import Rti1516eAmbassador


class RecordingAmbassador(Rti1516eAmbassador):
    """Captures each callback invocation into a list of (name, payload-tuple)."""

    def __init__(self) -> None:
        super().__init__()
        self.calls: list[tuple[str, tuple[Any, ...]]] = []

    def discoverObjectInstance(  # noqa: N802
        self,
        object_handle: int,
        class_name: str,
        instance_name: str,
        object_class: int | None = None,  # M39 typed-handle parity (§6.9)
    ) -> None:
        self.calls.append(("discoverObjectInstance", (object_handle, class_name, instance_name)))

    def reflectAttributeValues(  # noqa: N802
        self,
        object_handle: int,
        values: dict[str, Any],
        timestamp: float | None,
        attribute_values: dict[Any, bytes] | None = None,  # M39 (§6.11)
    ) -> None:
        self.calls.append(("reflectAttributeValues", (object_handle, values, timestamp)))

    def receiveInteraction(  # noqa: N802
        self, class_name: str, parameters: dict[str, Any], timestamp: float | None
    ) -> None:
        self.calls.append(("receiveInteraction", (class_name, parameters, timestamp)))

    def timeAdvanceGrant(self, time: float) -> None:  # noqa: N802
        self.calls.append(("timeAdvanceGrant", (time,)))

    def federationHalted(self, cause: str, stalled_federate_handle: int) -> None:  # noqa: N802
        self.calls.append(("federationHalted", (cause, stalled_federate_handle)))

    def removeObjectInstance(  # noqa: N802
        self, object_handle: int, tag: bytes, timestamp: float | None
    ) -> None:
        self.calls.append(("removeObjectInstance", (object_handle, tag, timestamp)))

    def provideAttributeValueUpdate(  # noqa: N802
        self, object_handle: int, attribute_handles: tuple[int, ...], tag: bytes
    ) -> None:
        self.calls.append(
            ("provideAttributeValueUpdate", (object_handle, attribute_handles, tag))
        )

    def announceSynchronizationPoint(self, label: str, tag: bytes) -> None:  # noqa: N802
        self.calls.append(("announceSynchronizationPoint", (label, tag)))

    def federationSynchronized(  # noqa: N802
        self, label: str, failed_to_sync: tuple[int, ...] = ()
    ) -> None:
        self.calls.append(("federationSynchronized", (label,)))

    def requestAttributeOwnershipAssumption(  # noqa: N802
        self,
        object_handle: int,
        attribute_handles: tuple[int, ...],
        divesting_federate: int,
        tag: bytes,
    ) -> None:
        self.calls.append(
            (
                "requestAttributeOwnershipAssumption",
                (object_handle, attribute_handles, divesting_federate, tag),
            )
        )

    def attributeOwnershipAcquisitionNotification(  # noqa: N802
        self,
        object_handle: int,
        attribute_handles: tuple[int, ...],
        owning_federate: int,
    ) -> None:
        self.calls.append(
            (
                "attributeOwnershipAcquisitionNotification",
                (object_handle, attribute_handles, owning_federate),
            )
        )

    def requestDivestitureConfirmation(  # noqa: N802
        self, object_handle: int, attribute_handles: tuple[int, ...]
    ) -> None:
        self.calls.append(("requestDivestitureConfirmation", (object_handle, attribute_handles)))

    def initiateFederateSave(  # noqa: N802
        self, label: str, save_time: float | None
    ) -> None:
        self.calls.append(("initiateFederateSave", (label, save_time)))

    def federationSaved(self, label: str) -> None:  # noqa: N802
        self.calls.append(("federationSaved", (label,)))

    def federationNotSaved(self, label: str) -> None:  # noqa: N802
        self.calls.append(("federationNotSaved", (label,)))


class _FakeEventStream:
    """Stand-in for a Federate whose events() returns a fixed sequence."""

    def __init__(self, events: list[Any]) -> None:
        self._events = events

    def events(self) -> Any:
        async def gen() -> Any:
            for ev in self._events:
                yield ev

        return gen()


@pytest.mark.spec
def test_spec_m25_pump_events_dispatches_full_surface() -> None:
    """Every M25 event type produces a callback on the override slot."""
    events: list[Any] = [
        DiscoverObjectInstance(object_handle=1, class_name="Vehicle", instance_name="v1"),
        ReflectAttributeValues(object_handle=1, values={"Pos": b"\x00"}, timestamp=None),
        ReceiveInteraction(class_name="Honk", parameters={"Vol": b"\x05"}, timestamp=2.5),
        TimeAdvanceGrant(time=10.0),
        FederationHalted(cause="stall", stalled_federate_handle=7),
        RemoveObjectInstance(object_handle=1, tag=b"bye", timestamp=3.0),
        ProvideAttributeValueUpdate(object_handle=1, attribute_handles=(1, 2), tag=b"req"),
        SynchronizationPointAnnounced(label="phase1", tag=b"go", required_federates=(1, 2)),
        FederationSynchronized(label="phase1"),
        RequestAttributeOwnershipAssumption(
            object_handle=1, attribute_handles=(1,), divesting_federate=4, tag=b"hand"
        ),
        AttributeOwnershipAcquisitionNotification(
            object_handle=1, attribute_handles=(1,), owning_federate=2
        ),
        RequestDivestitureConfirmation(object_handle=1, attribute_handles=(1,)),
        InitiateFederateSave(label="ckpt", save_time=5.0),
        FederationSaved(label="ckpt"),
        FederationNotSaved(label="ckpt-fail"),
    ]
    amb = RecordingAmbassador()
    amb._federate = _FakeEventStream(events)  # type: ignore[assignment]
    amb._callback_target = amb

    asyncio.run(amb._pump_events())

    names = [c[0] for c in amb.calls]
    expected = [
        "discoverObjectInstance",
        "reflectAttributeValues",
        "receiveInteraction",
        "timeAdvanceGrant",
        "federationHalted",
        "removeObjectInstance",
        "provideAttributeValueUpdate",
        "announceSynchronizationPoint",
        "federationSynchronized",
        "requestAttributeOwnershipAssumption",
        "attributeOwnershipAcquisitionNotification",
        "requestDivestitureConfirmation",
        "initiateFederateSave",
        "federationSaved",
        "federationNotSaved",
    ]
    assert names == expected, f"dispatch order = {names!r}; want {expected!r}"

    payloads = dict(amb.calls)
    assert payloads["removeObjectInstance"] == (1, b"bye", 3.0)
    assert payloads["provideAttributeValueUpdate"] == (1, (1, 2), b"req")
    assert payloads["announceSynchronizationPoint"] == ("phase1", b"go")
    assert payloads["initiateFederateSave"] == ("ckpt", 5.0)


@pytest.mark.spec
def test_spec_m25_base_ambassador_callbacks_are_noops() -> None:
    """Base-class no-op callbacks don't raise.

    Each method is typed -> None, so asserting `== None` would be
    redundant under mypy --strict. The test value is "calling these
    on a fresh ambassador doesn't blow up" — the absence of an
    exception is the assertion.
    """
    amb = Rti1516eAmbassador()
    amb.removeObjectInstance(1, b"", None)
    amb.provideAttributeValueUpdate(1, (1,), b"")
    amb.announceSynchronizationPoint("x", b"")
    amb.federationSynchronized("x")
    amb.requestAttributeOwnershipAssumption(1, (1,), 2, b"")
    amb.attributeOwnershipAcquisitionNotification(1, (1,), 2)
    amb.requestDivestitureConfirmation(1, (1,))
    amb.initiateFederateSave("x", None)
    amb.federationSaved("x")
    amb.federationNotSaved("x")
