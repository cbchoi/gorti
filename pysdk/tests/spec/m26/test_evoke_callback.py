"""M26 Phase E — evokeCallback / evokeMultipleCallbacks (cheap variant).

Pitch-style callback evocation. gorti dispatches callbacks
HLA_IMMEDIATE-flavored from a background pump; evokeCallback gives
ported Pitch federates a sync API that returns whether a callback
fired during a time window.

These tests drive the ambassador's internal loop without joining a
real federation: we install a fake Federate whose events() yields
queued events on demand and assert that evokeCallback's bool return
tracks whether _pump_events fired a callback during the window.
"""

from __future__ import annotations

import asyncio
import contextlib
import threading
from typing import Any

import pytest

from rti1516e.events import ReceiveInteraction
from rti1516e.standard import Rti1516eAmbassador


class _PushFederate:
    """Federate stand-in whose events() yields whatever's been pushed.

    push(event) schedules an event for delivery in the SAME asyncio
    loop the ambassador's pump is running on. Tests construct the
    fake, attach it to the ambassador, start the pump task by hand,
    and use push() to drive timed event delivery.
    """

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
        self.received: list[ReceiveInteraction] = []

    def receiveInteraction(  # noqa: N802
        self, class_name: str, parameters: dict[str, Any], timestamp: float | None
    ) -> None:
        self.received.append(
            ReceiveInteraction(class_name=class_name, parameters=parameters, timestamp=timestamp)
        )


def _install_ambassador_with_fake_federate() -> tuple[
    _RecordingAmbassador, _PushFederate, asyncio.Task[None]
]:
    """Construct an ambassador, start its loop, attach a fake federate, and
    kick off the _pump_events task. Returns the trio so tests can push
    events and assert on results.
    """
    amb = _RecordingAmbassador()
    amb._start_loop()
    loop = amb._loop_required()

    # Build the fake federate ON the ambassador's loop so its queue is
    # bound to the same loop.
    ready = threading.Event()
    fake_box: dict[str, Any] = {}

    async def _build() -> None:
        fake_box["fed"] = _PushFederate(loop)
        ready.set()

    asyncio.run_coroutine_threadsafe(_build(), loop).result()
    ready.wait()
    fake = fake_box["fed"]
    amb._federate = fake

    # Start the pump as a loop-side task.
    pump_box: dict[str, asyncio.Task[None]] = {}

    async def _start_pump() -> None:
        pump_box["task"] = asyncio.ensure_future(amb._pump_events())

    asyncio.run_coroutine_threadsafe(_start_pump(), loop).result()
    return amb, fake, pump_box["task"]


def _teardown(amb: _RecordingAmbassador, pump_task: asyncio.Task[None]) -> None:
    loop = amb._loop_required()

    async def _cancel() -> None:
        pump_task.cancel()
        with contextlib.suppress(BaseException):
            await pump_task

    asyncio.run_coroutine_threadsafe(_cancel(), loop).result()
    amb._stop_loop()


@pytest.mark.spec
def test_spec_m26_evoke_callback_returns_true_when_event_arrives() -> None:
    """evokeCallback returns True if a callback fired in the window."""
    amb, fake, pump = _install_ambassador_with_fake_federate()
    try:
        # Push an event from this thread; the pump will pick it up.
        fake.push(ReceiveInteraction("Honk", {"Vol": b"\x01"}, 1.0))
        # Give the pump time to dispatch (~5ms is plenty in-process).
        fired = amb.evokeCallback(approx_min_time=0.05)
        assert fired is True
        assert len(amb.received) == 1
        assert amb.received[0].class_name == "Honk"
    finally:
        _teardown(amb, pump)


@pytest.mark.spec
def test_spec_m26_evoke_callback_returns_false_when_idle() -> None:
    """evokeCallback returns False when no event fires in the window."""
    amb, _, pump = _install_ambassador_with_fake_federate()
    try:
        fired = amb.evokeCallback(approx_min_time=0.05)
        assert fired is False
    finally:
        _teardown(amb, pump)


@pytest.mark.spec
def test_spec_m26_evoke_reports_immediate_callback_once() -> None:
    """A callback won by the immediate pump is not lost at the evoke boundary."""
    amb = _RecordingAmbassador()
    amb._start_loop()
    try:
        amb._dispatch_event(ReceiveInteraction("Early", {}, None))

        assert amb.evokeCallback(approx_min_time=0.0) is True
        assert amb.evokeCallback(approx_min_time=0.0) is False
    finally:
        amb._stop_loop()


@pytest.mark.spec
def test_spec_m26_evoke_multiple_callbacks_drains_batch() -> None:
    """evokeMultipleCallbacks reports True if any callback in the batch fires."""
    amb, fake, pump = _install_ambassador_with_fake_federate()
    try:
        fake.push(ReceiveInteraction("A", {}, None))
        fake.push(ReceiveInteraction("B", {}, None))
        fake.push(ReceiveInteraction("C", {}, None))
        fired = amb.evokeMultipleCallbacks(approx_min_time=0.05)
        assert fired is True
        # All three should have been dispatched in the 50ms window.
        assert {ev.class_name for ev in amb.received} == {"A", "B", "C"}
    finally:
        _teardown(amb, pump)


@pytest.mark.spec
def test_spec_m26_evoke_callback_window_waits_for_late_arrival() -> None:
    """If event arrives during approx_max_time window, evokeCallback returns True."""
    amb, fake, pump = _install_ambassador_with_fake_federate()
    try:
        loop = amb._loop_required()

        # Schedule an event to arrive ~30ms from now.
        async def _delayed_push() -> None:
            await asyncio.sleep(0.03)
            await fake._queue.put(ReceiveInteraction("Late", {}, None))

        asyncio.run_coroutine_threadsafe(_delayed_push(), loop)
        # min=0 (return as soon as something arrives, but wait up to 200ms).
        fired = amb.evokeCallback(approx_min_time=0.0, approx_max_time=0.2)
        assert fired is True
        assert any(ev.class_name == "Late" for ev in amb.received)
    finally:
        _teardown(amb, pump)


@pytest.mark.spec
def test_spec_m26_callback_fired_count_bumps_once_per_event() -> None:
    """The internal counter increments exactly once per recognized event."""
    amb = _RecordingAmbassador()
    start = amb._callback_fired_count
    amb._dispatch_event(ReceiveInteraction("X", {}, None))
    amb._dispatch_event(ReceiveInteraction("Y", {}, None))
    # An unrecognized payload should NOT bump.
    amb._dispatch_event(object())
    assert amb._callback_fired_count == start + 2
