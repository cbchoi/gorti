"""Producer federate entry point. Spawned by ``runner.py`` (or by
``producer_run.sh``) as a subprocess; not imported by anything else.

Boot sequence:
  1. Parse the runner-supplied URL + result-path + tunables.
  2. Open an :class:`RtiConnection` to ``rtid``, join the federation
     under the name "producer".
  3. Declare ``ProducerOutput`` as a published interaction class.
     The producer does NOT subscribe -- it's a pure source.
  4. Run an untimed output loop for ``ticks`` cycles. Each cycle calls
     ``Producer.output_handler``, which stamps the
     next monotonic seq and emits it on the ``out_seq`` port; the
     loop translates that into ``send_interaction("ProducerOutput",
     {"_payload": <bytes>})``.
  5. Stay joined for ``drain_ticks`` without publishing, then write
     {"published": [seq, ...]} to the result file and exit.
"""

from __future__ import annotations

import asyncio
import sys
from typing import Any

# ruff: noqa: E402  (local sibling imports follow the package layout)
from _federate_common import (  # type: ignore[import-not-found]
    common_parser,
    declare_pub_sub,
    federation_spec,
    run_untimed_loop,
    staggered_start,
    write_result,
)
from producer import Producer  # type: ignore[import-not-found]
from rti1516e.connection import RtiConnection  # type: ignore[import-not-found]


async def run(
    *,
    url: str,
    ticks: int,
    drain_ticks: int,
    tick_period: float,
    startup_delay: float,
) -> dict[str, Any]:
    print(f"producer_main: connecting to {url}", file=sys.stderr, flush=True)
    model = Producer()
    spec = federation_spec()
    async with (
        RtiConnection.connect(url) as rti,
        rti.join_federation(spec, federate_name="producer") as fed,
    ):
            print(
                "producer_main: joined; declaring publish",
                file=sys.stderr, flush=True,
            )
            await declare_pub_sub(
                fed,
                publish_classes=("ProducerOutput",),
                subscribe_classes=(),
            )
            await staggered_start(startup_delay)
            print(
                "producer_main: entering tick loop",
                file=sys.stderr, flush=True,
            )
            # Producer is a pure publisher; fom_to_port is empty, so
            # drain_externals_now never delivers anything.
            await run_untimed_loop(
                fed=fed,
                coupled_model=model,
                fom_to_port={},
                out_port_to_class={"out_seq": "ProducerOutput"},
                total_ticks=ticks,
                tick_period=tick_period,
            )
            # Remain joined while in-flight deliveries settle, but do
            # not call output_handler during the drain phase.
            for _ in range(drain_ticks):
                await asyncio.sleep(tick_period)
    return {"published": list(model.published)}


def main(argv: list[str] | None = None) -> int:
    parser = common_parser("producer_main")
    args = parser.parse_args(argv)
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
        print(f"producer_main: {exc}", file=sys.stderr)
        return 1
    write_result(args.result, result)
    return 0


if __name__ == "__main__":
    sys.exit(main())
