"""Independent regressions for ownership receipt and local-delete scope handling."""

import asyncio
from contextlib import asynccontextmanager
from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest

from rti1516e._transport import GrpcTransport, _ensure_generated_path
from rti1516e.connection import Federate
from rti1516e.events import (
    AttributeOwnershipUnavailable,
    DiscoverObjectInstance,
    FederationRestored,
    RequestAttributeOwnershipAssumption,
)


@pytest.fixture
def callback_transport():
    _ensure_generated_path()
    transport = object.__new__(GrpcTransport)
    transport._federation_name = "ownership-review"
    transport._generation_by_federation = {"ownership-review": 4}
    transport._callback_epochs = {8: 0, 9: 0}
    transport._session_tokens = {8: b"a" * 32, 9: b"b" * 32}
    transport._event_queues = {}
    transport._event_sinks = {}
    transport._stream_tasks = {}
    transport._identity_bindings = {}
    transport.refresh_fom = AsyncMock()
    transport.objects = SimpleNamespace(LocalDeleteObjectInstanceFenced=AsyncMock(
        return_value=SimpleNamespace(
            retired_knowledge_epoch=10,
            federation_generation=4,
            callback_epoch=0,
        )
    ))
    return transport


def joined_federate(transport, handle):
    @asynccontextmanager
    async def admit():
        yield transport

    client = Federate(
        connection=SimpleNamespace(_admit_work=admit),
        transport=transport,
        handle=handle,
        name=f"review-{handle}",
    )
    client._ownership_callback_rpc = AsyncMock()
    state = client._ownership_callback_state()
    state.enabled = True
    state.scope = (4, 0)
    return client, state


def assumption(epoch):
    event = RequestAttributeOwnershipAssumption(80, (1,), 2, b"")
    object.__setattr__(event, "_object_knowledge_epoch", epoch)
    return event


def discovery(epoch):
    event = DiscoverObjectInstance(80, "class", "object")
    object.__setattr__(event, "_object_knowledge_epoch", epoch)
    return event


def terminal(epoch=0, attempt=1):
    event = AttributeOwnershipUnavailable(80, (1,))
    object.__setattr__(
        event,
        "_ownership_receipt",
        SimpleNamespace(
            federation_generation=4,
            callback_epoch=epoch,
            callback_id=1,
            delivery_attempt=attempt,
        ),
    )
    return event


@pytest.mark.asyncio
async def test_shared_transport_local_delete_retires_only_requesting_federate(callback_transport):
    first, first_state = joined_federate(callback_transport, 8)
    second, second_state = joined_federate(callback_transport, 9)
    assert first_state is not second_state
    first_state.observe(discovery(10))
    second_state.observe(discovery(10))

    await first.local_delete_object_instance(80)

    first_calls, second_calls = [], []
    await first.dispatch_event(assumption(10), first_calls.append)
    await second.dispatch_event(assumption(10), second_calls.append)
    assert first_calls == [], "the deleting federate must filter its buffered old assumption"
    assert len(second_calls) == 1, "a peer's independent object knowledge must remain live"
    callback_transport.objects.LocalDeleteObjectInstanceFenced.assert_awaited_once()
    request = callback_transport.objects.LocalDeleteObjectInstanceFenced.await_args.args[0]
    assert request.federate_handle == 8
    assert request.object_handle == 80

    fresh = discovery(11)
    first_state.observe(fresh)
    await first.dispatch_event(fresh, first_calls.append)
    await first.dispatch_event(assumption(11), first_calls.append)
    assert len(first_calls) == 2


@pytest.mark.asyncio
async def test_shared_transport_restore_moves_only_restored_federate_receipt_ledger(
    callback_transport,
):
    first, first_state = joined_federate(callback_transport, 8)
    second, second_state = joined_federate(callback_transport, 9)
    first_calls, second_calls = [], []
    await first.dispatch_event(terminal(), first_calls.append)
    await second.dispatch_event(terminal(), second_calls.append)
    first_state.retire(80, 10)
    second_state.retire(80, 2)
    first_state.observe(discovery(11))
    second_state.observe(discovery(3))

    await callback_transport._apply_restore_terminal(8, FederationRestored("saved", 2, 1))

    assert first.handle == 2
    assert second.handle == 9
    assert first._ownership_callback_state() is first_state
    assert second._ownership_callback_state() is second_state
    assert first_state.scope == (4, 1)
    assert second_state.scope == (4, 0)
    await first.dispatch_event(terminal(epoch=1), first_calls.append)
    await second.dispatch_event(terminal(attempt=2), second_calls.append)
    assert len(first_calls) == 2, "restored callback IDs must have a fresh dedup scope"
    assert len(second_calls) == 1, "an unrelated restore must not clear peer dedup"
    assert first_state.begin(discovery(1))
    assert not second_state.begin(assumption(2))
    assert second_state.begin(assumption(3))
    assert first_state.pending() == second_state.pending() == []


@pytest.mark.asyncio
async def test_epoch_zero_delete_response_is_stale_after_restore_handle_change(callback_transport):
    client, state = joined_federate(callback_transport, 8)
    entered, release = asyncio.Event(), asyncio.Event()

    async def delayed_response(request, **kwargs):
        assert request.federate_handle == 8
        entered.set()
        await release.wait()
        return SimpleNamespace(
            retired_knowledge_epoch=10,
            federation_generation=4,
            callback_epoch=0,
        )

    callback_transport.objects.LocalDeleteObjectInstanceFenced.side_effect = delayed_response
    deletion = asyncio.create_task(client.local_delete_object_instance(80))
    try:
        await asyncio.wait_for(entered.wait(), 1)
        await callback_transport._apply_restore_terminal(8, FederationRestored("saved", 2, 1))
        assert client.handle == 2
        assert state.scope == (4, 1)
        assert 8 not in callback_transport._callback_epochs
        release.set()

        with pytest.raises(RuntimeError, match="stale"):
            await asyncio.wait_for(deletion, 1)

        calls = []
        fresh = discovery(1)
        state.observe(fresh)
        await client.dispatch_event(fresh, calls.append)
        await client.dispatch_event(assumption(1), calls.append)
        assert len(calls) == 2, "the old delete response must not retire restored knowledge"
    finally:
        release.set()
        if not deletion.done():
            deletion.cancel()
        await asyncio.gather(deletion, return_exceptions=True)


@pytest.mark.asyncio
@pytest.mark.parametrize("async_handler", [False, True], ids=["sync-handler", "async-handler"])
async def test_handler_exception_remains_primary_on_ack_failure_and_retry_deduplicates(
    callback_transport, async_handler,
):
    client, state = joined_federate(callback_transport, 8)
    handler_error = ValueError("handler failed")
    ack_error = RuntimeError("ACK unavailable")
    client._ownership_callback_rpc.side_effect = ack_error
    entered = []

    def fail(event):
        entered.append(event)
        raise handler_error

    async def fail_async(event):
        fail(event)

    callback = fail_async if async_handler else fail
    event = terminal()
    with pytest.raises(ValueError, match="handler failed") as failure:
        await client.dispatch_event(event, callback)
    assert failure.value is handler_error
    assert entered == [event]
    assert state.active_task is None
    assert state.pending() == [event._ownership_receipt]

    client._ownership_callback_rpc.side_effect = None
    retry = terminal(attempt=2)
    await client.dispatch_event(retry, callback)
    assert entered == [event], "receipt retries must never invoke the failed handler again"
    assert state.pending() == []
    assert client._ownership_callback_rpc.await_count == 2
    method, reported = client._ownership_callback_rpc.await_args.args
    assert method == "ReportOwnershipCallbackInvocation"
    assert reported is retry._ownership_receipt
    assert reported.delivery_attempt == 2
