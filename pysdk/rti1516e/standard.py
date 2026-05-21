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

    def localDeleteObjectInstance(self, object_handle: int) -> None:  # noqa: N802
        self._run(self._fed().local_delete_object_instance(object_handle))

    def requestAttributeValueUpdate(  # noqa: N802
        self, object_handle: int, attribute_handles: list[int], tag: bytes = b"",
    ) -> None:
        self._run(self._fed().request_attribute_value_update(object_handle, attribute_handles, tag))

    def requestClassAttributeValueUpdate(  # noqa: N802
        self, object_class_handle: int, attribute_handles: list[int], tag: bytes = b"",
    ) -> None:
        self._run(self._fed().request_class_attribute_value_update(object_class_handle, attribute_handles, tag))

    def changeAttributeTransportationType(  # noqa: N802
        self, object_handle: int, attribute_handles: list[int], transport: int,
    ) -> None:
        self._run(self._fed().change_attribute_transportation_type(object_handle, attribute_handles, transport))

    def changeInteractionTransportationType(  # noqa: N802
        self, interaction_class_handle: int, transport: int,
    ) -> None:
        self._run(self._fed().change_interaction_transportation_type(interaction_class_handle, transport))

    # --- §10.2 Support services (M25 Phase B) ---
    # Pitch-style sync wrappers around the async SupportClient. Each
    # delegates to fed.support.<method> via _run(). Federates ported
    # from Pitch use these directly.

    def getObjectClassHandle(self, class_name: str) -> int:  # noqa: N802
        return int(self._run(self._fed().support.get_object_class_handle(class_name)))

    def getObjectClassName(self, class_handle: int) -> str:  # noqa: N802
        return str(self._run(self._fed().support.get_object_class_name(class_handle)))

    def getAttributeHandle(self, class_handle: int, attribute_name: str) -> int:  # noqa: N802
        return int(
            self._run(self._fed().support.get_attribute_handle(class_handle, attribute_name))
        )

    def getAttributeName(self, class_handle: int, attribute_handle: int) -> str:  # noqa: N802
        return str(
            self._run(self._fed().support.get_attribute_name(class_handle, attribute_handle))
        )

    def getInteractionClassHandle(self, class_name: str) -> int:  # noqa: N802
        return int(self._run(self._fed().support.get_interaction_class_handle(class_name)))

    def getInteractionClassName(self, class_handle: int) -> str:  # noqa: N802
        return str(self._run(self._fed().support.get_interaction_class_name(class_handle)))

    def getParameterHandle(self, class_handle: int, parameter_name: str) -> int:  # noqa: N802
        return int(
            self._run(self._fed().support.get_parameter_handle(class_handle, parameter_name))
        )

    def getParameterName(self, class_handle: int, parameter_handle: int) -> str:  # noqa: N802
        return str(
            self._run(self._fed().support.get_parameter_name(class_handle, parameter_handle))
        )

    def getDimensionHandle(self, dimension_name: str) -> int:  # noqa: N802
        return int(self._run(self._fed().support.get_dimension_handle(dimension_name)))

    def getDimensionName(self, dimension_handle: int) -> str:  # noqa: N802
        return str(self._run(self._fed().support.get_dimension_name(dimension_handle)))

    def getDimensionUpperBound(self, dimension_handle: int) -> int:  # noqa: N802
        return int(self._run(self._fed().support.get_dimension_upper_bound(dimension_handle)))

    def getOrderType(self, order_name: str) -> int:  # noqa: N802
        return int(self._run(self._fed().support.get_order_type(order_name)))

    def getOrderName(self, order_type: int) -> str:  # noqa: N802
        return str(self._run(self._fed().support.get_order_name(order_type)))

    def getTransportationType(self, transportation_name: str) -> int:  # noqa: N802
        return int(self._run(self._fed().support.get_transportation_type(transportation_name)))

    def getTransportationName(self, transportation_type: int) -> str:  # noqa: N802
        return str(self._run(self._fed().support.get_transportation_name(transportation_type)))

    # --- §4.11-4.13 Synchronization points (M25 Phase C) ---

    def registerFederationSynchronizationPoint(  # noqa: N802
        self, label: str, tag: bytes = b"", sync_set: list[int] | None = None
    ) -> None:
        self._run(
            self._fed().sync.register_synchronization_point(
                label, tag=tag, required_federates=sync_set
            )
        )

    def synchronizationPointAchieved(self, label: str) -> None:  # noqa: N802
        self._run(self._fed().sync.synchronization_point_achieved(label))

    # --- §7 Ownership Management (M25 Phase C) ---

    def unconditionalAttributeOwnershipDivestiture(  # noqa: N802
        self, object_handle: int, attribute_handles: list[int]
    ) -> None:
        self._run(self._fed().ownership.unconditional_divest(object_handle, attribute_handles))

    def negotiatedAttributeOwnershipDivestiture(  # noqa: N802
        self, object_handle: int, attribute_handles: list[int], tag: bytes = b""
    ) -> None:
        self._run(
            self._fed().ownership.negotiated_divest(object_handle, attribute_handles, tag=tag)
        )

    def attributeOwnershipAcquisition(  # noqa: N802
        self, object_handle: int, attribute_handles: list[int], tag: bytes = b""
    ) -> None:
        self._run(
            self._fed().ownership.acquire(object_handle, attribute_handles, tag=tag)
        )

    def cancelNegotiatedAttributeOwnershipDivestiture(  # noqa: N802
        self, object_handle: int, attribute_handles: list[int]
    ) -> None:
        self._run(
            self._fed().ownership.cancel_negotiated_divest(object_handle, attribute_handles)
        )

    def cancelAttributeOwnershipAcquisition(  # noqa: N802
        self, object_handle: int, attribute_handles: list[int]
    ) -> None:
        self._run(self._fed().ownership.cancel_acquire(object_handle, attribute_handles))

    def attributeOwnershipDivestitureIfWanted(  # noqa: N802
        self, object_handle: int, attribute_handles: list[int]
    ) -> None:
        self._run(self._fed().ownership.divest_if_wanted(object_handle, attribute_handles))

    def queryAttributeOwnership(  # noqa: N802
        self, object_handle: int, attribute_handle: int
    ) -> tuple[int, bool]:
        return self._run(
            self._fed().ownership.query_attribute_ownership(object_handle, attribute_handle)
        )

    def isAttributeOwnedByFederate(  # noqa: N802
        self, object_handle: int, attribute_handle: int
    ) -> bool:
        return bool(
            self._run(
                self._fed().ownership.is_attribute_owned_by_federate(
                    object_handle, attribute_handle
                )
            )
        )

    # --- §4.8-4.15 Federation save/restore (M25 Phase C) ---

    def requestFederationSave(  # noqa: N802
        self, label: str, save_time: float | None = None
    ) -> None:
        self._run(self._fed().savepoint.request_federation_save(label, save_time=save_time))

    def federateSaveComplete(self) -> None:  # noqa: N802
        self._run(self._fed().savepoint.federate_save_complete())

    def federateSaveNotComplete(self) -> None:  # noqa: N802
        self._run(self._fed().savepoint.federate_save_not_complete())

    def queryFederationSaveStatus(self, label: str) -> Any:  # noqa: N802
        return self._run(self._fed().savepoint.query_save_state(label))

    def requestFederationRestore(self, label: str) -> None:  # noqa: N802
        self._run(self._fed().savepoint.request_federation_restore(label))

    def federateRestoreComplete(self) -> None:  # noqa: N802
        self._run(self._fed().savepoint.federate_restore_complete())

    def queryFederationRestoreStatus(self, label: str) -> Any:  # noqa: N802
        return self._run(self._fed().savepoint.query_restore_state(label))

    # --- §9 Data Distribution Management (M25 Phase C) ---

    def createRegion(  # noqa: N802
        self, routing_space_handle: int, dimension_handles: list[int]
    ) -> int:
        return int(
            self._run(self._fed().ddm.create_region(routing_space_handle, dimension_handles))
        )

    def setRangeBounds(  # noqa: N802
        self,
        region_handle: int,
        dimension_handle: int,
        lower_bound: int,
        upper_bound: int,
    ) -> None:
        self._run(
            self._fed().ddm.set_range_bounds(
                region_handle, dimension_handle, lower=lower_bound, upper=upper_bound
            )
        )

    def commitRegionModifications(self, region_handles: list[int]) -> None:  # noqa: N802
        self._run(self._fed().ddm.commit_region_modifications(region_handles))

    def deleteRegion(self, region_handle: int) -> None:  # noqa: N802
        self._run(self._fed().ddm.delete_region(region_handle))

    def subscribeObjectClassAttributesWithRegions(  # noqa: N802
        self,
        object_class_handle: int,
        attribute_handles: list[int],
        region_handles: list[int],
    ) -> None:
        self._run(
            self._fed().ddm.subscribe_object_class_attributes_with_regions(
                object_class_handle, attribute_handles, region_handles
            )
        )

    def subscribeInteractionClassWithRegions(  # noqa: N802
        self, interaction_class_handle: int, region_handles: list[int]
    ) -> None:
        self._run(
            self._fed().ddm.subscribe_interaction_class_with_regions(
                interaction_class_handle, region_handles
            )
        )

    def registerObjectInstanceWithRegions(  # noqa: N802
        self,
        object_class_handle: int,
        attributes_and_regions: dict[int, list[int]],
        instance_name: str = "",
    ) -> int:
        from rti1516e.ddm import AttributeRegions

        bindings = [
            AttributeRegions(attribute_handle=int(a), region_handles=[int(r) for r in regs])
            for a, regs in attributes_and_regions.items()
        ]
        return int(
            self._run(
                self._fed().ddm.register_object_instance_with_regions(
                    object_class_handle, bindings, object_name=instance_name
                )
            )
        )

    def associateRegionsForUpdates(  # noqa: N802
        self,
        object_handle: int,
        attributes_and_regions: dict[int, list[int]],
    ) -> None:
        from rti1516e.ddm import AttributeRegions

        bindings = [
            AttributeRegions(attribute_handle=int(a), region_handles=[int(r) for r in regs])
            for a, regs in attributes_and_regions.items()
        ]
        self._run(self._fed().ddm.associate_regions_for_updates(object_handle, bindings))

    def unassociateRegionsForUpdates(  # noqa: N802
        self,
        object_handle: int,
        attributes_and_regions: dict[int, list[int]] | None = None,
    ) -> None:
        from rti1516e.ddm import AttributeRegions

        bindings: list[AttributeRegions] | None
        if attributes_and_regions is None:
            bindings = None
        else:
            bindings = [
                AttributeRegions(
                    attribute_handle=int(a), region_handles=[int(r) for r in regs]
                )
                for a, regs in attributes_and_regions.items()
            ]
        self._run(self._fed().ddm.unassociate_regions_for_updates(object_handle, bindings))

    def unsubscribeObjectClassAttributesWithRegions(  # noqa: N802
        self,
        object_class_handle: int,
        attribute_handles: list[int],
        region_handles: list[int],
    ) -> None:
        self._run(
            self._fed().ddm.unsubscribe_object_class_attributes_with_regions(
                object_class_handle, attribute_handles, region_handles
            )
        )

    def unsubscribeInteractionClassWithRegions(  # noqa: N802
        self, interaction_class_handle: int, region_handles: list[int]
    ) -> None:
        self._run(
            self._fed().ddm.unsubscribe_interaction_class_with_regions(
                interaction_class_handle, region_handles
            )
        )

    def sendInteractionWithRegions(  # noqa: N802
        self,
        interaction_class_handle: int,
        parameters: dict[int, bytes],
        region_handles: list[int],
        timestamp: float | None = None,
    ) -> None:
        self._run(
            self._fed().ddm.send_interaction_with_regions(
                interaction_class_handle,
                parameters,
                region_handles,
                timestamp=timestamp,
            )
        )

    def requestAttributeValueUpdateWithRegions(  # noqa: N802
        self,
        object_class_handle: int,
        attribute_handles: list[int],
        region_handles: list[int],
        tag: bytes = b"",
    ) -> None:
        self._run(
            self._fed().ddm.request_attribute_value_update_with_regions(
                object_class_handle, attribute_handles, region_handles, tag=tag
            )
        )

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

    # --- M25 Phase D — additional FederateAmbassador callbacks ---
    # Each is a no-op by default; federates ported from Pitch
    # override the ones they care about. The Layer-1 event types
    # were already wired through pysdk/rti1516e/events.py.

    def removeObjectInstance(  # noqa: N802
        self, object_handle: int, tag: bytes, timestamp: float | None
    ) -> None:
        """§6.16 — an instance was deleted by its owner."""

    def provideAttributeValueUpdate(  # noqa: N802
        self, object_handle: int, attribute_handles: tuple[int, ...], tag: bytes
    ) -> None:
        """§6.26 — peer requested fresh values; owner should respond."""

    def synchronizationPointRegistrationSucceeded(self, label: str) -> None:  # noqa: N802
        """§4.5 — sync-point registration succeeded.

        Currently fires via the Federate.events() stream as
        SynchronizationPointAnnounced for the registrant; this
        callback is provided for Pitch symmetry.
        """

    def announceSynchronizationPoint(self, label: str, tag: bytes) -> None:  # noqa: N802
        """§4.6 — a sync point was announced to this federate.

        Pitch's announceSynchronizationPoint matches our internal
        SynchronizationPointAnnounced event.
        """

    def federationSynchronized(self, label: str) -> None:  # noqa: N802
        """§4.7 — all required federates have achieved the sync point."""

    def requestAttributeOwnershipAssumption(  # noqa: N802
        self,
        object_handle: int,
        attribute_handles: tuple[int, ...],
        divesting_federate: int,
        tag: bytes,
    ) -> None:
        """§7.3 — current owner offered ownership; this federate may acquire."""

    def attributeOwnershipAcquisitionNotification(  # noqa: N802
        self,
        object_handle: int,
        attribute_handles: tuple[int, ...],
        owning_federate: int,
    ) -> None:
        """§7.4 — this federate's acquisition succeeded; it now owns the attrs."""

    def requestDivestitureConfirmation(  # noqa: N802
        self, object_handle: int, attribute_handles: tuple[int, ...]
    ) -> None:
        """§7.3 (divester half) — pending divest was matched and transferred."""

    def initiateFederateSave(  # noqa: N802
        self, label: str, save_time: float | None
    ) -> None:
        """§4.8 — federation save has started; federate must save state."""

    def federationSaved(self, label: str) -> None:  # noqa: N802
        """§4.9 — federation save completed successfully."""

    def federationNotSaved(self, label: str) -> None:  # noqa: N802
        """§4.9 — federation save was aborted; bundle was NOT written."""

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
                # M25 Phase D — broaden dispatch to cover the full
                # FederateAmbassador callback surface. Each branch
                # forwards directly to the override slot; the base
                # class no-ops mean unhandled callbacks are silently
                # dropped, matching Pitch's "subscribe to what you
                # care about" model.
                elif isinstance(event, RemoveObjectInstance):
                    target.removeObjectInstance(
                        event.object_handle, event.tag, event.timestamp
                    )
                elif isinstance(event, ProvideAttributeValueUpdate):
                    target.provideAttributeValueUpdate(
                        event.object_handle, event.attribute_handles, event.tag
                    )
                elif isinstance(event, SynchronizationPointAnnounced):
                    target.announceSynchronizationPoint(event.label, event.tag)
                elif isinstance(event, FederationSynchronized):
                    target.federationSynchronized(event.label)
                elif isinstance(event, RequestAttributeOwnershipAssumption):
                    target.requestAttributeOwnershipAssumption(
                        event.object_handle,
                        event.attribute_handles,
                        event.divesting_federate,
                        event.tag,
                    )
                elif isinstance(event, AttributeOwnershipAcquisitionNotification):
                    target.attributeOwnershipAcquisitionNotification(
                        event.object_handle,
                        event.attribute_handles,
                        event.owning_federate,
                    )
                elif isinstance(event, RequestDivestitureConfirmation):
                    target.requestDivestitureConfirmation(
                        event.object_handle, event.attribute_handles
                    )
                elif isinstance(event, InitiateFederateSave):
                    target.initiateFederateSave(event.label, event.save_time)
                elif isinstance(event, FederationSaved):
                    target.federationSaved(event.label)
                elif isinstance(event, FederationNotSaved):
                    target.federationNotSaved(event.label)
                # Unknown event types are silently ignored — Layer 1 owns
                # the closed-set of dataclasses, so this branch is dead in
                # practice but defensive against future additions.
        except asyncio.CancelledError:
            return
