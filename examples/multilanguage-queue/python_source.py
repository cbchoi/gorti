"""Unchanged pyjevsim atomic models connected to a Go workstation via HLA."""

from __future__ import annotations

import argparse
import asyncio
import json
import os
from pathlib import Path
import struct
import sys

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "examples/pyjevsim-real-model"))
sys.path.insert(0, str(ROOT / "examples/pyjevsim"))

from models import PulseGenerator, PulseSink
from _real_pyjevsim_adapter import RealPyjevsimAdapter
from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.events import (
    FederationHalted, ReceiveInteraction, TimeAdvanceGrant,
    TimeConstrainedEnabled, TimeRegulationEnabled,
)

JOB = "HLAinteractionRoot.Job"
COMPLETION = "HLAinteractionRoot.Completion"


async def run(args):
    directory = Path(args.output)
    services = json.loads((directory / "inputs.json").read_text())["services"]
    generator, sink = PulseGenerator(), PulseSink()
    source = RealPyjevsimAdapter(generator, ta_seconds=1, out_ports=("out_seq",))
    receiver = RealPyjevsimAdapter(sink)
    departures, grants = [], []
    enabled = 0
    async with RtiConnection.connect(args.url) as conn:
        async with conn.join_federation(
            FederationSpec(name=args.federation, fom_modules=[args.fom]),
            federate_name="python-pyjevsim",
        ) as fed:
            await fed.publish_interaction_class(JOB)
            await fed.subscribe_interaction_class(COMPLETION)
            await fed.enable_time_regulation(1)
            await fed.enable_time_constrained()
            events = fed.events()

            def handle(event):
                nonlocal enabled
                if isinstance(event, (TimeRegulationEnabled, TimeConstrainedEnabled)):
                    enabled += 1
                elif isinstance(event, TimeAdvanceGrant):
                    grants.append(event.time)
                elif isinstance(event, ReceiveInteraction):
                    if event.class_name not in (COMPLETION, "Completion") or event.timestamp is None:
                        raise RuntimeError("invalid timestamped Completion")
                    payload = event.parameters["seq"]
                    sequence = struct.unpack(">I", payload)[0]
                    receiver.external_transition("in_seq", {"_payload": payload})
                    departures.append({"sequence": sequence, "finish": event.timestamp})
                elif isinstance(event, FederationHalted):
                    raise RuntimeError(f"federation halted: {event}")

            async def receive():
                await fed.dispatch_event(await anext(events), handle)

            while enabled < 2:
                await receive()
            (directory / "python-ready.json").write_text(json.dumps({"pid": os.getpid(), "handle": fed.handle}))
            while not (directory / "start").exists():
                await asyncio.sleep(0.01)
            for target in range(1, args.horizon + 1):
                await fed.time_advance_request(target)
                while len(grants) < target:
                    await receive()
                if grants[-1] != target:
                    raise RuntimeError("unexpected grant")
                # The model fires only after its logical-time grant. The link
                # adds one modelled time unit, also satisfying lookahead=1.
                if target <= len(services):
                    payload = source.output_handler()["out_seq"]
                    await fed.send_interaction(JOB, {"seq": payload, "service": struct.pack(">I", services[target-1])}, timestamp=target+1)
                    source.internal_transition()
            (directory / "python-result.json").write_text(json.dumps({
                "pid": os.getpid(), "handle": fed.handle,
                "published": generator.published, "received": sink.received,
                "departures": departures, "grants": grants,
            }, indent=2))


async def bounded(args):
    async with asyncio.timeout(120):
        await run(args)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    for field in ("url", "federation", "fom", "output"):
        parser.add_argument("--" + field, required=True)
    parser.add_argument("--horizon", required=True, type=int)
    asyncio.run(bounded(parser.parse_args()))
