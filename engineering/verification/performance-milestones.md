# Performance milestones

This record tracks performance work that preserves gorti's public behavior,
error boundaries, callback order, federation-generation fencing, and atomic
fanout semantics. A change is retained only when focused tests pass and a
paired two-federate DEVStone-HLA screen improves the subscriber's final
validated-callback boundary.

The short milestone screen uses one warm-up pair and four measured pairs with
alternating AB/BA order. Both implementations use the same FOM bytes, workload
plan, seed, payloads, two independent federate processes, sequential
receive-order update-then-interaction choreography, callback policy, and timer
boundary. Each accepted run must deliver 81,200 validated callbacks with no
drops, duplicates, invalid callbacks, unexpected callbacks, or rejections.
These screens guide engineering decisions; release or publication claims still
require the full preregistered campaign.

## Baseline

**Plan.** Establish a repeatable semantic and performance gate before changing
the hot path.

**Do.** Run the Go regression suite and the paired LocalLRC DEVStone-HLA
comparison. The initial short-screen medians were 1.142 s for gorti and 0.729 s
for Portico.

**Review.** Every accepted run reported the expected callback count and zero
semantic defects. The measured boundary excluded setup and teardown for both
implementations.

**Reflect.** Use the gorti median as the milestone baseline. Do not infer that
Go should be faster than Java from language choice alone; architecture,
serialization, synchronization, and process boundaries dominate this workload.

## Milestone 1: callback batch sizing

**Plan.** Remove repeated serialization-size scans while retaining the gRPC
message-size limit and event order.

**Do.** Replace repeated `proto.Size` calls over the growing callback batch
with exact incremental Protobuf wire-size accounting. Add boundary tests for
varint sizes, exact-limit batches, limit-plus-one splitting, oversized events,
and send failure.

**Review.** The 32-event microbenchmark improved from about 156 us to 11 us,
and the 256-event case improved from about 9.74 ms to 91 us. The short-screen
gorti median improved from 1.142 s to 1.034 s, a 9.47% reduction. Callback
evidence remained exact.

**Reflect.** Retain the change. Exact incremental sizing removes quadratic
work without changing the protocol or callback boundary.

## Milestone 2: persistent stream reuse

**Plan.** Determine whether per-operation stream setup remains in the
receive-order path.

**Do.** Trace the LocalLRC and callback paths and benchmark direct, unary TCP,
and persistent-stream calls.

**Review.** LocalLRC already multiplexes updates, interactions, flushes, and
cumulative acknowledgements over one bidirectional stream. Persistent stream
calls measured about 83 us versus 233 us for unary TCP and used about 67% fewer
allocations. Combining callback and confirmed-operation streams would merge
different failure and backpressure contracts.

**Reflect.** Mark the milestone already satisfied. Keep the streams separate;
there is no semantics-preserving implementation change to promote.

## Milestone 3: callback notification

**Plan.** Test whether replacing per-notification channels in the verifier
reduces the callback-boundary cost.

**Do.** Implement and review a reusable condition-variable notification path,
including cancellation and deadline tests.

**Review.** The notification microbenchmark improved, but the paired gorti
median moved from 1.034 s to 1.032 s, only 0.12%, while host drift was larger.
The change affected the measurement harness rather than the RTI.

**Reflect.** Reject and remove the experiment. It did not produce a meaningful
end-to-end improvement and would make comparison history harder to interpret.

## Milestone 4: LocalLRC validation snapshots

**Plan.** Evaluate immutable local declaration and ownership snapshots to
remove validation locks.

**Do.** Add serial and parallel validation benchmarks and audit publication,
ownership, restore, resign, and federation-recreation invalidation paths.

**Review.** Interaction validation measured about 45 ns and attribute
validation about 59 ns, both with zero allocations. Full LocalLRC admission was
about 1.25 us with 496 B and seven allocations. A snapshot would add stale-state
risk because unpublish, ownership transfer, and same-generation restore do not
currently share a complete client-side invalidation boundary.

**Reflect.** Reject the snapshot optimization. Keep the focused benchmarks and
the existing authoritative state lock. Revisit only if a concurrent profile
shows material contention after lifecycle invalidation is centralized.

## Milestone 5: one-recipient atomic reservation

**Plan.** Avoid the generic all-recipient reservation setup for the common
two-federate case while preserving reservation-before-WAL, commit ordering,
capacity checks, timers, and teardown exclusion.

**Do.** Add a dedicated single-recipient reservation type. It copies the
delivery value and holds the exact recipient lock from successful reservation
through idempotent commit or release. The generic multi-recipient transaction
is unchanged. Tests cover order, release, overflow, grant-forced flush,
caller mutation, send overtaking, cancellation, and concurrent commit/release.

**Review.** The focused median improved from about 739 ns, 188 B, and six
allocations to 165 ns, 84 B, and one allocation, a 77.7% latency reduction.
The generic two-recipient path retained 289 B and seven allocations and stayed
within timing noise. The paired gorti median improved from 1.034 s to 0.987 s,
a 4.50% reduction; Portico changed by 0.39%. All accepted runs delivered the
expected 81,200 callbacks with zero semantic defects. One complete measured
pair was retried under the fixed same-seed replacement rule.

**Reflect.** Retain the change. Relative to the initial short baseline, the
combined retained work reduces the gorti median by 13.55%. The current gorti
median remains 27.8% above Portico in this short screen, so further work should
target server fanout and transport transactions rather than local validation.

## Milestone 6: receive-order fanout allocation

**Plan.** Reduce allocation in the server's receive-order interaction and
attribute fanout preparation without changing callback sequence assignment,
write-ahead ordering, all-recipient admission, TSO buffering, or management
delivery counters.

**Do.** Replace the interaction path's parallel recipient and event slices plus
its synthesized reservation slice with one `OutboxDelivery` slice. Store the
common one-recipient delivery inside the prepared transaction. Apply the same
single-recipient storage to attribute reflects and their management-counter
recipient list. For untimed attributes, append directly to the outbox delivery
list instead of first constructing a TSO delivery list. Add a focused
receive-order attribute benchmark alongside the existing interaction
benchmarks.

**Review.** The interaction benchmark median improved from about 907 ns, 904 B,
and 16 allocations to 750 ns, 856 B, and 13 allocations, a 17.3% latency
reduction. The attribute benchmark improved from about 1,087 ns, 1,288 B, and
19 allocations to 1,007 ns, 1,240 B, and 16 allocations, a 7.4% latency
reduction. The paired gorti median improved from 0.987 s to 0.962 s, a 2.53%
reduction; Portico moved from 0.772 s to 0.778 s, a 0.77% change. Every run
completed on its first attempt with 81,200 callbacks and zero drops,
duplicates, invalid callbacks, unexpected callbacks, or rejections.

**Reflect.** Retain the change. Relative to the initial short baseline, the
retained milestones reduce the gorti median by 15.74%. The gorti median remains
23.7% above Portico in this screen. The next experiment should reduce protocol
transactions while preserving one ordered server result per submitted
operation.

## Milestone 7: LocalLRC request batching

**Plan.** Reduce per-operation HTTP/2 and Protobuf framing while retaining one
gapless sequence, one normal server service execution, and one final result for
every locally admitted operation.

**Do.** Add an additive `LocalLRCBatch` wire message. The server advertises its
maximum batch size in the opening ACK; a zero value from an older server keeps
the client on single-operation frames. The client coalesces up to 32 operations
for at most 50 us, flushes a pending batch before a flush marker, and publishes
the batch's upper sequence before sending. The server processes list entries
sequentially through the existing membership guard and Object Management
service. It ACKs each completed cumulative interval and, on failure, ACKs only
the successful prefix before returning the service error. Tests cover batched
mixed-operation order, per-operation membership leases, prefix ACK on failure,
legacy fallback, negotiated limits, complete drain, and reduced operation-frame
count. The fair verifier rejects a run unless batching was negotiated and the
operation-frame count is below the operation count.

**Review.** Local admission remained at about 1.20 us, 496 B, and seven
allocations. The paired gorti median improved from 0.962 s to 0.898 s, a 6.67%
reduction, and its four measured runs occupied the narrow 0.890--0.905 s range,
which did not overlap the previous 0.939--0.996 s range. Every run completed on
its first attempt with exact callback evidence. Portico also improved by 10.8%
between the two screens, showing material host drift; this short screen supports
the absolute gorti improvement but not a stronger relative ranking claim.

**Reflect.** Retain the negotiated batch path and its legacy fallback. Relative
to the initial short baseline, retained milestones reduce the gorti median by
21.36%. The current screen places gorti 29.4% above Portico, but that ratio is
not suitable for a release claim because of the Portico drift. Re-run the full
paired campaign after all milestones.

## Milestone 8: generation-fenced receive-order execution plans

**Plan.** Evaluate a server-owned immutable routing plan keyed by federation
generation and routing epoch. As a lower-risk probe, remove the temporary unary
request and response objects inside the already generation-validated LocalLRC
stream while retaining the normal core registry call and per-operation member
lease.

**Do.** Audit publication, subscription, DDM, ownership, resign, restore, and
federation-recreation mutation paths. Implement the narrow direct adapter probe
and add a focused interaction-execution benchmark.

**Review.** The audit found no single routing epoch that covers all required
mutation paths, so a full cached execution plan could become stale. The narrow
probe improved its microbenchmark from about 360 ns, 400 B, and four allocations
to 328 ns, 352 B, and three allocations. Its paired gorti median moved from
0.898 s to 0.880 s, a 1.95% reduction, but the ranges overlapped and Portico
moved by 5.29% in the same direction. Exact callback and batch-use evidence
passed.

**Reflect.** Reject and remove the direct adapter probe because its end-to-end
change is smaller than observed host drift. Keep the benchmark. Defer a full
execution plan until declaration, DDM, ownership, restore, and lifecycle
invalidation share an authoritative routing epoch.

## Milestone 9: immutable declaration routing snapshots

**Plan.** Remove repeated declaration sorting and allocation while preserving
the manager's linearization point and deterministic recipient order. The
attribute profile attributed 6.67% of allocation objects to sorted declaration
handles: 2.82% in publisher validation and 3.85% in subscriber routing.

**Do.** Add exact publisher-membership lookup and manager-owned immutable sorted
subscriber snapshots. Object and interaction subscription mutations invalidate
the related snapshot under the declaration lock; resign clears all snapshots
for the federation, and federation destruction discards their owning state.
The public snapshot-copy APIs remain unchanged. DDM and ownership remain
authoritative and outside the declaration-only cache. Tests exercise expansion,
unsubscribe, resign, destroy/recreate, old-reader immutability, and concurrent
read/write access.

**Review.** Three steady-state declaration queries together measured about
63 ns with zero allocations. Interaction routing dropped from 856 B and 13
allocations to 848 B and 12 allocations. Attribute routing dropped from 1,240 B
and 16 allocations to 1,224 B and 14 allocations. The paired gorti median moved
from 0.898 s to 0.884 s, a 1.51% reduction, while Portico moved 7.9% in the
slower direction. Every run completed on its first attempt with exact callback
and negotiated-batch evidence.

**Reflect.** Retain the change. Relative to the initial receive-order short
baseline, retained milestones reduce the gorti median by 22.55%. The final
short screen places gorti 18.1% above Portico, but short-screen host variation
still precludes a release ranking claim.

## Milestone 10: Time Management evaluator allocation

**Plan.** Measure Time Management independently because the DEVStone-HLA
receive-order workload excludes TAR, NER, TSO buffering, and grants. Separate
the pure grant predicate from a two-regulator TAR round in which each federate
waits for its peer before both grants advance.

**Do.** Add focused benchmarks. Replace reflection-based candidate and
regulator sorting with typed sorting, materialize each regulating federate only
once as `(handle, lookahead)`, fill its logical-time floor in place, and use
four stack candidate slots before falling back to a growing slice. The
fixed-point restart, ascending handle order, per-mode decision, TSO scan,
write-ahead, outbox, and state transition are unchanged.

**Review.** The pure TAR grant predicate remained about 1.9 ns with zero
allocations. A two-regulator TAR round improved from a median of about 3.87 us,
1,304 B, and 35 allocations to 1.50 us, 320 B, and 10 allocations. That is a
61.1% latency, 75.5% byte, and 71.4% allocation reduction. Existing TAR, NER,
NMRA, TARA, FQR, TSO-before-grant, delivery-failure, and deterministic-order
tests passed.

**Reflect.** Retain the Time Management change. This is an internal algorithm
benchmark, not a Portico comparison; a publication claim requires a separate
paired multi-process TM workload with identical grant and TSO boundaries.

## Milestone 11: callback transport and comparison parity

**Plan.** Reduce callback-path framing and verifier overhead without changing
the public name-oriented callback API, event order, grant boundaries, bounded
backpressure, or the subscriber-completion metric. Make all transport controls
explicit in the benchmark record before evaluating a candidate.

**Do.** The callback stream now combines only event batches that are already
ready, up to 64 events, and flushes immediately at a time-advance grant. The Go
SDK adds an opt-in handle representation for reflection and interaction maps;
the existing name representation remains the default. LocalLRC opening frames
can request 32, 64, 128, or 256 operations, with the server returning its
negotiated limit independently of ACK cadence. The default remains 32 after
screening. The server outbox remains bounded but its production capacity rises
from 1,024 to 8,192 events per federate, exposed as a validated CLI option.

The Go comparison verifier now validates against precomputed expected callback
values and uses a reusable condition variable instead of allocating a channel
and recomputing a SHA-256 value for every measured callback. The Portico and
gorti verifier paths both consume handle maps at the callback boundary.
Benchmark contracts record callback representation, LocalLRC batch size, and
all three outbox settings.

**Review.** Coalescing limits of 32 and 256 and LocalLRC frame requests above
32 were rejected because they were slower or unstable in repeated native
screens. With the retained 64-event callback limit and 8,192-event outbox, five
native stress runs completed without the intermittent overflow seen at 1,024
events. An interleaved five-run verifier screen put handle callbacks below name
callbacks in four runs, with medians 2.528 s and 3.344 s respectively; this is
a harness-cost result, not an RTI ranking.

The final paired screen used one warm-up and four measured AB/BA pairs. Every
run completed on its first attempt, delivered all 81,200 callbacks, and
reported zero drops, duplicates, invalid callbacks, unexpected callbacks, or
rejections. gorti's median subscriber completion was 0.861 s at 94,287
deliveries/s; Portico's was 0.734 s at 110,695 deliveries/s. The aggregate
median ratio was 1.173, so gorti remained 17.3% slower on this boundary.
LocalLRC admission medians were about 0.786 us for both operations versus
Portico service-return medians of 2.510 us and 2.472 us, but those return
guarantees differ and are not a product-ranking result.

**Reflect.** Retain the semantic-preserving callback coalescing, opt-in handle
events, negotiated LocalLRC batch control, and larger bounded outbox. Keep 32
as the LocalLRC default. The verifier correction means this screen is not a
clean continuation of earlier harness timings, and four pairs are too few for
a release claim. The evidence supports better burst stability and a fairer
callback representation; it does not show that gorti has overtaken Portico.
Run the tracked five-warm-up, 30-pair campaign before updating publication
results.

## Milestone 12: event-log frame ownership

**Plan.** Reduce the per-operation event-log cost without bypassing sequence
assignment, Protobuf validation, sink errors, write-ahead ordering, persisted
bytes, replay, or close and sync behavior. Reject a sequence-only no-log path
unless its different marshal-error boundary is made an explicit contract.

**Do.** Give each federation writer one mutex-owned serialization scratch
buffer. Reuse frames up to 64 KiB and release larger frames after their
synchronous write so an outlier cannot become a permanent per-federation
memory cost. Marshal directly into the reusable frame rather than calling
`proto.Size` first and then traversing the message again in `MarshalAppend`.
The file and discard sinks continue to receive exactly the same
length-prefixed Protobuf bytes.

**Review.** The steady-state minimal-event append benchmark reached about
80 ns/op with zero bytes and zero allocations per operation. The first paired
screen, with frame reuse but the explicit size pass retained, placed gorti at
0.853 s and Portico at 0.744 s. After removing the duplicate size traversal, a
second one-warm-up, four-measured AB/BA screen placed gorti at 0.800 s and
Portico at 0.725 s. Relative to the M11 screen, gorti improved by 7.1% while
Portico improved by 1.3%; the gorti/Portico median ratio moved from 1.173 to
1.104. gorti's four runs occupied 0.777--0.831 s. Every run completed on its
first attempt with all 81,200 callbacks and zero semantic or accounting
defects.

The promoted campaign then ran five warm-up and 30 measured AB/BA pairs.
Portico's median completion time was 0.696 s (95% CI 0.664--0.734 s), and
gorti's was 0.808 s (95% CI 0.791--0.820 s). The paired median gorti/Portico
ratio was 1.167 (95% CI 1.097--1.194), while the order-adjusted ratio was 1.161
(95% CI 1.076--1.200). Portico was faster in 29 of the 30 pairs. All 60
measured runs completed on their first attempt, delivered exactly 81,200
callbacks, and produced matching callback and terminal-state digests with no
accounting defects.

**Reflect.** Retain bounded frame ownership and removal of the duplicate size
pass. Reject a transparent sequence-only or `EventLog=nil` replacement in this
milestone because it would remove the existing marshal-error boundary. The
short screen's 0.800 s gorti median reproduced closely at 0.808 s in the full
campaign. gorti did not overtake Portico. Under the predeclared practical
equivalence interval of 0.9091--1.1, the formal decision is inconclusive because
the paired ratio confidence interval begins at 1.097, just inside the boundary.
The point estimates and 29-of-30 pair direction still identify the server and
callback path as the next performance target.

## Milestone 13: optional Protobuf initialization check

**Plan.** Measure the cost of the event-log Protobuf required-field
initialization check without disabling event serialization, UTF-8 checks,
sequence assignment, sink writes, or the existing write-ahead boundary.
Retain strict validation as the default.

**Do.** Add an opt-in `--event-log-protobuf-validation=false` server setting,
implemented with `proto.MarshalOptions.AllowPartial`. The setting is forwarded
through the multiplex writer to each federation writer. Tests verify that the
option reaches the writer and that valid strict and partial marshaling produce
identical length-prefixed event-log bytes. The comparison harness records the
setting and changes no other transport, callback, logging, workload, or
measurement control.

**Review.** Repeated microbenchmarks reduced the reusable minimal-event append
median from about 81 ns to 75 ns. A nested interaction event showed a smaller,
noisier local reduction with the same 128 B and eight allocations per append.
In the current-build strict-validation control, the one-warm-up,
four-measured AB/BA screen placed gorti at 0.771 s and Portico at 0.712 s, a
gorti/Portico median ratio of 1.0834. With validation disabled, gorti measured
0.796 s and Portico 0.721 s, a ratio of 1.1034. The disabled-validation ratio
was 1.85% slower after Portico normalization, and pair-level directions were
mixed. All 16 measured implementation runs completed on their first attempt
with exactly 81,200 callbacks and no accounting defects.

**Reflect.** Keep the explicit diagnostic option and the strict default. The
local initialization walk has a measurable nanosecond-scale cost, but removing
it did not produce a measurable end-to-end gain in this workload. Do not
promote the setting into the publication benchmark or attribute an RTI-level
performance improvement to it. The current event-log schema is proto3 and has
no required fields, so this setting does not replace work on the server and
callback delivery path.

## Milestone 14: explicit event-journal profiles

**Historical status.** This milestone records the strict/off experiment as it
was run. The current runtime supersedes both profiles: `gorti-hla-core` is the
only built-in profile, and audit/replay is an optional plugin. The legacy flag
and profile identifiers below are retained only to interpret the recorded
measurements.

**Plan.** Separate human-readable diagnostic logging from the binary event
journal. Preserve the current journal and failure boundaries as the zero-value
`strict` default. Add an explicit `off` profile only for deployments and
experiments that accept the loss of event sequences, journal-originated error
boundaries, bounded replay evidence, and `AdminService.TailEvents`. Compare the
profiles with the same binary, FOM bytes, DVSHLA plan, seed, federate
processes, choreography, callback instrumentation, and subscriber completion
boundary.

**Do.** Add `--event-journal-mode=strict|off` at the `rtid` composition root.
Strict injects the existing multiplex writer. Off injects a true nil event log
into every FM, OM, DM, TM, ownership, synchronization, save/restore, DDM, MOM,
and admin dependency while retaining an unopened multiplexer for teardown.
This skips event creation, payload copies, timestamp lookup used only by the
record, sequence assignment, Protobuf encoding, mutex acquisition, and sink
write. The default and `rtidConfig` zero value remain strict. Invalid values
and off in non-server modes are rejected; off emits a startup warning.

The Portico comparison now records versioned runtime profiles and fixes its
official gorti arm to `gorti-journal-strict-discard`. A separate paired runner
compares that profile with `gorti-journal-off`, requires exact callback
evidence equality, alternates AB/BA order, retries only fully cleaned
participant or Docker attach failures, and retains only `comparison.json`.

**Review.** Local registry screens separated the cost into off, no-op journal,
and real strict-discard arms. Receive-order interaction medians were about
1.25 microseconds for off and 2.18 microseconds for strict-discard, with
allocations falling from 16 to 8. Attribute update medians were about
1.31 microseconds and 4.38 microseconds, with allocations falling from 18 to
8. A TSO-reserved interaction screen measured about 1.85 microseconds and
3.09 microseconds, with allocations falling from 20 to 11. These local results
are screening evidence, not publication performance claims.

The first end-to-end screen used one warm-up and four measured pairs. Its
paired journal-off/strict-discard completed-batch ratio was 0.8430, with a
deterministic order-stratified bootstrap 95% interval of 0.7900--0.9037. An
independent two-warm-up, six-measured confirmation produced 0.8400 with an
interval of 0.7407--0.9085. All ten measured pairs favored journal-off. Every
accepted run delivered 81,200 callbacks with matching payload, callback-trace,
and terminal-state digests and zero rejected, dropped, unexpected, duplicate,
or invalid callbacks.

The promoted campaign used five warm-up and 30 measured pairs. The primary
paired journal-off/strict-discard completed-batch ratio was 0.8455, with a
deterministic order-stratified bootstrap 95% interval of 0.8076--0.8850.
Journal-off favored 28 of 30 measured pairs. The order-stratum medians were
0.8794 when strict ran first and 0.8077 when off ran first; both strata retain
the same direction. The first- and second-half medians were 0.8794 and 0.8522,
so the direction also persisted across the run. Strict and off completed-batch
medians were 975.5 ms and 830.6 ms. All 70 profile runs completed on their
first attempt, cleanup succeeded, and every accepted run delivered exactly
81,200 of 81,200 callbacks with zero accounting defects and matching
callback-trace, terminal-state, and workload-instance digests within each
pair.

Caller-side latency remains secondary because caller completion is not the
subscriber batch boundary. Its median-of-run-medians was effectively
unchanged: `sendInteraction` measured 990.5 ns for strict and 995.5 ns for off,
while `updateAttributeValues` measured 1,000.0 ns and 999.0 ns. The measured
gain therefore belongs to completed delivery through the server path, not to
the synchronous API-return boundary alone.

**Reflect.** The formal result confirms that bypassing semantic journal work
reduces completed-batch time by about 15.5% under this two-federate workload.
Journal-off remains an explicit optional profile: successful-run HLA
observations are held equal by the benchmark, but journal-originated failure
reporting, event sequences, bounded replay evidence, and `TailEvents` are
intentionally unavailable. Strict therefore remains the default and the
official cross-product comparison profile. Further strict-mode work should
target allocation-aware event encoding and batching rather than presenting
diagnostic logging changes as equivalent to removing the semantic journal.

## Final regression gate

The retained Go changes pass `go test ./...`, including the federation,
declaration, object, transport, federate, Time Management, examples, and
specification packages. The Milestone 14 comparator suites pass 61 tests, and
the official gorti-Portico orchestrator passes 36 tests, including rejection
of journal-off data mislabeled as the strict official arm. Ruff, `mkdocs build
--strict`, and `git diff --check` complete successfully.

The Python SDK suite reached 919 passing tests with two failures in duplicated
pyjevsim determinism checks. Inspection showed that the server had accepted all
interactions, but a fixed wall-clock consumer tail could expire when the
producer process was descheduled for several seconds. The consumer example now
keeps its subscription alive until the known finite workload is received or a
bounded completion deadline expires. Both determinism checks then passed in a
focused 20-run replay. This changes termination choreography, not interaction
ordering or payload semantics.

The repository-wide Python gate still reports 35 `mypy --strict` diagnostics
in five pyjevsim/RL test files outside these performance changes. They remain a
separate cleanup item. Linux race validation is also still required because
the current Windows host has no C toolchain for Go's race detector.

## Follow-up

The full paired 5-warm-up plus 30-measured receive-order campaign is complete.
Before publication, repeat it from a clean release commit and archive the four
structured artifacts with the release record. Add a paired multi-process TM
workload before making comparative TM claims. The next code-level candidates
are callback serialization allocations, server-to-subscriber stream scheduling,
and multi-recipient reservations; each requires its own ownership or
topology-specific semantic gate.

Direct federate-to-federate delivery remains out of scope because it would
require distributed reservation, deduplication, replay, and callback sequence
merging.

Linux race testing remains required before release promotion. Windows race
testing is unavailable on the current host because the required C toolchain is
not installed.
