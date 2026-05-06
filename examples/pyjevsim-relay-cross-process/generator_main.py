"""Generator federate entry point. Spawned by ``runner.py`` as a
subprocess; not imported by anything else.

Boot sequence:
  1. Parse the runner-supplied URL + result-path + tunables.
  2. Open an :class:`RtiConnection` to ``rtid``, join the federation
     under the name "generator".
  3. Declare ``GenToBuffer`` as a published interaction class. The
     generator does NOT subscribe -- it's a pure source.
  4. Run an untimed tick loop (see ``_federate_common.run_untimed_loop``)
     for ``gen_messages + drain_ticks`` cycles. The first
     ``gen_messages`` cycles emit one ``GenToBuffer`` each; the
     trailing ``drain_ticks`` are no-ops (output_handler returns
     ``{}``) but keep the federate alive so the buffer + processor
     can finish draining.
  5. Write the published-seq list to the result file and exit.

Why script-by-path, not ``python -m``: the directory name
``pyjevsim-relay-cross-process`` contains hyphens, which are not
legal in a Python package identifier. The runner spawns this file by
absolute path; the sys.path bootstrapping in ``_federate_common``
makes the local imports work either way.
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
from generator import Generator  # type: ignore[import-not-found]
from rti1516e.connection import RtiConnection  # type: ignore[import-not-found]


async def run(
    *,
    url: str,
    gen_messages: int,
    drain_ticks: int,
    tick_period: float,
    startup_delay: float,
) -> dict[str, Any]:
    print(
        f"generator_main: connecting to {url}", file=sys.stderr, flush=True
    )
    model = Generator(stop_after=gen_messages)
    spec = federation_spec()
    async with RtiConnection.connect(url) as rti:
        async with rti.join_federation(
            spec, federate_name="generator"
        ) as fed:
            print(
                "generator_main: joined; declaring publish",
                file=sys.stderr, flush=True,
            )
            await declare_pub_sub(
                fed,
                publish_classes=("GenToBuffer",),
                subscribe_classes=(),
            )
            await staggered_start(startup_delay)
            print(
                "generator_main: entering tick loop",
                file=sys.stderr, flush=True,
            )
            # The generator subscribes to nothing, so fom_to_port is
            # empty -- ``drain_externals_now`` will see no events
            # for it and turn into a no-op.
            await run_untimed_loop(
                fed=fed,
                coupled_model=model,
                fom_to_port={},
                out_port_to_class={"out_seq": "GenToBuffer"},
                total_ticks=gen_messages + drain_ticks,
                tick_period=tick_period,
                drain_first=False,
            )
    return {"published": list(model.published)}


def main(argv: list[str] | None = None) -> int:
    parser = common_parser("generator_main")
    args = parser.parse_args(argv)
    try:
        result = asyncio.run(
            run(
                url=args.url,
                gen_messages=args.gen_messages,
                drain_ticks=args.drain_ticks,
                tick_period=args.tick_period,
                startup_delay=args.startup_delay,
            )
        )
    except Exception as exc:  # noqa: BLE001
        print(f"generator_main: {exc}", file=sys.stderr)
        return 1
    write_result(args.result, result)
    return 0


if __name__ == "__main__":
    sys.exit(main())
