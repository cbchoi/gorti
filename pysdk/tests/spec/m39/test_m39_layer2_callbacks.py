"""M39 HA-1 — Layer-2 dispatch of the M37 wire-parity callbacks.

Contract asserted here:

  1. Every new ``rti1516e.events`` dataclass reaches its IEEE-named
     ambassador callback (§4.12 registration acks, §4.15 failed-set,
     §4.25-4.26 restore family, §5.10-5.13 advisories, §6.17-6.18
     scope, §7.10/§7.11 ownership, §8.22 retraction).
  2. Typed handles cross the Layer-2 boundary: advisories deliver
     ObjectClassHandle / InteractionClassHandle; retraction delivers
     MessageRetractionHandle + FederateHandle; discover/reflect deliver
     the typed forms ALONGSIDE the deprecated stringified ones.
  3. Back-compat: subclasses overriding discoverObjectInstance /
     reflectAttributeValues / federationSynchronized with the legacy
     (shorter) signatures still dispatch — the new arguments are only
     passed to overrides that accept them.
"""

from __future__ import annotations

import time
from typing import Any

import pytest

from rti1516e import Rti1516eAmbassador
from rti1516e import events as ev
from rti1516e.handles import (
    AttributeHandle,
    FederateHandle,
    InteractionClassHandle,
    MessageRetractionHandle,
    ObjectClassHandle,
)

# Spec-test-only fake (same consumption pattern as tests/spec/m4).
from tests.spec.m4._fakes import FakeRtiServer


@pytest.fixture
def fake_rti() -> FakeRtiServer:
    return FakeRtiServer()


class _Recorder(Rti1516eAmbassador):
    """Records every M39 callback with its payload (new signatures)."""

    def __init__(self) -> None:
        super().__init__()
        self.calls: list[tuple[str, tuple[Any, ...]]] = []

    def _rec(self, name: str, *payload: Any) -> None:
        self.calls.append((name, payload))

    # -- new-signature overrides ------------------------------------------

    def discoverObjectInstance(  # noqa: N802
        self,
        object_handle: int,
        class_name: str,
        instance_name: str,
        object_class: ObjectClassHandle | None = None,
    ) -> None:
        self._rec("discover", object_handle, class_name, instance_name, object_class)

    def reflectAttributeValues(  # noqa: N802
        self,
        object_handle: int,
        values: dict[str, Any],
        timestamp: float | None,
        attribute_values: dict[AttributeHandle, bytes] | None = None,
    ) -> None:
        self._rec("reflect", object_handle, values, timestamp, attribute_values)

    def synchronizationPointRegistrationSucceeded(self, label: str) -> None:  # noqa: N802
        self._rec("regSucceeded", label)

    def synchronizationPointRegistrationFailed(  # noqa: N802
        self, label: str, reason: ev.SynchronizationPointFailureReason | None
    ) -> None:
        self._rec("regFailed", label, reason)

    def federationSynchronized(  # noqa: N802
        self, label: str, failed_to_sync: tuple[int, ...] = ()
    ) -> None:
        self._rec("synchronized", label, failed_to_sync)

    def requestAttributeOwnershipRelease(  # noqa: N802
        self, object_handle: int, attribute_handles: tuple[int, ...], tag: bytes
    ) -> None:
        self._rec("release", object_handle, attribute_handles, tag)

    def attributeOwnershipUnavailable(  # noqa: N802
        self, object_handle: int, attribute_handles: tuple[int, ...]
    ) -> None:
        self._rec("unavailable", object_handle, attribute_handles)

    def initiateFederateRestore(  # noqa: N802
        self, label: str, federate_handle: int, federate_name: str
    ) -> None:
        self._rec("initRestore", label, federate_handle, federate_name)

    def federationRestored(self, label: str) -> None:  # noqa: N802
        self._rec("restored", label)

    def federationNotRestored(self, label: str) -> None:  # noqa: N802
        self._rec("notRestored", label)

    def requestFederationRestoreSucceeded(self, label: str) -> None:  # noqa: N802
        self._rec("restoreReqOk", label)

    def requestFederationRestoreFailed(self, label: str, reason: str) -> None:  # noqa: N802
        self._rec("restoreReqFail", label, reason)

    def federationRestoreBegun(self) -> None:  # noqa: N802
        self._rec("restoreBegun")

    def startRegistrationForObjectClass(  # noqa: N802
        self, object_class_handle: ObjectClassHandle
    ) -> None:
        self._rec("startReg", object_class_handle)

    def stopRegistrationForObjectClass(  # noqa: N802
        self, object_class_handle: ObjectClassHandle
    ) -> None:
        self._rec("stopReg", object_class_handle)

    def turnInteractionsOn(  # noqa: N802
        self, interaction_class_handle: InteractionClassHandle
    ) -> None:
        self._rec("intOn", interaction_class_handle)

    def turnInteractionsOff(  # noqa: N802
        self, interaction_class_handle: InteractionClassHandle
    ) -> None:
        self._rec("intOff", interaction_class_handle)

    def attributesInScope(  # noqa: N802
        self, object_handle: int, attribute_handles: tuple[int, ...]
    ) -> None:
        self._rec("inScope", object_handle, attribute_handles)

    def attributesOutOfScope(  # noqa: N802
        self, object_handle: int, attribute_handles: tuple[int, ...]
    ) -> None:
        self._rec("outOfScope", object_handle, attribute_handles)

    def requestRetraction(  # noqa: N802
        self,
        retraction_handle: MessageRetractionHandle,
        sender_federate: FederateHandle,
    ) -> None:
        self._rec("retract", retraction_handle, sender_federate)


def _drive(fake_rti: FakeRtiServer, amb: Rti1516eAmbassador, events: list[Any]) -> None:
    """Join, push ``events`` at the joined federate, pump, resign."""
    amb.connect(amb, "memory://fake-rti")
    try:
        amb.createFederationExecution("demo", [])
        amb.joinFederationExecution("alice", "demo")
        fed = amb._federate  # noqa: SLF001 — test introspection
        assert fed is not None
        handle = fed.handle
        for event in events:
            fake_rti.push_event(handle, event)
        deadline = time.monotonic() + 5.0
        recorder_calls = getattr(amb, "calls", None)
        while time.monotonic() < deadline:
            amb.evokeMultipleCallbacks(0.01, 0.1)
            if recorder_calls is not None and len(recorder_calls) >= len(events):
                break
            if recorder_calls is None and amb._callback_fired_count >= len(events):  # noqa: SLF001
                break
        amb.resignFederationExecution()
    finally:
        amb.disconnect()


def test_all_m39_events_reach_their_ieee_callbacks(fake_rti: FakeRtiServer) -> None:
    """1+2 — each new event dispatches to its IEEE-named callback with
    field-faithful, typed payloads."""
    amb = _Recorder()
    _drive(
        fake_rti,
        amb,
        [
            ev.SynchronizationPointRegistrationSucceeded(label="ready"),
            ev.SynchronizationPointRegistrationFailed(
                label="dup",
                reason=ev.SynchronizationPointFailureReason.SYNCHRONIZATION_POINT_LABEL_NOT_UNIQUE,
            ),
            ev.FederationSynchronized(label="gate", failed_to_sync=(4,)),
            ev.RequestAttributeOwnershipRelease(
                object_handle=9, attribute_handles=(1, 2), tag=b"please"
            ),
            ev.AttributeOwnershipUnavailable(object_handle=9, attribute_handles=(3,)),
            ev.InitiateFederateRestore(
                label="cp1", federate_handle=3, federate_name="alice"
            ),
            ev.FederationRestored(label="cp1"),
            ev.FederationNotRestored(label="cp2"),
            ev.RequestFederationRestoreSucceeded(label="cp1"),
            ev.RequestFederationRestoreFailed(label="cp3", reason="unknown label"),
            ev.FederationRestoreBegun(),
            ev.StartRegistrationForObjectClass(object_class_handle=2),
            ev.StopRegistrationForObjectClass(object_class_handle=2),
            ev.TurnInteractionsOn(interaction_class_handle=5),
            ev.TurnInteractionsOff(interaction_class_handle=5),
            ev.AttributesInScope(object_handle=8, attribute_handles=(1,)),
            ev.AttributesOutOfScope(object_handle=8, attribute_handles=(1,)),
            ev.RequestRetraction(sender_federate=2, retraction_handle=41),
        ],
    )
    got = dict(amb.calls)  # callback-name -> last payload
    assert got["regSucceeded"] == ("ready",)
    assert got["regFailed"] == (
        "dup",
        ev.SynchronizationPointFailureReason.SYNCHRONIZATION_POINT_LABEL_NOT_UNIQUE,
    )
    assert got["synchronized"] == ("gate", (4,))
    assert got["release"] == (9, (1, 2), b"please")
    assert got["unavailable"] == (9, (3,))
    assert got["initRestore"] == ("cp1", 3, "alice")
    assert got["restored"] == ("cp1",)
    assert got["notRestored"] == ("cp2",)
    assert got["restoreReqOk"] == ("cp1",)
    assert got["restoreReqFail"] == ("cp3", "unknown label")
    assert got["restoreBegun"] == ()
    # §5.10-5.13 advisories deliver TYPED class handles.
    assert got["startReg"] == (2,) and isinstance(got["startReg"][0], ObjectClassHandle)
    assert got["stopReg"] == (2,) and isinstance(got["stopReg"][0], ObjectClassHandle)
    assert got["intOn"] == (5,) and isinstance(got["intOn"][0], InteractionClassHandle)
    assert got["intOff"] == (5,) and isinstance(got["intOff"][0], InteractionClassHandle)
    assert got["inScope"] == (8, (1,))
    assert got["outOfScope"] == (8, (1,))
    # §8.22 delivers typed retraction + federate handles.
    assert got["retract"] == (41, 2)
    assert isinstance(got["retract"][0], MessageRetractionHandle)
    assert isinstance(got["retract"][1], FederateHandle)


def test_typed_discover_and_reflect_reach_new_overrides(
    fake_rti: FakeRtiServer,
) -> None:
    """2 — overrides that accept the M39 typed arguments receive them."""
    amb = _Recorder()
    _drive(
        fake_rti,
        amb,
        [
            ev.DiscoverObjectInstance(
                object_handle=7,
                class_name="2",
                instance_name="V1",
                object_class=ObjectClassHandle(2),
            ),
            ev.ReflectAttributeValues(
                object_handle=7,
                values={"1": b"\x01"},
                timestamp=None,
                attribute_values={AttributeHandle(1): b"\x01"},
            ),
        ],
    )
    got = dict(amb.calls)
    assert got["discover"] == (7, "2", "V1", ObjectClassHandle(2))
    assert isinstance(got["discover"][3], ObjectClassHandle)
    assert got["reflect"] == (7, {"1": b"\x01"}, None, {AttributeHandle(1): b"\x01"})
    assert all(isinstance(k, AttributeHandle) for k in got["reflect"][3])


def test_legacy_short_signature_overrides_still_dispatch(
    fake_rti: FakeRtiServer,
) -> None:
    """3 — pre-M39 subclasses (shorter callback signatures) keep working;
    the dispatcher withholds the arguments they don't accept."""

    class _Legacy(Rti1516eAmbassador):
        def __init__(self) -> None:
            super().__init__()
            self.calls: list[tuple[str, tuple[Any, ...]]] = []

        # The short signatures below are the POINT of this test (the
        # pre-M39 override shape) — silence mypy's [override] check.

        def discoverObjectInstance(  # type: ignore[override]  # noqa: N802
            self, object_handle: int, class_name: str, instance_name: str
        ) -> None:
            self.calls.append(("discover", (object_handle, class_name, instance_name)))

        def reflectAttributeValues(  # type: ignore[override]  # noqa: N802
            self,
            object_handle: int,
            values: dict[str, Any],
            timestamp: float | None,
        ) -> None:
            self.calls.append(("reflect", (object_handle, values, timestamp)))

        def federationSynchronized(self, label: str) -> None:  # type: ignore[override]  # noqa: N802
            self.calls.append(("synchronized", (label,)))

    amb = _Legacy()
    _drive(
        fake_rti,
        amb,
        [
            ev.DiscoverObjectInstance(
                object_handle=7,
                class_name="2",
                instance_name="V1",
                object_class=ObjectClassHandle(2),
            ),
            ev.ReflectAttributeValues(
                object_handle=7,
                values={"1": b"\x01"},
                timestamp=1.5,
                attribute_values={AttributeHandle(1): b"\x01"},
            ),
            ev.FederationSynchronized(label="gate", failed_to_sync=(4,)),
        ],
    )
    got = dict(amb.calls)
    assert got["discover"] == (7, "2", "V1")
    assert got["reflect"] == (7, {"1": b"\x01"}, 1.5)
    assert got["synchronized"] == ("gate",)
