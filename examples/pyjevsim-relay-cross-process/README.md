# pyjevsim relay

Three federates form a `Generator -> Buffer -> Processor` pipeline through a
real `rtid` listener.

```text
generator -- GenToBuffer(seq) --> buffer -- BufferToProc(seq) --> processor
```

Incoming interaction parameters are decoded and passed to each model's
`external_transition`; model outputs are sent back as HLA interactions.

## Run

Prepare the Python SDK from the repository root:

```bash
python -m pip install -e "./pysdk[dev]"
python -m rti1516e._proto
```

Then run either launcher:

```bash
bash examples/pyjevsim-relay-cross-process/run.sh
```

```powershell
.\examples\pyjevsim-relay-cross-process\run.ps1
```

Go 1.22 or later is needed unless `--rtid-binary` supplies the server. Runner
options pass through unchanged:

```bash
bash examples/pyjevsim-relay-cross-process/run.sh \
  --gen-messages 20 --capacity 5 --service-period 1 --drain-ticks 20
```

## Check performed

The default generator publishes `1..50`. Scheduling affects how many values
the bounded buffer forwards, so the verifier does not require one fixed split.
It does require:

- publication to equal `1..50`;
- no duplicates in any result list;
- processor input to match buffer output in order;
- no value to appear in both forwarded and dropped lists; and
- `forwarded | dropped | residual` to equal the published set.

All three federates write a ready marker after joining and declaring their
classes. The runner releases a shared start marker only after all markers
exist, which keeps startup scheduling out of the accounting result.

## Files and test

| File | Role |
|---|---|
| `runner.py` | Starts the processes, gates startup, and checks accounting |
| `generator_main.py`, `buffer_main.py`, `processor_main.py` | Federate entry points |
| `generator.py`, `buffer.py`, `processor.py` | pyjevsim-compatible models |
| `_federate_common.py` | Interaction mapping and startup barrier |
| `relay-fom.xml` | Relay interaction definitions |

```bash
python -m pytest examples/pyjevsim-relay-cross-process/test_relay_cross_process.py -v
```

Run data stays under `.run/run-*` unless `--no-keep-workdir` is passed.
