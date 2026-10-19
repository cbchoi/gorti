"""Real TCP recovery of controls/results across fresh clients and server restart."""

import asyncio
import hashlib
import json
import os
import subprocess
import sys
from pathlib import Path

import grpc
import pytest

from rti1516e._generic_callbacks import callback_state
from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.events import (
    FederationRestored,
    FederationSaved,
    InitiateFederateRestore,
    InitiateFederateSave,
    ObjectInstanceNameReservationSucceeded,
    TimeAdvanceGrant,
)
from tests.test_live_service_e2e import live_artifacts  # noqa: F401
from tests.test_runtime_checkpoint_live import acknowledge
from tests.test_runtime_checkpoint_restart_live import owned_server

pytestmark = pytest.mark.skipif(
    os.environ.get("GORTI_RUN_LIVE_E2E") != "1", reason="production localhost tests are opt-in"
)
NAMES = ("pending-control", "outcome-success", "outcome-failure", "outcome-ackloss")


async def next_event(fed):
    event = await fed._transport.events_for(fed.handle).get()
    if isinstance(event, BaseException):
        raise event
    return event


async def lifecycle(fed, kind, *, retain_control=False, before_terminal=False):
    retained = []
    try:
        async with asyncio.timeout(15):
            while True:
                event = await next_event(fed)
                if before_terminal and getattr(event, "_callback_recovered", False):
                    raise AssertionError("recovery carrier arrived before terminal")
                if isinstance(event, kind):
                    return event
                if retain_control and isinstance(event, ObjectInstanceNameReservationSucceeded):
                    retained.append(event)
                else:
                    await acknowledge(fed, event)
    finally:
        for event in retained:
            fed._transport.events_for(fed.handle).put_nowait(event)


async def phase(action, url, base, family, evidence):
    exact_time = 9007199254740993 if family == "HLAinteger64Time" else 17.25
    async with asyncio.timeout(65), RtiConnection.connect(url) as connection:
        spec = FederationSpec(
            "python-control-outcome-restart", [base], logical_time_implementation_name=family
        )
        async with connection.join_federation(spec, federate_name="saved-client") as fed:
            if action == "save":
                await fed.time_advance_request(exact_time)
                await acknowledge(fed, await lifecycle(fed, TimeAdvanceGrant))
                controls = {}
                for name in NAMES:
                    await fed.reservation.reserve(name)
                    event = await lifecycle(fed, ObjectInstanceNameReservationSucceeded)
                    assert event.object_name == name
                    assert len(event._callback_receipt) == 32
                    controls[name] = event
                receipts = {name: event._callback_receipt for name, event in controls.items()}
                blocked = {receipts[name] for name in NAMES if name != "pending-control"}
                original_rpc = fed._service_rpc
                committed = set()
                source_bodies = []

                async def failed_report(*args, **kwargs):
                    receipt = kwargs.get("callback_receipt")
                    if (
                        args[:2] == ("support", "ReportCallbackInvocation")
                        and not kwargs.get("invocation_entry", False)
                        and receipt in blocked
                    ):
                        if receipt == receipts["outcome-ackloss"] and receipt not in committed:
                            await original_rpc(*args, **kwargs)
                            committed.add(receipt)
                        raise RuntimeError("injected outcome report failure")
                    return await original_rpc(*args, **kwargs)

                fed._service_rpc = failed_report

                def body(event):
                    source_bodies.append(event.object_name)
                    if event.object_name == "outcome-failure":
                        raise ValueError("source failure")

                # Ackloss comes first: later generic flushes must not discard its result.
                for name in ("outcome-ackloss", "outcome-success", "outcome-failure"):
                    error = ValueError if name == "outcome-failure" else RuntimeError
                    with pytest.raises(error):
                        await fed.dispatch_event(controls[name], body)
                assert len(source_bodies) == 3 and len(committed) == 1
                fed._transport.events_for(fed.handle).put_nowait(controls["pending-control"])
                await fed.savepoint.request_federation_save("control-outcome-state")
                initiated = await lifecycle(fed, InitiateFederateSave, retain_control=True)
                await acknowledge(fed, initiated)
                await fed.savepoint.federate_save_begun()
                payload = json.loads(fed._transport.sdk_checkpoint(fed.handle))
                assert payload["state"]["callback_recovery"] == "server-v2"
                assert len(payload["state"]["pending_outcomes"]) == 3
                assert len(payload["state"]["pending_callbacks"]) == 1
                await fed.savepoint.federate_save_complete()
                await acknowledge(fed, await lifecycle(fed, FederationSaved, retain_control=True))
                evidence.write_text(
                    json.dumps(
                        {
                            "source_generation": payload["generation"],
                            "source_self": fed.handle,
                            "identities": {
                                name: hashlib.sha256(receipt).hexdigest()
                                for name, receipt in receipts.items()
                            },
                            "receipts": {name: receipt.hex() for name, receipt in receipts.items()},
                            "pending_outcomes": payload["state"]["pending_outcomes"],
                            "source_bodies": source_bodies,
                            "exact_time": exact_time,
                        }
                    ),
                    encoding="utf-8",
                )
                print(
                    "source saved pending control + success/failure + committed ackloss", flush=True
                )
            else:
                saved = json.loads(evidence.read_text(encoding="utf-8"))
                assert (
                    connection.transport._generation_by_federation[spec.name]
                    != saved["source_generation"]
                )
                await fed.savepoint.request_federation_restore("control-outcome-state")
                initiated = await lifecycle(fed, InitiateFederateRestore, before_terminal=True)
                state = json.loads(initiated.client_state)
                assert state["state"]["pending_outcomes"] == saved["pending_outcomes"]
                await acknowledge(fed, initiated)
                await fed.savepoint.federate_restore_complete()
                terminal = await lifecycle(fed, FederationRestored, before_terminal=True)
                assert fed.handle == state["self"] == terminal.federate_handle
                await acknowledge(fed, terminal)
                assert await fed.query_logical_time() == exact_time
                assert fed._transport._logical_times_by_federate[fed.handle] == exact_time
                original_rpc = fed._service_rpc
                reports = []
                bodies = []
                seen = set()
                completed = []

                async def record_report(*args, **kwargs):
                    if args[:2] == ("support", "ReportCallbackInvocation"):
                        reports.append(dict(kwargs))
                    return await original_rpc(*args, **kwargs)

                fed._service_rpc = record_report
                by_identity = {identity: name for name, identity in saved["identities"].items()}
                async with asyncio.timeout(15):
                    while len(seen) < 4:
                        event = await next_event(fed)
                        assert getattr(event, "_callback_recovered", False), type(event).__name__
                        identity = event._callback_invocation_identity.hex()
                        name = by_identity.get(identity)
                        # Save lifecycle completed tombstones may also be reconciled.
                        if name is None:
                            assert event._callback_completed
                            await fed.dispatch_event(
                                event, lambda event: pytest.fail("marker body")
                            )
                            continue
                        assert name not in seen, "duplicate server recovery carrier"
                        seen.add(name)
                        if name == "outcome-ackloss":
                            assert event._callback_completed and not getattr(
                                event, "_callback_receipt", b""
                            )
                            completed.append(identity)
                        else:
                            assert not event._callback_completed
                            assert event._callback_receipt.hex() not in saved["receipts"].values()
                            assert event._callback_outcome_only == (name != "pending-control")
                        await fed.dispatch_event(
                            event, lambda value: bodies.append(value.object_name)
                        )
                assert bodies == ["pending-control"]
                assert completed == [saved["identities"]["outcome-ackloss"]]
                assert callback_state(fed).checkpoint_outcomes() == []
                assert len(reports) == 4, "pending control Entry/Outcome plus two recovery Outcomes"
                assert sum(row["invocation_entry"] for row in reports) == 1
                failures = [row for row in reports if not row["success"]]
                assert (
                    len(failures) == 1 and failures[0]["exception"] == "ValueError: source failure"
                )
                try:
                    async with asyncio.timeout(0.3):
                        while True:
                            event = await next_event(fed)
                            assert event._callback_invocation_identity.hex() not in by_identity, (
                                "duplicate recovery"
                            )
                            assert event._callback_completed
                            await fed.dispatch_event(
                                event, lambda event: pytest.fail("marker body")
                            )
                except TimeoutError:
                    pass
                # Exercise server ticket fencing without adding a fabricated SDK result.
                with pytest.raises(Exception) as rejected:
                    await original_rpc(
                        "support",
                        "ReportCallbackInvocation",
                        "ReportCallbackInvocationRequest",
                        authenticated=True,
                        callback_receipt=bytes.fromhex(saved["receipts"]["pending-control"]),
                        success=True,
                        exception="",
                        invocation_entry=False,
                    )
                cause = rejected.value.__cause__ or rejected.value
                assert isinstance(cause, grpc.RpcError)
                assert cause.code() in (
                    grpc.StatusCode.FAILED_PRECONDITION,
                    grpc.StatusCode.PERMISSION_DENIED,
                    grpc.StatusCode.NOT_FOUND,
                    grpc.StatusCode.INVALID_ARGUMENT,
                )
                assert (
                    json.loads(fed._transport.sdk_checkpoint(fed.handle))["state"][
                        "pending_outcomes"
                    ]
                    == []
                )
                print(
                    json.dumps(
                        {
                            "family": family,
                            "fresh_generation": state["generation"],
                            "pending_control_bodies": len(bodies),
                            "outcome_only_reports": 2,
                            "completed_markers_without_rpc": 1,
                            "old_ticket_rejected": True,
                            "exact_time": exact_time,
                        }
                    ),
                    flush=True,
                )


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
def test_live_controls_and_known_outcomes_after_actual_restart(live_artifacts, family):  # noqa: F811
    directory, binary, base, _ = live_artifacts
    assert "GORTI_LIVE_RTID" in os.environ, "parent must supply the integrated assessed binary"
    run = directory / f"control-outcome-restart-{family}"
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
