# pyjevsim producer/consumer

A producer and consumer run as separate Python federates. The producer sends
`ProducerOutput` interactions through `rtid`; the consumer records the decoded
payloads.

```text
Producer process -- ProducerOutput --> rtid -- ProducerOutput --> Consumer process
```

## Run

Install the Python SDK and generate its gRPC bindings from the repository root:

```bash
python -m pip install -e "./pysdk[dev]"
python -m rti1516e._proto
```

Then use either launcher:

```bash
bash examples/pyjevsim/run.sh
```

```powershell
.\examples\pyjevsim\run.ps1
```

Go 1.22 or later is needed unless `--rtid-binary` points to an existing
server. Both launchers accept the `runner.py` options; for example:

```bash
bash examples/pyjevsim/run.sh --ticks 10 --tick-period 0.02
```

Use `--help` for the full option list. Logs and JSON results are kept under
`.run/<timestamp>/`. Add `--no-keep-workdir` to remove them after a passing
run.

## Check performed

The consumer starts first so its subscription is active. The producer sends
the integers `1..50`, then stays joined for 30 drain ticks. The run passes only
when both processes exit cleanly and the consumer receives the exact producer
sequence without gaps, duplicates, or reordering. The final line contains
`verify=ok`.

## Model boundary

`Producer` and `Consumer` are plain Python classes implementing the methods
expected by `pyjevsim_bridge.CoupledModelProtocol`. The run checks the bridge
protocol and cross-process interaction path; it does not import the
external pyjevsim package or coordinate HLA logical time.

For actual `pyjevsim.behavior_model.BehaviorModel` subclasses, use
[`../pyjevsim-real-model`](../pyjevsim-real-model/README.md). The optional
`_real_pyjevsim_adapter.py` also shows how existing pyjevsim models can be
adapted to the bridge protocol.

## Main files

| File | Role |
|---|---|
| `runner.py` | Starts the server and federates, checks results, and cleans up |
| `producer_main.py`, `consumer_main.py` | Federate processes |
| `producer.py`, `consumer.py` | Transport-independent model code |
| `_federate_common.py` | SDK connection and interaction mapping |
| `pyjevsim-fom.xml` | `ProducerOutput` definition |

If the run fails, inspect `producer.log`, `consumer.log`, and `rtid.log` in the
printed work directory.
