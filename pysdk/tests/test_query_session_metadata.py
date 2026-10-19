"""R7: caller credentials on handleless queries, without new auth requirements."""

import asyncio
from types import SimpleNamespace
from unittest.mock import AsyncMock, Mock

import pytest

from rti1516e._transport import _ensure_generated_path
from rti1516e.connection import Federate
from rti1516e.ddm import DDMClient
from rti1516e.handles import DimensionHandle, RegionHandle
from rti1516e.savepoint import SavepointClient
from rti1516e.support import SupportClient

_ensure_generated_path()

_SUPPORT = [
    ("get_object_class_handle", "GetObjectClassHandle", ("Vehicle",), {"class_handle": 3}),
    ("get_object_class_name", "GetObjectClassName", (3,), {"class_name": "Vehicle"}),
    ("get_attribute_handle", "GetAttributeHandle", (3, "Position"), {"attribute_handle": 4}),
    ("get_attribute_name", "GetAttributeName", (3, 4), {"attribute_name": "Position"}),
    ("get_interaction_class_handle", "GetInteractionClassHandle", ("Honk",), {"class_handle": 5}),
    ("get_interaction_class_name", "GetInteractionClassName", (5,), {"class_name": "Honk"}),
    ("get_parameter_handle", "GetParameterHandle", (5, "Volume"), {"parameter_handle": 6}),
    ("get_parameter_name", "GetParameterName", (5, 6), {"parameter_name": "Volume"}),
    ("get_dimension_handle", "GetDimensionHandle", ("X",), {"dimension_handle": 7}),
    ("get_dimension_name", "GetDimensionName", (7,), {"dimension_name": "X"}),
    ("get_dimension_upper_bound", "GetDimensionUpperBound", (7,), {"upper_bound": 1000}),
    ("get_order_type", "GetOrderType", ("Receive",), {"order_type": 1}),
    ("get_order_name", "GetOrderName", (1,), {"order_name": "Receive"}),
    (
        "get_transportation_type",
        "GetTransportationType",
        ("HLAreliable",),
        {"transportation_type": 1},
    ),
    (
        "get_transportation_name",
        "GetTransportationName",
        (1,),
        {"transportation_name": "HLAreliable"},
    ),
]
_STATUS = [
    (
        "query_federation_save_status",
        "QuerySaveState",
        (),
        {"state": 1, "active_label": "", "federate_statuses": []},
    ),
    (
        "query_federation_restore_status",
        "QueryRestoreState",
        (),
        {"state": 1, "active_label": "", "federate_statuses": []},
    ),
]


@pytest.mark.parametrize("authenticated", [False, True], ids=["legacy", "session"])
@pytest.mark.parametrize(
    "method,rpc,args,response", _SUPPORT + _STATUS, ids=[row[0] for row in _SUPPORT + _STATUS]
)
def test_handleless_query_metadata(method, rpc, args, response, authenticated):
    cls = SupportClient if method.startswith("get_") else SavepointClient
    client = cls.__new__(cls)
    client._federation_name = "query-session"
    client._federate_handle = 42
    client._session_token = b"unit-test-only-not-a-live-session" if authenticated else b""
    call = AsyncMock(return_value=SimpleNamespace(**response))
    client._stub = SimpleNamespace(**{rpc: call})
    result = asyncio.run(getattr(client, method)(*args))
    assert result is not None
    options = call.await_args.kwargs
    if authenticated:
        assert options.get("metadata") == (("gorti-session-token-bin", client._session_token),)
    else:
        assert options == {}
    request = call.await_args.args[0]
    assert "federate_handle" not in request.DESCRIPTOR.fields_by_name


@pytest.mark.parametrize("group", ["support", "savepoint", "ddm"])
def test_federate_binds_query_session_and_preserves_it_after_restore(group):
    token = b"unit-test-only-not-a-live-session"
    transport = SimpleNamespace(
        channel=Mock(), _federation_name="query-session", session_token=lambda _: token
    )
    fed = Federate(connection=Mock(), transport=transport, handle=42, name="caller")
    getattr(fed, group)
    client = getattr(fed, f"_{group}_client")
    assert getattr(client, "_session_token", b"") == token
    fed._restore_identity(77)
    assert client._federate_handle == 77
    assert client._session_token == token


@pytest.mark.parametrize("authenticated", [False, True], ids=["legacy", "session"])
@pytest.mark.parametrize("found", [False, True], ids=["missing", "bounds"])
def test_ddm_query_bounds_session_metadata(authenticated, found):
    client = DDMClient.__new__(DDMClient)
    client._federation_name = "query-session"
    client._federate_handle = 42
    client._session_token = b"ddm-query-session" if authenticated else b""
    call = AsyncMock(
        return_value=SimpleNamespace(
            found=found,
            bounds=SimpleNamespace(lower=1 << 53, upper=(1 << 64) - 1),
        )
    )
    client._stub = SimpleNamespace(QueryBounds=call)
    result = asyncio.run(client.query_bounds(RegionHandle(3), DimensionHandle(7)))
    assert result == ((1 << 53, (1 << 64) - 1) if found else None)
    expected = (
        {"metadata": (("gorti-session-token-bin", client._session_token),)} if authenticated else {}
    )
    assert call.await_args.kwargs == expected
    request = call.await_args.args[0]
    assert request.federation_name == "query-session"
    assert request.region_handle == 3
    assert request.dimension_handle == 7
    # QueryBounds authenticates the member via metadata, not a wire handle.
    assert "federate_handle" not in request.DESCRIPTOR.fields_by_name


def test_ddm_query_options_copy_mutable_token_and_are_call_local():
    token = bytearray(b"ddm-query-session")
    client = DDMClient(
        Mock(), federation_name="query-session", federate_handle=42, session_token=token
    )
    token[:] = b"x" * len(token)
    assert client._session_token == b"ddm-query-session"
    assert isinstance(client._session_token, bytes)
    options = client._query_options()
    assert options == {"metadata": (("gorti-session-token-bin", b"ddm-query-session"),)}
    options.clear()
    assert client._query_options() == {
        "metadata": (("gorti-session-token-bin", b"ddm-query-session"),)
    }


def test_ddm_query_rebinds_member_and_token_after_restore():
    tokens = {42: b"original-session", 77: b"restored-session"}
    transport = SimpleNamespace(
        channel=Mock(), _federation_name="query-session", session_token=tokens.__getitem__
    )
    fed = Federate(connection=Mock(), transport=transport, handle=42, name="caller")
    _ = fed.ddm  # Initialize the lazy client before rebinding its restored identity.
    client = fed._ddm_client
    fed._restore_identity(77)
    call = AsyncMock(
        return_value=SimpleNamespace(found=True, bounds=SimpleNamespace(lower=10, upper=20))
    )
    client._stub = SimpleNamespace(QueryBounds=call)
    assert asyncio.run(client.query_bounds(3, 7)) == (10, 20)
    assert client._federate_handle == 77
    assert call.await_args.kwargs == {"metadata": (("gorti-session-token-bin", tokens[77]),)}
