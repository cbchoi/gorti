from __future__ import annotations

import asyncio
import threading

import pytest

from rti1516e._inprocess import InProcessTransport
from rti1516e.events import FederationRestored, ReceiveInteraction
from rti1516e.standard import Rti1516eAmbassador


class RecordingCallbacks(Rti1516eAmbassador):
    def __init__(self):
        super().__init__()
        self.received = []
        self.threads = []
        self.handler = None

    def receiveInteraction(self, name, parameters, timestamp):  # noqa: N802
        self.received.append(name)
        self.threads.append(threading.get_ident())
        if self.handler is not None:
            self.handler(name)


@pytest.fixture
def ambassador(request):
    model = getattr(request, "param", "HLA_EVOKED")
    url = f"memory://callback-{request.node.name}"
    InProcessTransport(url=url)
    amb = RecordingCallbacks()
    amb.connect(amb, url, callback_model=model)
    try:
        yield amb
    finally:
        amb.disconnect()


def push(amb, *names):
    async def receive():
        for name in names:
            amb._dispatch_event(ReceiveInteraction(name, {}, None))

    amb._run(receive())


def test_evoked_never_calls_outside_evoke_and_single_is_single(ambassador):
    amb = ambassador
    push(amb, "first", "second")
    assert amb.received == []
    assert amb._callback_fired_count == 0
    amb.evokeCallback(0)
    assert amb.received == ["first"]
    amb.evokeCallback(0)
    assert amb.received == ["first", "second"]
    assert amb.threads == [threading.get_ident()] * 2


def test_evoked_enable_only_enables_and_disabled_events_do_not_count(ambassador):
    amb = ambassador
    amb.disableCallbacks()
    push(amb, "first", "second")
    assert amb._callback_fired_count == 0
    assert not amb.evokeCallback(0)
    amb.enableCallbacks()
    assert amb.received == []
    amb.evokeMultipleCallbacks(0, 0.05)
    assert amb.received == ["first", "second"]


def test_evoked_callback_exception_consumes_only_failed_callback(ambassador):
    amb = ambassador

    def fail_first(name):
        if name == "first":
            raise ValueError("user callback failed")

    amb.handler = fail_first
    push(amb, "first", "second")
    with pytest.raises(ValueError, match="user callback failed"):
        amb.evokeCallback(0)
    assert amb.received == ["first"]
    amb.evokeCallback(0)
    assert amb.received == ["first", "second"]


@pytest.mark.parametrize("ambassador", ["HLA_EVOKED", "HLA_IMMEDIATE"], indirect=True)
def test_callback_can_call_sync_service_without_io_loop_deadlock(ambassador):
    amb = ambassador
    complete = threading.Event()
    result = []

    def service(_):
        result.append(amb._run(asyncio.sleep(0, result=12.5)))
        complete.set()

    amb.handler = service
    push(amb, "service")
    if not complete.is_set():
        amb.evokeCallback(0.1)
    assert complete.wait(1)
    assert result == [12.5]
    assert amb.threads[0] != amb._loop_thread.ident


@pytest.mark.parametrize("operation", ["connect", "disconnect", "join", "resign", "evoke"])
def test_callback_rejects_forbidden_lifecycle_reentrancy(ambassador, operation):
    amb = ambassador
    errors = []
    calls = {
        "connect": lambda: amb.connect(amb, "memory://unused"),
        "disconnect": amb.disconnect,
        "join": lambda: amb.joinFederationExecution("name", "fed"),
        "resign": amb.resignFederationExecution,
        "evoke": amb.evokeCallback,
    }

    def attempt(_):
        try:
            calls[operation]()
        except Exception as exc:
            errors.append(type(exc).__name__)

    amb.handler = attempt
    push(amb, "reentrant")
    amb.evokeCallback(0)
    assert errors == ["CallNotAllowedFromWithinCallback"]


def test_callback_disable_stops_current_multi_evoke_without_reordering(ambassador):
    amb = ambassador
    amb.handler = lambda name: amb.disableCallbacks() if name == "first" else None
    push(amb, "first", "second", "third")
    amb.evokeMultipleCallbacks(0, 0.05)
    assert amb.received == ["first"]
    amb.enableCallbacks()
    assert amb.received == ["first"]
    amb.evokeMultipleCallbacks(0, 0.05)
    assert amb.received == ["first", "second", "third"]


def test_restore_terminal_purges_old_evoked_epoch_at_receipt(ambassador):
    amb = ambassador
    restored = []
    amb.federationRestored = restored.append
    push(amb, "stale")
    amb._dispatch_event(FederationRestored("saved", 2, 1))
    push(amb, "current")
    assert restored == []
    assert amb.received == []
    amb.evokeMultipleCallbacks(0, 0.05)
    assert restored == ["saved"]
    assert amb.received == ["current"]


def test_disabled_callbacks_are_not_counted_before_delivery():
    amb = RecordingCallbacks()
    amb.disableCallbacks()
    amb._dispatch_event(ReceiveInteraction("held", {}, None))
    assert amb._callback_fired_count == 0
    amb.enableCallbacks()
    assert amb._callback_fired_count == 1


def test_restore_clear_invalidates_reserved_but_not_invoked_callback():
    from rti1516e._callbacks import CallbackDispatcher

    received = []
    dispatcher = CallbackDispatcher(lambda event: received.append(event) or True, "HLA_EVOKED")
    dispatcher.submit("stale", wait=False)
    with dispatcher._condition:
        reserved = dispatcher._queue.popleft()
    dispatcher.clear()
    assert not dispatcher._deliver(reserved, asynchronous=False)
    assert received == []


@pytest.mark.parametrize("ambassador", ["HLA_IMMEDIATE"], indirect=True)
def test_immediate_exception_does_not_kill_stream_or_repeat_callback(ambassador):
    amb = ambassador
    done = threading.Event()

    def handler(name):
        if name == "first":
            raise ValueError("callback error")
        done.set()

    amb.handler = handler
    push(amb, "first", "second")
    assert done.wait(1)
    with pytest.raises(ValueError, match="callback error"):
        amb.evokeCallback(0)
    amb.evokeCallback(0)
    assert amb.received == ["first", "second"]
    assert amb._callback_error is None


def test_connection_failure_cleans_loop(monkeypatch):
    from rti1516e.connection import RtiConnection

    async def fail(_):
        raise OSError("cannot connect")

    monkeypatch.setattr(RtiConnection, "__aenter__", fail)
    amb = RecordingCallbacks()
    with pytest.raises(OSError, match="cannot connect"):
        amb.connect(amb, "memory://fail", callback_model="HLA_EVOKED")
    try:
        assert amb._loop_thread is None
        assert amb._connection is None
    finally:
        amb.disconnect()


def test_disable_waits_for_inflight_callback_and_retains_next_in_order():
    from rti1516e._callbacks import CallbackDispatcher

    entered, release, disabled = threading.Event(), threading.Event(), threading.Event()
    received = []

    def invoke(event):
        received.append(event)
        if event == "first":
            entered.set()
            assert release.wait(2)
        return True

    dispatcher = CallbackDispatcher(invoke, "HLA_IMMEDIATE")
    dispatcher.submit("first", wait=False)
    assert entered.wait(1)
    # Disable is serialized with the callback currently running.
    stopper = threading.Thread(target=lambda: (dispatcher.disable(), disabled.set()))
    stopper.start()
    assert not disabled.wait(0.02)
    release.set()
    assert disabled.wait(1)
    stopper.join(1)
    dispatcher.submit("second", wait=False)
    assert received == ["first"]
    dispatcher.enable(wait=True)
    assert received == ["first", "second"]
    dispatcher.close()


def test_evoked_unknown_and_invalid_model_do_not_invoke_user_callback(ambassador):
    from rti1516e import Rti1516eAmbassador, UnsupportedCallbackModel

    ambassador._dispatch_event(object())
    assert not ambassador.evokeCallback(0)
    assert ambassador._callback_fired_count == 0
    other = Rti1516eAmbassador()
    with pytest.raises(UnsupportedCallbackModel):
        other.connect(other, "memory://unused", callback_model="invalid")
    assert other._loop is None
