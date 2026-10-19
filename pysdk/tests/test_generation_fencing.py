from __future__ import annotations

from types import SimpleNamespace
from typing import Any, cast

import pytest

from rti1516e._transport import GrpcTransport


class _FederationStub:
    def __init__(self) -> None:
        self.join_request: Any | None = None
        self.destroy_request: Any | None = None

    async def ListFederations(self, _request: Any) -> Any:  # noqa: N802
        from rti.v1 import federation_pb2

        return federation_pb2.ListFederationsResponse(
            federations=[federation_pb2.FederationSummary(name="fed", federation_generation=17)]
        )

    async def JoinFederation(self, request: Any) -> Any:  # noqa: N802
        from rti.v1 import federation_pb2

        self.join_request = request
        return federation_pb2.JoinFederationResponse(federate_handle=(17 << 32) | 1)

    async def DestroyFederation(self, request: Any) -> None:  # noqa: N802
        self.destroy_request = request


def _transport() -> tuple[GrpcTransport, _FederationStub]:
    transport: Any = object.__new__(GrpcTransport)
    stub = _FederationStub()
    transport.federation = stub
    transport._generation_by_federation = {}
    transport._interaction_handles = {"known": 1}
    transport._federation_name = None
    transport._callback_entry_supported = {}
    transport._start_event_stream = lambda _handle: None
    return cast(GrpcTransport, transport), stub


@pytest.mark.asyncio
async def test_join_and_destroy_carry_resolved_generation() -> None:
    transport, stub = _transport()
    spec = SimpleNamespace(name="fed", fom_modules=[])

    handle = await transport._join_federation(spec, "alice")
    assert handle == (17 << 32) | 1
    assert stub.join_request is not None
    assert stub.join_request.HasField("expected_federation_generation")
    assert stub.join_request.expected_federation_generation == 17

    await transport._destroy_federation("fed")
    assert stub.destroy_request is not None
    assert stub.destroy_request.HasField("expected_federation_generation")
    assert stub.destroy_request.expected_federation_generation == 17
    assert "fed" not in transport._generation_by_federation


@pytest.mark.asyncio
@pytest.mark.parametrize("recreated", [False, True])
async def test_successful_resign_requires_current_generation_before_rejoin(recreated):
    from unittest.mock import AsyncMock

    transport, stub = _transport()
    transport._stream_tasks = {}
    stub.ResignFederation = AsyncMock()
    await transport._join_federation(SimpleNamespace(name="fed", fom_modules=[]), "first")
    transport._logical_time_by_federation["fed"] = "HLAfloat64Time"
    transport._requested_mims = {"fed": SimpleNamespace(designator="old.xml", xml=b"old")}
    await transport._resign_federation((17 << 32) | 1)

    current_generation = 18 if recreated else 17
    from rti.v1 import federation_pb2

    stub.ListFederations = AsyncMock(
        return_value=federation_pb2.ListFederationsResponse(
            federations=[
                federation_pb2.FederationSummary(
                    name="fed", federation_generation=current_generation
                )
            ]
        )
    )
    assert await transport._resolve_federation_generation("fed") == current_generation
    stub.ListFederations.assert_awaited_once()
    assert transport._generation_by_federation["fed"] == current_generation
    assert ("fed" in transport._logical_time_by_federation) is (not recreated)
    assert ("fed" in transport._requested_mims) is (not recreated)
    assert await transport._resolve_federation_generation("fed") == current_generation
    stub.ListFederations.assert_awaited_once()


@pytest.mark.asyncio
async def test_failed_resign_keeps_current_generation_fence():
    from unittest.mock import AsyncMock

    transport, stub = _transport()
    transport._stream_tasks = {}
    await transport._join_federation(SimpleNamespace(name="fed", fom_modules=[]), "first")
    stub.ResignFederation = AsyncMock(side_effect=RuntimeError("resign rejected"))
    stub.ListFederations = AsyncMock(side_effect=AssertionError("must retain active generation"))
    with pytest.raises(RuntimeError, match="resign rejected"):
        await transport._resign_federation((17 << 32) | 1)
    assert await transport._resolve_federation_generation("fed") == 17
    stub.ListFederations.assert_not_awaited()


@pytest.mark.asyncio
@pytest.mark.parametrize("catalog_failure", ["rpc", "absent"])
async def test_generation_refresh_failure_never_reuses_stale_generation(catalog_failure):
    from unittest.mock import AsyncMock

    from rti.v1 import federation_pb2

    from rti1516e.errors import FederationExecutionDoesNotExist

    transport, stub = _transport()
    transport._generation_by_federation["fed"] = 17
    transport._stale_federation_generations = {"fed"}
    if catalog_failure == "rpc":
        stub.ListFederations = AsyncMock(side_effect=RuntimeError("catalog unavailable"))
        expected_error = RuntimeError
    else:
        stub.ListFederations = AsyncMock(return_value=federation_pb2.ListFederationsResponse())
        expected_error = FederationExecutionDoesNotExist
    with pytest.raises(expected_error):
        await transport._resolve_federation_generation("fed")
    assert "fed" in transport._stale_federation_generations
    stub.ListFederations = AsyncMock(
        return_value=federation_pb2.ListFederationsResponse(
            federations=[federation_pb2.FederationSummary(name="fed", federation_generation=18)]
        )
    )
    assert await transport._resolve_federation_generation("fed") == 18
    stub.ListFederations.assert_awaited_once()


@pytest.mark.asyncio
async def test_direct_rejoin_does_not_apply_recreated_executions_old_mim_or_time():
    from unittest.mock import AsyncMock

    from rti.v1 import federation_pb2

    transport, stub = _transport()
    transport._generation_by_federation["fed"] = 17
    transport._stale_federation_generations = {"fed"}
    transport._logical_time_by_federation = {"fed": "HLAfloat64Time"}
    transport._requested_mims = {"fed": SimpleNamespace(designator="old.xml", xml=b"old")}
    stub.ListFederations = AsyncMock(
        return_value=federation_pb2.ListFederationsResponse(
            federations=[federation_pb2.FederationSummary(name="fed", federation_generation=18)]
        )
    )
    stub.JoinFederation = AsyncMock(
        return_value=federation_pb2.JoinFederationResponse(
            federate_handle=(18 << 32) | 1, logical_time_implementation_name="HLAinteger64Time"
        )
    )
    assert (
        await transport._join_federation(SimpleNamespace(name="fed", fom_modules=[]), "new")
        == (18 << 32) | 1
    )
    request = stub.JoinFederation.call_args.args[0]
    assert request.expected_federation_generation == 18
    assert not request.HasField("expected_mim_module")
    assert transport._logical_time_by_federation["fed"] == "HLAinteger64Time"
