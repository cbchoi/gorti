"""Resignation must not destroy a membership's stream before RPC acceptance."""

import asyncio
from types import SimpleNamespace

import pytest

from rti1516e._transport import GrpcTransport, _CallbackStreamClosed
from rti1516e.errors import OwnershipAcquisitionPending
from rti1516e.events import SynchronizationPointAnnounced
from tests.spec.m27.test_ieee2010_divest_followup import _barrier, _participants
from tests.test_live_service_e2e import live_artifacts  # noqa: F401 - pytest fixture
from tests.test_live_service_e2e import live_server as _live_server  # noqa: F401 - named fixture


async def _transport(rpc, *, sink=None):
    transport = object.__new__(GrpcTransport)
    transport._federation_name = "resign-test"
    transport._session_tokens = {1: b"s" * 32}
    transport._event_queues = {}
    transport._event_sinks = {} if sink is None else {1: sink}
    transport._stale_federation_generations = set()
    stream_closed = asyncio.Event()

    async def accepted_rpc(*args, **kwargs):
        await rpc(*args, **kwargs)
        stream_closed.set()

    transport.federation = SimpleNamespace(ResignFederation=accepted_rpc)
    task = asyncio.create_task(stream_closed.wait())
    transport._stream_tasks = {1: task}
    await asyncio.sleep(0)
    return transport, task


async def _close_task(task):
    task.cancel()
    await asyncio.gather(task, return_exceptions=True)


@pytest.mark.asyncio
async def test_resign_rpc_precedes_stream_retirement():
    async def rpc(request, **kwargs):
        assert transport._stream_tasks[1] is task
        assert not task.done()
        assert transport.session_token(1) == b"s" * 32
        assert kwargs["metadata"] == transport.session_metadata(b"s" * 32)

    transport, task = await _transport(rpc)
    try:
        await transport._resign_federation(1)
        assert task.done() and not task.cancelled()
        assert 1 not in transport._stream_tasks
        assert transport.session_token(1) == b""
        assert transport._stale_federation_generations == {"resign-test"}
    finally:
        await _close_task(task)


@pytest.mark.asyncio
async def test_rejected_resign_preserves_stream_session_and_retry():
    failure = OwnershipAcquisitionPending("pending acquisition")
    actions = []

    async def rpc(request, **kwargs):
        actions.append(request.action)
        if len(actions) == 1:
            raise failure

    transport, task = await _transport(rpc)
    try:
        with pytest.raises(OwnershipAcquisitionPending) as caught:
            await transport._resign_federation(1)
        assert caught.value is failure
        assert transport._stream_tasks.get(1) is task
        assert not task.done()
        assert transport.session_token(1) == b"s" * 32
        assert not transport._stale_federation_generations
        await transport._resign_federation(1, "CANCEL_THEN_DELETE_THEN_DIVEST")
        from rti.v1 import common_pb2

        assert actions == [
            common_pb2.RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES,
            common_pb2.RESIGN_ACTION_CANCEL_THEN_DELETE,
        ]
        assert task.done() and not task.cancelled()
    finally:
        await _close_task(task)


@pytest.mark.asyncio
async def test_invalid_resign_action_preserves_stream():
    async def rpc(*args, **kwargs):
        pytest.fail("invalid action reached RPC")

    transport, task = await _transport(rpc)
    try:
        with pytest.raises(ValueError):
            await transport._resign_federation(1, "INVALID")
        assert transport._stream_tasks.get(1) is task
        assert not task.done()
    finally:
        await _close_task(task)


@pytest.mark.asyncio
@pytest.mark.parametrize("direct_sink", [False, True])
@pytest.mark.parametrize("accepted", [False, True])
async def test_stream_end_is_expected_only_after_success(direct_sink, accepted):
    delivered = []
    terminal = _CallbackStreamClosed("callback stream closed unexpectedly")

    async def rpc(*args, **kwargs):
        transport._deliver_event(1, terminal)
        await asyncio.sleep(0)
        assert not delivered
        assert transport.events_for(1).empty()
        if not accepted:
            raise OwnershipAcquisitionPending("pending acquisition")

    sink = delivered.append if direct_sink else None
    transport, task = await _transport(rpc, sink=sink)
    try:
        if accepted:
            await transport._resign_federation(1)
        else:
            with pytest.raises(OwnershipAcquisitionPending):
                await transport._resign_federation(1)
        values = delivered if direct_sink else list(transport.events_for(1)._queue)
        assert values == ([] if accepted else [terminal])
        assert transport._event_sinks.get(1) == sink
    finally:
        await _close_task(task)


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "detail", ["broken callback decoding", "callback stream closed unexpectedly"]
)
async def test_successful_resign_does_not_hide_unrelated_stream_failure(detail):
    failure = RuntimeError(detail)

    async def rpc(*args, **kwargs):
        transport._deliver_event(1, failure)

    transport, task = await _transport(rpc)
    try:
        await transport._resign_federation(1)
        assert await transport.events_for(1).get() is failure
    finally:
        await _close_task(task)


@pytest.mark.asyncio
async def test_cancelled_resign_preserves_membership_and_replays_stream_end():
    entered = asyncio.Event()
    terminal = _CallbackStreamClosed("callback stream closed unexpectedly")

    async def rpc(*args, **kwargs):
        transport._deliver_event(1, terminal)
        entered.set()
        await asyncio.Event().wait()

    transport, task = await _transport(rpc)
    operation = asyncio.create_task(transport._resign_federation(1))
    try:
        await entered.wait()
        operation.cancel()
        with pytest.raises(asyncio.CancelledError):
            await operation
        assert transport._stream_tasks.get(1) is task
        assert not task.done()
        assert transport.session_token(1) == b"s" * 32
        assert not transport._stale_federation_generations
        assert await transport.events_for(1).get() is terminal
        assert 1 not in transport._event_sinks
    finally:
        await _close_task(operation)
        await _close_task(task)


@pytest.mark.asyncio
@pytest.mark.parametrize("replacement", ["token", "task", "both"])
async def test_accepted_resign_preserves_replacement_membership(replacement):
    replacement_task = asyncio.create_task(asyncio.Event().wait())
    replacement_state = object()

    async def rpc(request, **kwargs):
        assert kwargs["metadata"] == transport.session_metadata(b"s" * 32)
        if replacement in ("token", "both"):
            transport._session_tokens[1] = b"n" * 32
        if replacement in ("task", "both"):
            transport._stream_tasks[1] = replacement_task
        transport._ownership_callbacks_by_federate[1] = replacement_state

    transport, task = await _transport(rpc)
    transport._ownership_callbacks_by_federate = {}
    try:
        await asyncio.wait_for(transport._resign_federation(1), 0.5)
        assert transport._stream_tasks[1] is (
            replacement_task if replacement in ("task", "both") else task
        )
        assert transport.session_token(1) == (
            b"n" * 32 if replacement in ("token", "both") else b"s" * 32
        )
        assert transport._ownership_callbacks_by_federate[1] is replacement_state
        assert not replacement_task.done()
        assert task.done() and not task.cancelled()
    finally:
        await _close_task(task)
        await _close_task(replacement_task)


@pytest.mark.asyncio
@pytest.mark.parametrize("accepted", [False, True])
async def test_stale_explicit_resign_does_not_intercept_current_stream(accepted):
    delivered = []
    terminal = _CallbackStreamClosed("new membership EOF")

    async def rpc(request, **kwargs):
        assert kwargs["metadata"] == transport.session_metadata(b"o" * 32)
        transport._deliver_event(1, terminal)
        assert delivered == [terminal]
        if not accepted:
            raise OwnershipAcquisitionPending("old membership rejected")

    transport, task = await _transport(rpc, sink=delivered.append)
    try:
        if accepted:
            await transport._resign_federation(1, session_token=b"o" * 32)
        else:
            with pytest.raises(OwnershipAcquisitionPending):
                await transport._resign_federation(1, session_token=b"o" * 32)
        assert transport._stream_tasks[1] is task
        assert transport.session_token(1) == b"s" * 32
        assert transport._event_sinks[1] == delivered.append
    finally:
        await _close_task(task)


@pytest.mark.asyncio
async def test_rejected_resign_does_not_replay_old_eof_to_replacement_sink():
    delivered = []
    terminal = _CallbackStreamClosed("old membership EOF")
    replacement_task = asyncio.create_task(asyncio.Event().wait())

    async def rpc(*args, **kwargs):
        transport._deliver_event(1, terminal)
        transport._session_tokens[1] = b"n" * 32
        transport._stream_tasks[1] = replacement_task
        transport._event_sinks[1] = delivered.append
        raise OwnershipAcquisitionPending("old membership rejected")

    transport, task = await _transport(rpc)
    try:
        with pytest.raises(OwnershipAcquisitionPending):
            await transport._resign_federation(1)
        assert delivered == []
        assert transport._event_sinks[1] == delivered.append
        assert transport._stream_tasks[1] is replacement_task
        assert transport.session_token(1) == b"n" * 32
    finally:
        await _close_task(task)
        await _close_task(replacement_task)


@pytest.mark.asyncio
async def test_replaced_drainer_does_not_deliver_its_eof_to_new_membership():
    started, finish = asyncio.Event(), asyncio.Event()
    natural_eof = asyncio.Event()
    delivered = []

    async def stream():
        started.set()
        await finish.wait()
        yield object()
        natural_eof.set()

    async def rpc(*args, **kwargs):
        pass

    transport, placeholder = await _transport(rpc)
    await _close_task(placeholder)
    transport.streams = SimpleNamespace(Events=lambda *args, **kwargs: stream())
    old_task = asyncio.create_task(transport._drain_events(1))
    transport._stream_tasks[1] = old_task
    await started.wait()
    replacement_task = asyncio.create_task(asyncio.Event().wait())
    transport._stream_tasks[1] = replacement_task
    transport._session_tokens[1] = b"n" * 32
    transport._event_sinks[1] = delivered.append
    try:
        finish.set()
        await asyncio.wait_for(old_task, 0.5)
        assert natural_eof.is_set(), "stale drainer did not finish the server iterator naturally"
        assert delivered == []
        assert transport._stream_tasks[1] is replacement_task
        assert not replacement_task.done()
    finally:
        await _close_task(old_task)
        await _close_task(replacement_task)


@pytest.mark.integration
def test_live_pending_acquisition_rejects_default_resign_without_losing_callbacks(live_server):
    async def exercise():
        contexts = []
        async with _participants(live_server, join_contexts=contexts) as result:
            federates, callbacks, obj, attr = result
            owner, acquirer, _ = federates
            context = contexts[1]
            context.resign_action = None
            await acquirer.ownership.acquire(obj, [attr], tag=b"pending-resign")
            await _barrier(owner, callbacks, "before-rejected-resign")
            transport = acquirer._transport
            task = transport._stream_tasks[acquirer.handle]
            token = transport.session_token(acquirer.handle)
            try:
                with pytest.raises(OwnershipAcquisitionPending) as caught:
                    await context.__aexit__(None, None, None)
                assert type(caught.value) is OwnershipAcquisitionPending
                metadata = dict(caught.value.__cause__.trailing_metadata())
                assert metadata["rti-spec-exception"] == "OwnershipAcquisitionPending"
                assert context._federate is acquirer
                assert transport._stream_tasks.get(acquirer.handle) is task
                assert not task.done()
                assert transport.session_token(acquirer.handle) == token
                await _barrier(owner, callbacks, "after-rejected-resign")
                assert any(
                    isinstance(event, SynchronizationPointAnnounced)
                    and event.label == "after-rejected-resign"
                    for event in callbacks[1].events
                )
                assert await owner.ownership.is_attribute_owned_by_federate(obj, attr)
                assert not await acquirer.ownership.is_attribute_owned_by_federate(obj, attr)
            finally:
                context.resign_action = "CANCEL_THEN_DELETE_THEN_DIVEST"
                await context.__aexit__(None, None, None)
            assert context._federate is None
            assert acquirer.handle not in transport._stream_tasks
            assert transport.session_token(acquirer.handle) == b""
            assert callbacks[1].failure is None
            await _barrier(owner, [callbacks[0], callbacks[2]], "after-explicit-cancel")

    asyncio.run(exercise())
