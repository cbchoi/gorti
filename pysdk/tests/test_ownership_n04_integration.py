"""Ownership binding checks against a freshly built production rtid."""

import asyncio
import contextlib
import socket
import subprocess
import uuid
from unittest.mock import AsyncMock

import pytest

from rti1516e.errors import FederationExecutionAlreadyExists
from tests._legacy_server import build_rtid, spawn_rtid
from tests.spec.m39.test_m39_layer2_api import (
    FOM,
    _leave,
    _Recorder,
    _setup_owned_instance,
    _wait_for_grpc,
)


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


@pytest.fixture
def production_rtid():
    binary = build_rtid()
    port = free_port()
    proc = spawn_rtid(binary, port, 0)
    try:
        asyncio.run(_wait_for_grpc(port))
        yield f"grpc://127.0.0.1:{port}"
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()


class TaggedRecorder(_Recorder):
    def attributeOwnershipAcquisitionNotificationWithTag(  # noqa: N802
        self, obj, attrs, owner, tag
    ):
        self._record("tagged", object_handle=obj, attrs=attrs, owner=owner, tag=tag)

    def requestAttributeOwnershipAssumption(self, obj, attrs, owner, tag):  # noqa: N802
        self._record("offer", object_handle=obj, attrs=attrs, tag=tag)


def join(url, federation, name):
    amb = TaggedRecorder()
    amb.connect(amb, url)
    try:
        with contextlib.suppress(FederationExecutionAlreadyExists):
            amb.createFederationExecution(federation, [FOM])
        amb.joinFederationExecution(name, federation)
    except BaseException:
        amb.disconnect()
        raise
    return amb


@pytest.mark.integration
def test_n04_production_tags_result_and_no_followup_queries(production_rtid):
    federation = "n04-" + uuid.uuid4().hex[:12]
    pub = join(production_rtid, federation, "owner")
    sub = join(production_rtid, federation, "acquirer")
    try:
        obj, pos = _setup_owned_instance(pub, sub)
        velocity = int(pub.getAttributeHandle(pub.getObjectClassHandle("Vehicle"), "Velocity"))
        offer, acquire, confirm = b"\x01\x00\x80\xff", b"\x02\x00\x80\xff", b"\x00\x80\xff"
        pub.negotiatedAttributeOwnershipDivestiture(obj, [pos], offer, two_phase=True)
        sub.wait_for("offer", object_handle=obj, tag=offer)
        sub.attributeOwnershipAcquisition(obj, [pos], acquire)
        pub.wait_for("divestConfirmation", object_handle=obj)
        pub.confirmDivestiture(obj, [pos], confirm)
        sub.wait_for("tagged", object_handle=obj, attrs=(pos,), tag=confirm)

        pub.attributeOwnershipAcquisition(obj, [pos], acquire)
        sub.wait_for("releaseRequested", object_handle=obj, tag=acquire)
        result = sub.attributeOwnershipDivestitureIfWanted(obj, [pos])
        assert result == (pos,)
        pub.wait_for("tagged", object_handle=obj, attrs=(pos,), tag=acquire)

        sub.attributeOwnershipAcquisition(obj, [pos], acquire)
        pub.wait_for("releaseRequested", object_handle=obj, tag=acquire)
        binding = pub._fed().ownership
        binding._stub.QueryAttributeOwnership = AsyncMock(
            side_effect=AssertionError("follow-up query")
        )
        binding._stub.IsAttributeOwnedByFederate = AsyncMock(
            side_effect=AssertionError("follow-up query")
        )
        partial = pub.attributeOwnershipDivestitureIfWanted(obj, [pos, velocity])
        assert partial == (pos,)
        binding._stub.QueryAttributeOwnership.assert_not_called()
        binding._stub.IsAttributeOwnedByFederate.assert_not_called()
        assert result == (pos,)
    finally:
        _leave(sub)
        _leave(pub)
