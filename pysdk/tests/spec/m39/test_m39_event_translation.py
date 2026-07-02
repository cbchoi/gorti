"""M39 HA-1 — full M37 FederateEvent translation coverage.

Contract asserted here (IEEE 1516.1-2010 § references per callback):

  1. Every FederateEvent oneof variant on the M37 wire translates to a
     typed ``rti1516e.events`` dataclass — no silent drops. Includes
     the M37 additions (tags 22/23 sync-registration acks, 33/34
     ownership release/unavailable, 46/47/48 restore acks+begun, 60-63
     registration/interaction advisories, 64/65 DDM scope, 66
     retraction) AND the pre-M37 restore family (43/44/45) that
     ``_translate_event`` silently dropped until M39.
  2. The exhaustiveness is future-proof: a wire variant WITHOUT a
     translation branch trips a once-per-variant RuntimeWarning instead
     of vanishing (so the NEXT wire addition can't silently disappear
     either).
  3. Field mapping fidelity: labels, handles, tags, reasons and sets
     survive the proto -> dataclass hop.
"""

from __future__ import annotations

import warnings
from typing import Any

import pytest

from rti1516e import events as ev
from rti1516e._transport import (
    _UNTRANSLATED_EVENT_WARNED,
    GrpcTransport,
    _ensure_generated_path,
)
from rti1516e.handles import AttributeHandle, ObjectClassHandle

_ensure_generated_path()
stream_pb2 = pytest.importorskip("rti.v1.stream_pb2")


def _transport() -> GrpcTransport:
    """A GrpcTransport shell for exercising _translate_event.

    ``_translate_event`` only consults instance state on the
    ``receive`` branch (interaction name tables); every other branch is
    pure. Bypass __init__ (which needs a live channel) and provide the
    tables the receive branch reads.
    """
    t = GrpcTransport.__new__(GrpcTransport)
    t._inverse_interaction_handles = {}
    t._interaction_handles = {}
    return t


def _translate(event: Any) -> Any:
    return _transport()._translate_event(event)


# --- 1+3: per-variant field-fidelity translations ---------------------------


def test_sync_registration_succeeded_translates() -> None:
    """§4.12 — tag 22 -> SynchronizationPointRegistrationSucceeded."""
    got = _translate(
        stream_pb2.FederateEvent(
            seq=1,
            sync_registration_succeeded=stream_pb2.SyncRegistrationSucceeded(
                label="ready"
            ),
        )
    )
    assert got == ev.SynchronizationPointRegistrationSucceeded(label="ready")


@pytest.mark.parametrize(
    ("wire_reason", "expected"),
    [
        (1, ev.SynchronizationPointFailureReason.SYNCHRONIZATION_POINT_LABEL_NOT_UNIQUE),
        (2, ev.SynchronizationPointFailureReason.SYNCHRONIZATION_SET_MEMBER_NOT_JOINED),
        (0, None),
    ],
)
def test_sync_registration_failed_translates(
    wire_reason: int, expected: ev.SynchronizationPointFailureReason | None
) -> None:
    """§4.12 — tag 23 -> SynchronizationPointRegistrationFailed with the
    IEEE SynchronizationPointFailureReason mapped (unspecified -> None)."""
    got = _translate(
        stream_pb2.FederateEvent(
            seq=2,
            sync_registration_failed=stream_pb2.SyncRegistrationFailed(
                label="dup", reason=wire_reason
            ),
        )
    )
    assert got == ev.SynchronizationPointRegistrationFailed(label="dup", reason=expected)


def test_federation_synchronized_carries_failed_to_sync() -> None:
    """§4.15 — federationSynchronized carries the failed-to-sync set."""
    got = _translate(
        stream_pb2.FederateEvent(
            seq=3,
            sync_synchronized=stream_pb2.FederationSynchronized(
                label="gate", failed_to_sync=[4, 7]
            ),
        )
    )
    assert got == ev.FederationSynchronized(label="gate", failed_to_sync=(4, 7))


def test_ownership_release_requested_translates() -> None:
    """§7.11 — tag 33 -> RequestAttributeOwnershipRelease with tag bytes."""
    got = _translate(
        stream_pb2.FederateEvent(
            seq=4,
            ownership_release_requested=stream_pb2.RequestAttributeOwnershipRelease(
                object_handle=9, attribute_handles=[1, 2], tag=b"please"
            ),
        )
    )
    assert got == ev.RequestAttributeOwnershipRelease(
        object_handle=9, attribute_handles=(1, 2), tag=b"please"
    )


def test_ownership_unavailable_translates() -> None:
    """§7.10 — tag 34 -> AttributeOwnershipUnavailable."""
    got = _translate(
        stream_pb2.FederateEvent(
            seq=5,
            ownership_unavailable=stream_pb2.AttributeOwnershipUnavailable(
                object_handle=9, attribute_handles=[3]
            ),
        )
    )
    assert got == ev.AttributeOwnershipUnavailable(
        object_handle=9, attribute_handles=(3,)
    )


def test_restore_family_translates() -> None:
    """§4.25/§4.26 + restore outcomes — tags 43-48 all translate.

    43/44/45 predate M37 (M17.25) but were dropped by the pre-M39
    switch; asserted here alongside the M37 acks.
    """
    assert _translate(
        stream_pb2.FederateEvent(
            seq=6,
            restore_initiate=stream_pb2.InitiateFederateRestore(
                label="cp1", federate_handle=3, federate_name="alice"
            ),
        )
    ) == ev.InitiateFederateRestore(label="cp1", federate_handle=3, federate_name="alice")
    assert _translate(
        stream_pb2.FederateEvent(
            seq=7, restore_completed=stream_pb2.FederationRestored(label="cp1")
        )
    ) == ev.FederationRestored(label="cp1")
    assert _translate(
        stream_pb2.FederateEvent(
            seq=8, restore_failed=stream_pb2.FederationNotRestored(label="cp1")
        )
    ) == ev.FederationNotRestored(label="cp1")
    assert _translate(
        stream_pb2.FederateEvent(
            seq=9,
            restore_request_succeeded=stream_pb2.RequestFederationRestoreSucceeded(
                label="cp1"
            ),
        )
    ) == ev.RequestFederationRestoreSucceeded(label="cp1")
    assert _translate(
        stream_pb2.FederateEvent(
            seq=10,
            restore_request_failed=stream_pb2.RequestFederationRestoreFailed(
                label="cp1", reason="unknown label"
            ),
        )
    ) == ev.RequestFederationRestoreFailed(label="cp1", reason="unknown label")
    assert _translate(
        stream_pb2.FederateEvent(
            seq=11, restore_begun=stream_pb2.FederationRestoreBegun()
        )
    ) == ev.FederationRestoreBegun()


def test_registration_and_interaction_advisories_translate() -> None:
    """§5.10-§5.13 — tags 60-63 -> advisory events."""
    assert _translate(
        stream_pb2.FederateEvent(
            seq=12,
            start_registration=stream_pb2.StartRegistrationForObjectClass(
                object_class_handle=2
            ),
        )
    ) == ev.StartRegistrationForObjectClass(object_class_handle=2)
    assert _translate(
        stream_pb2.FederateEvent(
            seq=13,
            stop_registration=stream_pb2.StopRegistrationForObjectClass(
                object_class_handle=2
            ),
        )
    ) == ev.StopRegistrationForObjectClass(object_class_handle=2)
    assert _translate(
        stream_pb2.FederateEvent(
            seq=14,
            turn_interactions_on=stream_pb2.TurnInteractionsOn(
                interaction_class_handle=5
            ),
        )
    ) == ev.TurnInteractionsOn(interaction_class_handle=5)
    assert _translate(
        stream_pb2.FederateEvent(
            seq=15,
            turn_interactions_off=stream_pb2.TurnInteractionsOff(
                interaction_class_handle=5
            ),
        )
    ) == ev.TurnInteractionsOff(interaction_class_handle=5)


def test_ddm_scope_advisories_translate() -> None:
    """§6.17/§6.18 — tags 64/65 -> AttributesIn/OutOfScope."""
    assert _translate(
        stream_pb2.FederateEvent(
            seq=16,
            attributes_in_scope=stream_pb2.AttributesInScope(
                object_handle=8, attribute_handles=[1, 2]
            ),
        )
    ) == ev.AttributesInScope(object_handle=8, attribute_handles=(1, 2))
    assert _translate(
        stream_pb2.FederateEvent(
            seq=17,
            attributes_out_of_scope=stream_pb2.AttributesOutOfScope(
                object_handle=8, attribute_handles=[2]
            ),
        )
    ) == ev.AttributesOutOfScope(object_handle=8, attribute_handles=(2,))


def test_request_retraction_translates() -> None:
    """§8.22 — tag 66 -> RequestRetraction with the (sender, handle) pair."""
    got = _translate(
        stream_pb2.FederateEvent(
            seq=18,
            retraction_requested=stream_pb2.RequestRetraction(
                sender_federate=2, message_retraction_handle=41
            ),
        )
    )
    assert got == ev.RequestRetraction(sender_federate=2, retraction_handle=41)


def test_discover_carries_typed_object_class() -> None:
    """§6.9 — discover delivers the typed ObjectClassHandle alongside the
    deprecated stringified class_name (M39 typed-handle parity)."""
    got = _translate(
        stream_pb2.FederateEvent(
            seq=19,
            discover=stream_pb2.DiscoverObjectInstance(
                object_handle=7, object_class_handle=2, object_name="V1"
            ),
        )
    )
    assert got.object_class == ObjectClassHandle(2)
    assert isinstance(got.object_class, ObjectClassHandle)
    assert got.class_name == "2"  # deprecated back-compat form


def test_reflect_carries_typed_attribute_values() -> None:
    """§6.11 — reflect keys the typed map by AttributeHandle alongside the
    deprecated string-keyed map (M39 typed-handle parity)."""
    got = _translate(
        stream_pb2.FederateEvent(
            seq=20,
            reflect=stream_pb2.ReflectAttributeValues(
                object_handle=7, object_class_handle=2, attributes={1: b"\x01"}
            ),
        )
    )
    assert got.attribute_values == {AttributeHandle(1): b"\x01"}
    assert all(isinstance(k, AttributeHandle) for k in got.attribute_values)
    assert got.values == {"1": b"\x01"}  # deprecated back-compat form


# --- 2: exhaustiveness + warn-once default ----------------------------------


def test_every_wire_variant_translates() -> None:
    """No silent drops: EVERY FederateEvent oneof variant in the current
    generated stubs must yield a typed event. When the wire grows a new
    tag and stubs are regenerated, this test fails until the branch is
    added."""
    oneof = stream_pb2.FederateEvent.DESCRIPTOR.oneofs_by_name["event"]
    missing: list[str] = []
    for field in oneof.fields:
        event = stream_pb2.FederateEvent(seq=100)
        getattr(event, field.name).SetInParent()
        with warnings.catch_warnings():
            warnings.simplefilter("error")  # a warn here means "untranslated"
            try:
                translated = _translate(event)
            except RuntimeWarning:
                translated = None
        if translated is None:
            missing.append(field.name)
    assert not missing, (
        f"FederateEvent variants dropped by _translate_event: {missing}"
    )


def test_unknown_variant_warns_once_then_drops_quietly() -> None:
    """A variant without a branch warns ONCE (RuntimeWarning naming the
    tag) and returns None; repeats stay quiet."""

    class _AlienEvent:
        seq = 41

        def WhichOneof(self, _name: str) -> str:  # noqa: N802 — protobuf API shape
            return "m39_test_alien_variant"

    _UNTRANSLATED_EVENT_WARNED.discard("m39_test_alien_variant")
    with pytest.warns(RuntimeWarning, match="m39_test_alien_variant"):
        assert _translate(_AlienEvent()) is None
    # Second occurrence: no further warning.
    with warnings.catch_warnings():
        warnings.simplefilter("error")
        assert _translate(_AlienEvent()) is None
