"""End-to-end runner for the Sensor → Dashboard object-class example
(Path B — via ``pyjevsim_bridge.HLAFederate``).

This is the **bridged** variant of ``examples/pyjevsim-dashboard/``.
The pedagogical contrast:

  * **Path A** (``examples/pyjevsim-dashboard/runner.py``) — federate
    code calls ``Federate.register_object_instance`` /
    ``Federate.update_attributes`` directly. The runner owns the
    Sensor and Dashboard as plain Python objects and threads the
    raw RTI calls through them.
  * **Path B** (this file) — Sensor and Dashboard are plain
    coupled-model-shaped objects that opt INTO
    ``ObjectClassFederateProtocol``. ``HLAFederate`` reads the
    publications / subscriptions / instance registrations off the
    model and issues the corresponding ``publish_object_class`` /
    ``subscribe_object_class`` / ``register_object_instance`` /
    ``update_attributes`` RPCs. ``DiscoverObjectInstance`` /
    ``ReflectAttributeValues`` events route to the model's
    ``discover_handler`` / ``reflect_handler``.

Run from the repo root::

    python3 examples/pyjevsim-dashboard-bridged/runner.py

Optional flags::

    --ticks N            sensor publishes N updates (default 10)
    --mode {sequence,sine}
    --amplitude A        for sine mode: integer amplitude (default 100)
    --verbose            print per-tick state

Exit code 0 on success, 1 on verify failure.

Why the runner still synthesizes Discover + Reflect events
----------------------------------------------------------
``InProcessTransport`` is a **recorder**, not a router — it captures
every ``record(...)`` call but doesn't walk the FOM to dispatch
``DiscoverObjectInstance`` / ``ReflectAttributeValues`` to
subscribers. Production rtid does this via the
DeclarationService + Registry hooks (``rti/internal/object/registry.go``).

So the runner does the same thing the bypass runner does: after the
sensor's ``register_object_instance`` lands (visible in
``transport.calls``), push a synthesised Discover into the
dashboard's queue; after every ``update_attributes`` call, push a
synthesised Reflect. The bridge then routes those events to the
dashboard's handlers exactly as it would for a real RTI.

This is allowed by the task constraints — runners may be aware of
the in-process transport's recorder-not-router quirk.
"""

from __future__ import annotations

import argparse
import asyncio
import importlib.util as _il
import sys
from pathlib import Path
from typing import Any

# Make sibling modules importable when run as
# ``python examples/pyjevsim-dashboard-bridged/runner.py``.
_HERE = Path(__file__).resolve().parent

# Make the pysdk package importable when not pip-installed.
_PYSDK = _HERE.parents[1] / "pysdk"
if str(_PYSDK) not in sys.path:
    sys.path.insert(0, str(_PYSDK))


def _load_sibling(unique_name: str, file_name: str) -> Any:
    """Load ``_HERE/<file_name>`` under ``unique_name`` so it doesn't
    collide with the bypass example's ``sensor.py`` / ``dashboard.py``
    when both example dirs are on ``sys.path`` in one pytest session.

    Mirrors the pattern the test files use (per the dcefea5
    housekeeping fix), applied at runtime so bare ``import sensor``
    can't pick up the bypass variant by accident."""
    spec = _il.spec_from_file_location(unique_name, _HERE / file_name)
    if spec is None or spec.loader is None:
        raise ImportError(f"failed to load {file_name} from {_HERE}")
    module = _il.module_from_spec(spec)
    sys.modules[unique_name] = module
    spec.loader.exec_module(module)
    return module


_sensor = _load_sibling("_pyjevsim_dashboard_bridged_sensor", "sensor.py")
_dashboard = _load_sibling("_pyjevsim_dashboard_bridged_dashboard", "dashboard.py")
Sensor = _sensor.Sensor
Dashboard = _dashboard.Dashboard

# ruff: noqa: E402  (sys.path tweaks above must precede project imports)
from pyjevsim_bridge import HLAFederate, PortMapping
from rti1516e._inprocess import InProcessTransport
from rti1516e.connection import FederationSpec
from rti1516e.events import (
    DiscoverObjectInstance,
    ReflectAttributeValues,
)

FOM_PATH = _HERE / "dashboard-fom.xml"


async def run_once(
    *,
    ticks: int = 10,
    mode: str = "sequence",
    amplitude: int = 100,
    verbose: bool = False,
) -> dict[str, Any]:
    """Run one full Sensor → Dashboard exchange via the bridge and
    return a result dict suitable for ``verify()``.

    The returned shape matches the bypass variant exactly so the same
    verifier logic works for both.
    """
    transport = InProcessTransport()
    federation = FederationSpec(
        name="pyjevsim-dashboard-bridged-example",
        fom_modules=[str(FOM_PATH)],
        seed=0,
    )

    sensor = Sensor(stop_after=ticks, mode=mode, amplitude=amplitude)
    dashboard = Dashboard()

    sensor_fed = HLAFederate(
        coupled_model=sensor,
        federation=federation,
        federate_name="sensor",
        port_mapping=PortMapping.from_dict({}),
        url="memory://fake-rti",
    )
    dashboard_fed = HLAFederate(
        coupled_model=dashboard,
        federation=federation,
        federate_name="dashboard",
        port_mapping=PortMapping.from_dict({}),
        url="memory://fake-rti",
    )

    # Bring both bridges up so we can read their handles + the
    # sensor's instance handle before starting the per-tick loop.
    # _ensure_federate runs the lazy object-class declaration init
    # for free, which is why we don't need an explicit
    # publish/subscribe call here.
    await sensor_fed._ensure_federate()  # noqa: SLF001
    await dashboard_fed._ensure_federate()  # noqa: SLF001
    dashboard_handle = dashboard_fed._federate.handle  # type: ignore[union-attr]
    # Sensor instance handle is in the bridge's local-name → handle map
    # after register_instances ran during _ensure_federate.
    obj_handle = sensor_fed._instance_handles[Sensor.INSTANCE_NAME]  # noqa: SLF001

    # The runner mediates the in-process transport's recorder/router
    # gap by synthesising Discover + Reflect events into the
    # dashboard's bridge queue. Bridge then routes them to
    # dashboard.discover_handler / dashboard.reflect_handler.
    await dashboard_fed.deliver_object_event(
        DiscoverObjectInstance(
            object_handle=obj_handle,
            class_name=Sensor.CLASS_NAME,
            instance_name=Sensor.INSTANCE_NAME,
        )
    )
    # Drain that single discover event before the first reflect, so
    # the trace shows discover-before-reflect (matches the bypass
    # variant's ordering invariant).
    await dashboard_fed.step_once()

    # Track which update_attributes calls we've already converted to
    # Reflect events so we don't double-fire when the runner loops.
    update_cursor = 0

    for tick in range(ticks):
        # Sensor cycle: bridge calls attribute_update_handler internally
        # and emits update_attributes via the in-process transport.
        await sensor_fed.step_once()

        # Drain new update_attributes calls into the dashboard bridge's
        # event queue as ReflectAttributeValues. The transport.calls
        # list is append-only, so a cursor is enough to find newcomers.
        new_updates = transport.calls_for("update_attributes")[update_cursor:]
        update_cursor += len(new_updates)
        for call in new_updates:
            if call.args.get("federate_handle") == dashboard_handle:
                # Defensive: skip self-loop (shouldn't happen — the
                # dashboard never calls update_attributes — but keep
                # the symmetry with the relay example's fanout filter).
                continue
            await dashboard_fed.deliver_object_event(
                ReflectAttributeValues(
                    object_handle=int(call.args.get("object_handle", 0)),
                    values=dict(call.args.get("values", {})),
                    timestamp=call.args.get("timestamp"),
                )
            )

        # Dashboard cycle: drains its pending Reflect events. The
        # bridge's external-first contract makes this run
        # _drain_pending_external (no internal cycle) when the queue
        # is non-empty, and a normal idle internal cycle when it's
        # empty (e.g. when the sensor returned no update this tick).
        await dashboard_fed.step_once()

        if verbose:
            print(
                f"tick={tick:3d}  "
                f"sensor.published={len(sensor.published):3d}  "
                f"dashboard.received={len(dashboard.received):3d}",
                flush=True,
            )

    await sensor_fed.aclose()
    await dashboard_fed.aclose()

    return {
        "published": list(sensor.published),
        "received": list(dashboard.received),
        "discovered": list(dashboard.discovered),
        "update_attribute_calls": len(transport.calls_for("update_attributes")),
        "register_object_calls": len(
            transport.calls_for("register_object_instance")
        ),
        "publish_object_calls": len(transport.calls_for("publish_object_class")),
        "subscribe_object_calls": len(
            transport.calls_for("subscribe_object_class")
        ),
    }


def verify(result: dict[str, Any]) -> tuple[bool, str]:
    """End-to-end checks (same shape as bypass variant + bridge wiring
    invariants).

    1. Dashboard's received list equals the sensor's published list.
    2. Discover happened exactly once.
    3. update_attribute_calls equals the number of values published.
    4. Sensor's bridge issued exactly one publish_object_class.
    5. Dashboard's bridge issued exactly one subscribe_object_class.
    6. Sensor's bridge issued exactly one register_object_instance.
    """
    published = result["published"]
    received = result["received"]
    discovered = result["discovered"]

    if received != published:
        return False, (
            f"received != published: "
            f"len(received)={len(received)} len(published)={len(published)}; "
            f"first divergence at index "
            f"{_first_divergence_index(received, published)}"
        )

    if len(discovered) != 1:
        return False, (
            f"expected exactly one DiscoverObjectInstance; got "
            f"{len(discovered)} ({discovered!r})"
        )

    if result["update_attribute_calls"] != len(published):
        return False, (
            f"update_attribute_calls={result['update_attribute_calls']} "
            f"!= len(published)={len(published)}"
        )

    if result["register_object_calls"] != 1:
        return False, (
            f"register_object_calls={result['register_object_calls']} "
            "must be 1"
        )

    if result["publish_object_calls"] != 1:
        return False, (
            f"publish_object_calls={result['publish_object_calls']} "
            "must be 1 (bridge issues publish exactly once at startup)"
        )

    if result["subscribe_object_calls"] != 1:
        return False, (
            f"subscribe_object_calls={result['subscribe_object_calls']} "
            "must be 1 (bridge issues subscribe exactly once at startup)"
        )

    return True, "ok"


def _first_divergence_index(a: list[Any], b: list[Any]) -> int:
    for i, (x, y) in enumerate(zip(a, b, strict=False)):
        if x != y:
            return i
    return min(len(a), len(b))


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ticks", type=int, default=10)
    parser.add_argument(
        "--mode", choices=("sequence", "sine"), default="sequence"
    )
    parser.add_argument("--amplitude", type=int, default=100)
    parser.add_argument("--verbose", action="store_true")
    args = parser.parse_args(argv)

    try:
        result = asyncio.run(
            run_once(
                ticks=args.ticks,
                mode=args.mode,
                amplitude=args.amplitude,
                verbose=args.verbose,
            )
        )
    except Exception as exc:  # noqa: BLE001
        print(f"runner: {exc}", file=sys.stderr)
        return 1

    ok, msg = verify(result)
    print(
        f"runner: published={len(result['published'])}  "
        f"received={len(result['received'])}  "
        f"discovered={len(result['discovered'])}  "
        f"updates={result['update_attribute_calls']}  "
        f"verify={msg}"
    )
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
