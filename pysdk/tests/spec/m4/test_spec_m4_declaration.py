"""SDK Layer 1 — declaration management (publish/subscribe object + interaction).

Implements: IR-PYAPI-1.
"""

from __future__ import annotations

import pytest

from rti1516e import FederationSpec, RtiConnection


@pytest.mark.spec
@pytest.mark.asyncio
async def test_spec_m4_publish_object_class_records(fake_rti) -> None:  # type: ignore[no-untyped-def]
    async with RtiConnection.connect("memory://fake-rti") as rti:
        async with rti.join_federation(FederationSpec(name="demo"), federate_name="alice") as fed:
            await fed.publish_object_class("Vehicle", attributes=["pos", "vel"])
    calls = fake_rti.calls_for("publish_object_class")
    assert calls, "expected at least one publish_object_class call"
    assert calls[0].args.get("class_name") == "Vehicle"
    assert calls[0].args.get("attributes") == ["pos", "vel"]


@pytest.mark.spec
@pytest.mark.asyncio
async def test_spec_m4_subscribe_object_class_records(fake_rti) -> None:  # type: ignore[no-untyped-def]
    async with RtiConnection.connect("memory://fake-rti") as rti:
        async with rti.join_federation(FederationSpec(name="demo"), federate_name="alice") as fed:
            await fed.subscribe_object_class("Vehicle", attributes=["pos"])
    calls = fake_rti.calls_for("subscribe_object_class")
    assert calls, "expected at least one subscribe_object_class call"
    assert calls[0].args.get("class_name") == "Vehicle"


@pytest.mark.spec
@pytest.mark.asyncio
async def test_spec_m4_publish_interaction_class_records(fake_rti) -> None:  # type: ignore[no-untyped-def]
    async with RtiConnection.connect("memory://fake-rti") as rti:
        async with rti.join_federation(FederationSpec(name="demo"), federate_name="alice") as fed:
            await fed.publish_interaction_class("Ping")
    calls = fake_rti.calls_for("publish_interaction_class")
    assert calls
    assert calls[0].args.get("class_name") == "Ping"
