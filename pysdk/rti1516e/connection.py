"""Layer 1 — RtiConnection + Federate. Agent C implements per TASK-063.

Idiomatic asyncio API per docs/agent-c-pysdk.md §4.4 Layer 1:

    async with RtiConnection.connect(url="grpc://localhost:8442") as rti:
        async with rti.join_federation(
            FederationSpec(name="demo", fom_modules=["./demo.fom.xml"]),
            federate_name="alice",
        ) as fed:
            await fed.publish_object_class("Vehicle", attributes=["pos"])
            async for event in fed.events():
                ...

The connection-level transport is gRPC over HTTP/2 to the rtid binary.
Generated stubs live in rti1516e._generated/ (gitignored; regenerate with
`make py-codegen`). Agent C wires the generated client into the
async-context-manager surface.

This file is FROZEN-shape — Agent C may add private methods and dataclass
fields with defaults, but the public method names + signatures are part
of the M4 contract.
"""

from __future__ import annotations

from collections.abc import AsyncIterator
from dataclasses import dataclass, field
from types import TracebackType
from typing import Any, Self


@dataclass(frozen=True)
class FederationSpec:
    """Description of a federation to create or join."""

    name: str
    fom_modules: list[str] = field(default_factory=list)
    mode: str = "verbose"  # "verbose" | "best-effort" — wired by TASK-076
    seed: int = 0  # 0 = use server default; non-zero pins for determinism
    stall_timeout_seconds: int = 0  # 0 = server default (60s)


class RtiConnection:
    """Async connection to a single rtid instance."""

    @classmethod
    def connect(cls, url: str, *, options: dict[str, Any] | None = None) -> Self:
        """Build a connection wrapper bound to ``url`` (grpc://host:port).

        ``connect()`` is intentionally synchronous so it can be used as the
        head of an ``async with`` statement::

            async with RtiConnection.connect("grpc://localhost:8442") as rti:
                ...

        The actual gRPC channel setup happens inside ``__aenter__``.

        Raises NotImplementedError until TASK-063.
        """
        raise NotImplementedError("TASK-063")

    async def __aenter__(self) -> Self:
        """Open the gRPC channel. Wired by TASK-063."""
        raise NotImplementedError("TASK-063")

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        await self.close()

    async def close(self) -> None:
        """Tear down the connection. Idempotent."""
        raise NotImplementedError("TASK-063")

    def join_federation(
        self,
        spec: FederationSpec,
        *,
        federate_name: str,
    ) -> _FederateContextManager:
        """Open the per-federate async context manager.

        Use as ``async with rti.join_federation(spec, federate_name="x") as fed``.

        Raises NotImplementedError until TASK-063.
        """
        raise NotImplementedError("TASK-063")


class _FederateContextManager:
    """Internal: returned by RtiConnection.join_federation. Agent C may
    rename if convenient as long as the async-with semantics hold."""

    async def __aenter__(self) -> Federate:
        raise NotImplementedError("TASK-063")

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        raise NotImplementedError("TASK-063")


class Federate:
    """A joined federate. Created via ``rti.join_federation(...)``.

    The full pub/sub/object/interaction surface is wired by TASK-064..067.
    Methods declared here are the public contract; private helpers are
    Agent C's choice.
    """

    name: str
    handle: int

    # --- Declaration management (TASK-064) ---

    async def publish_object_class(
        self, class_name: str, *, attributes: list[str]
    ) -> None:
        """Declare publication of an object class + its attributes."""
        raise NotImplementedError("TASK-064")

    async def subscribe_object_class(
        self, class_name: str, *, attributes: list[str]
    ) -> None:
        """Declare subscription to an object class + its attributes."""
        raise NotImplementedError("TASK-064")

    async def publish_interaction_class(self, class_name: str) -> None:
        """Declare publication of an interaction class."""
        raise NotImplementedError("TASK-064")

    async def subscribe_interaction_class(self, class_name: str) -> None:
        """Declare subscription to an interaction class."""
        raise NotImplementedError("TASK-064")

    # --- Object management (TASK-065) ---

    async def register_object_instance(
        self, class_name: str, *, instance_name: str | None = None
    ) -> int:
        """Register an instance and return its handle."""
        raise NotImplementedError("TASK-065")

    async def update_attributes(
        self,
        object_handle: int,
        values: dict[str, Any],
        *,
        timestamp: float | None = None,
    ) -> None:
        """Update one or more attribute values on an object instance."""
        raise NotImplementedError("TASK-065")

    # --- Interaction management (TASK-066) ---

    async def send_interaction(
        self,
        class_name: str,
        parameters: dict[str, Any],
        *,
        timestamp: float | None = None,
    ) -> None:
        """Send an interaction with the given parameters."""
        raise NotImplementedError("TASK-066")

    # --- Time management (TASK-070 wires from the bridge side) ---

    async def enable_time_regulation(self, lookahead: float) -> None:
        """Become time-regulating with the given lookahead."""
        raise NotImplementedError("TASK-067")

    async def enable_time_constrained(self) -> None:
        """Become time-constrained."""
        raise NotImplementedError("TASK-067")

    async def next_message_request(self, time: float) -> None:
        """Request advance to ``time``. Grant arrives via events()."""
        raise NotImplementedError("TASK-067")

    # --- Event stream (TASK-067) ---

    def events(self) -> AsyncIterator[Any]:
        """Yield events emitted by the RTI to this federate.

        Each event is one of the dataclasses in rti1516e.events. See that
        module for the closed set.

        Raises NotImplementedError until TASK-067.
        """
        raise NotImplementedError("TASK-067")
