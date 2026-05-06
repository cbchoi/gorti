"""Shared scaffolding for the three federate entry points.

Each federate (generator/buffer/processor) runs as its own Python
subprocess. This module factors out the bits the three entry points
have in common -- argparse setup, sys.path bootstrapping, the
``RtiConnection`` + federation-join boilerplate, the explicit
publish/subscribe declarations, the per-tick driver that translates
the bridge's ``CoupledModelProtocol`` into ``send_interaction`` /
``events()`` calls, and the result-file write.

Why a shared module: in the in-process variant the runner builds all
three federates in one event loop and orchestrates them; here each
federate is a standalone process, but they all need exactly the same
boot sequence. Keeping that in one place means the per-federate entry
points stay short and the difference between them is visible at a
glance.

Why we DON'T use ``HLAFederate`` directly here
==============================================
The bridge's ``HLAFederate.step_once`` issues
``next_message_request`` on every tick. Real ``rtid`` (M3+) does not
yet wire the time-service gRPC handlers
(``rti/internal/transport/grpc/server.go`` line 144:
``timeService: nil``), so any cross-process call to
``next_message_request`` hangs forever waiting for a grant that
never arrives -- see ``pysdk/rti1516e/_transport.py``'s module
docstring: *"Time-management RPCs (NextMessageRequest etc.) are not
wired ... The bridge's ``_await_grant`` would block forever in real
gRPC mode; cross-language tests therefore avoid the time-managed
code path and instead drive interactions directly."*

Cross-process therefore uses an *untimed* driver (``run_untimed_loop``
below): each tick is a wall-clock period, the driver pulls externals
off the events stream into ``external_transition``, then runs
``output_handler`` -> ``send_interaction`` -> ``internal_transition``.
Logical time still flows (the model's ``time_advance`` is consulted
to size the per-tick wall-clock sleep), but there is no LBTS / NER
coordination.

The downside: drop counts can vary across runs because
generator-fast-buffer-slow becomes a real wall-clock race rather
than a deterministic same-tick fan-out. The accounting invariants
(every published seq is forwarded-or-dropped-or-residual) still
hold; that's what the runner verifies.
"""

from __future__ import annotations

import argparse
import asyncio
import contextlib
import json
import sys
from collections.abc import Iterable
from pathlib import Path
from typing import Any

# Make the pysdk package importable when this module runs as a
# subprocess that may not have the package installed in site-packages.
_HERE = Path(__file__).resolve().parent
_REPO_ROOT = _HERE.parents[1]
_PYSDK = _REPO_ROOT / "pysdk"
for _path in (_PYSDK, _HERE):
    if str(_path) not in sys.path:
        sys.path.insert(0, str(_path))

# ruff: noqa: E402  (sys.path tweaks above must precede project imports)
from rti1516e.connection import Federate, FederationSpec, RtiConnection
from rti1516e.events import ReceiveInteraction

FOM_PATH = _HERE / "relay-fom.xml"
FEDERATION_NAME = "pyjevsim-relay-cross-process"


def common_parser(prog: str) -> argparse.ArgumentParser:
    """Build the argparse for a federate entry point.

    Each federate accepts the same set of knobs even when it doesn't
    use them all (the buffer ignores ``--gen-messages``, the generator
    ignores ``--capacity``, etc.). Keeping the surface uniform lets the
    runner spawn all three with the same argv builder.
    """
    p = argparse.ArgumentParser(prog=prog)
    p.add_argument("--url", required=True, help="grpc://host:port of the rtid")
    p.add_argument("--result", required=True, help="path to write the JSON result")
    p.add_argument("--gen-messages", type=int, default=50)
    p.add_argument("--capacity", type=int, default=5)
    p.add_argument("--service-period", type=int, default=2)
    p.add_argument("--drain-ticks", type=int, default=30)
    p.add_argument(
        "--tick-period",
        type=float,
        default=0.05,
        help=(
            "wall-clock seconds per logical tick (default 0.05). "
            "Bigger values give the network more slack between ticks "
            "and reduce drop-rate variance; smaller values run the "
            "pipeline faster but increase race risk."
        ),
    )
    p.add_argument(
        "--tail-ticks",
        type=int,
        default=0,
        help=(
            "extra ticks beyond gen_messages + drain_ticks to absorb "
            "in-flight delivery latency. The runner sets a positive "
            "value on the buffer + processor so a buffer emit on the "
            "last drain tick is still received by the processor."
        ),
    )
    # Per-federate startup delay so the runner can stagger the joins.
    # Defaults to 0; the runner sets a small positive value on the
    # buffer + processor so the generator has a chance to become the
    # publisher before the consumers start dialing for events.
    p.add_argument("--startup-delay", type=float, default=0.0)
    return p


def federation_spec() -> FederationSpec:
    """Construct the shared FederationSpec for this example."""
    return FederationSpec(
        name=FEDERATION_NAME,
        fom_modules=[str(FOM_PATH)],
        seed=0,
    )


async def declare_pub_sub(
    fed: Federate,
    *,
    publish_classes: Iterable[str],
    subscribe_classes: Iterable[str],
) -> None:
    """Issue publish + subscribe declarations on the joined federate."""
    for class_name in publish_classes:
        await fed.publish_interaction_class(class_name)
    for class_name in subscribe_classes:
        await fed.subscribe_interaction_class(class_name)


async def staggered_start(delay: float) -> None:
    """Optional startup delay before the first tick.

    The runner uses this to start subscribers slightly after the
    publisher so the publisher's ``publish_interaction_class`` has
    time to register before any subscriber expects events.
    """
    if delay > 0:
        await asyncio.sleep(delay)


async def drain_externals_now(
    fed: Federate,
    coupled_model: Any,
    fom_to_port: dict[str, str],
) -> int:
    """Pop every event currently in the federate's events queue and
    deliver each ``ReceiveInteraction`` to ``coupled_model.external_transition``.

    Returns the number of externals delivered. We intentionally read
    from the underlying ``transport.events_for(handle)`` queue using
    ``get_nowait`` rather than ``async for`` over ``fed.events()`` --
    the async generator never returns naturally, and we want a
    non-blocking drain that surfaces "no externals this tick"
    promptly.
    """
    transport = fed._transport  # noqa: SLF001 — tested seam, see fed.events() impl
    queue = transport.events_for(fed.handle)
    delivered = 0
    while True:
        try:
            event = queue.get_nowait()
        except asyncio.QueueEmpty:
            return delivered
        if not isinstance(event, ReceiveInteraction):
            # ReflectAttributeValues / DiscoverObjectInstance / TimeAdvanceGrant
            # / FederationHalted etc. -- the relay example doesn't
            # use them. Drop on the floor.
            continue
        port_name = fom_to_port.get(event.class_name)
        if port_name is None:
            # Subscribed to a class we have no port for -- the bridge
            # silently ignores; mirror that here.
            continue
        coupled_model.external_transition(port_name, dict(event.parameters))
        delivered += 1


async def emit_outputs_now(
    fed: Federate,
    coupled_model: Any,
    out_port_to_class: dict[str, str],
) -> int:
    """Run ``coupled_model.output_handler`` once and dispatch each
    produced (port, payload) pair to ``fed.send_interaction`` under
    the FOM class name registered for that port.

    Returns the number of interactions sent. Mirrors the bridge's
    ``_run_internal_cycle`` ordering (sort by port name for
    determinism) so the cross-process wire log is comparable to the
    in-process one.
    """
    outputs = coupled_model.output_handler()
    sent = 0
    # Sort port names so wire-visible send_interaction order is
    # deterministic regardless of how the user model assembled its
    # output dict (mirrors pyjevsim_bridge/time_advance.py M5-audit
    # issue #2).
    for port_name in sorted(outputs):
        payload = outputs[port_name]
        class_name = out_port_to_class.get(port_name)
        if class_name is None:
            continue
        if isinstance(payload, (bytes, bytearray)):
            wire = bytes(payload)
        else:
            # Defensive: stringify and encode so a model returning the
            # wrong type surfaces as a wire-readable error rather than
            # a silent drop.
            wire = repr(payload).encode("utf-8")
        await fed.send_interaction(
            class_name, parameters={"_payload": wire}
        )
        sent += 1
    coupled_model.internal_transition()
    return sent


async def run_untimed_loop(
    *,
    fed: Federate,
    coupled_model: Any,
    fom_to_port: dict[str, str],
    out_port_to_class: dict[str, str],
    total_ticks: int,
    tick_period: float,
    drain_first: bool,
) -> None:
    """Drive ``total_ticks`` per-tick cycles.

    Each cycle:
      - Drain pending externals into ``external_transition``.
      - Run ``output_handler`` -> ``send_interaction`` -> ``internal_transition``.
      - Sleep ``tick_period`` so the next cycle starts roughly
        on a wall-clock heartbeat.

    ``drain_first`` is purely cosmetic for the buffer: the buffer is
    both publisher and subscriber, and the in-process variant uses a
    "two step_once" trick to drain externals in one half-cycle and
    emit in the other. Setting ``drain_first=True`` here doubles the
    drain pass (drain -> emit -> drain) per outer tick, which roughly
    matches the in-process variant's behaviour. For pure publishers
    (generator) or pure subscribers (processor) this flag has no
    effect on observable accounting.

    Cancellation: a cancelled task interrupts the sleep cleanly via
    ``asyncio.CancelledError``; the caller's ``finally`` block writes
    the partial result so a SIGTERM still produces a usable file.
    """
    for _ in range(total_ticks):
        await drain_externals_now(fed, coupled_model, fom_to_port)
        await emit_outputs_now(fed, coupled_model, out_port_to_class)
        if drain_first:
            # Second pass: an emit may have unblocked the buffer's
            # head; drain any externals that arrived during the emit
            # so the next cycle's queue state reflects the latest
            # arrivals.
            await drain_externals_now(fed, coupled_model, fom_to_port)
        with contextlib.suppress(asyncio.CancelledError):
            await asyncio.sleep(tick_period)


async def run_drain_only_loop(
    *,
    fed: Federate,
    coupled_model: Any,
    fom_to_port: dict[str, str],
    total_ticks: int,
    tick_period: float,
) -> None:
    """Variant for pure-subscriber federates (processor): only drain
    externals, never emit. ``output_handler`` is still called for
    parity with the in-process bridge's loop, but its return value is
    expected to be empty so we don't dispatch anything to
    ``send_interaction``.
    """
    for _ in range(total_ticks):
        await drain_externals_now(fed, coupled_model, fom_to_port)
        # Run output_handler + internal_transition for protocol
        # symmetry; the processor's output is empty so nothing is
        # sent. internal_transition is a no-op for the processor but
        # we still call it so a future model swap doesn't silently
        # skip its state-advance.
        outputs = coupled_model.output_handler()
        if outputs:
            # If a future processor variant DOES emit, surface that
            # via stderr -- the relay processor is by design a sink.
            print(
                "_federate_common: processor emitted unexpected outputs: "
                f"{sorted(outputs)} (ignored)",
                file=sys.stderr,
                flush=True,
            )
        coupled_model.internal_transition()
        with contextlib.suppress(asyncio.CancelledError):
            await asyncio.sleep(tick_period)


def write_result(path: str, payload: dict[str, Any]) -> None:
    """Atomic-ish write of a small JSON result file.

    Each federate calls this once just before exit. The runner reads
    all three after every federate process has terminated, so the
    write doesn't need to be truly atomic -- but writing through a
    temp file + rename keeps a partially-written file from confusing
    a re-run that points at the same path.
    """
    out = Path(path)
    tmp = out.with_suffix(out.suffix + ".tmp")
    tmp.parent.mkdir(parents=True, exist_ok=True)
    tmp.write_text(json.dumps(payload, indent=2), encoding="utf-8")
    tmp.replace(out)
