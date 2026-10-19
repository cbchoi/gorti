# Portico comparison after static FOM indexing

Date: 2026-10-07. The current gorti build was compared with Portico 2.1.4 in a
fixed native-process, same-seed crossover campaign. **Portico has the lower
observed mean and median completion time and the higher aggregate throughput.**
Its occasional long trials make the mean-ratio confidence interval wide enough
to include one. The experiment does not establish consistent gorti superiority.

No RTI, SDK or federate implementation changed for this comparison. The gorti
server is the [static FOM metadata build](fom-static-metadata-20261007.md).

## Experiment

The study plan was saved before execution. A is Portico and B is gorti. Every
workload seed runs four times, as ABBA or BAAB, so both orders are observed for
each seed. Sequence allocation is balanced and randomized with order seed
20261007. Five warm-up blocks precede twenty measured blocks: **50 trials per
RTI, including 40 measured trials per RTI**, 100 trials overall.

| Condition | Configuration |
| --- | --- |
| Host | Windows 11 build 26300, Core i7-12700, 20 logical CPUs |
| gorti | Go federates, static-index server, Go 1.26.5 builds, `callback_async`, LocalLRC batch 32, runtime-default `GOMAXPROCS` |
| Portico | Portico 2.1.4, Java federates, Temurin OpenJDK 11.0.31+11 |
| Processes | Two independent federate processes for each RTI; gorti also starts a standalone server; all processes are fresh each trial |
| Transport | Loopback TCP; Portico uses the existing JGroups TCP/TCPPING resource override |
| Workload | DEVStone-HLA HI width/depth 10, 100 external events, identical FOM bytes and per-seed binary plan |
| Callback count | 40,600 attribute reflections plus 40,600 interactions per trial |
| In-trial warm-up | 128 operations per channel |
| Logging | Compact participant summaries; Portico log level OFF; gorti error-level server logs; no per-message benchmark logging |

The Portico JAR, all 50 compiled Java class files and the FOM bytes match the
preceding Portico comparison. The new Go SDK/verifier executable is likewise
unchanged from preceding gorti increments. Manifests record artifact hashes and
full process arguments. No builds, tests or profiling overlapped timed trials.

The primary timer runs from receiver arming before VERIFY_START to the final
subsequently validated data receive timestamp. Go timestamps event-pump dequeue
before DispatchEvent; Java timestamps callback entry before payload validation.
Successful validation, completion and teardown are required. Final gorti
entry/outcome ACK drain is outside the primary interval. This is a received-data
completion comparison, not an equal API-return or remote-outcome-settled latency
comparison. API return medians are retained as metadata, not ranked here.

Fresh JVMs receive the same operation-count warm-up, but this is not a long-lived
JVM steady-state study. Host background activity is not isolated. N=2 receive-order
results do not establish TM, larger-federation or multi-host performance.

## Results

Only the twenty measured blocks contribute to the following statistics.
Throughput is total delivered callbacks divided by total measured seconds,
not the arithmetic mean of individual trial rates.

| Metric | Portico | gorti |
| --- | ---: | ---: |
| Measured trials | 40 | 40 |
| Mean completion, seconds | 0.919762 | 1.162298 |
| Median completion, seconds | 0.672423 | 1.148051 |
| Completion standard deviation, seconds | 0.686381 | 0.067179 |
| Minimum completion, seconds | 0.596114 | 1.089194 |
| Maximum completion, seconds | 3.289359 | 1.417305 |
| Aggregate received callbacks/second | **88,284** | **69,862** |
| Faster seed-block mean | **17/20** | **3/20** |
| Faster within-order pair | **35/40** | **5/40** |

The observed mean gives Portico **26.37% more aggregate throughput**. Equivalently,
gorti throughput is 20.87% lower and gorti mean completion is 26.37% longer.
The ratio of completion medians is **1.7073 gorti/Portico**; this is a separate
descriptive statistic, not the mean speed ratio.

The mean completion ratio is:

- **gorti / Portico = 1.263694**, 95% interval **[0.956849, 1.720296]**.
- Equivalently, gorti / Portico aggregate throughput = **0.791331**, interval
  **[0.581295, 1.045097]**.

The intervals use 10,000 percentile bootstrap replicates, seed 1516, resampling
whole four-trial seed blocks within ABBA/BAAB strata. There are twenty resampling
units, not forty independent pairs. The interval includes one, so the mean gap
does not exclude equal means at this confidence level. It does not establish
equivalence either. The interval describes this campaign, not between-session
uncertainty on an otherwise uncontrolled desktop host.

### Execution order

| Order | Portico mean s | gorti mean s | gorti/Portico throughput ratio |
| --- | ---: | ---: | ---: |
| AB: Portico first | 0.846721 | 1.144976 | 0.739510 |
| BA: gorti first | 0.992803 | 1.179620 | 0.841630 |

Both order means favor Portico. These strata share seeds and are not independent
replications. The second/first-in-pair duration ratio was 1.054995, with interval
[0.982182, 1.148505]. Unlike the preceding same-Go-build control, this campaign
does not establish a nonzero second-position effect at that confidence level.

### Variability

Portico had five measured observations between 2.25 and 3.29 seconds, retained
without exclusion. For seed 1529, its two executions completed in 0.686293 and
2.252136 seconds with the same normalized semantic evidence. Therefore seed
alone cannot account for that within-seed difference. Neither binaries nor
benchmark settings were changed between those trials.

The comparison does not diagnose the cause. No conclusion about JIT, GC, CPU
temperature, scheduling or network timers follows without separate diagnostic
measurements. The smaller observed gorti spread is specific to this campaign,
not proof of generally superior stability. Keep the long Portico observations
in the primary mean; do not select its fast trials or discard its slow ones to
manufacture a ranking.

Measured-trial wall time, including process startup, warm-up, controls and
teardown, averaged 60.173 seconds for Portico and 1.552 seconds for gorti. These
are lifecycle measurements of this runner, not the received-data interval above.
Their decomposition has not been measured. They must not be substituted for
callback throughput or used to reverse the primary comparison's interpretation.

## Behavioral Evidence

All **100/100 trials** were accepted, and all **25 four-trial seed blocks** had
matching normalized evidence: plan identity, exact callback counts, zero rejected,
dropped, unexpected, duplicate or invalid callbacks, synchronization markers,
payload/order hashes and normalized terminal state. No trial was retried,
excluded or replaced, and no outcome-dependent extra campaign was added.

Every gorti trial recorded 81,456 local entries and 81,456 outcomes including
warm-up, 162,934 subscriber reports, 81,456 publisher ACKed operations and zero
LocalLRC failures. Reporting, validation and drain were not disabled to improve
the score. Portico's internal reporting mechanism is not assumed identical;
the compared behavioral evidence is the participant-observable workload.

The gorti binaries match the preceding fresh 978/978 required IEEE-service
regression results in both profiles. That suite was not rerun for a benchmark-
only change. Portico was not run through the entire gorti IEEE evidence inventory;
passing this workload is not a full-conformance verdict for either RTI.

## Interpretation

The recent gorti implementation changes have measured before/after benefits,
but **they have not demonstrated a consistent lead over Portico**. In this
campaign Portico wins the observed mean, median and most matched comparisons,
while its long observations leave the mean-ratio estimate uncertain. The earlier
campaign whose mean favored gorti must not be treated as a universal ranking.

The next measurement work remains the M0 decomposition of control, delivery and
settlement, together with long-lived participants and independent-session
confirmation. Those experiments can distinguish lifecycle/runtime variation from
service throughput; this comparison alone does not identify the next bottleneck.
Do not attribute the result to Go versus Java in isolation from their SDKs,
process topology and middleware architecture.

## Reproduction And Artifacts

`compare_portico.py` now supports the same-seed crossover while retaining its
paired default. It reuses the existing participant runners and normalized
evidence validator. The common crossover analyzer accepts that normalized evidence
without synthesizing Go SDK summaries for Java. Eight focused tests pass,
including ratio direction, incomplete/mismatched evidence and failure retention.
Recomputing the analysis from saved observations reproduced `comparison.json`.

See the [benchmark instructions](../../benchmark/callback-reporting/README.md#native-portico-comparison).
This campaign used `--design crossover --order-seed 20261007 --warmup 5 --runs 20
--external-events 100 --producer-mode local-lrc --batch-size 32 --gorti-profile async`.
Java was `C:/Program Files/Eclipse Adoptium/jdk-11.0.31.11-hotspot/bin/java.exe`,
with classes from `portico-server-callback-batch-builds-20261006/classes` under
the external benchmark-results directory. The manifest records full paths.

All execution artifacts are outside Git in
`D:/workspace/gorti/benchmark-results/portico-fom-static-metadata-study-20261007/`:

- `study-plan.json`: the fixed plan saved before execution.
- `crossover/manifest.json` and `schedule.json`: conditions, artifacts and order.
- `crossover/results.json`: all 100 observations, including the 20 warm-up trials.
- `crossover/comparison.json`: primary statistics, order diagnostics and intervals.
- `crossover/block-*/`: all plans' participant evidence, commands and summaries.

| Artifact | SHA-256 |
| --- | --- |
| gorti async server | `25fa04118532a7638d64df9e9228879c4aa8777e052863e4a62f9b2b37cd3955` |
| Go SDK/verifier | `a13ece3f3e6b987a99efa7aa40b90d6006171b1ef63fb7c2fe8d91199da876cd` |
| Portico JAR | `146dc0a7916b168396c6e595c42aaed0bdc69d64c5c54e728b7cb53fdd94ea84` |
| FOM | `633aaa61984966237f3675bcf005a5fb3722392f2c113ef41ca8b255e1a15040` |
