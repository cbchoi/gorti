"""Regulator federate subprocess.

Cycles:
  1. Open RtiConnection to rtid, join the federation under --name.
  2. enable_time_regulation(--lookahead).
  3. (optional) enable_time_constrained.
  4. For --cycles iterations:
     - issue next_message_request(i * tick-step)
     - wait for *full* TimeAdvanceGrant on events() — accumulating
       any forced (partial) grants per IEEE 1516.1
  5. Write result JSON: name, lookahead, list of grant times.

M22 W3: the M21-era retry-on-TimeAdvancingState backoff loop is
gone. The symptom it worked around was an SDK-side semantics gap,
not a server race: forced grants (clearPending=false in
advance.go::decideGrant) leave the federate in time-advancing
state, and the federate must keep waiting on the same NER until
a full grant arrives. ``wait_for_full_grant`` accumulates forced
grants and returns only on the full grant.
"""

from __future__ import annotations

import asyncio
import sys
from typing import Any

# ruff: noqa: E402  (sys.path tweaks via _federate_common must precede imports)
from _federate_common import (  # type: ignore[import-not-found]
    common_parser,
    federation_spec,
    wait_for_full_grant,
    write_result,
)
from rti1516e.connection import RtiConnection  # type: ignore[import-not-found]


async def run(
    *,
    url: str,
    name: str,
    lookahead: float,
    cycles: int,
    tick_step: float,
    constrained: bool,
    grant_timeout: float,
) -> dict[str, Any]:
    print(f"regulator_main[{name}]: connecting to {url}", file=sys.stderr, flush=True)
    spec = federation_spec()
    grants: list[float] = []
    async with RtiConnection.connect(url) as rti:
        async with rti.join_federation(spec, federate_name=name) as fed:
            print(f"regulator_main[{name}]: joined as handle {fed.handle}",
                  file=sys.stderr, flush=True)

            await fed.enable_time_regulation(lookahead)
            if constrained:
                await fed.enable_time_constrained()

            for i in range(1, cycles + 1):
                t = i * tick_step
                await fed.next_message_request(t)
                grant_t = await wait_for_full_grant(fed, t, grant_timeout)
                grants.append(grant_t)

            print(f"regulator_main[{name}]: done — {len(grants)} grants",
                  file=sys.stderr, flush=True)

    return {
        "name": name,
        "lookahead": lookahead,
        "primitive": "NER",
        "constrained": constrained,
        "grants": grants,
        "ticks_sent": len(grants),
    }


def main(argv: list[str] | None = None) -> int:
    parser = common_parser("regulator_main")
    args = parser.parse_args(argv)
    try:
        result = asyncio.run(run(
            url=args.url,
            name=args.name,
            lookahead=args.lookahead,
            cycles=args.cycles,
            tick_step=args.tick_step,
            constrained=args.constrained,
            grant_timeout=args.grant_timeout,
        ))
    except Exception as exc:  # noqa: BLE001
        print(f"regulator_main[{args.name}]: {exc}", file=sys.stderr)
        return 1
    write_result(args.result, result)
    return 0


if __name__ == "__main__":
    sys.exit(main())
