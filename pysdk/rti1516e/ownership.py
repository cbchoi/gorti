"""Layer-1 SDK client for the OwnershipService (M12 W2).

Thin async wrapper around the generated ``OwnershipServiceStub`` that
exposes the IEEE 1516-2010 §7 negotiated divest+acquire two-phase
protocol plus per-attribute ownership queries.

Composition:

    async with rti.join_federation(spec, federate_name="alice") as fed:
        await fed.ownership.negotiated_divest(obj, [attr], tag=b"...")

The ``fed.ownership`` accessor (see :class:`Federate.ownership` in
``connection.py``) lazily constructs one :class:`OwnershipClient` per
federate, bound to the same gRPC channel + federation_name +
federate_handle the federate already holds.

Cut-3 wire limitation: the proto ``FederateEvent`` oneof does not yet
carry ``RequestAttributeOwnershipAssumption`` /
``AttributeOwnershipAcquisitionNotification`` variants, so the
ownership-transfer callbacks are NOT delivered over the
StreamService.Events stream in this cut. Tests observe the transfer
via the (already-wire-exposed) :meth:`query_attribute_ownership` /
:meth:`is_attribute_owned_by_federate` round-trip RPCs, which return
the post-transfer owner directly.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

from rti1516e._grpc_errors import translate_rpc_error

if TYPE_CHECKING:  # pragma: no cover - type-check imports only
    import grpc


class OwnershipClient:
    """Federate-bound client for the OwnershipService gRPC surface."""

    def __init__(
        self,
        channel: grpc.aio.Channel,
        *,
        federation_name: str,
        federate_handle: int,
    ) -> None:
        from rti.v1 import ownership_pb2_grpc

        self._stub = ownership_pb2_grpc.OwnershipServiceStub(channel)
        self._federation_name = federation_name
        self._federate_handle = int(federate_handle)

    # --- Divest / acquire (§7.2-7.7) ----------------------------------------

    async def unconditional_divest(
        self, object_handle: int, attribute_handles: list[int]
    ) -> None:
        """§7.2 — drop ownership without asking anyone to take it."""
        from rti.v1 import common_pb2, ownership_pb2

        req = ownership_pb2.UnconditionalDivestRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            object_handle=int(object_handle),
            attribute_handles=[int(h) for h in attribute_handles],
        )
        try:
            await self._stub.UnconditionalAttributeOwnershipDivestiture(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def negotiated_divest(
        self,
        object_handle: int,
        attribute_handles: list[int],
        *,
        tag: bytes = b"",
    ) -> None:
        """§7.3 — offer ownership; transfer fires when an acquirer arrives."""
        from rti.v1 import common_pb2, ownership_pb2

        req = ownership_pb2.NegotiatedDivestRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            object_handle=int(object_handle),
            attribute_handles=[int(h) for h in attribute_handles],
            tag=bytes(tag),
        )
        try:
            await self._stub.NegotiatedAttributeOwnershipDivestiture(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def acquire(
        self,
        object_handle: int,
        attribute_handles: list[int],
        *,
        tag: bytes = b"",
    ) -> None:
        """§7.4 — request ownership; matched against pending divestitures."""
        from rti.v1 import common_pb2, ownership_pb2

        req = ownership_pb2.AcquireRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            object_handle=int(object_handle),
            attribute_handles=[int(h) for h in attribute_handles],
            tag=bytes(tag),
        )
        try:
            await self._stub.AttributeOwnershipAcquisition(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def cancel_negotiated_divest(
        self, object_handle: int, attribute_handles: list[int]
    ) -> None:
        """§7.5 — withdraw a pending negotiated divestiture."""
        from rti.v1 import common_pb2, ownership_pb2

        req = ownership_pb2.CancelDivestRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            object_handle=int(object_handle),
            attribute_handles=[int(h) for h in attribute_handles],
        )
        try:
            await self._stub.CancelNegotiatedAttributeOwnershipDivestiture(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def cancel_acquire(
        self, object_handle: int, attribute_handles: list[int]
    ) -> None:
        """§7.6 — withdraw a pending acquisition."""
        from rti.v1 import common_pb2, ownership_pb2

        req = ownership_pb2.CancelAcquireRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            object_handle=int(object_handle),
            attribute_handles=[int(h) for h in attribute_handles],
        )
        try:
            await self._stub.CancelAttributeOwnershipAcquisition(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def divest_if_wanted(
        self, object_handle: int, attribute_handles: list[int]
    ) -> None:
        """§7.7 — transfer ownership only if a matching acquirer is queued."""
        from rti.v1 import common_pb2, ownership_pb2

        req = ownership_pb2.DivestIfWantedRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            object_handle=int(object_handle),
            attribute_handles=[int(h) for h in attribute_handles],
        )
        try:
            await self._stub.AttributeOwnershipDivestitureIfWanted(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    # --- Queries (§7.8-7.9) -------------------------------------------------

    async def query_attribute_ownership(
        self, object_handle: int, attribute_handle: int
    ) -> tuple[int, bool]:
        """§7.8 — return ``(owner_handle, owned)``.

        ``owned=False`` indicates the attribute is mid-transfer or
        unowned (post-unconditional-divest with no acquirer yet); in
        that case ``owner_handle`` is 0 and should be ignored.
        """
        from rti.v1 import common_pb2, ownership_pb2

        req = ownership_pb2.QueryOwnershipRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            object_handle=int(object_handle),
            attribute_handle=int(attribute_handle),
        )
        try:
            resp = await self._stub.QueryAttributeOwnership(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)
        return int(resp.owner_federate_handle), bool(resp.owned)

    async def is_attribute_owned_by_federate(
        self, object_handle: int, attribute_handle: int
    ) -> bool:
        """§7.9 — return True iff this federate currently owns the attribute."""
        from rti.v1 import common_pb2, ownership_pb2

        req = ownership_pb2.IsOwnedRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            object_handle=int(object_handle),
            attribute_handle=int(attribute_handle),
        )
        try:
            resp = await self._stub.IsAttributeOwnedByFederate(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)
        return bool(resp.owned)
