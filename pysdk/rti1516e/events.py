"""Typed event dataclasses yielded by Federate.events().

Agent C wires the closed-set dispatch in TASK-067; the dataclass
definitions here are FROZEN-shape so spec tests can match against them.

Use a discriminated union via ``isinstance`` (or ``match`` in 3.10+):

    async for event in fed.events():
        match event:
            case ReflectAttributeValues(obj, values, ts): ...
            case ReceiveInteraction(cls, params, ts): ...
            case TimeAdvanceGrant(t): ...
            case DiscoverObjectInstance(obj, cls, name): ...
            case SynchronizationPointAnnounced(label, tag, required): ...
            case FederationSynchronized(label): ...
            case RequestAttributeOwnershipAssumption(obj, attrs, divester, tag): ...
            case AttributeOwnershipAcquisitionNotification(obj, attrs, owning): ...
            case RequestDivestitureConfirmation(obj, attrs): ...
            case InitiateFederateSave(label, save_time): ...
            case FederationSaved(label): ...
            case FederationNotSaved(label): ...
            case FederationHalted(cause, federate_handle): ...
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any


@dataclass(frozen=True)
class DiscoverObjectInstance:
    """A new object instance was registered by another federate."""

    object_handle: int
    class_name: str
    instance_name: str


@dataclass(frozen=True)
class ReflectAttributeValues:
    """Attribute values updated on an object we subscribe to."""

    object_handle: int
    values: dict[str, Any]
    timestamp: float | None  # None for RO; non-None for TSO


@dataclass(frozen=True)
class ReceiveInteraction:
    """An interaction we subscribe to was sent by another federate."""

    class_name: str
    parameters: dict[str, Any]
    timestamp: float | None


@dataclass(frozen=True)
class TimeAdvanceGrant:
    """The RTI granted our pending NER. Federate is now at ``time``."""

    time: float


@dataclass(frozen=True)
class FederationHalted:
    """The federation has been halted (e.g. by stall timeout).

    No further calls on the Federate will succeed; close the connection.
    Named to match the M3 event type emitted from the Go side.
    """

    cause: str  # "stall" | other future causes
    stalled_federate_handle: int


@dataclass(frozen=True)
class RemoveObjectInstance:
    """The object instance has been deleted by its owner (M23, IEEE 1516.1 §6.16)."""

    object_handle: int
    tag: bytes
    timestamp: float | None  # None for RO; non-None for TSO delete


# --- Cut-2 service-group callbacks (M12 W2 deferral #1 close) -------------
# Wire-side proto variants live on rti.v1.FederateEvent at oneof tags
# 20/21 (sync), 30/31/32 (ownership), 40/41/42 (save). The dataclasses
# here are the typed Python mirrors yielded by Federate.events() once
# the GrpcTransport's _translate_event resolves the matching oneof.


@dataclass(frozen=True)
class SynchronizationPointAnnounced:
    """A sync point was registered (§4.6).

    Fires on every joined federate (when the implicit "all members"
    set was used at registration) or every required federate (when an
    explicit set was passed).
    """

    label: str
    tag: bytes
    # Empty when the implicit all-members set was used.
    required_federates: tuple[int, ...] = field(default_factory=tuple)


@dataclass(frozen=True)
class FederationSynchronized:
    """All required federates achieved a sync point (§4.7).

    Fires on every required federate. Federates that were waiting on
    the achieve callback should now resume.
    """

    label: str


@dataclass(frozen=True)
class RequestAttributeOwnershipAssumption:
    """A negotiated divest is in progress; this federate may take over (§7.3).

    Fires on every subscriber of the attribute when the current owner
    calls negotiated_divest. The receiving federate may call
    ``acquire`` to take ownership.
    """

    object_handle: int
    attribute_handles: tuple[int, ...]
    divesting_federate: int
    tag: bytes


@dataclass(frozen=True)
class AttributeOwnershipAcquisitionNotification:
    """A two-phase ownership transfer completed (§7.4).

    Fires on the new owner. ``owning_federate`` echoes the recipient's
    own handle so federates with multiple streams can correlate.
    """

    object_handle: int
    attribute_handles: tuple[int, ...]
    owning_federate: int


@dataclass(frozen=True)
class RequestDivestitureConfirmation:
    """An acquirer took over a pending divest (§7.3 divestee half).

    Fires on the federate that called negotiated_divest once the
    transfer to an acquirer fires.
    """

    object_handle: int
    attribute_handles: tuple[int, ...]


@dataclass(frozen=True)
class InitiateFederateSave:
    """The federation save has started (§4.8).

    Fires on every joined federate. ``save_time`` is the optional
    logical-time pin the requester passed; ``None`` when the requester
    asked for "save now".
    """

    label: str
    save_time: float | None


@dataclass(frozen=True)
class FederationSaved:
    """The federation save completed successfully (§4.9 success half).

    Fires on every federate that participated in the save once every
    federate has reported FederateSaveComplete and the bundle has been
    persisted.
    """

    label: str


@dataclass(frozen=True)
class FederationNotSaved:
    """The federation save aborted (§4.9 failure half).

    Fires on every federate that participated in the save when at
    least one reported FederateSaveNotComplete OR bundle persistence
    failed. The save bundle is NOT written.
    """

    label: str
