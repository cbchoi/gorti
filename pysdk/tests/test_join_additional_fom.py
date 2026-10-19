"""Additional FOM input must never be silently treated as local-only data."""

import pytest

from rti1516e import Rti1516eAmbassador
from tests.spec.m4._fakes import FakeRtiServer


@pytest.mark.parametrize("modules", [["new.xml"], ["missing.xml", "other.xml"]])
def test_nonempty_additional_fom_forwarded_to_join(modules):
    server = FakeRtiServer()
    ambassador = Rti1516eAmbassador()
    try:
        ambassador.connect(ambassador, "memory://fake-rti")
        ambassador.joinFederationExecution("alice", "existing", modules)
        calls = [call for call in server.calls if call.method == "join_federation"]
        assert len(calls) == 1
        assert calls[0].args["spec"].additional_fom_modules == modules
        assert calls[0].args["spec"].fom_modules == []
        ambassador.resignFederationExecution()
    finally:
        ambassador.disconnect()


@pytest.mark.parametrize("modules", [None, []])
def test_empty_additional_fom_preserves_join_behavior(modules):
    server = FakeRtiServer()
    ambassador = Rti1516eAmbassador()
    try:
        ambassador.connect(ambassador, "memory://fake-rti")
        ambassador.joinFederationExecution("alice", "existing", modules)
        assert [call.method for call in server.calls].count("join_federation") == 1
        ambassador.resignFederationExecution()
    finally:
        ambassador.disconnect()
