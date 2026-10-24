# Performance

## Workload

DEVStone-HLA generates receive-order object updates and interactions from a
deterministic model hierarchy. HI width/depth 10 with 100 external events
produces 40,600 attribute reflections and 40,600 received interactions per trial.

The primary metrics are receiver completion time and received-callback
throughput. API return, local queue admission, and remote acknowledgement are
separate completion boundaries.

## Portico comparison

The 7--8 October 2026 campaigns used Portico 2.1.4 with Java federates and
gorti with Go federates on Windows 11, an Intel Core i7-12700, and loopback TCP.
Each RTI ran two independent federate processes; gorti also ran its server.
gorti used the HLA core profile, the `callback_async` build, and LocalLRC
batches of 32.

Five warm-up seed blocks preceded 20 measured ABBA/BAAB blocks. Each block ran
both RTIs twice, giving 40 measured trials per RTI. Both campaigns completed
all 100 trials, including warm-ups.

| Campaign | Portico mean time (s) | gorti mean time (s) | Portico callbacks/s | gorti callbacks/s | gorti/Portico throughput ratio, 95% CI |
|---|---:|---:|---:|---:|---|
| 7 October | 0.920 | 1.162 | 88,284 | 69,862 | 0.791 [0.581, 1.045] |
| 8 October | 2.272 | 1.413 | 35,742 | 57,462 | 1.608 [1.320, 1.924] |

Throughput is total received callbacks divided by total measured time.
Portico had the lower mean completion time on 7 October; gorti had the lower
mean on 8 October. The ordering changed between sessions, so these results
do not establish a consistent advantage for either RTI.

Portico's JAR, JVM, participants, and FOM were unchanged between sessions.
The gorti server was unchanged; its Go SDK changed. The
[experiment record](https://github.com/cbchoi/gorti/blob/main/benchmark/callback-reporting/historical-results-20261007-08.md)
contains artifact hashes, per-session results, and a separate same-session
measurement of that SDK change.

These binaries predate the 19 October service-validation baseline
`4b11da478f423c0776f6ba6ae580f1670b61f2a9`. The later validation did not
remeasure performance.

## Measurement boundary

The receiver starts timing before `VERIFY_START` and stops at the last
subsequently validated data receive timestamp. Go records event-pump dequeue
before `DispatchEvent`; Java records callback entry before payload validation.
Final validation, successful teardown, and gorti report-ACK drain are required.
The final drain is outside the timing interval.

Each trial uses fresh processes and 128 warm-up operations per channel.
Portico logging is off, gorti logs errors, and participants write compact
summaries. The results concern same-host, two-federate receive-order traffic.

The 95% intervals resample whole seed blocks within execution-order strata,
using 10,000 bootstrap replicates and seed 1516. See
[reproducibility](reproducibility.md) for the analysis and run designs.

## Runtime settings

The default `gorti-hla-core` profile runs without audit journaling. The optional
`auditreplay` plugin adds event encoding and storage. Measure that profile
separately.

Confirmed calls wait for a server result. LocalLRC calls return on local
admission and expose remote completion through await/drain operations.
Callback reporting is synchronous by default; the `callback_async` build
pipelines eligible reports.

Record the following settings when comparing runs:

| Setting | Required detail |
|---|---|
| Server | HLA core or audit/replay; logging level |
| Producer | Confirmed or LocalLRC; batch size and ACK counters |
| Callback reporting | Build tag and binary hashes |
| Timing | Receiver completion, callback-body completion, or report drain |
| Workload | FOM, payload size/seed, counts, RO or TSO |
| Topology | Federate count, process placement, hosts, and network |

Use the [LocalLRC guide](local-lrc.md) for completion behavior, the
[callback-reporting guide](https://github.com/cbchoi/gorti/blob/main/benchmark/callback-reporting/README.md)
for build commands, and the [benchmark guide](benchmark.md) for execution.
