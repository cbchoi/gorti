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

## Why this example reads the model protocol manually

The cross-process entry points read the model's `ObjectClassFederateProtocol`
surface (publications, instance registrations, attribute updates,
discover/reflect handlers) and make the corresponding RPCs directly,
rather than going through `HLAFederate.step_once`. This is a
demonstration of the bridge's protocol shape, not a workaround:
the dashboard demo doesn't need LBTS / NER coordination because
its accounting (received == published) holds under wall-clock pacing.

As of M21, rtid's TimeService is wired and `HLAFederate.step_once`
works cross-process. Tightening this example to use `step_once`
is a future-cut ergonomics improvement; the model code stays
unchanged either way. For a federation that *does* exercise time
advance, see `examples/pyjevsim-time-advance/` (M21 W4B).

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
