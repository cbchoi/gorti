"""R7 engineering regressions for compatible federation facade inputs.

These test the Python product API, not a normative binding conformance claim.
The transport supports Float64 and exact Integer64 and forwards its selector.
"""

from __future__ import annotations

import asyncio
import subprocess
from collections.abc import Iterator
from pathlib import Path
from typing import Any

import pytest

from rti1516e import Rti1516eAmbassador
from rti1516e._inprocess import InProcessTransport
from rti1516e.errors import CouldNotCreateLogicalTimeFactory, FederationExecutionAlreadyExists
from tests._legacy_server import build_rtid, spawn_rtid
from tests.spec.m26.test_ieee1516e_ambassador_smoke import _free_port, _wait_for_grpc

pytestmark = pytest.mark.spec
_ROOT = Path(__file__).resolve().parents[4]
_FOM = str(_ROOT / "examples" / "ieee1516e-ambassador-smoke" / "federation.fom.xml")


@pytest.fixture
def recorded_ambassador() -> Iterator[tuple[Rti1516eAmbassador, InProcessTransport]]:
    url = "memory://m26-facade-inputs"
    transport = InProcessTransport(url=url)
    amb = Rti1516eAmbassador()
    amb.connect(amb, url)
    try:
        yield amb, transport
    finally:
        try:
            amb.resignFederationExecution()
        finally:
            amb.disconnect()


@pytest.mark.parametrize("form", ["two-positional", "three-positional", "keywords"])
def test_join_legacy_forms_forward_inputs(recorded_ambassador: Any, form: str) -> None:
    amb, transport = recorded_ambassador
    amb.createFederationExecution("demo", ["base.xml"])
    additional: list[str] = []
    if form == "two-positional":
        amb.joinFederationExecution("alice", "demo")
    elif form == "three-positional":
        additional = ["extra.xml"]
        amb.joinFederationExecution("alice", "demo", additional)
    else:
        additional = ["extra.xml"]
        amb.joinFederationExecution(
            federate_name="alice",
            federation_name="demo",
            additional_fom_modules=additional,
        )
    (call,) = transport.calls_for("join_federation")
    assert call.args["federate_name"] == "alice"
    assert call.args["federate_type"] == ""
    assert call.args["spec"].name == "demo"
    assert call.args["spec"].fom_modules == ["base.xml"]
    assert call.args["spec"].additional_fom_modules == additional
    additional.append("later.xml")
    assert "later.xml" not in call.args["spec"].additional_fom_modules


@pytest.mark.parametrize(
    "federate_type",
    ["", "VehicleModel", "  exact type  "],
    ids=["empty", "named", "whitespace"],
)
def test_join_keyword_type_reaches_transport(recorded_ambassador: Any, federate_type: str) -> None:
    amb, transport = recorded_ambassador
    amb.createFederationExecution("demo", ["base.xml"])
    amb.joinFederationExecution(
        "alice",
        "demo",
        ["extra.xml"],
        federate_type=federate_type,
    )
    (call,) = transport.calls_for("join_federation")
    assert call.args["federate_name"] == "alice"
    assert call.args["federate_type"] == federate_type
    assert call.args["spec"].name == "demo"
    assert call.args["spec"].fom_modules == ["base.xml"]
    assert call.args["spec"].additional_fom_modules == ["extra.xml"]


def test_join_keyword_type_does_not_reuse_other_federation_modules(
    recorded_ambassador: Any,
) -> None:
    amb, transport = recorded_ambassador
    amb.createFederationExecution("previous", ["previous.xml"])
    amb.joinFederationExecution(
        federate_name="alice",
        federation_name="target",
        federate_type="VehicleModel",
        additional_fom_modules=["extra.xml"],
    )
    (call,) = transport.calls_for("join_federation")
    assert call.args["federate_type"] == "VehicleModel"
    assert call.args["spec"].name == "target"
    assert call.args["spec"].fom_modules == []
    assert call.args["spec"].additional_fom_modules == ["extra.xml"]


@pytest.mark.parametrize("ambiguous_modules", ["demo", "", b"demo"], ids=["name", "empty", "bytes"])
def test_join_rejects_ambiguous_three_string_form_before_dispatch(
    recorded_ambassador: Any,
    ambiguous_modules: Any,
) -> None:
    amb, transport = recorded_ambassador
    with pytest.raises(TypeError, match="additional_fom_modules.*federate_type"):
        amb.joinFederationExecution("alice", "VehicleModel", ambiguous_modules)
    assert transport.calls_for("create_federation") == []
    assert transport.calls_for("join_federation") == []
    assert amb._federate is None


@pytest.mark.parametrize("federate_type", [None, 42, ["VehicleModel"]], ids=["none", "int", "list"])
def test_join_rejects_non_string_type_before_dispatch(
    recorded_ambassador: Any,
    federate_type: Any,
) -> None:
    amb, transport = recorded_ambassador
    with pytest.raises(TypeError, match="federate_type must be a string"):
        amb.joinFederationExecution("alice", "demo", federate_type=federate_type)
    assert transport.calls_for("create_federation") == []
    assert transport.calls_for("join_federation") == []


@pytest.mark.parametrize("designator", ["", "HLAfloat64Time"], ids=["default", "float64"])
@pytest.mark.parametrize("connected", [False, True], ids=["deferred", "eager"])
def test_create_supported_time_designator_preserves_modules(
    designator: str,
    connected: bool,
) -> None:
    url = "memory://m26-create-time-inputs"
    transport = InProcessTransport(url=url)
    amb = Rti1516eAmbassador()
    modules = ["base.xml"]
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
                amb.createFederationExecution(
                    "demo",
                    modules,
                    logical_time_implementation_name=designator,
                )
            assert transport.calls == []
            assert (
                amb._federation_name,
                amb._fom_modules,
                amb._mim_module,
                amb._logical_time_implementation_name,
            ) == cached_before
        amb.connect(amb, url)
        amb.createFederationExecution(
            "demo",
            modules,
            logical_time_implementation_name=designator,
        )
        modules.append("later.xml")
        amb.joinFederationExecution("alice", "demo")
        creates = transport.calls_for("create_federation")
        assert len(creates) == 2
        assert [call.args["exist_ok"] for call in creates] == [False, True]
        for call in creates:
            assert call.args["spec"].name == "demo"
            assert call.args["spec"].fom_modules == ["base.xml"]
            assert call.args["spec"].additional_fom_modules == []
            assert call.args["spec"].logical_time_implementation_name == designator
    finally:
        try:
            amb.resignFederationExecution()
        finally:
            amb.disconnect()


@pytest.mark.parametrize(
    "designator",
    ["custom-time", "hlafloat64time", "custom-mim.xml"],
    ids=["custom", "case-mismatch", "mim-not-time"],
)
def test_create_rejects_unsupported_time_before_dispatch_or_cache_change(
    recorded_ambassador: Any,
    designator: str,
) -> None:
    amb, transport = recorded_ambassador
    amb.createFederationExecution("previous", ["previous.xml"])
    before = list(transport.calls)
    with pytest.raises(ValueError, match="unsupported logical time implementation"):
        amb.createFederationExecution(
            "rejected",
            ["rejected.xml"],
            logical_time_implementation_name=designator,
        )
    assert transport.calls == before
    amb.joinFederationExecution("alice", "previous")
    (call,) = transport.calls_for("join_federation")
    assert call.args["spec"].name == "previous"
    assert call.args["spec"].fom_modules == ["previous.xml"]


def test_create_integer64_is_supported_locally(recorded_ambassador: Any) -> None:
    amb, transport = recorded_ambassador
    amb.createFederationExecution(
        "integer64",
        [],
        logical_time_implementation_name="HLAinteger64Time",
    )
    (call,) = transport.calls_for("create_federation")
    assert call.args["spec"].logical_time_implementation_name == "HLAinteger64Time"


@pytest.mark.parametrize("designator", [None, 42, []], ids=["none", "int", "list"])
def test_create_rejects_non_string_time_before_dispatch(
    recorded_ambassador: Any,
    designator: Any,
) -> None:
    amb, transport = recorded_ambassador
    with pytest.raises(TypeError, match="logical_time_implementation_name must be a string"):
        amb.createFederationExecution("demo", [], logical_time_implementation_name=designator)
    assert transport.calls_for("create_federation") == []


def test_create_does_not_offer_positional_mim_overload(recorded_ambassador: Any) -> None:
    amb, transport = recorded_ambassador
    with pytest.raises(TypeError):
        amb.createFederationExecution("demo", ["base.xml"], "custom-mim.xml")
    with pytest.raises(TypeError):
        amb.createFederationExecution("demo", ["base.xml"], mim_designator="custom-mim.xml")
    assert transport.calls_for("create_federation") == []


@pytest.fixture(scope="module")
def live_rtid_url() -> Iterator[str]:
    binary = build_rtid()
    port = _free_port()
    metrics_port = _free_port()
    while metrics_port == port:
        metrics_port = _free_port()
    proc = spawn_rtid(binary, port, metrics_port)
    try:
        asyncio.run(_wait_for_grpc(port))
        yield f"grpc://127.0.0.1:{port}"
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=5)


@pytest.mark.integration
@pytest.mark.parametrize("form", ["legacy", "keyword-type"])
def test_live_join_type_and_additional_fom_reach_server(live_rtid_url: str, form: str) -> None:
    amb = Rti1516eAmbassador()
    name = f"m26-r7-{form}"
    amb.connect(amb, live_rtid_url)
    try:
        amb.createFederationExecution(name, [])
        kwargs = {} if form == "legacy" else {"federate_type": "VehicleModel"}
        amb.joinFederationExecution("alice", name, [_FOM], **kwargs)
        handle = amb.getFederateHandle("alice")
        attrs = amb.queryFederateAttributes(handle)
        assert attrs.found
        assert attrs.federate_name == "alice"
        assert attrs.federate_type == ("" if form == "legacy" else "VehicleModel")
        assert amb.queryFederationAttributes().federation_name == name
        assert amb.getObjectClassHandle("Vehicle") > 0
        assert amb.getInteractionClassHandle("Honk") > 0
    finally:
        try:
            amb.resignFederationExecution()
            amb.destroyFederationExecution(name)
        finally:
            amb.disconnect()


@pytest.mark.integration
@pytest.mark.parametrize("time_name", ["HLAfloat64Time", "HLAinteger64Time"])
def test_live_create_time_validation_and_legacy_lifecycle(
    live_rtid_url: str, time_name: str
) -> None:
    amb = Rti1516eAmbassador()
    name = "m26-r7-time"
    amb.connect(amb, live_rtid_url)
    try:
        before = amb.listFederationExecutions()
        with pytest.raises(CouldNotCreateLogicalTimeFactory):
            amb.createFederationExecution(
                name,
                [_FOM],
                logical_time_implementation_name="unsupportedTime",
            )
        assert amb.listFederationExecutions() == before
        amb.createFederationExecution(
            name,
            [_FOM],
            logical_time_implementation_name=time_name,
        )
        with pytest.raises(FederationExecutionAlreadyExists):
            amb.createFederationExecution(name, [_FOM])
        amb.joinFederationExecution("alice", name)
        assert amb.getLogicalTimeImplementationName() == time_name
        logical_time = amb.queryLogicalTime()
        assert logical_time == 0
        assert type(logical_time) is (int if time_name == "HLAinteger64Time" else float)
        assert amb.getObjectClassHandle("Vehicle") > 0
        amb.resignFederationExecution()
        amb.destroyFederationExecution(name)
        assert amb.listFederationExecutions() == before
    finally:
        try:
            amb.resignFederationExecution()
        finally:
            amb.disconnect()
