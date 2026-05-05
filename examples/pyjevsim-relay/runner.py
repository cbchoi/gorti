"""End-to-end runner for the 3-federate relay pipeline.

Wires :class:`generator.Generator`, :class:`buffer.Buffer`, and
:class:`processor.Processor` to three ``HLAFederate`` instances on a
shared in-process ``InProcessTransport``. Drives them tick-by-tick
with synchronous fanout between, and at exit reports + verifies the
end-to-end accounting.

Run from the repo root::

    python examples/pyjevsim-relay/runner.py

Optional flags::

    --gen-messages N       generator emits N messages then idles (default 50)
    --capacity K           buffer holds K in flight (default 5)
    --service-period P     buffer emits once every P ticks (default 2)
    --drain-ticks D        D extra ticks after the generator stops (default 30)
    --verbose              print per-tick state

Exit code is 0 on success, 1 on any uncaught exception or accounting
mismatch (forwarded + dropped != published).

Pipeline shape::

    Generator ──GenToBuffer──▶ Buffer ──BufferToProc──▶ Processor

Defaults: 50 emitted, capacity 5, service-period 2 (= every other
tick), 30-tick drain. Steady state during the generator's 50 ticks
sees the queue saturate at 5 and arrivals after that hit the
drop-on-overflow path; the drain phase then flushes the 5 still
queued. Expected verification: forwarded ≈ 30, dropped ≈ 20,
forwarded + dropped == 50.
"""

from __future__ import annotations

import argparse
import asyncio
import sys
from pathlib import Path
from typing import Any

# Make sibling modules importable when run as
# ``python examples/pyjevsim-relay/runner.py``.
_HERE = Path(__file__).resolve().parent
if str(_HERE) not in sys.path:
    sys.path.insert(0, str(_HERE))

# Make the pysdk package importable when the user has not installed it.
_PYSDK = _HERE.parents[1] / "pysdk"
if str(_PYSDK) not in sys.path:
    sys.path.insert(0, str(_PYSDK))

# ruff: noqa: E402  (sys.path tweaks above must precede project imports)
from buffer import Buffer
from generator import Generator
from processor import Processor

from pyjevsim_bridge import HLAFederate, PortMapping
from rti1516e._inprocess import InProcessTransport
from rti1516e.connection import FederationSpec
from rti1516e.events import ReceiveInteraction

FOM_PATH = _HERE / "relay-fom.xml"


async def run_once(
    *,
    gen_messages: int = 50,
    capacity: int = 5,
    service_period: int = 2,
    drain_ticks: int = 30,
    verbose: bool = False,
) -> dict[str, Any]:
    """Run one full Generator → Buffer → Processor exchange and return
    a result dict for the caller / test harness.
    """

    server = InProcessTransport()
    federation = FederationSpec(
        name="pyjevsim-relay-example",
        fom_modules=[str(FOM_PATH)],
        seed=0,
    )

    gen = Generator(stop_after=gen_messages)
    buf = Buffer(capacity=capacity, service_period=service_period)
    proc = Processor()

    gen_federate = HLAFederate(
        coupled_model=gen,
        federation=federation,
        federate_name="generator",
        port_mapping=PortMapping.from_dict({"out_seq": "GenToBuffer"}),
        url="memory://fake-rti",
    )
    buf_federate = HLAFederate(
        coupled_model=buf,
        federation=federation,
        federate_name="buffer",
        port_mapping=PortMapping.from_dict(
            {"in_msg": "GenToBuffer", "out_msg": "BufferToProc"}
        ),
        url="memory://fake-rti",
    )
    proc_federate = HLAFederate(
        coupled_model=proc,
        federation=federation,
        federate_name="processor",
        port_mapping=PortMapping.from_dict({"in_msg": "BufferToProc"}),
        url="memory://fake-rti",
    )

    # Bring federates up so we can read their handles before starting
    # the fan-out task.
    await gen_federate._ensure_federate()  # noqa: SLF001
    await buf_federate._ensure_federate()  # noqa: SLF001
    await proc_federate._ensure_federate()  # noqa: SLF001
    gen_handle = gen_federate._federate.handle  # type: ignore[union-attr]
    buf_handle = buf_federate._federate.handle  # type: ignore[union-attr]
    proc_handle = proc_federate._federate.handle  # type: ignore[union-attr]

    # Per-class subscriber tables. Keep them disjoint so a fanout pass
    # doesn't loop messages back to the publisher.
    subscribers = {
        "GenToBuffer": [buf_handle],
        "BufferToProc": [proc_handle],
    }

    fanout_cursor = 0

    def _fanout_now() -> None:
        """Drain every send_interaction recorded since the last sweep
        into each subscriber's event queue.

        Called twice per tick: once after the generator emits (so the
        buffer sees the new arrival on the same tick), once after the
        buffer emits (so the processor sees the new release on the
        same tick).
        """
        nonlocal fanout_cursor
        end = len(server.calls)
        while fanout_cursor < end:
            call = server.calls[fanout_cursor]
            fanout_cursor += 1
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

    total_ticks = gen_messages + drain_ticks

    try:
        for tick in range(total_ticks):
            # Generator emits during the producing window only; after
            # gen_messages it is idle (output_handler returns {}).
            await gen_federate.step_once()
            _fanout_now()  # GenToBuffer → Buffer's event queue

            # Buffer is both a subscriber AND a publisher. The bridge's
            # cycle semantics (pysdk/pyjevsim_bridge/time_advance.py
            # §4.4: "external arrived earlier than ta → no internal
            # cycle this round") means a single step_once with pending
            # externals drains them and returns without running
            # output_handler. We need two step_once calls per outer
            # tick: one to consume incoming arrivals, one to emit
            # whatever's at the head of the queue.
            await buf_federate.step_once()  # external drain
            await buf_federate.step_once()  # internal cycle (output)
            _fanout_now()  # BufferToProc → Processor's event queue

            # Processor consumes whatever the buffer just released.
            await proc_federate.step_once()

            if verbose:
                print(
                    f"tick={tick:3d}  "
                    f"gen.published={len(gen.published):3d}  "
                    f"buf.queue={len(buf.queue):2d}  "
                    f"buf.forwarded={len(buf.forwarded):3d}  "
                    f"buf.dropped={len(buf.dropped):3d}  "
                    f"proc.received={len(proc.received):3d}",
                    flush=True,
                )
    finally:
        await gen_federate.aclose()
        await buf_federate.aclose()
        await proc_federate.aclose()

    return {
        "published": list(gen.published),
        "forwarded": list(buf.forwarded),
        "dropped": list(buf.dropped),
        "received": list(proc.received),
        "queue_residual": list(buf.queue),
        "send_interactions": len(server.calls_for("send_interaction")),
    }


def verify(result: dict[str, Any]) -> tuple[bool, str]:
    """Assert the end-to-end accounting holds.

    Returns ``(ok, message)``. ``ok == False`` means the example
    detected a violation; the runner exits 1 in that case.
    """
    published = set(result["published"])
    forwarded = set(result["forwarded"])
    dropped = set(result["dropped"])
    received = set(result["received"])
    residual = set(result["queue_residual"])

    # 1. Every published seq is accounted for: forwarded ∪ dropped ∪ residual == published.
    seen = forwarded | dropped | residual
    if seen != published:
        return False, (
            f"accounting leak: published={len(published)} but "
            f"forwarded({len(forwarded)}) ∪ dropped({len(dropped)}) ∪ "
            f"residual({len(residual)}) = {len(seen)} unique seqs"
        )

    # 2. Forwarded and dropped are disjoint (a seq cannot be both).
    overlap = forwarded & dropped
    if overlap:
        return False, f"forwarded ∩ dropped = {sorted(overlap)} (must be empty)"

    # 3. Processor received exactly what the buffer forwarded.
    if received != forwarded:
        only_proc = received - forwarded
        only_buf = forwarded - received
        return False, (
            f"received != forwarded: only-processor={sorted(only_proc)}, "
            f"only-buffer={sorted(only_buf)}"
        )

    return True, "ok"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gen-messages", type=int, default=50)
    parser.add_argument("--capacity", type=int, default=5)
    parser.add_argument("--service-period", type=int, default=2)
    parser.add_argument("--drain-ticks", type=int, default=30)
    parser.add_argument("--verbose", action="store_true")
    args = parser.parse_args(argv)

    try:
        result = asyncio.run(
            run_once(
                gen_messages=args.gen_messages,
                capacity=args.capacity,
                service_period=args.service_period,
                drain_ticks=args.drain_ticks,
                verbose=args.verbose,
            )
        )
    except Exception as exc:  # noqa: BLE001
        print(f"runner: {exc}", file=sys.stderr)
        return 1

    ok, msg = verify(result)
    print(
        f"runner: published={len(result['published'])}  "
        f"forwarded={len(result['forwarded'])}  "
        f"dropped={len(result['dropped'])}  "
        f"received={len(result['received'])}  "
        f"residual={len(result['queue_residual'])}  "
        f"verify={msg}"
    )
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
