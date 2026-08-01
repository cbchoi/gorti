"""Participant federate entry point. Spawned by ``runner.py`` (or by
``alpha_run.sh`` / ``beta_run.sh`` / ``gamma_run.sh``) as a subprocess.

Cross-process sync-point bootstrap:

  1. Connect, join the federation under the configured ``--name``.
  2. Publish + subscribe Tick.
  3. Wait ``--join-settle`` seconds so the other participants have
     time to join (the register's "all currently joined" required
     set must cover everyone).
  4. Try register_synchronization_point(start_simulation). One of the
     three callers wins, the other two swallow a duplicate-label
     error (see ``_federate_common.register_or_swallow``).
  5. Wait for SynchronizationPointAnnounced(start_simulation).
  6. Vote synchronization_point_achieved(start_simulation).
  7. Wait for FederationSynchronized(start_simulation).
  8. Run ``--running-ticks`` cycles emitting a Tick interaction each
     and recording every Tick delivered from peers.
  9. Repeat steps 4-7 for end_simulation.
 10. Resign (handled by the join_federation context manager).
 11. Write the result JSON.
"""

from __future__ import annotations

import asyncio
import sys
from typing import Any

# ruff: noqa: E402
from _federate_common import (  # type: ignore[import-not-found]
    END_LABEL,
    START_LABEL,
    common_parser,
    declare_pub_sub,
    drain_peer_ticks,
    federation_spec,
    register_or_swallow,
    run_running_phase,
    wait_for_announced,
    wait_for_synchronized,
    write_result,
)
from participant import Participant  # type: ignore[import-not-found]
from rti1516e.connection import RtiConnection  # type: ignore[import-not-found]


async def _rendezvous(
    fed,
    model: Participant,
    label: str,
    timeout: float,
    received_during_sync: list[int],
) -> None:
    """One full sync-point dance: register-or-swallow → wait announce
    → vote achieved → wait synchronized.
    """
    print(
        f"participant_main[{fed.name}]: rendezvous start label={label}",
        file=sys.stderr,
        flush=True,
    )
    await register_or_swallow(fed, label)

    def receive_tick(seq: int) -> None:
        received_during_sync.append(seq)
        model.external_transition("in_tick", seq)

    await wait_for_announced(
        fed,
        label,
        timeout=timeout,
        on_tick=receive_tick,
    )
    await fed.sync.synchronization_point_achieved(label)
    model.achieve(label)
    await wait_for_synchronized(
        fed,
        label,
        timeout=timeout,
        on_tick=receive_tick,
    )
    model.mark_synchronized(label)
    print(
        f"participant_main[{fed.name}]: rendezvous done  label={label}",
        file=sys.stderr,
        flush=True,
    )


async def run(
    *,
    url: str,
    name: str,
    running_ticks: int,
    tick_period: float,
    join_settle: float,
    rendezvous_timeout: float,
) -> dict[str, Any]:
    print(f"participant_main[{name}]: connecting to {url}", file=sys.stderr, flush=True)
    model = Participant(name=name)
    spec = federation_spec()
    received_during_sync: list[int] = []
    received_ticks: list[int] = []

    async with (
        RtiConnection.connect(url) as rti,
        rti.join_federation(spec, federate_name=name) as fed,
    ):
        print(
            f"participant_main[{name}]: joined; declaring pub/sub",
            file=sys.stderr,
            flush=True,
        )
        # Bind the federate's name onto the object for log clarity.
        fed.name = name  # type: ignore[attr-defined]
        await declare_pub_sub(
            fed,
            publish_classes=("Tick",),
            subscribe_classes=("Tick",),
        )

        print(
            f"participant_main[{name}]: settling for {join_settle}s",
            file=sys.stderr,
            flush=True,
        )
        await asyncio.sleep(join_settle)

        await _rendezvous(
            fed, model, START_LABEL, rendezvous_timeout, received_during_sync
        )

        print(
            f"participant_main[{name}]: running phase ({running_ticks} ticks)",
            file=sys.stderr,
            flush=True,
        )
        model.running = True
        await run_running_phase(
            fed=fed,
            coupled_model=model,
            fom_to_port={"Tick": "in_tick"},
            out_port_to_class={"out_tick": "Tick"},
            total_ticks=running_ticks,
            tick_period=tick_period,
            received_ticks=received_ticks,
        )
        await drain_peer_ticks(
            fed=fed,
            coupled_model=model,
            fom_to_port={"Tick": "in_tick"},
            received_ticks=received_ticks,
            expected_count=2 * running_ticks,
            timeout=rendezvous_timeout,
        )
        model.running = False

        await _rendezvous(
            fed, model, END_LABEL, rendezvous_timeout, received_during_sync
        )

    return {
        "name": name,
        "achieved": list(model.achieved),
        "synchronized": list(model.synchronized),
        "sent_ticks": list(model.sent_ticks),
        "received_ticks": list(model.received_ticks),
        # Ticks that landed during a sync-rendezvous wait -- usually
        # zero because no peer is in running phase during a
        # rendezvous, but kept for diagnostic completeness.
        "ticks_during_sync": received_during_sync,
    }


def main(argv: list[str] | None = None) -> int:
    parser = common_parser("participant_main")
    args = parser.parse_args(argv)
    try:
        result = asyncio.run(
            run(
                url=args.url,
                name=args.name,
                running_ticks=args.running_ticks,
                tick_period=args.tick_period,
                join_settle=args.join_settle,
                rendezvous_timeout=args.rendezvous_timeout,
            )
        )
    except Exception as exc:  # noqa: BLE001
        print(f"participant_main[{args.name}]: {exc}", file=sys.stderr)
        return 1
    write_result(args.result, result)
    return 0


if __name__ == "__main__":
    sys.exit(main())
