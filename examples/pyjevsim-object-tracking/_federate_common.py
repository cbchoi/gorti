"""Shared scaffolding for the time-managed object-tracking example.

The federation has three federates managed by rtid's TimeService:

  - producer:  registers a Vehicle instance, time-regulating + constrained,
               updates Position/Speed with TSO timestamps at each NER grant.
  - tracker-A, tracker-B: subscribe to Vehicle.Position + Vehicle.Speed,
               time-regulating + constrained, receive ReflectAttributeValues
               events on the events() stream and feed them into the
               pyjevsim-style coupled model's external_transition() port.

All three federates use the same per-cycle pattern:

  1. issue NER(t) for t = i * tick_step
  2. wait for the *full* TimeAdvanceGrant (accumulating any forced grants)
  3. drain pending events; route reflects/discovers into the model
  4. emit any model outputs (producer: update attributes; trackers: no-op)
  5. internal_transition; record + repeat

This is the time-managed analog of pyjevsim-relay-cross-process's
``run_untimed_loop`` — the cycle is gated by a real grant from rtid
instead of a wall-clock heartbeat.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import sys
from pathlib import Path
from typing import Any

_HERE = Path(__file__).resolve().parent
_REPO_ROOT = _HERE.parents[1]
_PYSDK = _REPO_ROOT / "pysdk"

sys.path.insert(0, str(_PYSDK))

from rti1516e.connection import Federate  # noqa: E402
from rti1516e.events import (  # noqa: E402
    DiscoverObjectInstance,
    ReflectAttributeValues,
    TimeAdvanceGrant,
)

FEDERATION_NAME = "pyjevsim-obj-track"
FOM_PATH = _HERE / "vehicle-fom.xml"


def common_parser(prog: str) -> argparse.ArgumentParser:
    """Argparse skeleton shared by every federate entry point."""
    p = argparse.ArgumentParser(prog=prog)
    p.add_argument("--url", required=True)
    p.add_argument("--result", required=True, type=Path)
    p.add_argument("--name", required=True)
    p.add_argument("--cycles", type=int, default=10)
    p.add_argument("--tick-step", type=float, default=1.0)
    p.add_argument("--lookahead", type=float, default=0.5)
    p.add_argument("--grant-timeout", type=float, default=30.0)
    return p


def federation_spec() -> Any:
    """Return a FederationSpec the runner / federates can pass to
    ``RtiConnection.join_federation``. Inlined import so importing this
    module doesn't pull in the FOM parser unconditionally.
    """
    from rti1516e.connection import FederationSpec  # noqa: PLC0415

    return FederationSpec(
        name=FEDERATION_NAME,
        fom_modules=[str(FOM_PATH)],
        seed=0,
    )


async def wait_for_full_grant(fed: Federate, requested: float, timeout: float) -> float:
    """Block until a TimeAdvanceGrant arrives whose .time >= requested.

    Forced (partial) grants leave the federate in time-advancing state
    per IEEE 1516.1 §8.10; we accumulate them silently and only return
    on the full grant. Mirrors examples/pyjevsim-time-advance/_federate_common.py.
    """
    deadline = asyncio.get_event_loop().time() + timeout
    async for evt in fed.events():
        if isinstance(evt, TimeAdvanceGrant):
            t = float(evt.time)
            if t >= requested:
                return t
            print(
                f"  forced grant @ {t} < requested {requested}; waiting for full",
                flush=True,
            )
        if asyncio.get_event_loop().time() > deadline:
            break
    raise TimeoutError(
        f"no full TimeAdvanceGrant within {timeout}s (requested={requested})"
    )


async def wait_for_discover(
    fed: Federate, model: Any, *, timeout: float = 10.0,
) -> int:
    """Block until a DiscoverObjectInstance arrives on the events
    queue, or ``timeout`` elapses. Routes the discover into the
    model.discover_handler if defined.

    Used as a barrier in the tracker before its NMRA cycle loop:
    without this, a constrained-only tracker spawned BEFORE the
    producer would see no regulators in the federation and grant
    every NMRA instantly with LBTS=+Inf, racing past the producer's
    updates entirely.

    Returns the number of discovers consumed (1 in the common case;
    >1 if multiple instances arrived in a burst before the wait).
    """
    transport = fed._transport  # noqa: SLF001
    queue = transport.events_for(fed.handle)
    deadline = asyncio.get_event_loop().time() + timeout
    n = 0
    while True:
        remaining = deadline - asyncio.get_event_loop().time()
        if remaining <= 0:
            if n == 0:
                raise TimeoutError(f"wait_for_discover: no Discover within {timeout}s")
            return n
        try:
            event = await asyncio.wait_for(queue.get(), timeout=remaining)
        except asyncio.TimeoutError:
            if n == 0:
                raise TimeoutError(
                    f"wait_for_discover: no Discover within {timeout}s",
                ) from None
            return n
        if isinstance(event, DiscoverObjectInstance):
            n += 1
            handler = getattr(model, "discover_handler", None)
            if handler is not None:
                handler(event.object_handle, event.class_name, event.instance_name)
            # Drain any other pending discovers that arrived in the
            # same burst, then return.
            while True:
                try:
                    extra = queue.get_nowait()
                except asyncio.QueueEmpty:
                    return n
                if isinstance(extra, DiscoverObjectInstance):
                    n += 1
                    if handler is not None:
                        handler(extra.object_handle, extra.class_name, extra.instance_name)
                # Else: drop other event types — tracker hasn't
                # started its NMRA cycle yet so any reflect / grant
                # arriving here is a server-side anomaly.
        # Else: silently drop pre-Discover events (none expected).


def _values_with_attr_names(fed: Federate, raw: dict[Any, Any]) -> dict[str, Any]:
    """Convert event.values from handle-stringified keys to attribute names.

    Pysdk's stream-event decoder yields ReflectAttributeValues.values
    keyed by stringified attribute handles ("1", "2") rather than
    attribute names. This helper looks up the inverse mapping via the
    transport's per-class attribute-handle cache and returns a name-
    keyed dict ("Position", "Speed"). Unknown handles pass through
    with their original key.
    """
    transport = fed._transport  # noqa: SLF001
    # Build a global handle → name map from the FOM. The cache is
    # per-class; we walk all known classes.
    inverse: dict[tuple[int, int], str] = {}
    fom = getattr(transport, "_fom_cache", None)
    if fom is not None:
        for cls_idx, oc in enumerate(sorted(fom.object_classes, key=lambda c: c.name)):
            cls_handle = cls_idx + 1
            for attr_idx, a in enumerate(oc.attributes):
                inverse[(cls_handle, attr_idx + 1)] = a.name
    # Without a class context we don't know which class the keys
    # belong to; the same per-class handle (e.g. 1) may map to
    # different names across classes. For this single-class example
    # (Vehicle), we look up by attribute name in the Vehicle class.
    out: dict[str, Any] = {}
    # Find Vehicle's handle.
    vehicle_handle = None
    if fom is not None:
        for cls_idx, oc in enumerate(sorted(fom.object_classes, key=lambda c: c.name)):
            if oc.name == "Vehicle":
                vehicle_handle = cls_idx + 1
                break
    for k, v in raw.items():
        try:
            handle = int(k)
        except (TypeError, ValueError):
            out[str(k)] = v
            continue
        name = None
        if vehicle_handle is not None:
            name = inverse.get((vehicle_handle, handle))
        out[name or str(handle)] = v
    return out


async def drain_events_into_model(
    fed: Federate, model: Any, *,
    until_grant: bool, requested: float, timeout: float = 30.0,
) -> tuple[int, int, float | None]:
    """Drain the federate's events queue and route into the pyjevsim model.

    Modes:
      - ``until_grant=True``: AWAIT events (blocking up to ``timeout``
        seconds); return as soon as a TimeAdvanceGrant with
        .time >= requested arrives. Forced (partial) grants are
        accumulated; reflects / discovers in between are routed to
        the model in arrival order.
      - ``until_grant=False``: non-blocking drain — return immediately
        when the queue is empty.

    Routing:
      - DiscoverObjectInstance → model.discover_handler(handle, class, name)
      - ReflectAttributeValues → model.external_transition("reflect:<class>", attrs)
        — the pyjevsim canonical "external input message arrived" hook,
        same shape as in examples/pyjevsim-relay-cross-process.
      - TimeAdvanceGrant → return when full.

    Returns (discover_count, reflect_count, full_grant_time_or_None).
    """
    transport = fed._transport  # noqa: SLF001 — same pattern as relay-cross-process
    queue = transport.events_for(fed.handle)
    n_discover = 0
    n_reflect = 0
    full_grant: float | None = None
    deadline = asyncio.get_event_loop().time() + timeout

    while True:
        if until_grant:
            remaining = deadline - asyncio.get_event_loop().time()
            if remaining <= 0:
                return n_discover, n_reflect, full_grant
            try:
                event = await asyncio.wait_for(queue.get(), timeout=remaining)
            except asyncio.TimeoutError:
                return n_discover, n_reflect, full_grant
        else:
            try:
                event = queue.get_nowait()
            except asyncio.QueueEmpty:
                return n_discover, n_reflect, full_grant
        if isinstance(event, DiscoverObjectInstance):
            n_discover += 1
            handler = getattr(model, "discover_handler", None)
            if handler is not None:
                handler(event.object_handle, event.class_name, event.instance_name)
        elif isinstance(event, ReflectAttributeValues):
            n_reflect += 1
            # pyjevsim-style: external input arrives on a named port.
            # The port name encodes "reflect:<class>" so a multi-class
            # model can route. Single-class trackers in this example
            # ignore the port suffix.
            handler = getattr(model, "external_transition", None)
            if handler is not None:
                handler("reflect:Vehicle", _values_with_attr_names(fed, event.values))
        elif isinstance(event, TimeAdvanceGrant):
            t = float(event.time)
            if until_grant and t >= requested:
                full_grant = t
                # rtid's emitGrant emits the grant BEFORE
                # releaseBufferedTSO drains pending TSO events.
                # The tracker's queue typically receives
                # [Grant, Reflect_1, Reflect_2, ...] for a federate
                # that had reflects buffered in the time-advancing
                # window. The reflects may be IN FLIGHT through the
                # multiOutbox at the moment the grant arrives —
                # get_nowait alone races. Use a small post-grant
                # wait window (50 ms) to catch in-flight events.
                post_deadline = asyncio.get_event_loop().time() + 0.05
                while True:
                    remaining_post = post_deadline - asyncio.get_event_loop().time()
                    if remaining_post <= 0:
                        return n_discover, n_reflect, full_grant
                    try:
                        follow = await asyncio.wait_for(
                            queue.get(), timeout=remaining_post,
                        )
                    except asyncio.TimeoutError:
                        return n_discover, n_reflect, full_grant
                    if isinstance(follow, DiscoverObjectInstance):
                        n_discover += 1
                        h = getattr(model, "discover_handler", None)
                        if h is not None:
                            h(follow.object_handle, follow.class_name, follow.instance_name)
                    elif isinstance(follow, ReflectAttributeValues):
                        n_reflect += 1
                        h = getattr(model, "external_transition", None)
                        if h is not None:
                            h("reflect:Vehicle", _values_with_attr_names(fed, follow.values))
                    # else: drop other event types
            if until_grant:
                print(
                    f"  forced grant @ {t} < requested {requested}; waiting for full",
                    flush=True,
                )
        # else: silently drop any other event type (FederationHalted etc.)


def write_result(path: Path, payload: dict[str, Any]) -> None:
    """Atomic-ish write: tmp file + rename so a crashed federate doesn't
    leave a partial JSON the runner mistakes for "ok"."""
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text(json.dumps(payload, indent=2), encoding="utf-8")
    tmp.replace(path)
