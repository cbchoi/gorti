"""Shared driver helpers for the IVCT-inspired conformance subset.

Two access levels:

- :class:`Recorder` — a ``Rti1516eAmbassador`` subclass that records every
  callback thread-safely and offers deadline-based waiting. This is the
  primary instrument: tests assert on the recorded callback stream.
- ``raw_*`` helpers — thin synchronous gRPC stubs over the generated
  ``rti.v1`` protos, used ONLY where the pysdk deliberately papers over
  wire semantics the tests must observe (Layer 1 rolls create+join into
  one idempotent call and swallows ALREADY_EXISTS; it never exposes
  DestroyFederation; the ownership client lacks the M37 ``two_phase`` /
  ``if_available`` request flags and the ConfirmDivestiture RPC).
  Each raw call site names the pysdk gap it works around.

Not a report file — this is test infrastructure imported by tc_*.py.
"""

from __future__ import annotations

import sys
import threading
import time
from pathlib import Path
from typing import Any

HERE = Path(__file__).resolve().parent
REPO_ROOT = HERE.parents[3]
PYSDK = REPO_ROOT / "pysdk"
GENERATED = PYSDK / "rti1516e" / "_generated"
for _p in (str(PYSDK), str(GENERATED)):
    if _p not in sys.path:
        sys.path.insert(0, _p)

# ruff: noqa: E402
from rti1516e import Rti1516eAmbassador

FOM = str(HERE / "federation.fom.xml")

#: Default per-wait deadline. Local rtid round-trips are sub-millisecond;
#: 5s absorbs CI scheduling noise without slowing green runs (waits
#: return as soon as the predicate holds).
WAIT = 5.0

#: Window used to assert an event did NOT arrive. Kept short: every
#: negative wait costs this much wall-clock on every run.
QUIET = 0.7


class Recorder(Rti1516eAmbassador):
    """Ambassador that records every callback for later assertion.

    Callbacks fire on the ambassador's private event-pump thread; the
    recording list is guarded by a lock and waiters use a Condition so
    tests can block until a predicate over the recorded stream holds.
    """

    def __init__(self) -> None:
        super().__init__()
        self._cv = threading.Condition()
        #: list of (callback_name, payload_dict) in arrival order.
        self.calls: list[tuple[str, dict[str, Any]]] = []

    def _record(self, name: str, **payload: Any) -> None:
        with self._cv:
            self.calls.append((name, payload))
            self._cv.notify_all()

    # --- waiting / querying --------------------------------------------

    def snapshot(self, name: str | None = None) -> list[tuple[str, dict[str, Any]]]:
        """Copy of the recorded stream, optionally filtered by callback name."""
        with self._cv:
            if name is None:
                return list(self.calls)
            return [c for c in self.calls if c[0] == name]

    def wait_for(
        self, name: str, *, timeout: float = WAIT, **match: Any
    ) -> dict[str, Any]:
        """Block until a ``name`` callback whose payload contains ``match``
        arrives; return its payload. Raises TimeoutError with the full
        recorded stream on expiry (so failures are diagnosable)."""

        def _find() -> dict[str, Any] | None:
            for cb_name, payload in self.calls:
                if cb_name != name:
                    continue
                if all(payload.get(k) == v for k, v in match.items()):
                    return payload
            return None

        deadline = time.monotonic() + timeout
        with self._cv:
            while True:
                found = _find()
                if found is not None:
                    return found
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise TimeoutError(
                        f"no {name!r} callback matching {match!r} within "
                        f"{timeout}s; recorded: {self.calls!r}"
                    )
                self._cv.wait(remaining)

    def count(self, name: str) -> int:
        with self._cv:
            return sum(1 for c in self.calls if c[0] == name)

    def assert_quiet(self, name: str, *, window: float = QUIET) -> None:
        """Assert no ``name`` callback arrives within ``window`` seconds."""
        time.sleep(window)
        with self._cv:
            got = [c for c in self.calls if c[0] == name]
        assert not got, f"expected no {name!r} callback, got {got!r}"

    # --- recorded callback overrides -------------------------------------

    def discoverObjectInstance(  # noqa: N802
        self, object_handle: int, class_name: str, instance_name: str
    ) -> None:
        self._record(
            "discoverObjectInstance",
            object_handle=object_handle,
            class_name=class_name,
            instance_name=instance_name,
        )

    def reflectAttributeValues(  # noqa: N802
        self, object_handle: int, values: dict[str, Any], timestamp: float | None
    ) -> None:
        self._record(
            "reflectAttributeValues",
            object_handle=object_handle,
            values=values,
            timestamp=timestamp,
        )

    def receiveInteraction(  # noqa: N802
        self, class_name: str, parameters: dict[str, Any], timestamp: float | None
    ) -> None:
        self._record(
            "receiveInteraction",
            class_name=class_name,
            parameters=parameters,
            timestamp=timestamp,
        )

    def removeObjectInstance(  # noqa: N802
        self, object_handle: int, tag: bytes, timestamp: float | None
    ) -> None:
        self._record(
            "removeObjectInstance",
            object_handle=object_handle,
            tag=tag,
            timestamp=timestamp,
        )

    def timeAdvanceGrant(self, time: float) -> None:  # noqa: N802
        self._record("timeAdvanceGrant", time=time)

    def announceSynchronizationPoint(self, label: str, tag: bytes) -> None:  # noqa: N802
        self._record("announceSynchronizationPoint", label=label, tag=tag)

    def federationSynchronized(self, label: str) -> None:  # noqa: N802
        self._record("federationSynchronized", label=label)

    def synchronizationPointRegistrationSucceeded(self, label: str) -> None:  # noqa: N802
        self._record("synchronizationPointRegistrationSucceeded", label=label)

    def requestAttributeOwnershipAssumption(  # noqa: N802
        self,
        object_handle: int,
        attribute_handles: tuple[int, ...],
        divesting_federate: int,
        tag: bytes,
    ) -> None:
        self._record(
            "requestAttributeOwnershipAssumption",
            object_handle=object_handle,
            attribute_handles=attribute_handles,
            divesting_federate=divesting_federate,
            tag=tag,
        )

    def attributeOwnershipAcquisitionNotification(  # noqa: N802
        self,
        object_handle: int,
        attribute_handles: tuple[int, ...],
        owning_federate: int,
    ) -> None:
        self._record(
            "attributeOwnershipAcquisitionNotification",
            object_handle=object_handle,
            attribute_handles=attribute_handles,
            owning_federate=owning_federate,
        )

    def requestDivestitureConfirmation(  # noqa: N802
        self, object_handle: int, attribute_handles: tuple[int, ...]
    ) -> None:
        self._record(
            "requestDivestitureConfirmation",
            object_handle=object_handle,
            attribute_handles=attribute_handles,
        )

    def objectInstanceNameReservationSucceeded(self, object_name: str) -> None:  # noqa: N802
        self._record("objectInstanceNameReservationSucceeded", object_name=object_name)

    def objectInstanceNameReservationFailed(self, object_name: str) -> None:  # noqa: N802
        self._record("objectInstanceNameReservationFailed", object_name=object_name)

    def provideAttributeValueUpdate(  # noqa: N802
        self, object_handle: int, attribute_handles: tuple[int, ...], tag: bytes
    ) -> None:
        self._record(
            "provideAttributeValueUpdate",
            object_handle=object_handle,
            attribute_handles=attribute_handles,
            tag=tag,
        )


def join(url: str, federation: str, federate: str) -> Recorder:
    """Connect + create/join in one step. Caller must :func:`leave`."""
    amb = Recorder()
    amb.connect(amb, url)
    try:
        amb.createFederationExecution(federation, [FOM])
        amb.joinFederationExecution(federate, federation)
    except BaseException:
        amb.disconnect()
        raise
    return amb


def leave(amb: Recorder, action: str | None = None) -> None:
    """Resign (best-effort) + disconnect. Safe to call twice."""
    try:
        if amb._federate is not None:  # noqa: SLF001 — teardown introspection
            if action is None:
                amb.resignFederationExecution()
            else:
                amb.resignFederationExecution(action)
    finally:
        amb.disconnect()


def federate_handle(amb: Recorder) -> int:
    """The joined federate's wire handle (Layer-1 surface)."""
    fed = amb._federate  # noqa: SLF001 — Layer 2 exposes no §4.9 return value
    assert fed is not None, "not joined"
    return int(fed.handle)


# --- raw wire helpers (sync gRPC over the generated stubs) -----------------
#
# Used only where the pysdk cannot express the wire semantics under test.
# Every helper takes the plain host:port target derived from the fixture
# grpc:// URL.


def target_of(url: str) -> str:
    assert url.startswith("grpc://"), url
    return url[len("grpc://") :]


def raw_channel(url: str) -> Any:
    import grpc

    return grpc.insecure_channel(target_of(url))


def raw_federation_stub(channel: Any) -> Any:
    from rti.v1 import federation_pb2_grpc

    return federation_pb2_grpc.FederationServiceStub(channel)


def raw_ownership_stub(channel: Any) -> Any:
    from rti.v1 import ownership_pb2_grpc

    return ownership_pb2_grpc.OwnershipServiceStub(channel)


def fom_modules_proto() -> list[Any]:
    from rti.v1 import common_pb2

    return [common_pb2.FOMModule(path=FOM, xml=Path(FOM).read_bytes())]


def raw_create(stub: Any, federation: str) -> Any:
    from rti.v1 import common_pb2, federation_pb2

    return stub.CreateFederation(
        federation_pb2.CreateFederationRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=federation,
            fom_modules=fom_modules_proto(),
        )
    )


def raw_join(stub: Any, federation: str, federate: str) -> int:
    from rti.v1 import common_pb2, federation_pb2

    resp = stub.JoinFederation(
        federation_pb2.JoinFederationRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=federation,
            federate_name=federate,
        )
    )
    return int(resp.federate_handle)


def raw_resign(stub: Any, federation: str, federate_handle: int) -> None:
    from rti.v1 import common_pb2, federation_pb2

    stub.ResignFederation(
        federation_pb2.ResignFederationRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=federation,
            federate_handle=federate_handle,
            # RESIGN_ACTION_UNSPECIFIED is rejected outright
            # ("resign action not supported"); use the §4.10 default.
            action=common_pb2.ResignAction.RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES,
        )
    )


def raw_destroy(stub: Any, federation: str) -> None:
    from rti.v1 import common_pb2, federation_pb2

    stub.DestroyFederation(
        federation_pb2.DestroyFederationRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=federation,
        )
    )


def raw_negotiated_divest_two_phase(
    stub: Any,
    federation: str,
    federate_handle: int,
    object_handle: int,
    attribute_handles: list[int],
    tag: bytes = b"",
) -> None:
    """§7.3 negotiated divest with the M37 two_phase flag.

    pysdk gap: rti1516e.ownership.OwnershipClient.negotiated_divest never
    sets NegotiatedDivestRequest.two_phase (proto tag 7), so Layer 2 can
    only drive the pre-M37 one-phase flow.
    """
    from rti.v1 import common_pb2, ownership_pb2

    stub.NegotiatedAttributeOwnershipDivestiture(
        ownership_pb2.NegotiatedDivestRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=federation,
            federate_handle=federate_handle,
            object_handle=object_handle,
            attribute_handles=attribute_handles,
            tag=tag,
            two_phase=True,
        )
    )


def raw_confirm_divestiture(
    stub: Any,
    federation: str,
    federate_handle: int,
    object_handle: int,
    attribute_handles: list[int],
) -> None:
    """§7.6 confirmDivestiture.

    pysdk gap: OwnershipClient has no confirm_divestiture wrapper for the
    M37 ConfirmDivestiture RPC; Layer 2 cannot complete a two-phase divest.
    """
    from rti.v1 import common_pb2, ownership_pb2

    stub.ConfirmDivestiture(
        ownership_pb2.ConfirmDivestitureRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=federation,
            federate_handle=federate_handle,
            object_handle=object_handle,
            attribute_handles=attribute_handles,
        )
    )


def raw_acquire_if_available(
    stub: Any,
    federation: str,
    federate_handle: int,
    object_handle: int,
    attribute_handles: list[int],
    tag: bytes = b"",
) -> None:
    """§7.8 attributeOwnershipAcquisitionIfAvailable.

    pysdk gap: OwnershipClient.acquire never sets
    AcquireRequest.if_available (proto tag 7), so Layer 2 always queues a
    pending acquire instead of the §7.8 grab-only-available semantics.
    """
    from rti.v1 import common_pb2, ownership_pb2

    stub.AttributeOwnershipAcquisition(
        ownership_pb2.AcquireRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=federation,
            federate_handle=federate_handle,
            object_handle=object_handle,
            attribute_handles=attribute_handles,
            tag=tag,
            if_available=True,
        )
    )
