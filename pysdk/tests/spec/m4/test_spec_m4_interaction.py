"""SDK Layer 1 — send_interaction + ReceiveInteraction event delivery.

Implements: IR-PYAPI-1.
"""

from __future__ import annotations

import pytest

from rti1516e import FederationSpec, ReceiveInteraction, RtiConnection


@pytest.mark.spec
@pytest.mark.asyncio
async def test_spec_m4_send_interaction_records(fake_rti) -> None:  # type: ignore[no-untyped-def]
    async with RtiConnection.connect("memory://fake-rti") as rti:
        async with rti.join_federation(FederationSpec(name="demo"), federate_name="alice") as fed:
            await fed.publish_interaction_class("Ping")
            await fed.send_interaction("Ping", parameters={"seq": b"\x00\x00\x00\x01"})
    sends = fake_rti.calls_for("send_interaction")
    assert sends, "expected at least one send_interaction call"
    assert sends[0].args.get("class_name") == "Ping"


@pytest.mark.spec
@pytest.mark.asyncio
async def test_spec_m4_receive_interaction_yielded(fake_rti) -> None:  # type: ignore[no-untyped-def]
    async with RtiConnection.connect("memory://fake-rti") as rti:
        async with rti.join_federation(FederationSpec(name="demo"), federate_name="alice") as fed:
            await fed.subscribe_interaction_class("Ping")
            fake_rti.push_event(
                fed.handle,
                ReceiveInteraction(class_name="Ping", parameters={"seq": 1}, timestamp=None),
            )
            async for evt in fed.events():
                assert isinstance(evt, ReceiveInteraction)
                assert evt.class_name == "Ping"
                break
