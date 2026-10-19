# Preserved performance reference and current IEEE service regression

The [subsequent bounded-backpressure change](local-lrc-backpressure-20261006.md)
completed the 81,200-callback workload and reran the internal IEEE service tests.
The failed trials below remain evidence of this earlier implementation.

## Reference

The July 27 two-federate result used 81,200 measured callbacks, five warm-up
pairs and twenty measured AB/BA pairs. Its saved median completion times were
634.410 ms for Portico and 223.845 ms for gorti. These are historical results,
not measurements of today's source or native Windows configuration.

The earliest preserved release is commit
`5cf718a8f282d6be36cd1eed8d0efb06d388a80c`, tree
`5017a49882d048990507d100e4250597b888f638` (August 1). The July 27 source
revision is not attested by that release. `restore_reference.py` extracts this
release outside the active checkout, builds its server and Go participant,
and records the commit, tree, archive hash, toolchain and binary hashes.
No current source, branch or uncommitted work is reset.

The preserved SDK already uses LocalLRC queues, streamed batches and cumulative
ACKs. Its receiving participant reads events directly and does not implement
the current invocation-entry and outcome-recovery protocol. It is a performance
reference, not a candidate replacement for the current IEEE-tested SDK.

## Changes

The current async SDK keeps negotiated local admission for eligible RO data,
bounded ENTRY/OUTCOME reporting, cumulative ACKs and ordered service barriers.
TSO, DDM-region, recovery and control callbacks retain confirmed admission.
The current receipt, recovery and server service checks remain enabled.

Pending callback bookkeeping retains the arrival-ordered slice and cached
recovery identities. Normal FIFO retirement no longer shifts all remaining
payloads. Arbitrary recovery retirement preserves order, and vacated slots are
cleared so they do not retain detached payloads. An emptied one-element queue
keeps its capacity for reuse. The checkpoint and wire formats are unchanged.

An ID/identity-indexed linked queue was tried first, then rejected after the
paired experiment did not support a throughput gain. In twenty measured pairs,
the baseline median was 169.414 ms and the indexed candidate median 183.474 ms.
One candidate trial took 4.496 seconds; it remains in the results. Mean speedup
was 0.4521 with 95% interval [0.2103, 1.0449]. The source archive and binary
hashes for this rejected candidate are retained outside the repository.

Explicit report drain now includes tasks whose wire ACK has arrived but whose
local bookkeeping is still running. Previously, removing a task from the wire
queue could hide that unfinished work from drain and cause unnecessary retries.
Ordinary service barriers still wait for the wire ACK, so a checkpoint holding
its lock cannot deadlock the report transport.

A deterministic regression test failed before the drain correction and passed
afterwards. Another test retires the head, middle and tail, checks saved order,
reconciles completed identities and appends after the queue empties. Existing
tests retain receipt aliasing, stale-epoch, recovery, backpressure, fallback,
failure and at-most-once coverage.

## Experiment protocol

The attempted historical-size comparison used `--external-events 100`. A
separate comparison within the currently admissible workload uses five external
events (4,060 measured callbacks). Both use identical FOM bytes and
per-pair seeds, two independent federate processes, LocalLRC on both sides,
numeric-handle callbacks and compact summaries. Use `GOMAXPROCS=2`, fresh
server/client processes, 128 warm-up operations per channel, five warm-up
pairs and twenty measured AB/BA pairs. Compare callback counts and payload/order
hashes for every pair. Retain failures; do not discard slow or failed trials.

The endpoint is the final subsequently validated callback's dequeue timestamp,
before dispatch. Successful final report drain and teardown remain required
outside the timer. Legacy transport counters are explicitly unavailable;
candidate transport and service tests are not relaxed. This does not measure
remote-outcome-settled latency or imply that the old SDK satisfies today's suite.

Raw results, extracted source, logs and executables stay in the external
`../benchmark-results/` directory. Only experiment implementation and findings
belong in the repository.

## Results

The historical-size attempt exposed a capacity limit, not a completed timing
comparison. Both M4 and the indexed candidate failed with `MOM: callback receipt
capacity exceeded`. The restored release completed its one screening trial in
325.827 ms, but this single run is not a performance estimate or a speedup.
The current mandatory receipt ledger defaults to 8,192 pending callbacks and
rejects admission when full. Its capacity includes callbacks already removed
from the outbox but not yet outcome-reported. The configured 8,192-event outbox
therefore does not prevent this limit from being reached.

Increasing that limit or weakening receipt requirements was not used to hide
the failure. A lossless, bounded producer/receiver backpressure design remains
necessary before claiming recovery of the full 81,200-callback historical run.
The failed attempts remain in `indexed-callback-paired-20261006` and
`legacy-long-screen-20261006`. The rejected indexed-queue comparison is in
`indexed-callback-short-paired-20261006`.

After final regression, the FIFO binary reproduced the same capacity failure
in `legacy-fifo-long-screen-20261006`. Its restored-release counterpart finished
in 242.395 ms. Again, this screening run is retained as a failure investigation,
not used to claim a long-workload speedup.

### Accepted FIFO change versus M4

The same server binary is used in both conditions. Only the SDK changes. Both
use LocalLRC and `callback_async`; 50 trials completed, including warm-up, and
all 25 pairs matched callback counts, order/payload hashes and final evidence.

| Metric | M4 SDK | FIFO and complete-drain SDK |
| --- | ---: | ---: |
| Measured trials | 20 | 20 |
| Mean completion, ms | 175.889 | 176.744 |
| Median completion, ms | 167.183 | 165.249 |
| Standard deviation, ms | 23.853 | 29.089 |
| Mean subscriber reports, including warm-up/control | 8,680.30 | 8,654.00 |
| Mean subscriber report batches | 184.50 | 158.05 |
| Local data entries, including warm-up | 4,316 | 4,316 |

Mean speedup is 0.99516 with paired order-stratified bootstrap 95% interval
[0.92669, 1.06751]. This does not demonstrate an improvement or a significant
regression. The change is retained for correct drain completion and avoiding
FIFO payload shifts, not as an established end-to-end speedup.
The lower report count eliminates redundant idempotent reports, not application
callbacks: payloads, callback counts and local-entry counts are unchanged.

Evidence: `fifo-callback-short-paired-20261006/comparison.json` and its
`manifest.json` and `results.json`, outside the repository.

### Restored release versus current SDK

This separate, same-host campaign also completed 50 trials and matched all 25
pairs. The shared workload has 4,060 measured callbacks. This comparison does
not use the historical July timings as its denominator.

| Metric | Restored release | Current FIFO SDK/server |
| --- | ---: | ---: |
| Measured trials | 20 | 20 |
| Mean completion, ms | 22.735 | 604.324 |
| Median completion, ms | 21.839 | 156.364 |
| Standard deviation, ms | 3.516 | 1,398.182 |

The current median is about 7.16 times the restored release's median. Two current
trials took 3.671 and 5.523 seconds; both remain in the mean. Their cause has
not been established, so they must not be silently attributed to host noise or
removed. The current build was slower in every measured pair. Full recovery of
the old throughput is not established, and no fresh Portico comparison was run.

Evidence: `legacy-fifo-short-paired-20261006/`. The original source reference is
in `legacy-reference-20261006/reference.json`.

### IEEE regression

The final FIFO candidate passes the current prepared IEEE service manifest in
both profiles. No required case was omitted or relaxed.

| Gate | Result |
| --- | --- |
| Required IEEE service cases, sync | 978 / 978 passed |
| Required IEEE service cases, async | 978 / 978 passed |
| Wider sync package run | 2,897 passed; 10 async-only skips; 0 failed |
| Wider async package run | 2,907 passed; 0 skipped; 0 failed |

Both runs include the live restart/control-outcome checkpoint tests, Float64
and Integer64 coverage, official FDD schema checks and wire descriptor goldens.
Evidence: `fifo-reference-ieee-qc-20261006/summary.json`, with the exact commands
and server hashes. These are prepared supported-profile test results, not
independent certification of the entire IEEE standard family.

Linux race could not be rerun:
the Docker Desktop Linux engine was unavailable, and no native GCC/Clang was
found. Earlier race results do not certify this new change. Whole Python and
Java SDK campaigns were not rerun; their implementation was not changed.

The final source archive is `callback-fifo-builds-20261006/source.tgz`, SHA-256
`71bc4a0a682978f8568108888cbbed900b5d92bc85225451062f4c0fcd1a8692`.
It contains production, tests, protocol sources and experiment code; execution
logs and the working tree's unrelated documentation/assets are not included.

## Next performance boundary

The restored release provides a useful target but cannot replace the current
callback path without addressing invocation, recovery and MOM semantics. The
next implementation step is bounded, lossless flow control across outbox,
pending invocation receipts and SDK reporting, with atomic fanout and no
service-lock waits that prevent the receiver from releasing capacity. Capacity
exhaustion must not be disguised as a completed benchmark or solved by dropping
required receipts. After that, repeat the full historical-size workload and
profile entry/outcome processing before choosing the next optimization.
