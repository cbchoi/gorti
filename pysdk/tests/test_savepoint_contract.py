"""Explicit Save/Restore forwarding and status-vector regression tests."""

from __future__ import annotations

import asyncio
from types import SimpleNamespace
from unittest.mock import AsyncMock

import grpc
import pytest

from rti1516e._transport import _ensure_generated_path
from rti1516e.errors import RtiError
from rti1516e.savepoint import (
    FederateRestoreStatus,
    FederateSaveStatus,
    RestoreState,
    RestoreStatus,
    RestoreStatusResponse,
    SavepointClient,
    SaveState,
    SaveStatus,
    SaveStatusResponse,
)
from rti1516e.standard import Rti1516eAmbassador

_ensure_generated_path()
from rti.v1 import common_pb2, savepoint_pb2, savepoint_pb2_grpc  # noqa: E402


@pytest.mark.asyncio
async def test_explicit_forwarding_and_status_vectors() -> None:
    calls = []

    class Service(savepoint_pb2_grpc.SavepointServiceServicer):
        async def FederateSaveBegun(self, request, context):  # noqa: N802
            calls.append(("begun", request))
            return common_pb2.Empty()

        async def FederateSaveComplete(self, request, context):  # noqa: N802
            calls.append(("complete", request))
            await context.abort(grpc.StatusCode.FAILED_PRECONDITION, "save has not begun")

        async def FederateRestoreNotComplete(self, request, context):  # noqa: N802
            calls.append(("restore-failed", request))
            return common_pb2.Empty()

        async def QuerySaveState(self, request, context):  # noqa: N802
            calls.append(("save-query", request))
            return savepoint_pb2.QuerySaveStateResponse(
                state=savepoint_pb2.SAVE_STATE_SAVED,
                active_label="save",
                federate_statuses=[
                    savepoint_pb2.FederateSaveStatus(
                        federate_handle=17,
                        status=savepoint_pb2.FEDERATE_SAVE_PROGRESS_SAVING,
                    ),
                    savepoint_pb2.FederateSaveStatus(
                        federate_handle=23,
                        status=savepoint_pb2.FEDERATE_SAVE_PROGRESS_WAITING_FOR_FEDERATION_TO_SAVE,
                    ),
                ],
            )

        async def QueryRestoreState(self, request, context):  # noqa: N802
            calls.append(("restore-query", request))
            return savepoint_pb2.QueryRestoreStateResponse(
                state=savepoint_pb2.RESTORE_STATE_FAILED,
                active_label="restore",
                federate_statuses=[
                    savepoint_pb2.FederateRestoreStatus(
                        pre_restore_handle=17,
                        post_restore_handle=4,
                        status=savepoint_pb2.FEDERATE_RESTORE_PROGRESS_RESTORING,
                    )
                ],
            )

    server = grpc.aio.server()
    savepoint_pb2_grpc.add_SavepointServiceServicer_to_server(Service(), server)
    port = server.add_insecure_port("127.0.0.1:0")
    await server.start()
    try:
        async with grpc.aio.insecure_channel(f"127.0.0.1:{port}") as channel:
            client = SavepointClient(channel, federation_name="fed", federate_handle=17)
            with pytest.raises(RtiError):
                await client.federate_save_complete()
            assert [name for name, _ in calls] == ["complete"]
            await client.federate_save_begun()
            await client.federate_restore_not_complete()
            assert [name for name, _ in calls] == ["complete", "begun", "restore-failed"]
            for _, request in calls:
                assert request.federation_name == "fed"
                assert request.federate_handle == 17
                assert request.wire_version == common_pb2.WIRE_VERSION_V1
            save = await client.query_federation_save_status()
            assert save.active_label == "save"
            assert save.federate_statuses == (
                FederateSaveStatus(17, SaveStatus.FEDERATE_SAVING),
                FederateSaveStatus(23, SaveStatus.FEDERATE_WAITING_FOR_FEDERATION_TO_SAVE),
            )
            restore = await client.query_federation_restore_status()
            assert restore.active_label == "restore"
            assert restore.federate_statuses == (
                FederateRestoreStatus(17, 4, RestoreStatus.FEDERATE_RESTORING),
            )
            assert await client.query_save_state("old-save") == SaveState.SAVED
            assert calls[-1][1].label == "old-save"
            assert await client.query_restore_state("old-restore") == RestoreState.FAILED
            assert calls[-1][1].label == "old-restore"
    finally:
        await server.stop(None)


def test_standard_queries_deliver_real_vectors_and_honor_callback_disable() -> None:
    save = SaveStatusResponse(
        SaveState.INITIATED, "save", (FederateSaveStatus(17, SaveStatus.FEDERATE_SAVING),)
    )
    restore = RestoreStatusResponse(
        RestoreState.INITIATED,
        "restore",
        (FederateRestoreStatus(17, 4, RestoreStatus.FEDERATE_RESTORING),),
    )
    client = SimpleNamespace(
        federate_save_begun=AsyncMock(),
        federate_restore_not_complete=AsyncMock(),
        query_federation_save_status=AsyncMock(return_value=save),
        query_federation_restore_status=AsyncMock(return_value=restore),
    )
    ambassador = Rti1516eAmbassador()
    ambassador._federate = SimpleNamespace(savepoint=client)
    ambassador._run = asyncio.run
    received = []
    ambassador.federationSaveStatusResponse = lambda values: received.append(("save", values))
    ambassador.federationRestoreStatusResponse = lambda values: received.append(("restore", values))
    ambassador.federateSaveBegun()
    ambassador.federateRestoreNotComplete()
    client.federate_save_begun.assert_awaited_once_with()
    client.federate_restore_not_complete.assert_awaited_once_with()
    assert ambassador.queryFederationSaveStatus() == save
    assert received == [("save", save.federate_statuses)]
    ambassador.disableCallbacks()
    assert ambassador.queryFederationRestoreStatus() == restore
    assert len(received) == 1
    ambassador.enableCallbacks()
    assert received[-1] == ("restore", restore.federate_statuses)
    client.query_federation_save_status.side_effect = RuntimeError("query failed")
    with pytest.raises(RuntimeError, match="query failed"):
        ambassador.queryFederationSaveStatus()
    assert len(received) == 2
