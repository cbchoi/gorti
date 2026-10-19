"""Response metadata must confirm, rather than silently change, time selection."""

import asyncio
from types import SimpleNamespace
from unittest.mock import AsyncMock, Mock

import pytest

from rti1516e import FederationSpec, errors
from rti1516e._transport import GrpcTransport


def test_integer64_create_and_join_confirm_exact_selection():
    transport = GrpcTransport(Mock(), "grpc://unused")
    transport.federation.CreateFederation = AsyncMock(return_value=SimpleNamespace(
        federation_generation=4, logical_time_implementation_name="HLAinteger64Time"))
    transport.federation.JoinFederation = AsyncMock(return_value=SimpleNamespace(
        federate_handle=9, session_token=b"s" * 32,
        logical_time_implementation_name="HLAinteger64Time"))
    transport._start_event_stream = Mock()
    spec = FederationSpec("integer", logical_time_implementation_name="HLAinteger64Time")

    async def run():
        await transport._create_federation(spec)
        assert await transport._join_federation(spec, "a") == 9

    asyncio.run(run())
    assert transport.logical_time_implementation_name("integer") == "HLAinteger64Time"
    transport._start_event_stream.assert_called_once_with(9)


@pytest.mark.parametrize("returned", [None, "", "HLAfloat64Time", "unknown"])
def test_integer64_create_rejects_missing_mixed_or_unknown_confirmation(returned):
    transport = GrpcTransport(Mock(), "grpc://unused")
    response = SimpleNamespace(federation_generation=4)
    if returned is not None:
        response.logical_time_implementation_name = returned
    transport.federation.CreateFederation = AsyncMock(return_value=response)
    spec = FederationSpec("integer", logical_time_implementation_name="HLAinteger64Time")
    with pytest.raises(errors.FederationCreationResponseError):
        asyncio.run(transport._create_federation(spec))
    assert "integer" not in transport._logical_time_by_federation
    assert transport._generation_by_federation["integer"] == 4


@pytest.mark.parametrize("returned", [None, "", "HLAfloat64Time", "unknown"])
def test_integer64_join_rejects_missing_mixed_or_unknown_confirmation_and_cleans_up(returned):
    transport = GrpcTransport(Mock(), "grpc://unused")
    transport._generation_by_federation["integer"] = 4
    transport._logical_time_by_federation["integer"] = "HLAinteger64Time"
    response = SimpleNamespace(federate_handle=9, session_token=b"s" * 32)
    if returned is not None:
        response.logical_time_implementation_name = returned
    transport.federation.JoinFederation = AsyncMock(return_value=response)
    transport._resign_federation = AsyncMock()
    transport._start_event_stream = Mock()
    with pytest.raises((errors.CouldNotCreateLogicalTimeFactory, RuntimeError)):
        asyncio.run(transport._join_federation(FederationSpec("integer"), "a"))
    transport._resign_federation.assert_awaited_once_with(
        9, federation_name="integer", session_token=b"s" * 32)
    transport._start_event_stream.assert_not_called()
    assert transport.logical_time_implementation_name("integer") == "HLAinteger64Time"
