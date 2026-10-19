from contextlib import asynccontextmanager
from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest

from rti1516e._transport import _ensure_generated_path
from rti1516e.connection import Federate

_ensure_generated_path()
from rti.v1 import common_pb2, time_pb2  # noqa: E402


class Channel:
    def __init__(self):
        self.calls = []
        self.response = common_pb2.Empty()

    def unary_unary(self, path, **kwargs):
        async def call(request, **options):
            self.calls.append(
                (path, type(request).FromString(request.SerializeToString()), options)
            )
            return self.response

        return call


@pytest.fixture(name="federate")
def federate():
    leases = []

    @asynccontextmanager
    async def admit():
        leases.append("enter")
        try:
            yield
        finally:
            leases.append("exit")

    channel = Channel()
    transport = SimpleNamespace(
        channel=channel,
        _federation_name="f",
        guard_service=lambda: None,
        session_metadata=lambda token: (("gorti-session-token-bin", token),),
    )
    fed = Federate(
        connection=SimpleNamespace(_admit_work=admit), transport=transport, handle=7, name="alice"
    )
    fed._session_token = b"t" * 32
    return fed, channel, leases


@pytest.mark.asyncio
async def test_missing_declaration_services_use_wire_and_current_identity(federate):
    fed, channel, leases = federate
    await fed.unpublish_object_class(10, attributes=[2])
    await fed.unsubscribe_object_class(10)
    fed._restore_identity(9)
    await fed.unpublish_interaction_class(20)
    await fed.unsubscribe_interaction_class(20)
    assert [path.rsplit("/", 1)[-1] for path, _, _ in channel.calls] == [
        "UnpublishObjectClassAttributes",
        "UnsubscribeObjectClassAttributes",
        "UnpublishInteractionClass",
        "UnsubscribeInteractionClass",
    ]
    assert list(channel.calls[0][1].attribute_handles) == [2]
    assert list(channel.calls[1][1].attribute_handles) == []
    assert channel.calls[2][1].federate_handle == 9
    assert leases == ["enter", "exit"] * 4


@pytest.mark.asyncio
@pytest.mark.parametrize("attributes,present", [(None, False), ([], True), ([2], True)])
async def test_unpublish_optional_attribute_set_survives_wire(federate, attributes, present):
    fed, channel, _ = federate
    await fed.unpublish_object_class(10, attributes=attributes)
    request = channel.calls[0][1]
    assert request.HasField("attribute_set_present") is present
    if present:
        assert request.attribute_set_present is True
    assert list(request.attribute_handles) == (attributes or [])
    assert request.object_class_handle == 10
    assert request.federate_handle == 7


@pytest.mark.parametrize("attributes", [[], [2]])
def test_standard_unpublish_attribute_set_preserves_explicit_empty(federate, attributes):
    import asyncio

    from rti1516e.standard import Rti1516eAmbassador

    fed, channel, _ = federate
    amb = Rti1516eAmbassador()
    amb._federate = fed
    amb._run = asyncio.run
    amb.unpublishObjectClass(10)
    amb.unpublishObjectClassAttributes(10, attributes)
    whole, targeted = [request for _, request, _ in channel.calls]
    assert not whole.HasField("attribute_set_present")
    assert targeted.HasField("attribute_set_present")
    assert targeted.attribute_set_present is True
    assert list(targeted.attribute_handles) == attributes


@pytest.mark.asyncio
@pytest.mark.parametrize("attributes,present", [(None, False), ([], True), ([2], True)])
async def test_unsubscribe_optional_attribute_set_survives_wire(federate, attributes, present):
    fed, channel, _ = federate
    await fed.unsubscribe_object_class(10, attributes=attributes)
    request = channel.calls[0][1]
    assert request.HasField("attribute_set_present") is present
    if present:
        assert request.attribute_set_present is True
    assert list(request.attribute_handles) == (attributes or [])
    assert request.object_class_handle == 10
    assert request.federate_handle == 7


@pytest.mark.parametrize("attributes", [[], [2]])
def test_standard_unsubscribe_attribute_set_preserves_explicit_empty(federate, attributes):
    import asyncio

    from rti1516e.standard import Rti1516eAmbassador

    fed, channel, _ = federate
    amb = Rti1516eAmbassador()
    amb._federate = fed
    amb._run = asyncio.run
    amb.unsubscribeObjectClass(10)
    amb.unsubscribeObjectClassAttributes(10, attributes)
    whole, targeted = [request for _, request, _ in channel.calls]
    assert not whole.HasField("attribute_set_present")
    assert targeted.HasField("attribute_set_present")
    assert targeted.attribute_set_present is True
    assert list(targeted.attribute_handles) == attributes


@pytest.mark.asyncio
async def test_time_queries_preserve_finite_flag(federate):
    fed, channel, _ = federate
    channel.response = time_pb2.QueryGALTResponse(galt=12.5, finite=True)
    assert await fed.query_galt() == (12.5, True)
    channel.response = time_pb2.QueryLITSResponse(finite=False)
    assert await fed.query_lits() == (0.0, False)


@pytest.mark.asyncio
async def test_report_receipt_metadata_no_token_in_body(federate):
    fed, channel, _ = federate
    await fed.report_callback_invocation(b"r" * 32, False, "ValueError: user callback")
    path, req, options = channel.calls[0]
    assert path.endswith("/ReportCallbackInvocation")
    assert req.callback_receipt == b"r" * 32
    assert not req.success and req.exception == "ValueError: user callback"
    assert options["metadata"] == (("gorti-session-token-bin", b"t" * 32),)
    assert "session_token" not in req.DESCRIPTOR.fields_by_name
    fed._session_token = b""
    with pytest.raises(RuntimeError, match="session"):
        await fed.report_callback_invocation(b"r" * 32, True)
    assert len(channel.calls) == 1


def test_evoked_reporting_follows_actual_invocation_only():
    from rti1516e._callbacks import CallbackDispatcher
    from rti1516e.events import ReceiveInteraction
    from rti1516e.standard import Rti1516eAmbassador

    amb = Rti1516eAmbassador()
    report = AsyncMock()
    amb._federate = SimpleNamespace(
        report_callback_invocation=report, callback_invocation_entry_supported=True)
    import asyncio

    amb._run = asyncio.run
    amb._callback_dispatcher = CallbackDispatcher(amb._dispatch_callback, "HLA_EVOKED")
    event = ReceiveInteraction("Signal", {}, None)
    object.__setattr__(event, "_callback_receipt", b"r" * 32)
    amb._dispatch_event(event)
    report.assert_not_called()
    amb.evokeCallback(0)
    assert report.await_count == 2
    assert report.await_args_list[0].args == (b"r" * 32, True, "")
    assert report.await_args_list[0].kwargs == {"invocation_entry": True}
    assert report.await_args_list[1].args == (b"r" * 32, True, "")


@pytest.mark.parametrize(
    "standard,async_name,args,kwargs",
    [
        ("unpublishObjectClass", "unpublish_object_class", (10,), {}),
        ("unsubscribeObjectClass", "unsubscribe_object_class", (10,), {}),
        ("unpublishInteractionClass", "unpublish_interaction_class", (20,), {}),
        ("unsubscribeInteractionClass", "unsubscribe_interaction_class", (20,), {}),
        ("changeAttributeOrderType", "change_attribute_order_type", (4, [2], 1), {}),
        ("changeInteractionOrderType", "change_interaction_order_type", (20, 1), {}),
        ("setAutomaticResignDirective", "set_automatic_resign_directive", ("NO_ACTION",), {}),
        ("getAutomaticResignDirective", "get_automatic_resign_directive", (), {}),
        ("queryGALT", "query_galt", (), {}),
        ("queryLITS", "query_lits", (), {}),
        ("retract", "retract", (123,), {}),
    ],
)
def test_standard_service_forwarding(standard, async_name, args, kwargs):
    import asyncio

    from rti1516e.standard import Rti1516eAmbassador

    method = AsyncMock(return_value=1)
    amb = Rti1516eAmbassador()
    amb._federate = SimpleNamespace(**{async_name: method})
    amb._run = asyncio.run
    getattr(amb, standard)(*args, **kwargs)
    method.assert_awaited_once_with(*args, **kwargs)


def test_transportation_callbacks_obey_evoked_and_preserve_order():
    import asyncio

    from rti1516e._callbacks import CallbackDispatcher
    from rti1516e.standard import Rti1516eAmbassador

    amb = Rti1516eAmbassador()
    amb._run = asyncio.run
    amb._callback_dispatcher = CallbackDispatcher(amb._dispatch_callback, "HLA_EVOKED")
    amb._federate = SimpleNamespace(
        request_attribute_transportation_type_change=AsyncMock(),
        query_attribute_transportation_type=AsyncMock(return_value=2),
        request_interaction_transportation_type_change=AsyncMock(),
        query_interaction_transportation_type=AsyncMock(return_value=1),
    )
    received = []
    amb.confirmAttributeTransportationTypeChange = lambda *args: received.append(("attr", args))
    amb.reportAttributeTransportationType = lambda *args: received.append(("attr-query", args))
    amb.confirmInteractionTransportationTypeChange = lambda *args: received.append(("int", args))
    amb.reportInteractionTransportationType = lambda *args: received.append(("int-query", args))
    amb.requestAttributeTransportationTypeChange(5, [2], 2)
    amb.queryAttributeTransportationType(5, 2)
    amb.requestInteractionTransportationTypeChange(20, 1)
    amb.queryInteractionTransportationType(8, 20)
    assert received == []
    amb.evokeMultipleCallbacks(0, 0.05)
    assert received == [
        ("attr", (5, [2], 2)),
        ("attr-query", (5, 2, 2)),
        ("int", (20, 1)),
        ("int-query", (8, 20, 1)),
    ]


@pytest.mark.asyncio
async def test_secure_automatic_resign_wire(federate):
    from rti.v1 import common_pb2, federation_pb2

    fed, channel, _ = federate
    await fed.set_automatic_resign_directive("NO_ACTION")
    assert channel.calls[0][1].action == common_pb2.RESIGN_ACTION_NO_ACTION
    assert channel.calls[0][2]["metadata"] == (("gorti-session-token-bin", b"t" * 32),)
    channel.response = federation_pb2.GetAutomaticResignDirectiveResponse(
        action=common_pb2.RESIGN_ACTION_DELETE_OBJECTS
    )
    assert await fed.get_automatic_resign_directive() == common_pb2.RESIGN_ACTION_DELETE_OBJECTS
    fed._session_token = b""
    with pytest.raises(RuntimeError, match="session"):
        await fed.get_automatic_resign_directive()


@pytest.mark.parametrize("message", ["user error", ""])
def test_callback_failure_and_report_failure_are_independent(message):
    import asyncio

    from rti1516e._callbacks import CallbackDispatcher
    from rti1516e.events import ReceiveInteraction
    from rti1516e.standard import Rti1516eAmbassador

    amb = Rti1516eAmbassador()
    report = AsyncMock(side_effect=[None, RuntimeError("report unavailable")])
    amb._federate = SimpleNamespace(
        report_callback_invocation=report, callback_invocation_entry_supported=True)
    amb._run = asyncio.run
    amb._callback_dispatcher = CallbackDispatcher(amb._dispatch_callback, "HLA_EVOKED")
    calls = []
    failure = ValueError(message)

    def fail(*args):
        calls.append(args)
        raise failure

    amb.receiveInteraction = fail
    event = ReceiveInteraction("Signal", {}, None)
    object.__setattr__(event, "_callback_receipt", b"r" * 32)
    amb._dispatch_event(event)
    with pytest.raises(ValueError) as caught:
        amb.evokeCallback(0)
    assert caught.value is failure
    assert len(calls) == 1
    assert len(amb._callback_report_failures) == 1
    assert report.await_count == 2
    assert report.await_args_list[0].kwargs == {"invocation_entry": True}
    assert report.await_args_list[1].args == (b"r" * 32, False, f"ValueError: {message}")
    assert not amb.evokeCallback(0)


def test_purged_unrecognized_and_disabled_events_are_not_reported():
    import asyncio

    from rti1516e._callbacks import CallbackDispatcher
    from rti1516e.events import ReceiveInteraction
    from rti1516e.standard import Rti1516eAmbassador

    amb = Rti1516eAmbassador()
    report = AsyncMock()
    amb._federate = SimpleNamespace(report_callback_invocation=report)
    amb._run = asyncio.run
    amb._callback_dispatcher = CallbackDispatcher(amb._dispatch_callback, "HLA_EVOKED")
    event = ReceiveInteraction("Signal", {}, None)
    object.__setattr__(event, "_callback_receipt", b"r" * 32)
    amb.disableCallbacks()
    amb._dispatch_event(event)
    amb.evokeCallback(0)
    amb._callback_dispatcher.clear()
    amb.enableCallbacks()
    amb._dispatch_event(SimpleNamespace(_callback_receipt=b"u" * 32))
    amb.evokeCallback(0)
    report.assert_not_called()


@pytest.mark.asyncio
async def test_receipt_retry_reuses_exact_outcome(federate):
    import grpc

    class Transient(grpc.RpcError):
        def code(self):
            return grpc.StatusCode.UNAVAILABLE

    fed, _, _ = federate
    fed._service_rpc = AsyncMock(side_effect=[Transient(), common_pb2.Empty()])
    await fed.report_callback_invocation(b"r" * 32, False, "user error")
    assert fed._service_rpc.await_count == 2
    assert fed._service_rpc.await_args_list[0] == fed._service_rpc.await_args_list[1]


@pytest.mark.asyncio
async def test_atomic_name_release_is_one_wire_request(federate):
    fed, channel, _ = federate
    await fed.reservation.release_multiple(["first", "second"])
    assert len(channel.calls) == 1
    path, req, _ = channel.calls[0]
    assert path.endswith("/ReleaseMultipleObjectInstanceNames")
    assert list(req.object_names) == ["first", "second"]


@pytest.mark.asyncio
async def test_restore_source_generation_presence_and_zero(federate):
    fed, channel, _ = federate
    await fed.savepoint.request_federation_restore("saved")
    assert not channel.calls[0][1].HasField("saved_federation_generation")
    await fed.savepoint.request_federation_restore("saved", saved_federation_generation=0)
    assert channel.calls[1][1].HasField("saved_federation_generation")
    assert channel.calls[1][1].saved_federation_generation == 0
    await fed.savepoint.request_federation_restore("saved", saved_federation_generation=123)
    assert channel.calls[2][1].saved_federation_generation == 123


def test_list_federations_before_join_defers_callback():
    import asyncio

    from rti.v1 import federation_pb2

    from rti1516e._callbacks import CallbackDispatcher
    from rti1516e.standard import Rti1516eAmbassador

    channel = Channel()
    channel.response = federation_pb2.ListFederationsResponse(federations=[
        federation_pb2.FederationSummary(
            name="one", mode=1, federates_joined=2, federation_generation=7),
    ])

    @asynccontextmanager
    async def admit():
        yield SimpleNamespace(channel=channel)

    amb = Rti1516eAmbassador()
    amb._run = asyncio.run
    amb._connection = SimpleNamespace(_admit_work=admit)
    amb._callback_dispatcher = CallbackDispatcher(amb._dispatch_callback, "HLA_EVOKED")
    reports = []
    amb.reportFederationExecutions = reports.append
    rows = amb.listFederationExecutions()
    assert rows[0].name == "one" and rows[0].federation_generation == 7
    assert reports == []
    amb.evokeCallback(0)
    assert reports == [rows]
    assert len(channel.calls) == 1


@pytest.mark.asyncio
async def test_mom_actual_measurement_maps_are_optional_and_detached(federate):
    from rti.v1 import mom_pb2

    fed, channel, _ = federate
    channel.response = mom_pb2.QueryFederateAttributesResponse(
        found=True, federate_handle=7,
        attribute_values={"HLAinteractionsReceived": b"\x00\x00\x00\x02"},
        unavailable_attributes={"HLAreflectionsReceived": "pending callback receipts"},
    )
    result = await fed.mom.query_federate_attributes(7)
    assert result.attribute_values["HLAinteractionsReceived"] == b"\x00\x00\x00\x02"
    assert result.unavailable_attributes["HLAreflectionsReceived"] == "pending callback receipts"
    channel.response.attribute_values.clear()
    assert len(result.attribute_values) == 1
    channel.response = mom_pb2.QueryFederationAttributesResponse(
        federation_name="f", attribute_values={"HLAfederationName": b"encoded"},
        unavailable_attributes={"HLAnextSaveTime": "no save scheduled"},
    )
    federation = await fed.mom.query_federation_attributes()
    assert federation.attribute_values == {"HLAfederationName": b"encoded"}
    assert federation.unavailable_attributes == {"HLAnextSaveTime": "no save scheduled"}
    channel.response = mom_pb2.QueryFederateAttributesResponse(found=True)
    legacy = await fed.mom.query_federate_attributes(7)
    assert legacy.attribute_values == {} and legacy.unavailable_attributes == {}
