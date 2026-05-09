# pyjevsim dashboard example -- cross-process (bridged)

Same Sensor → Dashboard object-class topology as
[`examples/pyjevsim-dashboard`](../pyjevsim-dashboard/README.md), but
the federate models implement the bridge's
`ObjectClassFederateProtocol` shape -- declaring publications /
subscriptions / instance registrations on the **model** rather than
in the federate entry point. The pedagogical contrast: same wire
behavior, different developer ergonomics.

```text
   sensor.py exposes:                 dashboard.py exposes:
     object_class_publications()         object_class_subscriptions()
     register_instances()                discover_handler(...)
     attribute_update_handler()          reflect_handler(...)
   sensor_main.py reads those           dashboard_main.py reads those
   hooks and issues the                 hooks and dispatches drained
   corresponding RPCs.                  events to the model.
```

## Why we don't use `HLAFederate.step_once` here

Same reason as
[`examples/pyjevsim-relay-cross-process`](../pyjevsim-relay-cross-process/README.md#why-we-dont-use-hlafederatestep_once-here):
real rtid (M3+) does not yet wire the time-service gRPC handlers
(`timeService: nil`). The bridge's `HLAFederate.step_once` issues
`next_message_request` on every cycle, which would block forever
waiting for a grant. The cross-process entry points therefore read
the model's protocol surface and make the corresponding RPCs
manually.

When rtid ships TimeService, this example can be tightened by
replacing the manual cycle in `sensor_main.py` / `dashboard_main.py`
with `HLAFederate.step_once` -- the model code stays unchanged.

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
python3 examples/pyjevsim-dashboard-bridged/runner.py

# Knobs (defaults shown)
python3 examples/pyjevsim-dashboard-bridged/runner.py \
    --ticks 10 --mode sequence --tick-period 0.05 \
    --keep-tempdir
```

Sample output:

```text
runner: published=10  received=10  discovered=1  rtid_port=44983  verify=ok
```

## Run it -- manually (shell scripts)

4 terminals: rtid, dashboard (subscribe FIRST), sensor, then verify.

```bash
cd examples/pyjevsim-dashboard-bridged

./rtid_run.sh           # Terminal 1
./dashboard_run.sh      # Terminal 2 -- subscribe FIRST
./sensor_run.sh         # Terminal 3
./verify_run.sh         # Terminal 4 -- after both federates exit
```

Each federate prints `DONE` / `FAILED` summaries on exit;
`verify_run.sh` checks `received == published` plus
`DiscoverObjectInstance` was observed.

Tunables (env-overridable, same as bypass variant):

| var | default |
|---|---|
| `TICKS` | 10 |
| `MODE` | `sequence` (or `sine`) |
| `AMPLITUDE` | 100 |
| `TICK_PERIOD` | 0.05 |
| `DASHBOARD_DRAIN_TICKS` | 20 |
| `RESULT_DIR` | /tmp/pyjevsim-dashboard-bridged-cross |

## Files

| File | Role |
|------|------|
| `runner.py` | spawns rtid + 2 federates; reads JSONs; verifies |
| `_federate_common.py` | argparse / sys.path / federation spec / write_result |
| `sensor_main.py` | reads `model.object_class_publications()` + `register_instances()` + per-tick `attribute_update_handler()`; issues the RPCs |
| `dashboard_main.py` | reads `model.object_class_subscriptions()`; drain loop dispatches Discover/Reflect events to `model.discover_handler` / `model.reflect_handler` |
| `sensor.py` `dashboard.py` | model files (transport-free; expose the bridge's protocol surface) |
| `dashboard-fom.xml` | FOM declaring `SensorReading.value` |
| `_run_common.sh` `rtid_run.sh` `sensor_run.sh` `dashboard_run.sh` `verify_run.sh` | shell scaffolding for manual runs |

## Wire shape

Same as the bypass variant — one object class, one attribute. See
`dashboard-fom.xml`.
