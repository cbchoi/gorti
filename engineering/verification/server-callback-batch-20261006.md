# Server callback report batching

The subsequent [fresh Portico comparison](portico-server-callback-batch-20261006.md)
uses this server executable with runtime-default CPU settings. Its cross-RTI
results are separate from the `GOMAXPROCS=2` before/after experiment below.

## Plan

Reduce server work in the existing `callback_async` report stream without
removing callback receipts, changing service completion boundaries, or changing
the wire protocol. Keep the preceding LocalLRC SDK implementation fixed and
compare the preceding server with the new server. Use the prepared IEEE service
regression inventory as the implementation quality check.

## Implementation

The stream already carries up to 64 reports per frame. Previously, each report
repeated time-factory routing, authentication, receipt locking, event cloning and
MOM observation. The new path processes an eligible prefix under one authenticated
membership/checkpoint lease and one receipt-ledger critical section.

Only successful, ordinary, untimed, nonregional RO reflection and interaction
reports qualify. The production composition explicitly supplies this capability;
custom observer compositions do not silently bypass their observers. The port
converts callback metadata into domain accounting records. It does not clone the
attribute/parameter payload or construct unused detailed MOM argument lists.

MOM accounting uses the existing entry and pending-counter operations in their
original order. Checking whether detailed service/exception reporting is disabled
and applying accounting share the MOM lock. If reporting is enabled, the batch
declines without mutation and the original detailed observer runs. Optional
accounting failures remain unavailable counter coverage, not callback rejection.

TSO, conveyed regions, ownership, synchronization, save/restore, recovery,
unsuccessful outcomes and malformed reports retain individual processing. The
first ineligible report resumes that path. Failure ACKs retain the exact applied
prefix. Completed-receipt retries are resolved against committed retention state,
including tickets evicted during this frame. Membership and checkpoint leases
are released before sending ACKs or receiving another frame.

No SDK source, Protobuf message, RPC, journal or save-bundle format changed in
this increment. The default synchronous reporting profile remains unchanged.

## Review

The initial MOM equivalence tests were introduced before the new domain helper;
they initially failed to compile because that helper was absent, then passed
after implementation. Additional adapter and production-server tests cover:

- Sequential/batched counter, pending-count, overflow and object-history equivalence.
- No mutation when detailed MOM reporting requires fallback.
- One scope/accounting call for a prefix, entry deduplication and completed retries.
- Authentication failure, cancellation, unsupported wire versions and outcome-before-entry.
- Control, TSO, DDM, recovery and failed-outcome fallback.
- Exact failed-sequence ACKs, untouched suffixes and lease release before ACK.
- Retention-window eviction before a retry in the same frame.
- Authenticated Float64 and Integer64 production streams, foreign-token rejection,
  and switching MOM reporting off/on/off without losing counters or report serials.

Review identified a retention edge: planning a duplicate outcome before committing
earlier completions could overlook eviction of its receipt. A prefix now ends
before that duplicate and resolves it against the committed ledger. The explicit
eviction regression passes.

The final whole-service regression ran both build profiles once, including the
new cases and the live Float64/Integer64 server restart/control-result recovery
tests. Wire projection and retained-FDD schema checks were included.

| Profile | Required inventory | Top-level passes | Failures | Skips |
| --- | ---: | ---: | ---: | ---: |
| `callback_sync` | 978/978 | 2,921 | 0 | 16 async-only cases |
| `callback_async` | 978/978 | 2,937 | 0 | 0 |

No required test was skipped or relaxed. These are prepared internal service
regression results, not independent full IEEE conformance certification. Complete
Python and Java SDK campaigns were not rerun. A fresh race run was not performed:
the native C compiler required for Go race builds is unavailable on this host.

## Performance experiment

Both conditions used identical Go SDK executable bytes, two independent federate
processes, a fresh server per trial, identical FOM bytes and per-pair seeds. The
DEVStone-HLA HI 10 x 10 workload used 100 external events, producing 40,600
attribute callbacks and 40,600 interaction callbacks per measured trial.

The Windows host has 20 logical CPUs; both conditions used `GOMAXPROCS=2`,
`callback_async`, LocalLRC batch size 32, handle callbacks and compact summary
logging. Five warm-up pairs preceded 20 measured alternating AB/BA pairs. Each
trial also included 128 warm-up operations per channel. No tests or builds ran
during measurement. Ordinary host background activity was not isolated.

Timing begins with the subscriber armed before VERIFY_START and ends at dequeue
of the last subsequently validated callback, before DispatchEvent. Final report
ACK drain remains mandatory but outside that timer. This measures workload
delivery completion, not per-service latency or application-handler execution.

All 50 trials were accepted. All 25 pairs matched normalized callback counts,
payload/order hashes and lifecycle evidence. Every measured trial delivered
81,200 callbacks with zero rejected, dropped, duplicate, invalid or unexpected
callbacks.

| Metric | Previous server | Batched server |
| --- | ---: | ---: |
| Mean completion, seconds | 2.868679 | 1.481895 |
| Median completion, seconds | 2.734974 | 1.467093 |
| Completion standard deviation, seconds | 0.493787 | 0.068359 |
| Aggregate callbacks/second | 28,305.7 | 54,794.7 |
| Mean per-trial callbacks/second | 28,799.3 | 54,891.0 |

Mean completion decreased **48.34%**. Fixed-work aggregate throughput increased
**93.58%**. Mean-time speedup is **1.93582**, with a paired, execution-order-stratified
bootstrap 95% interval of **[1.84642, 2.09099]**. All 20 measured pairs favored the
candidate. The median-time ratio is 1.86421. Aggregate throughput is total
callbacks divided by total measured time, not the mean of per-trial rates.

All observations were retained, including the previous server's 4.932098-second
trial and the candidate's 1.757415-second trial. This estimates the combined
batching, payload-copy avoidance and disabled-report construction changes; it
does not separately attribute gains to each change.

## Follow-up performance analysis

This analysis reuses the completed campaign; it is not a new timing run. No
production code changed and no CPU, allocation or mutex profile was collected.

### Robustness

| Statistic | Previous server | Batched server |
| --- | ---: | ---: |
| Minimum completion, seconds | 2.666455 | 1.437357 |
| Completion interquartile interval, seconds | 2.715650 to 2.799686 | 1.448935 to 1.487125 |
| Maximum completion, seconds | 4.932098 | 1.757415 |
| Median absolute deviation, seconds | 0.045585 | 0.018326 |
| Sample coefficient of variation | 17.21% | 4.61% |

Quantiles use linear interpolation at `(n-1)*p`; MAD is unscaled. Median
completion fell 46.36%, and MAD fell 59.80%. Thus the improvement is not only
an artifact of the previous server's slowest observation. As a post-hoc
sensitivity check, excluding both observations of pair 8 gives a mean-time
speedup of 1.86400 across the remaining 19 pairs. The primary result still
includes that pair and remains 1.93582.

Execution order does not reverse the result: the ten baseline-first pairs yield
a ratio of mean times of 1.96620; the ten candidate-first pairs yield 1.90434.
Every paired speedup lies between 1.73273 and 3.27935. These checks do not
identify the cause of the outlier or establish population tail-latency bounds
from only 20 observations.

### Work and transport evidence

The following counters cover the participant lifetime, including its warm-up
and control traffic. They are not restricted to the timed 81,200 callbacks.
Frame counts mean application-level stream frames, not TCP packets.

| Counter or derived value | Previous server | Batched server |
| --- | ---: | ---: |
| Subscriber logical reports per trial | 162,934 | 162,934 |
| Subscriber local data entries per trial | 81,456 | 81,456 |
| Subscriber asynchronous data outcomes per trial | 81,456 | 81,456 |
| Mean callback-report frames per trial | 2,928.35 | 5,331.50 |
| Reports per frame, pooled across trials | 55.64 | 30.56 |
| Publisher LocalLRC enqueued/sent/ACKed operations per trial | 81,456 | 81,456 |
| Mean publisher operation frames per trial | 3,498.90 | 2,674.80 |
| Operations per frame, pooled across trials | 23.28 | 30.45 |
| Median of per-trial update API medians, microseconds | 0.8 | 0.8 |
| Median of per-trial send API medians, microseconds | 0.8 | 0.8 |

The logical report count is identical in every pair. Remote reporting and
LocalLRC operation acknowledgement were not removed to obtain the speedup.
Callback-report frames increased 82.06%, while publisher operation frames
decreased 23.55%. The result therefore cannot be explained simply as sending
fewer callback-report frames. It supports the value of reducing server work per
report, together with the resulting changes in pipeline scheduling and batching.

The SDK drains the currently available report queue into bounded windows; it
does not wait to fill every 64-report frame. Faster server completion is consistent
with more frequent, less-filled report windows. That mechanism is an inference
from the unchanged SDK code and the counters, not a measured scheduler trace.
No claim is made that the changed frame sizes independently caused the gain.

The current report frames average 47.75% of the 64-report maximum. Publisher
LocalLRC frames average 95.17% of their 32-operation maximum. Consequently, a
bounded report-coalescing experiment is more directly motivated by these counters
than assuming that publisher frames are routinely underfilled. Waiting to collect
reports can also increase latency or delay service barriers, so full frames are
not automatically preferable.

The 0.8-microsecond API medians measure local queue admission, not remote service
completion or recipient delivery. Their equality is compatible with much faster
end-to-end workload delivery. Neither these medians nor the reciprocal throughput
provide per-message network latency, API tail latency or final-report-drain time.

### Remaining hypotheses and next measurements

The combined server change removes repeated routing/authentication, full event
clones in ordinary entry/outcome processing, repeated accounting locks and
detailed MOM argument construction when reporting is disabled. The experiment
establishes a gain for the combined change, not the CPU contribution of each.

The current batch still holds the receipt mutex while MOM takes its manager and
traffic locks. Its planner constructs receipt keys and a state map, and the
composition allocates accounting slices. These are inspected cost sites, not
profile-confirmed dominant bottlenecks. Per-callback protobuf reports also remain
on the wire even when their server processing is batched.

Prioritize a separate CPU/allocation/mutex profiling run on the unchanged
workload, followed by N=4 and larger-recipient experiments. Use those results to
choose between report coalescing, scratch-allocation reduction and finer-grained
receipt/MOM locking. Keep profiled timings separate from uninstrumented comparison
results and preserve callback order, failed-prefix ACKs, checkpoint admission and
MOM reporting transitions in every candidate.

The historical Portico mean of 1.865878 seconds and this candidate mean of
1.481895 seconds are not a fresh paired comparison. The prior campaign used
runtime-default CPU settings, different measured seeds and a different collection
period. It also contained a fast Portico cluster whose cause remains unresolved.
Do not derive a current cross-RTI speedup or ranking from those two averages.

## Reflect

Retain the implementation: it passed the prepared service regression and reduced
completion time in this paired workload. Per-callback state is still retained
where recovery needs it; neither receipts nor remote report completion were
disabled. The improvement comes from processing the same evidence more cheaply.

This run compares current gorti implementations at N=2. Portico was not rerun in
this increment. The preceding Portico campaign used runtime-default CPU settings,
so its historical values must not be combined with this `GOMAXPROCS=2` campaign
to claim a new cross-RTI ranking. A fresh paired cross-RTI campaign and larger
federations are the next performance questions, especially because the receipt
and MOM locks are still shared resources.

## Reproduction and artifacts

Results are outside the repository under `D:/workspace/gorti/benchmark-results/`:

- `server-callback-batch-ieee-qc-20261006/summary.json`: full regression summary.
- `server-callback-batch-paired-20261006/`: manifest, plans, participant logs,
  50 trial records and statistical comparison.
- `server-callback-batch-builds-20261006/`: sync/async servers and SDK executables.
- `server-callback-batch-builds-20261006/source-server.tgz`: captured Go module
  files, gRPC adapter, server composition, MOM domain and callback benchmark
  sources. This is a scoped source archive, not a complete repository snapshot.

SHA-256 identities:

| Artifact | SHA-256 |
| --- | --- |
| Previous async server | `90893f4492b12e105359c83581d7fff9c15e481fd62301bd0cdb4e85c3670dad` |
| New async server | `768765cfd9265c0d316705c657f3fdcf18eae51db0c54ffa1722a743ed094dab` |
| New sync server | `a4406a37aa364f95cbf8dffff93f9b3ed299e54b058102d5168191d061ee9419` |
| Identical SDK in both conditions | `a13ece3f3e6b987a99efa7aa40b90d6006171b1ef63fb7c2fe8d91199da876cd` |
| Source archive | `c357930aab395fc278e401b025f53d9c482bdee528f37876abe115255190a717` |

From the repository root, after building with `benchmark/callback-reporting/build.ps1`:

```powershell
$root = 'D:/workspace/gorti/benchmark-results'
$old = "$root/callback-pipeline-headroom-builds-20261006"
$new = "$root/server-callback-batch-builds-20261006"
$env:GOMAXPROCS = '2'
python benchmark/callback-reporting/compare_transports.py `
  --baseline-server "$old/rtid-async.exe" `
  --baseline-client "$old/gorti-go-fair-async.exe" `
  --server "$new/rtid-async.exe" --client "$old/gorti-go-fair-async.exe" `
  --baseline-mode local-lrc --candidate-mode local-lrc `
  --external-events 100 --warmup 5 --runs 20 `
  --output "$root/server-callback-batch-repetition"
```
