"""Layer-1 SDK client for the DDMService (M12 W2).

Thin async wrapper around the generated ``DDMServiceStub`` that
exposes IEEE 1516-2010 §6 Data Distribution Management — region
creation, range bounds, region-scoped pub/sub, and overlap-driven
fan-out filtering.

Composition:

    async with rti.join_federation(spec, federate_name="alice") as fed:
        space = await fed.ddm.lookup_routing_space("RoutingSpace")
        dim = await fed.ddm.lookup_dimension(space, "X")
        region = await fed.ddm.create_region(space, [dim])
        await fed.ddm.set_range_bounds(region, dim, lower=10, upper=20)
        await fed.ddm.commit_region_modifications([region])

The ``fed.ddm`` accessor (see :class:`Federate.ddm` in
``connection.py``) lazily constructs one :class:`DDMClient` per
federate, bound to the same gRPC channel + federation_name +
federate_handle the federate already holds.

User-facing two-step (M12 W1 handoff): the cut-3
``RegisterObjectInstanceWithRegions`` Go-side handler accepts the
request shape but does NOT mint the object handle on its own — the
SDK is responsible for calling :class:`ObjectService.RegisterObject`
first to mint a handle, then forwarding to
:class:`DDMService.RegisterObjectInstanceWithRegions` to record the
per-attribute region bindings. The :meth:`register_object_instance_with_regions`
helper performs both calls and returns the minted handle.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING, TypeAlias

from rti1516e._grpc_errors import translate_rpc_error
from rti1516e.handles import (
    AttributeHandle,
    DimensionHandle,
    InteractionClassHandle,
    ObjectClassHandle,
    ObjectInstanceHandle,
    ParameterHandle,
    RegionHandle,
)
from rti1516e.sets import (
    AttributeHandleSet,
    DimensionHandleSet,
    ParameterHandleValueMap,
    RegionHandleSet,
)

if TYPE_CHECKING:  # pragma: no cover - type-check imports only
    import grpc

# M28 — IEEE 1516 portability type aliases. Public service entry points widen to
# accept typed handles + typed sets; internal protobuf request fields
# stay bare int via existing ``int(h)`` coercion.
_ObjectClassRef: TypeAlias = "int | ObjectClassHandle"
_InteractionClassRef: TypeAlias = "int | InteractionClassHandle"
_ObjectInstanceRef: TypeAlias = "int | ObjectInstanceHandle"
_DimensionRef: TypeAlias = "int | DimensionHandle"
_RegionRef: TypeAlias = "int | RegionHandle"
_AttributeRefList: TypeAlias = "list[int | AttributeHandle] | AttributeHandleSet"
_DimensionRefList: TypeAlias = "list[int | DimensionHandle] | DimensionHandleSet"
_RegionRefList: TypeAlias = "list[int | RegionHandle] | RegionHandleSet"
_ParameterValueDict: TypeAlias = (
    "dict[int | ParameterHandle, bytes] | ParameterHandleValueMap"
)


@dataclass(frozen=True)
class AttributeRegions:
    """Per-attribute region binding for region-scoped pub/sub."""

    attribute_handle: int
    region_handles: list[int]


class DDMClient:
    """Federate-bound client for the DDMService gRPC surface."""

    def __init__(
        self,
        channel: grpc.aio.Channel,
        *,
        federation_name: str,
        federate_handle: int,
    ) -> None:
        from rti.v1 import ddm_pb2_grpc, object_pb2_grpc

        self._stub = ddm_pb2_grpc.DDMServiceStub(channel)
        # ObjectServiceStub is needed for the
        # register_object_instance_with_regions two-step (mint handle
        # via ObjectService, then record region bindings via DDMService).
        self._object_stub = object_pb2_grpc.ObjectServiceStub(channel)
        self._federation_name = federation_name
        self._federate_handle = int(federate_handle)

    # --- Routing-space + dimension handle lookup (§6.4) -----------------------

    async def lookup_routing_space(self, name: str) -> int:
        """Return the routing-space handle for ``name``.

        Returns 0 (the proto's "not found" sentinel) if the FOM does
        not declare a routing space with this name. Callers that need
        to disambiguate "space exists with handle 0" from "missing"
        should use the underlying response's ``found`` field —
        exposed via :meth:`lookup_routing_space_response` if needed.
        """
        from rti.v1 import common_pb2, ddm_pb2

        req = ddm_pb2.LookupRoutingSpaceRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            name=name,
        )
        try:
            resp = await self._stub.LookupRoutingSpace(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)
        return int(resp.routing_space_handle) if resp.found else 0

    async def lookup_dimension(self, routing_space_handle: int, name: str) -> int:
        """Return the dimension handle for ``name`` in the given routing space.

        Returns 0 if not found (see :meth:`lookup_routing_space`).
        """
        from rti.v1 import common_pb2, ddm_pb2

        req = ddm_pb2.LookupDimensionRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            routing_space_handle=int(routing_space_handle),
            name=name,
        )
        try:
            resp = await self._stub.LookupDimension(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)
        return int(resp.dimension_handle) if resp.found else 0

    # --- Region lifecycle (§6.5-6.6) -----------------------------------------

    async def create_region(
        self, routing_space_handle: int, dimension_handles: _DimensionRefList
    ) -> int:
        """§6.5 — create a region in the given routing space; return handle."""
        from rti.v1 import common_pb2, ddm_pb2

        req = ddm_pb2.CreateRegionRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            routing_space_handle=int(routing_space_handle),
            dimension_handles=[int(h) for h in dimension_handles],
        )
        try:
            resp = await self._stub.CreateRegion(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)
        return int(resp.region_handle)

    async def set_range_bounds(
        self,
        region_handle: _RegionRef,
        dimension_handle: _DimensionRef,
        *,
        lower: int,
        upper: int,
    ) -> None:
        """§6.5 — stage a pending range update on the region.

        Bounds are not committed until :meth:`commit_region_modifications`
        succeeds; subscribers do not see them until then.
        """
        from rti.v1 import common_pb2, ddm_pb2

        req = ddm_pb2.SetRangeBoundsRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            region_handle=int(region_handle),
            dimension_handle=int(dimension_handle),
            bounds=ddm_pb2.Range(lower=int(lower), upper=int(upper)),
        )
        try:
            await self._stub.SetRangeBounds(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def commit_region_modifications(self, region_handles: _RegionRefList) -> None:
        """§6.5 — atomically commit pending bounds across the supplied regions."""
        from rti.v1 import common_pb2, ddm_pb2

        req = ddm_pb2.CommitRegionRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            region_handles=[int(h) for h in region_handles],
        )
        try:
            await self._stub.CommitRegionModifications(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def delete_region(self, region_handle: _RegionRef) -> None:
        """§6.6 — delete a region. Pending publishers/subscribers detach."""
        from rti.v1 import common_pb2, ddm_pb2

        req = ddm_pb2.DeleteRegionRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            region_handle=int(region_handle),
        )
        try:
            await self._stub.DeleteRegion(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def query_bounds(
        self, region_handle: _RegionRef, dimension_handle: _DimensionRef
    ) -> tuple[int, int] | None:
        """Return committed ``(lower, upper)`` for (region, dim), or None."""
        from rti.v1 import common_pb2, ddm_pb2

        req = ddm_pb2.QueryBoundsRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            region_handle=int(region_handle),
            dimension_handle=int(dimension_handle),
        )
        try:
            resp = await self._stub.QueryBounds(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)
        if not resp.found:
            return None
        return int(resp.bounds.lower), int(resp.bounds.upper)

    # --- Region-scoped pub/sub (§6.10, §6.13) --------------------------------

    async def subscribe_object_class_attributes_with_regions(
        self,
        object_class_handle: _ObjectClassRef,
        attribute_handles: _AttributeRefList,
        region_handles: _RegionRefList,
    ) -> None:
        """§6.10 — region-scoped subscription to object-class attributes."""
        from rti.v1 import common_pb2, ddm_pb2

        req = ddm_pb2.SubscribeOCAWithRegionsRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            object_class_handle=int(object_class_handle),
            attribute_handles=[int(h) for h in attribute_handles],
            region_handles=[int(h) for h in region_handles],
        )
        try:
            await self._stub.SubscribeObjectClassAttributesWithRegions(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def subscribe_interaction_class_with_regions(
        self,
        interaction_class_handle: _InteractionClassRef,
        region_handles: _RegionRefList,
    ) -> None:
        """§6.13 — region-scoped subscription to an interaction class."""
        from rti.v1 import common_pb2, ddm_pb2

        req = ddm_pb2.SubscribeICWithRegionsRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            interaction_class_handle=int(interaction_class_handle),
            region_handles=[int(h) for h in region_handles],
        )
        try:
            await self._stub.SubscribeInteractionClassWithRegions(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    # --- Object-instance + regions (§6.7) ------------------------------------

    async def register_object_instance_with_regions(
        self,
        object_class_handle: _ObjectClassRef,
        attribute_regions: list[AttributeRegions],
        *,
        object_name: str = "",
    ) -> int:
        """§6.7 — register an instance with per-attribute region bindings.

        Implements the M12 W1 two-step handoff: the cut-3 Go-side
        ``DDMService.RegisterObjectInstanceWithRegions`` does NOT mint
        the object handle (its response carries handle 0 by design,
        see ``rti/internal/transport/grpc/ddm.go``). The SDK first
        invokes :class:`ObjectService.RegisterObjectInstance` to mint
        the handle, then calls the DDM RPC to record the bindings.

        Returns the minted object handle.
        """
        from rti.v1 import common_pb2, ddm_pb2, object_pb2

        # Step 1: mint the object handle via ObjectService.
        register_req = object_pb2.RegisterObjectRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            object_class_handle=int(object_class_handle),
            object_name=object_name,
        )
        try:
            register_resp = await self._object_stub.RegisterObjectInstance(register_req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)
        object_handle = int(register_resp.object_handle)
        # Echo back the actually-assigned name (rtid generates one if
        # the caller passed an empty string). The user-facing return
        # is just the handle; the name is recorded on the wire by
        # ObjectService and again by DDMService for symmetry.
        actual_name = register_resp.object_name or object_name

        # Step 2: record per-attribute region bindings on DDMService.
        # The proto's response carries handle 0 (the manager records
        # bindings but does not re-mint a handle); we ignore the
        # response since the authoritative handle is already in hand.
        ddm_req = ddm_pb2.RegisterObjectWithRegionsRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            object_class_handle=int(object_class_handle),
            object_name=actual_name,
            attribute_regions=[
                ddm_pb2.AttributeRegions(
                    attribute_handle=int(ar.attribute_handle),
                    region_handles=[int(h) for h in ar.region_handles],
                )
                for ar in attribute_regions
            ],
        )
        try:
            await self._stub.RegisterObjectInstanceWithRegions(ddm_req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)
        return object_handle

    # --- M23 W5 — §9 missing services -----------------------------------

    async def associate_regions_for_updates(
        self, object_handle: _ObjectInstanceRef, bindings: list[AttributeRegions],
    ) -> None:
        """§9.6 — record per-attribute region associations on an existing instance."""
        from rti.v1 import common_pb2, ddm_pb2

        req = ddm_pb2.AssociateRegionsForUpdatesRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            object_handle=int(object_handle),
            attribute_regions=[
                ddm_pb2.AttributeRegions(
                    attribute_handle=int(b.attribute_handle),
                    region_handles=[int(r) for r in b.region_handles],
                )
                for b in bindings
            ],
        )
        try:
            await self._stub.AssociateRegionsForUpdates(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def unassociate_regions_for_updates(
        self,
        object_handle: _ObjectInstanceRef,
        bindings: list[AttributeRegions] | None = None,
    ) -> None:
        """§9.7 — drop matching attr-region pairs. None or empty drops ALL."""
        from rti.v1 import common_pb2, ddm_pb2

        pairs = bindings or []
        req = ddm_pb2.UnassociateRegionsForUpdatesRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            object_handle=int(object_handle),
            attribute_regions=[
                ddm_pb2.AttributeRegions(
                    attribute_handle=int(b.attribute_handle),
                    region_handles=[int(r) for r in b.region_handles],
                )
                for b in pairs
            ],
        )
        try:
            await self._stub.UnassociateRegionsForUpdates(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def unsubscribe_object_class_attributes_with_regions(
        self,
        object_class_handle: _ObjectClassRef,
        attribute_handles: _AttributeRefList,
        region_handles: _RegionRefList,
    ) -> None:
        """§9.9 — drop the region-scoped subscription on (cls, attr)."""
        from rti.v1 import common_pb2, ddm_pb2

        req = ddm_pb2.UnsubscribeOCAWithRegionsRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            object_class_handle=int(object_class_handle),
            attribute_handles=[int(h) for h in attribute_handles],
            region_handles=[int(h) for h in region_handles],
        )
        try:
            await self._stub.UnsubscribeObjectClassAttributesWithRegions(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def unsubscribe_interaction_class_with_regions(
        self, interaction_class_handle: _InteractionClassRef, region_handles: _RegionRefList,
    ) -> None:
        """§9.11 — drop the region-scoped interaction subscription."""
        from rti.v1 import common_pb2, ddm_pb2

        req = ddm_pb2.UnsubscribeICWithRegionsRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            interaction_class_handle=int(interaction_class_handle),
            region_handles=[int(h) for h in region_handles],
        )
        try:
            await self._stub.UnsubscribeInteractionClassWithRegions(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def send_interaction_with_regions(
        self,
        interaction_class_handle: _InteractionClassRef,
        parameters: _ParameterValueDict,
        region_handles: _RegionRefList,
        timestamp: float | None = None,
    ) -> None:
        """§9.12 — region-scoped interaction send."""
        from rti.v1 import common_pb2, ddm_pb2

        req = ddm_pb2.SendInteractionWithRegionsRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            interaction_class_handle=int(interaction_class_handle),
            parameters={int(k): bytes(v) for k, v in parameters.items()},
            region_handles=[int(h) for h in region_handles],
        )
        if timestamp is not None:
            req.logical_time = float(timestamp)
        try:
            await self._stub.SendInteractionWithRegions(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def request_attribute_value_update_with_regions(
        self,
        object_class_handle: _ObjectClassRef,
        attribute_handles: _AttributeRefList,
        region_handles: _RegionRefList,
        tag: bytes = b"",
    ) -> None:
        """§9.13 — class-scoped pull filtered by region overlap."""
        from rti.v1 import common_pb2, ddm_pb2

        req = ddm_pb2.RequestAttributeValueUpdateWithRegionsRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            object_class_handle=int(object_class_handle),
            attribute_handles=[int(h) for h in attribute_handles],
            region_handles=[int(h) for h in region_handles],
            user_supplied_tag=tag,
        )
        try:
            await self._stub.RequestAttributeValueUpdateWithRegions(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)
