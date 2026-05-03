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
            case FederationHalted(cause, federate_handle): ...
"""

from __future__ import annotations

from dataclasses import dataclass
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
