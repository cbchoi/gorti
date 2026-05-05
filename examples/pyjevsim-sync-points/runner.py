"""End-to-end runner for the canonical HLA bootstrap pattern: 3
federates rendezvous at named sync labels, run a brief exchange,
then rendezvous again at end_simulation, then resign.

The example demonstrates the runner-as-orchestrator workaround for
M12 deferral #1: federation-synchronized callbacks are unwired at
the wire layer in M12 — the proto FederateEvent oneof does not
include sync events yet (see ``docs/reports/M12/agent-c.md``). For
that reason this example does NOT use ``Federate.sync`` (which
requires a real gRPC channel anyway and would not deliver the
Synchronized event back through the in-process transport).

Instead, the runner is the oracle:

  1. Each Participant exposes ``achieve(label)`` which appends to
     its ``achieved`` list (modelled state — would be a real
     ``fed.sync.synchronization_point_achieved`` RPC in a
     production wiring with cut-4 wire support).
  2. The runner calls ``achieve(label)`` on every required peer.
  3. After every peer has voted, the runner calls
     ``mark_synchronized(label)`` on every peer. In a cut-4 world
     this would be a FederationSynchronized event on
     ``fed.events()``.
  4. The running-phase loop drives the bridge's step_once for the
     configured number of ticks.
  5. Repeat the rendezvous for the end_simulation label.

Run from the repo root::

    python3 examples/pyjevsim-sync-points/runner.py

Optional flags::

    --running-ticks N   ticks between start and end (default 10)
    --verbose           per-phase log

Exit code 0 on success, 1 on verify failure.
"""

from __future__ import annotations

import argparse
import asyncio
import sys
from pathlib import Path
from typing import Any

# Make sibling modules importable when run as
# ``python examples/pyjevsim-sync-points/runner.py``.
_HERE = Path(__file__).resolve().parent
if str(_HERE) not in sys.path:
    sys.path.insert(0, str(_HERE))

# Make the pysdk package importable when not pip-installed.
_PYSDK = _HERE.parents[1] / "pysdk"
if str(_PYSDK) not in sys.path:
    sys.path.insert(0, str(_PYSDK))

# ruff: noqa: E402  (sys.path tweaks above must precede project imports)
from participant import Participant

from pyjevsim_bridge import HLAFederate, PortMapping
from rti1516e._inprocess import InProcessTransport
from rti1516e.connection import FederationSpec

FOM_PATH = _HERE / "sync-points-fom.xml"

START_LABEL = "start_simulation"
END_LABEL = "end_simulation"


async def run_once(
    *,
    running_ticks: int = 10,
    verbose: bool = False,
) -> dict[str, Any]:
    """Run the full sync-point bootstrap, run, teardown and return
    a result dict for the caller / test harness."""

    server = InProcessTransport()
    federation = FederationSpec(
        name="pyjevsim-sync-points-example",
        fom_modules=[str(FOM_PATH)],
        seed=0,
    )

    participants: dict[str, Participant] = {
        name: Participant(name=name)
        for name in ("alpha", "beta", "gamma")
    }

    federates: dict[str, HLAFederate] = {}
    for name, model in participants.items():
        federates[name] = HLAFederate(
            coupled_model=model,
            federation=federation,
            federate_name=name,
            port_mapping=PortMapping.from_dict({"out_tick": "Tick"}),
            url="memory://fake-rti",
        )

    # Bring federates up so we have handles to feed into orchestrator
    # bookkeeping. (The bridge does this lazily on the first
    # step_once anyway; we do it eagerly here to keep the rendezvous
    # phase in the timeline before the running-phase events.)
    for fed in federates.values():
        await fed._ensure_federate()  # noqa: SLF001

    phase_log: list[tuple[str, str]] = []  # (phase, label)

    def log_phase(phase: str, label: str = "") -> None:
        phase_log.append((phase, label))
        if verbose:
            line = f"phase: {phase}"
            if label:
                line += f"  label={label!r}"
            print(line, flush=True)

    try:
        # === Phase 1: register start_simulation =================
        # In a wired cut-4 world this is fed.sync.register_synchronization_point;
        # here it is a runner-side bookkeeping entry (the proto
        # layer can't yet emit the callback, see docstring).
        log_phase("register", START_LABEL)
        registered_by = "alpha"  # alpha is the registrar

        # === Phase 2: every federate achieves start_simulation ===
        # The runner explicitly drives each federate's achieve()
        # method. In production each federate would fire its own
        # fed.sync.synchronization_point_achieved when its
        # init-bootstrap is complete.
        log_phase("achieve_loop", START_LABEL)
        for name, model in participants.items():
            model.achieve(START_LABEL)

        # === Phase 3: gate on "all required peers achieved" ======
        # Runner observes the achieve list across every federate
        # and gates the running phase on it. This is the
        # workaround for M12 deferral #1 — the rti's manager
        # would emit FederationSynchronized here but the proto
        # FederateEvent oneof has no variant for it at this cut.
        all_required = list(participants)  # every federate is required
        for name in all_required:
            assert START_LABEL in participants[name].achieved, (
                f"{name} did not achieve {START_LABEL}"
            )
        # Notify each federate that the federation is now synced.
        for name, model in participants.items():
            model.mark_synchronized(START_LABEL)
        log_phase("synchronized", START_LABEL)

        # === Phase 4: running phase ==============================
        # Now that every federate has achieved start_simulation we
        # let the bridge drive the per-tick exchange. Each
        # participant emits one Tick per cycle (its output_handler
        # returns {} until ``running`` is True).
        for model in participants.values():
            model.running = True
        log_phase("running_start")
        for tick in range(running_ticks):
            for name in ("alpha", "beta", "gamma"):
                await federates[name].step_once()
            if verbose:
                print(
                    f"  tick={tick:3d} "
                    + " ".join(
                        f"{n}.sent={len(participants[n].sent_ticks)}"
                        for n in participants
                    ),
                    flush=True,
                )
        for model in participants.values():
            model.running = False
        log_phase("running_end")

        # === Phase 5: register + achieve end_simulation ==========
        # Same dance as phase 1-3, different label. In a real
        # federation either the registrar or a different federate
        # registers the end label; here the runner records both.
        log_phase("register", END_LABEL)
        for name, model in participants.items():
            model.achieve(END_LABEL)
        log_phase("achieve_loop", END_LABEL)
        for name in all_required:
            assert END_LABEL in participants[name].achieved, (
                f"{name} did not achieve {END_LABEL}"
            )
        for name, model in participants.items():
            model.mark_synchronized(END_LABEL)
        log_phase("synchronized", END_LABEL)

    finally:
        for fed in federates.values():
            await fed.aclose()
        log_phase("resign_all")

    return {
        "phase_log": phase_log,
        "registered_by": registered_by,
        "achieved": {name: list(p.achieved) for name, p in participants.items()},
        "synchronized": {
            name: list(p.synchronized) for name, p in participants.items()
        },
        "sent_ticks": {
            name: list(p.sent_ticks) for name, p in participants.items()
        },
        "send_interaction_count": len(server.calls_for("send_interaction")),
        "running_ticks": running_ticks,
        "labels": [START_LABEL, END_LABEL],
    }


def verify(result: dict[str, Any]) -> tuple[bool, str]:
    """End-to-end checks:

    1. Every federate achieved every label exactly once, in order.
    2. Every federate observed federationSynchronized for every
       label exactly once.
    3. Each federate sent exactly ``running_ticks`` Ticks.
    4. The phase log shows the expected ordering: start register →
       achieve_loop → synchronized → running_start → running_end →
       end register → achieve_loop → synchronized → resign_all.
    """
    labels = list(result["labels"])
    achieved = result["achieved"]
    synchronized = result["synchronized"]

    for name, votes in achieved.items():
        if votes != labels:
            return False, (
                f"{name}.achieved={votes!r} but expected {labels!r}"
            )
    for name, syncs in synchronized.items():
        if syncs != labels:
            return False, (
                f"{name}.synchronized={syncs!r} but expected {labels!r}"
            )

    expected_per_fed = result["running_ticks"]
    for name, ticks in result["sent_ticks"].items():
        if len(ticks) != expected_per_fed:
            return False, (
                f"{name} sent {len(ticks)} ticks; expected {expected_per_fed}"
            )
        if ticks != list(range(1, expected_per_fed + 1)):
            return False, (
                f"{name}.sent_ticks not monotonic: {ticks}"
            )

    expected_total = expected_per_fed * len(achieved)
    if result["send_interaction_count"] != expected_total:
        return False, (
            f"send_interaction count={result['send_interaction_count']} "
            f"!= expected {expected_total}"
        )

    expected_phases = [
        ("register", "start_simulation"),
        ("achieve_loop", "start_simulation"),
        ("synchronized", "start_simulation"),
        ("running_start", ""),
        ("running_end", ""),
        ("register", "end_simulation"),
        ("achieve_loop", "end_simulation"),
        ("synchronized", "end_simulation"),
        ("resign_all", ""),
    ]
    actual = result["phase_log"]
    if actual != expected_phases:
        return False, (
            f"phase log mismatch:\nexpected={expected_phases}\nactual={actual}"
        )

    return True, "ok"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--running-ticks", type=int, default=10)
    parser.add_argument("--verbose", action="store_true")
    args = parser.parse_args(argv)

    try:
        result = asyncio.run(
            run_once(
                running_ticks=args.running_ticks,
                verbose=args.verbose,
            )
        )
    except Exception as exc:  # noqa: BLE001
        print(f"runner: {exc}", file=sys.stderr)
        return 1

    ok, msg = verify(result)
    print(
        f"runner: federates={len(result['achieved'])}  "
        f"labels={result['labels']}  "
        f"running_ticks={result['running_ticks']}  "
        f"interactions={result['send_interaction_count']}  "
        f"verify={msg}"
    )
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
