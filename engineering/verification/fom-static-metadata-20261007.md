# Static FOM metadata indexing

Date: 2026-10-07. The static-index portion of M3 is implemented and measured.
Both prepared IEEE-service profiles pass all 978 required cases. A fixed
same-seed crossover campaign shows **3.92% higher aggregate received throughput**
than the final M2 server, with a 95% speedup interval **[1.027088, 1.051105]**.
Execution-position effects remain visible, including in the same-binary control.
This does not establish consistent superiority over Portico or across sessions.

This increment follows the [performance plan](../design/consistent-performance-plan-20261007.md)
and uses the [same-seed crossover protocol](callback-crossover-20261007.md).

## Plan

`ObjectClassAncestors` and `InteractionClassAncestors` repeatedly walked parent
names, searched the complete class table and allocated a visited map and growing
result. `AdvisorySwitchDefault` obtained a defensive copy of the FOM's switch
list for each query. Public class-name lookup also scanned all classes.

Move those computations to construction of each immutable FOM binding view.
Preserve root-qualified and root-omitted names, unambiguous leaf lookup, ambiguous
name rejection, class-handle assignment, advisory presence and caller-owned
return slices. Rebuild indexes after live extension and snapshot restore have
established their history-dependent handle ordering.

Limit this increment to static metadata. Do not cache dynamic subscriptions,
DDM overlap, object visibility, ownership or routing decisions. Do not change
wire messages, SDK behavior, callback receipts, journal semantics or ACK/drain
boundaries.

## Do

| File in `rti/internal/application/composition` | Change |
| --- | --- |
| `fom_metadata.go` | Private name indexes with explicit ambiguity, precomputed class ancestry and advisory defaults |
| `fom_bindings.go` | Immutable metadata belongs to each assigned-handle binding table; initial member inheritance reuses the same ancestry |
| `fom_state.go` | Rebuild class indexes after restoring saved handle ordering; retain the restored FOM's advisory values |
| `fom_repository.go` | Public class-name lookup uses the indexes without changing name/handle results |
| `fom_advisories.go` | Constant-size advisory lookup; ancestry returns a defensive copy of the cached sequence |
| `fom_metadata_test.go` | Five new regression tests and focused metadata microbenchmarks |

Indexes are built on unpublished tables and become visible through the existing
repository/atomic-reference publication boundary. Old views remain unchanged.
No lazy cache writes, extra locks or worker goroutines were added to reads.
Construction and retained memory increase: ancestry storage is proportional to
the sum of class depths, rather than being rebuilt on each query. This trades
per-view storage for repeated allocation and traversal; it is not a measurement
of lower total server memory.

## Review

### Test-first evidence

Before implementation, the first three new behavioral tests passed on the
original code. The allocation-budget test failed as intended: advisory lookup
allocated once, and each deep object/interaction ancestry query allocated eight
times. Review added the fifth test for all advisory kinds and duplicate precedence.
All five passed in both complete regression profiles after implementation.

The five tests cover:

- Differential comparison with an independently written linear lookup/ancestry
  oracle, including qualified aliases, incorrect paths, duplicate leaf names,
  normalized-key collisions, missing/ambiguous parents and programmatic cycles.
- All 256 advisory-kind values, presence versus false, and the historical first
  declaration rule for programmatic duplicate metadata. Parser validation remains
  unchanged and still rejects conflicting XML declarations.
- Prepare/release/commit publication and repeated restoration between old and
  extended snapshots, including changed model sort order and advisory defaults.
- Concurrent readers mutating their returned ancestry slices, invalid handles
  and invalid FOMs. Returned slices cannot modify another reader's view.
- Zero advisory-query allocations and at most one allocation for each caller-owned
  ancestry result.

Existing dynamic-FOM, inheritance, MIM, rollback, service and persistence tests
were also included in the full prepared regression.

### Full prepared regression

| Profile | Required inventory | Top-level passed | Failed | Skipped |
| --- | ---: | ---: | ---: | ---: |
| `callback_sync` | 978/978 | 2,952 | 0 | 16 |
| `callback_async` | 978/978 | 2,968 | 0 | 0 |

The 16 sync skips are async-only tests. Both profiles include the Go SDK/verifier
and live Float64/Integer64 checkpoint and control-outcome recovery cases. This
is a fresh run against the new server binaries, not reused earlier evidence.
The independent standalone Python/Java SDK campaigns were not repeated.

A fresh native race attempt with `CGO_ENABLED=1` could not build because `gcc`
is absent from PATH. Concurrent-reader tests are not a substitute for a race
detector pass. Internal prepared regression is not independent whole-standard
certification.

### Query allocation

The microbenchmark uses a chain of 16 classes below the root, returning 17
ancestry handles. Three repetitions used the same Go 1.26.5 toolchain and host,
with `-benchtime=200ms`; setup is outside query timing.

| Query | Before allocations | After allocations | Before bytes/query | After bytes/query |
| --- | ---: | ---: | ---: | ---: |
| Advisory default, one declared switch | 1 | 0 | 24 | 0 |
| Object ancestry, 17 returned handles | 8 | 1 | 1,384 | 144 |
| Interaction ancestry, 17 returned handles | 8 | 1 | 1,384 | 144 |
| Object/interaction name | 0 | 0 | 0 | 0 |

The last ancestry allocation is intentional: callers own their result. Local
query timings also fell substantially, but the artificial deep hierarchy is not
the end-to-end workload and those ratios are not RTI throughput multipliers.

### End-to-end experiment

The original production async server was rebuilt before editing and its hash
matched the final M2 binary. The newly built Go federate executables are byte-
identical to the previous versions, so only the server implementation changed.

Before either campaign, the saved study plan fixed A/A first (five warm-up plus
ten measured seed blocks), then A/B (five warm-up plus twenty measured blocks).
Each seed runs both orders, in a balanced randomized ABBA/BAAB sequence with
order seed 20261007. Every trial starts a fresh server and two independent Go
federate processes. No builds, tests or profiling overlap timed trials.

Conditions: Windows 11 build 26300, Core i7-12700, 20 logical CPUs, runtime-
default `GOMAXPROCS`, `callback_async`, LocalLRC batch size 32, identical FOM bytes,
DEVStone-HLA HI width/depth 10 and 100 external events, with compact summaries.
Each trial includes 128 warm-up operations per channel and 81,200 measured
callbacks. The receiver timer runs from arming before VERIFY_START to dequeue of
the last subsequently validated callback, before DispatchEvent. Report/ACK drain
remains required outside that interval. This is not a settled-outcome latency.

| Campaign | A mean s | B mean s | A callbacks/s | B callbacks/s | Throughput change | Speedup 95% CI | B faster blocks |
| --- | ---: | ---: | ---: | ---: | ---: | --- | ---: |
| A/A, same new binary | 1.252555 | 1.266209 | 64,828 | 64,128 | -1.08% | [0.967622, 1.009667] | 3/10 |
| A/B, M2 versus static indexes | 1.341970 | 1.291300 | 60,508 | 62,882 | **+3.92%** | **[1.027088, 1.051105]** | **15/20** |

A/B mean completion fell **3.78%** and median completion changed from 1.380004
to 1.314588 seconds. There are forty measured trials per build but only twenty
four-trial seed-block resampling units. The 95% percentile bootstrap uses 10,000
replicates, seed 1516, resampling whole blocks within ABBA/BAAB strata. Its
interval describes this session and is not a between-session uncertainty estimate.
The A/A interval contains one; its point estimate is not a correction to subtract
from the A/B gain or proof of statistical equivalence.

All 60 A/A and 100 A/B trials were accepted, with all forty four-trial blocks
matching normalized data/order hashes and counts. Each trial delivered exactly
40,600 attribute and 40,600 interaction callbacks, recorded 81,456 local entries
and 81,456 outcomes including warm-up, and sent 162,934 subscriber reports.
Each publisher had 81,456 ACKed operations and zero LocalLRC failures. There
were no excluded, retried or replaced observations.

### Execution-position effect

| Campaign | AB throughput change | BA throughput change | Second/first duration ratio | Ratio 95% CI |
| --- | ---: | ---: | ---: | --- |
| A/A | -4.47% | +2.44% | 1.035503 | [1.021466, 1.048743] |
| A/B | -0.37% | +8.41% | 1.043136 | [1.033178, 1.052022] |

The second/first effect is present even with identical binaries. In A/B, mean
times at block positions 1-4 were 1.230536, 1.334295, 1.347138 and 1.354571 seconds.
Therefore the result does **not** show improvement in every order stratum.
Balancing both labels across positions and using the same seeds avoids the old
seed/order confounding, but does not remove host variation or identify its cause.
No claim about CPU temperature, cache, GC or scheduling is established here.
The two order strata share seeds and are not independent replications.

## Reflect

Retain the static indexes: they remove repeated work, preserve prepared service
behavior, and show a modest positive mean effect under the declared crossover.
Do not add this 3.92% to previous milestone percentages or compare these absolute
times to an older Portico campaign as though measured together.

The broader M3 routing cache, M4 reporting pipeline and M0 control/delivery/
settlement timing plus long-lived-participant work remain open. The observed
position effect warrants the remaining M0 investigation and independent-session
confirmation before claiming consistent superiority. Reprofile the new server
before choosing a dynamic cache or changing ownership of routing state. The
0.60-second engineering target is not reached; Portico was not rerun here.

## Local artifacts

All execution data and binaries are outside Git under
`D:/workspace/gorti/benchmark-results/`:

- `fom-static-metadata-20261007/`: test-first failure, focused passing results,
  baseline/candidate query benchmarks, rebuilt baseline and race build failure.
- `fom-static-metadata-builds-20261007/`: sync/async servers, unchanged federates
  and a six-file source increment archive (not a complete repository snapshot).
- `fom-static-metadata-ieee-qc-20261007/`: both complete JSONL regression runs,
  live-server evidence and `summary.json`.
- `fom-static-metadata-crossover-20261007/`: predeclared `study-plan.json`,
  A/A and A/B schedules, manifests, plans, participant summaries, all observations
  and `comparison.json` in each campaign directory.

The [benchmark README](../../benchmark/callback-reporting/README.md#seed-block-crossover)
documents reproduction. Both campaigns used `--design crossover --order-seed
20261007 --warmup 5 --external-events 100 --batch-size 32 --callback-profile async
--baseline-mode local-lrc --candidate-mode local-lrc`, with `--runs 10` for A/A
and `--runs 20` for A/B. Manifest files record all executable paths and hashes.

| Artifact | SHA-256 |
| --- | --- |
| Baseline async server, final M2 | `153c2664d3c3e88e640c51b4a3619c13c42dfdd917a173535af2e5127640031e` |
| Static-index async server | `25fa04118532a7638d64df9e9228879c4aa8777e052863e4a62f9b2b37cd3955` |
| Static-index sync server | `0987b6a5732c46202d3ab353534971a7c6f12a2c459c147e36170656c35c0c7c` |
| Identical async SDK/verifier | `a13ece3f3e6b987a99efa7aa40b90d6006171b1ef63fb7c2fe8d91199da876cd` |
| Source increment archive | `84ae627a71127d930b537246f77c1454e1b62b0237cd96de04da6d737d7c8988` |
