import asyncio
import json
from unittest.mock import AsyncMock

import pytest

from rti1516e._callbacks import HLA_EVOKED, CallbackDispatcher, ServiceCallback
from rti1516e._services import FederationExecutionInformation
from rti1516e.connection import Federate
from rti1516e.events import FederationRestored, InitiateFederateRestore
from rti1516e.savepoint import (
    FederateRestoreStatus,
    FederateSaveStatus,
    RestoreState,
    RestoreStatus,
    RestoreStatusResponse,
    SaveState,
    SaveStatus,
    SaveStatusResponse,
)
from rti1516e.standard import Rti1516eAmbassador
from tests.test_runtime_checkpoint import transport

EVENTS = [
    ServiceCallback(
        "reportFederationExecutions",
        (
            (
                FederationExecutionInformation(
                    "historical-other", 2, 3, 9007199254740993, "HLAinteger64Time"
                ),
            ),
        ),
    ),
    ServiceCallback("confirmAttributeTransportationTypeChange", (17, [2, 3], 2)),
    ServiceCallback("reportAttributeTransportationType", (17, 2, 1)),
    ServiceCallback("confirmInteractionTransportationTypeChange", (19, 2)),
    ServiceCallback("reportInteractionTransportationType", ((1 << 32) | 8, 19, 1)),
    SaveStatusResponse(
        SaveState.IDLE,
        "historical-save",
        (FederateSaveStatus((1 << 32) | 8, SaveStatus.NO_SAVE_IN_PROGRESS),),
    ),
    RestoreStatusResponse(
        RestoreState.COMPLETED,
        "historical-restore",
        (
            FederateRestoreStatus(
                (1 << 32) | 8, 9007199254740993, RestoreStatus.NO_RESTORE_IN_PROGRESS
            ),
        ),
    ),
]


def ambassador(source):
    value = Rti1516eAmbassador()
    value._callback_dispatcher.close()
    value._callback_dispatcher = CallbackDispatcher(value._dispatch_callback, HLA_EVOKED)
    value._federate = Federate(connection=object(), transport=source, handle=8, name="saved")
    value._run = asyncio.run
    source._checkpoint_sources[8] = value._checkpoint_pending_callbacks
    source.set_event_sink(8, value._dispatch_event)
    return value


@pytest.mark.parametrize(
    "event",
    EVENTS,
    ids=[
        "execution-list",
        "attribute-confirm",
        "attribute-report",
        "interaction-confirm",
        "interaction-report",
        "save-status",
        "restore-status",
    ],
)
def test_all_seven_local_notifications_export_without_invocation(event):
    source = transport()
    amb = ambassador(source)
    calls = []
    amb._invoke_event = lambda value: calls.append(value) or True
    amb._dispatch_event(event)
    state = json.loads(source.sdk_checkpoint(8))
    assert len(state["state"]["pending_local_notifications"]) == 1
    assert state["state"]["pending_callbacks"] == []
    assert state["state"]["pending_outcomes"] == []
    assert calls == []
    assert amb._callback_dispatcher.snapshot_pending() == [event]
    assert source.sdk_checkpoint(8) == source.sdk_checkpoint(8)
    amb._callback_dispatcher.close()


@pytest.mark.asyncio
async def test_restored_locals_follow_terminal_once_with_order_and_multiplicity():
    source = transport()
    source_amb = ambassador(source)
    for event in (EVENTS[5], EVENTS[2], EVENTS[5], EVENTS[6]):
        source_amb._dispatch_event(event)
    payload = source.sdk_checkpoint(8)
    target = transport()
    target_amb = ambassador(target)
    trace = []
    target_amb._invoke_event = lambda event: trace.append(event) or True
    target_amb._federate.report_callback_invocation = AsyncMock(
        side_effect=AssertionError("report")
    )
    target_amb._federate._activate_transportation_confirmation = AsyncMock(
        side_effect=AssertionError("activation")
    )
    target_amb._dispatch_event(ServiceCallback("reportAttributeTransportationType", (99, 2, 2)))
    await target._apply_restore_terminal(8, InitiateFederateRestore("s", 8, "saved", payload))
    assert len(target_amb._callback_dispatcher.snapshot_pending()) == 1
    terminal = FederationRestored("s", 8, 1)
    await target._apply_restore_terminal(8, terminal)
    assert trace == []
    target._deliver_event(8, terminal)
    for _ in range(5):
        assert await asyncio.to_thread(target_amb._callback_dispatcher.evoke, 0, 0, multiple=False)
    assert trace == [terminal, EVENTS[5], EVENTS[2], EVENTS[5], EVENTS[6]]
    assert not target_amb._callback_dispatcher.evoke(0, 0, multiple=False)
    target_amb._federate.report_callback_invocation.assert_not_awaited()
    target_amb._federate._activate_transportation_confirmation.assert_not_awaited()
    assert (
        json.loads(target.sdk_checkpoint(8))["state"].get("pending_local_notifications", []) == []
    )
    source_amb._callback_dispatcher.close()
    target_amb._callback_dispatcher.close()


def row_for(event=EVENTS[2]):
    from rti1516e._local_notifications import prepare, to_row

    return to_row(prepare(event, 1))


@pytest.mark.parametrize("event", EVENTS)
@pytest.mark.parametrize(
    "mutation",
    [
        lambda row: row.update(extra="unknown"),
        lambda row: row["payload"].update(callback_receipt="old-authority"),
        lambda row: row["payload"].update(ticket="unactivated"),
        lambda row: row.update(scope="connection" if row["scope"] == "member" else "member"),
        lambda row: row.update(local_id="A" * 64),
        lambda row: row.update(order=True),
        lambda row: row.update(order=1.0),
        lambda row: row.update(order="1"),
        lambda row: row.update(payload=None),
    ],
)
def test_every_local_kind_rejects_unknown_authority_and_malformed_fields(event, mutation):
    from rti1516e._local_notifications import from_rows

    row = row_for(event)
    mutation(row)
    with pytest.raises(ValueError):
        from_rows([row], 1)


@pytest.mark.parametrize(
    "event,payload",
    [
        (
            EVENTS[0],
            {"executions": [{"name": "", "logical_time_implementation_name": "HLAfloat64Time"}]},
        ),
        (EVENTS[0], {"executions": [{"name": "valid", "logical_time_implementation_name": ""}]}),
        (
            EVENTS[0],
            {
                "executions": [
                    {"name": "same", "logical_time_implementation_name": "HLAfloat64Time"}
                ]
                * 2
            },
        ),
        (EVENTS[0], {"executions": None}),
        (EVENTS[1], {"object_handle": 17, "attribute_handles": [2, 2], "transport_type": 2}),
        (EVENTS[1], {"object_handle": 17, "attribute_handles": None, "transport_type": 2}),
        (EVENTS[2], {"object_handle": 17, "attribute_handle": 0, "transport_type": 2}),
        (EVENTS[3], {"interaction_class_handle": 19, "transport_type": 0}),
        (
            EVENTS[4],
            {"federate_handle": (2 << 32) | 8, "interaction_class_handle": 19, "transport_type": 1},
        ),
        (EVENTS[5], {"statuses": [{"federate_handle": (1 << 32) | 8, "status": 1}] * 2}),
        (EVENTS[5], {"statuses": [], "state": None}),
        (EVENTS[5], {"statuses": [{"federate_handle": (1 << 32) | 8, "status": 5}]}),
        (
            EVENTS[6],
            {
                "statuses": [
                    {
                        "pre_restore_handle": (1 << 32) | 8,
                        "post_restore_handle": 2 << 32,
                        "status": 1,
                    }
                ]
            },
        ),
        (
            EVENTS[6],
            {
                "statuses": [
                    {"pre_restore_handle": (1 << 32) | 8, "post_restore_handle": 0, "status": 7}
                ]
            },
        ),
        (EVENTS[6], {"statuses": [], "active_label": None}),
    ],
)
def test_exact_local_payload_negatives(event, payload):
    from rti1516e._local_notifications import from_rows

    row = row_for(event)
    row["payload"] = payload
    with pytest.raises(ValueError):
        from_rows([row], 1)


@pytest.mark.parametrize("rows", [None, {}, [None]])
def test_local_collection_rejects_nonarrays(rows):
    from rti1516e._local_notifications import from_rows

    with pytest.raises(ValueError):
        from_rows(rows)


def test_local_ids_and_order_are_unique_while_same_payload_is_allowed():
    from rti1516e._local_notifications import from_rows

    row = row_for()
    with pytest.raises(ValueError, match="identity"):
        from_rows([row, {**row, "order": 2}])
    with pytest.raises(ValueError, match="order"):
        from_rows([row, {**row, "local_id": "b" * 64}])
    duplicate = {**row, "local_id": "b" * 64, "order": 3}
    assert len(from_rows([row, duplicate])) == 2


def test_snapshot_copies_mutable_arguments_and_never_calls_handlers():
    from rti1516e._runtime_checkpoint import decode

    source = transport()
    amb = ambassador(source)
    attributes = [2, 3]
    event = ServiceCallback("confirmAttributeTransportationTypeChange", (17, attributes, 2))
    amb.confirmAttributeTransportationTypeChange = lambda *args: pytest.fail(
        "handler during snapshot"
    )
    amb._dispatch_event(event)
    payload = source.sdk_checkpoint(8)
    attributes.append(4)
    runtime = decode(payload, "HLAinteger64Time")
    assert runtime["pending_local_notifications"][0]["payload"]["attribute_handles"] == [2, 3]
    assert runtime["decoded_local_notifications"][0].args == (17, [2, 3], 2)
    amb._callback_dispatcher.close()


@pytest.mark.asyncio
async def test_invalid_local_staging_is_atomic_and_legacy_omission_is_supported():
    from rti1516e._runtime_checkpoint import _checksum

    target = transport()
    payload = json.loads(target.sdk_checkpoint(8))
    assert "pending_local_notifications" not in payload["state"]
    bad = row_for(EVENTS[0])
    bad["payload"]["executions"][0]["name"] = ""
    payload["state"]["pending_local_notifications"] = [bad]
    raw = json.dumps(payload["state"], sort_keys=True, separators=(",", ":")).encode()
    payload["checksum"] = _checksum(payload, raw)
    target.events_for(8).put_nowait("preserved")
    with pytest.raises(ValueError):
        await target._apply_restore_terminal(
            8, InitiateFederateRestore("s", 8, "saved", json.dumps(payload).encode())
        )
    assert target._staged_sdk_restores == {}
    assert target.events_for(8).get_nowait() == "preserved"
    target.refresh_fom.assert_not_awaited()


@pytest.mark.asyncio
async def test_layer1_local_dispatch_has_no_rpc_and_running_body_blocks_checkpoint():
    from rti1516e._local_notifications import from_rows

    source = transport()
    fed = Federate(connection=object(), transport=source, handle=8, name="saved")
    fed._service_rpc = AsyncMock(side_effect=AssertionError("RPC"))
    event = from_rows([row_for(EVENTS[2])])[0]
    invoked = []

    async def body(value):
        invoked.append(value)
        with pytest.raises(RuntimeError, match="executing local"):
            source.sdk_checkpoint(8)

    await fed.dispatch_event(event, body)
    await fed.dispatch_event(event, body)
    assert invoked == [event]
    fed._service_rpc.assert_not_awaited()
    assert source.sdk_checkpoint(8)


def test_unactivated_or_arbitrary_closures_fail_export_without_execution():
    source = transport()
    amb = ambassador(source)
    event = ServiceCallback(
        "confirmAttributeTransportationTypeChange", (17, lambda: pytest.fail("closure"), 2)
    )
    amb._dispatch_event(event)
    with pytest.raises(ValueError):
        source.sdk_checkpoint(8)
    queued = amb._callback_dispatcher.snapshot_pending()[0]
    object.__setattr__(queued, "ticket", b"old-ticket")
    with pytest.raises(ValueError, match="authority"):
        source.sdk_checkpoint(8)
    amb._callback_dispatcher.close()


def test_local_module_definitions_and_imports_are_unique_and_compile():
    import ast
    from collections import Counter
    from pathlib import Path

    from rti1516e import _local_notifications

    path = Path(_local_notifications.__file__)
    text = path.read_text(encoding="utf-8")
    tree = ast.parse(text)
    compile(tree, str(path), "exec")
    names = [node.name for node in tree.body if isinstance(node, (ast.ClassDef, ast.FunctionDef))]
    assert all(count == 1 for count in Counter(names).values()), Counter(names)
    imports = [
        ast.dump(node) for node in tree.body if isinstance(node, (ast.Import, ast.ImportFrom))
    ]
    assert len(imports) == len(set(imports)), "duplicate imports"
    for node in tree.body:
        if isinstance(node, ast.ClassDef):
            methods = [member.name for member in node.body if isinstance(member, ast.FunctionDef)]
            assert len(methods) == len(set(methods)), node.name
