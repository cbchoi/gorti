# Performance

## What is measured

DEVStone-HLA turns a deterministic model hierarchy into receive-order object
updates and interactions. The HI width/depth 10 workload with 100 external
events produces 40,600 attribute reflections and 40,600 received interactions
per measured trial.

The primary metrics are receiver completion time and received-callback
throughput. Caller return, local queue admission, server acknowledgement, and
callback-report drain are separate measurements. Choose the boundary before
comparing implementations.

## October 2026 Portico comparisons

These are historical 7--8 October measurements. Their binaries predate the
19 October service-validation baseline `4b11da478f423c0776f6ba6ae580f1670b61f2a9`.
The later test campaign verifies behavior; it does not remeasure performance.
Use the [retained experiment record](https://github.com/cbchoi/gorti/blob/main/benchmark/callback-reporting/historical-results-20261007-08.md)
for artifact identities, methods, and the separate SDK-change experiment.

Both campaigns used Portico 2.1.4 with Java federates and gorti with Go federates
on Windows 11, an Intel Core i7-12700, and loopback TCP. Each RTI ran two
independent federate processes; gorti also ran its standalone server. gorti
used the HLA core profile, the `callback_async` build, and LocalLRC batches of 32.

Five warm-up seed blocks preceded 20 measured blocks. Each ABBA or BAAB block
ran both RTIs twice, giving 40 measured trials per RTI. Both campaigns completed
100/100 trials, including warm-ups. Confidence intervals resample whole seed
blocks within execution-order strata.

| Campaign | Portico mean time (s) | gorti mean time (s) | Portico callbacks/s | gorti callbacks/s | gorti/Portico throughput ratio, 95% CI |
|---|---:|---:|---:|---:|---|
| 7 October, FOM metadata cache | 0.920 | 1.162 | 88,284 | 69,862 | 0.791 [0.581, 1.045] |
| 8 October, lazy report signals | 2.272 | 1.413 | 35,742 | 57,462 | 1.608 [1.320, 1.924] |

Throughput is total received callbacks divided by total measured time.
The 8 October campaign favored gorti in mean and median completion time and
in 17 of 20 seed-block means. The preceding session favored Portico in its
central estimates. Portico's JAR, JVM, compiled participants, and FOM were
unchanged between these sessions; the gorti server was also unchanged.
Session variability therefore matters when interpreting the ranking.

These runs show that gorti can provide comparable or higher throughput in
this tested configuration, but they do not show a stable ranking across
sessions. They compare the complete Go/gorti and Java/Portico stacks, not
Go and Java as programming languages in isolation.

The isolated, same-session old/new Go SDK experiment measured a 4.88%
throughput improvement from lazy callback-report completion signals
(95% ratio interval 1.014--1.088). That result measures the SDK change;
the 60.8% cross-RTI difference on 8 October measures the complete tested stacks.

Source reports record methods, artifact hashes, result summaries, commands,
and references to retained external artifacts. Raw execution output is not
stored in Git:

- [7 October comparison](https://github.com/cbchoi/gorti/blob/main/engineering/verification/portico-fom-static-metadata-20261007.md)
- [8 October comparison](https://github.com/cbchoi/gorti/blob/main/engineering/verification/portico-report-signal-20261008.md)
- [Isolated SDK change](https://github.com/cbchoi/gorti/blob/main/engineering/design/consistent-performance-plan-20261007.md)

### Timing and execution conditions

The receiver starts its timer before `VERIFY_START` and stops at the last
subsequently validated data receive timestamp. Go timestamps event-pump dequeue
before `DispatchEvent`; Java timestamps callback entry before payload
validation. Final validation, successful teardown, and gorti report-ACK drain
are required, with the final drain outside the primary interval.

Every trial starts fresh processes and performs 128 warm-up operations per
channel. Portico logging is off, gorti logs errors, and participants write
compact summaries rather than per-message diagnostics. These are same-host,
two-federate receive-order measurements. Long-lived JVM, multi-host, TM, and
larger-federation measurements require separate workloads.

## Implemented optimizations

- Go LocalLRC queues receive-order operations, batches requests, and receives
  cumulative acknowledgements. Explicit confirmed methods remain available.
- The `callback_async` build admits eligible data callbacks locally and sends
  ordered entry/outcome reports through a bounded pipeline.
- A session-owned worker processes report completion; completion signals are
  allocated when a waiter needs them.
- Eligible server report batches share validation and accounting work.
- Static FOM metadata and handle-oriented callbacks reduce repeated lookups
  and name-map projection.

Callbacks retain identity, outcome reporting, backpressure, and restore state.
The [LocalLRC guide](local-lrc.md) describes eligibility and drain behavior.

## Runtime profiles

Human-readable diagnostics use Go `slog`. The default `gorti-hla-core` profile
does not load the audit journal. The optional `auditreplay` plugin adds semantic
event encoding and storage and is measured separately as `gorti-audit-replay`.

Confirmed calls wait for the server's result. Local admission returns earlier
and reports remote failures through completion/drain operations. Neither
boundary is a substitute for the receiver's completion time.

Record these choices explicitly in a new experiment:

| Choice | What to report |
|---|---|
| Server profile | HLA core or audit/replay, with logging settings |
| Producer transport | Confirmed or LocalLRC, batch size, admission and ACK counters |
| Callback reporting | Default/`callback_sync` or `callback_async`, with both binary hashes |
| Completion boundary | Receiver completion, callback-body completion, or remote report drain |
| Workload | FOM, payload size/seed, object and interaction counts, RO or TSO |
| Topology | Federate count, process placement, hosts, and network |

The default build uses synchronous callback reporting. The faster asynchronous
reporting profile is selected at build time; it is separate from receive-order
LocalLRC send admission. The [callback-reporting guide](https://github.com/cbchoi/gorti/blob/main/benchmark/callback-reporting/README.md)
gives the build commands and eligible callback types.

## Earlier measurements

The July 2026 paired DEVStone-HLA campaigns and the anonymized reference-RTI
service comparison remain in dated engineering and
[verification records](https://github.com/cbchoi/gorti/blob/main/verification/RESULTS.md).
They used earlier builds or different workloads. Use their own manifests and
measurement definitions when reproducing them.

See [reproducibility](reproducibility.md) for paired and crossover execution,
and the [roadmap](roadmap.md) for further measurements.
