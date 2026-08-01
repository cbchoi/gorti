"""Shared scaffolding for the participant federate entry point.

Mirrors examples/pyjevsim/_federate_common.py with two additions:

  - ``register_or_swallow`` -- one of the 3 federates wins the
    register_synchronization_point race; the other two get a
    "label already registered" error from rtid that we swallow.
  - ``wait_for_event`` -- pop events off ``fed.events()``, return the
    first matching one, queue Ticks back to the model so peer
    Ticks delivered during a sync phase aren't lost.

The cross-process conversion replaces the runner-as-oracle pattern
the original docstring described (M12 deferral #1) with real
SyncService RPCs and real
SynchronizationPointAnnounced / FederationSynchronized event
deliveries. SyncService was wired in M12 W1; sync events are at
proto FederateEvent oneof tags 20 / 21.
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

_HERE = Path(__file__).resolve().parent
_REPO_ROOT = _HERE.parents[1]
_PYSDK = _REPO_ROOT / "pysdk"
for _path in (_PYSDK, _HERE):
    if str(_path) not in sys.path:
        sys.path.insert(0, str(_path))

# ruff: noqa: E402
from rti1516e.connection import Federate, FederationSpec
from rti1516e.errors import RtiError
from rti1516e.events import (
    FederationSynchronized,
    ReceiveInteraction,
    SynchronizationPointAnnounced,
)

FOM_PATH = _HERE / "sync-points-fom.xml"
FEDERATION_NAME = "pyjevsim-sync-points"

START_LABEL = "start_simulation"
END_LABEL = "end_simulation"
PARTICIPANT_NAMES = ("alpha", "beta", "gamma")


def common_parser(prog: str) -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(prog=prog)
    p.add_argument("--url", required=True, help="grpc://host:port of the rtid")
    p.add_argument("--result", required=True, help="path to write the JSON result")
    p.add_argument(
        "--name", required=True, choices=PARTICIPANT_NAMES,
        help="federate name (also used as the HLA federate name)",
    )
    p.add_argument(
        "--running-ticks", type=int, default=10,
        help="ticks between start_simulation and end_simulation (default 10)",
    )
    p.add_argument(
        "--tick-period", type=float, default=0.05,
        help="wall-clock seconds per logical tick (default 0.05)",
    )
    p.add_argument(
        "--join-settle", type=float, default=1.0,
        help=(
            "seconds to wait after joining before registering the "
            "sync point. Gives all peers time to join so the "
            "register's 'all currently joined' required-set covers "
            "everyone (default 1.0)."
        ),
    )
    p.add_argument(
        "--rendezvous-timeout", type=float, default=15.0,
        help="per-rendezvous deadline in seconds (default 15.0)",
    )
    return p


def federation_spec() -> FederationSpec:
    return FederationSpec(name=FEDERATION_NAME, fom_modules=[str(FOM_PATH)], seed=0)


async def declare_pub_sub(
    fed: Federate,
    *,
    publish_classes: Iterable[str],
    subscribe_classes: Iterable[str],
) -> None:
    for class_name in publish_classes:
        await fed.publish_interaction_class(class_name)
    for class_name in subscribe_classes:
        await fed.subscribe_interaction_class(class_name)


async def register_or_swallow(fed: Federate, label: str) -> None:
    """Try to register a sync point; ignore "already registered" errors.

    With 3 federates calling this near-simultaneously, exactly one
    will succeed at the RTI; the other two will get a duplicate-label
    error which we swallow because the announce will fan out to all
    federates regardless.
    """
    try:
        await fed.sync.register_synchronization_point(label)
    except RtiError as exc:
        # Duplicate-label is the expected outcome for 2 of the 3
        # callers. Any other RTI error is a real failure.
        msg = str(exc).lower()
        if "already" in msg or "duplicate" in msg or "exists" in msg:
            return
        raise


def _decode_tick(parameters: Any) -> int | None:
    params = dict(parameters)
    wire = params.get("_payload")
    if wire is None and len(params) == 1:
        wire = next(iter(params.values()))
    if isinstance(wire, (bytes, bytearray)) and len(wire) == 4:
        return int.from_bytes(wire, "big")
    return None


async def wait_for_event(
    fed: Federate,
    *,
    matches,
    timeout: float,
    on_tick=None,
) -> Any:
    """Poll the federate's event queue until ``matches(event)`` returns
    True; return that event. Tick interactions delivered while we
    wait are passed to ``on_tick(seq)`` if provided, else dropped.
    """
    transport = fed._transport  # noqa: SLF001
    queue = transport.events_for(fed.handle)
    loop = asyncio.get_event_loop()
    deadline = loop.time() + timeout
    while True:
        try:
            event = queue.get_nowait()
        except asyncio.QueueEmpty:
            if loop.time() >= deadline:
                raise TimeoutError(
                    f"timeout waiting for event after {timeout}s"
                ) from None
            await asyncio.sleep(0.05)
            continue
        if matches(event):
            return event
        if isinstance(event, ReceiveInteraction) and on_tick is not None:
            tick = _decode_tick(event.parameters)
            if tick is not None:
                on_tick(tick)
        # else: drop unrelated event


async def wait_for_announced(
    fed: Federate, label: str, *, timeout: float, on_tick=None,
) -> SynchronizationPointAnnounced:
    return await wait_for_event(
        fed,
        matches=lambda e: isinstance(e, SynchronizationPointAnnounced) and e.label == label,
        timeout=timeout,
        on_tick=on_tick,
    )


async def wait_for_synchronized(
    fed: Federate, label: str, *, timeout: float, on_tick=None,
) -> FederationSynchronized:
    return await wait_for_event(
        fed,
        matches=lambda e: isinstance(e, FederationSynchronized) and e.label == label,
        timeout=timeout,
        on_tick=on_tick,
    )


async def run_running_phase(
    *,
    fed: Federate,
    coupled_model: Any,
    fom_to_port: dict[str, str],
    out_port_to_class: dict[str, str],
    total_ticks: int,
    tick_period: float,
    received_ticks: list[int],
) -> None:
    """Drive ``total_ticks`` cycles. Each cycle drains incoming Ticks
    into ``received_ticks`` (and the model's external_transition),
    runs the model's output_handler (which emits a Tick payload
    bytes), and sleeps. Ignores any sync events that race in.
    """
    transport = fed._transport  # noqa: SLF001
    queue = transport.events_for(fed.handle)
    for _ in range(total_ticks):
        # Drain.
        while True:
            try:
                event = queue.get_nowait()
            except asyncio.QueueEmpty:
                break
            if isinstance(event, ReceiveInteraction):
                port_name = fom_to_port.get(event.class_name)
                if port_name is None:
                    continue
                params = dict(event.parameters)
                tick = _decode_tick(params)
                if tick is not None:
                    received_ticks.append(tick)
                coupled_model.external_transition(port_name, params)
        # Emit.
        outputs = coupled_model.output_handler()
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
        coupled_model.internal_transition()
        with contextlib.suppress(asyncio.CancelledError):
            await asyncio.sleep(tick_period)


async def drain_peer_ticks(
    *,
    fed: Federate,
    coupled_model: Any,
    fom_to_port: dict[str, str],
    received_ticks: list[int],
    expected_count: int,
    timeout: float,
) -> None:
    """Drain all expected peer Ticks before entering the end rendezvous."""
    transport = fed._transport  # noqa: SLF001
    queue = transport.events_for(fed.handle)
    loop = asyncio.get_event_loop()
    deadline = loop.time() + timeout
    while len(received_ticks) < expected_count:
        try:
            event = queue.get_nowait()
        except asyncio.QueueEmpty:
            if loop.time() >= deadline:
                raise TimeoutError(
                    f"received {len(received_ticks)} peer ticks; "
                    f"expected {expected_count} before synchronization"
                ) from None
            await asyncio.sleep(0.01)
            continue
        if not isinstance(event, ReceiveInteraction):
            continue
        port_name = fom_to_port.get(event.class_name)
        if port_name is None:
            continue
        params = dict(event.parameters)
        tick = _decode_tick(params)
        if tick is not None:
            received_ticks.append(tick)
        coupled_model.external_transition(port_name, params)

    if len(received_ticks) != expected_count:
        raise RuntimeError(
            f"received {len(received_ticks)} peer ticks; expected {expected_count}"
        )


def write_result(path: str, payload: dict[str, Any]) -> None:
    out = Path(path)
    tmp = out.with_suffix(out.suffix + ".tmp")
    tmp.parent.mkdir(parents=True, exist_ok=True)
    tmp.write_text(json.dumps(payload, indent=2), encoding="utf-8")
    tmp.replace(out)
