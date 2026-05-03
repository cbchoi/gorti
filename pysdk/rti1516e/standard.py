"""Layer 2 — Rti1516eAmbassador (1516-2010 standard-shaped callback API).

Agent C implements per TASK-068. Wraps Layer 1 (RtiConnection + Federate)
internally; intended for users porting from Java/C++ RTIs that use the
ambassador callback pattern.

Per docs/agent-c-pysdk.md §4.4 Layer 2 — this is a thin adapter, not a
re-implementation. Layer 1 owns the actual gRPC + asyncio surface.

Methods preserve the IEEE 1516.1 names (camelCase) for portability;
Python style would use snake_case but that defeats the porting purpose.
"""

from __future__ import annotations

from typing import Any


class Rti1516eAmbassador:
    """1516-2010-shaped ambassador callback API. Wraps Layer 1.

    This is a base class; users subclass it and override the callback
    methods (e.g. ``discoverObjectInstance``, ``reflectAttributeValues``).
    """

    # --- Connection / federation lifecycle ---

    def connect(self, callback_target: Rti1516eAmbassador, url: str) -> None:
        """Open the connection. Wraps RtiConnection.connect."""
        raise NotImplementedError("TASK-068")

    def disconnect(self) -> None:
        """Tear down the connection."""
        raise NotImplementedError("TASK-068")

    def createFederationExecution(self, name: str, fom_modules: list[str]) -> None:  # noqa: N802
        raise NotImplementedError("TASK-068")

    def joinFederationExecution(  # noqa: N802
        self, federate_name: str, federation_name: str
    ) -> None:
        raise NotImplementedError("TASK-068")

    def resignFederationExecution(self, action: str = "UNCONDITIONALLY_DIVEST_ATTRIBUTES") -> None:  # noqa: N802
        raise NotImplementedError("TASK-068")

    # --- Declaration management ---

    def publishObjectClassAttributes(  # noqa: N802
        self, class_name: str, attributes: list[str]
    ) -> None:
        raise NotImplementedError("TASK-068")

    def subscribeObjectClassAttributes(  # noqa: N802
        self, class_name: str, attributes: list[str]
    ) -> None:
        raise NotImplementedError("TASK-068")

    def publishInteractionClass(self, class_name: str) -> None:  # noqa: N802
        raise NotImplementedError("TASK-068")

    def subscribeInteractionClass(self, class_name: str) -> None:  # noqa: N802
        raise NotImplementedError("TASK-068")

    # --- Object management ---

    def registerObjectInstance(  # noqa: N802
        self, class_name: str, instance_name: str | None = None
    ) -> int:
        raise NotImplementedError("TASK-068")

    def updateAttributeValues(  # noqa: N802
        self,
        object_handle: int,
        values: dict[str, Any],
        timestamp: float | None = None,
    ) -> None:
        raise NotImplementedError("TASK-068")

    def sendInteraction(  # noqa: N802
        self,
        class_name: str,
        parameters: dict[str, Any],
        timestamp: float | None = None,
    ) -> None:
        raise NotImplementedError("TASK-068")

    # --- Time management ---

    def enableTimeRegulation(self, lookahead: float) -> None:  # noqa: N802
        raise NotImplementedError("TASK-068")

    def enableTimeConstrained(self) -> None:  # noqa: N802
        raise NotImplementedError("TASK-068")

    def nextMessageRequest(self, time: float) -> None:  # noqa: N802
        raise NotImplementedError("TASK-068")

    # --- Callbacks: subclass overrides these ---

    def discoverObjectInstance(  # noqa: N802
        self, object_handle: int, class_name: str, instance_name: str
    ) -> None:
        """Override to handle DiscoverObjectInstance."""

    def reflectAttributeValues(  # noqa: N802
        self,
        object_handle: int,
        values: dict[str, Any],
        timestamp: float | None,
    ) -> None:
        """Override to handle ReflectAttributeValues."""

    def receiveInteraction(  # noqa: N802
        self,
        class_name: str,
        parameters: dict[str, Any],
        timestamp: float | None,
    ) -> None:
        """Override to handle ReceiveInteraction."""

    def timeAdvanceGrant(self, time: float) -> None:  # noqa: N802
        """Override to handle TimeAdvanceGrant."""

    def federationHalted(self, cause: str, stalled_federate_handle: int) -> None:  # noqa: N802
        """Override to handle FederationHalted."""
