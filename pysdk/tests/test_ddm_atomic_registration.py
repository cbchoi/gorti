"""Atomic DDM registration through the real generated client and a fake wire."""

from __future__ import annotations

import grpc
import pytest

from rti1516e import errors as errors_module
from rti1516e._grpc_errors import InvalidSyncState
from rti1516e._transport import _ensure_generated_path
from rti1516e.ddm import AttributeRegions, DDMClient
from rti1516e.errors import AttributeNotDefined, InvalidRegionContext, RtiError
from rti1516e.handles import ObjectClassHandle

_ensure_generated_path()
from rti.v1 import common_pb2, ddm_pb2, object_pb2  # noqa: E402

DDM_REGISTER = "/rti.v1.DDMService/RegisterObjectInstanceWithRegions"
DDM_SET_RANGE_BOUNDS = "/rti.v1.DDMService/SetRangeBounds"
OBJECT_REGISTER = "/rti.v1.ObjectService/RegisterObjectInstance"
SERVER_HANDLE = (1 << 60) + 73


class RegistrationWire:
    def __init__(self, *, object_handle=SERVER_HANDLE, error=None):
        self.calls = []
        self.object_handle = object_handle
        self.error = error

    def unary_unary(self, method, *, request_serializer, response_deserializer, **kwargs):
        async def invoke(request):
            self.calls.append((method, request_serializer(request)))
            if method in (DDM_REGISTER, DDM_SET_RANGE_BOUNDS) and self.error is not None:
                raise self.error
            if method == DDM_REGISTER:
                response = ddm_pb2.RegisterObjectWithRegionsResponse(
                    object_handle=self.object_handle,
                )
            elif method == DDM_SET_RANGE_BOUNDS:
                response = common_pb2.Empty()
            elif method == OBJECT_REGISTER:
                # Make any obsolete first call visible, with a distinct result.
                response = object_pb2.RegisterObjectResponse(
                    object_handle=91, object_name="ordinary-only-name",
                )
            else:
                raise AssertionError(f"unexpected RPC: {method}")
            return response_deserializer(response.SerializeToString())

        return invoke


@pytest.fixture(params=["region-object", ""], ids=["named", "unnamed"])
def object_name(request):
    return request.param


@pytest.fixture
def wire(object_name):
    wire = RegistrationWire()
    yield wire
    assert [method for method, _ in wire.calls] == [DDM_REGISTER]
    request = ddm_pb2.RegisterObjectWithRegionsRequest.FromString(wire.calls[0][1])
    assert request == ddm_pb2.RegisterObjectWithRegionsRequest(
        wire_version=common_pb2.WIRE_VERSION_V1,
        federation_name="atomic-registration",
        federate_handle=23,
        object_class_handle=7,
        object_name=object_name,
        attribute_regions=[
            ddm_pb2.AttributeRegions(attribute_handle=2, region_handles=[17, 11]),
            ddm_pb2.AttributeRegions(attribute_handle=3, region_handles=[29]),
        ],
    )


async def register(wire, object_name):
    client = DDMClient(wire, federation_name="atomic-registration", federate_handle=23)
    return await client.register_object_instance_with_regions(
        ObjectClassHandle(7),
        [AttributeRegions(2, [17, 11]), AttributeRegions(3, [29])],
        object_name=object_name,
    )


@pytest.mark.asyncio
async def test_registration_returns_ddm_server_handle(wire, object_name):
    assert await register(wire, object_name) == SERVER_HANDLE


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("code", "detail", "exception_type"),
    [
        (
            grpc.StatusCode.FAILED_PRECONDITION,
            "region does not belong to federate",
            InvalidSyncState,
        ),
        (grpc.StatusCode.UNAVAILABLE, "DDM service unavailable", grpc.aio.AioRpcError),
        (grpc.StatusCode.NOT_FOUND, "attribute not found", AttributeNotDefined),
    ],
    ids=["rejected", "unavailable", "typed-error"],
)
async def test_registration_failure_has_no_ordinary_register(
    wire, object_name, code, detail, exception_type,
):
    wire.error = grpc.aio.AioRpcError(
        code, grpc.aio.Metadata(), grpc.aio.Metadata(), details=detail,
    )
    with pytest.raises(exception_type, match=detail) as caught:
        await register(wire, object_name)
    if exception_type is grpc.aio.AioRpcError:
        assert caught.value is wire.error
    else:
        assert caught.value.__cause__ is wire.error


@pytest.mark.asyncio
async def test_registration_translates_invalid_region_context_trailer(wire, object_name):
    wire.error = grpc.aio.AioRpcError(
        grpc.StatusCode.INVALID_ARGUMENT,
        grpc.aio.Metadata(),
        grpc.aio.Metadata(("rti-spec-exception", "InvalidRegionContext")),
        details="opaque validation failure",
    )
    with pytest.raises(InvalidRegionContext, match="opaque validation failure") as caught:
        await register(wire, object_name)
    assert caught.value.__cause__ is wire.error


@pytest.mark.asyncio
async def test_registration_rejects_zero_success_handle(wire, object_name):
    wire.object_handle = 0
    with pytest.raises(RtiError, match="zero object handle"):
        await register(wire, object_name)


@pytest.mark.asyncio
async def test_registration_preserves_empty_bindings_and_default_name():
    wire = RegistrationWire()
    client = DDMClient(wire, federation_name="atomic-registration", federate_handle=23)
    handle = await client.register_object_instance_with_regions(7, [])
    assert [method for method, _ in wire.calls] == [DDM_REGISTER]
    assert ddm_pb2.RegisterObjectWithRegionsRequest.FromString(wire.calls[0][1]) == (
        ddm_pb2.RegisterObjectWithRegionsRequest(
            wire_version=common_pb2.WIRE_VERSION_V1,
            federation_name="atomic-registration",
            federate_handle=23,
            object_class_handle=7,
        )
    )
    assert handle == SERVER_HANDLE


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("detail", "trailer_name"),
    [
        ("range bounds are invalid for the dimension", "InvalidRangeBound"),
        ("opaque validation failure", "InvalidRangeBound"),
        ("attribute not found", "InvalidRangeBound"),
        ("opaque validation failure", b"InvalidRangeBound"),
    ],
    ids=["server-detail", "metadata-only", "metadata-precedence", "bytes-metadata"],
)
async def test_set_range_bounds_translates_invalid_range_bound_trailer(detail, trailer_name):
    error = grpc.aio.AioRpcError(
        grpc.StatusCode.INVALID_ARGUMENT,
        grpc.aio.Metadata(),
        grpc.aio.Metadata(("rti-spec-exception", trailer_name)),
        details=detail,
    )
    wire = RegistrationWire(error=error)
    client = DDMClient(wire, federation_name="atomic-registration", federate_handle=23)
    with pytest.raises(RtiError, match=detail) as caught:
        await client.set_range_bounds(17, 11, lower=20, upper=10)
    assert type(caught.value) is errors_module.InvalidRangeBound
    assert caught.value.__cause__ is error
    assert str(caught.value) == detail
    assert [method for method, _ in wire.calls] == [DDM_SET_RANGE_BOUNDS]
    assert ddm_pb2.SetRangeBoundsRequest.FromString(wire.calls[0][1]) == (
        ddm_pb2.SetRangeBoundsRequest(
            wire_version=common_pb2.WIRE_VERSION_V1,
            federation_name="atomic-registration",
            federate_handle=23,
            region_handle=17,
            dimension_handle=11,
            bounds=ddm_pb2.Range(lower=20, upper=10),
        )
    )


@pytest.mark.asyncio
@pytest.mark.parametrize("trailer_name", [None, "UnknownRangeError"])
async def test_set_range_bounds_preserves_unrecognized_error_semantics(trailer_name):
    metadata = grpc.aio.Metadata()
    if trailer_name is not None:
        metadata.add("rti-spec-exception", trailer_name)
    error = grpc.aio.AioRpcError(
        grpc.StatusCode.INVALID_ARGUMENT,
        grpc.aio.Metadata(),
        metadata,
        details="range bounds are invalid for the dimension",
    )
    wire = RegistrationWire(error=error)
    client = DDMClient(wire, federation_name="atomic-registration", federate_handle=23)
    with pytest.raises(grpc.aio.AioRpcError) as caught:
        await client.set_range_bounds(17, 11, lower=20, upper=10)
    assert caught.value is error
    assert [method for method, _ in wire.calls] == [DDM_SET_RANGE_BOUNDS]
