# pyjevsim dashboard: bridge hooks

This scenario has the same wire behavior and verifier as the direct dashboard
example. The difference is in the model boundary: publication, subscription,
registration, discovery, and reflection are declared by transport-independent
model hooks.

```text
Sensor hooks --> sensor_main --> rtid --> dashboard_main --> Dashboard hooks
```

## Run

Prepare the Python SDK from the repository root:

```bash
python -m pip install -e "./pysdk[dev]"
python -m rti1516e._proto
```

Then run either launcher:

```bash
bash examples/pyjevsim-dashboard-bridged/run.sh
```

```powershell
.\examples\pyjevsim-dashboard-bridged\run.ps1
```

Go 1.22 or later is needed when no server is supplied with `--rtid-binary`.
Both scripts pass extra arguments to `runner.py`:

```bash
bash examples/pyjevsim-dashboard-bridged/run.sh --ticks 16 --mode sine --amplitude 25
```

## Check performed

The no-argument run publishes `0..9` as `SensorReading.value`. It passes when
both federates exit cleanly, the dashboard discovers the sensor, and the
reflected list matches the published list exactly. The summary contains
`published=10`, `received=10`, `discovered=1`, and `verify=ok`.

## Model hooks

| Sensor | Dashboard |
|---|---|
| `object_class_publications()` | `object_class_subscriptions()` |
| `register_instances()` | `discover_handler(...)` |
| `attribute_update_handler()` | `reflect_handler(...)` |

The models are plain Python classes that follow the bridge protocol. The
entry points translate their hooks into `rti1516e` calls; they do not construct
`HLAFederate` or import the external pyjevsim package. Actual pyjevsim models
are covered by [`../pyjevsim-real-model`](../pyjevsim-real-model/README.md).

| File | Role |
|---|---|
| `runner.py` | Process startup, exact comparison, and cleanup |
| `sensor.py`, `dashboard.py` | Models and RTI-facing hooks |
| `sensor_main.py`, `dashboard_main.py` | Hook-to-SDK adapters |
| `dashboard-fom.xml` | `SensorReading.value` definition |

Run artifacts are kept under `.run/<timestamp>/`. Inspect `sensor.log`,
`dashboard.log`, and `rtid.log` there when verification fails.
