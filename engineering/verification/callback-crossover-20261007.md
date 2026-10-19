# Same-seed crossover for callback delivery batching

Date: 2026-10-07. M0a, the execution-order/seed investigation, is complete.
The final M2 server has **4.12% higher aggregate received throughput** than the
immediately preceding M1 server in this campaign, with a seed-block bootstrap
speedup interval of **[1.022449, 1.059576]**. This is evidence of a modest gain
on this workload, not completion of the consistent-performance target.

No RTI or SDK implementation changed in this increment. Portico was not rerun.
The earlier [M2 campaigns](callback-delivery-batch-20261007.md), including their
inconclusive final result, remain part of the evidence.

## Plan

The previous alternating AB/BA experiment assigned each seed to only one order.
The final campaign showed -6.70% throughput change in AB but +11.83% in BA.
Seed, execution order and chronological variation could not be separated from
those results alone.

Before running new trials, save a fixed study plan outside the repository:

1. A/A control: the final M2 server and client for both labels; five warm-up seed
   blocks and ten measured seed blocks, 60 trials total.
2. A/B comparison: M1 baseline versus final M2; five warm-up seed blocks and
   twenty measured seed blocks, 100 trials total.

Every block uses one workload seed for four trials, either ABBA or BAAB.
Both AB and BA are therefore observed for every seed. Balance the two sequences
within each phase and randomize their allocation with order seed 20261007,
independently of workload seeds `1516 + block index`. Retain all observations;
stop on failure, with no replacements or outcome-dependent extra campaigns.

## Do

`benchmark/callback-reporting/compare_transports.py` now accepts
`--design crossover` and `--order-seed`. The existing paired default is unchanged.
`crossover.py` implements scheduling, four-trial execution, semantic matching,
failure retention and clustered analysis. The runner saves the schedule before
execution and records its hash in the manifest. Separate block/order directories
preserve both executions of each condition without changing federation names
within a seed block.

The primary estimator is mean baseline duration / mean candidate duration.
Since every trial delivers the same callback count, it is also the ratio of
aggregate received throughputs. A 95% percentile interval uses 10,000 bootstrap
replicates with seed 1516, resampling whole four-trial seed blocks within ABBA
and BAAB strata. Forty measured trials per build in A/B are **twenty seed-block
resampling units**, not forty independent pairs. Neighboring blocks
can still share host-state effects; this within-session interval is not a
between-session uncertainty estimate.

The runner also records execution positions, UTC start, total trial wall time,
AB/BA means and the second/first-in-pair duration ratio. Trial wall time includes
startup and teardown and does not replace the existing receive-completion metric.

### Fixed conditions

- Windows 11 build 26300, 20 logical CPUs, Go 1.26.5 binaries.
- `GOMAXPROCS` unset; both conditions use the runtime default.
- `callback_async`, LocalLRC producer mode and batch size 32 in both conditions.
- Byte-identical Go SDK/verifier binaries, FOM bytes and compact-summary logging.
- Two independent Go federate processes and a fresh server for every trial.
- DEVStone-HLA HI width/depth 10, 100 external events: 40,600 attribute plus
  40,600 interaction callbacks; 128 warm-up operations per channel per trial.
- Primary timer from subscriber arming before VERIFY_START to dequeue of the
  last subsequently validated callback, before DispatchEvent. Final report/ACK
  drain is required outside that interval.
- No builds, regression tests or profiling during timed execution.

This is a fresh-process, two-federate RO experiment. It does not measure TM,
large-federation scaling or long-lived-JVM performance.

## Review

### Experimental code and execution

All five focused tests in `test_crossover.py` passed. They check reproducible
balanced scheduling, a same-binary order-effect control, a known gain with seed
variation and linear drift, rejection of incomplete/mislabelled/invalid evidence,
and separate output/failure retention without retries. The tests use synthetic
data and mocked execution; the following counts come from the actual campaigns.

| Campaign | Warm-up blocks | Measured blocks | Measured trials per label | Accepted / all trials |
| --- | ---: | ---: | ---: | ---: |
| A/A, identical M2 binaries | 5 | 10 | 20 | 60/60 |
| A/B, M1 versus M2 | 5 | 20 | 40 | 100/100 |

All 40 four-trial blocks matched normalized semantic evidence, including exact
callback counts and payload/order hashes. Every trial delivered 81,200 measured
callbacks, retained 81,456 local entries and 81,456 outcomes including warm-up,
and sent 162,934 subscriber reports. Every publisher had 81,456 acknowledged
operations and zero LocalLRC failures. Nothing was retried, excluded or replaced.

### Primary results

| Campaign | A mean s | B mean s | A callbacks/s | B callbacks/s | Apparent throughput gain | Speedup 95% CI | B faster blocks |
| --- | ---: | ---: | ---: | ---: | ---: | --- | ---: |
| A/A control | 1.385642 | 1.360240 | 58,601 | 59,695 | +1.87% | [0.990205, 1.051241] | 4/10 |
| A/B comparison | 1.389323 | 1.334342 | 58,446 | 60,854 | **+4.12%** | **[1.022449, 1.059576]** | **17/20** |

A/A has no implementation difference. Its nonzero point estimate illustrates
finite-sample variation; its interval contains one. It is neither an equivalence
proof nor an estimated correction to subtract from the A/B gain.

In A/B, mean completion fell **3.96%**. Median completion changed from 1.389002
to 1.323224 seconds. Both the mean and interval favor the final M2 build in this
campaign. This does not retroactively make the earlier inconclusive experiments
positive or establish a repeatable four-percent gain across sessions.

### Order diagnostics

| Campaign | AB throughput gain | BA throughput gain | Second/first duration | Second/first 95% CI |
| --- | ---: | ---: | ---: | --- |
| A/A | +1.15% | +2.61% | 1.006936 | [0.984485, 1.031014] |
| A/B | +3.05% | +5.22% | 1.010098 | [0.988963, 1.033640] |

The large sign reversal in the preceding final M2 campaign did not recur here.
Both AB and BA means favor M2, while neither second/first interval excludes one.
The two order strata share seeds and must not be treated as independent replications.
These diagnostics do not prove that seed caused the earlier difference, that
order never matters, or that cache, CPU temperature, GC or scheduling is the cause.

### IEEE regression boundary

No server, SDK, protocol or service behavior was edited. The executables match
the hashes in the prior final M2 evidence, which passed 978/978 required prepared
IEEE-service cases in both sync and async profiles. The full suite was **not
rerun** for this benchmark-only increment. Fresh evidence here is the 160 accepted
workload trials and the five experimental-code tests, not another conformance run.
The prior native-race limitation remains unchanged.

## Reflect

The batching change now has positive end-to-end evidence under a stronger
same-seed crossover design, in addition to its previously measured reduction in
allocation/admission cost. Retain the implementation. Keep broad consistency,
tail-latency and cross-session acceptance open; do not pool this positive campaign
with selectively chosen earlier observations.

M0a is complete, but M0's separate control/delivery/settlement timestamps and
long-lived participant experiment remain open. Those are still needed to examine
Portico's between-campaign variation. The next isolated production candidate is
M3's static FOM metadata indexing, with its own service regression and crossover
comparison. Dynamic routing caching and M4's reporting worker/window change
remain separate, higher-risk work. No new Portico superiority claim follows.

## Reproduction and artifacts

See the [crossover instructions](../../benchmark/callback-reporting/README.md#seed-block-crossover).
This campaign used `--design crossover --order-seed 20261007 --warmup 5`,
`--runs 10` for A/A and `--runs 20` for A/B, plus
`--baseline-mode local-lrc --candidate-mode local-lrc --callback-profile async
--external-events 100 --batch-size 32`.

All plans, manifests, schedules, summaries and observations are outside Git in
`D:/workspace/gorti/benchmark-results/callback-crossover-study-20261007/`:

- `study-plan.json`: the fixed plan saved before either campaign.
- `aa-control/`: identical-binary control, including all warm-up observations.
- `ab-comparison/`: the complete M1/final-M2 comparison.
- Each campaign has `manifest.json`, `schedule.json`, `results.json` and
  `comparison.json`; participant summaries remain under block/order directories.

| Artifact | SHA-256 |
| --- | --- |
| A/B baseline server, M1 | `463158ece286801f205cba935ae80b2fcb5e264e5dc04cb13c9b5bb4a0560bb6` |
| Final M2 server, both A/A labels and A/B candidate | `153c2664d3c3e88e640c51b4a3619c13c42dfdd917a173535af2e5127640031e` |
| Identical SDK/verifier in all conditions | `a13ece3f3e6b987a99efa7aa40b90d6006171b1ef63fb7c2fe8d91199da876cd` |
| FOM | `633aaa61984966237f3675bcf005a5fb3722392f2c113ef41ca8b255e1a15040` |
| A/B prewritten schedule | `801fbd44e12861b09c9bb973fcc97d4cd5443a1f1a090842b1db882232e46d38` |
