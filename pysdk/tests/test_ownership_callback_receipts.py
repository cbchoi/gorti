import asyncio
from contextlib import asynccontextmanager
from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest

from rti1516e._ownership_callbacks import OwnershipCallbacks
from rti1516e._services import FederateServices
from rti1516e.connection import Federate
from rti1516e.events import (
    AttributeOwnershipUnavailable,
    DiscoverObjectInstance,
    RequestAttributeOwnershipAssumption,
)


def receipt(attempt=1):
    return SimpleNamespace(
        federation_generation=4, callback_epoch=0, callback_id=1, delivery_attempt=attempt
    )


def test_epoch_fence_keeps_new_discovery_and_terminal():
    state = OwnershipCallbacks()
    state.retire(8, 10)
    old = RequestAttributeOwnershipAssumption(8, (1,), 2, b"")
    object.__setattr__(old, "_object_knowledge_epoch", 10)
    assert not state.begin(old)
    new = DiscoverObjectInstance(8, "class", "object")
    object.__setattr__(new, "_object_knowledge_epoch", 11)
    state.observe(new)
    assert state.begin(new)
    assert state.begin(AttributeOwnershipUnavailable(8, (1,)))


def test_actual_invocation_failure_retry_does_not_reinvoke():
    class Client(FederateServices):
        pass

    client = Client()
    client._transport = SimpleNamespace()
    client._ownership_callback_rpc = AsyncMock(side_effect=RuntimeError("unavailable"))
    state = client._ownership_callback_state()
    state.enabled = True
    state.scope = (4, 0)
    event = AttributeOwnershipUnavailable(8, (1,))
    object.__setattr__(event, "_ownership_receipt", receipt())
    invoked = []

    async def run():
        with pytest.raises(RuntimeError):
            await client.dispatch_event(event, lambda e: invoked.append(e))
        assert len(invoked) == 1 and len(client._ownership_callback_state().pending()) == 1
        retry = AttributeOwnershipUnavailable(8, (1,))
        object.__setattr__(retry, "_ownership_receipt", receipt(2))
        client._ownership_callback_rpc.side_effect = None
        await client.dispatch_event(retry, lambda e: invoked.append(e))
        assert len(invoked) == 1
        assert client._ownership_callback_state().pending() == []
        assert client._ownership_callback_rpc.await_args.args[1].delivery_attempt == 2

    asyncio.run(run())


def test_separate_task_local_delete_waits_for_dispatch_then_filters_buffered_advice():
    async def run():
        @asynccontextmanager
        async def admit():
            yield

        entered, release = asyncio.Event(), asyncio.Event()
        order = []
        transport = SimpleNamespace()
        client = Federate(
            connection=SimpleNamespace(_admit_work=admit),
            transport=transport,
            handle=1,
            name="callback",
        )
        client._guard_service = lambda: None
        state = client._ownership_callback_state()

        async def record(method, **kwargs):
            assert method == "local_delete_object_instance"
            state.retire(kwargs["object_handle"], 2)
            order.append("delete")

        transport.record = record
        event = RequestAttributeOwnershipAssumption(8, (1,), 2, b"")
        object.__setattr__(event, "_object_knowledge_epoch", 2)

        async def callback(_):
            order.append("entered")
            entered.set()
            await release.wait()
            order.append("returned")

        handler = asyncio.create_task(client.dispatch_event(event, callback))
        await entered.wait()
        deletion = asyncio.create_task(client.local_delete_object_instance(8))
        await asyncio.sleep(0)
        assert not deletion.done() and order == ["entered"]
        stale = asyncio.create_task(client.dispatch_event(event, lambda _: order.append("stale")))
        release.set()
        await asyncio.wait_for(asyncio.gather(handler, deletion, stale), 1)
        assert order == ["entered", "returned", "delete"]
        assert state.active_task is None

    asyncio.run(run())


def test_same_task_callback_local_delete_is_reentrant():
    async def run():
        @asynccontextmanager
        async def admit():
            yield

        transport = SimpleNamespace(record=AsyncMock())
        client = Federate(
            connection=SimpleNamespace(_admit_work=admit),
            transport=transport,
            handle=1,
            name="callback",
        )
        client._guard_service = lambda: None
        event = RequestAttributeOwnershipAssumption(8, (1,), 2, b"")

        async def callback(_):
            await client.local_delete_object_instance(8)

        await asyncio.wait_for(client.dispatch_event(event, callback), 1)
        transport.record.assert_awaited_once_with(
            "local_delete_object_instance", federate_handle=1, object_handle=8
        )

    asyncio.run(run())
