"""A contradictory create success must retain evidence and never auto-destroy."""

import asyncio
from types import SimpleNamespace
from unittest.mock import AsyncMock, Mock

import pytest

import rti1516e
from rti1516e import FederationSpec, RtiConnection, errors
from rti1516e._transport import GrpcTransport


@pytest.mark.parametrize("join", [False, True])
def test_contradictory_create_retains_evidence_and_requires_explicit_destroy(join):
    transport = GrpcTransport(Mock(close=AsyncMock()), "grpc://unused")
    transport.federation.CreateFederation = AsyncMock(
        return_value=SimpleNamespace(
            federation_generation=4,
            effective_seed=7,
            logical_time_implementation_name="HLAinteger64Time",
        )
    )
    transport.federation.DestroyFederation = AsyncMock()
    transport.federation.JoinFederation = AsyncMock()
    transport.federation.ResignFederation = AsyncMock()
    conn = RtiConnection("grpc://unused")
    conn._transport = transport

    async def run():
        with pytest.raises(errors.RtiError) as caught:
            if join:
                await conn.join_federation(
                    FederationSpec("created-execution"),
                    federate_name="alice",
                ).__aenter__()
            else:
                await conn.create_federation(FederationSpec("created-execution"))
        failure = caught.value
        assert not isinstance(failure, errors.CouldNotCreateLogicalTimeFactory)
        assert isinstance(failure, errors.RTIinternalError)
        assert isinstance(failure, rti1516e.FederationCreationResponseError)
        assert failure.federation_name == "created-execution"
        assert failure.federation_generation == 4
        assert failure.effective_seed == 7
        assert failure.logical_time_implementation_name == "HLAinteger64Time"
        assert "server already created" in str(failure)
        assert "created-execution" in str(failure)
        assert "HLAinteger64Time" in str(failure)
        assert transport._generation_by_federation["created-execution"] == 4
        transport.federation.DestroyFederation.assert_not_awaited()
        transport.federation.JoinFederation.assert_not_awaited()
        transport.federation.ResignFederation.assert_not_awaited()
        assert conn._poison is None
        await conn.destroy_federation(failure.federation_name)
        request = transport.federation.DestroyFederation.await_args.args[0]
        assert request.federation_name == "created-execution"
        assert request.expected_federation_generation == 4
        await conn.close()
        transport.federation.DestroyFederation.assert_awaited_once()

    asyncio.run(run())
