"""pyjevsim bridge — DEVS time-advance cycle (NER + grant dispatch).

Implements: FR-PYJ-3.
"""

from __future__ import annotations

import pytest

from pyjevsim_bridge import HLAFederate, PortMapping
from rti1516e.connection import FederationSpec


@pytest.mark.spec
@pytest.mark.asyncio
async def test_spec_m4_internal_cycle_when_no_external_event(stub_coupled, fake_rti) -> None:  # type: ignore[no-untyped-def]
    """ta=2.0, no external event arrives at t < 2 → grant arrives at
    t = 2.0 → bridge runs output_handler + internal_transition."""
    stub_coupled._ta.append(2.0)  # noqa: SLF001 — fixture access for setup
    stub_coupled._outputs.append({})  # noqa: SLF001
    federate = HLAFederate(
        coupled_model=stub_coupled,
        federation=FederationSpec(name="demo"),
        federate_name="alice",
        port_mapping=PortMapping.from_dict({}),
        url="memory://fake-rti",
    )
    await federate.step_once()
    assert stub_coupled.internal_transitions == 1
    assert stub_coupled.external_transitions == []


@pytest.mark.spec
@pytest.mark.asyncio
async def test_spec_m4_external_cycle_when_event_arrives_first(stub_coupled, fake_rti) -> None:  # type: ignore[no-untyped-def]
    """ta=5.0, external event at t=1.0 → bridge calls external_transition,
    NO internal_transition for this cycle."""
    stub_coupled._ta.append(5.0)  # noqa: SLF001
    federate = HLAFederate(
        coupled_model=stub_coupled,
        federation=FederationSpec(name="demo"),
        federate_name="alice",
        port_mapping=PortMapping.from_dict({"in_cmd": "Command"}),
        url="memory://fake-rti",
    )
    await federate.deliver_external("Command", payload="cmd-1")
    await federate.step_once()
    assert stub_coupled.external_transitions, "expected external_transition to fire"
    assert stub_coupled.external_transitions[0].port == "in_cmd"
    assert stub_coupled.internal_transitions == 0


@pytest.mark.spec
@pytest.mark.asyncio
async def test_spec_m4_output_drained_to_send_interaction(stub_coupled, fake_rti) -> None:  # type: ignore[no-untyped-def]
    """When output_handler returns {"out_pos": payload}, bridge calls
    send_interaction("Position", parameters={...})."""
    stub_coupled._ta.append(1.0)  # noqa: SLF001
    stub_coupled._outputs.append({"out_pos": b"\x00\x00\x00\x05"})  # noqa: SLF001
    federate = HLAFederate(
        coupled_model=stub_coupled,
        federation=FederationSpec(name="demo"),
        federate_name="alice",
        port_mapping=PortMapping.from_dict({"out_pos": "Position"}),
        url="memory://fake-rti",
    )
    await federate.step_once()
    sends = fake_rti.calls_for("send_interaction")
    assert sends, "expected at least one send_interaction call"
    assert sends[0].args.get("class_name") == "Position"
