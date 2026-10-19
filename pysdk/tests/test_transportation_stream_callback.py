import asyncio
from types import SimpleNamespace

import pytest

from rti1516e.events import TransportationChangeConfirmed
from rti1516e.standard import Rti1516eAmbassador
from tests.test_service_completion import federate  # noqa: F401


@pytest.mark.parametrize("interaction", [False, True])
def test_transportation_stream_entry_activation_body_completion_and_duplicate(interaction):
    trace = []
    amb = Rti1516eAmbassador()
    event = TransportationChangeConfirmed(b"t" * 32, 3, 1, 0 if interaction else 4,
                                          () if interaction else (5, 6), 9 if interaction else 0, 1)
    object.__setattr__(event, "_callback_receipt", b"r" * 32)

    async def report(*args, invocation_entry=False):
        trace.append("entry" if invocation_entry else "completion")

    async def activate(value):
        assert value.ticket == event.ticket
        trace.append("activate")
        return TransportationChangeConfirmed(event.ticket, 3, 1, event.object_handle,
                                             (), event.interaction_class_handle, 1)

    amb._federate = SimpleNamespace(report_callback_invocation=report,
                                    callback_invocation_entry_supported=True,
                                    _activate_transportation_confirmation=activate)
    amb._run = asyncio.run
    amb.confirmAttributeTransportationTypeChange = lambda obj, attrs, kind: trace.append("body")
    amb.confirmInteractionTransportationTypeChange = lambda cls, kind: trace.append("body")
    assert amb._dispatch_callback(event)
    assert not amb._dispatch_callback(event)
    assert trace == ["activate", "entry", "body", "completion"]


def test_activation_failure_is_not_callback_completion_and_can_retry():
    amb = Rti1516eAmbassador()
    event = TransportationChangeConfirmed(b"t" * 32, 3, 1, 0, (), 9, 1)
    object.__setattr__(event, "_callback_receipt", b"r" * 32)
    outcomes, bodies = [], []
    failures = [True]

    async def report(*args, invocation_entry=False):
        outcomes.append(invocation_entry)

    async def activate(value):
        if failures[0]:
            raise RuntimeError("activation retry")
        return value

    amb._federate = SimpleNamespace(report_callback_invocation=report,
                                    callback_invocation_entry_supported=True,
                                    _activate_transportation_confirmation=activate)
    amb._run = asyncio.run
    amb.confirmInteractionTransportationTypeChange = lambda *args: bodies.append(True)
    with pytest.raises(RuntimeError, match="activation retry"):
        amb._dispatch_callback(event)
    assert bodies == [] and outcomes == []
    failures[0] = False
    assert amb._dispatch_callback(event)
    assert bodies == [True] and outcomes == [True, False]


def test_activated_ticket_retries_entry_failure_without_duplicate_body():
    amb = Rti1516eAmbassador()
    event = TransportationChangeConfirmed(b"t" * 32, 3, 1, 0, (), 9, 1)
    object.__setattr__(event, "_callback_receipt", b"r" * 32)
    trace, failures = [], [True]

    async def activate(value):
        trace.append("activate")
        return value

    async def report(*args, invocation_entry=False):
        trace.append("entry" if invocation_entry else "completion")
        if invocation_entry and failures[0]:
            assert trace == ["activate", "entry"]
            raise RuntimeError("entry retry")

    amb._federate = SimpleNamespace(report_callback_invocation=report,
                                    callback_invocation_entry_supported=True,
                                    _activate_transportation_confirmation=activate)
    amb._run = asyncio.run
    amb.confirmInteractionTransportationTypeChange = lambda *args: trace.append("body")
    with pytest.raises(RuntimeError, match="entry retry"):
        amb._dispatch_callback(event)
    failures[0] = False
    assert amb._dispatch_callback(event)
    assert not amb._dispatch_callback(event)
    assert trace == ["activate", "entry", "activate", "entry", "body", "completion"]


@pytest.mark.asyncio
async def test_activation_retains_authenticated_scope_and_empty_achieved_set(federate):  # noqa: F811
    from rti.v1 import object_pb2

    fed, channel, _ = federate
    fed._transport._generation_by_federation = {"f": 3}
    fed._transport._callback_epochs = {7: 1}
    event = TransportationChangeConfirmed(b"t" * 32, 3, 1, 4, (5, 6), 0, 1)
    channel.response = object_pb2.TransportationChange(ticket=event.ticket,
        federation_generation=3, callback_epoch=1, object_handle=4, transport_type=1)
    achieved = await fed._activate_transportation_confirmation(event)
    assert achieved.attribute_handles == ()
    path, request, options = channel.calls[-1]
    assert path.endswith("/ActivateTransportationTypeChange")
    assert request.federate_handle == 7 and request.expected_federation_generation == 3
    assert request.expected_callback_epoch == 1 and bytes(request.ticket) == event.ticket
    assert options["metadata"] == (("gorti-session-token-bin", b"t" * 32),)


@pytest.mark.asyncio
@pytest.mark.parametrize("field,value", [
    ("ticket", b"x" * 32), ("federation_generation", 4), ("callback_epoch", 2),
    ("object_handle", 8), ("interaction_class_handle", 9), ("transport_type", 2),
    ("attribute_handles", [7]), ("attribute_handles", [5, 5]),
])
async def test_mismatched_activation_never_becomes_confirmation(federate, field, value):  # noqa: F811
    from rti.v1 import object_pb2

    fed, channel, _ = federate
    fed._transport._generation_by_federation = {"f": 3}
    fed._transport._callback_epochs = {7: 1}
    fields = {"ticket": b"t" * 32, "federation_generation": 3, "callback_epoch": 1,
              "object_handle": 4, "transport_type": 1, "attribute_handles": [5]}
    fields[field] = value
    channel.response = object_pb2.TransportationChange(**fields)
    event = TransportationChangeConfirmed(b"t" * 32, 3, 1, 4, (5, 6), 0, 1)
    with pytest.raises(RuntimeError, match="mismatched"):
        await fed._activate_transportation_confirmation(event)


@pytest.mark.asyncio
@pytest.mark.parametrize("failed_phase", [None, "activate", "entry"])
async def test_layer1_transportation_dispatch_retains_admission_until_body(federate, failed_phase):  # noqa: F811
    from rti.v1 import common_pb2, object_pb2

    fed, _, _ = federate
    fed._callback_entry_supported = True
    fed._transport._generation_by_federation = {"f": 3}
    fed._transport._callback_epochs = {7: 1}
    event = TransportationChangeConfirmed(b"t" * 32, 3, 1, 0, (), 9, 1)
    object.__setattr__(event, "_callback_receipt", b"r" * 32)
    trace, failures = [], [failed_phase]

    async def rpc(service, method, request, **fields):
        phase = ("activate" if method == "ActivateTransportationTypeChange"
                 else "entry" if fields.get("invocation_entry") else "completion")
        trace.append(phase)
        if phase == failures[0]:
            raise RuntimeError("admission retry")
        if phase == "activate":
            return object_pb2.TransportationChange(ticket=event.ticket,
                federation_generation=3, callback_epoch=1, interaction_class_handle=9,
                transport_type=1)
        return common_pb2.Empty()

    async def body(value):
        assert value.interaction_class_handle == 9
        trace.append("body")

    fed._service_rpc = rpc
    if failed_phase is not None:
        with pytest.raises(RuntimeError, match="admission retry"):
            await fed.dispatch_event(event, body)
        assert "body" not in trace and "completion" not in trace
        failures[0] = None
    await fed.dispatch_event(event, body)
    await fed.dispatch_event(event, body)
    assert trace[-4:] == ["activate", "entry", "body", "completion"]
    assert trace.count("body") == trace.count("completion") == 1


@pytest.mark.parametrize("model", ["HLA_EVOKED", "HLA_IMMEDIATE"])
@pytest.mark.parametrize("failed_phase", ["activate", "entry"])
def test_standard_queue_retains_failed_admission_for_next_evoke(model, failed_phase):
    from rti1516e._callbacks import CallbackDispatcher

    amb = Rti1516eAmbassador()
    amb._callback_dispatcher = CallbackDispatcher(amb._dispatch_callback, model)
    event = TransportationChangeConfirmed(b"t" * 32, 3, 1, 0, (), 9, 1)
    object.__setattr__(event, "_callback_receipt", b"r" * 32)
    trace, failures = [], [failed_phase]

    async def activate(value):
        trace.append("activate")
        if failures[0] == "activate":
            raise RuntimeError("admission retry")
        return value

    async def report(*args, invocation_entry=False):
        phase = "entry" if invocation_entry else "completion"
        trace.append(phase)
        if failures[0] == phase:
            raise RuntimeError("admission retry")

    amb._federate = SimpleNamespace(report_callback_invocation=report,
        callback_invocation_entry_supported=True, _activate_transportation_confirmation=activate)
    amb._run = asyncio.run
    amb.confirmInteractionTransportationTypeChange = lambda *args: trace.append("body")
    dispatcher = amb._callback_dispatcher
    try:
        dispatcher.submit(event, wait=False)
        if model == "HLA_IMMEDIATE":
            with dispatcher._condition:
                assert dispatcher._condition.wait_for(lambda: bool(dispatcher._errors), timeout=2)
        with pytest.raises(RuntimeError, match="admission retry"):
            amb.evokeCallback(0, 0.2)
        assert "body" not in trace and "completion" not in trace
        failures[0] = None
        assert amb.evokeCallback(0, 0.2)
        assert trace[-4:] == ["activate", "entry", "body", "completion"]
        dispatcher.submit(event, wait=False)
        amb.evokeCallback(0, 0.05)
        assert trace.count("body") == trace.count("completion") == 1
    finally:
        dispatcher.close()


def test_standard_queue_does_not_retry_user_exception_or_retired_admission():
    from rti1516e._callbacks import CallbackDispatcher

    for retire in (False, True):
        amb = Rti1516eAmbassador()
        amb._callback_dispatcher = CallbackDispatcher(amb._dispatch_callback, "HLA_EVOKED")
        event = TransportationChangeConfirmed(b"t" * 32, 3, 1, 0, (), 9, 1)
        object.__setattr__(event, "_callback_receipt", b"r" * 32)
        trace = []

        async def activate(value, trace=trace, retire=retire):
            trace.append("activate")
            if retire:
                raise RuntimeError("retired admission")
            return value

        async def report(receipt, success, exception="", *, invocation_entry=False, trace=trace):
            trace.append("entry" if invocation_entry else ("completion", success))

        def body(*args, trace=trace):
            trace.append("body")
            raise RuntimeError("user exception")

        amb._federate = SimpleNamespace(report_callback_invocation=report,
            callback_invocation_entry_supported=True,
            _activate_transportation_confirmation=activate)
        amb._run = asyncio.run
        amb.confirmInteractionTransportationTypeChange = body
        dispatcher = amb._callback_dispatcher
        try:
            dispatcher.submit(event, wait=False)
            failure_message = "retired admission" if retire else "user exception"
            with pytest.raises(RuntimeError, match=failure_message):
                amb.evokeCallback(0)
            if retire:
                dispatcher.clear()
            assert not amb.evokeCallback(0)
            assert trace == (["activate"] if retire else
                             ["activate", "entry", "body", ("completion", False)])
        finally:
            dispatcher.close()
