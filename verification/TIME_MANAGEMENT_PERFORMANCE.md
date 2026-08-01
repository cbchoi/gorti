# Time Management performance analysis

## Purpose

Time Management (TM) performance cannot be inferred from receive-order
Object Management results. A `timeAdvanceRequest` (TAR) has two distinct
boundaries:

1. caller latency: request submission until the service call returns; and
2. grant latency: request submission until the exact `timeAdvanceGrant` (TAG)
   callback is observed after all eligible timestamp-order callbacks.

This analysis measures both boundaries and rejects performance samples unless
the TM semantics pass first.

## Evaluation cycle

### Protocol

- Use the same FOM bytes, seed, lookahead, two independent federate processes,
  callback model, logging, and payload validation for every implementation.
- Stage all timestamp-order attribute updates and interactions before the
  `VERIFY_MEASURE` barrier.
- Advance publisher and subscriber in lockstep through logical times
  `1..count+1`.
- Require exact grants, timestamp preservation, and both subscriber callbacks
  before each measured TAG.
- Discard five complete process pairs, then measure twenty AB/BA pairs with
  balanced 10/10 precedence.

### Instrumentation

- `timeAdvanceGrantLatency` uses the same request-to-exact-grant boundary in
  the neutral Java verifier and the Go verifier.
- `timeAdvanceRequest` remains a separate caller-latency sample.
- Publisher and subscriber grant latencies are aggregated by role.
- The three-implementation semantic screen uses staged-TM mode, followed by
  the accepted reference/gorti AB/BA comparison procedure.
- Accepted Java samples use callback notification instead of the earlier 10 ms
  polling wait. Samples collected with polling are excluded.
- gorti commits the logical-time/pending transition before TAG can be consumed,
  while preserving rollback when delivery commit fails.

### Results review

The accepted run used seed `1516`, count `100`, five warm-up pairs, twenty
measured pairs, and FOM SHA-256
`28c473b45137c4becaa0f93259e7c8d1dffd6785353b971424402669430c2618`.

| Metric | Reference median | gorti median | Paired gorti advantage (95% CI) |
|---|---:|---:|---:|
| updateAttributeValues caller | 105.4 us | 93.6 us | 1.116x [0.994, 1.149] |
| sendInteraction caller | 88.2 us | 99.9 us | 0.885x [0.813, 0.917] |
| TAR caller | 146.1 us | 142.5 us | 1.041x [0.982, 1.069] |
| TAR to TAG | 14.959 ms | 209.8 us | 71.241x [69.107, 75.399] |
| completed delivery batch | 490.9 us | 179.0 us | 2.814x [2.654, 2.861] |
| publisher TAR to TAG | 15.011 ms | 205.8 us | 72.911x [69.794, 76.357] |
| subscriber TAR to TAG | 14.899 ms | 213.9 us | 69.642x [68.552, 73.575] |

Raw TAR-to-TAG p95 was 29.304 ms for the reference RTI and 436.9 us for
gorti; p99 was 30.181 ms and 750.2 us. gorti's TAR caller latency is
statistically consistent with parity in this run, while its end-to-end
exact-grant path is substantially faster. `sendInteraction` remains slower
and should be treated as a separate OM issue. Pair order was small in the
accepted run: the TAR-to-TAG advantage median was 71.600x when gorti ran first
and 70.793x when the reference ran first. All results remain paired, same-host
measurements.

The installed Portico 2.1.4 configuration did not enter the performance
comparison. With the same staged choreography and the local TCP discovery
override, the subscriber received `TAG(1)` before either timestamped callback.
The strict callback-before-grant guard therefore failed on the first cycle.
This is a result for this version and configuration, not a claim about every
Portico deployment. No Portico TM latency is reported because failed semantics
cannot produce an accepted performance sample.

### Decision

gorti's current two-federate TM path is usable and fast for this workload. The
highest-value follow-up is not a speculative scheduler rewrite; it is
instrumentation that separates the remaining roughly 0.2 ms grant path:

1. Add internal histograms for request decode/admission, LBTS snapshot and
   decision, TSO extraction, outbox reservation/WAL/commit, stream send, and
   SDK callback dispatch.
2. Investigate the rare 15 ms gorti outliers with Go runtime traces, GC data,
   and Windows scheduler evidence. Median and p95 are low, but maxima affect
   the mean.
3. Evaluate a dedicated bidirectional TM command stream to remove unary gRPC
   setup from TAR admission. The TAG remains authoritative; requests cannot be
   logically pipelined past an outstanding advance.
4. Reuse candidate and delivery buffers only after profiles show allocation
   pressure. With two federates, map scans and handle sorting are very small.
5. Preserve the current atomic TSO-before-grant reservation, write-ahead event
   ordering, failure rollback, generation validation, and immediate grant
   batch flush. These correctness boundaries take priority over microseconds.

## Critical path

The measured gorti path is:

`Federate.TimeAdvanceRequest` -> unary TM gRPC -> `dispatchAdvance` ->
per-federation evaluator lock -> LBTS snapshot -> sorted grant candidates ->
grant decision -> buffered TSO extraction -> atomic outbox reservation ->
event-log append -> reservation commit -> event batch stream -> SDK event
dispatch -> exact-grant waiter.

The first TAR often records pending state and returns. The peer TAR raises the
federation floor, runs the fixed-point grant loop, releases each recipient's
eligible TSO callbacks, and then emits the grants. This explains why caller
latency and TAR-to-TAG latency must remain separate metrics.

## Scope and limitations

- Results cover one Windows host, one FOM, two federates, and one staged
  timestamp-order choreography.
- The reference RTI used an existing persistent server; gorti starts a fresh
  server per arm. Server topology is part of each measured product.
- Whole process runs were discarded for warm-up; TM has no in-process logical
  time reset or operation warm-up.
- The Java and Go verifier APIs differ by language, but use the same payloads,
  ordering, semantic gates, logging, and measurement boundaries.
- These results do not establish unrestricted HLA conformance or scale beyond
  the tested federation.

The ignored local evidence is under
`verification/out/tm-pair-fair-20260715-v4/`; the latest Portico semantic
failure evidence is under
`verification/out/tm-three-way-semantic-screen-20260715/`.
