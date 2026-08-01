from __future__ import annotations

import asyncio
import contextlib
import threading
import time
from collections.abc import Iterator
from typing import Any, TypeAlias, cast

import pytest

from rti1516e.standard import Rti1516eAmbassador


class _AsyncFederate:
    def __init__(self) -> None:
        self.active = 0
        self.max_active = 0
        self.events: list[str] = []
        self.send_completed = threading.Event()
        self.fail_update = False
        self.long_update = False
        self.update_release: threading.Event | None = None
        self.update_started = 0
        self.update_started_event = threading.Event()
        self.two_updates_started = threading.Event()
        self.tar_started = threading.Event()
        self.tar_release: threading.Event | None = None

    async def update_attributes(self, *_args: object, **_kwargs: object) -> None:
        self.active += 1
        self.update_started += 1
        self.update_started_event.set()
        if self.update_started >= 2:
            self.two_updates_started.set()
        self.max_active = max(self.max_active, self.active)
        try:
            if self.update_release is not None:
                await asyncio.to_thread(self.update_release.wait)
            elif self.long_update:
                await asyncio.sleep(10)
            else:
                await asyncio.sleep(0.02)
            self.events.append("update")
            if self.fail_update:
                raise RuntimeError("update failed")
        finally:
            self.active -= 1

    async def send_interaction(self, *_args: object, **_kwargs: object) -> None:
        self.active += 1
        self.max_active = max(self.max_active, self.active)
        try:
            await asyncio.sleep(0.03)
            self.events.append("interaction")
            self.send_completed.set()
        finally:
            self.active -= 1

    async def time_advance_request(self, _logical_time: float) -> None:
        self.tar_started.set()
        if self.tar_release is not None:
            await asyncio.to_thread(self.tar_release.wait)
        self.events.append("tar")

    async def next_message_request(self, _logical_time: float) -> None:
        self.events.append("nmr")

    async def next_message_request_available(self, _logical_time: float) -> None:
        self.events.append("nmra")

    async def time_advance_request_available(self, _logical_time: float) -> None:
        self.events.append("tara")

    async def flush_queue_request(self, _logical_time: float) -> None:
        self.events.append("fqr")


AsyncAmbassadorFixture: TypeAlias = tuple[Rti1516eAmbassador, _AsyncFederate]


@pytest.fixture
def async_ambassador() -> Iterator[AsyncAmbassadorFixture]:
    ambassador = Rti1516eAmbassador()
    federate = _AsyncFederate()
    ambassador._start_loop()
    ambassador._federate = cast(Any, federate)
    yield ambassador, federate
    with contextlib.suppress(BaseException):
        ambassador.flushAsyncOperations()
    ambassador._federate = None
    ambassador._stop_loop()


def test_async_om_submissions_execute_concurrently(
    async_ambassador: AsyncAmbassadorFixture,
) -> None:
    ambassador, federate = async_ambassador

    update = ambassador.updateAttributeValuesAsync(1, {1: b"a"}, timestamp=1.0)
    interaction = ambassador.sendInteractionAsync(1, {1: b"b"}, timestamp=1.0)
    ambassador.flushAsyncOperations()

    assert update.done()
    assert interaction.done()
    assert federate.max_active == 2
    assert set(federate.events) == {"update", "interaction"}


def test_flush_observes_all_operations_before_raising_first_error(
    async_ambassador: AsyncAmbassadorFixture,
) -> None:
    ambassador, federate = async_ambassador
    federate.fail_update = True
    ambassador.updateAttributeValuesAsync(1, {1: b"a"})
    ambassador.sendInteractionAsync(1, {1: b"b"})

    with pytest.raises(RuntimeError, match="update failed"):
        ambassador.flushAsyncOperations()

    assert federate.send_completed.is_set()


@pytest.mark.parametrize(
    ("method_name", "expected_event"),
    [
        ("nextMessageRequest", "nmr"),
        ("nextMessageRequestAvailable", "nmra"),
        ("timeAdvanceRequest", "tar"),
        ("timeAdvanceRequestAvailable", "tara"),
        ("flushQueueRequest", "fqr"),
    ],
)
def test_time_advance_primitives_flush_timestamped_om_before_advancing(
    async_ambassador: AsyncAmbassadorFixture,
    method_name: str,
    expected_event: str,
) -> None:
    ambassador, federate = async_ambassador
    ambassador.updateAttributeValuesAsync(1, {1: b"a"}, timestamp=1.0)

    getattr(ambassador, method_name)(1.0)

    assert federate.events == ["update", expected_event]


def test_async_tar_returns_future_and_does_not_block_caller(
    async_ambassador: AsyncAmbassadorFixture,
) -> None:
    ambassador, federate = async_ambassador
    release = threading.Event()
    federate.tar_release = release

    future = ambassador.timeAdvanceRequestAsync(1.0)

    assert federate.tar_started.wait(timeout=1.0)
    assert not future.done()
    release.set()
    ambassador.flushAsyncOperations()
    assert future.done()
    assert federate.events == ["tar"]


def test_async_tar_drains_prior_om_and_orders_later_om(
    async_ambassador: AsyncAmbassadorFixture,
) -> None:
    ambassador, federate = async_ambassador
    update_release = threading.Event()
    federate.update_release = update_release
    ambassador.updateAttributeValuesAsync(1, {1: b"before"})
    assert federate.update_started_event.wait(timeout=1.0)

    tar_submitted = threading.Event()

    def submit_tar() -> None:
        ambassador.timeAdvanceRequestAsync(1.0)
        tar_submitted.set()

    tar_thread = threading.Thread(target=submit_tar)
    tar_thread.start()
    assert not tar_submitted.wait(timeout=0.05)
    update_release.set()
    tar_thread.join(timeout=1.0)
    assert tar_submitted.is_set()

    federate.update_release = None
    ambassador.updateAttributeValuesAsync(1, {1: b"after"})
    ambassador.flushAsyncOperations()

    assert federate.events == ["update", "tar", "update"]


def test_async_operation_limit_applies_backpressure(
    async_ambassador: AsyncAmbassadorFixture,
) -> None:
    ambassador, federate = async_ambassador
    ambassador.setAsyncOperationLimit(1)
    ambassador.updateAttributeValuesAsync(1, {1: b"a"})

    started = time.perf_counter()
    ambassador.sendInteractionAsync(1, {1: b"b"})
    blocked_for = time.perf_counter() - started
    ambassador.flushAsyncOperations()

    assert blocked_for >= 0.01
    assert federate.max_active == 1


def test_cancelled_observer_does_not_cancel_mutating_rpc(
    async_ambassador: AsyncAmbassadorFixture,
) -> None:
    ambassador, federate = async_ambassador
    future = ambassador.updateAttributeValuesAsync(1, {1: b"a"})
    assert future.cancel()

    ambassador.flushAsyncOperations()

    assert future.cancelled()
    assert federate.events == ["update"]


def test_inflight_limit_holds_while_another_thread_flushes(
    async_ambassador: AsyncAmbassadorFixture,
) -> None:
    ambassador, federate = async_ambassador
    ambassador.setAsyncOperationLimit(2)
    release = threading.Event()
    federate.update_release = release
    ambassador.updateAttributeValuesAsync(1, {1: b"a"})
    ambassador.updateAttributeValuesAsync(1, {1: b"b"})
    assert federate.two_updates_started.wait(timeout=1.0)

    flush_started = threading.Event()
    flush_done = threading.Event()

    def flush() -> None:
        flush_started.set()
        ambassador.flushAsyncOperations()
        flush_done.set()

    flush_thread = threading.Thread(target=flush)
    flush_thread.start()
    assert flush_started.wait(timeout=1.0)

    submitted = threading.Event()

    def submit_third() -> None:
        ambassador.updateAttributeValuesAsync(1, {1: b"c"})
        submitted.set()

    submit_thread = threading.Thread(target=submit_third)
    submit_thread.start()
    assert not submitted.wait(timeout=0.05)

    release.set()
    flush_thread.join(timeout=1.0)
    submit_thread.join(timeout=1.0)
    ambassador.flushAsyncOperations()

    assert flush_done.is_set()
    assert submitted.is_set()
    assert federate.max_active <= 2


def test_limit_change_requires_empty_generation(
    async_ambassador: AsyncAmbassadorFixture,
) -> None:
    ambassador, _ = async_ambassador
    ambassador.updateAttributeValuesAsync(1, {1: b"a"})

    with pytest.raises(RuntimeError, match="empty generation"):
        ambassador.setAsyncOperationLimit(2)


def test_returned_future_and_flush_preserve_operation_exception(
    async_ambassador: AsyncAmbassadorFixture,
) -> None:
    ambassador, federate = async_ambassador
    federate.fail_update = True
    future = ambassador.updateAttributeValuesAsync(1, {1: b"a"})

    with pytest.raises(RuntimeError, match="update failed"):
        future.result(timeout=1.0)
    with pytest.raises(RuntimeError, match="update failed"):
        ambassador.flushAsyncOperations()


def test_disconnect_drains_pending_and_rejects_racing_submission(
    async_ambassador: AsyncAmbassadorFixture,
) -> None:
    ambassador, federate = async_ambassador
    release = threading.Event()
    federate.update_release = release
    accepted = ambassador.updateAttributeValuesAsync(1, {1: b"a"})
    assert federate.update_started_event.wait(timeout=1.0)

    disconnect_done = threading.Event()

    def disconnect() -> None:
        ambassador.disconnect()
        disconnect_done.set()

    disconnect_thread = threading.Thread(target=disconnect)
    disconnect_thread.start()
    deadline = time.monotonic() + 1.0
    while not ambassador._async_closing and time.monotonic() < deadline:
        time.sleep(0.001)
    assert ambassador._async_closing

    rejected: list[BaseException] = []

    def submit_during_disconnect() -> None:
        try:
            ambassador.sendInteractionAsync(1, {1: b"b"})
        except BaseException as exc:
            rejected.append(exc)

    submit_thread = threading.Thread(target=submit_during_disconnect)
    submit_thread.start()
    release.set()
    disconnect_thread.join(timeout=1.0)
    submit_thread.join(timeout=1.0)

    assert accepted.done()
    assert disconnect_done.is_set()
    assert rejected and isinstance(rejected[0], RuntimeError)
    assert ambassador._loop is None
    ambassador.disconnect()


@pytest.mark.parametrize("limit", [0, -1, True, 1.5])
def test_async_operation_limit_must_be_positive_integer(
    async_ambassador: AsyncAmbassadorFixture, limit: object
) -> None:
    ambassador, _ = async_ambassador
    with pytest.raises(ValueError, match="positive integer"):
        ambassador.setAsyncOperationLimit(limit)  # type: ignore[arg-type]


def test_direct_callback_delivery_must_be_selected_before_join() -> None:
    ambassador = Rti1516eAmbassador()
    ambassador.setDirectCallbackDelivery(True)
    assert ambassador._direct_callback_delivery is True

    with pytest.raises(TypeError, match="requires a bool"):
        ambassador.setDirectCallbackDelivery(1)  # type: ignore[arg-type]

    ambassador._federate = cast(Any, _AsyncFederate())
    with pytest.raises(RuntimeError, match="before joining"):
        ambassador.setDirectCallbackDelivery(False)
