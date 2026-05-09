"""Shared scaffolding for the producer + consumer federate entry points.

Each federate runs as its own Python subprocess. This module factors
out the bits the two entry points have in common -- argparse setup,
sys.path bootstrapping, the federation spec, the publish/subscribe
declaration helper, the per-tick driver loops that translate the
DEVS-canonical ``CoupledModelProtocol`` into ``send_interaction`` /
``events()`` calls, and the result-file write.

Mirrors ``examples/pyjevsim-relay-cross-process/_federate_common.py``
deliberately -- once a future contributor learns one example's
scaffolding, both feel the same.

Why we DON'T use ``HLAFederate`` directly here
==============================================
The bridge's ``HLAFederate.step_once`` issues ``next_message_request``
on every tick. Real ``rtid`` (M3+) does not yet wire the time-service
gRPC handlers, so any cross-process call to ``next_message_request``
hangs forever waiting for a grant that never arrives. See
``pysdk/rti1516e/_transport.py``'s module docstring.

Cross-process therefore uses an *untimed* driver: each tick is a
wall-clock period, the driver pulls externals off the events stream
into ``external_transition``, then runs ``output_handler`` ->
``send_interaction`` -> ``internal_transition``. Logical time still
flows (the model's ``time_advance`` is consulted by the model
itself), but there is no LBTS / NER coordination.
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

FOM_PATH = _HERE / "pyjevsim-fom.xml"
FEDERATION_NAME = "pyjevsim-cross-process"


def common_parser(prog: str) -> argparse.ArgumentParser:
    """Build the argparse for a federate entry point.

    Both federates accept the same set of knobs even when one of them
    doesn't use one (the producer ignores ``--tail-ticks``). Keeping
    the surface uniform lets the runner / shell scripts spawn either
    federate with the same argv builder.
    """
    p = argparse.ArgumentParser(prog=prog)
    p.add_argument("--url", required=True, help="grpc://host:port of the rtid")
    p.add_argument("--result", required=True, help="path to write the JSON result")
    p.add_argument(
        "--ticks", type=int, default=50,
        help="logical tick count for the producer's emit phase (default 50)",
    )
    p.add_argument(
        "--drain-ticks", type=int, default=30,
        help="extra idle ticks after the emit phase (default 30)",
    )
    p.add_argument(
        "--tail-ticks", type=int, default=0,
        help=(
            "extra ticks beyond ticks+drain_ticks to absorb in-flight "
            "delivery latency. The runner sets a positive value on the "
            "consumer so a producer emit on the last drain tick is still "
            "received before the consumer resigns."
        ),
    )
    p.add_argument(
        "--tick-period", type=float, default=0.05,
        help="wall-clock seconds per logical tick (default 0.05)",
    )
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

    The runner uses this to start the consumer slightly before the
    producer so the consumer's ``subscribe_interaction_class`` lands
    before any publish.
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

    Returns the number of externals delivered. Reads from the
    underlying ``transport.events_for(handle)`` queue using
    ``get_nowait`` for a non-blocking drain that surfaces "no
    externals this tick" promptly.
    """
    transport = fed._transport  # noqa: SLF001 — tested seam
    queue = transport.events_for(fed.handle)
    delivered = 0
    while True:
        try:
            event = queue.get_nowait()
        except asyncio.QueueEmpty:
            return delivered
        if not isinstance(event, ReceiveInteraction):
            continue
        port_name = fom_to_port.get(event.class_name)
        if port_name is None:
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
    """
    outputs = coupled_model.output_handler()
    sent = 0
    for port_name in sorted(outputs):
        payload = outputs[port_name]
        class_name = out_port_to_class.get(port_name)
        if class_name is None:
            continue
        if isinstance(payload, (bytes, bytearray)):
            wire = bytes(payload)
        else:
            wire = repr(payload).encode("utf-8")
        await fed.send_interaction(class_name, parameters={"_payload": wire})
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
) -> None:
    """Drive ``total_ticks`` per-tick cycles for a publishing federate.

    Each cycle: drain pending externals, run ``output_handler`` ->
    ``send_interaction`` -> ``internal_transition``, sleep
    ``tick_period``.
    """
    for _ in range(total_ticks):
        await drain_externals_now(fed, coupled_model, fom_to_port)
        await emit_outputs_now(fed, coupled_model, out_port_to_class)
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
    """Variant for pure-subscriber federates (consumer): only drain
    externals, never emit. ``output_handler`` is still called for
    parity with the in-process bridge's loop, but its return value is
    expected to be empty.
    """
    for _ in range(total_ticks):
        await drain_externals_now(fed, coupled_model, fom_to_port)
        outputs = coupled_model.output_handler()
        if outputs:
            print(
                "_federate_common: subscriber emitted unexpected outputs: "
                f"{sorted(outputs)} (ignored)",
                file=sys.stderr, flush=True,
            )
        coupled_model.internal_transition()
        with contextlib.suppress(asyncio.CancelledError):
            await asyncio.sleep(tick_period)


def write_result(path: str, payload: dict[str, Any]) -> None:
    """Atomic-ish write of a small JSON result file (temp + rename)."""
    out = Path(path)
    tmp = out.with_suffix(out.suffix + ".tmp")
    tmp.parent.mkdir(parents=True, exist_ok=True)
    tmp.write_text(json.dumps(payload, indent=2), encoding="utf-8")
    tmp.replace(out)
