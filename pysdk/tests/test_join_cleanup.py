"""Authenticated failed-join cleanup keeps retryable remote ownership."""

import asyncio
from types import SimpleNamespace
from unittest.mock import AsyncMock, Mock

import pytest

from rti1516e import FederationJoinCommitUnknownError, FederationSpec, RtiConnection, errors
from rti1516e._transport import GrpcTransport


def setup_transport():
    channel = Mock(close=AsyncMock())
    transport = GrpcTransport(channel, "grpc://unused")
    transport.federation.CreateFederation = AsyncMock(
        return_value=SimpleNamespace(federation_generation=4)
    )
    transport.federation.JoinFederation = AsyncMock(
        return_value=SimpleNamespace(
            federate_handle=9,
            session_token=b"q" * 32,
            logical_time_implementation_name="unsupportedTime",
        )
    )
    transport._start_event_stream = Mock()
    return transport


def assert_cleanup(request):
    message = request.args[0]
    assert message.federation_name == "fed"
    assert message.federate_handle == 9
    assert request.kwargs["metadata"] == (("gorti-session-token-bin", b"q" * 32),)


@pytest.mark.parametrize("changed_ambient_identity", [False, True])
def test_unsupported_join_compensation_uses_returned_session_token(changed_ambient_identity):
    transport = setup_transport()
    transport._generation_by_federation["fed"] = 4
    if changed_ambient_identity:
        response = transport.federation.JoinFederation.return_value

        async def join(_request):
            transport._federation_name = "another-join"
            return response

        transport.federation.JoinFederation.side_effect = join
    transport.federation.ResignFederation = AsyncMock()
    with pytest.raises(errors.CouldNotCreateLogicalTimeFactory):
        asyncio.run(transport._join_federation(FederationSpec("fed"), "alice"))
    assert_cleanup(transport.federation.ResignFederation.await_args)
    assert transport.session_token(9) == b""
    transport._start_event_stream.assert_not_called()


@pytest.mark.parametrize("cancelled", [False, True])
def test_failed_compensation_retried_before_public_recovery_closes_channel(cancelled):
    transport = setup_transport()
    first = (
        asyncio.CancelledError("initial cleanup cancelled")
        if cancelled
        else RuntimeError("initial cleanup unavailable")
    )
    second = RuntimeError("retry cleanup unavailable")
    transport.federation.ResignFederation = AsyncMock(side_effect=[first, second, None])
    conn = RtiConnection("grpc://unused")
    conn._transport = transport

    async def run():
        with pytest.raises(FederationJoinCommitUnknownError) as caught:
            await conn.join_federation(FederationSpec("fed"), federate_name="alice").__aenter__()
        failure = caught.value.failure
        assert isinstance(failure, BaseExceptionGroup)
        assert failure.subgroup(errors.CouldNotCreateLogicalTimeFactory) is not None
        assert first in failure.exceptions
        transport._start_event_stream.assert_not_called()
        assert transport.session_token(9) == b"q" * 32

        # Recovery must retain the failed join's identity, not ambient transport state.
        transport._federation_name = "other"
        transport._session_tokens[9] = b"x" * 32
        with pytest.raises(RuntimeError, match="retry cleanup unavailable"):
            await conn.recover_commit_unknown()
        transport.channel.close.assert_not_awaited()
        assert conn.transport is transport
        await conn.recover_commit_unknown()
        transport.channel.close.assert_awaited_once()
        assert conn._closed
        for call in transport.federation.ResignFederation.await_args_list:
            assert_cleanup(call)

    asyncio.run(run())
