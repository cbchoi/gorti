# DEVStone-HLA benchmark

The benchmark compares Portico and gorti with a deterministic workload derived
from DEVStone. DEVStone defines a synthetic hierarchy through a topology,
width, depth, event count, and transition cost. This repository uses that
hierarchy to generate RTI traffic; it does not measure a DEVS simulator kernel.

## Workload projection

The checked-in profile is HI with width 10, depth 10, and 100 injected events.
The published DEVStone equation yields 40,600 atomic event-value deliveries.
Each paired seed produces a `DVSHLA1` binary plan by traversing the generated
coupling graph. Portico and gorti consume the same plan bytes. Each delivery is
projected onto both receive-order OM paths:

- one object attribute update and reflection; and
- one interaction send and receive.

Both implementations therefore process 40,600 updates and 40,600 interactions
per measured execution. Payloads are generated deterministically from the
paired run seed.

## Experiment design

Each implementation runs five warm-up executions followed by 30 measured
executions. The measured runs form 30 matched pairs. Pair 1 runs Portico then
gorti, pair 2 runs gorti then Portico, and the pattern repeats, giving each
implementation 15 first-position runs. A fresh product process and two fresh
federate processes are used for every run. Each fresh process also performs an
untimed operation warm-up before `VERIFY_MEASURE` so JVM and Go initialization
do not enter the measured boundary.

The tracked gorti contract fixes handle-oriented callbacks, an 8,192-event
server outbox, 32-event internal batches, and a 1 ms partial-batch flush bound.
The LocalLRC profile additionally fixes a 1,024-operation admission capacity,
32-operation transport frames, and cumulative ACKs every 32 operations. The
runner records and validates these values in the comparison metadata.

For Portico, subscriber launch is gated by a transient marker written after the
publisher joins the federation and registers its object. The marker is removed
before subscriber launch and is never retained. This gate and the fixed 5000 ms
JGroups lifecycle response timeout are process-lifecycle controls outside the measurement
boundary; the marker is not a log. The result adapter requires
`protocol.portico_publisher_ready_gate` to be exactly `true`, and the manifest
records the controls and marker lifecycle.

Portico teardown is controlled by a three-phase transient handshake: the
subscriber resigns, the publisher resigns, and then the subscriber disconnects.
These phases run in that exact order after the measured callback boundary. The
phase evidence is discarded after validation, is not retained with the results,
and is not an execution log. The adapter requires
`protocol.portico_ordered_teardown_gate` to be exactly `true`; the manifest
records the ordered phases, evidence policy, and fixed 5000 ms JGroups
lifecycle response timeout.

The semantic gate requires complete delivery accounting; successful
`VERIFY_READY`, `VERIFY_MEASURE`, `VERIFY_START`, and `VERIFY_DONE`
synchronization; matching attribute, interaction, and terminal-state digests;
and zero rejected, dropped, unexpected, duplicate, or invalid callbacks.

## Run

Portico 2.1.4, Docker, Go, Java, and Python must be available. Set
`PORTICO_HOME` to the extracted Portico directory and choose an output path
outside the repository.

Windows PowerShell:

```powershell
$env:PORTICO_HOME = 'D:\workspace\gorti\gorti\.tools\portico-extracted\portico-2.1.4'
$env:GORTI_BENCHMARK_OUTPUT = 'D:\benchmark-results\devstone-hla-001'
.\benchmark\portico-gorti\run.ps1
```

Linux:

```bash
export PORTICO_HOME=/opt/portico-2.1.4
export GORTI_BENCHMARK_OUTPUT=/srv/benchmark-results/devstone-hla-001
bash benchmark/portico-gorti/run.sh
```

Use `--dry-run` with the Python runner to validate the tracked workload and
experiment contract without starting either RTI.

## Outputs

The external output directory contains only `manifest.json`, `results.json`,
`analysis.json`, and `comparison.tex`. The result document contains 60 accepted
measurements. The primary measurements are end-to-end callback completion and
throughput; synchronous API return latency is an implementation-specific
secondary measurement. The analysis reports median, p95, p99, paired comparisons,
order-adjusted estimates, and deterministic bootstrap 95% confidence
intervals. Process transcripts and event logs are not retained.

Two receive-order profiles are tracked. `experiment.json` selects gorti's
server-confirmed path. `experiment-local-lrc.json` fixes a 1,024-operation
LocalLRC capacity, 32-operation frames, cumulative ACK interval 32, and final
drain. Both stop when the final expected callback arrives at the subscriber
boundary, then validate the complete combined update/interaction callback
trace. Local queue-admission and server-confirmed return times are diagnostic
boundaries, not interchangeable performance claims.

The official Portico comparison fixes gorti to the `gorti-hla-core` profile.
The optional audit/replay module is not loaded, matching the benchmark's
disabled traffic-auditing condition. Every accepted run records a versioned
runtime profile, and the orchestrator rejects missing or mislabeled profiles.

`verification/portico/compare_gorti_journal_profiles.py` is a separate
intra-gorti ablation. It runs `gorti-audit-replay` and `gorti-hla-core` with
the same Go binaries, FOM bytes, DVSHLA plan, seed,
two-federate choreography, callback instrumentation, and measurement boundary.
Its primary statistic is the paired `core/audit` completed-batch ratio with a
deterministic, order-stratified bootstrap interval. This result measures event
journal overhead; it is not a full-semantic-equivalence or cross-product
ranking.
