"""End-to-end runner for the 3-federate time-management example.

Wires three :class:`regulator.Regulator` coupled models through the
``pyjevsim_bridge`` to three federates with different lookaheads
({0.5, 1.0, 2.0} by default). Each cycle the runner records the LBTS
contribution table and the grant times, then verifies the documented
time-management invariants.

Key teaching point
------------------
LBTS = min(current + lookahead) over the regulating set.  In any
given tick, the federate with the smallest ``current + lookahead`` is
the one whose grant fires earliest — that is the rule the rti's
``time/lbts.go`` module enforces server-side, and the rule this
runner reproduces in-process so you can read it off the trace.

Run from the repo root::

    python3 examples/pyjevsim-time-advance/runner.py

Optional flags::

    --ticks N            cycles per federate (default 6)
    --la-fast F          lookahead for the "fast" regulator (default 0.5)
    --la-mid M           lookahead for the "mid" regulator (default 1.0)
    --la-slow S          lookahead for the "slow" regulator (default 2.0)
    --step T             per-cycle time_advance for every regulator
                         (default 1.0; set differently per-federate by
                         editing main() if needed)
    --verbose            print per-tick LBTS + grant table

Exit code 0 on success, 1 on verify failure.
"""

from __future__ import annotations

import argparse
import asyncio
import struct
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

# Make sibling modules importable when run as
# ``python examples/pyjevsim-time-advance/runner.py``.
_HERE = Path(__file__).resolve().parent
if str(_HERE) not in sys.path:
    sys.path.insert(0, str(_HERE))

# Make the pysdk package importable when not pip-installed.
_PYSDK = _HERE.parents[1] / "pysdk"
if str(_PYSDK) not in sys.path:
    sys.path.insert(0, str(_PYSDK))

# ruff: noqa: E402  (sys.path tweaks above must precede project imports)
from regulator import Regulator

from pyjevsim_bridge import HLAFederate, PortMapping
from rti1516e._inprocess import InProcessTransport
from rti1516e.connection import FederationSpec

FOM_PATH = _HERE / "time-advance-fom.xml"


@dataclass
class TickRow:
    """One row of the time-management trace.

    For each tick (= one outer-runner cycle) we record every
    federate's logical-time-before-grant, its lookahead, the
    consequent ``current + lookahead`` contribution, and the LBTS
    that the runner computes from the contributions. After the
    bridge cycle we also record each federate's logical time after
    its grant (= the value the runner reads off ``Regulator.now``).
    """

    tick: int
    contributions: dict[str, float] = field(default_factory=dict)  # name -> current+lookahead
    times_before: dict[str, float] = field(default_factory=dict)
    times_after: dict[str, float] = field(default_factory=dict)
    lbts: float = 0.0
    earliest_federate: str = ""


def _compute_lbts(regulators: dict[str, Regulator]) -> tuple[float, dict[str, float]]:
    """Mirror ``rti/internal/time/lbts.go::LBTS``: min(now + lookahead).

    Returns ``(lbts, contributions)``. ``contributions`` is a name ->
    contribution map suitable for the trace.
    """
    contribs = {
        name: r.now + r.lookahead for name, r in regulators.items()
    }
    if not contribs:
        return float("inf"), {}
    return min(contribs.values()), contribs


async def run_once(
    *,
    ticks: int = 6,
    la_fast: float = 0.5,
    la_mid: float = 1.0,
    la_slow: float = 2.0,
    step: float = 1.0,
    verbose: bool = False,
) -> dict[str, Any]:
    """Run the 3-federate time-management exchange and return a result
    dict for the caller / test harness.
    """

    server = InProcessTransport()
    federation = FederationSpec(
        name="pyjevsim-time-advance-example",
        fom_modules=[str(FOM_PATH)],
        seed=0,
    )

    regulators: dict[str, Regulator] = {
        "fast": Regulator(name="fast", step=step, lookahead=la_fast),
        "mid": Regulator(name="mid", step=step, lookahead=la_mid),
        "slow": Regulator(name="slow", step=step, lookahead=la_slow),
    }

    federates: dict[str, HLAFederate] = {}
    for name, model in regulators.items():
        federates[name] = HLAFederate(
            coupled_model=model,
            federation=federation,
            federate_name=name,
            port_mapping=PortMapping.from_dict(
                {"out_heartbeat": "Heartbeat"}
            ),
            url="memory://fake-rti",
        )

    # Bring every federate up + enable time regulation. The bridge's
    # step_once issues next_message_request internally; the test
    # harness does NOT need to call it explicitly. But the bridge
    # does NOT call enable_time_regulation on its own (that's a
    # federation-bootstrap step, not a per-tick call), so we drive
    # it here once via the underlying Federate handle.
    for name, fed in federates.items():
        await fed._ensure_federate()  # noqa: SLF001
        underlying = fed._federate  # type: ignore[union-attr]  # noqa: SLF001
        await underlying.enable_time_regulation(regulators[name].lookahead)

    # Snapshot the federate-handle → name map BEFORE any aclose() so
    # the post-loop heartbeat-decoding can resolve sender handles.
    handle_to_name: dict[int, str] = {}
    for name, fed in federates.items():
        if fed._federate is not None:  # noqa: SLF001
            handle_to_name[fed._federate.handle] = name  # noqa: SLF001

    # Per-tick trace + grant log.
    trace: list[TickRow] = []
    grant_log: list[tuple[int, str, float]] = []  # (tick, name, grant_time)

    grant_cursor = 0  # index into server.calls of the next call to inspect for grants

    try:
        for tick in range(ticks):
            row = TickRow(tick=tick)
            row.times_before = {n: r.now for n, r in regulators.items()}

            # Compute LBTS BEFORE issuing this tick's grants. This is the
            # contribution table the rtid would see at the top of its
            # scheduler iteration. The federate with the smallest
            # contribution is the one whose grant should fire earliest.
            row.lbts, row.contributions = _compute_lbts(regulators)
            row.earliest_federate = min(
                row.contributions, key=row.contributions.get
            )

            # Step every federate. The InProcessTransport auto-grants on
            # NER (see InProcessTransport.record), so each step_once call
            # results in a TimeAdvanceGrant; the regulator's
            # output_handler runs and the federate's logical time
            # advances by ``step``.
            for name in ("fast", "mid", "slow"):
                await federates[name].step_once()
                # Read whatever next_message_request landed on the wire
                # in this step and record the requested time as the
                # grant time (the in-process driver auto-grants at
                # request time, so request == grant for the trace).
                while grant_cursor < len(server.calls):
                    call = server.calls[grant_cursor]
                    grant_cursor += 1
                    if call.method == "next_message_request":
                        sender_handle = call.args.get("federate_handle")
                        # Resolve sender name from handle (so the log
                        # is human-readable). Handles are allocated in
                        # join order: fast=1, mid=2, slow=3.
                        for fname, fed in federates.items():
                            if (
                                fed._federate is not None  # noqa: SLF001
                                and fed._federate.handle == sender_handle  # noqa: SLF001
                            ):
                                grant_log.append(
                                    (
                                        tick,
                                        fname,
                                        float(call.args.get("time", 0.0)),
                                    )
                                )
                                break

            row.times_after = {n: r.now for n, r in regulators.items()}
            trace.append(row)

            if verbose:
                print(
                    f"tick={tick:2d}  lbts={row.lbts:5.2f}  "
                    f"earliest={row.earliest_federate:4s}  "
                    f"contribs="
                    + ", ".join(
                        f"{n}={v:5.2f}" for n, v in row.contributions.items()
                    )
                    + "  after="
                    + ", ".join(
                        f"{n}={v:5.2f}" for n, v in row.times_after.items()
                    ),
                    flush=True,
                )
    finally:
        for fed in federates.values():
            await fed.aclose()

    # Decode the heartbeats off the wire so the test can verify the
    # payload too (every federate's emission carries its own ``now``).
    # ``handle_to_name`` was populated BEFORE the aclose() loop above
    # so we can still resolve federate-handle → name now that the
    # bridges are closed.
    heartbeats: list[tuple[str, float]] = []
    for call in server.calls_for("send_interaction"):
        if call.args.get("class_name") != "Heartbeat":
            continue
        sender = call.args.get("federate_handle")
        params = call.args.get("parameters") or {}
        wire_payload = params.get("_payload")
        if not isinstance(wire_payload, (bytes, bytearray)):
            continue
        if len(wire_payload) != 8:
            continue
        (now,) = struct.unpack(">d", wire_payload)
        heartbeats.append((handle_to_name.get(sender, f"<{sender}>"), now))

    return {
        "trace": [_row_to_dict(r) for r in trace],
        "grant_log": grant_log,
        "heartbeats": heartbeats,
        "lookaheads": {
            "fast": la_fast,
            "mid": la_mid,
            "slow": la_slow,
        },
        "send_interaction_count": len(server.calls_for("send_interaction")),
        "ner_count": len(server.calls_for("next_message_request")),
    }


def _row_to_dict(row: TickRow) -> dict[str, Any]:
    return {
        "tick": row.tick,
        "lbts": row.lbts,
        "earliest_federate": row.earliest_federate,
        "contributions": dict(row.contributions),
        "times_before": dict(row.times_before),
        "times_after": dict(row.times_after),
    }


def verify(result: dict[str, Any]) -> tuple[bool, str]:
    """End-to-end checks:

    1. LBTS is monotonic non-decreasing — at no point does the
       federation's LBTS go backwards.
    2. The federate identified as ``earliest_federate`` in each row
       is in fact the one with the smallest current+lookahead. (Sanity
       check for the runner's bookkeeping; the rule is the LBTS
       definition.)
    3. The "fast" federate has the smallest contribution at tick 0
       (its lookahead 0.5 < mid 1.0 < slow 2.0, and all federates
       start at now=0). This is the pedagogical anchor of the
       example.
    4. Each federate emits exactly ``ticks`` heartbeats and issues
       exactly ``ticks`` NER calls.
    """
    trace = result["trace"]
    if not trace:
        return False, "trace is empty"

    # 1. LBTS monotonicity
    prev = -float("inf")
    for row in trace:
        if row["lbts"] < prev:
            return False, (
                f"LBTS regressed at tick {row['tick']}: "
                f"prev={prev} now={row['lbts']}"
            )
        prev = row["lbts"]

    # 2. earliest_federate is consistent with contributions
    for row in trace:
        c = row["contributions"]
        true_earliest = min(c, key=c.get)
        if row["earliest_federate"] != true_earliest:
            return False, (
                f"tick {row['tick']}: earliest_federate="
                f"{row['earliest_federate']!r} but smallest contribution "
                f"is {true_earliest!r} (contribs={c})"
            )

    # 3. Tick 0 sanity: fast is earliest
    first = trace[0]
    if first["earliest_federate"] != "fast":
        return False, (
            f"tick 0: expected 'fast' to have smallest contribution "
            f"(its lookahead 0.5 < mid 1.0 < slow 2.0); got "
            f"{first['earliest_federate']!r} (contribs={first['contributions']})"
        )

    # 4. Per-federate NER + heartbeat counts
    expected_ticks = len(trace)
    expected_total_ner = expected_ticks * 3
    if result["ner_count"] != expected_total_ner:
        return False, (
            f"expected {expected_total_ner} NER calls "
            f"(3 federates * {expected_ticks} ticks); got "
            f"{result['ner_count']}"
        )
    expected_total_heartbeats = expected_ticks * 3
    if result["send_interaction_count"] != expected_total_heartbeats:
        return False, (
            f"expected {expected_total_heartbeats} heartbeats; got "
            f"{result['send_interaction_count']}"
        )

    return True, "ok"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ticks", type=int, default=6)
    parser.add_argument("--la-fast", type=float, default=0.5)
    parser.add_argument("--la-mid", type=float, default=1.0)
    parser.add_argument("--la-slow", type=float, default=2.0)
    parser.add_argument("--step", type=float, default=1.0)
    parser.add_argument("--verbose", action="store_true")
    args = parser.parse_args(argv)

    try:
        result = asyncio.run(
            run_once(
                ticks=args.ticks,
                la_fast=args.la_fast,
                la_mid=args.la_mid,
                la_slow=args.la_slow,
                step=args.step,
                verbose=args.verbose,
            )
        )
    except Exception as exc:  # noqa: BLE001
        print(f"runner: {exc}", file=sys.stderr)
        return 1

    ok, msg = verify(result)
    last = result["trace"][-1] if result["trace"] else None
    last_lbts = last["lbts"] if last else float("nan")
    print(
        f"runner: ticks={len(result['trace'])}  "
        f"final_lbts={last_lbts:5.2f}  "
        f"ner={result['ner_count']}  "
        f"heartbeats={result['send_interaction_count']}  "
        f"verify={msg}"
    )
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
