# pyjevsim object tracking

One producer registers a `Vehicle` and publishes timestamped `Position` and
`Speed` attributes. Two tracker federates receive the updates and feed them to
a pyjevsim-compatible model through `external_transition`.

The server, producer, and trackers are four separate processes. Time grants and
object callbacks come from `rtid`; there is no local event router.

## Run

Prepare the Python SDK:

```bash
python -m pip install -e "./pysdk[dev]"
python -m rti1516e._proto
```

Run from any directory:

```bash
bash examples/pyjevsim-object-tracking/run.sh
```

```powershell
.\examples\pyjevsim-object-tracking\run.ps1
```

Go 1.22 or later is needed unless `--rtid-binary` names an existing server.
For a shorter run:

```bash
bash examples/pyjevsim-object-tracking/run.sh --cycles 3 --tick-step 1.0
```

`--tick-step` must be at least the producer's 1.0 lookahead. Use `--help` for
timeouts, logging, work-directory, and server-binary options.

## Check performed

The default run has five model cycles. It publishes positions `2, 4, 6, 8,
10`, speed `5`, and logical times `1..5`. Both trackers must discover the same
object, receive the same ordered values, and complete one external transition
for each grant.

The producer regulates time; the trackers are constrained-only subscribers.
Timestamped reflections are released at each grant and passed to
`external_transition("reflect:Vehicle", attributes)`. The producer waits for
both subscriptions before registering the object because registrations made
before a matching subscription are not replayed by the current server.

## Files and test

| File | Role |
|---|---|
| `runner.py` | Starts four processes and compares their results |
| `producer_main.py` | Time-regulating vehicle model and publisher |
| `tracker_main.py` | Time-constrained subscriber and model adapter |
| `_federate_common.py` | FOM, grant, callback, and result helpers |
| `vehicle-fom.xml` | `Vehicle.Position` and `Vehicle.Speed` definitions |

```bash
python -m pytest examples/pyjevsim-object-tracking/test_object_tracking.py -v
```

Logs and results are stored in `.run/run-*` and retained by default. Pass
`--no-keep-workdir` after the runner options to remove a passing run.
