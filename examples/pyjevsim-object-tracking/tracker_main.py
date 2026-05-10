"""Tracker federate — subscribes to Vehicle.Position + Vehicle.Speed
and feeds reflects into a pyjevsim-style coupled model via
external_transition().

Time-managed shape:
  - enable_time_regulation(lookahead) — contributes to LBTS so the
    producer's NER waits for us before granting.
  - enable_time_constrained — receives grants; reflects with
    timestamp <= grant.time are released by rtid (M22 W2 TSO buffer).
  - For each cycle: NER(i * tick_step) → drain reflects into
    model.external_transition → wait for full grant → record.

The pyjevsim integration point is ``model.external_transition(port,
payload)``. Each ReflectAttributeValues event arrives on port
``"reflect:Vehicle"`` with the attribute-name → bytes dict as payload.
This mirrors the bridge's "incoming external input message" shape
(see examples/pyjevsim-relay-cross-process/_federate_common.py for
the canonical untimed analog).

Result JSON: {received: [(t, position, speed), ...], discovered: [name, ...]}
"""

from __future__ import annotations

import asyncio
import struct
import sys
from typing import Any

# ruff: noqa: E402
from _federate_common import (  # type: ignore[import-not-found]
    common_parser,
    drain_events_into_model,
    federation_spec,
    wait_for_discover,
    write_result,
)
from rti1516e.connection import RtiConnection  # type: ignore[import-not-found]


class VehicleTracker:
    """pyjevsim-style coupled model that records every reflected
    Position + Speed update.

    Receives reflects through the standard pyjevsim
    ``external_transition(port, payload)`` hook. The bridge convention
    is that incoming HLA events arrive as external inputs; the model
    decodes them and updates its internal state, exactly as it would
    for a wire interaction or a port-connected upstream model.
    """

    def __init__(self) -> None:
        self.received: list[dict[str, float]] = []
        self.discovered: list[str] = []
        # ``last_seen_at`` lets the runner attach the granted_time at
        # which a reflect was DELIVERED (vs the upstream timestamp the
        # producer attached). Useful for verifying TSO ordering.
        self._pending_reflect_at: float = 0.0

    # --- pyjevsim CoupledModelProtocol surface ---

    def time_advance(self) -> float:
        return 1.0

    def output_handler(self) -> dict[str, Any]:
        # Tracker emits no model outputs; it's a pure consumer.
        return {}

    def internal_transition(self) -> None:
        return None

    def external_transition(self, port: str, payload: dict[str, Any]) -> None:
        """The pyjevsim external-input hook.

        ``port`` is "reflect:Vehicle" (the FOM class name); ``payload``
        is the attribute-name → wire-bytes dict from the M23
        ReflectAttributeValues event. We decode the doubles and append
        the (position, speed) pair to ``received``.

        For multi-class trackers, the port suffix lets the model route
        per class; this single-class tracker ignores it.
        """
        del port  # single-class tracker
        pos_raw = payload.get("Position")
        spd_raw = payload.get("Speed")
        if pos_raw is None or spd_raw is None:
            return  # incomplete reflect — skip
        try:
            pos = struct.unpack(">d", _to_bytes(pos_raw))[0]
            spd = struct.unpack(">d", _to_bytes(spd_raw))[0]
        except struct.error:
            return
        self.received.append({
            "t": self._pending_reflect_at,
            "position": pos,
            "speed": spd,
        })

    # --- ObjectClassFederateProtocol opt-ins (used by the runner) ---

    def discover_handler(
        self, object_handle: int, class_name: str, instance_name: str,
    ) -> None:
        del object_handle, class_name
        self.discovered.append(instance_name)


def _to_bytes(value: Any) -> bytes:
    if isinstance(value, (bytes, bytearray)):
        return bytes(value)
    if isinstance(value, int):
        # Defensive: shouldn't happen for HLAfloat64BE but covers
        # transports that decode upstream.
        return value.to_bytes(8, "big", signed=True)
    return bytes(value)


async def run(
    *, url: str, name: str, cycles: int, tick_step: float,
    lookahead: float, grant_timeout: float,
) -> dict[str, Any]:
    print(f"tracker[{name}]: connecting to {url}", file=sys.stderr, flush=True)
    spec = federation_spec()
    model = VehicleTracker()
    async with RtiConnection.connect(url) as rti:
        async with rti.join_federation(spec, federate_name=name) as fed:
            print(
                f"tracker[{name}]: joined as handle {fed.handle}",
                file=sys.stderr, flush=True,
            )
            # Tracker is CONSTRAINED-ONLY (not regulating). The
            # producer's lookahead drives LBTS; the tracker's grants
            # are gated so it never advances past the producer's
            # timestamped updates. This is the standard HLA subscriber
            # pattern — a regulating subscriber would have to make its
            # own lookahead promise and would over-constrain the
            # federation. The ``lookahead`` parameter is recorded in
            # the result for cross-federate comparison only; the
            # tracker doesn't actually regulate.
            await fed.enable_time_constrained()
            await fed.subscribe_object_class("Vehicle", attributes=["Position", "Speed"])
            print(
                f"tracker[{name}]: subscribed; waiting for Vehicle Discover",
                file=sys.stderr, flush=True,
            )
            # Barrier: wait for the producer to register the Vehicle.
            # This guarantees the producer is in the federation as a
            # regulator before the tracker issues its first NMRA, so
            # LBTS is bounded by producer.contribution (not +Inf).
            n_disc = await wait_for_discover(fed, model, timeout=grant_timeout)
            print(
                f"tracker[{name}]: discovered {n_disc} Vehicle instance(s); "
                f"entering NMRA loop",
                file=sys.stderr, flush=True,
            )

            for i in range(1, cycles + 1):
                t = i * tick_step
                # NMRA (inclusive) — see producer_main.py for why
                # strict NER would deadlock in lockstep mode.
                await fed.next_message_request_available(t)
                # Drain events into the model UNTIL a full grant
                # arrives. Reflects with timestamp <= t are released
                # by rtid as part of (or just before) the grant.
                _, n_reflect, granted = await drain_events_into_model(
                    fed, model, until_grant=True, requested=t,
                    timeout=grant_timeout,
                )
                if granted is None:
                    raise TimeoutError(
                        f"tracker[{name}]: cycle {i} no grant within {grant_timeout}s",
                    )
                # Re-stamp the most recent reflects with this grant time.
                # (drain_events_into_model populated them with the
                # tracker's last-pending value, which is 0 on first
                # cycle; we overwrite here so the result reflects
                # delivery time, not arbitrary state.)
                for entry in model.received[-n_reflect:]:
                    entry["t"] = granted

            print(
                f"tracker[{name}]: done — {len(model.received)} reflects, "
                f"{len(model.discovered)} discovers",
                file=sys.stderr, flush=True,
            )

    return {
        "name": name,
        "role": "tracker",
        "lookahead": lookahead,
        "cycles": cycles,
        "tick_step": tick_step,
        "received": model.received,
        "discovered": model.discovered,
    }


def main(argv: list[str] | None = None) -> int:
    parser = common_parser("tracker_main")
    args = parser.parse_args(argv)
    try:
        result = asyncio.run(run(
            url=args.url,
            name=args.name,
            cycles=args.cycles,
            tick_step=args.tick_step,
            lookahead=args.lookahead,
            grant_timeout=args.grant_timeout,
        ))
    except Exception as exc:  # noqa: BLE001
        print(f"tracker[{args.name}]: {exc}", file=sys.stderr)
        return 1
    write_result(args.result, result)
    return 0


if __name__ == "__main__":
    sys.exit(main())
