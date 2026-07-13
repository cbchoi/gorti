"""Consumer federate entry point. Spawned by ``runner.py`` (or by
``consumer_run.sh``) as a subprocess; not imported by anything else.

Boot sequence:
  1. Parse the runner-supplied URL + result-path + tunables.
  2. Open an :class:`RtiConnection` to ``rtid``, join as "consumer".
  3. Subscribe to ``ProducerOutput``. The consumer does NOT publish.
  4. Run a drain-only loop for ``ticks + drain_ticks + tail_ticks``
     cycles. Each cycle drains pending interactions into
     ``Consumer.external_transition``.
  5. Decode the wire bytes (4-byte big-endian seq number) recorded on
     ``consumer.received`` into ints and write {"received": [seq, ...]}
     to the result file.

The consumer's tick count is strictly LARGER than the producer's so a
producer emit on its last drain tick is still received before the
consumer resigns. ``tail_ticks`` is the slack.
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
    run_drain_only_loop,
    staggered_start,
    write_result,
)
from consumer import Consumer  # type: ignore[import-not-found]
from rti1516e.connection import RtiConnection  # type: ignore[import-not-found]


async def run(
    *,
    url: str,
    ticks: int,
    drain_ticks: int,
    tail_ticks: int,
    tick_period: float,
    startup_delay: float,
) -> dict[str, Any]:
    print(f"consumer_main: connecting to {url}", file=sys.stderr, flush=True)
    model = Consumer()
    spec = federation_spec()
    async with RtiConnection.connect(url) as rti:
        async with rti.join_federation(spec, federate_name="consumer") as fed:
            print(
                "consumer_main: joined; declaring subscribe",
                file=sys.stderr, flush=True,
            )
            await declare_pub_sub(
                fed,
                publish_classes=(),
                subscribe_classes=("ProducerOutput",),
            )
            await staggered_start(startup_delay)
            print(
                "consumer_main: entering tick loop",
                file=sys.stderr, flush=True,
            )
            await run_drain_only_loop(
                fed=fed,
                coupled_model=model,
                fom_to_port={"ProducerOutput": "in_seq"},
                total_ticks=ticks + drain_ticks + tail_ticks,
                tick_period=tick_period,
            )

    # Decode the 4-byte BE wire payload back into a seq int. The
    # bridge wraps the wire bytes under the "_payload" parameter
    # name (see _federate_common.emit_outputs_now), so consumer.received
    # is shaped [(port_name, {"_payload": <bytes>}), ...].
    received_seqs: list[int] = []
    for _port, params in model.received:
        if not isinstance(params, dict):
            continue
        wire = params.get("_payload")
        if wire is None and len(params) == 1:
            wire = next(iter(params.values()))
        if isinstance(wire, (bytes, bytearray)) and len(wire) == 4:
            received_seqs.append(int.from_bytes(wire, "big"))

    return {"received": received_seqs}


def main(argv: list[str] | None = None) -> int:
    parser = common_parser("consumer_main")
    args = parser.parse_args(argv)
    try:
        result = asyncio.run(
            run(
                url=args.url,
                ticks=args.ticks,
                drain_ticks=args.drain_ticks,
                tail_ticks=args.tail_ticks,
                tick_period=args.tick_period,
                startup_delay=args.startup_delay,
            )
        )
    except Exception as exc:  # noqa: BLE001
        print(f"consumer_main: {exc}", file=sys.stderr)
        return 1
    write_result(args.result, result)
    return 0


if __name__ == "__main__":
    sys.exit(main())
