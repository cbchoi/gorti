"""Facade regressions for S1516.1 4.5 and nonmember sync/save/restore calls.

These isolate Python admission and delegation, not live RTI conformance.
Source PDF SHA256: 789780a38898bd513fe1dbdb877433e6518dac8d0eb577056fc0bbe9e5b5a9d4.
Locators: 4.5.3(a)/4.5.5(h), 4.11.5(c), 4.16.5(f), 4.24.5(c).
"""

from __future__ import annotations

import asyncio
from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest

from rti1516e import Rti1516eAmbassador
from rti1516e._inprocess import InProcessTransport
from rti1516e.connection import RtiConnection
from rti1516e.errors import FederateNotExecutionMember, FederationExecutionAlreadyExists

SERVICES = [
    (
        "registerFederationSynchronizationPoint",
        ("point", b"\x00tag\xff"),
        {},
        "sync",
        "register_synchronization_point",
        {"tag": b"\x00tag\xff", "required_federates": None},
    ),
    (
        "registerFederationSynchronizationPoint",
        ("point", b"tag", []),
        {},
        "sync",
        "register_synchronization_point",
        {"tag": b"tag", "required_federates": []},
    ),
    (
        "requestFederationSave",
        ("point",),
        {},
        "savepoint",
        "request_federation_save",
        {"save_time": None},
    ),
    (
        "requestFederationSave",
        ("point", 2.0),
        {},
        "savepoint",
        "request_federation_save",
        {"save_time": 2.0},
    ),
    ("requestFederationRestore", ("point",), {}, "savepoint", "request_federation_restore", {}),
    (
        "requestFederationRestore",
        ("point",),
        {"saved_federation_generation": 7},
        "savepoint",
        "request_federation_restore",
        {"saved_federation_generation": 7},
    ),
]
SERVICE_IDS = ["sync-all", "sync-empty", "save", "save-timed", "restore", "restore-generation"]


def cached_spec(amb):
    return (
        amb._federation_name,
        list(amb._fom_modules),
        amb._mim_module,
        amb._logical_time_implementation_name,
    )


@pytest.mark.parametrize("previously_connected", [False, True], ids=["fresh", "disconnected"])
def test_disconnected_create_rejects_before_dispatch_or_cache(previously_connected):
    url = "memory://fm-admission-offline"
    transport = InProcessTransport(url=url)
    amb = Rti1516eAmbassador()
    try:
        if previously_connected:
            amb.connect(amb, url)
            amb.createFederationExecution(
                "previous", ["previous.xml"], mim_module="previous-mim.xml"
            )
            amb.disconnect()
        before = cached_spec(amb)
        calls_before = list(transport.calls)
        with pytest.raises(RuntimeError, match=r"connect\(\).*before createFederationExecution"):
            amb.createFederationExecution(
                "rejected",
                ["rejected.xml"],
                mim_module="rejected-mim.xml",
                logical_time_implementation_name="HLAinteger64Time",
            )
        assert cached_spec(amb) == before
        assert transport.calls == calls_before

        amb.connect(amb, url)
        amb.joinFederationExecution("member", "rejected")
        (joined,) = transport.calls_for("join_federation")
        assert joined.args["spec"].fom_modules == []
        assert joined.args["spec"].mim_module is None
        assert joined.args["spec"].logical_time_implementation_name == ""
    finally:
        amb.disconnect()


def test_unopened_connection_rejects_create_before_cache(monkeypatch):
    amb = Rti1516eAmbassador()
    connection = RtiConnection.connect("memory://fm-admission-unopened")
    create = AsyncMock()
    monkeypatch.setattr(connection, "create_federation", create)
    monkeypatch.setattr(amb, "_connection", connection)
    before = cached_spec(amb)
    try:
        with pytest.raises(RuntimeError, match=r"connect\(\).*before createFederationExecution"):
            amb.createFederationExecution("rejected", ["rejected.xml"])
        create.assert_not_called()
        assert cached_spec(amb) == before
    finally:
        amb.disconnect()


@pytest.mark.parametrize("designator", ["HLAfloat64Time", "HLAinteger64Time"])
def test_connected_create_preserves_eager_dispatch_and_cached_inputs(designator):
    url = "memory://fm-admission-connected"
    transport = InProcessTransport(url=url)
    amb = Rti1516eAmbassador()
    modules = ["base.xml"]
    try:
        amb.connect(amb, url)
        amb.createFederationExecution(
            "accepted",
            modules,
            mim_module="mim.xml",
            logical_time_implementation_name=designator,
        )
        (created,) = transport.calls_for("create_federation")
        assert created.args["exist_ok"] is False
        assert created.args["spec"].name == "accepted"
        assert created.args["spec"].fom_modules == ["base.xml"]
        assert created.args["spec"].mim_module == "mim.xml"
        assert created.args["spec"].logical_time_implementation_name == designator
        modules.append("later.xml")
        assert cached_spec(amb) == ("accepted", ["base.xml"], "mim.xml", designator)
    finally:
        amb.disconnect()


def test_connected_create_failure_preserves_previous_cache_and_exception():
    url = "memory://fm-admission-create-failure"
    transport = InProcessTransport(url=url)
    amb = Rti1516eAmbassador()
    try:
        amb.connect(amb, url)
        amb.createFederationExecution("previous", ["previous.xml"], mim_module="previous-mim.xml")
        before = cached_spec(amb)
        error = FederationExecutionAlreadyExists("duplicate")
        transport.canned_responses["create_federation"] = error
        with pytest.raises(FederationExecutionAlreadyExists) as caught:
            amb.createFederationExecution("duplicate", ["new.xml"])
        assert caught.value is error
        assert cached_spec(amb) == before
        assert len(transport.calls_for("create_federation")) == 2
    finally:
        amb.disconnect()


@pytest.mark.parametrize("method,args,kwargs,client,operation,forwarded", SERVICES, ids=SERVICE_IDS)
def test_connected_nonmember_services_raise_public_exception_without_dispatch(
    method,
    args,
    kwargs,
    client,
    operation,
    forwarded,
):
    url = "memory://fm-admission-nonmember"
    transport = InProcessTransport(url=url)
    amb = Rti1516eAmbassador()
    try:
        amb.connect(amb, url)
        amb.createFederationExecution("accepted", ["base.xml"])
        calls_before = list(transport.calls)
        before = cached_spec(amb)
        with pytest.raises(FederateNotExecutionMember) as caught:
            getattr(amb, method)(*args, **kwargs)
        assert type(caught.value) is FederateNotExecutionMember
        assert transport.calls == calls_before
        assert cached_spec(amb) == before
        amb.joinFederationExecution("later-member", "accepted")
        (joined,) = transport.calls_for("join_federation")
        assert joined.args["federate_name"] == "later-member"
        assert joined.args["spec"].fom_modules == ["base.xml"]
    finally:
        amb.disconnect()


@pytest.mark.parametrize("method,args,kwargs,client,operation,forwarded", SERVICES, ids=SERVICE_IDS)
def test_member_services_preserve_delegation(
    monkeypatch, method, args, kwargs, client, operation, forwarded
):
    amb = Rti1516eAmbassador()
    target = AsyncMock()
    member = SimpleNamespace(**{client: SimpleNamespace(**{operation: target})})
    monkeypatch.setattr(amb, "_federate", member)
    monkeypatch.setattr(amb, "_run", asyncio.run)
    try:
        getattr(amb, method)(*args, **kwargs)
        target.assert_awaited_once_with("point", **forwarded)
    finally:
        amb.disconnect()
