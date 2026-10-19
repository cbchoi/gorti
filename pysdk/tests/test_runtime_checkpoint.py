import json
from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest

from rti1516e._transport import GrpcTransport, _ensure_generated_path
from rti1516e.connection import Federate
from rti1516e.events import FederationRestored, InitiateFederateRestore

_ensure_generated_path()
from rti.v1 import stream_pb2  # noqa: E402 - generated imports need path bootstrap above


def transport():
    value = GrpcTransport(
        SimpleNamespace(unary_unary=lambda *a, **k: None, unary_stream=lambda *a, **k: None),
        "grpc://unused",
    )
    value._federation_name = "f"
    value._generation_by_federation = {"f": 1}
    value._logical_time_by_federation = {"f": "HLAinteger64Time"}
    value._callback_entry_supported = {8: True}
    value._session_tokens = {8: b"t" * 32}
    value.refresh_fom = AsyncMock()
    return value


def carrier(value=9007199254740993):
    wire = stream_pb2.FederateEvent()
    wire.grant.logical_time_value.integer64_value = value
    wire.callback_receipt = b"o" * 32
    return wire


def test_checkpoint_nonempty_exact_queue_read_is_not_invocation():
    source = transport()
    event = source._decode_carrier(8, carrier())
    source._deliver_event(8, event)
    state = json.loads(source.sdk_checkpoint(8))
    assert state["magic"] == "GRS1"
    assert state["language"] == "python"
    assert state["version"] == 1
    assert state["state"]["logical_time"] == {"integer64_value": "9007199254740993"}
    assert len(state["state"]["pending_callbacks"]) == 1
    assert source.events_for(8).qsize() == 1
    assert source.events_for(8).get_nowait() is event
    assert not hasattr(source, "_generic_callback_invocations")


@pytest.mark.asyncio
async def test_checkpoint_staged_then_applied_with_server_replay_and_target_capability():
    source = transport()
    source._decode_carrier(8, carrier())
    state = source.sdk_checkpoint(8)
    target = transport()
    target._callback_entry_supported[8] = False
    fed = Federate(connection=object(), transport=target, handle=8, name="saved")
    target.events_for(8).put_nowait("stale")
    initiated = InitiateFederateRestore("s", 8, "saved", client_state=state)
    await target._apply_restore_terminal(8, initiated)
    assert target.events_for(8).qsize() == 1
    assert await target._apply_restore_terminal(8, FederationRestored("s", 8, 1)) == 8
    assert fed.callback_invocation_entry_supported is False
    assert target._logical_times_by_federate[8] == 9007199254740993
    assert target.events_for(8).empty()


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "payload", [b"{", b"[]", b"x" * (1048576 + 1)], ids=["truncated", "wrong-root", "oversize"]
)
async def test_corrupt_checkpoint_does_not_mutate_identity_or_queue(payload):
    target = transport()
    fed = Federate(connection=object(), transport=target, handle=8, name="saved")
    target.events_for(8).put_nowait("keep")
    with pytest.raises(ValueError):
        await target._apply_restore_terminal(
            8, InitiateFederateRestore("s", 8, "saved", client_state=payload)
        )
    assert fed.handle == 8
    assert target.events_for(8).get_nowait() == "keep"
    target.refresh_fom.assert_not_awaited()


@pytest.mark.asyncio
async def test_legacy_empty_checkpoint_preserves_queue_purge():
    target = transport()
    target.events_for(8).put_nowait("stale")
    await target._apply_restore_terminal(8, InitiateFederateRestore("s", 8, "saved"))
    await target._apply_restore_terminal(8, FederationRestored("s", 2, 1))
    assert target.events_for(2).empty()


@pytest.mark.asyncio
async def test_save_complete_sends_provider_checkpoint():
    from rti1516e.savepoint import SavepointClient

    client = object.__new__(SavepointClient)
    client._federation_name, client._federate_handle = "f", 8
    client._checkpoint_provider = lambda: b"private-state"
    client._stub = SimpleNamespace(FederateSaveComplete=AsyncMock())
    await client.federate_save_complete()
    assert client._stub.FederateSaveComplete.call_args.args[0].client_state == b"private-state"


def test_dispatcher_snapshot_does_not_invoke_callbacks():
    from rti1516e._callbacks import HLA_EVOKED, CallbackDispatcher

    invoked = []
    dispatcher = CallbackDispatcher(lambda event: invoked.append(event), HLA_EVOKED)
    dispatcher.submit("pending", wait=False)
    assert dispatcher.snapshot_pending() == ["pending"]
    assert invoked == []
    dispatcher.close()


@pytest.mark.parametrize(
    "family,value",
    [
        ("HLAinteger64Time", 9007199254740993),
        ("HLAinteger64Time", (1 << 63) - 1),
        ("HLAfloat64Time", 0.0),
        ("HLAfloat64Time", -0.0),
        ("HLAfloat64Time", 1.2345678901234567),
    ],
)
def test_exact_time_roundtrip(family, value):
    from rti1516e._runtime_checkpoint import decode

    source = transport()
    source._logical_time_by_federation["f"] = family
    source._logical_times_by_federate[8] = value
    decoded = decode(source.sdk_checkpoint(8), family)["decoded_time"]
    assert type(decoded) is type(value)
    assert decoded == value
    if isinstance(value, float):
        assert decoded.hex() == value.hex()


@pytest.mark.asyncio
async def test_recreated_generation_restores_knowledge_and_retractions_without_old_tickets():
    from rti1516e._ownership_callbacks import OwnershipCallbacks
    from rti1516e._runtime_checkpoint import decode, encode

    source = transport()
    state = OwnershipCallbacks()
    state._latest = {11: 7, 12: 9}
    state._retired = {12: 8}
    source._ownership_callbacks_by_federate[8] = state
    source._used_retractions[8] = {13, (1 << 64) - 1}
    source._deliver_event(8, source._decode_carrier(8, carrier()))
    runtime = decode(source.sdk_checkpoint(8), "HLAinteger64Time")
    runtime.pop("decoded_time")
    runtime.pop("decoded_callbacks")
    runtime["federation_generation"] = 2
    runtime["federate_handle"] = 17
    target = transport()
    target._generation_by_federation["f"] = 2
    fed = Federate(connection=object(), transport=target, handle=8, name="saved")
    fed._ownership_callback_state().enabled = True
    await target._apply_restore_terminal(
        8, InitiateFederateRestore("s", 8, "saved", encode(runtime))
    )
    await target._apply_restore_terminal(8, FederationRestored("s", 17, 4, 20))
    assert fed.handle == 17
    assert fed.callback_invocation_entry_supported is True
    assert fed._ownership_callback_state().scope == (2, 4)
    assert fed._ownership_callback_state()._latest == {11: 7, 12: 9}
    assert fed._ownership_callback_state()._retired == {12: 8}
    assert target._used_retractions[17] == {13, (1 << 64) - 1}
    assert target.events_for(17).empty()
    assert fed.next_retraction_handle == 20


@pytest.mark.parametrize(
    "change",
    [
        lambda state: state.update(version=2),
        lambda state: state.update(language="go"),
        lambda state: state.update(checksum="0" * 64),
        lambda state: state.update(extra="unrecognized"),
        lambda state: state.update(generation=True),
        lambda state: state.update(scalar="HLAfloat64Time"),
    ],
)
def test_envelope_tampering_is_rejected(change):
    from rti1516e._runtime_checkpoint import decode

    value = json.loads(transport().sdk_checkpoint(8))
    change(value)
    with pytest.raises(ValueError):
        decode(json.dumps(value).encode(), "HLAinteger64Time")


@pytest.mark.parametrize(
    "change",
    [
        lambda state: state.update(callback_epoch=True),
        lambda state: state.update(logical_time={"integer64_value": "09007199254740993"}),
        lambda state: state.update(logical_time={"float64_value": 1.0}),
        lambda state: state.update(object_knowledge={"latest": [[11, 1], [11, 2]], "retired": []}),
        lambda state: state.update(used_retraction_handles=[1, 1]),
        lambda state: state.update(pending_callbacks=[{}]),
        lambda state: state.update(callback_recovery="local"),
    ],
)
def test_rehashed_invalid_private_state_is_rejected(change):
    from rti1516e._runtime_checkpoint import decode, encode

    state = decode(transport().sdk_checkpoint(8), "HLAinteger64Time")
    state.pop("decoded_time")
    state.pop("decoded_callbacks")
    change(state)
    with pytest.raises(ValueError):
        encode(state)


def test_receiptless_pending_callback_fails_closed():
    source = transport()
    wire = carrier()
    wire.ClearField("callback_receipt")
    source._deliver_event(8, source._decode_carrier(8, wire))
    with pytest.raises(ValueError, match="recovery receipt"):
        source.sdk_checkpoint(8)
    assert source.events_for(8).qsize() == 1


def test_unresolved_ownership_outcome_fails_closed():
    fed = Federate(connection=object(), transport=transport(), handle=8, name="saved")
    fed._ownership_callback_state()._pending[(1, 0, 2)] = object()
    with pytest.raises(RuntimeError, match="unresolved ownership"):
        fed._transport.sdk_checkpoint(8)


def test_executing_callback_fails_closed():
    from rti1516e._callbacks import HLA_EVOKED, CallbackDispatcher

    observed = []
    dispatcher = None

    def invoke(event):
        with pytest.raises(RuntimeError, match="executing callback"):
            dispatcher.snapshot_pending()
        observed.append(event)
        return True

    dispatcher = CallbackDispatcher(invoke, HLA_EVOKED)
    dispatcher.submit("pending", wait=False)
    assert dispatcher.evoke(0, 0, multiple=False)
    assert observed == ["pending"]
    dispatcher.close()


def test_unknown_generic_outcome_fails_closed_but_known_finished_is_portable():
    from rti1516e._generic_callbacks import callback_state
    from rti1516e.standard import Rti1516eAmbassador

    ambassador = Rti1516eAmbassador()
    state = callback_state(ambassador)
    assert state.claim(b"entered")
    with pytest.raises(RuntimeError, match="unresolved callback invocation"):
        ambassador._checkpoint_pending_callbacks()
    state.complete(b"entered", True, "")
    assert ambassador._checkpoint_pending_callbacks() == []
    ambassador._callback_dispatcher.close()


def test_duplicate_json_keys_and_checksum_original_escaping():
    import hashlib

    from rti1516e._runtime_checkpoint import decode

    source = transport()
    # Whitespace/member order must not change checksum meaning.
    payload = source.sdk_checkpoint(8)
    envelope = json.loads(payload)
    with pytest.raises(ValueError, match="duplicate"):
        decode(
            payload.replace(b'"magic":"GRS1"', b'"magic":"GRS1","magic":"GRS1"'), "HLAinteger64Time"
        )
    state = json.dumps(envelope["state"], separators=(",", ":"))
    header = b"GRS1\n1\npython\nHLAinteger64Time\n1\n8\n"
    envelope["checksum"] = hashlib.sha256(header + state.encode()).hexdigest()
    assert decode(json.dumps(envelope, indent=2).encode(), "HLAinteger64Time")


@pytest.mark.asyncio
async def test_direct_invocation_entry_requires_outcome_before_checkpoint():
    target = transport()
    fed = Federate(connection=object(), transport=target, handle=8, name="saved")
    fed._service_rpc = AsyncMock()
    receipt = b"e" * 32
    await fed.report_callback_invocation(receipt, True, invocation_entry=True)
    with pytest.raises(RuntimeError, match="unresolved callback"):
        target.sdk_checkpoint(8)
    fed._service_rpc.side_effect = RuntimeError("outcome transport uncertain")
    with pytest.raises(RuntimeError, match="uncertain"):
        await fed.report_callback_invocation(receipt, True)
    import hashlib

    assert json.loads(target.sdk_checkpoint(8))["state"]["pending_outcomes"] == [
        {"identity": hashlib.sha256(receipt).hexdigest(), "success": True, "exception": ""}
    ]
    fed._service_rpc.side_effect = None
    await fed.report_callback_invocation(receipt, True)
    assert target.sdk_checkpoint(8)
