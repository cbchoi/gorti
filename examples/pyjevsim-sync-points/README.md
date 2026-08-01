# pyjevsim synchronization points

Federates `alpha`, `beta`, and `gamma` rendezvous at `start_simulation`,
exchange `Tick` interactions, rendezvous at `end_simulation`, and resign.
`rtid` and all three participants are separate processes.

```text
join -> start barrier -> exchange N ticks -> end barrier -> resign
```

## Run

Prepare the Python SDK:

```bash
python -m pip install -e "./pysdk[dev]"
python -m rti1516e._proto
```

Use either launcher from any directory:

```bash
bash examples/pyjevsim-sync-points/run.sh
```

```powershell
.\examples\pyjevsim-sync-points\run.ps1
```

Go 1.22 or later is needed unless `--rtid-binary` supplies the server. For a
short run:

```bash
bash examples/pyjevsim-sync-points/run.sh --running-ticks 3
```

## Check performed

Each participant must achieve and observe both labels in order, publish
`1..10`, and receive two copies of each sequence, one from each peer. A `Tick`
received while a participant is waiting at either barrier fails the run.

The participant model implements the pyjevsim-compatible transition methods.
Synchronization state changes only after the matching gorti service call or
callback succeeds; the model does not simulate the barrier locally.

| File | Role |
|---|---|
| `runner.py` | Starts the server and participants and checks all results |
| `participant_main.py` | Federation lifecycle, barriers, and interaction flow |
| `participant.py` | pyjevsim-compatible participant model |
| `_federate_common.py` | Event waits, FOM decoding, and result helpers |
| `sync-points-fom.xml` | `Tick.seq` definition |

```bash
python -m pytest examples/pyjevsim-sync-points/test_sync_points.py -v
```

Logs and JSON results are kept under `.run/run-*` by default. Use
`--no-keep-workdir` to remove a passing run.
