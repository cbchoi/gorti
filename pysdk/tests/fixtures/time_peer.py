"""Test-only Python federate and real pyjevsim bridge participant over JSON lines."""

from __future__ import annotations

import argparse
import asyncio
import importlib.util
import json
import os
import struct
import sys
from contextlib import AsyncExitStack
from pathlib import Path

from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.events import FederationHalted, ReceiveInteraction, TimeAdvanceGrant

TICK = "HLAinteractionRoot.Tick"
ROOT = Path(__file__).resolve().parents[3]


async def collect(fed, command):
    events = []
    target = command.get("until")
    try:
        async with asyncio.timeout(command["timeout_ms"] / 1000):
            async for event in fed.events():
                if isinstance(event, TimeAdvanceGrant):
                    events.append({"kind": "grant", "time": event.time})
                    if target is not None and event.time == target:
                        return events
                elif isinstance(event, ReceiveInteraction):
                    assert event.timestamp is not None
                    events.append(
                        {
                            "kind": "receive",
                            "time": event.timestamp,
                            "class": event.class_name,
                            "value": struct.unpack(">I", event.parameters["seq"])[0],
                        }
                    )
                elif isinstance(event, FederationHalted):
                    raise RuntimeError(f"federation halted: {event.cause}")
    except TimeoutError:
        if target is not None:
            raise TimeoutError(f"grant {target} not observed; events={events}") from None
        return events
    raise RuntimeError("callback stream closed")


def emit(value):
    print(json.dumps(value), flush=True)


def example_model():
    path = ROOT / "examples/pyjevsim-time-advance/regulator_main.py"
    sys.path.insert(0, str(path.parent))
    spec = importlib.util.spec_from_file_location("m21_existing_regulator", path)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.GrantDrivenModel(3.0)


async def run(args):
    spec = FederationSpec(name=args.federation, fom_modules=[args.fom])
    async with AsyncExitStack() as stack:
        if args.bridge:
            from pyjevsim_bridge import HLAFederate, PortMapping

            model = example_model()
            bridge = HLAFederate(
                model,
                spec,
                args.name,
                PortMapping({}, {}),
                url=args.url,
                time_managed=True,
                lookahead=1.0,
                grant_timeout=10,
            )
            stack.push_async_callback(bridge.aclose)
            # Establish all participants before the parent releases the first cycle.
            fed = await bridge._ensure_federate()
        else:
            connection = await stack.enter_async_context(RtiConnection.connect(args.url))
            fed = await stack.enter_async_context(
                connection.join_federation(spec, federate_name=args.name)
            )
            await fed.publish_interaction_class(TICK)
            await fed.subscribe_interaction_class(TICK)
            if args.regulator:
                await fed.enable_time_regulation(1.0)
            await fed.enable_time_constrained()
        emit(
            {
                "ready": True,
                "pid": os.getpid(),
                "handle": fed.handle,
                "language": "pyjevsim_bridge" if args.bridge else "python",
            }
        )
        while line := await asyncio.to_thread(sys.stdin.readline):
            command = json.loads(line)
            op = command["op"]
            response = {"op": op}
            if op == "request":
                method = {
                    "TAR": fed.time_advance_request,
                    "TARA": fed.time_advance_request_available,
                    "NMR": fed.next_message_request,
                    "NMRA": fed.next_message_request_available,
                    "FQR": fed.flush_queue_request,
                }[command["primitive"]]
                await method(command["time"])
                response["primitive"] = command["primitive"]
            elif op == "send":
                await fed.send_interaction(
                    TICK, {"seq": struct.pack(">I", command["value"])}, timestamp=command["time"]
                )
            elif op == "collect":
                response["events"] = await collect(fed, command)
            elif op == "bridge_step" and args.bridge:
                await bridge.step_once()
                response.update(
                    {"model_cycles": model.completed_cycles, "output_calls": model.output_calls}
                )
            elif op == "quit":
                return
            else:
                raise ValueError(f"unknown operation: {op}")
            response["logical_time"] = await fed.query_logical_time()
            emit(response)


def main():
    parser = argparse.ArgumentParser()
    for name in ("url", "federation", "fom", "name"):
        parser.add_argument(f"--{name}", required=True)
    parser.add_argument("--regulator", action="store_true")
    parser.add_argument("--bridge", action="store_true")
    args = parser.parse_args()
    asyncio.run(run(args))


if __name__ == "__main__":
    main()
