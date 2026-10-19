"""Bounded public DM/OM oracles, not complete IEEE or Java conformance.

S1516.1: 6.10.3(i), 6.11, 6.14.4(c), 8.1.2 Tables 1/2 and
8.1.4, physical PDF pages 125-126, 132 and 183-184 (visually reviewed).
Positive lookahead and no sender advance before deletion isolate the
non-strict deletion upper bound from zero-lookahead advance restrictions.
The existing independent fixture pins and cleans up each owned server.
"""

from __future__ import annotations

import asyncio
import hashlib
import json
import os
from contextlib import AsyncExitStack, asynccontextmanager
from pathlib import Path

import grpc
import pytest

from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.errors import InvalidLogicalTime, RtiError
from rti1516e.events import (
    DiscoverObjectInstance,
    OrderType,
    ReflectAttributeValues,
    RemoveObjectInstance,
    TimeAdvanceGrant,
    TimeConstrainedEnabled,
    TimeRegulationEnabled,
)
from tests.spec.m21.test_ieee2010_ro_delivery_live import _barrier
from tests.test_ieee2010_independent_contract_live import (
    BASE_CLASS,
    independent_artifacts,  # noqa: F401
)
from tests.test_ieee2010_independent_contract_live import (
    independent_server as _independent_server,  # noqa: F401 - named pytest fixture
)
from tests.test_live_service_e2e import LiveCallbacks

_FAMILIES = ["HLAfloat64Time", "HLAinteger64Time"]
_SOURCE_HASH = "789780a38898bd513fe1dbdb877433e6518dac8d0eb577056fc0bbe9e5b5a9d4"


def _source_evidence():
    source = os.environ.get("GORTI_IEEE1516_1_SOURCE_PDF")
    evidence = {"expected_sha256": _SOURCE_HASH, "verified_this_run": False}
    if source:
        path = Path(source).expanduser().resolve()
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        assert digest == _SOURCE_HASH, "IEEE 1516.1 source PDF does not match the reviewed edition"
        evidence.update(path=str(path), sha256=digest, verified_this_run=True)
    return evidence


def _record(run, name, observations):
    root = Path(__file__).resolve().parents[2]
    paths = [
        Path(__file__),
        root / "pysdk/rti1516e/connection.py",
        root / "pysdk/rti1516e/_transport.py",
        root / "pysdk/rti1516e/_grpc_errors.py",
    ]
    snapshots = {str(path): hashlib.sha256(path.read_bytes()).hexdigest() for path in paths}
    (run / name).write_text(
        json.dumps(
            {
                "source_id": "S1516.1",
                "source_document": _source_evidence(),
                "pdf_pages": [125, 126, 132, 183, 184],
                "read_time_sha256": snapshots,
                "observations": observations,
            },
            indent=2,
        ),
        encoding="utf-8",
    )


def test_source_evidence_without_private_document(monkeypatch):
    monkeypatch.delenv("GORTI_IEEE1516_1_SOURCE_PDF", raising=False)
    assert _source_evidence() == {"expected_sha256": _SOURCE_HASH, "verified_this_run": False}


def test_source_evidence_checks_explicit_document(monkeypatch, tmp_path):
    source = tmp_path / "source.pdf"
    source.write_bytes(b"reviewed test source")
    digest = hashlib.sha256(source.read_bytes()).hexdigest()
    monkeypatch.setenv("GORTI_IEEE1516_1_SOURCE_PDF", str(source))
    monkeypatch.setitem(globals(), "_SOURCE_HASH", digest)
    assert _source_evidence()["verified_this_run"] is True
    assert _source_evidence()["sha256"] == digest


def test_source_evidence_rejects_wrong_document(monkeypatch, tmp_path):
    source = tmp_path / "wrong.pdf"
    source.write_bytes(b"wrong edition")
    monkeypatch.setenv("GORTI_IEEE1516_1_SOURCE_PDF", str(source))
    with pytest.raises(AssertionError, match="reviewed edition"):
        _source_evidence()


def test_source_evidence_rejects_missing_explicit_document(monkeypatch, tmp_path):
    monkeypatch.setenv("GORTI_IEEE1516_1_SOURCE_PDF", str(tmp_path / "missing.pdf"))
    with pytest.raises(FileNotFoundError):
        _source_evidence()


@asynccontextmanager
async def _participants(server, family, *, unconstrained=False):
    url, base, _, run = server
    async with asyncio.timeout(45), AsyncExitStack() as stack:
        members, logs = [], []
        spec = FederationSpec(
            "order-completion", [str(base)], logical_time_implementation_name=family
        )
        for name in ("sender", "constrained", "unconstrained")[: 3 if unconstrained else 2]:
            connection = await stack.enter_async_context(RtiConnection.connect(url))
            member = await stack.enter_async_context(
                connection.join_federation(spec, federate_name=name)
            )
            members.append(member)
            log = LiveCallbacks(member)
            logs.append(log)
            stack.push_async_callback(log.close)
            assert member.logical_time_implementation_name == family
        sender, reader = members[:2]
        await sender.publish_object_class(BASE_CLASS, attributes=["Sample"])
        for member in members[1:]:
            await member.subscribe_object_class(BASE_CLASS, attributes=["Sample"])
        obj = await sender.register_object_instance(BASE_CLASS)
        for log in logs[1:]:
            await log.wait(DiscoverObjectInstance, lambda event: event.object_handle == obj)
        cls = await sender.support.get_object_class_handle(BASE_CLASS)
        attr = await sender.support.get_attribute_handle(cls, "Sample")
        privilege = await sender.support.get_attribute_handle(cls, "HLAprivilegeToDeleteObject")
        await sender.change_attribute_order_type(obj, [attr, privilege], OrderType.TIMESTAMP)
        lookahead = 1 if family == "HLAinteger64Time" else 1.0
        await sender.enable_time_regulation(lookahead)
        await logs[0].wait(TimeRegulationEnabled)
        await reader.enable_time_constrained()
        await logs[1].wait(TimeConstrainedEnabled)
        await _barrier(sender, logs, "configured")
        state = await sender.mom.query_federate_attributes(sender.handle)
        assert state.found and state.time_regulating and not state.time_constrained
        assert await sender.query_logical_time() == 0
        assert await sender.query_lookahead() == lookahead
        state = await reader.mom.query_federate_attributes(reader.handle)
        assert state.found and state.time_constrained and not state.time_regulating
        if unconstrained:
            state = await members[2].mom.query_federate_attributes(members[2].handle)
            assert not state.time_constrained and not state.time_regulating
        yield members, logs, obj, attr, run


async def _advance(member, log, target):
    mark = len(log.events)
    await member.time_advance_request(target)
    grant = await log.wait(TimeAdvanceGrant, after=mark)
    assert grant.time == target
    assert await member.query_logical_time() == target
    return grant


@pytest.mark.parametrize("family", _FAMILIES)
def test_tables_one_two_tso_send_has_receiver_specific_order(independent_server, family):
    async def exercise():
        async with _participants(independent_server, family, unconstrained=True) as data:
            members, logs, obj, attr, run = data
            sender, reader, ro_reader = members
            target = 1 if family == "HLAinteger64Time" else 1.0
            payload, tag = b"\x00\xffat-lookahead-floor", b"\x00table-one-two\xff"
            await sender.update_attributes(
                obj, {attr: payload}, timestamp=target, tag=tag, retraction_handle=901
            )
            await _barrier(sender, logs, "sent-at-inclusive-floor")
            ro = await logs[2].wait(ReflectAttributeValues)
            assert ro.object_handle == obj and ro.attribute_values == {attr: payload}
            assert ro.tag == tag and ro.timestamp == target
            assert ro.metadata.sent_order == OrderType.TIMESTAMP
            assert ro.metadata.received_order == OrderType.RECEIVE
            assert ro.metadata.message_retraction_handle == 901
            assert await ro_reader.query_logical_time() == 0
            assert not any(isinstance(event, ReflectAttributeValues) for event in logs[1].events)
            await _advance(sender, logs[0], target)
            grant = await _advance(reader, logs[1], target)
            tso = await logs[1].wait(ReflectAttributeValues)
            assert tso.object_handle == obj and tso.attribute_values == {attr: payload}
            assert tso.tag == tag and tso.timestamp == target
            assert tso.metadata.sent_order == OrderType.TIMESTAMP
            assert tso.metadata.received_order == OrderType.TIMESTAMP
            assert tso.metadata.message_retraction_handle == 901
            assert logs[1].events.index(tso) < logs[1].events.index(grant)
            assert not any(isinstance(event, ReflectAttributeValues) for event in logs[0].events)
            if family == "HLAinteger64Time":
                assert type(ro.timestamp) is int and type(tso.timestamp) is int
            _record(
                run,
                "tables-observations.json",
                {
                    "family": family,
                    "lookahead": target,
                    "timestamp": target,
                    "unconstrained": repr(ro),
                    "constrained": repr(tso),
                    "sender_events": [repr(event) for event in logs[0].events],
                },
            )

    asyncio.run(exercise())


@pytest.mark.parametrize("family", _FAMILIES)
@pytest.mark.parametrize("relation", ["below", "equal", "above"])
def test_pending_tso_delete_update_timestamp_boundary(independent_server, family, relation):
    async def exercise():
        async with _participants(independent_server, family) as data:
            members, logs, obj, attr, run = data
            sender, reader = members
            delete_time = (1 << 53) + 17 if family == "HLAinteger64Time" else 10.0
            below = delete_time - 1
            timestamp = {"below": below, "equal": delete_time, "above": delete_time + 1}[relation]
            await sender.delete_object_instance(
                obj, timestamp=delete_time, tag=b"pending-delete", retraction_handle=700
            )
            await _barrier(sender, logs, "delete-accepted-before-any-advance")
            name = await sender.support.get_object_instance_name(obj)
            before = await sender.mom.query_federate_attributes(sender.handle)
            assert not any(isinstance(event, RemoveObjectInstance) for event in logs[1].events)
            error = None
            try:
                await sender.update_attributes(
                    obj,
                    {attr: b"candidate"},
                    timestamp=timestamp,
                    tag=b"candidate",
                    retraction_handle=701,
                )
            except (RtiError, grpc.RpcError) as caught:
                error = caught
            await _barrier(sender, logs, "candidate-response-fence")
            after = await sender.mom.query_federate_attributes(sender.handle)
            assert await sender.query_logical_time() == 0
            assert await reader.query_logical_time() == 0
            assert await sender.query_lookahead() == 1
            assert await sender.support.get_object_instance_name(obj) == name
            assert await reader.support.get_object_instance_handle(name) == obj
            if error is not None:
                assert after.updates_sent == before.updates_sent
                assert not any(
                    isinstance(event, (ReflectAttributeValues, RemoveObjectInstance))
                    for event in logs[1].events
                )
                await sender.update_attributes(
                    obj,
                    {attr: b"after-reject"},
                    timestamp=below,
                    tag=b"after-reject",
                    retraction_handle=702,
                )
            await _advance(sender, logs[0], below)
            grant = await _advance(reader, logs[1], below)
            reflections = [
                event for event in logs[1].events if isinstance(event, ReflectAttributeValues)
            ]
            expected = b"after-reject" if error is not None else b"candidate"
            if error is not None or relation == "below":
                assert len(reflections) == 1
                assert reflections[0].attribute_values == {attr: expected}
                assert reflections[0].tag == expected and reflections[0].timestamp == below
                assert reflections[0].metadata.sent_order == OrderType.TIMESTAMP
                assert reflections[0].metadata.received_order == OrderType.TIMESTAMP
                assert logs[1].events.index(reflections[0]) < logs[1].events.index(grant)
            assert not any(isinstance(event, RemoveObjectInstance) for event in logs[1].events)
            if relation == "equal" and error is None:
                await _advance(sender, logs[0], delete_time)
                final_grant = await _advance(reader, logs[1], delete_time)
                final_reflections = [
                    event for event in logs[1].events if isinstance(event, ReflectAttributeValues)
                ]
                removals = [
                    event for event in logs[1].events if isinstance(event, RemoveObjectInstance)
                ]
                assert len(final_reflections) == 1
                assert final_reflections[0].attribute_values == {attr: b"candidate"}
                assert final_reflections[0].timestamp == delete_time
                assert len(removals) == 1 and removals[0].object_handle == obj
                assert removals[0].timestamp == delete_time
                assert logs[1].events.index(final_reflections[0]) < logs[1].events.index(
                    removals[0]
                )
                assert logs[1].events.index(removals[0]) < logs[1].events.index(final_grant)
            observations = {
                "family": family,
                "relation": relation,
                "delete_timestamp": delete_time,
                "update_timestamp": timestamp,
                "sender_initial_time": 0,
                "lookahead": 1,
                "error_type": type(error).__name__ if error else None,
                "error": str(error) if error else None,
                "rpc_status": error.code().name if isinstance(error, grpc.RpcError) else None,
                "sender_before": repr(before),
                "sender_after": repr(after),
                "reader_events": [repr(event) for event in logs[1].events],
                "rejected_call_state_preserved": error is not None,
            }
            _record(run, "boundary-observations.json", observations)
            if relation == "above":
                assert type(error) is InvalidLogicalTime, observations
            else:
                assert error is None, observations

    asyncio.run(exercise())
