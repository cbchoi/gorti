import asyncio
from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest

from rti1516e.events import SynchronizationPointAnnounced
from rti1516e.standard import Rti1516eAmbassador
from tests.test_service_completion import federate  # noqa: F401


def event():
    value = SynchronizationPointAnnounced("ready", b"", ())
    object.__setattr__(value, "_callback_receipt", b"r" * 32)
    return value


def test_entry_precedes_reentrant_body_and_duplicate_does_not_reinvoke():
    trace = []
    amb = Rti1516eAmbassador()

    async def report(receipt, success, exception="", *, invocation_entry=False):
        trace.append("entry" if invocation_entry else "completion")

    amb._federate = SimpleNamespace(
        report_callback_invocation=report, callback_invocation_entry_supported=True)
    amb._run = asyncio.run
    amb.announceSynchronizationPoint = lambda *args: trace.append("body")
    assert amb._dispatch_callback(event())
    assert not amb._dispatch_callback(event())
    assert trace == ["entry", "body", "completion"]


def test_failed_entry_can_retry_and_completion_failure_never_reinvokes():
    phases = []
    failures = {"entry": True, "completion": False}
    amb = Rti1516eAmbassador()
    bodies = []

    async def report(receipt, success, exception="", *, invocation_entry=False):
        phase = "entry" if invocation_entry else "completion"
        phases.append(phase)
        if failures[phase]:
            raise RuntimeError(phase)

    amb._federate = SimpleNamespace(
        report_callback_invocation=report, callback_invocation_entry_supported=True)
    amb._run = asyncio.run
    amb.announceSynchronizationPoint = lambda *args: bodies.append(True)
    with pytest.raises(RuntimeError, match="entry"):
        amb._dispatch_callback(event())
    assert bodies == []
    failures["entry"], failures["completion"] = False, True
    amb._dispatch_callback(event())
    failures["completion"] = False
    assert not amb._dispatch_callback(event())
    assert bodies == [True]
    assert phases == ["entry", "entry", "completion", "completion"]


def test_failure_completion_keeps_original_callback_error():
    amb = Rti1516eAmbassador()
    report = AsyncMock()
    amb._federate = SimpleNamespace(
        report_callback_invocation=report, callback_invocation_entry_supported=True)
    amb._run = asyncio.run
    failure = ValueError("user callback")

    def body(*args):
        raise failure

    amb.announceSynchronizationPoint = body
    with pytest.raises(ValueError) as caught:
        amb._dispatch_callback(event())
    assert caught.value is failure
    assert report.await_args_list[0].kwargs == {"invocation_entry": True}
    assert report.await_args_list[1].args == (b"r" * 32, False, "ValueError: user callback")


def test_unrecognized_receipt_is_not_reported_as_entered():
    amb = Rti1516eAmbassador()
    report = AsyncMock()
    amb._federate = SimpleNamespace(report_callback_invocation=report)
    amb._run = asyncio.run
    assert not amb._dispatch_callback(SimpleNamespace(_callback_receipt=b"r" * 32))
    report.assert_not_awaited()


@pytest.mark.asyncio
async def test_layer1_dispatch_authenticates_entry_and_retains_completion(federate):  # noqa: F811
    fed, channel, _ = federate
    fed._callback_entry_supported = True
    observed = []

    async def body(value):
        assert channel.calls[0][1].invocation_entry
        observed.append(value.label)

    await fed.dispatch_event(event(), body)
    await fed.dispatch_event(event(), body)
    assert observed == ["ready"]
    assert [request.invocation_entry for _, request, _ in channel.calls] == [True, False]
    assert all(options["metadata"] == (("gorti-session-token-bin", b"t" * 32),)
               for _, _, options in channel.calls)


@pytest.mark.asyncio
async def test_entry_wire_rejects_contradictory_outcomes_before_rpc(federate):  # noqa: F811
    fed, channel, _ = federate
    for success, exception in [(False, "failure"), (True, "failure")]:
        with pytest.raises(ValueError):
            await fed.report_callback_invocation(
                b"r" * 32, success, exception, invocation_entry=True)
    assert channel.calls == []


def test_pending_receipts_are_not_evicted_at_capacity_and_inflight_is_seen():
    from rti1516e._generic_callbacks import GenericCallbackState

    state = GenericCallbackState(capacity=1)
    assert state.claim(b"first")
    assert not state.claim(b"first")
    with pytest.raises(RuntimeError, match="capacity"):
        state.claim(b"second")
    state.complete(b"first", False, "failure")
    with pytest.raises(RuntimeError, match="capacity"):
        state.claim(b"second")
    assert state.pending() == [(b"first", False, "failure")]
    state.acknowledged(b"first")
    assert state.claim(b"second")


def test_legacy_server_gets_only_completion_after_body():
    amb = Rti1516eAmbassador()
    trace = []

    async def report(receipt, success, exception="", *, invocation_entry=False):
        assert not invocation_entry
        assert trace == ["body"]
        trace.append("completion")

    amb._federate = SimpleNamespace(report_callback_invocation=report)
    amb._run = asyncio.run
    amb.announceSynchronizationPoint = lambda *args: trace.append("body")
    amb._dispatch_callback(event())
    assert trace == ["body", "completion"]
