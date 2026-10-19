# Software Test Description

Document version: 4.6
Status: Current
Updated: 2026-10-18

## 1. Purpose

This document defines service scenarios, expected callbacks and errors,
persistence checks, and acceptance criteria for Go, Python, and Java.

The [4 October 2026 full service run](../../verification/ieee2010-full-service-rerun-post-sync-20261004.md)
passed 3,349 Go service tests on Windows and in a Linux race run, 2,157 Python
tests, 591 Java tests, and 35 internal IVCT-derived cases. Platform repeats and
supplemental fixture modes are counted separately. The 8 October callback-report
change passed the 978 required IEEE-service cases in both Go reporting builds;
the full Python/Java/Linux race selection was not repeated for that change.

The [requirements inventory](../../verification/requirements2010/README.md)
links reviewed clauses to implementation locations and test assertions. Its
4 October reconciliation found passing executions for all 639 applicable
records; 48 records were not applicable and 25 remained unresolved. Independent
assessment and formal certification are separate from these internal results.

Tests cover success and failure paths, callback order, state transitions, and
delivery accounting. Dated reports under `engineering/verification` retain
the development history; the scenarios below define current acceptance.

## 2. Test levels

| Level | Evidence |
|---|---|
| Unit | Service state transitions, validation, encodings, and error mapping |
| Integration | gRPC handlers, streams, persistence, teardown, and callbacks |
| Standard-interface regression | Java standard-mapping scenarios, Go/Python service behavior, and shared encoding vectors |
| Determinism | Repeated semantic projections and replay equality |
| Portability/differential | Equivalent black-box scenarios executed separately on another IEEE 1516-2010 RTI |
| Interoperability | Independently developed federates exchanging and using data under the declared FOM and federation agreement |
| Performance | Semantics-gated caller, server, callback, and throughput metrics |

## 3. Required service scenarios

### 3.1 Federation and Declaration Management

- default/explicit logical-time selection, matching create/list/join/roster/MOM
  metadata, unsupported selector rejection before state mutation, legacy absent
  metadata, and authenticated cleanup after contradictory join metadata;
- create, duplicate create, two joins, resign, destroy-with-members rejection,
  final destroy, and same-name recreation with a new generation;
- publish/subscribe and unpublish/unsubscribe with valid and invalid handles;
- synchronization success, missing participant, timeout, and teardown.

### 3.2 Object Management

- nearest subscribed ancestor discovery, retained known class after subscription
  changes, class-relative reflection/ownership handles, and saved discovery
  history validated against the saved FOM;
- ownership mapping and mutation under the same object lease, including all ten
  operation variants, inherited numeric-handle collisions, concurrent local
  deletion/rediscovery, release before callbacks, and release-denial remapping
  after callback admission;
- cancellable ownership-assumption epochs across local deletion and rediscovery,
  including same-class reuse, queue reservation and server stream dispatch;
- register/discover/update/remove an object instance;
- send and receive an interaction with fixed seeded payloads;
- receive-order and timestamp-order variants;
- zero, one, and multiple recipients;
- atomic fanout reservation failure and delivery failure;
- stale federation generation and complete teardown; and
- confirmed unary, confirmed stream, and LocalLRC completion accounting.

### 3.3 Time Management

Use two independent federate processes with lookahead 1. Both enable time
regulation and time constrained modes. Stage one timestamped attribute update
and one timestamped interaction for each measured logical step, then request
exact grants in lockstep.

The validator shall require:

- requests and grants exactly `1..count+1` for each role;
- both timestamped callbacks completed before the subscriber's related grant;
- no duplicate, rejected, dropped, or invalid callback;
- committed logical time and cleared pending state before grant visibility;
- grant withholding after TSO delivery failure; and
- immediate submission of the next request as a regression test for the grant
  visibility boundary.

The RO campaign additionally requires:

- reflection, interaction and removal held for an idle constrained receiver
  with asynchronous delivery disabled, but released when nonconstrained,
  advancing or asynchronous;
- discovery and control callbacks remaining independent of that gate;
- all five advance modes releasing applicable queued RO before TAG, including
  RO release while a peer still blocks the requested grant;
- repeated asynchronous enable/disable and constraint disable, with exactly-once
  release and no recall of messages already admitted to the stream;
- original optional timestamps, binary payloads, tags and received-order metadata
  preserved without releasing future TSO when asynchronous delivery is enabled;
- queued interaction unsubscription and attribute-subset changes evaluated at
  release, with removal still delivered for a previously discovered object;
- backlog larger than the production outbox capacity making bounded-prefix
  progress, with the unsent suffix retained and no TAG overtaking it;
- failed grant release recovering through the application retry worker after a
  public service call, with no private reevaluation call or peer activity needed;
- capacity pressure accepting asynchronous-enable/constraint-disable state while
  continuing the drain, and other errors rolling back flags without losing the
  remaining queue; and
- retry-worker checkpoint/generation admission, cancellation and joined shutdown.

Use IEEE 1516.1-2010 8.1.6 Table 3 (PDF pages 186-188), 8.1.7 (page 189) and
8.1.8 (page 191) for the reviewed message-before-grant, eligibility and final
subscription oracles. Keep the historical explicit-order RO cases separate from
the preferred-order cases below. Rechecking already queued recipients does not
verify retaining unsubscribed-at-send
candidates under the enabled Delay Subscription Evaluation Switch.

The bounded order/advance batch additionally requires:

- FOM Receive and TimeStamp defaults across all federation profiles, independently
  of explicit overrides in either direction, including unspecified or absent mode
  lookup in embedded configurations;
- mixed-order attributes partitioned without loss or duplication, deletion using
  `HLAprivilegeToDeleteObject` rather than an unrelated attribute, and ordinary,
  wire and regional send paths preserving timestamps, tags and order metadata;
- untimestamped sends remaining RO, TSO-only timestamp/retraction admission, and
  atomic rejection or admission of mixed deliveries, including no-recipient TSO;
- finite TAR, TARA, NMR, NMRA and FQR from a joined nonregulating, nonconstrained
  caller, plus regulating-only callers whose grants are not bounded by a peer's
  GALT; invalid targets must not create initial time state;
- constrained same-current-time TSO held while time-granted with asynchronous
  delivery on and off, including after an Available grant; NMR/NMRA must select
  current-time queued messages and deliver them before the same-time TAG;
- a separate time-state round trip for advances without either mode, checking
  retained logical time and later enablement without resetting that time;
- separate held-TSO snapshot/import cases, with no callback during import or
  asynchronous enablement, followed by message-before-grant delivery; and
- production gRPC membership cases for all five services: joined callers without
  either mode succeed, while unknown/resigned callers report
  `FederateNotExecutionMember` without creating or mutating time state.

Focused locations are `rti/internal/hla/object/preferred_order_test.go`,
`rti/internal/hla/time/ieee2010_advance_state_test.go`,
`rti/internal/hla/time/advance_state_persistence_test.go`,
`rti/internal/application/server/advance_membership_test.go` and
`pysdk/tests/spec/m21/test_ieee2010_order_advance_live.py`. Record each executed
scope in the new report; do not infer public save/restore coverage from a manager
round trip or production membership enforcement from a direct manager call.

The [flush, activation and retraction tests](../../verification/ieee1516-2010-flush-enable-retract-20260922.md)
add full-queue and minimum-grant cases, safe activation and pending exceptions,
delivered-message retraction, original deletion owners and saved-state identity.
They also cover current-subscription filtering in both DSE settings, FQR actual
delivery minima, NMR/NMRA empty-group reevaluation, receiver-specific deletion
expiry, query-only tombstone cleanup, implicit-enable timed saves and Go/Python
callback-before-response races. Current SDK acceptance covers Go, Python, and Java.
Capacity-failure cases must prove correct retry without duplicate callbacks or
time regression. Delayed subscription evaluation includes candidates retained
at send time and DDM filtering at delivery. Per-federation Integer64 production
tests are listed below; Float64 compatibility sentinels have separate cases.

Typed-domain and transport regressions define the bounded exact-time evidence:

| Boundary | Named regression cases |
|---|---|
| Integer domain and independent bounds | `TestIntegerManagerAllModesUseExactQueueOrder`, `TestIntegerManagerFinalTimeAndUnboundedRemainDistinct` in `rti/internal/hla/time/integer_manager_test.go`; `TestTypedGrantReviewIntegerBoundsBigIntOracle` in `typed_grant_review_test.go` |
| Object queue/deletion | `TestIntegerObjectRealQueueDeletionAndSnapshot` in `rti/internal/hla/object/integer_time_test.go` |
| Exact unary transport and fail-closed optimizations | `TestNewServerOfInteger64ExactUnary`, `TestNewServerOfRejectsInteger64Optimizations` in `rti/internal/adapters/grpc/server_typed_test.go`; `TestInteger64TimeServiceRejectsLegacyAndMixedTimeWithoutEffects` in `integer64_time_test.go` |
| Exact persistence and factory rejection | `TestInteger64TimeSnapshotCompleteQueuesAndFrontiers` in `rti/internal/adapters/savebundle/integer64_state_test.go`; `TestInteger64RestoreGraphRejectsManifestFactoryMismatch` in `integer64_graph_test.go` |
| Production composition | `TestProductionIntegerCompositionIdleMOMExactTimeAndIsolation`, `TestProductionIntegerTwoGoSDKFederatesExactGrants`, `TestProductionIntegerTimedSaveRestartAndIdentityRebinding`, `TestProductionIntegerObjectTSOReflectionBeforeExactGrant` in `rti/internal/application/server/integer_production_test.go` |
| Python actual-server boundary | `pysdk/tests/test_integer64_production_live.py`: five advances, exact queries, adjacent TSO ordering and invalid representations |

These cases require exact adjacent values above `2^53`, finite final versus
unbounded horizons, and no state change on invalid representations. Test presence
is not an executed campaign result. The production cases must use the assessed
server configuration and prove factory selection without floating-point
conversion. Float64-only optimized transports must reject Integer64 calls before
effects. Neither campaign establishes full conformance.
The [exact-time and callback record](../../verification/ieee1516-2010-exact-time-callbacks-20260930.md)
records 70 Go packages, 2,742 passing top-level tests, 7,031 passing test/subtest
events and 12 affected Linux race packages. The official supplementary schema was
recovered with its recorded hash; the final Go rerun has no skipped tests.
SDK campaign results and the executed profile gate are recorded separately there.

### 3.4 Ownership, DDM, save/restore, and MOM

Tests cover single-owner invariants, acquisition/divestiture races, resign
cleanup, region overlap and non-overlap, non-DDM equivalence, versioned save
bundles, restore rejection, and standard management-object reflection.

Association lifecycle cases additionally cover pre-acquisition inactive regions,
ancestor-known attributes with numeric-handle collisions, ownership activation and
loss, region/local deletion, resignation, and rollback concurrent with ownership
changes. Save tests cover multiple inactive associators, identity rebinding, legacy
snapshots and corrupt knowledge/region references. Actual Python/server cases
check routing before acquisition, after acquisition and after cleanup.
They use both string and numeric attribute keys. Python unit cases additionally
reject partial string updates, duplicate aliases and failed knowledge queries,
and confirm that numeric updates issue no support lookup.

Terminal receipt regressions distinguish queue admission and transport success
from authenticated actual callback invocation. Failure cases must retain outcomes
without repeating application code or allowing local deletion prematurely:

| Boundary | Named regression cases |
|---|---|
| Precommit obligation, failed send and atomic reservation | `TestTerminalCallbackEarlyInvocationAndFailedSendRetry`, `TestTerminalCallbackReleaseDeniedAtomicReservationAndProjectedIdentity` in `rti/internal/hla/ownership/terminal_callbacks_test.go` |
| Complete restore scope batch | `TestOwnershipCallbackRestoreScopesValidateAllBeforeMutation`, `TestOwnershipCallbackRestoreScopesCommitAtomicAndInfallible` in `rti/internal/hla/ownership/callback_restore_scopes_test.go` |
| Real receipt RPC/stream | `TestOwnershipReceiptRealWireInvocationAndDuplicateReplay` in `rti/internal/adapters/grpc/ownership_receipts_test.go` |
| Composed save/restore and authentication | `TestMandatoryOwnershipReceiptsProductionSaveRestore` in `rti/internal/application/server/ownership_receipts_test.go` |
| SDK invocation and buffered advice fence | `TestOwnershipTerminalActualInvocationAndRetry`, `TestOwnershipBufferedAdviceEpochAndLocalDeleteFence` in `rti/pkg/federate/ownership_callback_delivery_test.go` |

Require stale generation/epoch rejection, cross-member authentication rejection,
same-epoch replay acknowledgement, and invocation of saved pending outcomes after
restore. Optional MOM receipt coverage is not a substitute for the mandatory
ledger. Python/Java nonzero-generation join negotiation is implemented, but needs
separate executed SDK evidence; raw dispatch remains explicitly negotiated.
Unpublication contracts additionally cover owned-attribute relinquishment,
pending-acquisition guards, ancestor-known projection, concurrent publication
growth, callback-capacity rollback and explicit-empty versus omitted attribute
sets. `TestProductionUnpublicationOwnershipAndInvocation` in
`rti/internal/application/server/unpublication_production_test.go` exercises
authenticated public RPCs for both time implementations. Results belong in a
verification report, not inferred from test presence.

### 3.5 Mandatory Callback Entry and Completion

Synchronization callbacks use authenticated entry and outcome reports. Entry
must precede the application callback body so that an announcement handler can
call `synchronizationPointAchieved` reentrantly. Transport delivery alone must
not authorize that service or retire the synchronization label. Completion
applies the mandatory lifecycle transition once, including a throwing callback;
an optional MOM metric failure must not reopen it.

Require all-recipient receipt-capacity rollback, transient application-failure
retry, immutable original payloads, ticket deduplication, failed-send recovery,
restored-handle reconnect and stale-generation rejection. The Go/Python/Java
interfaces must negotiate entry support before sending a pre-body entry report;
an older server must never mistake that report for callback completion.

Adapter cases are in `callback_entry_test.go` and `callback_required_test.go`.
`TestSynchronizationProductionTCPGoSDKReentrantCallbacks` adds actual TCP/SDK
evidence for Float64 and Integer64. Save/restore tests must preserve uninvoked
domain obligations, not just queue contents. Record executed binding and race
campaigns independently.

The bounded DDM registration campaign additionally checks real returned
identity, invalid-later-pair rollback, creator and dimension context, ordinary
and regional recipient composition, name retry, deletion cleanup, numeric
bound invariance and same-roster restore. SDK forwarding tests must assert
one DDM registration RPC and no preliminary ordinary registration. The manual
evidence gate requires the named Go cases; SDK-language campaign results are
recorded separately in `engineering/verification/ieee1516-2010-ddm-registration.md`.

RO persistence acceptance covers time-state v4 encode/decode and actual public
save/restore with waiting reflections, interactions and removals. Assert retained
original timestamps, tags, payloads, metadata, time flags and pending advances;
post-save live queues must be replaced, not merged or replayed twice. Cover
recipient rejoin, saved identities, typed cross-generation rebinding, deleted
objects and resigned producers. Invalid references must fail before publication.
Eligible messages resume only after the restored identity/epoch terminal boundary.
Scheduled saves must release required eligible messages before initiation without
discarding the original advance target; immediate saves must not release idle RO.

Compatibility cases retain v1-v3 readers, version selection with and without RO,
strict TSO timestamp checks, and rejection of RO fields mislabeled as an older
version even when the field is empty or null. Descriptor/RPC compatibility checks
must show no new wire change from this work.

MOM queue tests distinguish RO and TSO data across time-waiting and admitted
ready/transport/client stages. Release to transport must conserve the combined
count until authenticated callback entry. Control callbacks and tentative
reservations must not increase it. Require independent equal-message and partial
handoff/retry oracles, foreign-entry rejection, duplicate-entry idempotence and
unknown-coverage errors. Fresh-process already-ready/client recovery is not
inferred from same-process time-waiting restore tests.

Incoming MOM counter tests require entry before measurement, no duplicate count
at outcome, atomic failure/retry and reflected-invocation rather than attribute
counting. Pending outcomes must not mask the three entry-authoritative incoming
metrics when entry observation is configured, while legacy/control pending state
and failure precedence remain unchanged. Optional observation failure must not
prevent invocation of the user callback body.

## 4. Cross-language tests

Go, Python, and Java shall consume the same FOM and encoding vectors. For each
supported data type, encoded bytes and decoded values must match. Equivalent
federates shall produce the same canonical FM, DM, OM, and TM projection after
language-specific setup records are removed.

## 5. Requirement traceability

| Requirement groups | Primary executable evidence |
|---|---|
| FR-FM, FR-DM, FR-OM | `rti/tests/integration/spec/`, `tests/conformance/rti/`, SDK integration tests |
| FR-TM | `rti/internal/hla/time/`, `rti/tests/integration/spec/`, `verification/gorti-go-fair/` |
| FR-TM-RO, FR-SR-RO, FR-MOM-RO | Time/object RO tests, savebundle RO state/graph tests, application delivery-retry/MOM tests, `pysdk/tests/spec/m21/test_ieee2010_ro_delivery_live.py` |
| FR-OM-ORDER, FR-TM-ADVANCE, FR-TM-TSO, FR-SR-ADVANCE | Preferred-order, advance-state persistence, production membership tests, and `pysdk/tests/spec/m21/test_ieee2010_order_advance_live.py`; execution results in the 4 October service report |
| FR-OWN, FR-DDM, FR-MOM | Corresponding `rti/internal/hla/*` tests and conformance fixtures |
| FR-FOM, FR-ENC | `rti/pkg/fom/`, `rti/pkg/encoding/`, `tests/conformance/` |
| FR-EVT, save/restore | `rti/internal/adapters/eventlog/`, `rti/internal/adapters/savebundle/`, HLA state round trips |
| IR-WIRE, IR-SDK, IR-CB | `proto/rti/v1/`, adapter tests, descriptor golden test, and SDK tests |
| NFR-DET, NFR-PERF | deterministic examples and `verification/fair-comparison/` |

The listed directories are ownership-neutral evidence locations. Each public
behavior change must add or update a focused test close to the affected
implementation and, when externally observable, an integration or conformance
scenario.

## 6. Fair performance protocol

Comparative evidence requires:

- identical FOM bytes and recorded SHA-256;
- seed 1516 and identical generated payloads;
- two independent federate processes per implementation;
- identical sequential or parallel choreography;
- identical callback handling and declared runtime profiles;
- the same caller and completed-delivery boundaries;
- the predeclared warm-up and measured-run counts;
- balanced AB/BA pairs or ABBA/BAAB seed blocks, as selected by the runner;
- exact service and delivery accounting; and
- semantic acceptance before any latency or throughput result is reported.

Cross-product publication comparisons use the default `gorti-hla-core`
profile. The `gorti-audit-replay` profile is accepted only as a separately
labeled intra-gorti plugin-overhead experiment with exact successful-run
callback evidence equality.

Report median, p95, p99, paired ratios, paired-bootstrap 95% confidence
intervals, and order effect. Crossover intervals resample complete seed blocks
within execution-order strata. Queue admission shall not be compared with a
confirmed server-completion boundary.

## 7. Acceptance and rejection

A test run is accepted only when configuration provenance, FOM identity,
process count, semantic projection, callback order, logical grants, and
delivery accounting all pass. Missing provenance, a changed measurement
boundary, callback-before-grant failure, partial teardown, or retries that hide
a deterministic failure reject the run.

## 8. Standard commands

The source and documentation baseline is:

```text
buf generate
go test ./...
python scripts/validate_sdks.py python java ivct
make determinism
python -m mkdocs build --strict
```

The race profile additionally runs `go test -race` over the affected Go
packages. The SDK runner builds and selects the current server and starts the
Java endpoint. External IEEE source/schema and cross-binding checkpoint fixture
diagnostics require their separate inputs. Verification-tool tests under
`verification/**/tests` are required when their corresponding runner changes.

`go test ./tests/architecture` is a mandatory source-boundary gate. It rejects
gRPC, generated Protobuf, Prometheus, HTTP, and filesystem imports from HLA
production packages and rejects production APIs named only for tests. The
generated Go package also hashes the complete descriptor set and RPC surface.
Existing event-log and save-bundle fixtures must continue to read, replay, and
restore after an internal package move.

Use `bash scripts/ci-gates.sh` for the default `stubs go python java static ivct`
sequence, or select individual stages. Current release acceptance covers Go,
Python and Java; retained C++ results are historical. Comparisons record the Section 7
provenance and acceptance checks alongside their measurements.

## 9. IEEE 1516-2010 Evidence Gate

The [quality-gate policy](../../verification/ieee1516-2010-quality-gates.md)
separates bounded regression results, complete normative assessment, independent
verification and certification. A passing comparison, method-count check or
IVCT-inspired subset shall not be presented as complete RTI conformance.

The [source-based review](../../verification/ieee1516-2010-normative-review-20260921.md)
registers the three supplied 2010 redlines. Normative assertions must identify
the standard, clause and one-based PDF page, distinguish the responsible RTI,
federate, federation or modeling tool, and exclude deleted earlier-edition text.
Inspect the page rendering for each requirement used as an oracle; extracted
text alone is insufficient for a redline. Keep standard text and page images
outside the repository. An ambiguous passage is an individually tracked source
question, not a reason to suspend unrelated service tests.

The [Python-first correction record](../../verification/ieee1516-2010-python-behavior-20260921.md)
adds source-derived future-TSO, received-order/retraction metadata, exact positive
update-rate, and additive regional-subscription cases. Test both an already-enabled
async receiver and a queue populated before enabling async; compare binary payloads,
tags, timestamps and callback-before-grant order. Regional option groups must survive
save/restore, and delivery failure must retain a retryable queue. The Python campaign
inventory includes these cases. Current Python HLA collection excludes the
legacy C++ interoperability and optional RL/PPO selections.

IEEE 1516.1 Clause 13.2 requires at least one language-specific API and the
specified service behavior. Its Clause 13 does not permit an internal test run
to grant the "HLA Conforming" label. Java is the selected standard language
mapping; Go and Python have their own product-interface tests.

The local/manual executable is `scripts/ieee2010_quality_gate.py`, configured by
`engineering/verification/ieee1516-2010-evidence.json`. It requires named tests
to execute and rejects failures, skips, empty selections and missing evidence.
`--check` validates a plan without reporting an executed PASS. `--claim full-2010`
also requires reviewed normative applicability and independent evidence; the
current incomplete inventory must fail this mode. This adds no automatic CI job.

Changes addressing an observed standard-service defect require a failing
behavioral regression before implementation, then passing focused tests and
review of state, errors and callbacks. For concurrent mutations, exercise
interleavings with explicit hooks/barriers. Record remaining paths separately;
passing one test shall not close an entire service group.

Keep execution transcripts outside version control. Retain the exact tested
source/build, toolchain, configuration and inputs for release evidence; a dirty
working-tree run is development evidence unless its source snapshot is archived.

The current bounded campaign includes qualified MIM class identity, inherited
target parameters, actual MOM adjustments through ObjectService, branch-local
FOM validation, ownership query preconditions, binary ownership tags and atomic
divest-operation results. Name-only dispatch fixtures and recording emitters do
not replace production repository/codec tests. Full RTI coverage remains subject
to the [whole-RTI audit](../../verification/ieee1516-2010-full-rti-audit.md).

The live-service campaign additionally requires stable authoritative bindings
through additional-module join, failed-join publication rollback, scheduled
save with crossing advances and zero lookahead, saved-identity restoration,
stale callback/cache invalidation, update-rate advisory rollback under
concurrent ownership changes, and real MOM report routing. Production local
server tests exercise current Go, Python, and Java paths. The historical
[implementation record](../../verification/ieee1516-2010-live-services-20260920.md)
lists executed evidence and remaining behavior separately.

The remaining-service campaign adds production restart restoration into a
fresh generation, complete typed snapshot graph rejection, authenticated
automatic resignation and reconnect/rejoin races, synchronization failure
history, retractable timestamped deletion, atomic ownership denial and multiple
name release, LocalLRC snapshot markers and retired tickets, explicit Python
callback models, and actual callback receipt accounting. Each defect found in
review requires a reproducing regression before the fix is accepted.

The acceptance gate retains historical descriptor projections. Additive fields
and RPCs must not change any earlier field number or RPC signature. Full-standard
claim mode still requires normative applicability review and independent
evidence; passing this implementation campaign does not bypass that gate.

See the [remaining-service record](../../verification/ieee1516-2010-remaining-services-20260920.md)
for implementation, review corrections and the current verification status.

The [second-batch record](../../verification/ieee1516-2010-batch2-20260920.md)
adds metadata retention through dynamic join and savebundle restore, official
XSD plus independent semantic vectors, authenticated handleless MOM attribution,
checkpoint admission reuse, Python federation inputs, and strict original-frame
replay comparison. Retired identities and conflicting joins must not change
current state or be attributed to a replacement federate. Replay passthrough
byte equality is not evidence that an unsupported operation was applied.

Delivery-accounting prototype tests execute separately from production queue
accounting. Historical C++ build results are outside current SDK acceptance.
Required Windows/Linux cases must execute with their actual prerequisites;
packages with no test files are distinguished from skipped test cases.

The [third-batch record](../../verification/ieee1516-2010-batch3-20260920.md)
adds declared transportation and exact-rate round trips, independent XSD/semantic
mutations, separate MIM input and pre-mutation join expectations, terminal MOM
resign reports, and opt-in production outbox diagnostics. Negative MIM tests must
prove unchanged model, roster and handle allocation, not merely a returned error.
Signed resign rejection must not report against an unrelated requested handle.
Queue observations must remain conserved through restore and concurrent close;
only closed observations may be described as final.

Python tests cover actual session metadata, V2 MIM forwarding, legacy-server
rejection and local cache preservation. Current Integer64 acceptance includes
exact values above 2^53, extrema, checked arithmetic, service calls, callbacks,
and persisted time state in the Go/Python/Java selections.

The [callback/SDK/MOM campaign](../../verification/ieee1516-2010-callback-sdk-mom-20261001.md)
adds distinct ready/transport/client callback conservation, fresh capabilities,
terminal-first replay, duplicate payloads, admission-order reconnect and exact
copied-region leases after a true server restart. Its SDK cases separately cover
nonempty portable runtime state, translated identities, object knowledge,
exact logical time and old-epoch fencing in Go, Python and Java. Unknown private
formats and unknown invocation results must fail without publishing partial
restored state. Retired outcomes never authorize body reinvocation.

MOM acceptance executes the literal 32-service catalog in both time families,
checking real effects and reports plus unsigned, foreign-token, unknown-target
and undefined-parameter rejection. Separate report, regional routing and
Reliable/BestEffort partition cases do not stand for every IEEE exception
permutation. Version-1 Reliable and version-2 partitioned traffic fixtures remain
required, along with unsupported-version rejection. Exact runs and coverage
limits belong to the campaign record; full-standard readiness remains separate.

The control/outcome recovery campaign adds descriptor-derived stream-control
inventory, pending/outcome/completed phases, source-before-writer preflight,
fresh-capability reporting without body reentry, ACK-loss tombstones, repeated
save/restore, synchronization duplicate suppression and ownership authority
renewal. SDK actual-TCP tests use stopped/restarted servers and fresh clients;
unit inventory coverage is not described as every callback's live execution.
Additional regressions require obsolete-advice retirement without capacity leaks,
identity-only tombstone conservation, failed completed-marker send/reconnect and
queued-marker eviction protection. SDK-local notifications require an explicit
kind inventory, strict DTO rejection cases, current-reference-only rebinding,
local FIFO/multiplicity, terminal-first dispatch and absence of repeated RPCs.
An arbitrary closure or pending activation must fail export, not be omitted.
Transportation recovery tests require actual MOM requests through TCP, fresh
server generations, planned callback epochs, old-ticket rejection, pending versus
already-activated intents, and finished results after source cache eviction in
both time families. Domain tests separately cover ownership-loss projection,
empty remaining sets, required activation-phase decoding and foreign-federation
capacity leases. Unconsumed acknowledgement tickets must remain bounded before
and after installation. Post-import terminal failure tests require a failed
restore state, retained committed-state diagnosis, and no success journal record,
invented terminal notification or time resume; imported state is not rolled back.
The current results are recorded in
[the control/outcome record](../../verification/ieee1516-2010-control-outcomes-20261001.md).
