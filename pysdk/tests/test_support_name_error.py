"""By-name lookup errors are distinct from synchronization and handle errors."""

from unittest.mock import AsyncMock

import pytest

from rti1516e import errors
from rti1516e._grpc_errors import translate_rpc_error
from rti1516e.support import SupportClient
from tests.spec.m39.test_m39_spec_exception_mapping import FakeAioRpcError


@pytest.mark.parametrize("structured", [False, True])
@pytest.mark.asyncio
async def test_missing_object_class_name_has_public_name_not_found(structured):
    import rti1516e

    original = FakeAioRpcError(
        code="NOT_FOUND",
        details='object class "Missing" not found',
        trailing=[("rti-spec-exception", "NameNotFound")] if structured else None,
    )
    client = object.__new__(SupportClient)
    client._federation_name = "fed"
    client._stub = AsyncMock()
    client._stub.GetObjectClassHandle.side_effect = original
    with pytest.raises(errors.RtiError) as caught:
        await client.get_object_class_handle("Missing")
    assert type(caught.value).__name__ == "NameNotFound"
    assert type(caught.value) is errors.NameNotFound
    assert rti1516e.NameNotFound is errors.NameNotFound
    assert "NameNotFound" in rti1516e.__all__
    assert caught.value.__cause__ is original


@pytest.mark.asyncio
async def test_by_name_lookup_preserves_authoritative_other_exception():
    original = FakeAioRpcError(
        code="NOT_FOUND",
        details='object class "Missing" not found',
        trailing=[("rti-spec-exception", "FederateNotExecutionMember")],
    )
    client = object.__new__(SupportClient)
    client._federation_name = "fed"
    client._stub = AsyncMock()
    client._stub.GetObjectClassHandle.side_effect = original
    with pytest.raises(errors.FederateNotExecutionMember):
        await client.get_object_class_handle("Missing")


def test_by_handle_and_unrelated_legacy_errors_keep_their_mapping():
    from rti1516e._grpc_errors import SyncPointNotFound

    with pytest.raises(errors.ObjectClassNotDefined):
        translate_rpc_error(FakeAioRpcError(code="NOT_FOUND", details="object class not found"))
    with pytest.raises(SyncPointNotFound):
        translate_rpc_error(
            FakeAioRpcError(code="NOT_FOUND", details="unknown synchronization label")
        )


@pytest.mark.parametrize(
    "name", ["NameNotFound", "InvalidObjectClassHandle", "InvalidInteractionClassHandle"]
)
def test_support_lookup_exception_metadata_resolves_public_types(name):
    import rti1516e

    expected = getattr(errors, name)
    original = FakeAioRpcError(
        code="NOT_FOUND",
        details="Lookup failed",
        trailing=[("rti-spec-exception", name)],
    )
    with pytest.raises(errors.RtiError) as caught:
        translate_rpc_error(original)
    assert type(caught.value) is expected
    assert getattr(rti1516e, name) is expected
    assert name in rti1516e.__all__
    assert caught.value.__cause__ is original
