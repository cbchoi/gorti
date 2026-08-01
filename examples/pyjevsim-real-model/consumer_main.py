"""Run a real pyjevsim sink as an independent gorti federate."""

from __future__ import annotations

import asyncio
import sys
from typing import Any

from _common import (
    RealPyjevsimAdapter,
    common_parser,
    declare_pub_sub,
    federation_spec,
    mark_ready,
    run_drain_only_loop,
    staggered_start,
    write_result,
)
from models import PulseSink
from rti1516e.connection import RtiConnection


async def run(
    *, url: str, ticks: int, drain_ticks: int, tail_ticks: int,
    tick_period: float, startup_delay: float, ready_file: str | None,
) -> dict[str, Any]:
    model = PulseSink()
    adapter = RealPyjevsimAdapter(model, ta_seconds=1.0)
    async with (
        RtiConnection.connect(url) as rti,
        rti.join_federation(
            federation_spec(), federate_name="real-pyjevsim-consumer"
        ) as fed,
    ):
        await declare_pub_sub(
            fed,
            publish_classes=(),
            subscribe_classes=("Pulse",),
        )
        mark_ready(ready_file)
        await staggered_start(startup_delay)
        await run_drain_only_loop(
            fed=fed,
            coupled_model=adapter,
            fom_to_port={"Pulse": "in_seq"},
            total_ticks=ticks + drain_ticks + tail_ticks,
            tick_period=tick_period,
        )
    return {"received": model.received, "model": type(model).__name__}


def main(argv: list[str] | None = None) -> int:
    args = common_parser("pyjevsim-real-consumer").parse_args(argv)
    try:
        result = asyncio.run(
            run(
                url=args.url,
                ticks=args.ticks,
                drain_ticks=args.drain_ticks,
                tail_ticks=args.tail_ticks,
                tick_period=args.tick_period,
                startup_delay=args.startup_delay,
                ready_file=args.ready_file,
            )
        )
    except Exception as exc:  # noqa: BLE001
        print(f"consumer: {exc}", file=sys.stderr)
        return 1
    write_result(args.result, result)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
