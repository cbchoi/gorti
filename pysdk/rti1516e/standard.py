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
import inspect
import threading
from concurrent.futures import Future
from typing import TYPE_CHECKING, Any, TypeAlias, cast

from rti1516e import _transport
from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.events import (
    AttributeOwnershipAcquisitionNotification,
    AttributeOwnershipUnavailable,
    AttributesInScope,
    AttributesOutOfScope,
    DiscoverObjectInstance,
    FederationHalted,
    FederationNotRestored,
    FederationNotSaved,
    FederationRestoreBegun,
    FederationRestored,
    FederationSaved,
    FederationSynchronized,
    InitiateFederateRestore,
    InitiateFederateSave,
    MultipleObjectInstanceNameReservationFailed,
    MultipleObjectInstanceNameReservationSucceeded,
    ObjectInstanceNameReservationFailed,
    ObjectInstanceNameReservationSucceeded,
    ProvideAttributeValueUpdate,
    ReceiveInteraction,
    ReflectAttributeValues,
    RemoveObjectInstance,
    RequestAttributeOwnershipAssumption,
    RequestAttributeOwnershipRelease,
    RequestDivestitureConfirmation,
    RequestFederationRestoreFailed,
    RequestFederationRestoreSucceeded,
    RequestRetraction,
    StartRegistrationForObjectClass,
    StopRegistrationForObjectClass,
    SynchronizationPointAnnounced,
    SynchronizationPointFailureReason,
    SynchronizationPointRegistrationFailed,
    SynchronizationPointRegistrationSucceeded,
    TimeAdvanceGrant,
    TurnInteractionsOff,
    TurnInteractionsOn,
)
from rti1516e.factories import (
    AttributeHandleSetFactory,
    AttributeHandleValueMapFactory,
    DimensionHandleSetFactory,
    FederateHandleSetFactory,
    ParameterHandleValueMapFactory,
    RegionHandleSetFactory,
)
from rti1516e.handles import (
    AttributeHandle,
    DimensionHandle,
    FederateHandle,
    InteractionClassHandle,
    MessageRetractionHandle,
    ObjectClassHandle,
    ObjectInstanceHandle,
    ParameterHandle,
    RegionHandle,
)
from rti1516e.sets import (
    AttributeHandleSet,
    AttributeHandleValueMap,
    DimensionHandleSet,
    FederateHandleSet,
    ParameterHandleSet,
    ParameterHandleValueMap,
    RegionHandleSet,
)

if TYPE_CHECKING:
    from rti1516e.connection import Federate, _FederateContextManager


# M28 — Pitch-port type aliases. Typed handles are int subclasses, so
# the bare-int callers from M25-M27 keep working at runtime; the alias
# documents that typed forms are accepted too and lets mypy --strict
# resolve mixed-typed Pitch federate code unchanged.
ObjectClassRef: TypeAlias = "int | str | ObjectClassHandle"
AttributeRef: TypeAlias = "int | str | AttributeHandle"
InteractionClassRef: TypeAlias = "int | str | InteractionClassHandle"
ParameterRef: TypeAlias = "int | str | ParameterHandle"
ObjectInstanceRef: TypeAlias = "int | ObjectInstanceHandle"
DimensionRef: TypeAlias = "int | str | DimensionHandle"
FederateRef: TypeAlias = "int | FederateHandle"
RegionRef: TypeAlias = "int | RegionHandle"
AttributeRefList: TypeAlias = "list[int | str | AttributeHandle] | AttributeHandleSet"
ParameterRefList: TypeAlias = "list[int | str | ParameterHandle] | ParameterHandleSet"
FederateRefList: TypeAlias = "list[int | FederateHandle] | FederateHandleSet"
DimensionRefList: TypeAlias = "list[int | str | DimensionHandle] | DimensionHandleSet"
RegionRefList: TypeAlias = "list[int | RegionHandle] | RegionHandleSet"
AttributeValueDict: TypeAlias = (
    "dict[int | str | AttributeHandle, Any] | AttributeHandleValueMap"
)
ParameterValueDict: TypeAlias = (
    "dict[int | str | ParameterHandle, Any] | ParameterHandleValueMap"
)


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
        # M26 Phase E — evokeCallback callback-fired counter. Bumped
        # exactly once per dispatched event by _pump_events; read by
        # evokeCallback to compute its bool return.
        self._callback_fired_count: int = 0
        # M27 Phase C — §10.4 callback enable/disable. When False,
        # _dispatch_event buffers events to self._callback_buffer
        # instead of firing override slots; enableCallbacks drains
        # the buffer through the normal dispatch path.
        self._callbacks_enabled: bool = True
        self._callback_buffer: list[Any] = []
        # M28 — Pitch-style factory singletons. Stateless; one instance per
        # ambassador per Pitch convention.
        self._attribute_handle_set_factory = AttributeHandleSetFactory()
        self._attribute_handle_value_map_factory = AttributeHandleValueMapFactory()
        self._parameter_handle_value_map_factory = ParameterHandleValueMapFactory()
        self._federate_handle_set_factory = FederateHandleSetFactory()
        self._dimension_handle_set_factory = DimensionHandleSetFactory()
        self._region_handle_set_factory = RegionHandleSetFactory()
        # M39 — per-(class, callback) cache of which optional kwargs an
        # override accepts (see _invoke_compat).
        self._accepted_kwargs_cache: dict[
            tuple[type, str], frozenset[str] | None
        ] = {}

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
        """§4.5 — create the federation execution.

        M39 HA-2: when connected, the create RPC is issued EAGERLY and a
        duplicate name raises the typed
        :class:`rti1516e.errors.FederationExecutionAlreadyExists`
        (IEEE §4.5) — the standard HLA pattern is::

            try:
                amb.createFederationExecution(name, foms)
            except FederationExecutionAlreadyExists:
                pass  # someone else created it first
            amb.joinFederationExecution(federate, name)

        The spec is also stashed for the upcoming join, and the rolled
        create-on-join path stays idempotent — federates that skip
        ``createFederationExecution`` entirely keep working (Layer 1's
        ``join_federation`` creates with ``exist_ok=True``).
        """
        self._federation_name = name
        self._fom_modules = list(fom_modules)
        if self._connection is not None and self._connection_cm_open:
            spec = FederationSpec(name=name, fom_modules=list(fom_modules))
            self._run(self._connection.create_federation(spec))

    def destroyFederationExecution(self, name: str) -> None:  # noqa: N802
        """§4.6 — destroy the federation execution.

        M39 HA-2. Raises the typed
        :class:`rti1516e.errors.FederatesCurrentlyJoined` while members
        remain joined and
        :class:`rti1516e.errors.FederationExecutionDoesNotExist` for an
        unknown name.
        """
        if self._connection is None:
            raise RuntimeError(
                "connect() must be called before destroyFederationExecution()"
            )
        self._run(self._connection.destroy_federation(name))

    def joinFederationExecution(  # noqa: N802
        self,
        federate_name: str,
        federation_name: str,
        additional_fom_modules: list[str] | None = None,
    ) -> None:
        """§4.9 — join an existing federation.

        M27 Phase D: ``additional_fom_modules`` matches Pitch's
        IEEE 1516.1 ``additionalFomModules`` parameter. A federate
        that joins an already-created federation should pass the
        same FOM modules the creator used, so the local handle
        cache is populated. Without this, event translation falls
        back to stringified handles (Discover / Reflect / Receive
        callbacks see the handle as a decimal string instead of
        the FOM class name).

        If a prior ``createFederationExecution`` recorded modules,
        those are used unless ``additional_fom_modules`` is passed
        explicitly (the explicit form wins, matching Pitch).
        """
        if self._connection is None:
            raise RuntimeError("connect() must be called before joinFederationExecution()")
        # Honor a prior createFederationExecution; otherwise default to the
        # passed federation_name with no FOM modules.
        name = self._federation_name if self._federation_name is not None else federation_name
        modules: list[str]
        if additional_fom_modules is not None:
            modules = list(additional_fom_modules)
        else:
            modules = list(self._fom_modules)
        spec = FederationSpec(name=name, fom_modules=modules)
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
        """IEEE 1516.1-2010 §4.10 — resign with an explicit action.

        M36: the action designator is threaded through the federate
        context manager down to the wire (M24 W2 ResignAction enum), so
        e.g. ``CANCEL_THEN_DELETE_THEN_DIVEST`` makes the rtid delete
        the resigning federate's instances (subscribers see REMOVE).
        Unknown designators raise ``ValueError`` before any state is
        torn down (§4.10 InvalidResignAction).
        """
        if action not in _transport.RESIGN_ACTION_NAMES:
            valid = ", ".join(sorted(_transport.RESIGN_ACTION_NAMES))
            raise ValueError(
                f"invalid resign action {action!r}; expected one of: {valid}"
            )
        if self._event_pump_task is not None:
            self._event_pump_task.cancel()
            self._event_pump_task = None
        if self._federate_cm is not None:
            self._federate_cm.resign_action = action
            self._run(self._federate_cm.__aexit__(None, None, None))
            self._federate_cm = None
        self._federate = None

    # --- Declaration management ---

    def publishObjectClassAttributes(  # noqa: N802
        self, class_name: ObjectClassRef, attributes: AttributeRefList
    ) -> None:
        """M27 Phase B: ``class_name`` accepts ``int`` (Pitch-style FOM
        handle, e.g. from ``getObjectClassHandle``) or ``str`` (FOM name).
        Each entry of ``attributes`` is independently ``int`` or ``str``."""
        self._run(self._fed().publish_object_class(class_name, attributes=list(attributes)))

    def subscribeObjectClassAttributes(  # noqa: N802
        self, class_name: ObjectClassRef, attributes: AttributeRefList
    ) -> None:
        """See :meth:`publishObjectClassAttributes` for the M27 Phase B
        int|str semantics."""
        self._run(self._fed().subscribe_object_class(class_name, attributes=list(attributes)))

    def publishInteractionClass(self, class_name: InteractionClassRef) -> None:  # noqa: N802
        """M27 Phase D: ``class_name`` accepts ``int`` (FOM handle) or ``str``."""
        self._run(self._fed().publish_interaction_class(class_name))

    def subscribeInteractionClass(self, class_name: InteractionClassRef) -> None:  # noqa: N802
        """M27 Phase D: ``class_name`` accepts ``int`` (FOM handle) or ``str``."""
        self._run(self._fed().subscribe_interaction_class(class_name))

    # --- Object management ---

    def registerObjectInstance(  # noqa: N802
        self, class_name: ObjectClassRef, instance_name: str | None = None
    ) -> ObjectInstanceHandle:
        """M27 Phase B: ``class_name`` accepts ``int`` (Pitch-style FOM
        handle) or ``str`` (FOM name). The parameter is still named
        ``class_name`` for source-compat with pre-M27 callers."""
        result = self._run(
            self._fed().register_object_instance(class_name, instance_name=instance_name)
        )
        return ObjectInstanceHandle(result)

    def updateAttributeValues(  # noqa: N802
        self,
        object_handle: ObjectInstanceRef,
        values: AttributeValueDict,
        timestamp: float | None = None,
    ) -> None:
        """M27 Phase B: ``values`` dict keys accept ``int`` (Pitch-style
        attribute handle) or ``str`` (FOM attribute name)."""
        self._run(
            self._fed().update_attributes(object_handle, dict(values), timestamp=timestamp)
        )

    def sendInteraction(  # noqa: N802
        self,
        class_name: InteractionClassRef,
        parameters: ParameterValueDict,
        timestamp: float | None = None,
    ) -> None:
        """M27 Phase B: ``class_name`` and ``parameters`` dict keys
        accept ``int`` (handle) or ``str`` (FOM name)."""
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
        return float(self._run(self._fed().query_logical_time()))

    def queryLookahead(self) -> float:  # noqa: N802
        return float(self._run(self._fed().query_lookahead()))

    def queryLBTS(self) -> tuple[float, bool]:  # noqa: N802
        return cast("tuple[float, bool]", self._run(self._fed().query_lbts()))

    def enableAsynchronousDelivery(self) -> None:  # noqa: N802
        self._run(self._fed().enable_asynchronous_delivery())

    def disableAsynchronousDelivery(self) -> None:  # noqa: N802
        self._run(self._fed().disable_asynchronous_delivery())

    # --- M23 W1: §6 deleteObjectInstance ---

    def deleteObjectInstance(  # noqa: N802
        self,
        object_handle: ObjectInstanceRef,
        tag: bytes = b"",
        timestamp: float | None = None,
    ) -> None:
        self._run(self._fed().delete_object_instance(object_handle, tag, timestamp))

    def localDeleteObjectInstance(self, object_handle: ObjectInstanceRef) -> None:  # noqa: N802
        self._run(self._fed().local_delete_object_instance(object_handle))

    def requestAttributeValueUpdate(  # noqa: N802
        self,
        object_handle: ObjectInstanceRef,
        attribute_handles: AttributeRefList,
        tag: bytes = b"",
    ) -> None:
        self._run(
            self._fed().request_attribute_value_update(
                object_handle, list(attribute_handles), tag,
            )
        )

    def requestClassAttributeValueUpdate(  # noqa: N802
        self,
        object_class_handle: ObjectClassRef,
        attribute_handles: AttributeRefList,
        tag: bytes = b"",
    ) -> None:
        self._run(
            self._fed().request_class_attribute_value_update(
                object_class_handle, list(attribute_handles), tag,
            )
        )

    def changeAttributeTransportationType(  # noqa: N802
        self,
        object_handle: ObjectInstanceRef,
        attribute_handles: AttributeRefList,
        transport: int,
    ) -> None:
        self._run(
            self._fed().change_attribute_transportation_type(
                object_handle, list(attribute_handles), transport,
            )
        )

    def changeInteractionTransportationType(  # noqa: N802
        self, interaction_class_handle: InteractionClassRef, transport: int,
    ) -> None:
        self._run(
            self._fed().change_interaction_transportation_type(
                interaction_class_handle, transport,
            )
        )

    # --- §10.2 Support services (M25 Phase B) ---
    # Pitch-style sync wrappers around the async SupportClient. Each
    # delegates to fed.support.<method> via _run(). Federates ported
    # from Pitch use these directly.

    def getObjectClassHandle(self, class_name: str) -> ObjectClassHandle:  # noqa: N802
        return ObjectClassHandle(
            self._run(self._fed().support.get_object_class_handle(class_name))
        )

    def getObjectClassName(self, class_handle: ObjectClassRef) -> str:  # noqa: N802
        return str(self._run(self._fed().support.get_object_class_name(class_handle)))

    def getAttributeHandle(  # noqa: N802
        self, class_handle: ObjectClassRef, attribute_name: str
    ) -> AttributeHandle:
        return AttributeHandle(
            self._run(self._fed().support.get_attribute_handle(class_handle, attribute_name))
        )

    def getAttributeName(  # noqa: N802
        self, class_handle: ObjectClassRef, attribute_handle: AttributeRef
    ) -> str:
        return str(
            self._run(self._fed().support.get_attribute_name(class_handle, attribute_handle))
        )

    def getInteractionClassHandle(self, class_name: str) -> InteractionClassHandle:  # noqa: N802
        return InteractionClassHandle(
            self._run(self._fed().support.get_interaction_class_handle(class_name))
        )

    def getInteractionClassName(self, class_handle: InteractionClassRef) -> str:  # noqa: N802
        return str(self._run(self._fed().support.get_interaction_class_name(class_handle)))

    def getParameterHandle(  # noqa: N802
        self, class_handle: InteractionClassRef, parameter_name: str
    ) -> ParameterHandle:
        return ParameterHandle(
            self._run(self._fed().support.get_parameter_handle(class_handle, parameter_name))
        )

    def getParameterName(  # noqa: N802
        self, class_handle: InteractionClassRef, parameter_handle: ParameterRef
    ) -> str:
        return str(
            self._run(self._fed().support.get_parameter_name(class_handle, parameter_handle))
        )

    def getDimensionHandle(self, dimension_name: str) -> DimensionHandle:  # noqa: N802
        return DimensionHandle(
            self._run(self._fed().support.get_dimension_handle(dimension_name))
        )

    def getDimensionName(self, dimension_handle: DimensionRef) -> str:  # noqa: N802
        return str(self._run(self._fed().support.get_dimension_name(dimension_handle)))

    def getDimensionUpperBound(self, dimension_handle: DimensionRef) -> int:  # noqa: N802
        return int(self._run(self._fed().support.get_dimension_upper_bound(dimension_handle)))

    def getOrderType(self, order_name: str) -> int:  # noqa: N802
        return int(self._run(self._fed().support.get_order_type(order_name)))

    def getOrderName(self, order_type: int) -> str:  # noqa: N802
        return str(self._run(self._fed().support.get_order_name(order_type)))

    def getTransportationType(self, transportation_name: str) -> int:  # noqa: N802
        return int(self._run(self._fed().support.get_transportation_type(transportation_name)))

    def getTransportationName(self, transportation_type: int) -> str:  # noqa: N802
        return str(self._run(self._fed().support.get_transportation_name(transportation_type)))

    def getObjectInstanceHandle(self, object_name: str) -> ObjectInstanceHandle:  # noqa: N802
        """§6.30 — resolve a runtime object instance name to its handle.
        M27 Phase C."""
        return ObjectInstanceHandle(
            self._run(self._fed().support.get_object_instance_handle(object_name))
        )

    def getObjectInstanceName(self, object_handle: ObjectInstanceRef) -> str:  # noqa: N802
        """§6.31 — resolve a runtime object instance handle to its name."""
        return str(self._run(self._fed().support.get_object_instance_name(object_handle)))

    # --- §10.6 Handle factory accessors (M28 W2) ---

    def getAttributeHandleSetFactory(self) -> AttributeHandleSetFactory:  # noqa: N802
        return self._attribute_handle_set_factory

    def getAttributeHandleValueMapFactory(self) -> AttributeHandleValueMapFactory:  # noqa: N802
        return self._attribute_handle_value_map_factory

    def getParameterHandleValueMapFactory(self) -> ParameterHandleValueMapFactory:  # noqa: N802
        return self._parameter_handle_value_map_factory

    def getFederateHandleSetFactory(self) -> FederateHandleSetFactory:  # noqa: N802
        return self._federate_handle_set_factory

    def getDimensionHandleSetFactory(self) -> DimensionHandleSetFactory:  # noqa: N802
        return self._dimension_handle_set_factory

    def getRegionHandleSetFactory(self) -> RegionHandleSetFactory:  # noqa: N802
        return self._region_handle_set_factory

    # --- §4.11-4.13 Synchronization points (M25 Phase C) ---

    def registerFederationSynchronizationPoint(  # noqa: N802
        self, label: str, tag: bytes = b"", sync_set: FederateRefList | None = None
    ) -> None:
        self._run(
            self._fed().sync.register_synchronization_point(
                label,
                tag=tag,
                required_federates=list(sync_set) if sync_set is not None else None,
            )
        )

    def synchronizationPointAchieved(  # noqa: N802
        self, label: str, successfully: bool = True
    ) -> None:
        """§4.14 — M39 HA-2: ``successfully=False`` still counts toward
        the sync transition but lands this federate in the §4.15
        ``failed_to_sync`` set on federationSynchronized."""
        self._run(
            self._fed().sync.synchronization_point_achieved(
                label, successfully=successfully,
            )
        )

    # --- §7 Ownership Management (M25 Phase C) ---

    def unconditionalAttributeOwnershipDivestiture(  # noqa: N802
        self, object_handle: ObjectInstanceRef, attribute_handles: AttributeRefList
    ) -> None:
        self._run(
            self._fed().ownership.unconditional_divest(
                object_handle, list(attribute_handles)
            )
        )

    def negotiatedAttributeOwnershipDivestiture(  # noqa: N802
        self,
        object_handle: ObjectInstanceRef,
        attribute_handles: AttributeRefList,
        tag: bytes = b"",
        two_phase: bool = False,
    ) -> None:
        """§7.3 — M39 HA-2: ``two_phase=True`` parks the transfer on
        requestDivestitureConfirmation until :meth:`confirmDivestiture`."""
        self._run(
            self._fed().ownership.negotiated_divest(
                object_handle, list(attribute_handles), tag=tag, two_phase=two_phase
            )
        )

    def confirmDivestiture(  # noqa: N802
        self, object_handle: ObjectInstanceRef, attribute_handles: AttributeRefList
    ) -> None:
        """§7.6 — complete a parked two-phase negotiated divest (M39 HA-2)."""
        self._run(
            self._fed().ownership.confirm_divestiture(
                object_handle, list(attribute_handles)
            )
        )

    def attributeOwnershipAcquisition(  # noqa: N802
        self,
        object_handle: ObjectInstanceRef,
        attribute_handles: AttributeRefList,
        tag: bytes = b"",
    ) -> None:
        self._run(
            self._fed().ownership.acquire(object_handle, list(attribute_handles), tag=tag)
        )

    def attributeOwnershipAcquisitionIfAvailable(  # noqa: N802
        self,
        object_handle: ObjectInstanceRef,
        attribute_handles: AttributeRefList,
        tag: bytes = b"",
    ) -> None:
        """§7.9 — grab only currently-unowned attributes; nothing is
        queued (M39 HA-2). The unavailable subset arrives via the §7.10
        attributeOwnershipUnavailable callback."""
        self._run(
            self._fed().ownership.acquire(
                object_handle, list(attribute_handles), tag=tag, if_available=True
            )
        )

    def cancelNegotiatedAttributeOwnershipDivestiture(  # noqa: N802
        self, object_handle: ObjectInstanceRef, attribute_handles: AttributeRefList
    ) -> None:
        self._run(
            self._fed().ownership.cancel_negotiated_divest(
                object_handle, list(attribute_handles)
            )
        )

    def cancelAttributeOwnershipAcquisition(  # noqa: N802
        self, object_handle: ObjectInstanceRef, attribute_handles: AttributeRefList
    ) -> None:
        self._run(
            self._fed().ownership.cancel_acquire(object_handle, list(attribute_handles))
        )

    def attributeOwnershipDivestitureIfWanted(  # noqa: N802
        self, object_handle: ObjectInstanceRef, attribute_handles: AttributeRefList
    ) -> None:
        self._run(
            self._fed().ownership.divest_if_wanted(object_handle, list(attribute_handles))
        )

    def queryAttributeOwnership(  # noqa: N802
        self, object_handle: ObjectInstanceRef, attribute_handle: AttributeRef
    ) -> tuple[int, bool]:
        return cast(
            "tuple[int, bool]",
            self._run(
                self._fed().ownership.query_attribute_ownership(object_handle, attribute_handle)
            ),
        )

    def isAttributeOwnedByFederate(  # noqa: N802
        self, object_handle: ObjectInstanceRef, attribute_handle: AttributeRef
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
        self, routing_space_handle: int, dimension_handles: DimensionRefList
    ) -> RegionHandle:
        return RegionHandle(
            self._run(
                self._fed().ddm.create_region(
                    routing_space_handle, list(dimension_handles)
                )
            )
        )

    def setRangeBounds(  # noqa: N802
        self,
        region_handle: RegionRef,
        dimension_handle: DimensionRef,
        lower_bound: int,
        upper_bound: int,
    ) -> None:
        self._run(
            self._fed().ddm.set_range_bounds(
                region_handle, dimension_handle, lower=lower_bound, upper=upper_bound
            )
        )

    def commitRegionModifications(self, region_handles: RegionRefList) -> None:  # noqa: N802
        self._run(self._fed().ddm.commit_region_modifications(list(region_handles)))

    def deleteRegion(self, region_handle: RegionRef) -> None:  # noqa: N802
        self._run(self._fed().ddm.delete_region(region_handle))

    def subscribeObjectClassAttributesWithRegions(  # noqa: N802
        self,
        object_class_handle: ObjectClassRef,
        attribute_handles: AttributeRefList,
        region_handles: RegionRefList,
    ) -> None:
        self._run(
            self._fed().ddm.subscribe_object_class_attributes_with_regions(
                object_class_handle, list(attribute_handles), list(region_handles)
            )
        )

    def subscribeInteractionClassWithRegions(  # noqa: N802
        self, interaction_class_handle: InteractionClassRef, region_handles: RegionRefList
    ) -> None:
        self._run(
            self._fed().ddm.subscribe_interaction_class_with_regions(
                interaction_class_handle, list(region_handles)
            )
        )

    def registerObjectInstanceWithRegions(  # noqa: N802
        self,
        object_class_handle: ObjectClassRef,
        attributes_and_regions: dict[int | str | AttributeHandle, list[int | RegionHandle]],
        instance_name: str = "",
    ) -> ObjectInstanceHandle:
        from rti1516e.ddm import AttributeRegions

        bindings = [
            AttributeRegions(attribute_handle=int(a), region_handles=[int(r) for r in regs])
            for a, regs in attributes_and_regions.items()
        ]
        return ObjectInstanceHandle(
            self._run(
                self._fed().ddm.register_object_instance_with_regions(
                    object_class_handle, bindings, object_name=instance_name
                )
            )
        )

    def associateRegionsForUpdates(  # noqa: N802
        self,
        object_handle: ObjectInstanceRef,
        attributes_and_regions: dict[int | str | AttributeHandle, list[int | RegionHandle]],
    ) -> None:
        from rti1516e.ddm import AttributeRegions

        bindings = [
            AttributeRegions(attribute_handle=int(a), region_handles=[int(r) for r in regs])
            for a, regs in attributes_and_regions.items()
        ]
        self._run(self._fed().ddm.associate_regions_for_updates(object_handle, bindings))

    def unassociateRegionsForUpdates(  # noqa: N802
        self,
        object_handle: ObjectInstanceRef,
        attributes_and_regions: (
            dict[int | str | AttributeHandle, list[int | RegionHandle]] | None
        ) = None,
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
        object_class_handle: ObjectClassRef,
        attribute_handles: AttributeRefList,
        region_handles: RegionRefList,
    ) -> None:
        self._run(
            self._fed().ddm.unsubscribe_object_class_attributes_with_regions(
                object_class_handle, list(attribute_handles), list(region_handles)
            )
        )

    def unsubscribeInteractionClassWithRegions(  # noqa: N802
        self, interaction_class_handle: InteractionClassRef, region_handles: RegionRefList
    ) -> None:
        self._run(
            self._fed().ddm.unsubscribe_interaction_class_with_regions(
                interaction_class_handle, list(region_handles)
            )
        )

    def sendInteractionWithRegions(  # noqa: N802
        self,
        interaction_class_handle: InteractionClassRef,
        parameters: dict[int | str | ParameterHandle, bytes] | ParameterHandleValueMap,
        region_handles: RegionRefList,
        timestamp: float | None = None,
    ) -> None:
        self._run(
            self._fed().ddm.send_interaction_with_regions(
                interaction_class_handle,
                dict(parameters),
                list(region_handles),
                timestamp=timestamp,
            )
        )

    def requestAttributeValueUpdateWithRegions(  # noqa: N802
        self,
        object_class_handle: ObjectClassRef,
        attribute_handles: AttributeRefList,
        region_handles: RegionRefList,
        tag: bytes = b"",
    ) -> None:
        self._run(
            self._fed().ddm.request_attribute_value_update_with_regions(
                object_class_handle, list(attribute_handles), list(region_handles), tag=tag
            )
        )

    # --- §11 Management Object Model (M27 Phase D) ---
    # gorti exposes the MOM tracking surface via a query API rather than
    # via the MIM interaction set. The three methods below delegate to
    # fed.mom.* and are typed as Any to avoid pulling the dataclass
    # definitions into the standard.py top of file (the caller imports
    # FederationAttributes / FederateAttributes / MomInstance from
    # rti1516e.mom if they want to pattern-match).

    def queryFederationAttributes(self) -> Any:  # noqa: N802
        """§11 — return the HLAfederation MOM object snapshot.

        Returns :class:`rti1516e.mom.FederationAttributes`.
        """
        return self._run(self._fed().mom.query_federation_attributes())

    def queryFederateAttributes(self, federate_handle: FederateRef) -> Any:  # noqa: N802
        """§11 — return the HLAfederate MOM object snapshot for one federate.

        Returns :class:`rti1516e.mom.FederateAttributes`. The
        ``.found`` flag distinguishes "tracked + populated" from
        "no record (resigned or never joined)".
        """
        return self._run(self._fed().mom.query_federate_attributes(federate_handle))

    def enumerateMomInstances(self) -> Any:  # noqa: N802
        """§11 — list every active MOM instance in the federation.

        Returns ``list[rti1516e.mom.MomInstance]`` covering the
        HLAfederation singleton and one HLAfederate per joined
        federate.
        """
        return self._run(self._fed().mom.enumerate_mom_instances())

    # --- §6.1-6.5 Object instance name reservation (M26 Phase F) ---

    def reserveObjectInstanceName(self, object_name: str) -> None:  # noqa: N802
        """§6.1 — request a name reservation. Result delivered as
        objectInstanceNameReservationSucceeded / Failed callback."""
        self._run(self._fed().reservation.reserve(object_name))

    def releaseObjectInstanceName(self, object_name: str) -> None:  # noqa: N802
        """§6.4 — release a name reservation held by this federate."""
        self._run(self._fed().reservation.release(object_name))

    def reserveMultipleObjectInstanceNames(  # noqa: N802
        self, object_names: list[str]
    ) -> None:
        """§6.5 — atomic batch reservation. Result delivered as
        multipleObjectInstanceNameReservation{Succeeded,Failed}."""
        self._run(self._fed().reservation.reserve_multiple(object_names))

    # --- Callbacks: subclass overrides these ---

    def discoverObjectInstance(  # noqa: N802
        self,
        object_handle: int,
        class_name: str,
        instance_name: str,
        object_class: ObjectClassHandle | None = None,
    ) -> None:
        """Override to handle DiscoverObjectInstance.

        M39 typed-handle parity (§6.9): ``object_class`` is the typed
        :class:`ObjectClassHandle`. Overrides declared with the legacy
        3-argument signature keep working — the dispatcher only passes
        ``object_class`` to overrides that accept it. ``class_name``
        (stringified handle on the gRPC path) is DEPRECATED as the
        class identity; compare against ``object_class`` instead.
        """

    def reflectAttributeValues(  # noqa: N802
        self,
        object_handle: int,
        values: dict[str, Any],
        timestamp: float | None,
        attribute_values: dict[AttributeHandle, bytes] | None = None,
    ) -> None:
        """Override to handle ReflectAttributeValues.

        M39 typed-handle parity (§6.11): ``attribute_values`` keys the
        payloads by typed :class:`AttributeHandle`. Legacy 3-argument
        overrides keep working (the dispatcher only passes it to
        overrides that accept it); the string-keyed ``values`` map is
        DEPRECATED for handle identity.
        """

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

    # --- §10.4 Callback evocation (M26 Phase E, cheap variant) ---
    # gorti's native callback model is HLA_IMMEDIATE: _pump_events runs
    # on a background task and fires overrides as events arrive. Pitch
    # federates port more easily if the ambassador also accepts the
    # HLA_EVOKED-style `evokeCallback` API. Cheap implementation: yield
    # to the asyncio loop for approxMinTime seconds (up to approxMaxTime
    # if no callback has fired by then), and report whether any callback
    # was dispatched in the window. This is observable parity for
    # federate ports that drive their main loop via evoke, even though
    # the strict §10.4 HLA_EVOKED semantics (buffered drain, no
    # immediate dispatch) are not enforced. See docs/PITCH_PARITY.md.

    def evokeCallback(  # noqa: N802
        self, approx_min_time: float = 0.0, approx_max_time: float | None = None
    ) -> bool:
        """§10.4 — yield to the callback pump for ≥ approx_min_time seconds.

        Returns True if any callback fired during the window. If
        approx_max_time is None, the wait is exactly approx_min_time
        and the return value reflects whether a callback fired in
        that interval.

        Behaviour note: callbacks may fire OUTSIDE the window too —
        gorti is HLA_IMMEDIATE-flavored. This method observes
        firings within the window for spec-test compatibility.
        """
        start_count = self._callback_fired_count
        deadline_min = approx_min_time
        deadline_max = approx_max_time if approx_max_time is not None else approx_min_time
        if deadline_max < deadline_min:
            deadline_max = deadline_min

        async def _wait() -> bool:
            # Sleep at least approx_min_time. If a callback fires in
            # that window, return immediately after the minimum has
            # elapsed (matches Pitch's "deliver promptly but respect
            # the floor" intent).
            await asyncio.sleep(deadline_min)
            if self._callback_fired_count != start_count:
                return True
            # No callback yet — wait up to approx_max_time for one.
            remaining = deadline_max - deadline_min
            if remaining <= 0:
                return False
            # Poll cheaply rather than parking on a Condition; the
            # event pump runs in the same loop so a brief sleep loop
            # is enough.
            slept = 0.0
            tick = min(0.005, remaining)
            while slept < remaining:
                await asyncio.sleep(tick)
                if self._callback_fired_count != start_count:
                    return True
                slept += tick
            return False

        return bool(self._run(_wait()))

    def evokeMultipleCallbacks(  # noqa: N802
        self, approx_min_time: float = 0.0, approx_max_time: float | None = None
    ) -> bool:
        """§10.4 — multi-callback variant of evokeCallback.

        Same window semantics; returns True if at least one callback
        fired. Pitch federates typically loop on this:
        ``while running: rtiAmb.evokeMultipleCallbacks(0.1, 1.0)``.
        """
        return self.evokeCallback(approx_min_time, approx_max_time)

    def enableCallbacks(self) -> None:  # noqa: N802
        """§10.4 — resume callback dispatch.

        M27 Phase C. While disabled, _pump_events buffered events
        instead of firing override slots; calling enableCallbacks
        flushes the buffered events through the normal dispatch
        path. Idempotent — calling enable while already enabled is
        a no-op.
        """
        if self._callbacks_enabled:
            return
        self._callbacks_enabled = True
        # Drain the buffer through the normal dispatch path. We bump
        # _callback_fired_count via _dispatch_event, but since we
        # already counted these events on the way IN to the buffer
        # (the disabled path also bumps the counter), avoid double-
        # counting: subtract the buffer length from the counter
        # before dispatching, then let dispatch re-bump it.
        buffered = self._callback_buffer
        self._callback_buffer = []
        self._callback_fired_count -= len(buffered)
        for event in buffered:
            self._dispatch_event(event)

    def disableCallbacks(self) -> None:  # noqa: N802
        """§10.4 — suspend callback dispatch.

        M27 Phase C. While disabled, events from the event pump are
        buffered. Re-enable via enableCallbacks to drain. Buffer is
        unbounded — federates that disable for long stretches under
        high event rates should consider memory impact.
        """
        self._callbacks_enabled = False

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
        """§4.12 — this federate's sync-point registration was accepted.

        M39: fires from the wire's SyncRegistrationSucceeded event
        (stream.proto tag 22, registrant only).
        """

    def synchronizationPointRegistrationFailed(  # noqa: N802
        self, label: str, reason: SynchronizationPointFailureReason | None
    ) -> None:
        """§4.12 — this federate's sync-point registration was rejected.

        M39: fires from the wire's SyncRegistrationFailed event
        (stream.proto tag 23, registrant only).
        """

    def announceSynchronizationPoint(self, label: str, tag: bytes) -> None:  # noqa: N802
        """§4.6 — a sync point was announced to this federate.

        Pitch's announceSynchronizationPoint matches our internal
        SynchronizationPointAnnounced event.
        """

    def federationSynchronized(  # noqa: N802
        self, label: str, failed_to_sync: tuple[int, ...] = ()
    ) -> None:
        """§4.15 — all required federates have achieved the sync point.

        ``failed_to_sync`` lists federates that achieved with
        ``successfully=False`` (empty when everyone succeeded). Legacy
        1-argument overrides keep working — the dispatcher only passes
        the set to overrides that accept it.
        """

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

    # --- M26 Phase F — object instance name reservation callbacks ---

    def objectInstanceNameReservationSucceeded(self, object_name: str) -> None:  # noqa: N802
        """§6.1 — a previously requested name reservation was accepted."""

    def objectInstanceNameReservationFailed(self, object_name: str) -> None:  # noqa: N802
        """§6.1 — a previously requested name reservation was rejected."""

    def multipleObjectInstanceNameReservationSucceeded(  # noqa: N802
        self, object_names: tuple[str, ...]
    ) -> None:
        """§6.5 — an atomic batch reservation was accepted."""

    def multipleObjectInstanceNameReservationFailed(  # noqa: N802
        self, requested_names: tuple[str, ...], colliding_names: tuple[str, ...]
    ) -> None:
        """§6.5 — an atomic batch reservation was rejected (NONE reserved)."""

    # --- M39 (SDK Agent HA) — M37 wire-parity callbacks ---
    # Each is a no-op by default; override the ones you care about.

    def requestAttributeOwnershipRelease(  # noqa: N802
        self, object_handle: int, attribute_handles: tuple[int, ...], tag: bytes
    ) -> None:
        """§7.11 — another federate wants attributes this federate owns."""

    def attributeOwnershipUnavailable(  # noqa: N802
        self, object_handle: int, attribute_handles: tuple[int, ...]
    ) -> None:
        """§7.10 — acquisition-if-available found the attributes owned."""

    def initiateFederateRestore(  # noqa: N802
        self, label: str, federate_handle: int, federate_name: str
    ) -> None:
        """§4.26 — a federation restore began; load state and report back."""

    def federationRestored(self, label: str) -> None:  # noqa: N802
        """§4.14 — the federation restore completed successfully."""

    def federationNotRestored(self, label: str) -> None:  # noqa: N802
        """§4.14 — the federation restore aborted."""

    def requestFederationRestoreSucceeded(self, label: str) -> None:  # noqa: N802
        """§4.25 — this federate's restore request was accepted."""

    def requestFederationRestoreFailed(self, label: str, reason: str) -> None:  # noqa: N802
        """§4.25 — this federate's restore request was rejected."""

    def federationRestoreBegun(self) -> None:  # noqa: N802
        """§4.26 — the restore left idle (precedes initiateFederateRestore)."""

    def startRegistrationForObjectClass(  # noqa: N802
        self, object_class_handle: ObjectClassHandle
    ) -> None:
        """§5.10 — the object class gained its first subscriber."""

    def stopRegistrationForObjectClass(  # noqa: N802
        self, object_class_handle: ObjectClassHandle
    ) -> None:
        """§5.11 — the object class lost its last subscriber."""

    def turnInteractionsOn(  # noqa: N802
        self, interaction_class_handle: InteractionClassHandle
    ) -> None:
        """§5.12 — the interaction class gained its first subscriber."""

    def turnInteractionsOff(  # noqa: N802
        self, interaction_class_handle: InteractionClassHandle
    ) -> None:
        """§5.13 — the interaction class lost its last subscriber."""

    def attributesInScope(  # noqa: N802
        self, object_handle: int, attribute_handles: tuple[int, ...]
    ) -> None:
        """§6.17 — DDM region overlap brought the attributes into scope."""

    def attributesOutOfScope(  # noqa: N802
        self, object_handle: int, attribute_handles: tuple[int, ...]
    ) -> None:
        """§6.18 — the attributes dropped out of region-overlap scope."""

    def requestRetraction(  # noqa: N802
        self,
        retraction_handle: MessageRetractionHandle,
        sender_federate: FederateHandle,
    ) -> None:
        """§8.22 — a sender retracted a TSO message this federate saw."""

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

    def _invoke_compat(self, method_name: str, *args: Any, **optional: Any) -> None:
        """Call ``target.<method_name>(*args)`` plus the ``optional``
        kwargs the override's signature accepts.

        M39: lets the dispatcher pass NEW callback arguments (typed
        handles on discover/reflect, §4.15 failed_to_sync) without
        breaking subclasses written against the older, shorter
        signatures. Acceptance is resolved per (class, method) once and
        cached.
        """
        target = self._callback_target
        method = getattr(target, method_name)
        accepted_names = self._accepted_kwargs(type(target), method_name, method)
        if accepted_names is None:  # **kwargs override — pass everything
            method(*args, **optional)
            return
        method(*args, **{k: v for k, v in optional.items() if k in accepted_names})

    def _accepted_kwargs(
        self, target_type: type, method_name: str, method: Any
    ) -> frozenset[str] | None:
        """Return the parameter names ``method`` accepts beyond its
        positionals, or None when it takes ``**kwargs``. Cached per
        (target class, method name)."""
        key = (target_type, method_name)
        if key in self._accepted_kwargs_cache:
            return self._accepted_kwargs_cache[key]
        try:
            params = inspect.signature(method).parameters
            if any(
                p.kind is inspect.Parameter.VAR_KEYWORD for p in params.values()
            ):
                result: frozenset[str] | None = None
            else:
                result = frozenset(params)
        except (TypeError, ValueError):  # builtins / exotic callables
            result = frozenset()
        self._accepted_kwargs_cache[key] = result
        return result

    def _dispatch_event(self, event: Any) -> bool:
        """Dispatch one event to its callback. Return True if recognized.

        Factored out of _pump_events so evokeCallback (M26 Phase E) can
        share the dispatch path, and so the callback-fired counter is
        bumped exactly once per recognized event regardless of source.

        M27 Phase C: when callbacks are disabled, buffer the event for
        later replay via enableCallbacks. Returns True (buffered counts
        as recognized) so evokeCallback's counter still increments —
        the federate sees "a callback fired" semantically even though
        the override slot didn't run yet.
        """
        if not self._callbacks_enabled:
            self._callback_buffer.append(event)
            self._callback_fired_count += 1
            return True
        target = self._callback_target
        if isinstance(event, DiscoverObjectInstance):
            # M39 §6.9 — the typed object_class rides as an optional
            # kwarg so legacy 3-argument overrides keep working.
            self._invoke_compat(
                "discoverObjectInstance",
                event.object_handle,
                event.class_name,
                event.instance_name,
                object_class=event.object_class,
            )
        elif isinstance(event, ReflectAttributeValues):
            # M39 §6.11 — typed attribute_values as an optional kwarg.
            self._invoke_compat(
                "reflectAttributeValues",
                event.object_handle,
                event.values,
                event.timestamp,
                attribute_values=event.attribute_values,
            )
        elif isinstance(event, ReceiveInteraction):
            target.receiveInteraction(
                event.class_name, event.parameters, event.timestamp
            )
        elif isinstance(event, TimeAdvanceGrant):
            target.timeAdvanceGrant(event.time)
        elif isinstance(event, FederationHalted):
            target.federationHalted(event.cause, event.stalled_federate_handle)
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
            # M39 §4.15 — failed_to_sync as an optional kwarg.
            self._invoke_compat(
                "federationSynchronized",
                event.label,
                failed_to_sync=event.failed_to_sync,
            )
        elif isinstance(event, SynchronizationPointRegistrationSucceeded):
            target.synchronizationPointRegistrationSucceeded(event.label)
        elif isinstance(event, SynchronizationPointRegistrationFailed):
            target.synchronizationPointRegistrationFailed(event.label, event.reason)
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
        elif isinstance(event, ObjectInstanceNameReservationSucceeded):
            target.objectInstanceNameReservationSucceeded(event.object_name)
        elif isinstance(event, ObjectInstanceNameReservationFailed):
            target.objectInstanceNameReservationFailed(event.object_name)
        elif isinstance(event, MultipleObjectInstanceNameReservationSucceeded):
            target.multipleObjectInstanceNameReservationSucceeded(event.object_names)
        elif isinstance(event, MultipleObjectInstanceNameReservationFailed):
            target.multipleObjectInstanceNameReservationFailed(
                event.requested_names, event.colliding_names
            )
        # M39 (SDK Agent HA) — M37 wire-parity dispatch.
        elif isinstance(event, RequestAttributeOwnershipRelease):
            target.requestAttributeOwnershipRelease(
                event.object_handle, event.attribute_handles, event.tag
            )
        elif isinstance(event, AttributeOwnershipUnavailable):
            target.attributeOwnershipUnavailable(
                event.object_handle, event.attribute_handles
            )
        elif isinstance(event, InitiateFederateRestore):
            target.initiateFederateRestore(
                event.label, event.federate_handle, event.federate_name
            )
        elif isinstance(event, FederationRestored):
            target.federationRestored(event.label)
        elif isinstance(event, FederationNotRestored):
            target.federationNotRestored(event.label)
        elif isinstance(event, RequestFederationRestoreSucceeded):
            target.requestFederationRestoreSucceeded(event.label)
        elif isinstance(event, RequestFederationRestoreFailed):
            target.requestFederationRestoreFailed(event.label, event.reason)
        elif isinstance(event, FederationRestoreBegun):
            target.federationRestoreBegun()
        elif isinstance(event, StartRegistrationForObjectClass):
            target.startRegistrationForObjectClass(
                ObjectClassHandle(event.object_class_handle)
            )
        elif isinstance(event, StopRegistrationForObjectClass):
            target.stopRegistrationForObjectClass(
                ObjectClassHandle(event.object_class_handle)
            )
        elif isinstance(event, TurnInteractionsOn):
            target.turnInteractionsOn(
                InteractionClassHandle(event.interaction_class_handle)
            )
        elif isinstance(event, TurnInteractionsOff):
            target.turnInteractionsOff(
                InteractionClassHandle(event.interaction_class_handle)
            )
        elif isinstance(event, AttributesInScope):
            target.attributesInScope(event.object_handle, event.attribute_handles)
        elif isinstance(event, AttributesOutOfScope):
            target.attributesOutOfScope(event.object_handle, event.attribute_handles)
        elif isinstance(event, RequestRetraction):
            target.requestRetraction(
                MessageRetractionHandle(event.retraction_handle),
                FederateHandle(event.sender_federate),
            )
        else:
            return False
        self._callback_fired_count += 1
        return True

    async def _pump_events(self) -> None:
        """Drain Federate.events() and dispatch to the appropriate callback."""
        federate = self._federate
        if federate is None:
            return
        try:
            async for event in federate.events():
                self._dispatch_event(event)
        except asyncio.CancelledError:
            return
