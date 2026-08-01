"""Run a real pyjevsim generator as an independent gorti federate."""

from __future__ import annotations

import asyncio
import sys
from typing import Any

from _common import (
    RealPyjevsimAdapter,
    common_parser,
    declare_pub_sub,
    federation_spec,
    run_untimed_loop,
    staggered_start,
    write_result,
)
from models import PulseGenerator
from rti1516e.connection import RtiConnection


async def run(
    *, url: str, ticks: int, drain_ticks: int, tick_period: float,
    startup_delay: float,
) -> dict[str, Any]:
    model = PulseGenerator()
    adapter = RealPyjevsimAdapter(
        model, ta_seconds=1.0, out_ports=("out_seq",)
    )
    async with (
        RtiConnection.connect(url) as rti,
        rti.join_federation(
            federation_spec(), federate_name="real-pyjevsim-producer"
        ) as fed,
    ):
        await declare_pub_sub(
            fed,
            publish_classes=("Pulse",),
            subscribe_classes=(),
        )
        await staggered_start(startup_delay)
        await run_untimed_loop(
            fed=fed,
            coupled_model=adapter,
            fom_to_port={},
            out_port_to_class={"out_seq": "Pulse"},
            total_ticks=ticks,
            tick_period=tick_period,
        )
        if drain_ticks > 0:
            await asyncio.sleep(drain_ticks * tick_period)
    return {"published": model.published, "model": type(model).__name__}


def main(argv: list[str] | None = None) -> int:
    args = common_parser("pyjevsim-real-producer").parse_args(argv)
    try:
        result = asyncio.run(
            run(
                url=args.url,
                ticks=args.ticks,
                drain_ticks=args.drain_ticks,
                tick_period=args.tick_period,
                startup_delay=args.startup_delay,
            )
        )
    except Exception as exc:  # noqa: BLE001
        print(f"producer: {exc}", file=sys.stderr)
        return 1
    write_result(args.result, result)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
