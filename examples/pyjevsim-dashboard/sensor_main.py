"""Sensor federate entry point. Spawned by ``runner.py`` (or by
``sensor_run.sh``) as a subprocess.

Boot:
  1. Connect to rtid, join as "sensor".
  2. publish_object_class("SensorReading", ["value"]).
  3. register_object_instance("SensorReading", instance_name="sensor-1").
  4. For each of ``ticks`` cycles, compute the next value via the
     :class:`Sensor` model, encode 4-byte BE signed, and call
     update_attributes.
  5. Sleep tick_period between cycles.
  6. Write {"published": [seq, ...], "instance_name": ...} to the
     result file.
"""

from __future__ import annotations

import asyncio
import contextlib
import sys
from typing import Any

# ruff: noqa: E402
from _federate_common import (  # type: ignore[import-not-found]
    common_parser,
    federation_spec,
    write_result,
)
from rti1516e.connection import RtiConnection  # type: ignore[import-not-found]
from sensor import Sensor  # type: ignore[import-not-found]


async def run(
    *,
    url: str,
    ticks: int,
    mode: str,
    amplitude: int,
    tick_period: float,
    startup_delay: float,
    **_unused: Any,
) -> dict[str, Any]:
    print(f"sensor_main: connecting to {url}", file=sys.stderr, flush=True)
    model = Sensor(stop_after=ticks, mode=mode, amplitude=amplitude)
    spec = federation_spec()
    instance_name = "sensor-1"
    async with RtiConnection.connect(url) as rti:
        async with rti.join_federation(spec, federate_name="sensor") as fed:
            print("sensor_main: joined; declaring publish", file=sys.stderr, flush=True)
            await fed.publish_object_class("SensorReading", attributes=["value"])
            if startup_delay > 0:
                await asyncio.sleep(startup_delay)
            obj_handle = await fed.register_object_instance(
                "SensorReading", instance_name=instance_name,
            )
            print(
                f"sensor_main: registered instance {instance_name} (handle={obj_handle})",
                file=sys.stderr, flush=True,
            )
            for tick in range(ticks):
                value = model.tick()
                if value is None:
                    break
                wire = int(value).to_bytes(4, byteorder="big", signed=True)
                await fed.update_attributes(
                    obj_handle,
                    {"value": wire},
                    timestamp=float(tick),
                )
                with contextlib.suppress(asyncio.CancelledError):
                    await asyncio.sleep(tick_period)

    return {
        "published": list(model.published),
        "instance_name": instance_name,
        "mode": mode,
    }


def main(argv: list[str] | None = None) -> int:
    parser = common_parser("sensor_main")
    args = parser.parse_args(argv)
    try:
        result = asyncio.run(run(**vars(args)))
    except Exception as exc:  # noqa: BLE001
        print(f"sensor_main: {exc}", file=sys.stderr)
        return 1
    write_result(args.result, result)
    return 0


if __name__ == "__main__":
    sys.exit(main())
