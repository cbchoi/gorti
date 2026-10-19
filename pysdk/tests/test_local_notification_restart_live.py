"""Mixed local RPC-result notifications and server controls across actual restart."""

import asyncio
import json
import os
import subprocess
import sys
import threading
from pathlib import Path

import pytest

from rti1516e._callbacks import HLA_EVOKED, CallbackDispatcher
from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.events import (
    FederationRestored,
    FederationSaved,
    InitiateFederateRestore,
    InitiateFederateSave,
    ObjectInstanceNameReservationSucceeded,
)
from rti1516e.savepoint import RestoreStatusResponse, SaveStatusResponse
from rti1516e.standard import Rti1516eAmbassador
from tests.test_callback_recovery_restart_live import lifecycle, next_event
from tests.test_live_service_e2e import VEHICLE, live_artifacts  # noqa: F401
from tests.test_runtime_checkpoint_live import acknowledge
from tests.test_runtime_checkpoint_restart_live import owned_server

pytestmark = pytest.mark.skipif(
    os.environ.get("GORTI_RUN_LIVE_E2E") != "1", reason="production localhost tests are opt-in"
)


def local_ambassador(fed):
    loop = asyncio.get_running_loop()
    value = Rti1516eAmbassador()
    value._callback_dispatcher.close()
    value._callback_dispatcher = CallbackDispatcher(value._dispatch_callback, HLA_EVOKED)
    value._federate = fed
    value._loop_thread = threading.current_thread()
    value._run = lambda coroutine: asyncio.run_coroutine_threadsafe(coroutine, loop).result(15)
    fed._transport._checkpoint_sources[fed.handle] = value._checkpoint_pending_callbacks
    return value


async def phase(action, url, base, family, evidence):
    async with asyncio.timeout(65), RtiConnection.connect(url) as connection:
        spec = FederationSpec(
            "python-local-notification-restart", [base], logical_time_implementation_name=family
        )
        async with connection.join_federation(spec, federate_name="saved-client") as fed:
            amb = local_ambassador(fed)
            observed = []
            amb.federationSaveStatusResponse = lambda rows: observed.append(("save", rows))
            amb.federationRestoreStatusResponse = lambda rows: observed.append(("restore", rows))
            amb.confirmAttributeTransportationTypeChange = lambda *args: observed.append(
                ("confirmed", args)
            )
            try:
                if action == "save":
                    await fed.publish_object_class(VEHICLE, attributes=["Value"])
                    obj = await fed.register_object_instance(VEHICLE)
                    attribute = fed._transport._resolve_attribute_handles(VEHICLE, ["Value"])[0]
                    await asyncio.to_thread(
                        amb.requestAttributeTransportationTypeChange, int(obj), [attribute], 2
                    )
                    # Direct RPC has already changed the actual server before
                    # local callback invocation.
                    assert await fed.query_attribute_transportation_type(int(obj), attribute) == 2
                    await asyncio.to_thread(amb.queryFederationSaveStatus)
                    await asyncio.to_thread(amb.queryFederationRestoreStatus)
                    await asyncio.to_thread(amb.queryFederationSaveStatus)
                    assert observed == []
                    await fed.reservation.reserve("pending-server-control")
                    pending = await lifecycle(fed, ObjectInstanceNameReservationSucceeded)
                    fed._transport.events_for(fed.handle).put_nowait(pending)
                    await fed.savepoint.request_federation_save("mixed-local-state")
                    await acknowledge(
                        fed, await lifecycle(fed, InitiateFederateSave, retain_control=True)
                    )
                    await fed.savepoint.federate_save_begun()
                    snapshot = json.loads(fed._transport.sdk_checkpoint(fed.handle))
                    assert len(snapshot["state"]["pending_local_notifications"]) == 4
                    assert len(snapshot["state"]["pending_callbacks"]) == 1
                    await fed.savepoint.federate_save_complete()
                    await acknowledge(
                        fed, await lifecycle(fed, FederationSaved, retain_control=True)
                    )
                    evidence.write_text(
                        json.dumps(
                            {
                                "source_generation": snapshot["generation"],
                                "source_self": fed.handle,
                                "old_receipt": pending._callback_receipt.hex(),
                                "object": int(obj),
                                "attribute": attribute,
                                "local_notifications": snapshot["state"][
                                    "pending_local_notifications"
                                ],
                            }
                        ),
                        encoding="utf-8",
                    )
                    print("saved 4 uninvoked locals and 1 pending server control", flush=True)
                else:
                    saved = json.loads(evidence.read_text(encoding="utf-8"))
                    assert (
                        connection.transport._generation_by_federation[spec.name]
                        != saved["source_generation"]
                    )
                    await fed.savepoint.request_federation_restore("mixed-local-state")
                    initiated = await lifecycle(fed, InitiateFederateRestore, before_terminal=True)
                    restored = json.loads(initiated.client_state)
                    assert len(restored["state"]["pending_local_notifications"]) == 4
                    await acknowledge(fed, initiated)
                    await fed.savepoint.federate_restore_complete()
                    terminal = await lifecycle(fed, FederationRestored, before_terminal=True)
                    await acknowledge(fed, terminal)
                    assert observed == []
                    assert (
                        await fed.query_attribute_transportation_type(
                            saved["object"], saved["attribute"]
                        )
                        == 2
                    )
                    server_bodies = []
                    async with asyncio.timeout(15):
                        while len(observed) < 4 or not server_bodies:
                            event = await next_event(fed)
                            if isinstance(event, ObjectInstanceNameReservationSucceeded):
                                assert event._callback_receipt.hex() != saved["old_receipt"]
                                await fed.dispatch_event(
                                    event, lambda value: server_bodies.append(value.object_name)
                                )
                            elif getattr(event, "_callback_completed", False):
                                await fed.dispatch_event(
                                    event, lambda value: pytest.fail("marker body")
                                )
                            else:
                                assert (
                                    isinstance(event, (SaveStatusResponse, RestoreStatusResponse))
                                    or getattr(event, "method", "")
                                    == "confirmAttributeTransportationTypeChange"
                                )
                                assert not getattr(event, "_callback_receipt", b"")
                                amb._dispatch_event(event)
                                assert amb._callback_dispatcher.evoke(0, 0, multiple=False)
                    assert [kind for kind, _ in observed] == [
                        "confirmed",
                        "save",
                        "restore",
                        "save",
                    ]
                    assert observed[1] == observed[3], (
                        "identical queued local notifications retain multiplicity"
                    )
                    assert server_bodies == ["pending-server-control"]
                    assert (
                        json.loads(fed._transport.sdk_checkpoint(fed.handle))["state"].get(
                            "pending_local_notifications", []
                        )
                        == []
                    )
                    try:
                        async with asyncio.timeout(0.3):
                            while True:
                                event = await next_event(fed)
                                assert getattr(event, "_callback_completed", False), (
                                    "duplicate local/server recovery"
                                )
                                await fed.dispatch_event(
                                    event, lambda value: pytest.fail("marker body")
                                )
                    except TimeoutError:
                        pass
                    print(
                        json.dumps(
                            {
                                "family": family,
                                "local_notifications": 4,
                                "local_fifo": [kind for kind, _ in observed],
                                "fresh_server_control_bodies": 1,
                                "direct_transport_change_preserved": True,
                            }
                        ),
                        flush=True,
                    )
            finally:
                amb._callback_dispatcher.close()


def run_phase(action, url, base, family, evidence, run):
    result = subprocess.run(  # noqa: S603 - current interpreter and local test script, no shell
        [
            sys.executable,
            "-B",
            str(Path(__file__).resolve()),
            action,
            url,
            str(base),
            family,
            str(evidence),
        ],
        capture_output=True,
        text=True,
        timeout=80,
        creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
    )
    (run / f"{action}-client.log").write_text(result.stdout + result.stderr, encoding="utf-8")
    assert result.returncode == 0, result.stdout + result.stderr


@pytest.mark.parametrize("family", ["HLAfloat64Time", "HLAinteger64Time"])
def test_live_mixed_local_notifications_after_actual_restart(live_artifacts, family):  # noqa: F811
    directory, binary, base, _ = live_artifacts
    assert "GORTI_LIVE_RTID" in os.environ, (
        "parent must supply assessed local-schema integration binary"
    )
    run = directory / f"local-notification-restart-{family}"
    run.mkdir()
    evidence = run / "source-checkpoint.json"
    savedir = run / "saves"
    with owned_server(binary, run, savedir, "source") as (source, url):
        source_pid = source.pid
        run_phase("save", url, base, family, evidence, run)
    assert source.poll() is not None
    with owned_server(binary, run, savedir, "target") as (target, url):
        target_pid = target.pid
        assert source_pid != target_pid
        run_phase("restore", url, base, family, evidence, run)
    assert target.poll() is not None
    (run / "restart-result.json").write_text(
        json.dumps(
            {
                "family": family,
                "binary": str(binary),
                "source_pid": source_pid,
                "target_pid": target_pid,
                "save_dir": str(savedir),
                "both_owned_servers_stopped": True,
                "source_stopped_before_target_started": True,
                "fresh_client_processes": True,
            }
        ),
        encoding="utf-8",
    )


if __name__ == "__main__":
    asyncio.run(phase(*sys.argv[1:5], Path(sys.argv[5])))
