"""Float64-only selection, authoritative metadata, and legacy endpoint safety."""

import asyncio
import os
from types import SimpleNamespace
from unittest.mock import AsyncMock, Mock

import pytest

import rti1516e
from rti1516e import FederationSpec, Rti1516eAmbassador, RtiConnection, errors
from rti1516e._grpc_errors import translate_rpc_error
from rti1516e._inprocess import InProcessTransport
from rti1516e._services import list_federation_executions
from rti1516e._transport import GrpcTransport, _ensure_generated_path
from tests.test_live_service_e2e import live_artifacts, live_server  # noqa: F401

_ensure_generated_path()
from rti.v1 import federation_pb2  # noqa: E402 - generated namespace setup above


@pytest.mark.parametrize("requested", ["", "HLAfloat64Time"])
@pytest.mark.parametrize("returned", [None, "", "HLAfloat64Time"])
def test_create_wire_and_legacy_metadata(requested, returned):
    transport = GrpcTransport(Mock(), "grpc://unused")
    response = SimpleNamespace(federation_generation=4)
    if returned is not None:
        response.logical_time_implementation_name = returned
    transport.federation.CreateFederation = AsyncMock(return_value=response)
    spec = FederationSpec("selected", logical_time_implementation_name=requested)
    assert asyncio.run(transport._create_federation(spec)) is None
    request = transport.federation.CreateFederation.await_args.args[0]
    assert request.logical_time_implementation_name == requested
    assert transport.logical_time_implementation_name("selected") == "HLAfloat64Time"


@pytest.mark.parametrize(
    "name", ["unsupportedTime", "custom", "hlafloat64time", " HLAfloat64Time "]
)
def test_unsupported_selection_has_public_error_before_dispatch(name):
    assert rti1516e.CouldNotCreateLogicalTimeFactory is errors.CouldNotCreateLogicalTimeFactory
    transport = InProcessTransport(url="memory://time-rejected")

    async def run():
        async with RtiConnection.connect("memory://time-rejected") as conn:
            with pytest.raises(errors.CouldNotCreateLogicalTimeFactory):
                await conn.create_federation(
                    FederationSpec("fed"), logical_time_implementation_name=name
                )
            with pytest.raises(errors.CouldNotCreateLogicalTimeFactory):
                async with conn.join_federation(
                    FederationSpec("fed", logical_time_implementation_name=name), federate_name="a"
                ):
                    pytest.fail("unsupported federate exposed")

    asyncio.run(run())
    assert transport.calls_for("create_federation") == []
    assert transport.calls_for("join_federation") == []


def test_async_create_keyword_preserves_return_and_spec():
    transport = InProcessTransport(url="memory://time-keyword")
    spec = FederationSpec("fed")

    async def run():
        async with RtiConnection.connect("memory://time-keyword") as conn:
            assert (
                await conn.create_federation(
                    spec, logical_time_implementation_name="HLAfloat64Time"
                )
                is None
            )

    asyncio.run(run())
    assert spec.logical_time_implementation_name == ""
    assert (
        transport.calls_for("create_federation")[0].args["spec"].logical_time_implementation_name
        == "HLAfloat64Time"
    )


@pytest.mark.parametrize("returned", [None, "", "HLAfloat64Time", "unsupportedTime"])
def test_join_selection_and_unsupported_cleanup(returned):
    transport = GrpcTransport(Mock(), "grpc://unused")
    transport._generation_by_federation["fed"] = 4
    response = SimpleNamespace(federate_handle=9, session_token=b"s" * 32)
    if returned is not None:
        response.logical_time_implementation_name = returned
    transport.federation.JoinFederation = AsyncMock(return_value=response)
    transport._resign_federation = AsyncMock()
    transport._start_event_stream = Mock()
    if returned == "unsupportedTime":
        with pytest.raises(errors.CouldNotCreateLogicalTimeFactory):
            asyncio.run(transport._join_federation(FederationSpec("fed"), "a"))
        transport._resign_federation.assert_awaited_once_with(
            9,
            federation_name="fed",
            session_token=b"s" * 32,
        )
        transport._start_event_stream.assert_not_called()
    else:
        assert asyncio.run(transport._join_federation(FederationSpec("fed"), "a")) == 9
        fed = rti1516e.Federate(connection=Mock(), transport=transport, handle=9, name="a")
        assert fed.logical_time_implementation_name == "HLAfloat64Time"


@pytest.mark.parametrize("name", ["", "HLAfloat64Time", "HLAinteger64Time", "future"])
def test_list_preserves_shape_and_authoritative_names(monkeypatch, name):
    response = federation_pb2.ListFederationsResponse(
        federations=[
            federation_pb2.FederationSummary(
                name="fed", federation_generation=4, logical_time_implementation_name=name
            )
        ]
    )
    stub = SimpleNamespace(ListFederations=AsyncMock(return_value=response))
    monkeypatch.setattr("rti.v1.federation_pb2_grpc.FederationServiceStub", Mock(return_value=stub))
    conn = RtiConnection("grpc://unused")
    conn._transport = SimpleNamespace(channel=Mock())
    rows = asyncio.run(list_federation_executions(conn))
    assert isinstance(rows, tuple) and len(rows) == 1
    assert rows[0].logical_time_implementation_name == (name or "HLAfloat64Time")
    assert rows[0].name == "fed" and rows[0].federation_generation == 4


def test_named_wire_error_maps_to_public_type():
    class RpcError(Exception):
        def trailing_metadata(self):
            return (("rti-spec-exception", "CouldNotCreateLogicalTimeFactory"),)

    with pytest.raises(errors.CouldNotCreateLogicalTimeFactory):
        translate_rpc_error(RpcError("unsupported"))


@pytest.mark.parametrize("eager", [False, True])
def test_facade_retains_selector_and_exposes_joined_type(eager):
    transport = InProcessTransport(url="memory://time-facade")
    amb = Rti1516eAmbassador()
    try:
        if not eager:
            cached_before = (
                amb._federation_name,
                list(amb._fom_modules),
                amb._mim_module,
                amb._logical_time_implementation_name,
            )
            with pytest.raises(
                RuntimeError, match=r"connect\(\).*before createFederationExecution"
            ):
                amb.createFederationExecution(
                    "fed", [], logical_time_implementation_name="HLAfloat64Time"
                )
            assert transport.calls == []
            assert (
                amb._federation_name,
                amb._fom_modules,
                amb._mim_module,
                amb._logical_time_implementation_name,
            ) == cached_before
        amb.connect(amb, "memory://time-facade")
        amb.createFederationExecution("fed", [], logical_time_implementation_name="HLAfloat64Time")
        amb.joinFederationExecution("a", "fed")
        assert amb.getLogicalTimeImplementationName() == "HLAfloat64Time"
        for call in transport.calls_for("create_federation") + transport.calls_for(
            "join_federation"
        ):
            assert call.args["spec"].logical_time_implementation_name == "HLAfloat64Time"
        with pytest.raises(errors.CouldNotCreateLogicalTimeFactory):
            amb.createFederationExecution(
                "bad", [], logical_time_implementation_name="unsupportedTime"
            )
    finally:
        amb.resignFederationExecution()
        amb.disconnect()


def test_unsupported_create_response_does_not_become_confirmed_selection():
    transport = GrpcTransport(Mock(), "grpc://unused")
    transport.federation.CreateFederation = AsyncMock(
        return_value=SimpleNamespace(
            federation_generation=4, logical_time_implementation_name="unsupportedTime"
        )
    )
    with pytest.raises(errors.RTIinternalError):
        asyncio.run(transport._create_federation(FederationSpec("fed")))
    assert "fed" not in transport._logical_time_by_federation


@pytest.mark.integration
@pytest.mark.skipif(
    os.environ.get("GORTI_RUN_LIVE_E2E") != "1", reason="production localhost tests are opt-in"
)
def test_live_time_selection_metadata_and_rejection(live_server):  # noqa: F811 - imported fixture
    import grpc

    url, base, _ = live_server

    async def run():
        async with asyncio.timeout(25), RtiConnection.connect(url) as conn:
            for i, name in enumerate(("", "HLAfloat64Time", "HLAinteger64Time")):
                expected = name or "HLAfloat64Time"
                spec = FederationSpec(
                    f"time-{i}", [str(base)], logical_time_implementation_name=name
                )
                assert await conn.create_federation(spec) is None
                assert conn.transport.logical_time_implementation_name(spec.name) == expected
                rows = await list_federation_executions(conn)
                assert (
                    next(
                        row for row in rows if row.name == spec.name
                    ).logical_time_implementation_name
                    == expected
                )
                async with conn.join_federation(spec, federate_name="a") as fed:
                    assert fed.logical_time_implementation_name == expected
                    logical_time = await fed.query_logical_time()
                    assert logical_time == 0
                    assert type(logical_time) is (int if expected == "HLAinteger64Time" else float)
            before = await list_federation_executions(conn)
            for name in ("unsupportedTime", "custom"):
                with pytest.raises(errors.CouldNotCreateLogicalTimeFactory):
                    await conn.create_federation(
                        FederationSpec("rejected"), logical_time_implementation_name=name
                    )
                with pytest.raises(grpc.aio.AioRpcError) as caught:
                    await conn.transport.federation.CreateFederation(
                        federation_pb2.CreateFederationRequest(
                            wire_version=1,
                            federation_name="rejected",
                            logical_time_implementation_name=name,
                        )
                    )
                with pytest.raises(errors.CouldNotCreateLogicalTimeFactory):
                    translate_rpc_error(caught.value)
            assert await list_federation_executions(conn) == before

    asyncio.run(run())
