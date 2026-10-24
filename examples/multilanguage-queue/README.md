# Python DEVS source and Go workstation

A periodic source and a single-server workstation represent a small production
flow. Python reuses `PulseGenerator` and `PulseSink` from
`../pyjevsim-real-model/models.py` without changing their transitions. Go runs
a new nonpreemptive first-come, first-served workstation model. The models run
in two independent federate processes, with a third process running `rtid`.

## Run

Install Go 1.22+ and Python 3.11+, then from the repository root:

```sh
python -m pip install -e './pysdk[dev,pyjevsim]'
python -m rti1516e._proto
bash examples/multilanguage-queue/run.sh --output /tmp/gorti-model-results
```

Windows PowerShell:

```powershell
python -m pip install -e './pysdk[dev,pyjevsim]'
python -m rti1516e._proto
.\examples\multilanguage-queue\run.ps1 --output D:\results\gorti-model-results
```

The output directory must not already exist. Defaults are 100 jobs, seed 1516,
and three repeats of each of three service profiles: duration 1, duration 2,
and seeded integer durations 1--3. `--jobs`, `--repeats`, and `--seed` change
those inputs. The runner builds both Go executables and cleans up only its
own child processes. No external RTI installation is needed.
When WSL reads a Windows-managed Git worktree, pass `--source-commit` with
the full identifier obtained from Git on Windows. The per-file hashes still
identify the actual example source, including unpublished additions.

## Model and HLA mapping

The source fires at logical times 1, 2, ..., N, after each time grant.
`RealPyjevsimAdapter` captures `out_seq` and invokes the original `int_trans`.
One logical unit of transfer delay separates source output from workstation
arrival. This is a model parameter, not measured network latency. Both
federates are time regulating and constrained, with lookahead 1.

| Model operation | HLA representation |
|---|---|
| Source `out_seq` | `HLAinteractionRoot.Job(seq, service)` at source time + 1 |
| Workstation arrival | Timestamped `ReceiveInteraction`; pass timestamp and parameters to `workstation.Server.Accept` |
| Workstation departure | `HLAinteractionRoot.Completion(seq)` at computed finish time |
| Sink `in_seq` | Callback routed through the adapter to the original `ext_trans` |
| Progress | TAR and dispatched time-advance grants in both processes |

Parameters use four-byte big-endian positive integer values. The Go model
contains no HLA imports: on arrival it reserves service at
`start = max(arrival, previous_finish)`, then finishes at `start + service`.
There is no preemption or cancellation. Reserving the departure when the job
arrives is therefore equivalent to scheduling that future completion event.

Both federates use the SDK callback-dispatch API. A startup-only ready/start
barrier waits for both time-management callbacks and declarations; it carries
no jobs, model state, or per-step decisions. Every job and completion crosses
the RTI. Service durations are supplied only to the Python source and reach
Go through the `Job` interaction.

## Reference and results

`run.py` computes an independent, time-stepped FCFS reference with a waiting
queue and explicit completion events. It does not call the Go model or RTI.
Every observed arrival, service duration, start, finish, and waiting time must
match that reference. Completion callback timestamps, source IDs, sink IDs,
and both full grant sequences are checked separately. The existing model
files are hashed in `summary.json` to identify what was reused.

Each run directory contains inputs, reference and observed trajectories,
checks, and process logs. The campaign summary records source and binary
hashes, runtime versions, process identities, mean waiting/sojourn times and
maximum trajectory error. These are functional model results in logical time,
not a network-performance benchmark. Keep output directories outside Git.
