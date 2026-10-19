"""IEEE 1516.1-2010 7.6.5(a-b): distinct confirmation exceptions."""

import asyncio

import grpc
import pytest

from rti1516e import errors
from rti1516e._grpc_errors import InvalidSyncState, translate_rpc_error
from rti1516e._transport import _ensure_generated_path
from rti1516e.ownership import OwnershipClient

_ensure_generated_path()


@pytest.mark.parametrize(
    "name,detail",
    [
        (
            "AttributeDivestitureWasNotRequested",
            "attribute divestiture confirmation was not requested",
        ),
        ("NoAcquisitionPending", "no acquisition pending or willing acquirer for this attribute"),
    ],
)
@pytest.mark.parametrize("metadata", [True, False])
def test_confirmation_error_translation(name, detail, metadata):
    wire = grpc.aio.AioRpcError(
        grpc.StatusCode.FAILED_PRECONDITION,
        grpc.aio.Metadata(),
        grpc.aio.Metadata(("rti-spec-exception", name)) if metadata else grpc.aio.Metadata(),
        details="opaque detail" if metadata else detail,
    )
    expected = getattr(errors, name)
    with pytest.raises(expected) as caught:
        translate_rpc_error(wire)
    assert type(caught.value) is expected
    assert caught.value.__cause__ is wire


def test_ambiguous_legacy_transfer_error_stays_ambiguous():
    wire = grpc.aio.AioRpcError(
        grpc.StatusCode.FAILED_PRECONDITION,
        grpc.aio.Metadata(),
        grpc.aio.Metadata(),
        details="ownership transfer is not in progress for this attribute",
    )
    with pytest.raises(InvalidSyncState):
        translate_rpc_error(wire)


@pytest.mark.parametrize(
    "name", ["AttributeDivestitureWasNotRequested", "NoAcquisitionPending", None]
)
def test_ownership_client_confirmation_translates_trailer(name):
    class Stub:
        async def ConfirmDivestiture(self, request):  # noqa: N802 - generated gRPC method
            assert request.object_handle == 100
            assert list(request.attribute_handles) == [1, 2]
            assert request.tag == b"\x00\x80\xff"
            if name:
                raise grpc.aio.AioRpcError(
                    grpc.StatusCode.FAILED_PRECONDITION,
                    grpc.aio.Metadata(),
                    grpc.aio.Metadata(("rti-spec-exception", name)),
                    details="confirmation rejected",
                )

    client = OwnershipClient.__new__(OwnershipClient)
    client._stub = Stub()
    client._federation_name = "fed"
    client._federate_handle = 1
    call = client.confirm_divestiture(100, [1, 2], tag=b"\x00\x80\xff")
    if name:
        expected = getattr(errors, name, errors.RtiError)
        with pytest.raises(expected) as caught:
            asyncio.run(call)
        assert type(caught.value).__name__ == name
    else:
        assert asyncio.run(call) is None
