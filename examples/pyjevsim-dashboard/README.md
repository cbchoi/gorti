# pyjevsim dashboard example -- cross-process

Two federates exercising the **object half** of HLA: a `Sensor`
publishes attribute updates on a `SensorReading` object instance, a
`Dashboard` subscribes and accumulates the reflected sequence. Each
federate runs as its own Python subprocess; rtid is a real
subprocess; everything talks `grpc://`.

```text
   +------------+   register_object_instance,   +-------------+
   | sensor_main| ===update_attributes("value")==> dashboard_main
   +------------+                                +-------------+
         |                                              ^
         |                                              | DiscoverObjectInstance
         |                                              | + ReflectAttributeValues
         |     grpc://127.0.0.1:8442                    |
         +--------------------+-------------------------+
                              v
                       +-------------+
                       |    rtid     |
                       +-------------+
```

This is the OBJECT-CLASS variant of the pyjevsim cross-process
examples: it uses `register_object_instance` /
`update_attributes` / `ReflectAttributeValues` rather than
`send_interaction` / `ReceiveInteraction`. The pedagogical contrast
with `examples/pyjevsim/` (interactions) is the point.

## Prerequisites

Same as the other cross-process examples -- see
[`examples/pyjevsim-relay-cross-process`](../pyjevsim-relay-cross-process/README.md#prerequisites).
One-time setup, **from the repo root**:

```bash
pip install -e './pysdk[dev]'
make py-codegen
```

## Run it -- orchestrated (runner.py)

```bash
# From the repo root
python3 examples/pyjevsim-dashboard/runner.py

# Knobs (defaults shown)
python3 examples/pyjevsim-dashboard/runner.py \
    --ticks 10 \
    --mode sequence \
    --tick-period 0.05 \
    --keep-tempdir
```

Sample output:

```text
runner: published=10  received=10  discovered=1  rtid_port=44219  verify=ok
```

`mode=sine` quantises a sine wave at the configured `--amplitude`
(default 100) instead of the default `0..N-1` sequence — useful when
demonstrating that arbitrary integer streams round-trip through
the wire.

## Run it -- manually (shell scripts)

4 terminals: rtid, dashboard (subscribe FIRST), sensor, then verify.

```bash
cd examples/pyjevsim-dashboard

# Terminal 1
./rtid_run.sh

# Terminal 2 -- subscribe FIRST so register/update don't race the join
./dashboard_run.sh

# Terminal 3 -- sensor publishes attribute updates
./sensor_run.sh

# Terminal 4 -- after both federates exit
./verify_run.sh
```

Each federate prints a `DONE` summary on exit:

```text
sensor_run:    DONE — published=10  result=...
dashboard_run: DONE — received=10 discovered=1  result=...
```

`verify_run.sh` checks `received == published` (gRPC's in-order
delivery guarantees this for one publisher + one subscriber) AND
that the dashboard saw a `DiscoverObjectInstance` event for the
sensor's instance.

Tunables (env-overridable):

| var | default | meaning |
|---|---|---|
| `TICKS` | 10 | number of update_attributes calls the sensor makes |
| `MODE` | sequence | `sequence` (`0..N-1`) or `sine` |
| `AMPLITUDE` | 100 | for `MODE=sine`: integer amplitude |
| `TICK_PERIOD` | 0.05 | wall-clock seconds per tick |
| `DASHBOARD_DRAIN_TICKS` | 20 | extra cycles dashboard runs after sensor stops |
| `RESULT_DIR` | /tmp/pyjevsim-dashboard-cross | where result JSONs land |

## Files

| File | Role |
|------|------|
| `runner.py` | spawns rtid + 2 federate processes; reads JSONs; verifies |
| `_federate_common.py` | minimal scaffolding: argparse, sys.path, federation spec, write_result |
| `sensor_main.py` | sensor entry: register_object_instance + update_attributes loop |
| `dashboard_main.py` | dashboard entry: subscribe + drain DiscoverObjectInstance / ReflectAttributeValues |
| `sensor.py` `dashboard.py` | model files (transport-free) |
| `dashboard-fom.xml` | FOM declaring the `SensorReading` object class with `value` attribute |
| `_run_common.sh` `rtid_run.sh` `sensor_run.sh` `dashboard_run.sh` `verify_run.sh` | shell scaffolding for manual runs |

## Why this example uses the low-level API directly

Unlike `examples/pyjevsim/` (which goes through `HLAFederate` and
the bridge's port-mapping), this example uses
`Federate.register_object_instance` / `Federate.update_attributes`
directly. The pyjevsim bridge's port-mapping is interaction-only
at this cut: its `time_advance.py::_run_internal_cycle` emits via
`send_interaction(...)` and there's no equivalent path for object
attribute updates yet. Object-class semantics are pedagogically
distinct from interactions and need their own integration; the
bridge can grow that surface in a later cut.

The bridged variant of the same example lives at
[`examples/pyjevsim-dashboard-bridged/`](../pyjevsim-dashboard-bridged/README.md)
-- it shows how the same model could be written via `HLAFederate`
once the bridge gains an object-class surface.

## Debugging tips

### `received != published` from verify_run.sh

Most common cause: the sensor started publishing before the
dashboard's `subscribe_object_class` round-tripped to rtid.
Pre-subscription updates are dropped server-side. Restart with
the dashboard launched first; the orchestrator runner.py does
this with a 0.5-second sleep between the dashboard spawn and
the sensor spawn.

### `discovered=0` in dashboard's result

Means the dashboard never saw `DiscoverObjectInstance`. Either
the dashboard subscribed AFTER the sensor's `register_object_instance`
(so the discover went out before any subscriber existed -- some
RTI implementations replay it, gorti currently does not), or the
subscribe is on the wrong class. Check the federate logs:

```bash
python3 runner.py --keep-tempdir
# tempdir printed at end; logs at:
#   <tempdir>/federate-logs/{sensor,dashboard}.log
#   <tempdir>/rtid.log
```
