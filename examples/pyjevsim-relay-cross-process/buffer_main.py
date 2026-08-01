"""Buffer federate entry point. Spawned by ``runner.py``.

Boot sequence (mirrors ``generator_main`` + ``processor_main`` with
the bidirectional twist that the buffer is BOTH publisher AND
subscriber):

  1. Parse args, open :class:`RtiConnection`, join as "buffer".
  2. Declare ``BufferToProc`` published, ``GenToBuffer`` subscribed.
  3. Run an untimed loop with ``drain_first=True`` so each outer tick
     drains externals BEFORE emitting AND drains again AFTER emitting.
     This mirrors the in-process variant's "two step_once per tick"
     trick (``examples/pyjevsim-relay/runner.py`` lines 178-181):
     external arrivals collected this tick are visible to the queue
     before the next emit decision, and any new arrivals during the
     emit window get caught for the next cycle.
  4. Write the queue + dropped + forwarded lists to the result file
     and exit.
"""

from __future__ import annotations

import asyncio
import sys
from pathlib import Path
from typing import Any

# ruff: noqa: E402
from _federate_common import (  # type: ignore[import-not-found]
    common_parser,
    coordinated_start,
    declare_pub_sub,
    federation_spec,
    run_untimed_loop,
    write_result,
)
from buffer import Buffer  # type: ignore[import-not-found]
from rti1516e.connection import RtiConnection  # type: ignore[import-not-found]


async def run(
    *,
    url: str,
    gen_messages: int,
    capacity: int,
    service_period: int,
    drain_ticks: int,
    tail_ticks: int,
    tick_period: float,
    startup_delay: float,
    ready_file: Path | None,
    start_file: Path | None,
    startup_timeout: float,
) -> dict[str, Any]:
    print(
        f"buffer_main: connecting to {url}", file=sys.stderr, flush=True
    )
    model = Buffer(capacity=capacity, service_period=service_period)
    spec = federation_spec()
    async with RtiConnection.connect(url) as rti, rti.join_federation(
        spec, federate_name="buffer"
    ) as fed:
        print(
            "buffer_main: joined; declaring pub/sub",
            file=sys.stderr, flush=True,
        )
        await fed.enable_asynchronous_delivery()
        await declare_pub_sub(
            fed,
            publish_classes=("BufferToProc",),
            subscribe_classes=("GenToBuffer",),
        )
        await coordinated_start(
            ready_file=ready_file,
            start_file=start_file,
            startup_delay=startup_delay,
            timeout=startup_timeout,
        )
        print(
            "buffer_main: entering tick loop",
            file=sys.stderr, flush=True,
        )
        await run_untimed_loop(
            fed=fed,
            coupled_model=model,
            fom_to_port={"GenToBuffer": "in_msg"},
            out_port_to_class={"out_msg": "BufferToProc"},
            total_ticks=gen_messages + drain_ticks + tail_ticks,
            tick_period=tick_period,
            drain_first=True,
        )
    return {
        "queue_residual": list(model.queue),
        "dropped": list(model.dropped),
        "forwarded": list(model.forwarded),
    }


def main(argv: list[str] | None = None) -> int:
    parser = common_parser("buffer_main")
    args = parser.parse_args(argv)
    try:
        result = asyncio.run(
            run(
                url=args.url,
                gen_messages=args.gen_messages,
                capacity=args.capacity,
                service_period=args.service_period,
                drain_ticks=args.drain_ticks,
                tail_ticks=args.tail_ticks,
                tick_period=args.tick_period,
                startup_delay=args.startup_delay,
                ready_file=args.ready_file,
                start_file=args.start_file,
                startup_timeout=args.startup_timeout,
            )
        )
    except Exception as exc:  # noqa: BLE001
        print(f"buffer_main: {exc}", file=sys.stderr)
        return 1
    write_result(args.result, result)
    return 0


if __name__ == "__main__":
    sys.exit(main())
