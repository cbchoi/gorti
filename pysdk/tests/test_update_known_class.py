"""String-key updates resolve in the caller's current object knowledge."""

import asyncio
from types import SimpleNamespace

import pytest

from rti1516e._transport import GrpcTransport, _ensure_generated_path
from rti1516e.errors import AttributeNotDefined

_ensure_generated_path()


@pytest.fixture
def known_transport(monkeypatch):
    calls, lookups = [], []
    state = SimpleNamespace(known=20, failure=None, remote_attributes={})

    class Support:
        def __init__(self, channel, **options):
            assert channel is transport.channel
            assert options == {
                "federation_name": "fed",
                "federate_handle": 7,
                "session_token": b"token",
            }

        async def get_known_object_class_handle(self, obj):
            assert obj == 100
            lookups.append(state.known)
            if state.failure:
                raise state.failure
            return state.known

        async def get_attribute_handle(self, cls, name):
            if (cls, name) in state.remote_attributes:
                return state.remote_attributes[(cls, name)]
            raise AttributeNotDefined(f"{cls}:{name}")

    async def update(request):
        calls.append(request)

    transport = object.__new__(GrpcTransport)
    transport.channel = object()
    transport._federation_name = "fed"
    transport._session_tokens = {7: b"token"}
    transport._object_class_handles = {"Base": 10, "Base.Child": 20}
    transport._attribute_handle_cache = {
        "Base": {"position": 1},
        "Base.Child": {"childValue": 1, "position": 2},
    }
    transport._fom_cache = None
    transport.objects = SimpleNamespace(UpdateAttributeValues=update)
    monkeypatch.setattr("rti1516e.support.SupportClient", Support)
    return transport, state, calls, lookups


def test_string_update_uses_known_class_and_preserves_colliding_names(known_transport):
    transport, _, calls, lookups = known_transport
    asyncio.run(
        transport._update_attributes(
            7, 100, {"position": b"position", "childValue": b"child"}, None
        )
    )
    assert dict(calls[0].attributes) == {2: b"position", 1: b"child"}
    assert lookups == [20]


def test_string_update_rechecks_after_rediscovery(known_transport):
    transport, state, calls, lookups = known_transport

    async def exercise():
        await transport._update_attributes(7, 100, {"position": b"before"}, None)
        state.known = 10
        await transport._update_attributes(7, 100, {"position": b"after"}, None)

    asyncio.run(exercise())
    assert [dict(call.attributes) for call in calls] == [{2: b"before"}, {1: b"after"}]
    assert lookups == [20, 10]


def test_numeric_update_does_not_query_support(known_transport):
    transport, _, calls, lookups = known_transport
    asyncio.run(
        transport._update_attributes(7, 100, {2: b"value"}, 4, tag=b"tag", retraction_handle=9)
    )
    assert dict(calls[0].attributes) == {2: b"value"}
    assert calls[0].logical_time == 4 and calls[0].message_retraction_handle == 9
    assert calls[0].user_supplied_tag == b"tag"
    assert lookups == []


@pytest.mark.parametrize("values", [{"position": b"ok", "absent": b"bad"}, {"childValue": b"bad"}])
def test_unknown_name_rejects_whole_update(known_transport, values):
    transport, state, calls, _ = known_transport
    state.known = 10
    with pytest.raises(AttributeNotDefined):
        asyncio.run(transport._update_attributes(7, 100, values, None))
    assert calls == []


def test_numeric_and_name_alias_cannot_silently_overwrite(known_transport):
    transport, _, calls, _ = known_transport
    with pytest.raises(ValueError, match="same attribute"):
        asyncio.run(transport._update_attributes(7, 100, {"position": b"one", 2: b"two"}, None))
    assert calls == []


def test_failed_knowledge_query_sends_no_update(known_transport):
    transport, state, calls, _ = known_transport
    state.failure = RuntimeError("knowledge unavailable")
    with pytest.raises(RuntimeError, match="knowledge unavailable"):
        asyncio.run(transport._update_attributes(7, 100, {"position": b"value"}, None))
    assert calls == []


@pytest.mark.parametrize("missing", ["class", "attribute"])
def test_name_lookup_falls_back_to_server_on_cache_miss(known_transport, missing):
    transport, state, calls, lookups = known_transport
    if missing == "class":
        transport._object_class_handles.pop("Base.Child")
    else:
        transport._attribute_handle_cache["Base.Child"].pop("position")
    state.remote_attributes[(20, "position")] = 2
    asyncio.run(transport._update_attributes(7, 100, {"position": b"value"}, None))
    assert dict(calls[0].attributes) == {2: b"value"}
    assert lookups == [20]
