"""End-to-end runner for the pyjevsim bridge example.

Wires :class:`producer.Producer` and :class:`consumer.Consumer` to two
``HLAFederate`` instances, runs them concurrently against an in-process
``FakeRtiServer``, and returns the consumer's received list.

Run from the repo root::

    python examples/pyjevsim/runner.py

Exit code is 0 on success, 1 on any uncaught exception.

Notes on the in-process RTI
---------------------------
The runner uses ``InProcessTransport`` from
``pysdk/rti1516e/_inprocess.py`` — the production-suitable in-process
driver extracted at M6 close. Earlier cuts (M4/M5) imported
``FakeRtiServer`` from ``pysdk/tests/spec/m4/_fakes/``; that path was a
documented contract violation (examples must not depend on test
infrastructure) and was the M6-W2 follow-up that resolved it.

``InProcessTransport`` records every call deterministically, exposes
per-federate event queues that the runner's fan-out task drains, and
auto-registers under ``memory://fake-rti`` so existing call sites
keep working without re-plumbing.

The runner stages a cooperative fan-out:

1. Producer's federate calls ``send_interaction`` (recorded on the fake).
2. Each recorded ``send_interaction`` is translated into a
   :class:`rti1516e.events.ReceiveInteraction` and pushed onto every
   subscriber's event queue.
3. The consumer's bridge drains the queue inside its NER loop, hands the
   payload to ``Consumer.external_transition``, and the determinism
   harness reads the resulting ``Consumer.received`` list.

The fan-out runs as an asyncio task started before the federates so
producer and consumer can advance concurrently with no shared lock.
"""

from __future__ import annotations

import argparse
import asyncio
import sys
from pathlib import Path
from typing import Any

# Make sibling modules importable when run as ``python examples/pyjevsim/runner.py``.
_HERE = Path(__file__).resolve().parent
if str(_HERE) not in sys.path:
    sys.path.insert(0, str(_HERE))

# Make the pysdk package importable when the user has not installed it.
_PYSDK = _HERE.parents[1] / "pysdk"
if str(_PYSDK) not in sys.path:
    sys.path.insert(0, str(_PYSDK))

# ruff: noqa: E402  (sys.path tweaks above must precede project imports)
from consumer import Consumer
from producer import Producer

from pyjevsim_bridge import HLAFederate, PortMapping
from rti1516e._inprocess import InProcessTransport
from rti1516e.connection import FederationSpec
from rti1516e.events import ReceiveInteraction

FOM_PATH = (
    _HERE.parents[1] / "tests" / "conformance" / "foms" / "good" / "pyjevsim-bridge.xml"
)


async def run_once(*, ticks: int = 5, seed: int = 0) -> dict[str, Any]:
    """Run one full producer/consumer exchange and return a result dict.

    The dict shape is::

        {
            "received": [(port, parameters), ...],
            "published": [seq_number, ...],
            "send_interactions": int,  # producer-side wire calls
        }

    ``seed`` is currently informational — the harness has no random
    component to seed; deterministic behaviour comes from the deterministic
    FakeRtiServer + the producer's monotonic counter. The parameter exists
    so the M4 determinism harness can pass a seed and so a future
    implementation can wire it to the FederationSpec.
    """

    server = InProcessTransport()
    federation = FederationSpec(
        name="pyjevsim-bridge-example",
        fom_modules=[str(FOM_PATH)],
        seed=seed,
    )

    producer = Producer()
    consumer = Consumer()

    producer_federate = HLAFederate(
        coupled_model=producer,
        federation=federation,
        federate_name="producer",
        port_mapping=PortMapping.from_dict({"out_seq": "ProducerOutput"}),
        url="memory://fake-rti",
    )
    consumer_federate = HLAFederate(
        coupled_model=consumer,
        federation=federation,
        federate_name="consumer",
        port_mapping=PortMapping.from_dict({"in_seq": "ProducerOutput"}),
        url="memory://fake-rti",
    )

    # Bring federates up so we can read their handles before starting
    # the fan-out task. ``aclose`` is wired into the finally block.
    await producer_federate._ensure_federate()  # noqa: SLF001 — orchestrated lifecycle
    await consumer_federate._ensure_federate()  # noqa: SLF001
    consumer_handle = consumer_federate._federate.handle  # type: ignore[union-attr]
    producer_handle = producer_federate._federate.handle  # type: ignore[union-attr]

    subscribers = {"ProducerOutput": [consumer_handle]}

    def _fanout_now() -> None:
        """Translate every send_interaction recorded since the last sweep
        into ReceiveInteraction events on each subscriber's queue.

        Run synchronously between producer.step_once and consumer.step_once
        within each tick — that ordering is what makes the consumer see
        the producer's tick-N output as an external on the same tick,
        instead of racing the consumer's auto-grant.
        """
        nonlocal _fanout_cursor
        end = len(server.calls)
        while _fanout_cursor < end:
            call = server.calls[_fanout_cursor]
            _fanout_cursor += 1
            if call.method != "send_interaction":
                continue
            class_name = call.args.get("class_name")
            if not isinstance(class_name, str):
                continue
            sender = call.args.get("federate_handle")
            for handle in subscribers.get(class_name, ()):
                if handle == sender:
                    continue
                event = ReceiveInteraction(
                    class_name=class_name,
                    parameters=dict(call.args.get("parameters", {})),
                    timestamp=call.args.get("timestamp"),
                )
                server.push_event(handle, event)

    _fanout_cursor = 0

    try:
        # Drive each tick deterministically: producer first (so its
        # send_interaction lands), fan-out (so the consumer's queue has
        # the event before it asks for a grant), then consumer.
        for _ in range(ticks):
            await producer_federate.step_once()
            _fanout_now()
            await consumer_federate.step_once()
    finally:
        await producer_federate.aclose()
        await consumer_federate.aclose()

    return {
        "received": list(consumer.received),
        "published": list(producer.published),
        "send_interactions": len(server.calls_for("send_interaction")),
        "producer_handle": producer_handle,
        "consumer_handle": consumer_handle,
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--ticks", type=int, default=5, help="number of bridge cycles per federate"
    )
    parser.add_argument(
        "--seed",
        type=int,
        default=0,
        help="federation seed (informational; harness is deterministic without it)",
    )
    args = parser.parse_args(argv)

    try:
        result = asyncio.run(run_once(ticks=args.ticks, seed=args.seed))
    except Exception as exc:  # noqa: BLE001 — top-level wrapper, surface message
        print(f"runner: {exc}", file=sys.stderr)
        return 1

    print(
        f"runner: {args.ticks} ticks; "
        f"producer published {len(result['published'])} "
        f"({result['published']}); "
        f"consumer received {len(result['received'])} interaction(s)"
    )
    for entry in result["received"]:
        print(f"  consumer.received: {entry!r}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
