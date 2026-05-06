"""Layer-1 SDK client for the SavepointService (M12 W2).

Thin async wrapper around the generated ``SavepointServiceStub`` that
exposes IEEE 1516-2010 §4.8-4.15 federation save/restore.

Composition:

    async with rti.join_federation(spec, federate_name="alice") as fed:
        await fed.savepoint.request_federation_save("checkpoint-1")
        await fed.savepoint.federate_save_complete()
        state = await fed.savepoint.query_save_state("checkpoint-1")
        # state == SaveState.SAVED

The ``fed.savepoint`` accessor (see :class:`Federate.savepoint` in
``connection.py``) lazily constructs one :class:`SavepointClient` per
federate, bound to the same gRPC channel + federation_name +
federate_handle the federate already holds.

M12 W2 follow-up (deferral #1 closed for save half): the proto
``FederateEvent`` oneof now carries ``InitiateFederateSave`` (tag 40),
``FederationSaved`` (tag 41), and ``FederationNotSaved`` (tag 42).
Federates receive these callbacks over StreamService.Events as
:class:`rti1516e.events.InitiateFederateSave`,
:class:`rti1516e.events.FederationSaved`, and
:class:`rti1516e.events.FederationNotSaved`. Restore lifecycle
callbacks (``initiateFederateRestore`` / ``federationRestored``)
remain Query-only at this cut — the SDK's M12 restore tests use Query
observation and a future cut adds the matching variants.

Save/restore aggregation (cut-3 production rtid wiring): the
production server's savepoint manager runs in *dynamic* mode (no
MembersResolver). In this mode, the first federate to call
:meth:`federate_save_complete` adds itself to the required set, and
the save closes out as ``SAVED`` immediately. This is sufficient for
single-federate save/restore round trips, which is what the M12 W2
spec test exercises; multi-federate aggregation requires a
MembersResolver wiring (tracked as a cut-3 follow-up in
``docs/reports/M9/agent-a.md``).
"""

from __future__ import annotations

from enum import IntEnum
from typing import TYPE_CHECKING

from rti1516e._grpc_errors import translate_rpc_error

if TYPE_CHECKING:  # pragma: no cover - type-check imports only
    import grpc


class SaveState(IntEnum):
    """Mirrors ``rti.v1.SaveState`` for clean Pythonic comparison.

    Values intentionally match the proto enum integer values so this
    enum can be used as a drop-in replacement when the SDK exposes
    state to user code.
    """

    UNSPECIFIED = 0
    IDLE = 1
    INITIATED = 2
    SAVED = 3
    NOT_SAVED = 4


class RestoreState(IntEnum):
    """Mirrors ``rti.v1.RestoreState``."""

    UNSPECIFIED = 0
    IDLE = 1
    LOADING = 2
    INITIATED = 3
    COMPLETED = 4
    FAILED = 5


class SavepointClient:
    """Federate-bound client for the SavepointService gRPC surface."""

    def __init__(
        self,
        channel: grpc.aio.Channel,
        *,
        federation_name: str,
        federate_handle: int,
    ) -> None:
        from rti.v1 import savepoint_pb2_grpc

        self._stub = savepoint_pb2_grpc.SavepointServiceStub(channel)
        self._federation_name = federation_name
        self._federate_handle = int(federate_handle)

    # --- Save protocol (§4.8-4.11) ------------------------------------------

    async def request_federation_save(
        self, label: str, *, save_time: float | None = None
    ) -> None:
        """§4.8 — start a save. Optional ``save_time`` pins it to logical time.

        ``save_time=None`` saves now at the current sync point;
        passing a float pins the save to that logical timestamp.
        """
        from rti.v1 import common_pb2, savepoint_pb2

        req = savepoint_pb2.RequestFederationSaveRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            label=label,
        )
        if save_time is not None:
            req.save_time = float(save_time)
        try:
            await self._stub.RequestFederationSave(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def federate_save_complete(self) -> None:
        """§4.10 — notify the RTI this federate has saved successfully."""
        from rti.v1 import common_pb2, savepoint_pb2

        req = savepoint_pb2.FederateSaveResponseRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
        )
        try:
            await self._stub.FederateSaveComplete(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def federate_save_not_complete(self) -> None:
        """§4.10 — notify the RTI this federate failed to save."""
        from rti.v1 import common_pb2, savepoint_pb2

        req = savepoint_pb2.FederateSaveResponseRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
        )
        try:
            await self._stub.FederateSaveNotComplete(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def query_save_state(self, label: str) -> SaveState:
        """§4.11 — return the current save state for (federation, label)."""
        from rti.v1 import common_pb2, savepoint_pb2

        req = savepoint_pb2.QuerySaveStateRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            label=label,
        )
        try:
            resp = await self._stub.QuerySaveState(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)
        return SaveState(int(resp.state))

    # --- Restore protocol (§4.12-4.15) --------------------------------------

    async def request_federation_restore(self, label: str) -> None:
        """§4.12 — start a restore from the bundle stored under ``label``."""
        from rti.v1 import common_pb2, savepoint_pb2

        req = savepoint_pb2.RequestFederationRestoreRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
            label=label,
        )
        try:
            await self._stub.RequestFederationRestore(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def federate_restore_complete(self) -> None:
        """§4.14 — notify the RTI this federate has restored successfully."""
        from rti.v1 import common_pb2, savepoint_pb2

        req = savepoint_pb2.FederateRestoreResponseRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            federate_handle=self._federate_handle,
        )
        try:
            await self._stub.FederateRestoreComplete(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)

    async def query_restore_state(self, label: str) -> RestoreState:
        """§4.15 — return the current restore state for (federation, label)."""
        from rti.v1 import common_pb2, savepoint_pb2

        req = savepoint_pb2.QueryRestoreStateRequest(
            wire_version=common_pb2.WireVersion.WIRE_VERSION_V1,
            federation_name=self._federation_name,
            label=label,
        )
        try:
            resp = await self._stub.QueryRestoreState(req)
        except Exception as exc:  # noqa: BLE001
            translate_rpc_error(exc)
        return RestoreState(int(resp.state))
