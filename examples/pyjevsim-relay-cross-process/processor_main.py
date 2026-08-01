"""Processor federate entry point. Spawned by ``runner.py``.

Boot sequence:
  1. Parse args, open :class:`RtiConnection`, join as "processor".
  2. Declare ``BufferToProc`` subscribed. Pure sink -- no publish.
  3. Run a drain-only loop for ``gen_messages + drain_ticks`` ticks.
  4. Write the received-seq list to the result file and exit.
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
    run_drain_only_loop,
    write_result,
)
from processor import Processor  # type: ignore[import-not-found]
from rti1516e.connection import RtiConnection  # type: ignore[import-not-found]


async def run(
    *,
    url: str,
    gen_messages: int,
    drain_ticks: int,
    tail_ticks: int,
    tick_period: float,
    startup_delay: float,
    ready_file: Path | None,
    start_file: Path | None,
    startup_timeout: float,
) -> dict[str, Any]:
    print(
        f"processor_main: connecting to {url}",
        file=sys.stderr, flush=True,
    )
    model = Processor()
    spec = federation_spec()
    async with RtiConnection.connect(url) as rti, rti.join_federation(
        spec, federate_name="processor"
    ) as fed:
        print(
            "processor_main: joined; declaring subscribe",
            file=sys.stderr, flush=True,
        )
        await fed.enable_asynchronous_delivery()
        await declare_pub_sub(
            fed,
            publish_classes=(),
            subscribe_classes=("BufferToProc",),
        )
        await coordinated_start(
            ready_file=ready_file,
            start_file=start_file,
            startup_delay=startup_delay,
            timeout=startup_timeout,
        )
        print(
            "processor_main: entering tick loop",
            file=sys.stderr, flush=True,
        )
        await run_drain_only_loop(
            fed=fed,
            coupled_model=model,
            fom_to_port={"BufferToProc": "in_msg"},
            total_ticks=gen_messages + drain_ticks + tail_ticks,
            tick_period=tick_period,
        )
    return {"received": list(model.received)}


def main(argv: list[str] | None = None) -> int:
    parser = common_parser("processor_main")
    args = parser.parse_args(argv)
    try:
        result = asyncio.run(
            run(
                url=args.url,
                gen_messages=args.gen_messages,
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
        print(f"processor_main: {exc}", file=sys.stderr)
        return 1
    write_result(args.result, result)
    return 0


if __name__ == "__main__":
    sys.exit(main())
