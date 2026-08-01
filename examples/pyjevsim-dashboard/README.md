# pyjevsim dashboard: direct SDK

This two-process federation exercises HLA object services directly. A sensor
registers `SensorReading` and updates its `value` attribute. A dashboard
subscribes to the class and records discovery and reflection callbacks.

```text
Sensor -- register/update --> rtid -- discover/reflect --> Dashboard
```

## Run

From the repository root, install the SDK and generate bindings once:

```bash
python -m pip install -e "./pysdk[dev]"
python -m rti1516e._proto
```

Run the scenario from any directory:

```bash
bash examples/pyjevsim-dashboard/run.sh
```

```powershell
.\examples\pyjevsim-dashboard\run.ps1
```

Go 1.22 or later is needed unless an existing server is supplied with
`--rtid-binary`. Runner arguments are forwarded by both scripts:

```bash
bash examples/pyjevsim-dashboard/run.sh --ticks 16 --mode sine --amplitude 25
```

Logs and JSON results remain in `.run/<timestamp>/` unless
`--no-keep-workdir` is set.

## Check performed

The default sensor values are `0..9`. The dashboard starts first, subscribes,
and then receives one registered object and ten signed big-endian integer
updates. The final check compares the published and reflected lists exactly
and requires discovery of `sensor-1`. A passing summary reports
`published=10`, `received=10`, `discovered=1`, and `verify=ok`.

`--mode sine` uses a deterministic eight-tick quantized waveform.

## Direct SDK boundary

Despite the directory name, this variant does not import pyjevsim or
`pyjevsim_bridge`. `sensor_main.py` and `dashboard_main.py` call the Python HLA
object API directly. The transport-independent `sensor.py` and `dashboard.py`
files hold only application state.

Compare this with
[`../pyjevsim-dashboard-bridged`](../pyjevsim-dashboard-bridged/README.md),
where model hooks own the RTI declarations and event handlers.

| File | Role |
|---|---|
| `runner.py` | Process startup, result checking, and cleanup |
| `sensor_main.py` | Publication, registration, and attribute updates |
| `dashboard_main.py` | Subscription and callback dispatch |
| `sensor.py`, `dashboard.py` | Transport-independent state |
| `dashboard-fom.xml` | `SensorReading.value` definition |

For a failed run, inspect `sensor.log`, `dashboard.log`, and `rtid.log` in the
printed work directory.
