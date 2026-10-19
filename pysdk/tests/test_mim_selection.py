"""Explicit MIM role: transport, compatible facade, and local model retention."""

import asyncio
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock, Mock

import pytest

from rti1516e import FederationSpec, MIMModule, Rti1516eAmbassador
from rti1516e._inprocess import InProcessTransport
from rti1516e._transport import GrpcTransport, _ensure_generated_path

_ensure_generated_path()
from rti.v1 import common_pb2, federation_pb2, federation_pb2_grpc  # noqa: E402 - path bootstrap

ROOT = Path(__file__).resolve().parents[2]
MIM = ROOT / "rti/pkg/fom/mim/standard-mim.xml"
FOM = ROOT / "examples/ieee1516e-ambassador-smoke/federation.fom.xml"


def test_create_transport_keeps_mim_separate_from_fom():
    transport = GrpcTransport(Mock(), "grpc://unused")
    transport.federation.CreateFederation = AsyncMock(
        return_value=SimpleNamespace(federation_generation=4)
    )
    spec = SimpleNamespace(
        name="selected",
        fom_modules=[str(FOM)],
        mim_module=MIM,
        mode="verbose",
        seed=0,
        stall_timeout_seconds=0,
    )
    asyncio.run(transport._create_federation(spec))
    request = transport.federation.CreateFederation.await_args.args[0]
    assert request.HasField("mim_module")
    assert request.wire_version == 2
    assert request.mim_module.path == str(MIM)
    assert request.mim_module.xml == MIM.read_bytes()
    assert [module.path for module in request.fom_modules] == [str(FOM)]


def test_view_retains_selected_mim_and_hydrates_model():
    transport = GrpcTransport(Mock(), "grpc://unused")
    selected = common_pb2.FOMModule(path="selected-mim.xml", xml=MIM.read_bytes())
    view = federation_pb2.FOMView(
        revision=1,
        mim_module=selected,
        modules=[common_pb2.FOMModule(path="base.xml", xml=FOM.read_bytes())],
    )
    transport._install_fom_view(view, replace=True)
    assert getattr(transport, "_mim_module", None) is not None
    assert transport._mim_module.designator == "selected-mim.xml"
    assert transport._mim_module.xml == selected.xml
    assert transport._fom_cache is not None
    assert transport._fom_cache.mim_module == transport._mim_module


@pytest.fixture
def connected_facade():
    url = "memory://mim-input-validation"
    transport = InProcessTransport(url=url)
    amb = Rti1516eAmbassador()
    amb.connect(amb, url)
    try:
        yield amb, transport
    finally:
        amb.disconnect()


@pytest.mark.parametrize(
    "modules", ["base.xml", b"base.xml", [42]], ids=["string", "bytes", "bad-element"]
)
def test_facade_rejects_ambiguous_fom_input_before_cache_change(connected_facade, modules):
    amb, transport = connected_facade
    amb.createFederationExecution("before", ["base.xml"])
    calls_before = list(transport.calls)
    with pytest.raises(TypeError, match="fom_modules"):
        amb.createFederationExecution("after", modules)
    assert amb._federation_name == "before"
    assert amb._fom_modules == ["base.xml"]
    assert transport.calls == calls_before


@pytest.mark.parametrize("selected", [False, True], ids=["omitted", "inline"])
def test_create_wire_profile_and_exact_inline_bytes(selected):
    transport = GrpcTransport(Mock(), "grpc://unused")
    transport.federation.CreateFederation = AsyncMock(
        return_value=SimpleNamespace(federation_generation=4)
    )
    mim = MIMModule("  custom-mim.xml  ", MIM.read_bytes()) if selected else None
    asyncio.run(transport._create_federation(FederationSpec("selected", mim_module=mim)))
    request = transport.federation.CreateFederation.await_args.args[0]
    assert request.wire_version == (2 if selected else 1)
    assert request.HasField("mim_module") == selected
    assert list(request.fom_modules) == []
    if selected:
        assert request.mim_module.path == mim.designator
        assert request.mim_module.xml == mim.xml


@pytest.mark.parametrize("connected", [False, True], ids=["deferred", "eager"])
def test_facade_mim_keyword_survives_create_join_and_stays_federation_scoped(connected):
    url = "memory://mim-selection"
    transport = InProcessTransport(url=url)
    amb = Rti1516eAmbassador()
    selected = MIMModule("selected.xml", MIM.read_bytes())
    try:
        if not connected:
            cached_before = (
                amb._federation_name,
                list(amb._fom_modules),
                amb._mim_module,
                amb._logical_time_implementation_name,
            )
            with pytest.raises(
                RuntimeError, match=r"connect\(\).*before createFederationExecution"
            ):
                amb.createFederationExecution("selected", [str(FOM)], mim_module=selected)
            assert transport.calls == []
            assert (
                amb._federation_name,
                amb._fom_modules,
                amb._mim_module,
                amb._logical_time_implementation_name,
            ) == cached_before
        amb.connect(amb, url)
        amb.createFederationExecution("selected", [str(FOM)], mim_module=selected)
        amb.joinFederationExecution("caller", "selected")
        assert [call.args["exist_ok"] for call in transport.calls_for("create_federation")] == [
            False,
            True,
        ]
        for call in transport.calls_for("create_federation") + transport.calls_for(
            "join_federation"
        ):
            assert call.args["spec"].mim_module == selected
            assert call.args["spec"].fom_modules == [str(FOM)]
        amb.resignFederationExecution()
        amb.joinFederationExecution("caller", "another")
        assert transport.calls_for("join_federation")[-1].args["spec"].mim_module is None
    finally:
        try:
            amb.resignFederationExecution()
        finally:
            amb.disconnect()


@pytest.mark.parametrize(
    "bad", ["", " \t ", b"xml", 42, []], ids=["empty", "blank", "bytes", "number", "list"]
)
def test_mim_input_validation_before_facade_cache_change(connected_facade, bad):
    amb, transport = connected_facade
    amb.createFederationExecution("before", [])
    calls_before = list(transport.calls)
    with pytest.raises((TypeError, ValueError), match="mim_module"):
        amb.createFederationExecution("after", [], mim_module=bad)
    assert amb._federation_name == "before"
    assert amb._mim_module is None
    assert transport.calls == calls_before


@pytest.mark.parametrize(
    "designator,xml",
    [("", b"x"), (" \t", b"x"), ("selected", b"")],
    ids=["empty-designator", "blank-designator", "empty-xml"],
)
def test_explicit_mim_cannot_mean_omitted(designator, xml):
    with pytest.raises(ValueError, match="explicit MIM"):
        MIMModule(designator, xml)


def test_parser_uses_selected_mim_without_loading_embedded(monkeypatch):
    from rti1516e.fom import parse, parser

    selected = MIMModule("selected.xml", MIM.read_bytes())
    monkeypatch.setattr(
        parser, "_load_mim", Mock(side_effect=AssertionError("embedded MIM loaded"))
    )
    result = parse([FOM], mim_module=selected)
    assert result.diagnostics == []
    assert result.fom.mim_module == selected
    assert result.fom.find_object_class("HLAobjectRoot.HLAmanager.HLAfederation") is not None


def test_view_refresh_rejects_mim_change_atomically_but_restore_replaces():
    transport = GrpcTransport(Mock(), "grpc://unused")
    transport._federation_name = "selected"
    view = federation_pb2.FOMView(
        revision=1, mim_module=common_pb2.FOMModule(path="selected.xml", xml=MIM.read_bytes())
    )
    transport._install_fom_view(view, replace=True)
    original = transport._fom_cache
    view.mim_module.path = "other.xml"
    view.revision = 2
    with pytest.raises(ValueError, match="changed the selected MIM"):
        transport._install_fom_view(view)
    assert transport._fom_cache is original
    assert transport._fom_revision == 1
    transport._install_fom_view(view, replace=True)
    assert transport._fom_cache.mim_module.designator == "other.xml"
    assert transport._requested_mims["selected"].designator == "other.xml"
    view.ClearField("mim_module")
    transport._install_fom_view(view, replace=True)
    assert transport._mim_module is None
    assert transport._requested_mims["selected"] is None


@pytest.mark.parametrize(
    "response_kind",
    ["missing-view", "omitted", "wrong-path", "wrong-bytes"],
    ids=["missing-view", "omitted", "wrong-path", "wrong-bytes"],
)
def test_join_mim_mismatch_compensates_membership(response_kind):
    transport = GrpcTransport(Mock(), "grpc://unused")
    transport._generation_by_federation["selected"] = 4
    selected = MIMModule("selected.xml", MIM.read_bytes())
    response = federation_pb2.JoinFederationResponse(federate_handle=9)
    if response_kind != "missing-view":
        response.fom_view.revision = 1
        if response_kind != "omitted":
            response.fom_view.mim_module.CopyFrom(
                common_pb2.FOMModule(
                    path="other.xml" if response_kind == "wrong-path" else selected.designator,
                    xml=b"wrong" if response_kind == "wrong-bytes" else selected.xml,
                )
            )
    transport.federation.JoinFederation = AsyncMock(return_value=response)
    transport._resign_federation = AsyncMock()
    transport._start_event_stream = Mock()
    with pytest.raises(RuntimeError, match="did not confirm the selected MIM"):
        asyncio.run(
            transport._join_federation(FederationSpec("selected", mim_module=selected), "caller")
        )
    transport._resign_federation.assert_awaited_once_with(
        9,
        federation_name="selected",
        session_token=b"",
    )
    transport._start_event_stream.assert_not_called()
    assert transport.federation.JoinFederation.await_args.args[0].wire_version == 2


@pytest.mark.parametrize("explicit", [False, True], ids=["omitted", "selected"])
def test_join_sends_mim_precondition_before_additional_modules(explicit):
    transport = GrpcTransport(Mock(), "grpc://unused")
    transport._generation_by_federation["selected"] = 4
    selected = MIMModule("selected.xml", MIM.read_bytes()) if explicit else None
    response = federation_pb2.JoinFederationResponse(federate_handle=9)
    response.fom_view.revision = 1
    if explicit:
        response.fom_view.mim_module.CopyFrom(
            common_pb2.FOMModule(path=selected.designator, xml=selected.xml)
        )
    transport.federation.JoinFederation = AsyncMock(return_value=response)
    transport._start_event_stream = Mock()
    spec = FederationSpec("selected", additional_fom_modules=[str(FOM)], mim_module=selected)
    assert asyncio.run(transport._join_federation(spec, "caller")) == 9
    request = transport.federation.JoinFederation.await_args.args[0]
    assert request.wire_version == (2 if explicit else 1)
    assert request.HasField("expected_mim_module") == explicit
    assert [module.path for module in request.additional_fom_modules] == [str(FOM)]
    if explicit:
        assert request.expected_mim_module.path == selected.designator
        assert request.expected_mim_module.xml == selected.xml


def test_join_uses_known_authoritative_mim_when_spec_omits_it():
    transport = GrpcTransport(Mock(), "grpc://unused")
    transport._federation_name = "selected"
    transport._generation_by_federation["selected"] = 4
    response = federation_pb2.JoinFederationResponse(federate_handle=9)
    response.fom_view.revision = 1
    response.fom_view.mim_module.CopyFrom(
        common_pb2.FOMModule(path="selected.xml", xml=MIM.read_bytes())
    )
    transport._install_fom_view(response.fom_view, replace=True)
    transport.federation.JoinFederation = AsyncMock(return_value=response)
    transport._start_event_stream = Mock()
    asyncio.run(transport._join_federation(FederationSpec("selected"), "caller"))
    request = transport.federation.JoinFederation.await_args.args[0]
    assert request.wire_version == 2
    assert request.expected_mim_module == response.fom_view.mim_module


def test_legacy_server_rejects_explicit_mim_without_creation_or_downgrade():
    import grpc

    async def exercise():
        attempts, created, join_attempts, joined = [], [], [], []

        class LegacyServer(federation_pb2_grpc.FederationServiceServicer):
            async def CreateFederation(self, request, context):  # noqa: N802 - gRPC method
                attempts.append(request.wire_version)
                if request.wire_version != 1:
                    await context.abort(
                        grpc.StatusCode.INVALID_ARGUMENT, "legacy server rejects wire version"
                    )
                created.append(request.federation_name)
                return federation_pb2.CreateFederationResponse(federation_generation=4)

            async def JoinFederation(self, request, context):  # noqa: N802 - gRPC method
                join_attempts.append(request.wire_version)
                if request.wire_version != 1:
                    await context.abort(
                        grpc.StatusCode.INVALID_ARGUMENT, "legacy server rejects wire version"
                    )
                joined.append(request.federate_name)
                return federation_pb2.JoinFederationResponse(federate_handle=9)

        server = grpc.aio.server()
        federation_pb2_grpc.add_FederationServiceServicer_to_server(LegacyServer(), server)
        port = server.add_insecure_port("127.0.0.1:0")
        await server.start()
        channel = grpc.aio.insecure_channel(f"127.0.0.1:{port}")
        transport = GrpcTransport(channel, "grpc://legacy")
        try:
            await transport._create_federation(FederationSpec("legacy", [str(FOM)]))
            previous_model = transport._fom_cache
            previous_handles = dict(transport._interaction_handles)
            with pytest.raises(Exception, match="legacy server rejects wire version"):
                await transport._create_federation(FederationSpec("explicit", mim_module=MIM))
            assert attempts == [1, 2]
            assert created == ["legacy"]
            assert "explicit" not in transport._generation_by_federation
            assert transport._federation_name == "legacy"
            assert transport._mim_module is None
            assert transport._fom_cache is previous_model
            assert transport._interaction_handles == previous_handles
            with pytest.raises(Exception, match="legacy server rejects wire version"):
                await transport._join_federation(
                    FederationSpec("legacy", additional_fom_modules=[str(FOM)], mim_module=MIM),
                    "rejected",
                )
            assert join_attempts == [2]
            assert joined == []
        finally:
            await transport.close()
            await server.stop(None)

    asyncio.run(exercise())
