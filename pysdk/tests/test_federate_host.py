from __future__ import annotations

from types import SimpleNamespace

import pytest

from rti1516e._transport import GrpcTransport


@pytest.mark.asyncio
@pytest.mark.parametrize("host", [None, "", "sim-host.example", "sim-\uac00.example"])
async def test_join_sends_only_explicit_host(monkeypatch, host):
    if host is None:
        monkeypatch.delenv("GORTI_FEDERATE_HOST", raising=False)
    else:
        monkeypatch.setenv("GORTI_FEDERATE_HOST", host)
    calls = []

    async def join(request, **kwargs):
        from rti.v1 import federation_pb2

        calls.append((request, kwargs))
        return federation_pb2.JoinFederationResponse(federate_handle=7)

    transport = object.__new__(GrpcTransport)
    transport.federation = SimpleNamespace(JoinFederation=join)
    transport._generation_by_federation = {"fed": 1}
    transport._interaction_handles = {"known": 1}
    transport._callback_entry_supported = {}
    transport._start_event_stream = lambda _handle: None
    spec = SimpleNamespace(name="fed", fom_modules=[])
    assert await transport._join_federation(spec, "alice") == 7
    assert len(calls) == 1
    assert calls[0][0].federate_name == "alice"
    expected = {"metadata": (("gorti-federate-host-bin", host.encode("utf-8")),)} if host else {}
    assert calls[0][1] == expected
