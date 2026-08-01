# pyjevsim time advance

Three Python federates use lookaheads 0.5, 1.0, and 2.0 while running Next
Message Request (NER) cycles against `rtid`. A model cycle starts only after
the full `TimeAdvanceGrant` arrives on that federate's event stream.

## Run

Prepare the Python SDK:

```bash
python -m pip install -e "./pysdk[dev]"
python -m rti1516e._proto
```

Run from any directory:

```bash
bash examples/pyjevsim-time-advance/run.sh
```

```powershell
.\examples\pyjevsim-time-advance\run.ps1
```

Go 1.22 or later is needed unless `--rtid-binary` names an existing server.
For a three-cycle run:

```bash
bash examples/pyjevsim-time-advance/run.sh --cycles 3 --tick-step 3.0
```

`--tick-step` must exceed the largest lookahead, 2.0.

## Check performed

With default arguments, every federate requests and receives `3, 6, 9, ...
30`. Each full grant must lead to exactly one `output_handler` call and one
`internal_transition` call. Missing results, an early model transition, or a
nonzero process exit fails the run.

`GrantDrivenModel.time_advance` selects the next requested time. Partial forced
grants can occur while federation contributions settle; `wait_for_full_grant`
continues waiting on the same request and does not count a partial grant as a
model cycle.

The Python SDK also exposes NMRA, TAR, TARA, and FQR. This scenario fixes NER
so all three participants follow one choreography.

| File | Role |
|---|---|
| `runner.py` | Starts the server and regulators and checks exact grants |
| `regulator_main.py` | Grant-driven federate model |
| `_federate_common.py` | Federation, grant-wait, and result helpers |
| `time-advance-fom.xml` | FOM shared by all regulators |

```bash
python -m pytest examples/pyjevsim-time-advance/test_time_advance.py -v
```

Logs and results are kept under `.run/run-*`; pass `--no-keep-workdir` to
remove a passing run.
