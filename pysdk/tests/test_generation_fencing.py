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
