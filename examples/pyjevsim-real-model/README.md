# Real pyjevsim models

`PulseGenerator` and `PulseSink` inherit from
`pyjevsim.behavior_model.BehaviorModel`. They run in separate federate
processes and exchange `Pulse` interactions through a third `rtid` process.
The run passes only when every published sequence is received once and in
order.

## Setup and run

From the repository root:

```bash
python -m pip install -e "./pysdk[dev,pyjevsim]"
python -m rti1516e._proto
bash examples/pyjevsim-real-model/run.sh
```

Windows PowerShell:

```powershell
.\examples\pyjevsim-real-model\run.ps1
```

The example requires Go 1.22 or later, Python 3.11 or later, and the pyjevsim
version selected by the SDK extra. Pass `--ticks 4 --drain-ticks 1` for a short
smoke run.

The runner builds `rtid` when necessary, starts all three processes, checks the
published and received sequences, and stops any remaining child process. Logs
and JSON results are kept under `.run/<timestamp>-<pid>-<token>/`; use
`--no-keep-workdir` to remove a passing run.

## Integration boundary

The generator's output port maps to an HLA interaction class. The receiving
federate decodes each callback and calls the sink model's `ext_trans` method.
Federation creation, declaration, delivery, resign, and teardown all cross
process boundaries.

Interactions use receive order. See
[`../pyjevsim-time-advance`](../pyjevsim-time-advance/README.md) for
federation-wide logical-time coordination.
