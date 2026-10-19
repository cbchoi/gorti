import asyncio
from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest

from rti1516e._transport import _ensure_generated_path
from rti1516e.support import SupportClient

_ensure_generated_path()


@pytest.mark.parametrize(
    "method,rpc,args,fields,response,expected",
    [
        (
            "get_update_rate_value",
            "GetUpdateRateValue",
            ("Slow",),
            {"update_rate_designator": "Slow"},
            {"update_rate": 2.5},
            2.5,
        ),
        (
            "get_update_rate_value_for_attribute",
            "GetUpdateRateValueForAttribute",
            (8, 4),
            {"object_handle": 8, "attribute_handle": 4},
            {"update_rate": 0.0},
            0.0,
        ),
        (
            "get_available_dimensions_for_class_attribute",
            "GetAvailableDimensionsForClassAttribute",
            (3, 4),
            {"object_class_handle": 3, "attribute_handle": 4},
            {"dimension_handles": [1, 2]},
            [1, 2],
        ),
        (
            "get_available_dimensions_for_interaction_class",
            "GetAvailableDimensionsForInteractionClass",
            (5,),
            {"interaction_class_handle": 5},
            {"dimension_handles": []},
            [],
        ),
        (
            "get_dimension_handle_set",
            "GetDimensionHandleSet",
            (6,),
            {"region_handle": 6},
            {"dimension_handles": [2]},
            [2],
        ),
    ],
)
def test_support_rate_and_dimension_requests(method, rpc, args, fields, response, expected):
    client = SupportClient.__new__(SupportClient)
    client._federation_name = "f"
    client._federate_handle = 42
    call = AsyncMock(return_value=SimpleNamespace(**response))
    client._stub = SimpleNamespace(**{rpc: call})
    assert asyncio.run(getattr(client, method)(*args)) == expected
    request = call.await_args.args[0]
    assert request.wire_version == 1
    assert request.federation_name == "f"
    assert request.federate_handle == 42
    for name, value in fields.items():
        assert getattr(request, name) == value
