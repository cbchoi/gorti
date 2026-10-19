"""Authenticated bounds queries must retain server identity enforcement."""

import asyncio
import os

import grpc
import pytest

from rti1516e._grpc_errors import OwnershipNotPermitted
from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.ddm import DDMClient
from rti1516e.errors import RegionNotCreatedByThisFederate
from tests.spec.m10.test_ieee2010_range_bounds_live import _fom
from tests.test_live_service_e2e import live_artifacts  # noqa: F401 - pytest fixture
from tests.test_live_service_e2e import live_server as _live_server  # noqa: F401 - named fixture

pytestmark = pytest.mark.skipif(
    os.environ.get("GORTI_RUN_LIVE_E2E") != "1", reason="production localhost tests are opt-in"
)


@pytest.mark.parametrize(
    "identity",
    [
        "missing",
        "malformed",
        "unknown",
        "foreign-federation",
        "nonowner",
        "stale",
    ],
)
def test_ddm_query_bounds_rejects_invalid_identity(live_server, identity):
    url, base, _ = live_server

    async def exercise():
        async with asyncio.timeout(30), RtiConnection.connect(url) as owner_connection:
            spec = FederationSpec("ddm-query-auth", [str(_fom(base, 100))])
            async with owner_connection.join_federation(spec, federate_name="owner") as owner:
                space = await owner.ddm.lookup_routing_space("default")
                dimension = await owner.ddm.lookup_dimension(space, "X")
                region = await owner.ddm.create_region(space, [dimension])
                await owner.ddm.set_range_bounds(region, dimension, lower=10, upper=20)
                await owner.ddm.commit_region_modifications([region])
                assert await owner.ddm.query_bounds(region, dimension) == (10, 20)

                async with RtiConnection.connect(url) as other_connection:
                    other_spec = (
                        FederationSpec("foreign-ddm-query-auth", spec.fom_modules)
                        if identity == "foreign-federation"
                        else spec
                    )
                    async with other_connection.join_federation(
                        other_spec, federate_name="other"
                    ) as other:
                        if identity == "stale":
                            stale = DDMClient(
                                other_connection._transport.channel,
                                federation_name=spec.name,
                                federate_handle=other.handle,
                                session_token=other._session_token,
                            )
                        else:
                            await check_identity(owner, other, region, dimension)
                    if identity == "stale":
                        with pytest.raises(OwnershipNotPermitted) as caught:
                            await stale.query_bounds(region, dimension)
                        assert caught.value.__cause__.code() == grpc.StatusCode.PERMISSION_DENIED
                        async with other_connection.join_federation(
                            spec, federate_name="other"
                        ) as rejoined:
                            assert rejoined._session_token != stale._session_token
                            new_region = await rejoined.ddm.create_region(space, [dimension])
                            await rejoined.ddm.set_range_bounds(
                                new_region, dimension, lower=30, upper=40
                            )
                            await rejoined.ddm.commit_region_modifications([new_region])
                            assert await rejoined.ddm.query_bounds(new_region, dimension) == (
                                30,
                                40,
                            )
                            with pytest.raises(OwnershipNotPermitted) as caught:
                                await stale.query_bounds(new_region, dimension)
                            assert (
                                caught.value.__cause__.code() == grpc.StatusCode.PERMISSION_DENIED
                            )
                            assert await rejoined.ddm.query_bounds(new_region, dimension) == (
                                30,
                                40,
                            )
                assert await owner.ddm.query_bounds(region, dimension) == (10, 20)

    async def check_identity(owner, other, region, dimension):
        token = owner._session_token
        if identity == "missing":
            token = b""
        elif identity == "malformed":
            token = b"invalid"
        elif identity == "unknown":
            token = bytes([token[0] ^ 1]) + token[1:]
        else:
            token = other._session_token
        # A claimed owner handle cannot override the token's authenticated member.
        client = DDMClient(
            owner._transport.channel,
            federation_name=spec_name,
            federate_handle=owner.handle,
            session_token=token,
        )
        if identity == "nonowner":
            with pytest.raises(RegionNotCreatedByThisFederate) as caught:
                await client.query_bounds(region, dimension)
            assert (
                dict(caught.value.__cause__.trailing_metadata())["rti-spec-exception"]
                == "RegionNotCreatedByThisFederate"
            )
        else:
            unauthenticated = identity in {"missing", "malformed"}
            with pytest.raises(
                grpc.aio.AioRpcError if unauthenticated else OwnershipNotPermitted
            ) as caught:
                await client.query_bounds(region, dimension)
            expected = (
                grpc.StatusCode.UNAUTHENTICATED
                if unauthenticated
                else grpc.StatusCode.PERMISSION_DENIED
            )
            wire_error = caught.value if unauthenticated else caught.value.__cause__
            assert wire_error.code() == expected

    spec_name = "ddm-query-auth"
    asyncio.run(exercise())
