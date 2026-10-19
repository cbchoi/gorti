# Portico comparison after server callback batching

## Result

In this fresh N=2 receive-order workload, Portico 2.1.4 completed faster than
gorti in all 20 measured pairs. Its aggregate received-callback throughput was
2.33562 times gorti's. The previous gorti-only improvement remains valid, but
does not establish that the current implementation outperforms Portico.

| Metric | Portico | gorti |
| --- | ---: | ---: |
| Measured trials | 20 | 20 |
| Mean receive completion, seconds | 0.681147 | 1.590904 |
| Median receive completion, seconds | 0.677385 | 1.585673 |
| Sample standard deviation, seconds | 0.029233 | 0.050029 |
| Minimum completion, seconds | 0.636177 | 1.519861 |
| Maximum completion, seconds | 0.744612 | 1.721135 |
| Coefficient of variation | 4.29% | 3.14% |
| Aggregate received callbacks/second | 119,210.7 | 51,040.2 |
| Mean per-trial received callbacks/second | 119,416.2 | 51,086.8 |

The ratio of mean completion times, gorti/Portico, is **2.33562**, with a paired,
execution-order-stratified bootstrap 95% interval of **[2.28333, 2.39336]**.
The bootstrap uses 10,000 resamples with seed 1516. Because both conditions
perform the same work, this ratio also describes Portico/gorti aggregate
throughput. Portico's mean receive-completion time is 57.18% shorter; gorti
provides 42.82% of Portico's aggregate throughput in this workload.

All observations were retained. There were no failed trials or replaced pairs.
The ten Portico-first pairs give a mean-time ratio of 2.37258; the ten gorti-first
pairs give 2.29912. Both order groups favor Portico.

## Experimental conditions

- Native Windows 11, build 26300, on the same host with 20 logical CPUs.
- Runtime-default CPU settings for both Go and Java; `GOMAXPROCS` unset.
- Go 1.26.5 and Java Temurin 11.0.31+11.
- Fresh processes for every trial: two independent federates for each RTI,
  plus gorti's separate server process.
- Identical FOM bytes, DEVStone-HLA HI 10 x 10 workload, per-pair binary plan
  and seed, sequential publisher choreography and normalized result checks.
- 100 external events: 40,600 attribute callbacks and 40,600 interaction
  callbacks, totaling 81,200 measured data callbacks per trial.
- Five warm-up pairs followed by 20 measured alternating AB/BA pairs. The
  measured seeds are 1516 through 1535; warm-up seeds are 1536 through 1540.
- An additional 128 warm-up operations per channel within each trial.
- gorti: `callback_async`, LocalLRC batch size 32, handle callbacks,
  8,192-entry outbox and no audit/replay plugin.
- Portico: existing Java federate, IPv4 loopback and the existing JGroups
  TCP/TCPPING override. Other ordering/reliability protocols are retained.
- Compact participant summaries; no per-message execution logging. Process
  output is captured in memory by the runner.

No tests, builds or profiling runs executed concurrently with measurement.
Ordinary background host activity was not isolated. This is an implementation
comparison under a common workload, not a language-only or equal-total-CPU-time
experiment. The different native transport topologies and process counts remain
part of the implementations being evaluated.

## Measurement boundary

Timing starts when the subscriber arms before VERIFY_START and ends at its
last data-callback receive timestamp. Go records event-pump dequeue before
DispatchEvent; Java records callback entry before payload validation. Subsequent
validation and successful teardown are required. gorti's final outcome-report
ACK drain is required but remains outside this interval.

Aggregate throughput is total received data callbacks divided by total measured
time, not the arithmetic mean of individual trial rates. These results are not
individual message latency, application-handler execution time, remote-outcome-
settled latency, TM performance or maximum sustainable saturation throughput.

Fresh JVMs and 128 intra-trial warm-up operations per channel do not establish
long-lived JVM steady-state performance. Process setup and teardown are excluded
from the primary metric. The mean full trial wall durations were 59.35 seconds
for Portico and 1.97 seconds for gorti, but they include the runner's distinct
startup/teardown coordination and timeouts. They are not an isolated RTI startup
benchmark and are not used to reverse the receive-throughput conclusion.

## Behavioral and build evidence

All 50 trials were accepted and all 25 pairs matched callback counts, ordered
payload hashes, callback traces, synchronization evidence and normalized terminal
state. Each trial delivered exactly 81,200 measured callbacks, with zero rejected,
dropped, duplicate, invalid or unexpected callbacks. Each implementation delivered
1,624,000 data callbacks across its 20 measured trials.

The server and Go federate were rebuilt from the current checkout. Their hashes
match the preceding server-batching campaign exactly. Thus the gorti server used
here is the same executable that passed the prepared 978-test required inventory
in the prior async service regression. That campaign also had 2,937 passing
top-level tests, zero failures and no skips. No redundant smoke/staging or full
regression campaign was inserted before these experiments.

Portico's JAR, Java version and all 50 Java class-file hashes match the preceding
Portico campaign. The extracted TCP/TCPPING XML is identical despite the newly
created override archive having a different hash. FOM and workload hashes also
match. No RTI or federate source
code was changed for this comparison. This workload match does not mean Portico
has been run through gorti's complete IEEE service regression inventory.

## Interpretation

The server-batching change previously reduced gorti's completion time by 48.34%
against its preceding server in a paired `GOMAXPROCS=2` experiment. The current
comparison uses runtime-default CPU settings and newly collected Portico trials.
Do not reuse the previous 54,794.7 callbacks/second gorti value in this table or
treat the difference from 51,040.2 as a demonstrated code regression.

The preceding Portico campaign averaged 1.865878 seconds and contained both a
fast cluster and much slower observations. This campaign's measured Portico
values all lie between 0.636177 and 0.744612 seconds. The artifact checks rule out
changes in the Java classes, JAR, transport XML, FOM and workload as an explanation. They do not
identify the runtime or host-state cause of the earlier variability. The present
verdict uses only contemporaneous paired measurements; neither campaign's slower
or faster trials have been discarded.

Mean per-trial API medians were 4.950/4.885 microseconds for Portico's attribute/
interaction sends and 0.925/0.900 microseconds for gorti. These are different
return boundaries: gorti's LocalLRC completes on local queue admission. They
cannot be ranked as equal-semantics remote service latency, and do not contradict
Portico's higher measured receive throughput.

This experiment locates a remaining end-to-end performance gap, not its cause.
No CPU, allocation, scheduler or mutex profiles were collected. The next useful
investigation is a separate profiled run of the same workload, followed by larger
federations and payload sizes. Keep profile-instrumented times separate from
unprofiled comparisons, and keep callback/reporting correctness as a constraint.

## Artifacts and reproduction

All generated data and executables remain outside Git:

- `D:/workspace/gorti/benchmark-results/portico-server-callback-batch-20261006/`
  contains `manifest.json`, `results.json`, `comparison.json`, binary workload
  plans, per-participant summaries and normalized evidence.
- `D:/workspace/gorti/benchmark-results/portico-server-callback-batch-builds-20261006/`
  contains the rebuilt server, Go federate and Java classes.

| Artifact | SHA-256 |
| --- | --- |
| gorti server | `768765cfd9265c0d316705c657f3fdcf18eae51db0c54ffa1722a743ed094dab` |
| Go federate | `a13ece3f3e6b987a99efa7aa40b90d6006171b1ef63fb7c2fe8d91199da876cd` |
| Portico JAR | `146dc0a7916b168396c6e595c42aaed0bdc69d64c5c54e728b7cb53fdd94ea84` |
| FOM | `633aaa61984966237f3675bcf005a5fb3722392f2c113ef41ca8b255e1a15040` |
| Workload | `a82b332fc2935c0e0433edc21128d5594049de26e59d9f00e91f7b45d4392940` |
| TCP override JAR | `84594b8d58af42437dcac16d2b39a4aab16101cdbece0ed8048e0dbe057a13ff` |

From the repository root, with those build artifacts available:

```powershell
$root = 'D:/workspace/gorti/benchmark-results'
$build = "$root/portico-server-callback-batch-builds-20261006"
$env:GOMAXPROCS = $null
python benchmark/callback-reporting/compare_portico.py `
  --gorti-server "$build/rtid-async.exe" `
  --gorti-client "$build/gorti-go-fair-async.exe" `
  --gorti-profile async --producer-mode local-lrc --batch-size 32 `
  --external-events 100 `
  --java 'C:/Program Files/Eclipse Adoptium/jdk-11.0.31.11-hotspot/bin/java.exe' `
  --java-classes "$build/classes" `
  --portico-jar .tools/portico-extracted/portico-2.1.4/lib/portico.jar `
  --output "$root/portico-server-callback-batch-repetition" `
  --warmup 5 --runs 20
```
