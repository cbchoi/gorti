"""Layer 2 — Rti1516eAmbassador (1516-2010 standard-shaped callback API).

Wraps Layer 1 (RtiConnection + Federate) internally; intended for users
porting from Java/C++ RTIs that use the ambassador callback pattern.

Per docs/agent-c-pysdk.md §4.4 Layer 2 — this is a thin adapter, not a
re-implementation. Layer 1 owns the actual gRPC + asyncio surface.

Methods preserve the IEEE 1516.1 names (camelCase) for portability;
Python style would use snake_case but that defeats the porting purpose.

The ambassador presents a synchronous API to the caller. Internally it
runs a private asyncio event loop in a background thread; each public
call schedules its async equivalent on that loop and waits for the
result. Callbacks (``discoverObjectInstance`` etc.) are dispatched from
the event-pump task that drains ``Federate.events()``.
"""

from __future__ import annotations

import asyncio
import threading
from concurrent.futures import Future
from typing import TYPE_CHECKING, Any

from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.events import (
    DiscoverObjectInstance,
    FederationHalted,
    ReceiveInteraction,
    ReflectAttributeValues,
    TimeAdvanceGrant,
)

if TYPE_CHECKING:
    from rti1516e.connection import Federate, _FederateContextManager


class Rti1516eAmbassador:
    """1516-2010-shaped ambassador callback API. Wraps Layer 1.

    This is a base class; users subclass it and override the callback
    methods (e.g. ``discoverObjectInstance``, ``reflectAttributeValues``).

    Lifecycle:

        amb = MyAmbassador()
        amb.connect(amb, "memory://fake-rti")
        amb.createFederationExecution("demo", ["demo.fom.xml"])
        amb.joinFederationExecution("alice", "demo")
        amb.publishObjectClassAttributes("Vehicle", ["pos"])
        ...
        amb.resignFederationExecution()
        amb.disconnect()
    """

    def __init__(self) -> None:
        self._loop: asyncio.AbstractEventLoop | None = None
        self._loop_thread: threading.Thread | None = None
        self._url: str | None = None
        self._connection: RtiConnection | None = None
        self._connection_cm_open = False
        self._federation_name: str | None = None
        self._fom_modules: list[str] = []
        self._federate_cm: _FederateContextManager | None = None
        self._federate: Federate | None = None
        self._event_pump_task: Future[None] | None = None
        self._callback_target: Rti1516eAmbassador = self

    # --- Connection / federation lifecycle ---

    def connect(self, callback_target: Rti1516eAmbassador, url: str) -> None:
        """Open the connection. Wraps RtiConnection.connect.

        ``callback_target`` is the object whose ``discover*``/``reflect*`` etc.
        methods are invoked when events arrive. In the common case it's
        ``self`` (subclass override pattern).
        """
        # Spin up a private event loop in a background thread. All Layer 1
        # asyncio work runs there; the caller stays sync.
        self._callback_target = callback_target if callback_target is not None else self
        self._url = url
        self._start_loop()
        connection = RtiConnection.connect(url)
        self._connection = self._run(connection.__aenter__())
        self._connection_cm_open = True

    def disconnect(self) -> None:
        """Tear down the connection. Idempotent."""
        if self._connection is not None and self._connection_cm_open:
            self._run(self._connection.__aexit__(None, None, None))
            self._connection_cm_open = False
        self._connection = None
        self._stop_loop()

    def createFederationExecution(self, name: str, fom_modules: list[str]) -> None:  # noqa: N802
        """Stash the federation spec for the upcoming join.

        In a real RTI, ``create`` and ``join`` are distinct calls. The
        Layer 1 SDK rolls them into ``join_federation`` (idempotent on the
        server side), so we just record the args here and apply them on
        the next ``joinFederationExecution`` call.
        """
        self._federation_name = name
        self._fom_modules = list(fom_modules)

    def joinFederationExecution(  # noqa: N802
        self, federate_name: str, federation_name: str
    ) -> None:
        if self._connection is None:
            raise RuntimeError("connect() must be called before joinFederationExecution()")
        # Honor a prior createFederationExecution; otherwise default to the
        # passed federation_name with no FOM modules.
        name = self._federation_name if self._federation_name is not None else federation_name
        spec = FederationSpec(name=name, fom_modules=list(self._fom_modules))
        cm = self._connection.join_federation(spec, federate_name=federate_name)
        self._federate_cm = cm
        self._federate = self._run(cm.__aenter__())
        # Start draining events into the user's callbacks.
        self._event_pump_task = asyncio.run_coroutine_threadsafe(
            self._pump_events(), self._loop_required()
        )

    def resignFederationExecution(  # noqa: N802
        self, action: str = "UNCONDITIONALLY_DIVEST_ATTRIBUTES"
    ) -> None:
        del action  # accepted for API compat; semantics handled server-side
        if self._event_pump_task is not None:
            self._event_pump_task.cancel()
            self._event_pump_task = None
        if self._federate_cm is not None:
            self._run(self._federate_cm.__aexit__(None, None, None))
            self._federate_cm = None
        self._federate = None

    # --- Declaration management ---

    def publishObjectClassAttributes(  # noqa: N802
        self, class_name: str, attributes: list[str]
    ) -> None:
        self._run(self._fed().publish_object_class(class_name, attributes=list(attributes)))

    def subscribeObjectClassAttributes(  # noqa: N802
        self, class_name: str, attributes: list[str]
    ) -> None:
        self._run(self._fed().subscribe_object_class(class_name, attributes=list(attributes)))

    def publishInteractionClass(self, class_name: str) -> None:  # noqa: N802
        self._run(self._fed().publish_interaction_class(class_name))

    def subscribeInteractionClass(self, class_name: str) -> None:  # noqa: N802
        self._run(self._fed().subscribe_interaction_class(class_name))

    # --- Object management ---

    def registerObjectInstance(  # noqa: N802
        self, class_name: str, instance_name: str | None = None
    ) -> int:
        result = self._run(
            self._fed().register_object_instance(class_name, instance_name=instance_name)
        )
        return int(result)

    def updateAttributeValues(  # noqa: N802
        self,
        object_handle: int,
        values: dict[str, Any],
        timestamp: float | None = None,
    ) -> None:
        self._run(
            self._fed().update_attributes(object_handle, dict(values), timestamp=timestamp)
        )

    def sendInteraction(  # noqa: N802
        self,
        class_name: str,
        parameters: dict[str, Any],
        timestamp: float | None = None,
    ) -> None:
        self._run(
            self._fed().send_interaction(class_name, dict(parameters), timestamp=timestamp)
        )

    # --- Time management ---

    def enableTimeRegulation(self, lookahead: float) -> None:  # noqa: N802
        self._run(self._fed().enable_time_regulation(lookahead))

    def disableTimeRegulation(self) -> None:  # noqa: N802
        self._run(self._fed().disable_time_regulation())

    def enableTimeConstrained(self) -> None:  # noqa: N802
        self._run(self._fed().enable_time_constrained())

    def disableTimeConstrained(self) -> None:  # noqa: N802
        self._run(self._fed().disable_time_constrained())

    def modifyLookahead(self, lookahead: float) -> None:  # noqa: N802
        self._run(self._fed().modify_lookahead(lookahead))

    def nextMessageRequest(self, time: float) -> None:  # noqa: N802
        self._run(self._fed().next_message_request(time))

    def nextMessageRequestAvailable(self, time: float) -> None:  # noqa: N802
        self._run(self._fed().next_message_request_available(time))

    def timeAdvanceRequest(self, time: float) -> None:  # noqa: N802
        self._run(self._fed().time_advance_request(time))

    def timeAdvanceRequestAvailable(self, time: float) -> None:  # noqa: N802
        self._run(self._fed().time_advance_request_available(time))

    def flushQueueRequest(self, time: float) -> None:  # noqa: N802
        self._run(self._fed().flush_queue_request(time))

    def queryLogicalTime(self) -> float:  # noqa: N802
        return self._run(self._fed().query_logical_time())

    def queryLookahead(self) -> float:  # noqa: N802
        return self._run(self._fed().query_lookahead())

    def queryLBTS(self) -> tuple[float, bool]:  # noqa: N802
        return self._run(self._fed().query_lbts())

    def enableAsynchronousDelivery(self) -> None:  # noqa: N802
        self._run(self._fed().enable_asynchronous_delivery())

    def disableAsynchronousDelivery(self) -> None:  # noqa: N802
        self._run(self._fed().disable_asynchronous_delivery())

    # --- M23 W1: §6 deleteObjectInstance ---

    def deleteObjectInstance(  # noqa: N802
        self, object_handle: int, tag: bytes = b"", timestamp: float | None = None,
    ) -> None:
        self._run(self._fed().delete_object_instance(object_handle, tag, timestamp))

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

    # --- Internals ---

    def _start_loop(self) -> None:
        if self._loop is not None:
            return
        loop_ready = threading.Event()

        def _runner() -> None:
            loop = asyncio.new_event_loop()
            self._loop = loop
            asyncio.set_event_loop(loop)
            loop_ready.set()
            try:
                loop.run_forever()
            finally:
                loop.close()

        thread = threading.Thread(target=_runner, name="rti-ambassador-loop", daemon=True)
        thread.start()
        self._loop_thread = thread
        loop_ready.wait()

    def _stop_loop(self) -> None:
        loop = self._loop
        if loop is None:
            return
        loop.call_soon_threadsafe(loop.stop)
        if self._loop_thread is not None:
            self._loop_thread.join(timeout=2.0)
        self._loop = None
        self._loop_thread = None

    def _loop_required(self) -> asyncio.AbstractEventLoop:
        if self._loop is None:
            raise RuntimeError("ambassador event loop is not running — call connect() first")
        return self._loop

    def _run(self, coro: Any) -> Any:
        """Schedule ``coro`` on the background loop and block until done."""
        loop = self._loop_required()
        future: Future[Any] = asyncio.run_coroutine_threadsafe(coro, loop)
        return future.result()

    def _fed(self) -> Federate:
        if self._federate is None:
            raise RuntimeError("not joined — call joinFederationExecution() first")
        return self._federate

    async def _pump_events(self) -> None:
        """Drain Federate.events() and dispatch to the appropriate callback."""
        federate = self._federate
        if federate is None:
            return
        target = self._callback_target
        try:
            async for event in federate.events():
                if isinstance(event, DiscoverObjectInstance):
                    target.discoverObjectInstance(
                        event.object_handle, event.class_name, event.instance_name
                    )
                elif isinstance(event, ReflectAttributeValues):
                    target.reflectAttributeValues(
                        event.object_handle, event.values, event.timestamp
                    )
                elif isinstance(event, ReceiveInteraction):
                    target.receiveInteraction(
                        event.class_name, event.parameters, event.timestamp
                    )
                elif isinstance(event, TimeAdvanceGrant):
                    target.timeAdvanceGrant(event.time)
                elif isinstance(event, FederationHalted):
                    target.federationHalted(event.cause, event.stalled_federate_handle)
                # Unknown event types are silently ignored — Layer 1 owns
                # the closed-set of dataclasses, so this branch is dead in
                # practice but defensive against future additions.
        except asyncio.CancelledError:
            return
