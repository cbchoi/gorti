"""Restore success cannot precede validated SDK staging."""

import asyncio
from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest

from rti1516e.connection import Federate
from rti1516e.events import FederationNotRestored, FederationRestored, InitiateFederateRestore
from rti1516e.savepoint import SavepointClient
from rti1516e.standard import Rti1516eAmbassador
from tests.test_runtime_checkpoint import transport


def client_for(target):
    fed = Federate(connection=object(), transport=target, handle=8, name="saved")
    client = fed.savepoint
    client._client._stub = SimpleNamespace(
        FederateRestoreComplete=AsyncMock(), FederateRestoreNotComplete=AsyncMock()
    )
    return fed, client, client._client._stub


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "stage", ["missing", "invalid", "invalid-after-valid", "failed", "terminal"]
)
async def test_unready_restore_never_sends_success_rpc(stage):
    target = transport()
    _, client, stub = client_for(target)
    if stage not in ("missing", "invalid"):
        await target._apply_restore_terminal(8, InitiateFederateRestore("s", 8, "saved"))
    if stage.startswith("invalid"):
        with pytest.raises(ValueError):
            await target._apply_restore_terminal(8, InitiateFederateRestore("s", 8, "saved", b"{"))
    elif stage == "failed":
        await target._apply_restore_terminal(8, FederationNotRestored("s"))
    elif stage == "terminal":
        await target._apply_restore_terminal(8, FederationRestored("s", 8, 1))
    with pytest.raises(RuntimeError, match="validated SDK restore stage"):
        await client.federate_restore_complete()
    stub.FederateRestoreComplete.assert_not_awaited()
    await client.federate_restore_not_complete()
    stub.FederateRestoreNotComplete.assert_awaited_once()


@pytest.mark.asyncio
@pytest.mark.parametrize("empty", [True, False])
async def test_valid_stage_allows_success_including_empty_legacy(empty):
    target = transport()
    _, client, stub = client_for(target)
    payload = b"" if empty else target.sdk_checkpoint(8)
    await target._apply_restore_terminal(8, InitiateFederateRestore("s", 8, "saved", payload))
    await client.federate_restore_complete()
    stub.FederateRestoreComplete.assert_awaited_once()
    if empty:
        assert target._staged_sdk_restores[8][1] is None
    assert target._sdk_restore_ready[8] is True


@pytest.mark.asyncio
async def test_rejected_terminal_blocks_success_but_keeps_failure_report_available():
    target = transport()
    _, client, stub = client_for(target)
    await target._apply_restore_terminal(8, InitiateFederateRestore("s", 8, "saved"))
    target.refresh_fom = AsyncMock(side_effect=RuntimeError("refresh rejected"))
    with pytest.raises(RuntimeError, match="refresh rejected"):
        await target._apply_restore_terminal(8, FederationRestored("s", 8, 1))
    with pytest.raises(RuntimeError):
        await client.federate_restore_complete()
    stub.FederateRestoreComplete.assert_not_awaited()
    await client.federate_restore_not_complete()
    stub.FederateRestoreNotComplete.assert_awaited_once()


def test_standard_restore_complete_shares_stage_guard_and_failure_path():
    target = transport()
    fed, _, stub = client_for(target)
    ambassador = Rti1516eAmbassador()
    ambassador._federate = fed
    ambassador._run = asyncio.run
    try:
        with pytest.raises(RuntimeError, match="validated SDK restore stage"):
            ambassador.federateRestoreComplete()
        stub.FederateRestoreComplete.assert_not_awaited()
        target._restore_failure = RuntimeError("rejected")
        ambassador.federateRestoreNotComplete()
        stub.FederateRestoreNotComplete.assert_awaited_once()
    finally:
        ambassador._callback_dispatcher.close()


@pytest.mark.asyncio
async def test_unbound_savepoint_client_cannot_bypass_readiness():
    client = object.__new__(SavepointClient)
    client._federation_name, client._federate_handle = "f", 8
    client._stub = SimpleNamespace(FederateRestoreComplete=AsyncMock())
    with pytest.raises(RuntimeError, match="validated SDK restore stage"):
        await client.federate_restore_complete()
    client._stub.FederateRestoreComplete.assert_not_awaited()
