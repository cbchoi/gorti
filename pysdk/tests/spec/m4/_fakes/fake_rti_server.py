"""FakeRtiServer — pure-Python in-process double of the RTI gRPC surface.

Spec tests for the SDK (connection lifecycle, declaration, object,
interaction, events) drive scenarios through this fake instead of a real
rtid binary. The fake records every call (so tests can assert "the SDK
sent UpdateRequest with these attributes"), and exposes a way to push
canned events back into the federate's event stream (so tests can
exercise ReflectAttributeValues / ReceiveInteraction / TimeAdvanceGrant
delivery paths).

Agent C wires the SDK against this fake. They MAY add new recording
fields or canned-event helpers if a new spec test needs it, but MUST
NOT change existing field names or method signatures (back-compat
guarantee for tests already in flight).

Note: this is NOT a real gRPC server. It implements the SDK's
internal client surface (e.g. ``async def call_join_federation(spec)``
returning a fake response) — Agent C designs the SDK so that the
client's transport layer is injectable (constructor argument or
attribute), letting tests substitute this fake.

If Agent C's SDK doesn't expose such an injection point, the spec
tests will fail with AttributeError — that's a signal the SDK design
needs revision.
"""

from __future__ import annotations

import asyncio
from dataclasses import dataclass, field
from typing import Any


@dataclass
class RecordedCall:
    """One captured call from the SDK to the fake server."""

    method: str  # e.g. "join_federation", "publish_object_class"
    args: dict[str, Any] = field(default_factory=dict)


class FakeRtiServer:
    """In-process double. Pure Python — no gRPC.

    Usage in spec tests:

        srv = FakeRtiServer()
        async with RtiConnection.connect_with_transport(srv) as rti:
            ...
            srv.push_event(federate_handle=1, event=ReflectAttributeValues(...))
            ...
        assert any(c.method == "update_attributes" for c in srv.calls)

    The exact attachment mechanism (``connect_with_transport`` here is
    illustrative) is up to Agent C; the fake just needs to be reachable
    from the SDK's transport layer.
    """

    def __init__(self) -> None:
        self.calls: list[RecordedCall] = []
        # Per-federate-handle event queues. Tests push events; the SDK
        # drains them via its events() async iterator.
        self.event_queues: dict[int, asyncio.Queue[Any]] = {}
        # Optional canned responses keyed by method name. If a method has
        # an entry, the recorded call returns it (or raises if it's an
        # Exception). Lets tests inject failure modes.
        self.canned_responses: dict[str, Any] = {}
        # Federation state the fake maintains (name -> {"federates": [..]}).
        self.federations: dict[str, dict[str, Any]] = {}
        # Monotonic handle allocator.
        self._next_handle = 1
        # Auto-register under the canonical in-process URL so spec tests
        # that do `await RtiConnection.connect("memory://fake-rti")` find
        # this fake without extra fixture wiring. Last-writer-wins; tests
        # construct one fake per test, so no contention in practice.
        from rti1516e._transport import register_fake

        register_fake("memory://fake-rti", self)

    # --- Recording surface called by the SDK transport layer ----------------

    def record(self, method: str, **kwargs: Any) -> Any:
        """Record a call and return its canned response (or None).

        If canned_responses[method] is an Exception, raise it. Otherwise
        return the value (or None if no canned response is set).

        Test-only convenience: if ``method == "next_message_request"`` and
        no canned response is set, synthesize a ``TimeAdvanceGrant(time)``
        event into every known per-federate event queue. Real production
        traffic goes through rtid which decides when to grant; the fake
        has no scheduler, so the simplest "good enough" behavior for
        bridge tests is to grant immediately at the requested time. The
        existing spec test (``test_spec_m4_time_advance_grant_yielded``)
        explicitly pushes a redundant grant after calling
        ``next_message_request`` — both grants land in the queue, the
        test reads the first one, the times match, so the auto-grant is
        back-compat. Tests that need to suppress the auto-grant can
        register an explicit canned response for ``next_message_request``
        (any non-Exception value disables the synthesis).
        """
        self.calls.append(RecordedCall(method=method, args=dict(kwargs)))
        if method in self.canned_responses:
            response = self.canned_responses[method]
            if isinstance(response, Exception):
                raise response
            return response
        if method == "next_message_request":
            # Lazy import to avoid circular dependency between the test
            # double and the SDK package at module import time.
            from rti1516e.events import TimeAdvanceGrant

            grant = TimeAdvanceGrant(time=float(kwargs.get("time", 0.0)))
            target = kwargs.get("federate_handle")
            if target is not None:
                # ``events_for`` setdefaults a queue if one doesn't yet
                # exist; that's the same behavior the SDK relies on
                # when it first iterates ``fed.events()``.
                self.events_for(int(target)).put_nowait(grant)
            else:
                # No federate handle in kwargs (shouldn't happen via the
                # SDK, but defensively broadcast).
                for queue in self.event_queues.values():
                    queue.put_nowait(grant)
        return None

    # --- Test-side setup ----------------------------------------------------

    def push_event(self, federate_handle: int, event: Any) -> None:
        """Push a canned event into the federate's event stream.

        Agent C's SDK calls FakeRtiServer.events_for(handle) and yields
        from the resulting queue. Pushing here makes the event visible
        to the next ``async for`` iteration in the test.
        """
        queue = self.event_queues.setdefault(federate_handle, asyncio.Queue())
        queue.put_nowait(event)

    def events_for(self, federate_handle: int) -> asyncio.Queue[Any]:
        """Used by the SDK's transport stub to read events the test pushed."""
        return self.event_queues.setdefault(federate_handle, asyncio.Queue())

    def allocate_handle(self) -> int:
        """Mint a fresh monotonic handle. Used for register_object_instance,
        join_federation, etc."""
        h = self._next_handle
        self._next_handle += 1
        return h

    # --- Convenience assertions ---------------------------------------------

    def calls_for(self, method: str) -> list[RecordedCall]:
        """Return all recorded calls matching ``method``."""
        return [c for c in self.calls if c.method == method]

    def reset(self) -> None:
        """Clear all recorded state. Useful between subtests."""
        self.calls.clear()
        self.event_queues.clear()
        self.canned_responses.clear()
        self.federations.clear()
        self._next_handle = 1
