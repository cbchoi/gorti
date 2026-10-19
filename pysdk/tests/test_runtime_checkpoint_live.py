"""Fresh Python processes and a recreated execution against an owned real server."""

import asyncio
import json
import os
import subprocess
import sys
from contextlib import AsyncExitStack
from pathlib import Path

import pytest

from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.events import (
    DiscoverObjectInstance,
    FederationRestored,
    FederationSaved,
    InitiateFederateRestore,
    InitiateFederateSave,
    ReflectAttributeValues,
    TimeAdvanceGrant,
)
from tests.test_live_service_e2e import VEHICLE, live_artifacts, live_server  # noqa: F401

pytestmark = pytest.mark.skipif(
    os.environ.get("GORTI_RUN_LIVE_E2E") != "1", reason="production localhost tests are opt-in"
)


async def acknowledge(fed, event):
    receipt = getattr(event, "_callback_receipt", b"")
    if receipt:
        await fed.report_callback_invocation(receipt, True, "", invocation_entry=True)
        await fed.report_callback_invocation(receipt, True, "")


async def wait(fed, kind, *, retain_data=False, restored_order=False):
    queue = fed._transport.events_for(fed.handle)
    retained = []
    try:
        async with asyncio.timeout(15):
            while True:
                event = await queue.get()
                if isinstance(event, BaseException):
                    raise event
                if isinstance(event, kind):
                    return event
                if restored_order and isinstance(
                    event, (DiscoverObjectInstance, ReflectAttributeValues)
                ):
                    raise AssertionError(
                        f"restored {type(event).__name__} arrived before {kind.__name__}"
                    )
                if retain_data and isinstance(
                    event, (DiscoverObjectInstance, ReflectAttributeValues)
                ):
                    retained.append(event)
                else:
                    await acknowledge(fed, event)
    finally:
        for event in retained:
            queue.put_nowait(event)


async def phase(action, url, base, family, evidence):
    name = "python-runtime-recreated"
    target_time = 9007199254740993 if family == "HLAinteger64Time" else 17.25
    async with asyncio.timeout(70), AsyncExitStack() as stack:
        left = await stack.enter_async_context(RtiConnection.connect(url))
        right = await stack.enter_async_context(RtiConnection.connect(url))
        spec = FederationSpec(name, [base], logical_time_implementation_name=family)
        owner = await stack.enter_async_context(left.join_federation(spec, federate_name="owner"))
        receiver = await stack.enter_async_context(
            right.join_federation(spec, federate_name="receiver")
        )
        members = [owner, receiver]
        if action in ("save", "save-restart"):
            await receiver.enable_ownership_callback_receipts()
            for fed in members:
                await fed.time_advance_request(target_time)
                grant = await wait(fed, TimeAdvanceGrant)
                assert grant.time == target_time
                await acknowledge(fed, grant)
            await owner.publish_object_class(VEHICLE, attributes=["Value"])
            await receiver.subscribe_object_class(VEHICLE, attributes=["Value"])
            obj = await owner.register_object_instance(VEHICLE)
            discovery = await wait(receiver, DiscoverObjectInstance)
            receiver._transport.events_for(receiver.handle).put_nowait(discovery)
            await owner.update_attributes(
                obj, {"Value": b"exact-private-payload"}, tag=b"saved-tag"
            )
            reflection = await wait(receiver, ReflectAttributeValues, retain_data=True)
            receiver._transport.events_for(receiver.handle).put_nowait(reflection)
            assert getattr(reflection, "_callback_receipt", b"")
            await owner.savepoint.request_federation_save("sdk-state")
            for fed in members:
                initiated = await wait(fed, InitiateFederateSave, retain_data=True)
                await acknowledge(fed, initiated)
                await fed.savepoint.federate_save_begun()
                await fed.savepoint.federate_save_complete()
            for fed in members:
                await acknowledge(fed, await wait(fed, FederationSaved, retain_data=True))
            snapshot = json.loads(receiver._transport.sdk_checkpoint(receiver.handle))
            assert snapshot["state"]["object_knowledge"]["latest"]
            assert len(snapshot["state"]["pending_callbacks"]) >= 2
            evidence.write_text(
                json.dumps(
                    {
                        "source_generation": snapshot["generation"],
                        "source_self": receiver.handle,
                        "source_handles": {fed.name: fed.handle for fed in members},
                        "object": int(obj),
                        "time": target_time,
                        "source_receipts": [
                            row["callback_receipt"]
                            for row in snapshot["state"]["pending_callbacks"]
                        ],
                    }
                ),
                encoding="utf-8",
            )
        else:
            import base64

            saved = json.loads(evidence.read_text(encoding="utf-8"))
            assert right.transport._generation_by_federation[name] != saved["source_generation"]
            await receiver.enable_ownership_callback_receipts()
            await owner.savepoint.request_federation_restore("sdk-state")
            target_envelopes = {}
            for fed in members:
                initiated = await wait(fed, InitiateFederateRestore)
                assert initiated.client_state
                assert initiated.federate_name == fed.name
                assert initiated.federate_handle == saved["source_handles"][fed.name]
                envelope = json.loads(initiated.client_state)
                assert envelope["generation"] == fed._transport._generation_by_federation[name]
                assert envelope["generation"] != saved["source_generation"]
                target_envelopes[fed.name] = envelope["self"]
                await acknowledge(fed, initiated)
                await fed.savepoint.federate_restore_complete()
            terminal = await wait(receiver, FederationRestored, restored_order=True)
            assert terminal.federate_handle == target_envelopes[receiver.name]
            assert receiver.handle == target_envelopes[receiver.name]
            await acknowledge(receiver, terminal)
            assert receiver.callback_invocation_entry_supported
            assert await receiver.query_logical_time() == target_time
            assert receiver._transport._logical_times_by_federate[receiver.handle] == target_time
            assert receiver._ownership_callback_state()._latest
            discovery = await wait(receiver, DiscoverObjectInstance, restored_order=True)
            reflection = await wait(receiver, ReflectAttributeValues, restored_order=True)
            old = {base64.b64decode(value) for value in saved["source_receipts"]}
            for event in (discovery, reflection):
                assert event.object_handle == saved["object"]
                assert getattr(event, "_callback_receipt", b"") not in old
                assert getattr(event, "_callback_receipt", b"")
                await acknowledge(receiver, event)
            assert reflection.tag == b"saved-tag"
            assert b"exact-private-payload" in reflection.attribute_values.values()
            assert await receiver.support.get_known_object_class_handle(saved["object"])
            try:
                async with asyncio.timeout(0.25):
                    while True:
                        event = await receiver._transport.events_for(receiver.handle).get()
                        assert not isinstance(
                            event, (DiscoverObjectInstance, ReflectAttributeValues)
                        ), "duplicate SDK/server replay"
            except TimeoutError:
                pass
            print(
                json.dumps(
                    {
                        "restored_generation": right.transport._generation_by_federation[name],
                        "restored_self": receiver.handle,
                        "exact_time": target_time,
                        "fresh_data_callbacks": 2,
                    }
                ),
                flush=True,
            )
    if action == "save":
        async with RtiConnection.connect(url) as connection:
            await connection.destroy_federation(name)


@pytest.mark.parametrize("family", ["HLAfloat64Time", "HLAinteger64Time"])
def test_live_fresh_process_recreated_python_runtime(live_server, live_artifacts, family):  # noqa: F811
    url, base, _ = live_server
    directory = live_artifacts[0]
    evidence = directory / f"python-runtime-{family}.json"
    for action in ("save", "restore"):
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
            timeout=90,
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
        )
        (directory / f"python-runtime-{family}-{action}.log").write_text(
            result.stdout + result.stderr, encoding="utf-8"
        )
        assert result.returncode == 0, result.stdout + result.stderr


if __name__ == "__main__":
    asyncio.run(phase(*sys.argv[1:5], Path(sys.argv[5])))
