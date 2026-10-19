"""SDK Layer 1 — connection + join_federation lifecycle.

Drives RtiConnection through FakeRtiServer. Asserts:
  - connect / close are async-context-manager safe
  - join_federation records the right call args
  - resign on context exit fires
  - typed exceptions surface from canned error responses

Implements: IR-PYAPI-1.
"""

from __future__ import annotations

import asyncio

import pytest

import rti1516e.connection as connection_module
from rti1516e import (
    FederationJoinCommitUnknownError,
    FederationSpec,
    RtiConnection,
    RtiConnectionPoisonedError,
)
from rti1516e.errors import FederationAlreadyExists


@pytest.mark.spec
@pytest.mark.asyncio
async def test_spec_m4_connect_close_roundtrip(fake_rti) -> None:  # type: ignore[no-untyped-def]
    # The exact transport-injection mechanism is an implementation detail.
    # If the SDK uses connect_with_transport(srv) for testing, this
    # test will use it. Otherwise this test will fail at the
    # NotImplementedError + (later) AttributeError boundary, signaling
    # the SDK design needs revision.
    async with RtiConnection.connect("memory://fake-rti") as rti:
        assert rti is not None


@pytest.mark.spec
@pytest.mark.asyncio
async def test_close_waits_for_async_open_and_closes_late_transport(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    build_started = asyncio.Event()
    release_build = asyncio.Event()
    close_calls = 0

    class LateTransport:
        async def close(self) -> None:
            nonlocal close_calls
            close_calls += 1

    async def slow_build(*args: object, **kwargs: object) -> LateTransport:
        del args, kwargs
        build_started.set()
        await release_build.wait()
        return LateTransport()

    monkeypatch.setattr(connection_module, "_build_grpc_transport", slow_build)
    connection = RtiConnection.connect("grpc://late-open")
    opening = asyncio.create_task(connection.__aenter__())
    await build_started.wait()
    closing = asyncio.create_task(connection.close())
    await asyncio.sleep(0)
    assert not closing.done()

    release_build.set()
    with pytest.raises(RuntimeError, match="closed while opening"):
        await opening
    await closing
    assert close_calls == 1
    assert connection._transport is None  # noqa: SLF001
    assert connection._closed is True  # noqa: SLF001


@pytest.mark.spec
@pytest.mark.asyncio
async def test_repeated_open_cancellation_cannot_strand_admission_lease(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    build_started = asyncio.Event()
    release_build = asyncio.Event()
    close_calls = 0

    class LateTransport:
        async def close(self) -> None:
            nonlocal close_calls
            close_calls += 1

    async def slow_build(*args: object, **kwargs: object) -> LateTransport:
        del args, kwargs
        build_started.set()
        await release_build.wait()
        return LateTransport()

    monkeypatch.setattr(connection_module, "_build_grpc_transport", slow_build)
    connection = RtiConnection.connect("grpc://cancelled-open")
    opening = asyncio.create_task(connection.__aenter__())
    await build_started.wait()

    # Hold the exact Condition seam used by the open-finally bookkeeping.
    await connection._admission_condition.acquire()  # noqa: SLF001
    release_build.set()
    for _ in range(10):
        if connection._transport is not None:  # noqa: SLF001
            break
        await asyncio.sleep(0)
    assert connection._transport is not None  # noqa: SLF001

    opening.cancel()
    await asyncio.sleep(0)
    opening.cancel()
    closing = asyncio.create_task(connection.close())
    await asyncio.sleep(0)
    assert not closing.done()

    connection._admission_condition.release()  # noqa: SLF001
    with pytest.raises(asyncio.CancelledError):
        await opening
    await asyncio.wait_for(closing, timeout=1.0)

    assert connection._opening is False  # noqa: SLF001
    assert connection._inflight_operations == 0  # noqa: SLF001
    assert connection._closed is True  # noqa: SLF001
    assert close_calls == 1


@pytest.mark.spec
@pytest.mark.asyncio
async def test_cancelled_open_compensates_close_without_context_exit(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    build_started = asyncio.Event()
    release_build = asyncio.Event()
    close_calls = 0

    class PublishedTransport:
        async def close(self) -> None:
            nonlocal close_calls
            close_calls += 1

    async def controlled_build(
        *args: object, **kwargs: object
    ) -> PublishedTransport:
        del args, kwargs
        build_started.set()
        await release_build.wait()
        return PublishedTransport()

    monkeypatch.setattr(connection_module, "_build_grpc_transport", controlled_build)
    connection = RtiConnection.connect("grpc://cancelled-temporary-enter")
    opening = asyncio.create_task(connection.__aenter__())
    await build_started.wait()
    await connection._admission_condition.acquire()  # noqa: SLF001
    release_build.set()
    for _ in range(10):
        if connection._transport is not None:  # noqa: SLF001
            break
        await asyncio.sleep(0)
    opening.cancel("first open cancellation")
    await asyncio.sleep(0)
    opening.cancel("second open cancellation")
    connection._admission_condition.release()  # noqa: SLF001

    with pytest.raises(asyncio.CancelledError) as cancelled:
        await opening
    assert cancelled.value.args == ("first open cancellation",)
    assert close_calls == 1
    assert connection._closed is True  # noqa: SLF001
    assert connection._transport is None  # noqa: SLF001


@pytest.mark.spec
@pytest.mark.asyncio
async def test_cancelled_open_reports_compensating_close_failure(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    build_started = asyncio.Event()
    release_build = asyncio.Event()

    class PublishedTransport:
        async def close(self) -> None:
            raise RuntimeError("compensating close failed")

    async def controlled_build(
        *args: object, **kwargs: object
    ) -> PublishedTransport:
        del args, kwargs
        build_started.set()
        await release_build.wait()
        return PublishedTransport()

    monkeypatch.setattr(connection_module, "_build_grpc_transport", controlled_build)
    connection = RtiConnection.connect("grpc://failed-open-compensation")
    opening = asyncio.create_task(connection.__aenter__())
    await build_started.wait()
    await connection._admission_condition.acquire()  # noqa: SLF001
    release_build.set()
    for _ in range(10):
        if connection._transport is not None:  # noqa: SLF001
            break
        await asyncio.sleep(0)
    opening.cancel("open cancelled")
    connection._admission_condition.release()  # noqa: SLF001

    with pytest.raises(BaseExceptionGroup) as grouped:
        await opening
    assert grouped.value.subgroup(asyncio.CancelledError) is not None
    close_failures = grouped.value.subgroup(RuntimeError)
    assert close_failures is not None
    assert any(
        str(error) == "compensating close failed"
        for error in close_failures.exceptions
    )
    assert connection._transport is not None  # noqa: SLF001
    assert connection._closed is False  # noqa: SLF001


@pytest.mark.spec
@pytest.mark.asyncio
async def test_spec_m4_join_records_call(fake_rti) -> None:  # type: ignore[no-untyped-def]
    spec = FederationSpec(name="demo", fom_modules=["./demo.fom.xml"])
    async with RtiConnection.connect("memory://fake-rti") as rti:
        async with rti.join_federation(spec, federate_name="alice") as fed:
            assert fed.name == "alice"


@pytest.mark.spec
@pytest.mark.asyncio
async def test_cancel_after_join_registration_seam_compensates_with_resign(
    fake_rti,
) -> None:  # type: ignore[no-untyped-def]
    connection = await RtiConnection.connect("memory://fake-rti").__aenter__()
    context = connection.join_federation(
        FederationSpec(name="cancelled-registration"), federate_name="alice"
    )
    original_register = connection._register_federate_context  # noqa: SLF001
    registration_started = asyncio.Event()
    release_registration = asyncio.Event()

    async def blocked_registration(candidate: object) -> None:
        registration_started.set()
        await release_registration.wait()
        await original_register(candidate)  # type: ignore[arg-type]

    connection._register_federate_context = blocked_registration  # type: ignore[method-assign]  # noqa: SLF001
    entering = asyncio.create_task(context.__aenter__())
    await registration_started.wait()
    entering.cancel()
    await asyncio.sleep(0)
    entering.cancel()
    release_registration.set()

    with pytest.raises(asyncio.CancelledError):
        await entering

    assert len(fake_rti.calls_for("join_federation")) == 1
    assert len(fake_rti.calls_for("resign_federation")) == 1
    assert context._federate is None  # noqa: SLF001
    assert connection._active_federate_contexts == set()  # noqa: SLF001
    await connection.close()


@pytest.mark.spec
@pytest.mark.asyncio
async def test_cancel_after_join_commit_before_response_compensates_with_resign(
    fake_rti,
) -> None:  # type: ignore[no-untyped-def]
    connection = await RtiConnection.connect("memory://fake-rti").__aenter__()
    context = connection.join_federation(
        FederationSpec(name="cancelled-join-response"), federate_name="alice"
    )
    original_record = fake_rti.record
    join_committed = asyncio.Event()
    release_response = asyncio.Event()

    async def commit_then_block_response(
        method: str, **kwargs: object
    ) -> object:
        result = original_record(method, **kwargs)
        if method == "join_federation":
            join_committed.set()
            await release_response.wait()
        return result

    fake_rti.record = commit_then_block_response
    entering = asyncio.create_task(context.__aenter__())
    await join_committed.wait()
    entering.cancel()
    await asyncio.sleep(0)
    entering.cancel()
    release_response.set()

    with pytest.raises(asyncio.CancelledError):
        await entering

    assert len(fake_rti.calls_for("join_federation")) == 1
    assert len(fake_rti.calls_for("resign_federation")) == 1
    assert context._federate is None  # noqa: SLF001
    assert connection._active_federate_contexts == set()  # noqa: SLF001
    await connection.close()


@pytest.mark.spec
@pytest.mark.asyncio
async def test_spec_m4_join_existing_raises_typed_exception(fake_rti) -> None:  # type: ignore[no-untyped-def]
    """If create_federation returns ERR_FED_ALREADY_EXISTS, the SDK must
    raise FederationAlreadyExists (not a generic gRPC error)."""
    fake_rti.canned_responses["create_federation"] = FederationAlreadyExists("demo")
    with pytest.raises(FederationAlreadyExists):
        async with RtiConnection.connect("memory://fake-rti") as rti:
            spec = FederationSpec(name="demo")
            async with rti.join_federation(spec, federate_name="alice"):
                pass


@pytest.mark.spec
@pytest.mark.asyncio
async def test_connection_close_failure_remains_retryable(
    fake_rti,
) -> None:  # type: ignore[no-untyped-def]
    close_attempts = 0

    async def flaky_close() -> None:
        nonlocal close_attempts
        close_attempts += 1
        if close_attempts == 1:
            raise RuntimeError("transport close failed")

    fake_rti.close = flaky_close
    connection = await RtiConnection.connect("memory://fake-rti").__aenter__()

    with pytest.raises(RuntimeError, match="transport close failed"):
        await connection.__aexit__(None, None, None)
    assert connection._transport is fake_rti  # noqa: SLF001
    assert connection._closed is False  # noqa: SLF001

    await connection.__aexit__(None, None, None)
    assert close_attempts == 2
    assert connection._transport is None  # noqa: SLF001
    assert connection._closed is True  # noqa: SLF001


@pytest.mark.spec
@pytest.mark.asyncio
async def test_concurrent_connection_close_is_shared_and_blocks_new_work(
    fake_rti,
) -> None:  # type: ignore[no-untyped-def]
    started = asyncio.Event()
    release = asyncio.Event()
    close_attempts = 0

    async def slow_close() -> None:
        nonlocal close_attempts
        close_attempts += 1
        started.set()
        await release.wait()

    fake_rti.close = slow_close
    connection = await RtiConnection.connect("memory://fake-rti").__aenter__()
    first = asyncio.create_task(connection.close())
    await started.wait()
    second = asyncio.create_task(connection.close())
    await asyncio.sleep(0)

    with pytest.raises(RuntimeError, match="closing or closed"):
        await connection.create_federation(FederationSpec(name="late"))
    with pytest.raises(RuntimeError, match="closing or closed"):
        connection.join_federation(
            FederationSpec(name="late"), federate_name="late"
        )

    release.set()
    await asyncio.gather(first, second)
    assert close_attempts == 1


@pytest.mark.spec
@pytest.mark.asyncio
async def test_concurrent_connection_close_failure_is_shared_then_retryable(
    fake_rti,
) -> None:  # type: ignore[no-untyped-def]
    started = asyncio.Event()
    release = asyncio.Event()
    close_attempts = 0

    async def flaky_close() -> None:
        nonlocal close_attempts
        close_attempts += 1
        if close_attempts == 1:
            started.set()
            await release.wait()
            raise RuntimeError("shared close failure")

    fake_rti.close = flaky_close
    connection = await RtiConnection.connect("memory://fake-rti").__aenter__()
    first = asyncio.create_task(connection.close())
    await started.wait()
    second = asyncio.create_task(connection.close())
    await asyncio.sleep(0)
    release.set()

    results = await asyncio.gather(first, second, return_exceptions=True)
    assert all(
        isinstance(result, RuntimeError)
        and str(result) == "shared close failure"
        for result in results
    )
    assert close_attempts == 1
    assert connection._transport is fake_rti  # noqa: SLF001

    await connection.close()
    assert close_attempts == 2
    assert connection._closed is True  # noqa: SLF001


@pytest.mark.spec
@pytest.mark.asyncio
async def test_connection_close_drains_admitted_work_before_transport_close(
    fake_rti,
) -> None:  # type: ignore[no-untyped-def]
    original_record = fake_rti.record
    operation_started = asyncio.Event()
    release_operation = asyncio.Event()
    transport_closed = asyncio.Event()

    async def blocking_record(method: str, **kwargs: object) -> object:
        spec = kwargs.get("spec")
        if (
            method == "create_federation"
            and isinstance(spec, FederationSpec)
            and spec.name == "inflight"
        ):
            operation_started.set()
            await release_operation.wait()
        return original_record(method, **kwargs)

    async def close_transport() -> None:
        transport_closed.set()

    fake_rti.record = blocking_record
    fake_rti.close = close_transport
    connection = await RtiConnection.connect("memory://fake-rti").__aenter__()
    admitted = asyncio.create_task(
        connection.create_federation(FederationSpec(name="inflight"))
    )
    await operation_started.wait()
    closing = asyncio.create_task(connection.close())
    await asyncio.sleep(0)

    assert not closing.done()
    assert not transport_closed.is_set()
    with pytest.raises(RuntimeError, match="closing or closed"):
        await connection.create_federation(FederationSpec(name="late"))

    release_operation.set()
    await asyncio.gather(admitted, closing)
    assert transport_closed.is_set()


@pytest.mark.spec
@pytest.mark.asyncio
async def test_connection_close_drains_federate_service_operation(
    fake_rti,
) -> None:  # type: ignore[no-untyped-def]
    connection = await RtiConnection.connect("memory://fake-rti").__aenter__()
    context = connection.join_federation(
        FederationSpec(name="service-drain"), federate_name="alice"
    )
    federate = await context.__aenter__()
    original_record = fake_rti.record
    operation_started = asyncio.Event()
    release_operation = asyncio.Event()
    transport_closed = asyncio.Event()

    async def blocking_record(method: str, **kwargs: object) -> object:
        if method == "publish_object_class":
            operation_started.set()
            await release_operation.wait()
        return original_record(method, **kwargs)

    async def close_transport() -> None:
        transport_closed.set()

    fake_rti.record = blocking_record
    fake_rti.close = close_transport
    admitted = asyncio.create_task(
        federate.publish_object_class("Vehicle", attributes=["position"])
    )
    await operation_started.wait()
    closing = asyncio.create_task(connection.close())
    await asyncio.sleep(0)

    assert not closing.done()
    with pytest.raises(RuntimeError, match="closing or closed"):
        await federate.subscribe_object_class(
            "Vehicle", attributes=["position"]
        )

    release_operation.set()
    await asyncio.gather(admitted, closing)
    assert transport_closed.is_set()


@pytest.mark.spec
@pytest.mark.asyncio
async def test_federate_resign_failure_keeps_real_connection_for_retry(
    fake_rti,
) -> None:  # type: ignore[no-untyped-def]
    connection = await RtiConnection.connect("memory://fake-rti").__aenter__()
    context = connection.join_federation(
        FederationSpec(name="retry-demo"), federate_name="alice"
    )
    await context.__aenter__()
    fake_rti.canned_responses["resign_federation"] = RuntimeError(
        "resign failed"
    )

    with pytest.raises(RuntimeError, match="resign failed"):
        await context.__aexit__(None, None, None)
    assert connection.transport is fake_rti

    del fake_rti.canned_responses["resign_federation"]
    await context.__aexit__(None, None, None)
    assert len(fake_rti.calls_for("resign_federation")) == 2
    await connection.close()


@pytest.mark.spec
@pytest.mark.asyncio
async def test_concurrent_federate_exit_is_shared(
    fake_rti,
) -> None:  # type: ignore[no-untyped-def]
    connection = await RtiConnection.connect("memory://fake-rti").__aenter__()
    context = connection.join_federation(
        FederationSpec(name="shared-resign"), federate_name="alice"
    )
    await context.__aenter__()
    original_record = fake_rti.record
    started = asyncio.Event()
    release = asyncio.Event()
    resign_attempts = 0

    async def blocking_record(method: str, **kwargs: object) -> object:
        nonlocal resign_attempts
        if method == "resign_federation":
            resign_attempts += 1
            started.set()
            await release.wait()
        return original_record(method, **kwargs)

    fake_rti.record = blocking_record
    first = asyncio.create_task(context.__aexit__(None, None, None))
    await started.wait()
    second = asyncio.create_task(context.__aexit__(None, None, None))
    await asyncio.sleep(0)
    release.set()
    await asyncio.gather(first, second)

    assert resign_attempts == 1
    await connection.close()


@pytest.mark.spec
@pytest.mark.asyncio
async def test_connection_close_drains_resign_started_before_shutdown(
    fake_rti,
) -> None:  # type: ignore[no-untyped-def]
    connection = await RtiConnection.connect("memory://fake-rti").__aenter__()
    context = connection.join_federation(
        FederationSpec(name="resign-drain"), federate_name="alice"
    )
    await context.__aenter__()
    original_record = fake_rti.record
    resign_started = asyncio.Event()
    release_resign = asyncio.Event()
    order: list[str] = []
    resign_attempts = 0
    close_attempts = 0

    async def blocking_record(method: str, **kwargs: object) -> object:
        nonlocal resign_attempts
        if method == "resign_federation":
            resign_attempts += 1
            resign_started.set()
            await release_resign.wait()
            order.append("resign")
        return original_record(method, **kwargs)

    async def close_transport() -> None:
        nonlocal close_attempts
        close_attempts += 1
        order.append("close")

    fake_rti.record = blocking_record
    fake_rti.close = close_transport
    first_exit = asyncio.create_task(context.__aexit__(None, None, None))
    await resign_started.wait()
    second_exit = asyncio.create_task(context.__aexit__(None, None, None))
    closing = asyncio.create_task(connection.close())
    await asyncio.sleep(0)

    assert not closing.done()
    assert close_attempts == 0
    release_resign.set()
    await asyncio.gather(first_exit, second_exit, closing)
    assert order == ["resign", "close"]
    assert resign_attempts == 1
    assert close_attempts == 1


@pytest.mark.spec
@pytest.mark.asyncio
async def test_connection_close_cancels_blocked_event_waiter_without_deadlock(
    fake_rti,
) -> None:  # type: ignore[no-untyped-def]
    connection = await RtiConnection.connect("memory://fake-rti").__aenter__()
    context = connection.join_federation(
        FederationSpec(name="event-close"), federate_name="alice"
    )
    federate = await context.__aenter__()
    stream = federate.events()
    consumer = asyncio.create_task(anext(stream))
    for _ in range(10):
        if connection._event_waiters:  # noqa: SLF001
            break
        await asyncio.sleep(0)
    assert connection._event_waiters == {consumer}  # noqa: SLF001

    await asyncio.wait_for(connection.close(), timeout=1.0)
    with pytest.raises(asyncio.CancelledError):
        await consumer
    assert consumer.cancelled()
    assert connection._inflight_operations == 0  # noqa: SLF001
    assert len(fake_rti.calls_for("resign_federation")) == 1
    assert connection._active_federate_contexts == set()  # noqa: SLF001


@pytest.mark.spec
@pytest.mark.asyncio
@pytest.mark.parametrize("lease_kind", ["normal", "shutdown"])
async def test_repeated_cancellation_cannot_strand_admission_release(
    lease_kind: str,
) -> None:
    connection = RtiConnection("memory://retained-release")
    connection._transport = object()  # noqa: SLF001
    started = asyncio.Event()

    async def admitted() -> None:
        lease = (
            connection._admit_work()  # noqa: SLF001
            if lease_kind == "normal"
            else connection._admit_shutdown_work()  # noqa: SLF001
        )
        async with lease:
            started.set()
            await asyncio.Event().wait()

    operation = asyncio.create_task(admitted())
    await started.wait()
    await connection._admission_condition.acquire()  # noqa: SLF001
    operation.cancel("first cancellation")
    await asyncio.sleep(0)
    operation.cancel("second cancellation")
    await asyncio.sleep(0)
    connection._admission_condition.release()  # noqa: SLF001

    with pytest.raises(asyncio.CancelledError) as cancelled:
        await operation
    assert cancelled.value.args == ("first cancellation",)
    assert connection._inflight_operations == 0  # noqa: SLF001
    await asyncio.wait_for(connection.close(), timeout=1.0)


@pytest.mark.spec
@pytest.mark.asyncio
async def test_repeated_cancellation_cannot_strand_event_wait_release() -> None:
    connection = RtiConnection("memory://retained-event-release")
    connection._transport = object()  # noqa: SLF001
    operation = asyncio.create_task(connection._next_event(asyncio.Queue()))  # noqa: SLF001
    for _ in range(10):
        if connection._event_waiters:  # noqa: SLF001
            break
        await asyncio.sleep(0)
    await connection._admission_condition.acquire()  # noqa: SLF001
    operation.cancel("first event cancellation")
    await asyncio.sleep(0)
    operation.cancel("second event cancellation")
    await asyncio.sleep(0)
    connection._admission_condition.release()  # noqa: SLF001

    with pytest.raises(asyncio.CancelledError) as cancelled:
        await operation
    assert cancelled.value.args == ("first event cancellation",)
    assert connection._event_waiters == set()  # noqa: SLF001
    assert connection._inflight_operations == 0  # noqa: SLF001
    await asyncio.wait_for(connection.close(), timeout=1.0)


@pytest.mark.spec
@pytest.mark.asyncio
async def test_owned_failure_and_first_cancellation_are_both_preserved() -> None:
    release = asyncio.Event()

    async def fail_late() -> None:
        await release.wait()
        raise RuntimeError("owned operation failed")

    owned = asyncio.create_task(fail_late())

    async def wait_for_owned() -> None:
        cancellation = await connection_module._await_task_to_completion(owned)  # noqa: SLF001
        if cancellation is not None:
            raise cancellation

    waiter = asyncio.create_task(wait_for_owned())
    await asyncio.sleep(0)
    waiter.cancel("first cancellation")
    await asyncio.sleep(0)
    waiter.cancel("second cancellation")
    release.set()

    with pytest.raises(BaseExceptionGroup) as grouped:
        await waiter
    cancellations = grouped.value.subgroup(asyncio.CancelledError)
    failures = grouped.value.subgroup(RuntimeError)
    assert cancellations is not None
    assert cancellations.exceptions[0].args == ("first cancellation",)
    assert failures is not None
    assert str(failures.exceptions[0]) == "owned operation failed"


@pytest.mark.spec
@pytest.mark.asyncio
async def test_join_response_failure_poison_requires_explicit_recovery(
    fake_rti,
) -> None:  # type: ignore[no-untyped-def]
    connection = await RtiConnection.connect("memory://fake-rti").__aenter__()
    original_record = fake_rti.record
    committed = False

    async def commit_then_lose_response(method: str, **kwargs: object) -> object:
        nonlocal committed
        result = original_record(method, **kwargs)
        if method == "join_federation":
            committed = True
            raise RuntimeError("join response lost")
        return result

    fake_rti.record = commit_then_lose_response
    context = connection.join_federation(
        FederationSpec(name="commit-unknown"), federate_name="alice"
    )
    with pytest.raises(FederationJoinCommitUnknownError) as failure:
        await context.__aenter__()
    assert committed is True
    assert isinstance(failure.value.failure, RuntimeError)
    assert fake_rti.calls_for("resign_federation") == []

    with pytest.raises(RtiConnectionPoisonedError):
        await connection.create_federation(FederationSpec(name="blocked"))
    await connection.recover_commit_unknown()
    assert connection._closed is True  # noqa: SLF001
    assert fake_rti.calls_for("resign_federation") == []


@pytest.mark.spec
@pytest.mark.asyncio
async def test_federate_context_rejects_concurrent_enter_without_second_join(
    fake_rti,
) -> None:  # type: ignore[no-untyped-def]
    connection = await RtiConnection.connect("memory://fake-rti").__aenter__()
    original_record = fake_rti.record
    join_started = asyncio.Event()
    release_join = asyncio.Event()

    async def blocked_join(method: str, **kwargs: object) -> object:
        result = original_record(method, **kwargs)
        if method == "join_federation":
            join_started.set()
            await release_join.wait()
        return result

    fake_rti.record = blocked_join
    context = connection.join_federation(
        FederationSpec(name="single-use"), federate_name="alice"
    )
    first = asyncio.create_task(context.__aenter__())
    await join_started.wait()
    second = asyncio.create_task(context.__aenter__())
    await asyncio.sleep(0)
    assert not second.done()
    release_join.set()
    await first
    with pytest.raises(RuntimeError, match="single-use"):
        await second
    await context.__aexit__(None, None, None)
    assert len(fake_rti.calls_for("join_federation")) == 1
    assert len(fake_rti.calls_for("resign_federation")) == 1
    await connection.close()


@pytest.mark.spec
@pytest.mark.asyncio
async def test_concurrent_exit_waits_for_enter_then_resigns_published_handle(
    fake_rti,
) -> None:  # type: ignore[no-untyped-def]
    connection = await RtiConnection.connect("memory://fake-rti").__aenter__()
    original_record = fake_rti.record
    join_started = asyncio.Event()
    release_join = asyncio.Event()

    async def blocked_join(method: str, **kwargs: object) -> object:
        result = original_record(method, **kwargs)
        if method == "join_federation":
            join_started.set()
            await release_join.wait()
        return result

    fake_rti.record = blocked_join
    context = connection.join_federation(
        FederationSpec(name="enter-exit-race"), federate_name="alice"
    )
    entering = asyncio.create_task(context.__aenter__())
    await join_started.wait()
    exiting = asyncio.create_task(context.__aexit__(None, None, None))
    await asyncio.sleep(0)
    assert not exiting.done()
    release_join.set()

    await entering
    await exiting
    assert len(fake_rti.calls_for("join_federation")) == 1
    assert len(fake_rti.calls_for("resign_federation")) == 1
    assert connection._active_federate_contexts == set()  # noqa: SLF001
    await connection.close()
