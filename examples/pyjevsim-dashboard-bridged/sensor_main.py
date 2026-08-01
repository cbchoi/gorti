"""Sensor federate entry point (bridged variant). Spawned by
``runner.py`` (or by ``sensor_run.sh``) as a subprocess.

Boot:
  1. Connect to rtid, join as "sensor".
  2. Honor the model's bridge-protocol surface:
       - publish_object_class for each entry of
         ``model.object_class_publications()``
       - register_object_instance for each entry of
         ``model.register_instances()``
  3. Tick loop: call ``model.attribute_update_handler()``;
     for each (instance_name, attrs) pair, issue
     ``fed.update_attributes(handle, attrs)``. The bridge's
     ``HLAFederate`` would do the same -- but its step_once
     blocks on the unwired TimeService (see
     pyjevsim-relay-cross-process README §"Why we don't use
     HLAFederate.step_once here"). So this entry honors the
     ObjectClassFederateProtocol surface manually.
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
    mark_complete,
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
    publisher_done_file: str | None,
    **_unused: Any,
) -> dict[str, Any]:
    print(f"sensor_main: connecting to {url}", file=sys.stderr, flush=True)
    model = Sensor(stop_after=ticks, mode=mode, amplitude=amplitude)
    spec = federation_spec()

    async with (
        RtiConnection.connect(url) as rti,
        rti.join_federation(spec, federate_name="sensor") as fed,
    ):
            print("sensor_main: joined; honoring model publications", file=sys.stderr, flush=True)
            for class_name, attrs in model.object_class_publications().items():
                await fed.publish_object_class(class_name, attributes=attrs)
            if startup_delay > 0:
                await asyncio.sleep(startup_delay)

            instance_handles: dict[str, int] = {}
            for instance_name, class_name in model.register_instances().items():
                handle = await fed.register_object_instance(
                    class_name,
                    instance_name=instance_name,
                )
                instance_handles[instance_name] = handle
                print(
                    "sensor_main: registered "
                    f"{instance_name} (handle={handle}, class={class_name})",
                    file=sys.stderr,
                    flush=True,
                )

            print(f"sensor_main: tick loop ({ticks} cycles)", file=sys.stderr, flush=True)
            for _ in range(ticks):
                # Bridge-protocol shape: model returns
                # {instance_name: {attr: bytes}}. Empty dict means
                # the model has stopped.
                updates = model.attribute_update_handler()
                if not updates:
                    break
                for instance_name, attrs in updates.items():
                    handle = instance_handles.get(instance_name)
                    if handle is None:
                        print(
                            f"sensor_main: model produced update for unknown "
                            f"instance {instance_name!r}; ignoring",
                            file=sys.stderr, flush=True,
                        )
                        continue
                    await fed.update_attributes(handle, attrs)
                model.internal_transition()
                with contextlib.suppress(asyncio.CancelledError):
                    await asyncio.sleep(tick_period)
            mark_complete(publisher_done_file)

    return {
        "published": list(model.published),
        "instance_name": model.INSTANCE_NAME,
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
