"""End-to-end runner for the Sensor → Dashboard object-class example.

This example deliberately **bypasses** the ``pyjevsim_bridge`` and uses
the lower-level ``RtiConnection`` + ``Federate`` API directly. Rationale:

The bridge's ``port_mapping.py`` wraps payloads under ``_payload`` and
its ``time_advance.py::_run_internal_cycle`` calls
``send_interaction(...)`` for each output port. There is no equivalent
path in the cut-1/cut-2 bridge for ``register_object_instance`` /
``update_attributes`` / ``ReflectAttributeValues``. Object-class
semantics are pedagogically distinct from interactions and need their
own integration; the bridge can grow that surface in a later cut.

Until the bridge gains that surface, the canonical way to publish an
object instance from Python is the API the Sensor federate uses here.

Run from the repo root::

    python3 examples/pyjevsim-dashboard/runner.py

Optional flags::

    --ticks N            sensor publishes N updates (default 10)
    --mode {seq,sine}    publish a sequence or a quantised sine wave
                         (default: sequence)
    --amplitude A        for sine mode: integer amplitude (default 100)
    --verbose            print per-tick state

Exit code 0 on success, 1 on verify failure.
"""

from __future__ import annotations

import argparse
import asyncio
import sys
from pathlib import Path
from typing import Any

# Make sibling modules importable when run as
# ``python examples/pyjevsim-dashboard/runner.py``.
_HERE = Path(__file__).resolve().parent
if str(_HERE) not in sys.path:
    sys.path.insert(0, str(_HERE))

# Make the pysdk package importable when not pip-installed.
_PYSDK = _HERE.parents[1] / "pysdk"
if str(_PYSDK) not in sys.path:
    sys.path.insert(0, str(_PYSDK))

# ruff: noqa: E402  (sys.path tweaks above must precede project imports)
from dashboard import Dashboard
from sensor import Sensor

from rti1516e._inprocess import InProcessTransport
from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.events import (
    DiscoverObjectInstance,
    ReflectAttributeValues,
    TimeAdvanceGrant,
)

FOM_PATH = _HERE / "dashboard-fom.xml"


async def run_once(
    *,
    ticks: int = 10,
    mode: str = "sequence",
    amplitude: int = 100,
    verbose: bool = False,
) -> dict[str, Any]:
    """Run one full Sensor → Dashboard exchange and return a result
    dict suitable for verify()."""

    transport = InProcessTransport()
    federation = FederationSpec(
        name="pyjevsim-dashboard-example",
        fom_modules=[str(FOM_PATH)],
        seed=0,
    )

    sensor = Sensor(stop_after=ticks, mode=mode, amplitude=amplitude)
    dashboard = Dashboard()

    async with (
        RtiConnection.connect("memory://fake-rti") as rti_s,
        RtiConnection.connect("memory://fake-rti") as rti_d,
    ):
        async with (
            rti_s.join_federation(
                federation, federate_name="sensor"
            ) as fed_sensor,
            rti_d.join_federation(
                federation, federate_name="dashboard"
            ) as fed_dashboard,
        ):
            # Declaration management. Sensor publishes; Dashboard
            # subscribes. Both calls are recorded by the in-process
            # transport; routing is the runner's job (no real RTI to
            # walk the FOM and dispatch).
            await fed_sensor.publish_object_class(
                "SensorReading", attributes=["value"]
            )
            await fed_dashboard.subscribe_object_class(
                "SensorReading", attributes=["value"]
            )

            # Sensor registers its instance. The handle comes from the
            # in-process transport's monotonic allocator. The Dashboard
            # learns about the instance via a DiscoverObjectInstance
            # event the runner synthesises right here (the in-process
            # transport doesn't auto-route discover events; production
            # rtid does, via DeclarationService + Registry hooks).
            obj_handle = await fed_sensor.register_object_instance(
                "SensorReading", instance_name="sensor-1"
            )
            transport.push_event(
                fed_dashboard.handle,
                DiscoverObjectInstance(
                    object_handle=obj_handle,
                    class_name="SensorReading",
                    instance_name="sensor-1",
                ),
            )

            # Drain the dashboard's event queue once to deliver the
            # discover before the first reflect. We deliberately do
            # this outside the per-tick loop so the discover is
            # observable as a separate event in the trace.
            await _drain_dashboard_once(
                transport, fed_dashboard, dashboard, expect_grant=False
            )

            # Per-tick loop. Each tick:
            #   1. Sensor computes the next value, calls update_attributes.
            #   2. Runner synthesises a ReflectAttributeValues event to
            #      the dashboard's queue.
            #   3. Dashboard drains its queue. The reflect event is
            #      fed to dashboard.handle_reflect; any TimeAdvanceGrant
            #      we triggered earlier is consumed.
            for tick in range(ticks):
                value = sensor.tick()
                if value is None:
                    break  # sensor stopped early; runner exits the loop
                payload = value.to_bytes(4, byteorder="big", signed=True)
                # Issue the update. The in-process transport records
                # the call; we then synthesise the reflect ourselves.
                await fed_sensor.update_attributes(
                    obj_handle,
                    {"value": payload},
                    timestamp=float(tick),
                )
                transport.push_event(
                    fed_dashboard.handle,
                    ReflectAttributeValues(
                        object_handle=obj_handle,
                        values={"value": payload},
                        timestamp=float(tick),
                    ),
                )
                await _drain_dashboard_once(
                    transport,
                    fed_dashboard,
                    dashboard,
                    expect_grant=False,
                )
                if verbose:
                    print(
                        f"tick={tick:3d}  "
                        f"sensor.published={len(sensor.published):3d}  "
                        f"dashboard.received={len(dashboard.received):3d}  "
                        f"value={value}",
                        flush=True,
                    )

    return {
        "published": list(sensor.published),
        "received": list(dashboard.received),
        "discovered": list(dashboard.discovered),
        "update_attribute_calls": len(transport.calls_for("update_attributes")),
        "register_object_calls": len(
            transport.calls_for("register_object_instance")
        ),
    }


async def _drain_dashboard_once(
    transport: InProcessTransport,
    fed_dashboard: Any,
    dashboard: Dashboard,
    *,
    expect_grant: bool,
) -> None:
    """Drain the dashboard's event queue until empty.

    The in-process transport's queue is filled by the runner's
    push_event calls; we don't issue NER on the dashboard side, so
    there are no auto-grants in the queue. ``expect_grant`` is wired
    here as a forward-compatible knob for a future variant that
    enables time regulation on the dashboard.
    """
    queue = transport.events_for(fed_dashboard.handle)
    while not queue.empty():
        event = await queue.get()
        if isinstance(event, DiscoverObjectInstance):
            dashboard.handle_discover(event.object_handle, event.instance_name)
        elif isinstance(event, ReflectAttributeValues):
            dashboard.handle_reflect(dict(event.values))
        elif isinstance(event, TimeAdvanceGrant):
            if not expect_grant:
                # Defensive — at this cut neither federate calls NER.
                continue
        # Other event kinds (FederationHalted etc.) are out of scope
        # for this pedagogical example; drop silently.


def verify(result: dict[str, Any]) -> tuple[bool, str]:
    """End-to-end checks:

    1. Dashboard's received list equals the sensor's published list.
    2. Discover happened exactly once (one instance was registered).
    3. The number of update_attribute wire calls equals the number
       of values the sensor published.
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
