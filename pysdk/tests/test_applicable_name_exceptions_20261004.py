"""Public object-name exceptions retain the server's structured identity."""

import grpc
import pytest

import rti1516e
from rti1516e import errors
from rti1516e._grpc_errors import translate_rpc_error


def test_empty_name_set_exception_is_public():
    assert issubclass(errors.NameSetWasEmpty, errors.RtiError)
    assert rti1516e.NameSetWasEmpty is errors.NameSetWasEmpty
    assert "NameSetWasEmpty" in rti1516e.__all__


@pytest.mark.parametrize("name", ["NameSetWasEmpty", "ObjectInstanceNameNotReserved"])
def test_name_service_trailer_overrides_generic_status(name):
    original = grpc.aio.AioRpcError(
        grpc.StatusCode.INVALID_ARGUMENT,
        grpc.aio.Metadata(),
        grpc.aio.Metadata(("rti-spec-exception", name)),
        details="name operation rejected",
    )
    with pytest.raises(getattr(errors, name)) as caught:
        translate_rpc_error(original)
    assert type(caught.value).__name__ == name
    assert caught.value.__cause__ is original
    assert str(caught.value) == "name operation rejected"
