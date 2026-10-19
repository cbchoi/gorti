import asyncio
from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest

from rti1516e._transport import GrpcTransport
from rti1516e.connection import Federate
from rti1516e.events import FederationRestored, InitiateFederateRestore


@pytest.mark.asyncio
async def test_queue_preserves_event_type_and_filters_old_restore_serial():
    from rti1516e.events import ReceiveInteraction

    class Connection:
        async def _next_event(self, queue):
            return await queue.get()

    transport = GrpcTransport(SimpleNamespace(unary_unary=lambda *a, **k: None,
                                            unary_stream=lambda *a, **k: None), "grpc://unused")
    fed = Federate(connection=Connection(), transport=transport, handle=8, name="saved")
    old = ReceiveInteraction("Ping", {"Value": b"old"}, None)
    transport._deliver_event(8, old)
    assert transport.events_for(8).get_nowait() is old
    transport.events_for(8).put_nowait(old)
    transport._restore_serial = 1
    current = ReceiveInteraction("Ping", {"Value": b"new"}, None)
    transport._deliver_event(8, current)
    iterator = fed.events()
    assert await anext(iterator) is current
    await iterator.aclose()


@pytest.mark.asyncio
async def test_restore_terminal_switches_identity_clients_and_queue_together():
    transport = object.__new__(GrpcTransport)
    transport._callback_entry_supported = {8: True}
    queue = asyncio.Queue()
    queue.put_nowait("stale")
    transport._event_queues = {8: queue}
    transport._event_sinks = {}
    transport._stream_tasks = {8: object()}
    transport._identity_bindings = {}
    transport._callback_epochs = {}
    transport.refresh_fom = AsyncMock()
    fed = Federate(connection=object(), transport=transport, handle=8, name="saved")
    old_client = SimpleNamespace(_federate_handle=8)
    fed._support_client = old_client
    initiated = InitiateFederateRestore(label="s", federate_handle=2, federate_name="saved")
    assert await transport._apply_restore_terminal(8, initiated) == 8
    assert fed.handle == 8
    assert await transport._apply_restore_terminal(8, FederationRestored("s", 2, 1)) == 2
    assert fed.handle == 2
    assert fed.callback_invocation_entry_supported is True
    assert old_client._federate_handle == 2
    assert transport._event_queues[2] is queue
    assert queue.empty()
    assert 8 not in transport._stream_tasks
    transport.refresh_fom.assert_awaited_once_with(2)


@pytest.mark.asyncio
async def test_restore_refresh_failure_does_not_switch_local_identity():
    transport = object.__new__(GrpcTransport)
    transport._callback_epochs = {}
    transport._event_queues = {}
    transport.refresh_fom = AsyncMock(side_effect=RuntimeError("refresh failed"))
    with pytest.raises(RuntimeError, match="refresh failed"):
        await transport._apply_restore_terminal(8, FederationRestored("s", 2, 1))
    assert transport._callback_epochs == {}
    with pytest.raises(RuntimeError, match="transition failed"):
        transport.guard_service()


@pytest.mark.asyncio
async def test_restore_blocks_services_and_purges_queue_before_refresh_await():
    transport = object.__new__(GrpcTransport)
    transport._callback_epochs = {}
    queue = asyncio.Queue()
    queue.put_nowait("stale")
    transport._event_queues = {8: queue}
    transport._event_sinks = {}
    transport._stream_tasks = {}
    transport._identity_bindings = {}
    entered, release = asyncio.Event(), asyncio.Event()

    async def refresh(_):
        entered.set()
        await release.wait()

    transport.refresh_fom = refresh
    pending = asyncio.create_task(
        transport._apply_restore_terminal(8, FederationRestored("s", 2, 1))
    )
    await entered.wait()
    assert queue.empty()
    with pytest.raises(RuntimeError, match="is applying"):
        transport.guard_service()
    release.set()
    assert await pending == 2
    transport.guard_service()


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "event",
    [
        FederationRestored("s", None, 1),
        FederationRestored("s", 2, None),
        FederationRestored("s", 0, 1),
    ],
)
async def test_partial_or_invalid_terminal_poison_services(event):
    transport = object.__new__(GrpcTransport)
    transport._callback_epochs = {}
    with pytest.raises(RuntimeError, match="invalid restore"):
        await transport._apply_restore_terminal(8, event)
    with pytest.raises(RuntimeError, match="transition failed"):
        transport.guard_service()


@pytest.mark.asyncio
@pytest.mark.parametrize("hint", [None, 0, 19, (1 << 64) - 1])
async def test_restore_retraction_hint_is_exact_and_not_an_allocator(hint):
    transport = object.__new__(GrpcTransport)
    transport._callback_entry_supported = {}
    transport._event_queues = {}
    transport._event_sinks = {}
    transport._stream_tasks = {}
    transport._identity_bindings = {}
    transport._callback_epochs = {}
    transport.refresh_fom = AsyncMock()
    fed = Federate(connection=object(), transport=transport, handle=8, name="saved")
    assert fed.next_retraction_handle is None
    event = FederationRestored("s", 2, 1, next_retraction_handle=hint)
    await transport._apply_restore_terminal(8, event)
    assert fed.next_retraction_handle == hint
    assert fed.next_retraction_handle == hint


@pytest.mark.asyncio
async def test_failed_restore_does_not_publish_retraction_hint():
    transport = object.__new__(GrpcTransport)
    transport._callback_entry_supported = {}
    transport._event_queues = {}
    transport._identity_bindings = {}
    transport._callback_epochs = {}
    transport.refresh_fom = AsyncMock(side_effect=RuntimeError("refresh failed"))
    fed = Federate(connection=object(), transport=transport, handle=8, name="saved")
    with pytest.raises(RuntimeError, match="refresh failed"):
        await transport._apply_restore_terminal(
            8, FederationRestored("s", 2, 1, next_retraction_handle=19)
        )
    assert fed.next_retraction_handle is None


@pytest.mark.parametrize("hint", [None, 0, 19, (1 << 64) - 1])
def test_wire_restore_retraction_hint_preserves_presence(hint):
    from rti1516e._transport import _ensure_generated_path

    _ensure_generated_path()
    from rti.v1 import stream_pb2

    wire = stream_pb2.FederateEvent()
    wire.restore_completed.label = "s"
    wire.restore_completed.federate_handle = 2
    wire.restore_completed.callback_epoch = 1
    if hint is not None:
        wire.restore_completed.next_retraction_handle = hint
    event = object.__new__(GrpcTransport)._translate_event(wire)
    assert event.next_retraction_handle == hint
