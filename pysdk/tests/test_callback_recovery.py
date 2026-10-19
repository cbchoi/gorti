import asyncio
import base64
import hashlib
import json
from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest

from rti1516e._generic_callbacks import GenericCallbackState, callback_state
from rti1516e._transport import _ensure_generated_path
from rti1516e.events import SynchronizationPointAnnounced
from rti1516e.standard import Rti1516eAmbassador
from tests.test_runtime_checkpoint import transport
from tests.test_service_completion import federate as _federate  # noqa: F401 - named pytest fixture

_ensure_generated_path()
from rti.v1 import stream_pb2  # noqa: E402 - generated imports need path bootstrap above

IDENTITY = hashlib.sha256(b"s" * 32).digest()
OUTCOME = {"identity": IDENTITY.hex(), "success": False, "exception": "ValueError: body"}


def recovery(*, completed=False, identity=IDENTITY):
    event = SynchronizationPointAnnounced("must-not-run", b"", ())
    object.__setattr__(event, "_callback_invocation_identity", identity)
    object.__setattr__(event, "_callback_outcome_only", True)
    object.__setattr__(event, "_callback_completed", completed)
    if not completed:
        object.__setattr__(event, "_callback_receipt", b"f" * 32)
    return event


def test_known_finished_outcome_is_portable_but_unknown_is_not():
    state = GenericCallbackState()
    state.claim(b"s" * 32)
    with pytest.raises(RuntimeError, match="unresolved"):
        state.checkpoint_outcomes()
    state.complete(b"s" * 32, False, "ValueError: body")
    assert state.checkpoint_outcomes() == [OUTCOME]
    restored = GenericCallbackState()
    restored.restore_outcomes([OUTCOME])
    assert restored.pending() == []
    assert restored.checkpoint_outcomes() == [OUTCOME]


def test_standard_outcome_only_reports_without_body_or_entry():
    amb = Rti1516eAmbassador()
    report = AsyncMock()
    amb._federate = SimpleNamespace(
        report_callback_invocation=report, callback_invocation_entry_supported=True
    )
    amb._run = asyncio.run
    amb.announceSynchronizationPoint = lambda *args: pytest.fail("body invoked")
    callback_state(amb).restore_outcomes([OUTCOME])
    assert not amb._dispatch_callback(recovery())
    assert not amb._dispatch_callback(recovery())
    report.assert_awaited_once_with(b"f" * 32, False, "ValueError: body")
    assert callback_state(amb).checkpoint_outcomes() == []


def test_completed_marker_reconciles_without_rpc_and_duplicates_are_harmless():
    amb = Rti1516eAmbassador()
    amb._federate = SimpleNamespace(report_callback_invocation=AsyncMock())
    callback_state(amb).restore_outcomes([OUTCOME])
    assert not amb._dispatch_callback(recovery(completed=True))
    assert not amb._dispatch_callback(recovery(completed=True))
    amb._federate.report_callback_invocation.assert_not_awaited()
    assert callback_state(amb).checkpoint_outcomes() == []


def test_unknown_recovery_and_stale_report_fail_closed():
    amb = Rti1516eAmbassador()
    report = AsyncMock(side_effect=RuntimeError("stale ticket"))
    amb._federate = SimpleNamespace(report_callback_invocation=report)
    amb._run = asyncio.run
    with pytest.raises(RuntimeError, match="matching completed"):
        amb._dispatch_callback(recovery())
    callback_state(amb).restore_outcomes([OUTCOME])
    with pytest.raises(RuntimeError, match="stale ticket"):
        amb._dispatch_callback(recovery())
    assert callback_state(amb).checkpoint_outcomes() == [OUTCOME]


@pytest.mark.asyncio
async def test_layer1_recovery_shares_transport_ledger_and_never_enters(federate):  # noqa: F811
    fed, channel, _ = federate
    fed._callback_entry_supported = True
    callback_state(fed).restore_outcomes([OUTCOME])
    await fed.dispatch_event(recovery(), lambda event: pytest.fail("body invoked"))
    await fed.dispatch_event(recovery(), lambda event: pytest.fail("body invoked"))
    assert len(channel.calls) == 1
    assert not channel.calls[0][1].invocation_entry
    assert channel.calls[0][1].callback_receipt == b"f" * 32
    assert not channel.calls[0][1].success


VARIANTS = [
    field.name
    for field in stream_pb2.FederateEvent.DESCRIPTOR.oneofs_by_name["event"].fields
    if field.name != "local_validation_barrier"
]


def wire_event(kind, *, outcome_only=False):
    wire = stream_pb2.FederateEvent(callback_receipt=b"f" * 32)
    body = getattr(wire, kind)
    body.SetInParent()
    if kind in ("grant", "time_regulation_enabled", "time_constrained_enabled"):
        body.logical_time_value.integer64_value = 9007199254740993
    wire.callback_recovery.invocation_identity = IDENTITY
    wire.callback_recovery.outcome_only = outcome_only
    return wire


def projected_transport_recovery(interaction):
    from rti1516e.events import TransportationChangeConfirmed

    wire = wire_event("transportation_change_confirmed", outcome_only=True)
    body = wire.transportation_change_confirmed
    body.transport_type = 2
    if interaction:
        body.interaction_class_handle = 9
    else:
        body.object_handle = 4
        body.attribute_handles.extend([5, 6])
    assert not body.ticket and body.federation_generation == body.callback_epoch == 0
    event = transport()._decode_carrier(8, wire)
    assert isinstance(event, TransportationChangeConfirmed)
    assert event.ticket == b"" and event.federation_generation == event.callback_epoch == 0
    return event


@pytest.mark.parametrize("success", [True, False])
@pytest.mark.parametrize("interaction", [True, False])
def test_projected_transport_outcome_only_standard_never_activates_or_enters(success, interaction):
    event = projected_transport_recovery(interaction)
    exception = "" if success else "ValueError: transportation body"
    amb = Rti1516eAmbassador()
    report = AsyncMock()
    activate = AsyncMock(side_effect=AssertionError("activation"))
    amb._federate = SimpleNamespace(
        report_callback_invocation=report,
        callback_invocation_entry_supported=True,
        _activate_transportation_confirmation=activate,
        _ownership_callback_state=lambda: pytest.fail("ownership begin"),
    )
    amb._run = asyncio.run
    amb._invoke_event = lambda value: pytest.fail("body")
    callback_state(amb).restore_outcomes(
        [{"identity": IDENTITY.hex(), "success": success, "exception": exception}]
    )
    try:
        assert not amb._dispatch_callback(event)
        assert not amb._dispatch_callback(event)
        report.assert_awaited_once_with(b"f" * 32, success, exception)
        activate.assert_not_awaited()
        assert callback_state(amb).checkpoint_outcomes() == []
    finally:
        amb._callback_dispatcher.close()


@pytest.mark.asyncio
@pytest.mark.parametrize("success", [True, False])
@pytest.mark.parametrize("interaction", [True, False])
async def test_projected_transport_outcome_only_layer1_never_activates_or_enters(
    success, interaction, federate
):  # noqa: F811
    event = projected_transport_recovery(interaction)
    fed, channel, _ = federate
    fed._callback_entry_supported = True
    exception = "" if success else "ValueError: transportation body"
    fed._activate_transportation_confirmation = AsyncMock(side_effect=AssertionError("activation"))
    callback_state(fed).restore_outcomes(
        [{"identity": IDENTITY.hex(), "success": success, "exception": exception}]
    )
    await fed.dispatch_event(event, lambda value: pytest.fail("body"))
    await fed.dispatch_event(event, lambda value: pytest.fail("body"))
    fed._activate_transportation_confirmation.assert_not_awaited()
    assert len(channel.calls) == 1
    request = channel.calls[0][1]
    assert request.callback_receipt == b"f" * 32 and not request.invocation_entry
    assert request.success == success and request.exception == exception
    assert callback_state(fed).checkpoint_outcomes() == []


@pytest.mark.parametrize("kind", VARIANTS)
def test_every_application_stream_variant_is_portable_without_invocation(kind):
    source = transport()
    event = source._decode_carrier(8, wire_event(kind))
    assert event is not None
    source._deliver_event(8, event)
    envelope = json.loads(source.sdk_checkpoint(8))
    assert envelope["state"]["callback_recovery"] == "server-v2"
    assert envelope["state"]["pending_outcomes"] == []
    assert kind in envelope["state"]["pending_callbacks"][0]
    assert source.events_for(8).qsize() == 1


@pytest.mark.parametrize("kind", VARIANTS)
def test_every_outcome_only_body_is_suppressed_in_standard_dispatch(kind):
    source = transport()
    event = source._decode_carrier(8, wire_event(kind, outcome_only=True))
    amb = Rti1516eAmbassador()
    amb._federate = SimpleNamespace(
        report_callback_invocation=AsyncMock(),
        _activate_transportation_confirmation=AsyncMock(),
        _ownership_callback_state=lambda: pytest.fail("ownership begin"),
    )
    amb._invoke_event = lambda event: pytest.fail("application body")
    amb._run = asyncio.run
    callback_state(amb).restore_outcomes([OUTCOME])
    assert not amb._dispatch_callback(event)
    amb._federate.report_callback_invocation.assert_awaited_once_with(
        b"f" * 32, False, "ValueError: body"
    )
    amb._federate._activate_transportation_confirmation.assert_not_awaited()
    assert not source._logical_times_by_federate


@pytest.mark.asyncio
@pytest.mark.parametrize("kind", VARIANTS)
async def test_every_outcome_only_body_is_suppressed_in_layer1(kind, federate):  # noqa: F811
    fed, channel, _ = federate
    event = transport()._decode_carrier(8, wire_event(kind, outcome_only=True))
    callback_state(fed).restore_outcomes([OUTCOME])
    fed._activate_transportation_confirmation = AsyncMock(side_effect=AssertionError("activation"))
    await fed.dispatch_event(event, lambda event: pytest.fail("application body"))
    assert len(channel.calls) == 1
    assert not channel.calls[0][1].invocation_entry


def test_wire_completed_marker_needs_no_body_and_is_checkpoint_portable():
    source = transport()
    wire = stream_pb2.FederateEvent()
    wire.callback_recovery.invocation_identity = IDENTITY
    wire.callback_recovery.outcome_only = True
    wire.callback_recovery.completed = True
    event = source._decode_carrier(8, wire)
    source._deliver_event(8, event)
    envelope = json.loads(source.sdk_checkpoint(8))
    assert envelope["state"]["pending_callbacks"] == [
        {
            "callback_recovery": {
                "invocation_identity": base64.b64encode(IDENTITY).decode("ascii"),
                "outcome_only": True,
                "completed": True,
            }
        }
    ]


@pytest.mark.parametrize(
    "mutate",
    [
        lambda wire: setattr(wire.callback_recovery, "invocation_identity", b"short"),
        lambda wire: setattr(wire, "callback_receipt", b""),
        lambda wire: setattr(wire.callback_recovery, "completed", True),
    ],
)
def test_malformed_recovery_wire_fails_before_sdk_state_changes(mutate):
    source = transport()
    wire = wire_event("grant", outcome_only=True)
    mutate(wire)
    with pytest.raises(ValueError):
        source._decode_carrier(8, wire)
    assert not source._logical_times_by_federate
    assert not source._callback_ticket_identities


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "kind", ["restore_initiate", "restore_completed", "restore_failed", "restore_begun"]
)
async def test_historical_lifecycle_never_reapplies_restore(kind):
    target = transport()
    target._staged_sdk_restores[8] = ("active-restore", None)
    target.events_for(8).put_nowait("preserved")
    wire = wire_event(kind)
    if kind == "restore_completed":
        wire.restore_completed.federate_handle = 999
        wire.restore_completed.callback_epoch = 77
    event = target._decode_carrier(8, wire)
    assert await target._apply_restore_terminal(8, event) == 8
    assert target._staged_sdk_restores[8] == ("active-restore", None)
    assert target.events_for(8).get_nowait() == "preserved"
    target.refresh_fom.assert_not_awaited()


@pytest.mark.asyncio
async def test_repeated_restore_keeps_identity_and_never_reuses_source_ticket(federate):  # noqa: F811
    from rti1516e.connection import Federate
    from rti1516e.events import FederationRestored, InitiateFederateRestore

    source = transport()
    first = Federate(connection=object(), transport=source, handle=8, name="saved")
    ledger = callback_state(first)
    ledger.claim(b"s" * 32)
    ledger.complete(b"s" * 32, False, "ValueError: body")
    payload = source.sdk_checkpoint(8)
    for epoch in (1, 2):
        target = transport()
        target._callback_epochs[8] = epoch - 1
        fed = Federate(connection=object(), transport=target, handle=8, name="saved")
        await target._apply_restore_terminal(8, InitiateFederateRestore("s", 8, "saved", payload))
        await target._apply_restore_terminal(8, FederationRestored("s", 8, epoch))
        assert callback_state(fed).pending() == []
        wire = wire_event("sync_announced", outcome_only=True)
        wire.callback_receipt = bytes([epoch]) * 32
        event = target._decode_carrier(8, wire)
        fed._service_rpc = AsyncMock(side_effect=RuntimeError("report failed"))
        with pytest.raises(RuntimeError, match="report failed"):
            await fed.dispatch_event(event, lambda event: pytest.fail("body invoked"))
        payload = target.sdk_checkpoint(8)
        assert json.loads(payload)["state"]["pending_outcomes"] == [OUTCOME]
        assert callback_state(fed).pending() == [(bytes([epoch]) * 32, False, "ValueError: body")]


@pytest.mark.parametrize(
    "row",
    [
        {**OUTCOME, "identity": IDENTITY.hex().upper()},
        {**OUTCOME, "identity": "0" * 63},
        {**OUTCOME, "success": 1},
        {**OUTCOME, "exception": None},
        {**OUTCOME, "success": True},
        {**OUTCOME, "extra": True},
    ],
)
def test_bad_completed_outcomes_are_rejected_atomically(row):
    state = GenericCallbackState()
    with pytest.raises(ValueError):
        state.restore_outcomes([OUTCOME, row])
    assert state.checkpoint_outcomes() == []


def test_stale_event_is_rejected_before_body_or_recovery_report():
    source = transport()
    source._restore_serial = 2
    amb = Rti1516eAmbassador()
    amb._federate = SimpleNamespace(
        _transport=source, handle=8, report_callback_invocation=AsyncMock()
    )
    callback_state(amb).restore_outcomes([OUTCOME])
    event = recovery()
    object.__setattr__(event, "_restore_serial", 1)
    with pytest.raises(RuntimeError, match="stale callback"):
        amb._dispatch_callback(event)
    amb._federate.report_callback_invocation.assert_not_awaited()
    assert callback_state(amb).checkpoint_outcomes() == [OUTCOME]


@pytest.mark.parametrize("kind", VARIANTS)
def test_restored_ordinary_stream_callback_enters_and_invokes_only_once(kind):
    source = transport()
    event = source._decode_carrier(8, wire_event(kind))
    amb = Rti1516eAmbassador()
    report = AsyncMock()
    amb._federate = SimpleNamespace(
        report_callback_invocation=report,
        callback_invocation_entry_supported=True,
        _activate_transportation_confirmation=AsyncMock(return_value=event),
    )
    amb._run = asyncio.run
    bodies = []
    amb._invoke_event = lambda value: bodies.append(value) or True
    assert amb._dispatch_callback(event)
    assert not amb._dispatch_callback(event)
    assert bodies == [event]
    assert report.await_count == 2
    assert report.await_args_list[0].kwargs == {"invocation_entry": True}
    assert report.await_args_list[1].args == (b"f" * 32, True, "")


def test_historical_restore_notification_does_not_clear_dispatcher_or_saved_results():
    from rti1516e._callbacks import HLA_EVOKED, CallbackDispatcher

    amb = Rti1516eAmbassador()
    amb._callback_dispatcher.close()
    amb._callback_dispatcher = CallbackDispatcher(amb._dispatch_callback, HLA_EVOKED)
    callback_state(amb).restore_outcomes([OUTCOME])
    prior = SynchronizationPointAnnounced("prior", b"", ())
    amb._dispatch_event(prior)
    wire = wire_event("restore_completed")
    wire.restore_completed.federate_handle = 999
    wire.restore_completed.callback_epoch = 77
    event = transport()._decode_carrier(8, wire)
    amb._dispatch_event(event)
    assert amb._callback_dispatcher.snapshot_pending() == [prior, event]
    assert callback_state(amb).checkpoint_outcomes() == [OUTCOME]
    amb._callback_dispatcher.close()


def test_legacy_private_state_remains_readable_and_v2_requires_outcomes():
    from rti1516e._runtime_checkpoint import decode, encode

    runtime = decode(transport().sdk_checkpoint(8), "HLAinteger64Time")
    runtime.pop("decoded_callbacks")
    runtime.pop("decoded_time")
    runtime.pop("pending_outcomes")
    with pytest.raises(ValueError, match="private fields"):
        encode(runtime)
    runtime["callback_recovery"] = "server"
    assert decode(encode(runtime), "HLAinteger64Time")["callback_recovery"] == "server"


@pytest.mark.asyncio
async def test_unsupported_runtime_staging_preserves_queue_and_identity():
    from rti1516e._runtime_checkpoint import _checksum
    from rti1516e.events import InitiateFederateRestore

    target = transport()
    state = json.loads(target.sdk_checkpoint(8))
    target.events_for(8).put_nowait("preserved")
    state["state"]["callback_recovery"] = "server-v3"
    raw = json.dumps(state["state"], sort_keys=True, separators=(",", ":")).encode()
    state["checksum"] = _checksum(state, raw)
    with pytest.raises(ValueError):
        await target._apply_restore_terminal(
            8, InitiateFederateRestore("s", 8, "saved", json.dumps(state).encode())
        )
    assert target._staged_sdk_restores == {}
    assert target.events_for(8).get_nowait() == "preserved"
    target.refresh_fom.assert_not_awaited()


def test_outcome_capacity_and_conflict_do_not_mutate_existing_results():
    state = GenericCallbackState(capacity=1)
    state.restore_outcomes([OUTCOME])
    with pytest.raises(RuntimeError, match="capacity"):
        state.complete(b"other", True, "")
    assert state._tickets == {}
    state.recover(recovery())
    with pytest.raises(RuntimeError, match="conflicting"):
        state.complete(b"f" * 32, True, "")
    assert state.checkpoint_outcomes() == [OUTCOME]
