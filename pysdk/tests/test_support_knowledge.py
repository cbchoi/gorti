import asyncio
from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest

from rti1516e._transport import _ensure_generated_path
from rti1516e.support import SupportClient

_ensure_generated_path()


@pytest.mark.parametrize(
    "method,rpc,arg,response,expected",
    [
        (
            "get_object_instance_handle",
            "GetObjectInstanceHandle",
            "shared",
            {"object_handle": 73},
            73,
        ),
        (
            "get_object_instance_name",
            "GetObjectInstanceName",
            73,
            {"object_name": "shared"},
            "shared",
        ),
        (
            "get_known_object_class_handle",
            "GetKnownObjectClassHandle",
            73,
            {"class_handle": 19},
            19,
        ),
    ],
)
def test_support_queries_include_caller(method, rpc, arg, response, expected):
    client = SupportClient.__new__(SupportClient)
    client._federation_name = "knowledge"
    client._federate_handle = 42
    call = AsyncMock(return_value=SimpleNamespace(**response))
    client._stub = SimpleNamespace(**{rpc: call})
    assert asyncio.run(getattr(client, method)(arg)) == expected
    call.assert_awaited_once()
    request = call.await_args.args[0]
    assert request.federation_name == "knowledge"
    assert request.federate_handle == 42
    assert request.wire_version == 1
