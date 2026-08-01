"""Dashboard federate entry point (bridged variant). Spawned by
``runner.py`` (or by ``dashboard_run.sh``) as a subprocess.

Boot:
  1. Connect, join as "dashboard".
  2. Honor the model's bridge-protocol subscription surface:
     subscribe_object_class for each entry of
     ``model.object_class_subscriptions()``.
  3. Drain loop: pop events; route DiscoverObjectInstance to
     ``model.discover_handler``, ReflectAttributeValues to
     ``model.reflect_handler``. This is what the bridge would
     do internally; we manually invoke the protocol because
     HLAFederate.step_once blocks on the unwired TimeService.
"""

from __future__ import annotations

import asyncio
import contextlib
import sys
from pathlib import Path
from typing import Any

# ruff: noqa: E402
from _federate_common import (  # type: ignore[import-not-found]
    common_parser,
    federation_spec,
    mark_ready,
    write_result,
)
from dashboard import Dashboard  # type: ignore[import-not-found]
from rti1516e.connection import RtiConnection  # type: ignore[import-not-found]
from rti1516e.events import (  # type: ignore[import-not-found]
    DiscoverObjectInstance,
    ReflectAttributeValues,
)


async def _drain_one(fed, model: Dashboard) -> int:
    transport = fed._transport  # noqa: SLF001
    queue = transport.events_for(fed.handle)
    consumed = 0
    while True:
        try:
            event = queue.get_nowait()
        except asyncio.QueueEmpty:
            return consumed
        consumed += 1
        if isinstance(event, DiscoverObjectInstance):
            model.discover_handler(
                event.object_handle, event.class_name, event.instance_name
            )
        elif isinstance(event, ReflectAttributeValues):
            values = dict(event.values)
            if "value" not in values and len(values) == 1:
                values = {"value": next(iter(values.values()))}
            model.reflect_handler(event.object_handle, values)
        # else: ignore other events (TimeAdvanceGrant etc.)


async def run(
    *,
    url: str,
    ticks: int,
    drain_ticks: int,
    tick_period: float,
    startup_delay: float,
    ready_file: str | None,
    publisher_done_file: str | None,
    **_unused: Any,
) -> dict[str, Any]:
    print(f"dashboard_main: connecting to {url}", file=sys.stderr, flush=True)
    model = Dashboard()
    spec = federation_spec()

    async with (
        RtiConnection.connect(url) as rti,
        rti.join_federation(spec, federate_name="dashboard") as fed,
    ):
            print(
                "dashboard_main: joined; honoring model subscriptions",
                file=sys.stderr, flush=True,
            )
            for class_name, attrs in model.object_class_subscriptions().items():
                await fed.subscribe_object_class(class_name, attributes=attrs)
            mark_ready(ready_file)
            if startup_delay > 0:
                await asyncio.sleep(startup_delay)

            if publisher_done_file is None:
                cycles = ticks + drain_ticks
                print(
                    f"dashboard_main: drain loop ({cycles} cycles)",
                    file=sys.stderr,
                    flush=True,
                )
                for _ in range(cycles):
                    await _drain_one(fed, model)
                    with contextlib.suppress(asyncio.CancelledError):
                        await asyncio.sleep(tick_period)
            else:
                done = Path(publisher_done_file)
                print(
                    "dashboard_main: draining until sensor completion",
                    file=sys.stderr,
                    flush=True,
                )
                while not done.exists():  # noqa: ASYNC240
                    await _drain_one(fed, model)
                    with contextlib.suppress(asyncio.CancelledError):
                        await asyncio.sleep(tick_period)
                for _ in range(drain_ticks):
                    await _drain_one(fed, model)
                    with contextlib.suppress(asyncio.CancelledError):
                        await asyncio.sleep(tick_period)
            await _drain_one(fed, model)  # final flush

    return {
        "received": list(model.received),
        "discovered": [list(d) for d in model.discovered],
    }


def main(argv: list[str] | None = None) -> int:
    parser = common_parser("dashboard_main")
    args = parser.parse_args(argv)
    try:
        result = asyncio.run(run(**vars(args)))
    except Exception as exc:  # noqa: BLE001
        print(f"dashboard_main: {exc}", file=sys.stderr)
        return 1
    write_result(args.result, result)
    return 0


if __name__ == "__main__":
    sys.exit(main())
