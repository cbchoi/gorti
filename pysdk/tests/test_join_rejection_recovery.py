"""Definite join rejection is not a lost-response ownership boundary."""

import asyncio
from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest

from rti1516e import FederationJoinCommitUnknownError, FederationSpec, RtiConnection, errors
from rti1516e._grpc_errors import translate_rpc_error


@pytest.mark.parametrize(
    "status", ["INTERNAL", "UNKNOWN", "UNAVAILABLE", "DEADLINE_EXCEEDED", "CANCELLED", ""]
)
@pytest.mark.parametrize(
    "detail", ["federate already joined", "save in progress", "restore in progress"]
)
def test_uncertain_rpc_status_cannot_become_definite_join_rejection(status, detail):
    class WireFailureError(Exception):
        def code(self):
            return SimpleNamespace(name=status)

        def details(self):
            return detail

        def trailing_metadata(self):
            return ()

    async def exercise():
        failure = WireFailureError(detail)

        async def record(method, **kwargs):
            if method == "join_federation":
                translate_rpc_error(failure)

        connection = RtiConnection("memory://uncertain-wire-admission")
        connection._transport = SimpleNamespace(record=record, close=AsyncMock())
        with pytest.raises(FederationJoinCommitUnknownError) as caught:
            await connection.join_federation(
                FederationSpec("fed"), federate_name="alice"
            ).__aenter__()
        assert connection._poison is caught.value
        assert not connection._active_federate_contexts
        await connection.recover_commit_unknown()
        assert connection._closed

    asyncio.run(exercise())


def test_legacy_already_exists_rejection_allows_same_connection_retry():
    class WireRejectionError(Exception):
        def code(self):
            return SimpleNamespace(name="ALREADY_EXISTS")

        def details(self):
            return "federate already joined"

        def trailing_metadata(self):
            return ()

    async def exercise():
        calls = []

        async def record(method, **kwargs):
            calls.append((method, kwargs))
            if method == "join_federation":
                if kwargs["federate_name"] == "occupied":
                    translate_rpc_error(WireRejectionError())
                return 42
            return None

        connection = RtiConnection("memory://legacy-definite-admission")
        connection._transport = SimpleNamespace(record=record, close=AsyncMock())
        try:
            with pytest.raises(errors.FederateAlreadyExecutionMember):
                await connection.join_federation(
                    FederationSpec("fed"), federate_name="occupied"
                ).__aenter__()
            assert connection._poison is None
            async with connection.join_federation(
                FederationSpec("fed"), federate_name="retry"
            ) as member:
                assert member.handle == 42
            assert len([method for method, _ in calls if method == "resign_federation"]) == 1
        finally:
            await connection.close()

    asyncio.run(exercise())


@pytest.mark.parametrize(
    "kind",
    [
        errors.FederateNameAlreadyInUse,
        errors.FederateAlreadyExecutionMember,
        errors.SaveInProgress,
        errors.RestoreInProgress,
    ],
)
def test_definite_join_rejection_preserves_exception_and_same_connection_retry(kind):
    async def exercise():
        calls = []
        rejected = kind("definite rejection")

        async def record(method, **kwargs):
            calls.append((method, kwargs))
            if method == "join_federation":
                if kwargs["federate_name"] == "occupied":
                    raise rejected
                return 42
            return None

        transport = SimpleNamespace(record=record, close=AsyncMock())
        connection = RtiConnection("memory://join-rejection")
        connection._transport = transport
        try:
            with pytest.raises(kind) as caught:
                async with connection.join_federation(
                    FederationSpec("fed"), federate_name="occupied"
                ):
                    pytest.fail("rejected join entered")
            assert caught.value is rejected
            assert connection._poison is None
            assert connection._inflight_operations == 0
            assert not connection._active_federate_contexts
            assert not any(method == "resign_federation" for method, _ in calls)
            async with connection.join_federation(
                FederationSpec("fed"), federate_name="retry"
            ) as member:
                assert member.handle == 42
            resigns = [kwargs for method, kwargs in calls if method == "resign_federation"]
            assert len(resigns) == 1 and resigns[0]["federate_handle"] == 42
        finally:
            await connection.close()

    asyncio.run(exercise())


@pytest.mark.parametrize(
    "failure",
    [
        RuntimeError("response lost"),
        errors.RTIinternalError("uncertain"),
        errors.CouldNotCreateLogicalTimeFactory("response incompatible"),
    ],
)
def test_unqualified_failure_keeps_commit_unknown_protection(failure):
    async def exercise():
        async def record(method, **kwargs):
            if method == "join_federation":
                raise failure

        transport = SimpleNamespace(record=record, close=AsyncMock())
        connection = RtiConnection("memory://unknown-join")
        connection._transport = transport
        with pytest.raises(FederationJoinCommitUnknownError) as caught:
            await connection.join_federation(
                FederationSpec("fed"), federate_name="alice"
            ).__aenter__()
        assert caught.value.failure is failure
        assert connection._poison is caught.value
        await connection.recover_commit_unknown()
        assert connection._closed

    asyncio.run(exercise())
