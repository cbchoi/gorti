"""Producer federate — registers a Vehicle object and updates its
attributes at every TimeAdvanceGrant.

Time-managed shape:
  - enable_time_regulation(lookahead) — contributes to LBTS.
  - enable_time_constrained — receives grants like every other federate.
  - For each cycle i in [1, cycles]:
       1. issue NER(i * tick_step)
       2. wait for full grant at granted_time
       3. encode current Position + Speed (deterministic functions of
          granted_time so the verifier is deterministic)
       4. update_attributes with timestamp=granted_time (TSO update —
          rtid coordinates delivery order across subscribers)
  - Result JSON: {published: [(grant_time, position, speed), ...]}

The pyjevsim model surface here is minimal — the producer doesn't
receive externals; it just emits attribute updates. external_transition
is a no-op for symmetry with the tracker.
"""

from __future__ import annotations

import asyncio
import struct
import sys
from typing import Any

# ruff: noqa: E402  (sys.path tweaks happen via _federate_common)
from _federate_common import (  # type: ignore[import-not-found]
    common_parser,
    federation_spec,
    wait_for_full_grant,
    write_result,
)
from rti1516e.connection import RtiConnection  # type: ignore[import-not-found]


class VehicleProducer:
    """pyjevsim-style coupled-model surface.

    The producer doesn't have incoming externals — it only emits.
    But the protocol shape (external_transition + time_advance +
    output_handler + internal_transition) matches the tracker so the
    same per-cycle driver works for both.
    """

    def __init__(self, tick_step: float) -> None:
        self.tick_step = tick_step
        self.published: list[dict[str, float]] = []
        self.completed_cycles = 0
        self._output_time: float | None = None

    def time_advance(self) -> float:
        return self.tick_step

    def external_transition(self, port: str, payload: Any) -> None:
        # Producer doesn't subscribe; defensive no-op.
        del port, payload

    def output_handler(self) -> dict[str, Any]:
        if self._output_time is None:
            return {}
        return {
            "Position": self.position_at(self._output_time),
            "Speed": self.speed_at(self._output_time),
        }

    def internal_transition(self) -> None:
        self.completed_cycles += 1
        self._output_time = None

    def prepare_output(self, logical_time: float) -> None:
        self._output_time = logical_time

    def position_at(self, t: float) -> float:
        # Deterministic monotonic-ish curve so the verifier can
        # reconstruct expected values.
        return float(t) * 2.0

    def speed_at(self, t: float) -> float:
        return 5.0  # constant; trivially verifiable


async def run(
    *,
    url: str,
    name: str,
    cycles: int,
    tick_step: float,
    lookahead: float,
    grant_timeout: float,
) -> dict[str, Any]:
    if tick_step < lookahead:
        raise ValueError("tick_step must be at least lookahead for valid TSO timestamps")
    print(f"producer[{name}]: connecting to {url}", file=sys.stderr, flush=True)
    spec = federation_spec()
    model = VehicleProducer(tick_step)
    async with (
        RtiConnection.connect(url) as rti,
        rti.join_federation(spec, federate_name=name) as fed,
    ):
        print(
            f"producer[{name}]: joined as handle {fed.handle}",
            file=sys.stderr,
            flush=True,
        )
        # Time-management opt-in.
        await fed.enable_time_regulation(lookahead)
        await fed.enable_time_constrained()

        # Publish FIRST so the class is on file when subscribers
        # arrive; their subscription RPC matches against the
        # publication set at admit time.
        await fed.publish_object_class("Vehicle", attributes=["Position", "Speed"])

        # Setup barrier: wait for trackers to join + subscribe
        # before registering. The runner spawns the producer first
        # then sleeps 0.6s before spawning trackers; trackers need
        # another beat to complete their subscribe RPC. Registering
        # before they subscribe means they miss the Discover event
        # (rtid does not replay registrations to late subscribers
        # in cut-3). 1.5s covers spawn + subscribe with margin.
        await asyncio.sleep(1.5)

        obj_handle = await fed.register_object_instance(
            "Vehicle",
            instance_name=f"vehicle-{name}",
        )
        print(
            f"producer[{name}]: registered Vehicle (handle={obj_handle}) — "
            "trackers should now Discover",
            file=sys.stderr,
            flush=True,
        )

        # Standard HLA producer pattern: at each cycle
        #   1. update_attributes(timestamp=t) — emits BEFORE the
        #      NMRA so subscribers observe the reflect with
        #      timestamp=t as part of their grant cycle.
        #   2. NMRA(t) — advances producer's logical time to t.
        # Updating with timestamp=t requires
        # t >= current logical time + lookahead. Consecutive grant
        # times differ by tick_step, so tick_step >= lookahead.
        for i in range(1, cycles + 1):
            t = i * model.time_advance()
            model.prepare_output(t)
            output = model.output_handler()
            pos = float(output["Position"])
            spd = float(output["Speed"])
            await fed.update_attributes(
                obj_handle,
                {
                    "Position": struct.pack(">d", pos),
                    "Speed": struct.pack(">d", spd),
                },
                timestamp=t,
            )
            # NMRA (inclusive: LBTS >= t permits) — strict NER
            # would deadlock under lockstep semantics where every
            # federate's contribution equals requested.
            await fed.next_message_request_available(t)
            granted = await wait_for_full_grant(fed, t, grant_timeout)
            model.published.append(
                {
                    "t": granted,
                    "position": pos,
                    "speed": spd,
                }
            )
            model.internal_transition()

        print(
            f"producer[{name}]: done — {len(model.published)} updates",
            file=sys.stderr,
            flush=True,
        )

    return {
        "name": name,
        "role": "producer",
        "object_class": "Vehicle",
        "object_name": f"vehicle-{name}",
        "lookahead": lookahead,
        "cycles": cycles,
        "tick_step": tick_step,
        "model_cycles": model.completed_cycles,
        "published": model.published,
    }


def main(argv: list[str] | None = None) -> int:
    parser = common_parser("producer_main")
    args = parser.parse_args(argv)
    try:
        result = asyncio.run(
            run(
                url=args.url,
                name=args.name,
                cycles=args.cycles,
                tick_step=args.tick_step,
                lookahead=args.lookahead,
                grant_timeout=args.grant_timeout,
            )
        )
    except Exception as exc:  # noqa: BLE001
        print(f"producer[{args.name}]: {exc}", file=sys.stderr)
        return 1
    write_result(args.result, result)
    return 0


if __name__ == "__main__":
    sys.exit(main())
