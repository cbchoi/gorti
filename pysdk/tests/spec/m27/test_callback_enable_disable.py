"""M27 Phase C — §10.4 callback enable/disable toggle.

Verifies that disableCallbacks buffers events instead of firing
override slots, and enableCallbacks drains the buffer back through
the dispatch path.

Drives Rti1516eAmbassador directly with a fake event stream — no
federation / rtid required.
"""

from __future__ import annotations

import asyncio
import threading
from typing import Any

import pytest

from rti1516e.events import ReceiveInteraction
from rti1516e.standard import Rti1516eAmbassador


class _PushFederate:
    def __init__(self, loop: asyncio.AbstractEventLoop) -> None:
        self._loop = loop
        self._queue: asyncio.Queue[Any] = asyncio.Queue()

    def push(self, event: Any) -> None:
        asyncio.run_coroutine_threadsafe(self._queue.put(event), self._loop)

    def events(self) -> Any:
        async def gen() -> Any:
            while True:
                yield await self._queue.get()

        return gen()


class _RecordingAmbassador(Rti1516eAmbassador):
    def __init__(self) -> None:
        super().__init__()
        self.received: list[str] = []

    def receiveInteraction(  # noqa: N802
        self, class_name: str, parameters: dict[str, Any], timestamp: float | None
    ) -> None:
        self.received.append(class_name)


def _install():  # type: ignore[no-untyped-def]
    amb = _RecordingAmbassador()
    amb._start_loop()
    loop = amb._loop_required()
    ready = threading.Event()
    fake_box: dict[str, Any] = {}

    async def _build() -> None:
        fake_box["fed"] = _PushFederate(loop)
        ready.set()

    asyncio.run_coroutine_threadsafe(_build(), loop).result()
    ready.wait()
    fake = fake_box["fed"]
    amb._federate = fake  # type: ignore[assignment]
    pump_box: dict[str, asyncio.Task[None]] = {}

    async def _start_pump() -> None:
        pump_box["task"] = asyncio.ensure_future(amb._pump_events())

    asyncio.run_coroutine_threadsafe(_start_pump(), loop).result()
    return amb, fake, pump_box["task"]


def _teardown(amb, pump):  # type: ignore[no-untyped-def]
    loop = amb._loop_required()

    async def _cancel() -> None:
        pump.cancel()
        try:
            await pump
        except BaseException:  # noqa: BLE001
            pass

    asyncio.run_coroutine_threadsafe(_cancel(), loop).result()
    amb._stop_loop()


@pytest.mark.spec
def test_spec_m27c_disable_buffers_events() -> None:
    """While disabled, events arrive but don't fire override slots."""
    amb, fake, pump = _install()
    try:
        amb.disableCallbacks()
        fake.push(ReceiveInteraction("A", {}, None))
        fake.push(ReceiveInteraction("B", {}, None))
        # Give the pump time to consume.
        amb.evokeCallback(approx_min_time=0.05)
        # Events were "fired" (counter bumped) but override didn't run.
        assert amb.received == [], f"override fired while disabled: {amb.received}"
    finally:
        _teardown(amb, pump)


@pytest.mark.spec
def test_spec_m27c_enable_drains_buffered_events() -> None:
    """enableCallbacks replays buffered events through the override slot."""
    amb, fake, pump = _install()
    try:
        amb.disableCallbacks()
        fake.push(ReceiveInteraction("A", {}, None))
        fake.push(ReceiveInteraction("B", {}, None))
        amb.evokeCallback(approx_min_time=0.05)
        assert amb.received == []

        amb.enableCallbacks()
        # The drain ran synchronously inside enableCallbacks.
        assert amb.received == ["A", "B"]
    finally:
        _teardown(amb, pump)


@pytest.mark.spec
def test_spec_m27c_re_enable_then_new_event_fires_immediately() -> None:
    """After enableCallbacks, subsequent events dispatch live."""
    amb, fake, pump = _install()
    try:
        amb.disableCallbacks()
        fake.push(ReceiveInteraction("buffered", {}, None))
        amb.evokeCallback(approx_min_time=0.05)
        amb.enableCallbacks()  # drains "buffered"

        fake.push(ReceiveInteraction("live", {}, None))
        amb.evokeCallback(approx_min_time=0.05)
        assert amb.received == ["buffered", "live"]
    finally:
        _teardown(amb, pump)


@pytest.mark.spec
def test_spec_m27c_enable_when_already_enabled_is_noop() -> None:
    """enableCallbacks while already enabled is a no-op."""
    amb, _, pump = _install()
    try:
        # Default state: enabled. Calling again must not raise.
        amb.enableCallbacks()
        amb.enableCallbacks()
    finally:
        _teardown(amb, pump)


@pytest.mark.spec
def test_spec_m27c_counter_consistency_across_toggle() -> None:
    """_callback_fired_count is bumped exactly once per event regardless
    of whether the event was buffered or dispatched directly."""
    amb, fake, pump = _install()
    try:
        start = amb._callback_fired_count

        # 2 dispatched live.
        fake.push(ReceiveInteraction("X", {}, None))
        fake.push(ReceiveInteraction("Y", {}, None))
        amb.evokeCallback(approx_min_time=0.05)

        # 2 buffered.
        amb.disableCallbacks()
        fake.push(ReceiveInteraction("Z", {}, None))
        fake.push(ReceiveInteraction("W", {}, None))
        amb.evokeCallback(approx_min_time=0.05)

        # Drain. Drain must not double-count.
        amb.enableCallbacks()

        # Total events: 4. Counter should have advanced by 4 exactly.
        assert amb._callback_fired_count == start + 4, (
            f"counter delta = {amb._callback_fired_count - start}; "
            "want 4 (2 live + 2 buffered, no double-count on drain)"
        )
    finally:
        _teardown(amb, pump)
