# Portico comparison after lazy callback report signals

Date: 2026-10-08. All 100 trials passed. In this N=2 fresh-process receive-order
campaign, **gorti had lower mean and median completion times and 1.608 times
Portico's aggregate received-callback throughput**. This reverses the preceding
campaign's observed mean and median ranking, despite unchanged Portico artifacts.
The new gap must not be attributed entirely to the latest Go SDK optimization.

## Experiment

This comparison uses the Go SDK's lazy ACK and retirement signals from the
[latest improvement](../design/consistent-performance-plan-20261007.md#first-increment-lazy-ack-and-retirement-signals).
The RTI server, Portico JAR and Java participants are unchanged from the
[preceding Portico campaign](portico-fom-static-metadata-20261007.md).
No implementation or benchmark setting changes during this campaign.
Manifest comparison confirms identical Portico JAR, Java runtime, all 50 compiled
Java classes, FOM bytes and gorti server. The gorti SDK/verifier is the changed
artifact. No tests, builds or profiling overlapped timed trials.

A is Portico and B is gorti. Each seed executes an ABBA or BAAB block. Sequence
allocation is balanced and randomized using order seed 20261008. Five warm-up
blocks precede twenty measured blocks: **50 trials per RTI, of which 40 per RTI
contribute to the primary statistics**, 100 executions in total. Each execution
starts fresh independent participant processes.

| Condition | Configuration |
| --- | --- |
| Host | Windows 11 build 26300, Core i7-12700, 20 logical CPUs |
| gorti | Go federates, Go 1.26.5 build, `callback_async`, LocalLRC batch 32, runtime-default `GOMAXPROCS` |
| Portico | Portico 2.1.4, Java federates, Temurin OpenJDK 11.0.31+11 |
| Processes | Two federates per RTI; gorti additionally starts a standalone server |
| Transport | Loopback TCP; Portico's existing JGroups TCP/TCPPING resource override |
| Workload | DEVStone-HLA HI width/depth 10, 100 external events; identical FOM bytes and per-seed binary plans |
| Measured callbacks | 40,600 attribute reflections plus 40,600 interactions per trial |
| In-trial warm-up | 128 operations per channel |
| Logging | Compact participant summaries, Portico logging OFF, gorti server error level; no per-message benchmark logging |

The primary interval starts when the receiver arms before VERIFY_START and ends
at the final subsequently validated data receive timestamp. Go timestamps event
pump dequeue before DispatchEvent; Java timestamps callback entry before payload
validation. Final validation, successful teardown and gorti report ACK drain are
required; final drain remains outside the primary interval. This is a received
callback completion measurement, not equivalent API-return or remote-settled
latency for the two implementations.

Throughput is total delivered callbacks divided by total measured seconds.
Uncertainty uses 10,000 bootstrap samples, seed 1516, resampling whole four-trial
seed blocks within the ABBA/BAAB strata. The twenty measured blocks are the
resampling units, not forty independent pairs. Slow trials are not discarded;
failures are retained and stop the campaign instead of triggering replacements.

The receiver workload is receive-order, N=2 and single-host. Fresh JVMs receive
operation-count warm-up, not a long-lived steady-state warm-up. Host background
activity is not isolated. Results therefore do not establish a general RTI,
multi-host, time-management or scalability ranking.

## Results

Only the twenty measured blocks contribute to these statistics.

| Metric | Portico | gorti |
| --- | ---: | ---: |
| Measured trials | 40 | 40 |
| Mean completion, seconds | 2.271821 | **1.413107** |
| Median completion, seconds | 2.651503 | **1.249008** |
| Completion standard deviation, seconds | 1.260985 | 0.518157 |
| Minimum completion, seconds | 0.612144 | 1.140916 |
| Maximum completion, seconds | 4.612995 | 4.161532 |
| Aggregate received callbacks/second | 35,742 | **57,462** |
| Faster seed-block mean | 3/20 | **17/20** |
| Faster within-order pair | 14/40 | **26/40** |

gorti's observed aggregate throughput is **60.77% higher** and its mean completion
time is **37.80% lower**. The gorti/Portico throughput ratio is **1.607678**, with
whole-seed-block stratified bootstrap 95% interval **[1.319672, 1.924452]**.
Equivalently, the gorti/Portico mean completion ratio is **0.622015**, interval
**[0.519628, 0.757764]**. Equal means are outside this within-campaign interval.
This does not establish equal uncertainty across other sessions or workloads.

### Execution order

| Order | Portico mean s | gorti mean s | gorti/Portico throughput ratio |
| --- | ---: | ---: | ---: |
| AB: Portico first | 2.115885 | 1.388839 | 1.523492 |
| BA: gorti first | 2.427757 | 1.437375 | 1.689021 |

Both order strata favor gorti on the mean. They share seeds and are not independent
replications. The second/first-in-pair duration ratio was 1.074111, with interval
[0.859572, 1.352545]; this campaign does not establish a nonzero position effect
at that confidence level.

### Variability and comparison with the preceding session

All long observations remain in the primary statistics. For seed 1524, Portico's
two measured executions took 0.612144 and 2.770719 seconds with identical normalized
evidence. gorti also varied: its two executions for seed 1535 took 4.161532 and
1.535285 seconds. Seed alone therefore cannot explain the within-seed variation
in either implementation. No JIT, GC, scheduling, CPU-temperature or network cause
has been established by this uninstrumented comparison.

The preceding campaign observed Portico/gorti means of 0.919762/1.162298 seconds;
this campaign observes 2.271821/1.413107 seconds. Portico's artifact and Java class
hashes did not change. Consequently, the new comparative gap is not a measurement
of the Go SDK change's isolated effect. The separate same-session old/new gorti
experiment measured a **4.88%** throughput benefit from that change, not 60.77%.
Cross-session absolute times cannot substitute for a same-session old/new control.

The defensible conclusion is that gorti has the higher observed mean throughput
and lower mean and median completion in this campaign, with a block-bootstrap
interval favoring it. It is not uniformly faster: Portico wins 14/40 matched
within-order pairs and has the lower minimum. Together with the earlier reversal,
these results do not establish a session-independent RTI ranking. Long-lived
participant measurements remain a distinct follow-up, particularly because each
Portico trial here starts a new JVM.

Full trial wall time, including startup, warm-up, controls and teardown, averaged
80.636 seconds for Portico and 1.995 seconds for gorti. These lifecycle observations
are not used in the throughput calculation and must not be interpreted as the
received-callback interval or used to enlarge the primary speed ratio.

## Behavioral Evidence

All **100/100 trials** were accepted and all **25 seed blocks** had matching
normalized evidence: exact callback counts, payload/order hashes, synchronization
markers and terminal state. There were no rejected, dropped, unexpected, duplicate
or invalid callbacks. No trial was retried, excluded or replaced.

Every gorti trial retained 81,456 local entries, 81,456 asynchronous outcomes,
162,934 subscriber reports and 81,456 publisher ACKed operations, including warm-up
and control activity. All LocalLRC failure counts were zero. Reporting, outcome
drain and validation were not disabled. Recomputing the saved analysis from
`results.json` and `schedule.json` reproduced `comparison.json` exactly.

The current gorti implementation previously passed the 978 required IEEE-service
cases in both sync and async profiles, including the selected live restore tests.
That regression is separate from this benchmark. The whole gorti IEEE inventory
has not been run against Portico; success here is not a full-conformance verdict.

## Artifacts

All execution artifacts are outside Git under
`D:/workspace/gorti/benchmark-results/portico-report-signal-study-20261008/`:

- `manifest.json`: runtime conditions, binary/class hashes and timing boundaries.
- `schedule.json`: the schedule saved before execution.
- `results.json`: every trial observation, including warm-up.
- `comparison.json`: final comparison, or an incomplete record on failure.
- `block-*/`: participant evidence and process arguments.

| Artifact | SHA-256 |
| --- | --- |
| gorti async server | `25fa04118532a7638d64df9e9228879c4aa8777e052863e4a62f9b2b37cd3955` |
| gorti async SDK/verifier | `a93ae8c11cfa68262b1a1ac0f33a4def81af91689e7e8cf124a5b7708e5d01a1` |
| Portico JAR | `146dc0a7916b168396c6e595c42aaed0bdc69d64c5c54e728b7cb53fdd94ea84` |
| FOM | `633aaa61984966237f3675bcf005a5fb3722392f2c113ef41ca8b255e1a15040` |

Reproduction uses `benchmark/callback-reporting/compare_portico.py` with
`--design crossover --order-seed 20261008 --warmup 5 --runs 20
--external-events 100 --producer-mode local-lrc --batch-size 32
--gorti-profile async`. The manifest records the exact executable paths;
participant records retain full process arguments. No tests, builds or profiling
are run alongside timed trials.
