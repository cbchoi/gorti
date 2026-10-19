# Completed callback receipt retention

This change reduces completed-receipt bookkeeping while retaining the existing
callback evidence, recovery policy and capacity limits. It follows the
[bounded LocalLRC backpressure change](local-lrc-backpressure-20261006.md).
Both the candidate and its preceding baseline retain callback entry/outcome
reporting and can complete the 81,200-callback workload.

## Plan

The completed receipt ledger keeps an ordered slice of keys. When its default
8,192-entry limit is reached, the old implementation moves almost the whole
slice for each oldest-entry eviction. Keep the current oldest-eligible policy,
including retention of unsent or retry-pinned recovery markers, while removing
that repeated full-tail copy.

Compare the isolated bookkeeping cost first, then measure the whole two-process
federation workload. Use the prepared IEEE service test inventory for both the
synchronous and asynchronous callback-reporting builds.

## Implementation

`trimCompletedLocked` still finds the first eligible key and removes the same
entry. It now copies the shorter side of the slice. For ordinary FIFO eviction,
it clears the head and advances the slice without copying retained keys. With a
short pinned prefix, it copies that prefix rather than the large following tail.
Removing near the end retains the short-tail copy. Both paths preserve order
and clear the vacated reference. The existing mutex still protects all mutations.

`pruneCompletedLocked` also clears discarded suffix slots after compacting the
live keys. Clearing references is not a claim of secure erasure of ticket bytes.
The change adds no index, linked-list nodes, wire fields, public API or persisted
format. Callback validation, pending/completed limits and ACK behavior are unchanged.

FIFO eviction is constant-time, but appending to the advancing slice occasionally
grows its backing array. These copies are amortized. Pinned-marker selection is
still a linear scan, and eviction can still copy a long prefix or suffix when
many pinned entries surround the victim. The change is not a general constant-time
eviction algorithm for every recovery state.

## Review

The new reference-release tests failed on the baseline because removed suffix
slots retained receipt keys. They passed after the change. The added tests check
head, middle and tail eviction, pruning, duplicate/conflicting outcome reports,
and a deterministic 20,000-step comparison against the original retention policy
while pinning, unpinning and removing entries. Existing recovery-marker capacity,
rollback, disconnect and retry tests were included in repeated targeted regression.

Fresh Go-based service regression passed on the final binaries with `-count=1`,
the live runtime-checkpoint build tag and the official retained-FDD XSD:

| Profile | Required inventory | Top-level passes | Failures | Skips |
| --- | ---: | ---: | ---: | ---: |
| `callback_sync` | 978/978 | 2,910 | 0 | 10 async-only cases |
| `callback_async` | 978/978 | 2,920 | 0 | 0 |

There are no missing or skipped required inventory cases. The campaign includes
wire goldens, Float64/Integer64 services, live checkpoint restart and callback
outcome recovery, plus the four new top-level retention tests. This is evidence
that the prepared internal tests pass, not independent certification or a new
full-IEEE-conformance claim. The complete Python and Java SDK suites were not
rerun in this increment.

Race testing was attempted but could not start: the native default disables cgo,
and enabling it fails because `gcc` is absent from PATH. Docker's Linux-engine
pipe is also unavailable. These attempts are recorded separately from test
passes. Repeated contract tests do not substitute for a race-detector run.

## Isolated measurement

`BenchmarkCallbackCompletedRetention` measures ordered ledger append, map insert,
selection and eviction. It excludes transport, authentication, event encoding and
callback execution. Keys and entries are preallocated to expose bookkeeping.
Each case ran six times for 200 ms, with `GOMAXPROCS=2`, before and after the change.
These sequential microbenchmarks are diagnostic measurements, not the primary
paired federation experiment.

| Completed limit | Pinned prefix | Baseline median, ns/op | Candidate median, ns/op |
| --- | ---: | ---: | ---: |
| 256 | 0 | 73.100 | 57.505 |
| 256 | 1 | 82.025 | 65.785 |
| 256 | 8 | 122.550 | 112.400 |
| 8,192 | 0 | 3,652.500 | 79.620 |
| 8,192 | 1 | 3,823.500 | 85.525 |
| 8,192 | 8 | 3,741.000 | 132.900 |

The default-limit unpinned case is about 45.9 times faster in this isolated loop.
Candidate slice growth adds approximately 67 amortized bytes per operation at
the 8,192-entry limit, versus zero in the baseline; at 256 entries it adds about
28 bytes per operation. These figures do not imply a 45.9-fold RTI throughput gain.

## Federation experiment

The paired campaign uses DEVStone-HLA HI 10 by 10 with 100 external events and
81,200 measured data callbacks. Conditions are identical: FOM bytes, per-pair
seeds, two independent federate processes, LocalLRC batch size 32, numeric-handle
callbacks, compact summaries, fresh processes and `GOMAXPROCS=2`. Each condition
has five warm-up runs and twenty measured runs in alternating AB/BA order.
The host runs Windows 11 on 20 logical CPUs; builds use Go 1.26.5 windows/amd64.

Only the server binary changes. Both Go participant executables have SHA256
`1440db3ded793d0555d3b0cc19a3a59008f0329e63df7068adc480519b1d43d7`.
Both sides use the current `callback_async` profile, including receipt reporting.
Each pair must match callback counts and ordered payload hashes and complete
the final report drain. Timing ends at the last subsequently validated callback's
dequeue before dispatch; final report settlement is required outside that timer.

All 50 trials completed, including warm-up, and all 25 semantic pairs matched.

| Metric | Previous backpressure build | Retention improvement |
| --- | ---: | ---: |
| Measured trials | 20 | 20 |
| Mean completion, seconds | 4.238476 | 3.567564 |
| Median completion, seconds | 4.186337 | 3.379742 |
| Standard deviation, seconds | 0.216381 | 0.720120 |
| Mean per-trial callback throughput, callbacks/s | 19,202.6 | 23,346.7 |

Mean completion time decreased by 15.829%. The baseline/candidate mean-time
speedup is 1.188059, with paired order-stratified bootstrap 95% interval
[1.084554, 1.271788]. Nineteen of twenty measured pairs favored the candidate.
The 6.255-second candidate run remains in the mean and confidence interval;
it was not excluded or replaced. The interval excludes one, supporting an
improvement for this workload and environment.

Mean per-trial throughput increased by about 21.6%. This is the mean of each
trial's callbacks/second, so its ratio is not the reciprocal of the mean
completion-time ratio. Report the two statistics with their respective definitions.

No new Portico or preserved-release comparison is part of this increment,
and their older timings are not this campaign's baseline.

## Reflect

Both isolated and paired federation measurements support this narrowly scoped
improvement. The observed throughput gain is much smaller than the bookkeeping
microbenchmark gain, as expected when the complete path still includes callback
delivery, encoding, validation, report handling and SDK recovery bookkeeping.
This result does not establish parity with Portico or the old release.

Further work should profile the remaining callback-report and recovery path
with the new binary before choosing another optimization. The remaining gap
cannot be attributed to this now-reduced list operation alone.

## Evidence

Raw artifacts remain outside Git under `../benchmark-results/`:

| Directory | Evidence |
| --- | --- |
| `completed-retention-20261006` | Before/after microbenchmarks and native race-attempt diagnostics |
| `completed-retention-long-paired-20261006` | Paired current-baseline/candidate experiment |
| `completed-retention-builds-20261006` | Candidate source and both callback-profile binaries |
| `completed-retention-ieee-qc-20261006` | Final two-profile internal IEEE service regression |

The candidate source archive SHA256 is
`120bb311697e0ebece6f6e3c3c12124f6342189dbf4e043a2d18abf2894332e9`.
Candidate server hashes:

- Async: `a48aea2cd6896e5f82d8c1450ceea8b70041ded6c158883e3afb9d8f3f704d77`.
- Sync: `2a5e7b26dc283e07c754e5c11c29cb3993912d628030caef7bc5a8d098e5c4cf`.

The baseline server hash is
`ba58c1b416e507274fa53d7858413970976f3e86e90fe43b16ebfd8ea24a4b73`.
The campaign manifest records all executable, workload and FOM hashes.
