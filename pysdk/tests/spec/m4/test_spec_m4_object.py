"""SDK Layer 1 — register_object_instance + update_attributes + DiscoverObjectInstance.

Implements: IR-PYAPI-1.
"""

from __future__ import annotations

import pytest

from rti1516e import DiscoverObjectInstance, FederationSpec, RtiConnection


@pytest.mark.spec
@pytest.mark.asyncio
async def test_spec_m4_register_object_returns_handle(fake_rti) -> None:  # type: ignore[no-untyped-def]
    async with RtiConnection.connect("memory://fake-rti") as rti:
        async with rti.join_federation(FederationSpec(name="demo"), federate_name="alice") as fed:
            await fed.publish_object_class("Vehicle", attributes=["pos"])
            handle = await fed.register_object_instance("Vehicle", instance_name="V1")
            assert isinstance(handle, int)
            assert handle > 0


@pytest.mark.spec
@pytest.mark.asyncio
async def test_spec_m4_update_attributes_records(fake_rti) -> None:  # type: ignore[no-untyped-def]
    async with RtiConnection.connect("memory://fake-rti") as rti:
        async with rti.join_federation(FederationSpec(name="demo"), federate_name="alice") as fed:
            await fed.publish_object_class("Vehicle", attributes=["pos"])
            handle = await fed.register_object_instance("Vehicle")
            await fed.update_attributes(handle, {"pos": b"\x00\x00\x00\x05"})
    updates = fake_rti.calls_for("update_attributes")
    assert updates, "expected at least one update_attributes call"
    assert "pos" in updates[0].args.get("values", {})


@pytest.mark.spec
@pytest.mark.asyncio
async def test_spec_m4_discover_object_event_yielded(fake_rti) -> None:  # type: ignore[no-untyped-def]
    """Push a DiscoverObjectInstance event from the server side; the
    federate's events() async iterator must yield it."""
    async with RtiConnection.connect("memory://fake-rti") as rti:
        async with rti.join_federation(FederationSpec(name="demo"), federate_name="alice") as fed:
            await fed.subscribe_object_class("Vehicle", attributes=["pos"])
            fake_rti.push_event(
                fed.handle,
                DiscoverObjectInstance(object_handle=42, class_name="Vehicle", instance_name="V1"),
            )
            async for evt in fed.events():
                assert isinstance(evt, DiscoverObjectInstance)
                assert evt.object_handle == 42
                break
