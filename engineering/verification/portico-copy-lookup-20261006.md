# Portico comparison after callback-copy and MOM lookup optimization

## Result

The experiment started on 2026-10-06 and completed on 2026-10-07, Korea time.
In this campaign, gorti had higher aggregate throughput and shorter mean
completion time. It was faster in 15 of 20 measured pairs. However, Portico
exhibited substantially different fast and slow observations; its five fast
trials beat gorti. The result supports a campaign-specific average advantage,
not an unconditional implementation ranking.

| Metric | Portico | gorti |
| --- | ---: | ---: |
| Measured trials | 20 | 20 |
| Mean receive completion, seconds | 2.118250 | 1.363763 |
| Median receive completion, seconds | 2.418949 | 1.349536 |
| Sample standard deviation, seconds | 0.896906 | 0.049285 |
| Minimum completion, seconds | 0.706422 | 1.293780 |
| Maximum completion, seconds | 3.837631 | 1.512357 |
| Sample coefficient of variation | 42.34% | 3.61% |
| Aggregate received callbacks/second | 38,333.5 | 59,541.1 |
| Mean per-trial received callbacks/second | 52,173.3 | 59,611.7 |
| Faster measured pairs | 5 | 15 |

gorti's mean completion was **35.62% shorter**, and its aggregate throughput
was **55.32% higher**. The gorti/Portico mean-time ratio was **0.64382**, with an
execution-order-stratified paired-bootstrap 95% interval of
**[0.55015, 0.78760]**. Equivalently, gorti's aggregate-throughput advantage was
**1.55324x**, with an inverted interval of approximately **[1.27x, 1.82x]**.
The bootstrap uses 10,000 resamples with seed 1516.

All observations were retained, including Portico's 3.837631-second trial. No
trial was retried or replaced. The ten Portico-first pairs gave a gorti/Portico
mean-time ratio of 0.71074; the ten gorti-first pairs gave 0.58807. Both order
groups favor gorti on average. These intervals describe the sampled campaign;
they do not capture between-campaign host/runtime shifts or establish that
temporally clustered observations are independent.

## Conditions

The experiment uses the candidate server from the
[profile-guided optimization](callback-copy-lookup-20261006.md), not the preceding
server. Its async executable has already passed the prepared 978-test required
IEEE-service inventory and 2,940 top-level regression tests without failures or
skips. Tests were not rerun concurrently with performance measurements.

- Same Windows 11 build 26300 host, 20 logical CPUs, runtime-default Go/Java CPU
  settings; `GOMAXPROCS` is unset.
- Go 1.26.5; Portico 2.1.4 with Temurin Java 11.0.31+11.
- Two independent federate processes per implementation. gorti also uses its
  separate server. Every trial starts fresh processes.
- Identical FOM bytes, per-pair binary workload plan and seed, with sequential
  publisher choreography and common normalized result checks.
- DEVStone-HLA HI 10 x 10, 100 external events, 40,600 attribute callbacks and
  40,600 interaction callbacks: 81,200 measured callbacks per trial.
- Five warm-up pairs, then 20 measured AB/BA pairs. Measured seeds 1516-1535 and
  warm-up seeds 1536-1540. Each trial includes 128 warm-up operations per channel.
- gorti uses `callback_async`, LocalLRC batch size 32, handle callbacks, an
  8,192-entry outbox and no audit/replay plugin. ACKs, callback reporting and
  receipt validation remain enabled.
- Portico uses the existing Java participant with `HLA_IMMEDIATE` callbacks,
  IPv4 loopback and the existing JGroups TCP/TCPPING override.
- Compact summaries and no per-message execution logging. Process stdout/stderr
  is drained into memory. Generated results and logs remain outside Git.

No compilation, tests or profiling run concurrently with the experiment. Normal
host background activity is not isolated. This compares implementations and
their native SDKs, not Go versus Java in isolation or equal total CPU budgets.

## Measurement

Timing starts when the subscriber arms before VERIFY_START and ends at the
receive timestamp of its last data callback. Go records pump dequeue before
DispatchEvent; Java records callback entry before payload validation. Subsequent
payload/order validation, successful teardown and gorti's final report ACK drain
are required, but the drain is outside the timer.

Throughput is total received callbacks divided by total measured time. This is
a batch delivery-completion metric, not API return latency, handler-completion
latency, TM performance or maximum sustainable throughput. API return medians
are retained as diagnostics only because the completion boundaries differ.

Process setup and teardown are outside the primary measurement. Fresh JVMs and
128 warm-up operations per channel do not establish long-lived-JVM steady state.

## Repeated-campaign interpretation

The [preceding comparison](portico-server-callback-batch-20261006.md) measured
Portico at 0.681147 seconds and the preceding gorti server at 1.590904 seconds.
The new comparison must use its own paired observations, rather than combining
an old Portico mean with the latest gorti-only result at `GOMAXPROCS=2`.

The current Portico JAR, all 50 Java class-file hashes, Java version, transport
override XML, FOM, workload and execution schedule match the preceding campaign.
The Go federate executable is also identical; the gorti server is the new build.
Matching artifacts do not eliminate host/runtime variability. Any changed
Portico result cannot be attributed to the gorti code change.

Portico's current mean is 2.118250 seconds versus the preceding campaign's
0.681147 seconds. Thus the reversal of the mean ranking is not solely evidence
of gorti's code improvement. The separate gorti-only paired experiment
established a 15.80% completion reduction at `GOMAXPROCS=2`. Do not substitute
that experiment's 64,052 callbacks/second for the 59,541 here or treat their
difference as a demonstrated code regression.

A post-hoc descriptive split at one second exposes the current Portico samples:

| Portico observation group | Trials | Mean Portico seconds | Mean paired gorti seconds |
| --- | ---: | ---: | ---: |
| Below one second | 5 | 0.720361 | 1.358741 |
| At least one second | 15 | 2.584213 | 1.365438 |

The fast observations occurred at pairs 7, 8, 9, 11 and 12. They were not used to
replace or exclude the other observations. Portico was about 1.89 times faster
than gorti within those five pairs. This still leaves a relevant performance
gap to investigate, despite gorti winning the overall mean in this campaign.

Within Portico, the fast group's mean per-trial update/send API medians were
5.02/4.96 microseconds; the slow group's were 1.36/1.36 microseconds. Faster API
return therefore did not imply faster delivery completion. This association
does not identify a queueing, JIT, GC, scheduling or transport cause; none was
profiled during the timed experiment.

Before claiming a stable cross-RTI ranking, the next useful measurements are
separate timestamps for START synchronization completion, first data arrival and
last data arrival, followed by repeated batches within long-lived processes.
That can distinguish startup/control-path time from delivery work and expose
runtime-state changes without dropping unfavorable samples. The current primary
metric includes the final START synchronization after the subscriber is armed.

The workload's matching callback evidence does not mean Portico has been run
through gorti's complete internal IEEE-service regression inventory.

## Behavioral evidence

All 50 trials were accepted and all 25 pairs, including warm-up, matched the
normalized behavioral evidence: counts, payload/order hashes, callback traces,
synchronization and terminal state. Each implementation delivered 1,624,000
measured callbacks across its 20 measured trials. Every trial had zero rejected,
dropped, duplicate, invalid or unexpected callbacks.

Each gorti trial retained 162,934 subscriber reports, 81,456 local entries and
81,456 asynchronous outcomes across the participant lifetime, including warm-up
and controls. The existing runner also required all LocalLRC enqueued operations
to be sent and acknowledged, with no transport failures. No RTI or federate
source code changed for this campaign, and all experiment processes exited.

## Time-ordered observations

These are all measured pairs in collection order, not sorted by performance.

| Pair | Seed | First RTI | Portico seconds | gorti seconds |
| --- | ---: | --- | ---: | ---: |
| 1 | 1516 | Portico | 2.313986 | 1.345127 |
| 2 | 1517 | gorti | 2.191630 | 1.321966 |
| 3 | 1518 | Portico | 2.463824 | 1.444602 |
| 4 | 1519 | gorti | 2.518816 | 1.348390 |
| 5 | 1520 | Portico | 2.267380 | 1.338768 |
| 6 | 1521 | gorti | 2.884751 | 1.293780 |
| 7 | 1522 | Portico | 0.727853 | 1.401262 |
| 8 | 1523 | gorti | 0.734703 | 1.370470 |
| 9 | 1524 | Portico | 0.722588 | 1.377601 |
| 10 | 1525 | gorti | 2.423726 | 1.512357 |
| 11 | 1526 | Portico | 0.706422 | 1.316678 |
| 12 | 1527 | gorti | 0.710238 | 1.327694 |
| 13 | 1528 | Portico | 2.420764 | 1.350682 |
| 14 | 1529 | gorti | 3.837631 | 1.377914 |
| 15 | 1530 | Portico | 2.814306 | 1.376148 |
| 16 | 1531 | gorti | 2.593095 | 1.316799 |
| 17 | 1532 | Portico | 2.457839 | 1.344048 |
| 18 | 1533 | gorti | 2.802097 | 1.341384 |
| 19 | 1534 | Portico | 2.356216 | 1.387721 |
| 20 | 1535 | gorti | 2.417134 | 1.381878 |

## Artifacts

Generated evidence is in
`D:/workspace/gorti/benchmark-results/portico-copy-lookup-20261006/`, including
the manifest, execution schedule, plans, participant summaries, normalized
evidence and final comparison. Candidate Go binaries are in
`D:/workspace/gorti/benchmark-results/callback-copy-lookup-builds-20261006/`.
The unchanged Java classes are reused from
`D:/workspace/gorti/benchmark-results/portico-server-callback-batch-builds-20261006/classes/`.

| Artifact | SHA-256 |
| --- | --- |
| gorti server | `4f77da92379c3efa61f52dc295a7a1e6fc0be9b52811766b253e25bbce0d82e4` |
| Go federate | `a13ece3f3e6b987a99efa7aa40b90d6006171b1ef63fb7c2fe8d91199da876cd` |
| Portico JAR | `146dc0a7916b168396c6e595c42aaed0bdc69d64c5c54e728b7cb53fdd94ea84` |
| TCP override archive | `505e501bc71b6f2148229563567868d557d609e664f7f12361e40f1863e5514e` |

Reproduction from the repository root:

```powershell
$root = 'D:/workspace/gorti/benchmark-results'
$build = "$root/callback-copy-lookup-builds-20261006"
$env:GOMAXPROCS = $null
python benchmark/callback-reporting/compare_portico.py `
  --gorti-server "$build/rtid-async.exe" `
  --gorti-client "$build/gorti-go-fair-async.exe" `
  --gorti-profile async --producer-mode local-lrc --batch-size 32 `
  --external-events 100 `
  --java 'C:/Program Files/Eclipse Adoptium/jdk-11.0.31.11-hotspot/bin/java.exe' `
  --java-classes "$root/portico-server-callback-batch-builds-20261006/classes" `
  --portico-jar .tools/portico-extracted/portico-2.1.4/lib/portico.jar `
  --output "$root/portico-copy-lookup-repetition" `
  --warmup 5 --runs 20
```
