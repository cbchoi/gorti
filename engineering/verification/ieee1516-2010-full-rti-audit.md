# IEEE 1516-2010 Whole-RTI Engineering Audit

Reviewed: 2026-09-19. Target: IEEE 1516-2010, IEEE 1516.1-2010 and
IEEE 1516.2-2010. This is a whole-service-area engineering inventory, **not a
complete normative requirement inventory or a conformance declaration**.

## Verdict And Review Boundary

Current checkpoint: the [remaining-service implementation record](ieee1516-2010-remaining-services-20260920.md)
supersedes the original observations where it documents tested corrections.
The preceding [live-service record](ieee1516-2010-live-services-20260920.md)
documents dynamic FOMs and the initial MOM report corrections. Subsequent work
adds cross-generation restore, authenticated automatic resign, deferred deletion,
ownership denial, LocalLRC reinitialization and measured callback accounting.
The older findings below retain their audit history; the latest checkpoint
distinguishes corrected behavior from remaining implementation and evidence gaps.

**Full RTI conformance is not established.** The original audit found successful
no-ops, discarded service parameters, synthetic callback results, incomplete
persistence, and tests unable to distinguish those paths from correct behavior.
Those observations describe the audited revision; current dispositions require
the subsequent implementation records and their executed regressions.
The existing `full-2010` gate is a bounded evidence-prerequisite checker, not a
whole-RTI semantic test campaign. Passing existing Go tests, C++ fixture `FULL`
labels, or that gate with a future manifest cannot establish full conformance.

This audit read the current production paths, selected test assertions, the
[C01-C10/N/Q matrix](D:/workspace/gorti/gorti/engineering/verification/ieee1516-2010-requirements-matrix.md),
[implementation record](D:/workspace/gorti/gorti/engineering/verification/ieee1516-2010-implementation.md),
[assessment](D:/workspace/gorti/gorti/engineering/verification/ieee1516-2010-conformance-assessment.md),
and [sourced quality gates](D:/workspace/gorti/gorti/engineering/verification/ieee1516-2010-quality-gates.md).
HEAD was `3cf610c9bcd7953c91e1e38cdf9dd05155bf9e13`, with extensive tracked and
untracked concurrent changes. HEAD alone does not identify the audited source.
File/line anchors describe the functions read, not an immutable release.

The initial audit was read-only, apart from this report. Its only executable
gate invocation was the prerequisite check below; source inspection and test
presence are not newly executed test evidence. Subsequent corrections and
executed results are recorded in the implementation checkpoint. Read findings
below together with that checkpoint rather than treating earlier source
observations as a frozen release.

The current integration corrects qualified FOM/MIM identity, inherited MOM
targets, ownership-query preconditions, ownership tags and atomic divest results.
It does not close the remaining whole-service findings below. An additional MOM
response defect is recorded in the implementation checkpoint: counter handlers
emit `HLAcount`, which does not match the standard MIM report parameter layout.

## Sources And Applicability

The three full normative texts and applicable corrections were unavailable.
No clause numbers are supplied or inferred from repository comments. In
particular, comments calling a void-returning no-op "spec-legal" are not evidence
that a required state transition may be omitted.

The local official supplemental directory is
`C:/Users/cbchoi/AppData/Local/Temp/gorti-ieee1516-audit-20260918`.
Read-only SHA-256 checks in this audit confirmed:

| Artifact | SHA-256 / use |
|---|---|
| `1516.1-2010_downloads.zip` in that directory | `e6343f40b066d51b7f435c9aae6788be24f5d585e370d972c032ab0b503e27c1`; matches the quality-gates source record. |
| Official `interface/1516.1-2010_downloads/HLAstandardMIM.xml` and repository `rti/pkg/fom/mim/standard-mim.xml` | Both `839f092b4b3e02a1363ec4463efcb1aae7fbfdfb17778b159065f277a3f2b023`. File identity does not prove parsed class identity or MOM behavior. |
| `cpp-api/cpp/src/RTI/RTIambassador.h` | Independent API reference inspected for service signatures, callback model, time factory, DDM, support and ordering methods. No full official-header link/ABI campaign executed. |
| Sibling official FDD XSD, Java API archive and WSDL | Baselines registered by the earlier review, not full normative texts. The relaxed FDD schema does not establish all OMT requirements. No new Java/WSDL execution evidence. |

Applicability flags for the future inventory must be explicit:

- `M`: mandatory, with reviewed normative locator and responsible component.
- `C(trigger)`: conditional, with the exact trigger and evidence whether it holds.
- `U`: unknown because text, applicability, or profile interpretation is unresolved.
- `NA(reason)`: not applicable only with a reviewed normative rationale.

All whole-service groups below remain `U` for their **complete normative scope**.
Known APIs and demonstrably broken implemented behavior are engineering work now;
that does not justify inventing an `M`/`C` classification. "Not implemented" is
never an `NA` reason. C++/Java/WSDL distribution does not by itself prove either
universal binding requirements or exemptions. The same applies to callback
models, integer time, scheduled saves, and deployment options.

## Highest-Signal Code Findings

`F01-F13` are this audit's engineering identifiers, not IEEE requirements.
They separate observable implementation defects from the exact normative
conditions, exceptions, and permissible scheduling still controlled by Q01-Q10.
Reproductions below are **candidate assertions, not tests executed here**.

### F01 / P0: Restore Does Not Restore Most RTI State

Update: the subsequent same-roster checkpoint milestone adds object,
declaration and time snapshots and extends MOM runtime state. Production now
requires all seven snapshot keys. The observations below describe the original
audit state; see the [checkpoint report](ieee1516-2010-checkpoint-state.md) for
current coverage and remaining callback/network-buffer limitations. Attribute
values are federate-owned; the object registry has no last-value cache to save.

Production [snapshot composition](D:/workspace/gorti/gorti/rti/internal/application/server/run.go:1303)
registers only synchronization, ownership, MOM and DDM. Object instances/values,
name reservations, per-federate knowledge, declarations, time state/queues and
transport overrides are not snapshot participants there.
[Bundle writing](D:/workspace/gorti/gorti/rti/internal/hla/save_restore/manager.go:822)
always supplies an empty event-log slice; opening a reader does not copy records.
[Restore](D:/workspace/gorti/gorti/rti/internal/hla/save_restore/manager.go:961)
discards the bytes returned by `ReadBundle`, applies the four manager snapshots,
and then emits restore callbacks. There is no replay in that request path to
fill the missing state, despite comments describing one.

Consequently a completed restore can combine saved ownership/DDM state with
post-save objects, declarations and time. Save an object and nonzero logical
time, mutate/delete/register and advance afterward, restore, then exercise
lookup, update authorization, registration/name reuse and a new time advance.
Assert RTI-owned state, not just `federationRestored`. Application Counter memory
is the federate's responsibility and must not be the sole RTI restore oracle.
Q04 decides the complete required state and legal save points, but the absent
production restoration path is already established.

### F02 / P0: Save/Restore Isolation And Identity Remain Incomplete

Update: production admission now fences unary, persistent mutation streams and
admin mutations against checkpoint control calls. The required-state profile
defers commit until all participants succeed and rejects changed identities.
It does not implement handle rebinding or snapshot unread transport frames and
client callback queues. Direct in-process callers must use the same admission
boundary. The original findings below are retained for traceability.

[Save manager operation locks](D:/workspace/gorti/gorti/rti/internal/hla/save_restore/manager.go:930)
serialize save/restore work; the ordinary-service
[membership interceptor](D:/workspace/gorti/gorti/rti/internal/adapters/grpc/server.go:302)
only acquires membership admission. It does not consult save/restore state.
Sequential per-manager snapshot collection/application is therefore not a
federation-wide transaction with ordinary updates, ownership, declarations, DDM
and time calls. Prepared decoding repairs late-decoder failures, not visibility
of partially committed state to concurrent services.

[Restore name routing](D:/workspace/gorti/gorti/rti/internal/hla/save_restore/manager.go:979)
maps callback destinations to current handles while `postHandles` retains saved
handles. It does not rebind membership or remap the handles embedded in restored
ownership/DDM state. A resigned/rejoined participant can receive completion while
live authorization still uses a different identity. Test a post-restore update,
ownership operation and region operation with the returned identity, not just
the status vector. Unresolved/missing saved participants and abort/failure after
snapshot application also need state-continuation assertions. Q04 determines
which services must be rejected/deferred and the required restored identity;
neither a global blanket ban nor a callback-only remapping is a justified answer.

### F03 / P1: Scheduled Saves And Failure Reasons Are Still Missing

Update: initiation callback failures now fail the operation, and strict restore
reserves every terminal success callback before committing state. Delivery
errors are surfaced. Scheduled saves, complete failure-reason binding coverage
and normative service-state requirements remain open.

[Save requests](D:/workspace/gorti/gorti/rti/internal/hla/save_restore/manager.go:360)
reject every non-nil save timestamp with `ErrScheduledSaveUnsupported`; this is an
honest bounded correction to immediate initiation, not scheduled-save support.
[C++ save failure](D:/workspace/gorti/gorti/cppsdk/src/dlc/FederateAmbassadorBridge.cpp:540)
always reports `RTI_UNABLE_TO_SAVE`, and
[restore failure](D:/workspace/gorti/gorti/cppsdk/src/dlc/FederateAmbassadorBridge.cpp:603)
always reports `RTI_UNABLE_TO_RESTORE`. Wire failures carry labels rather than a
complete reason contract. Several save/restore callback sends, including
[initiation](D:/workspace/gorti/gorti/rti/internal/hla/save_restore/manager.go:1027),
discard outbox errors after changing state. Exercise scheduled-time admission,
participant negative completion, explicit abort, storage failure and failed
callback delivery separately. Q04 controls the exact failure enum and protocol.

### F04 / P0: Callback Metadata And Transport/Order Effects Are Incomplete

[Reflect/interaction conversion](D:/workspace/gorti/gorti/cppsdk/src/dlc/FederateAmbassadorBridge.cpp:215)
constructs empty tags, fixed `RELIABLE` transport, order from timestamp presence,
invalid retraction handles, and empty supplemental information. Timed
[removal](D:/workspace/gorti/gorti/cppsdk/src/dlc/FederateAmbassadorBridge.cpp:267)
preserves the tag but still fabricates timing/retraction metadata.
[Stream messages](D:/workspace/gorti/gorti/proto/rti/v1/stream.proto:104)
do not carry the complete reflect/interaction contract. Adding fields only to
the final bridge cannot recover discarded information.

The new transportation requests and queries store/read real overrides, but the
inspected [update](D:/workspace/gorti/gorti/rti/internal/hla/object/update.go:605)
and [interaction](D:/workspace/gorti/gorti/rti/internal/hla/object/interaction.go:432)
fanout paths do not consume `AttributeTransportType`/`InteractionTransportType`;
their production readers are the transportation queries. Two order-change
[C++ methods](D:/workspace/gorti/gorti/cppsdk/src/dlc/RTIambassadorImpl.cpp:1478)
discard all parameters without validation or mutation.

Use distinct nonempty binary tags, mixed attribute defaults/overrides, two
interaction publishers and timed/untimed traffic; assert the actual callback
overload, sent/received order, transport and applicable retraction/producer/region
information. Cover unary, batched streams, confirmed-object, DDM and LocalLRC
paths. Best-effort is allowed to arrive reliably; deliberately losing packets
is not a required positive oracle. Q05/Q07 determine legal overloads and optional
metadata. N04 ownership-tag work is a separate in-flight contract.

### F05 / P1: New Transportation API Does Not Repair Legacy Product Routes

[New request validation](D:/workspace/gorti/gorti/rti/internal/hla/object/transportation_service.go:75)
checks defined attributes and current ownership before mutation. However,
[legacy attribute change](D:/workspace/gorti/gorti/rti/internal/hla/object/transport.go:77)
still authorizes `inst.owner`, and legacy interaction change accepts undefined or
unpublished classes. Those paths remain exposed by
[gRPC](D:/workspace/gorti/gorti/rti/internal/adapters/grpc/object.go:243) and
[Go SDK](D:/workspace/gorti/gorti/rti/pkg/federate/object.go:321), and write the same
override maps. A caller denied by the new API can still change state through the
old RPC; the current attribute owner can be wrongly rejected by the old route.
Unify authority/validation for all supported entry points or explicitly retire
the old product route. Test both after ownership transfer and for mixed-invalid
attribute sets. N03 is therefore corrected for deletion/new requests, not closed
across every exposed transportation route.

### F06 / P1: Local Delete Leaves Knowledge Unchanged

[C++ local delete](D:/workspace/gorti/gorti/cppsdk/src/dlc/RTIambassadorImpl.cpp:1044)
does nothing. The server's
[LocalDelete](D:/workspace/gorti/gorti/rti/internal/hla/object/request_update.go:28)
checks global existence, ignores the caller, and explicitly performs no mutation.
Go/Python forwarding that RPC is not a complete alternative implementation.
Other parts of the object registry already track known recipients; the missing
piece is the coherent per-federate transition and its consumers, not simply
another global map. With A/B/C, locally delete only B's view and verify B's
support queries and subsequent delivery/rediscovery while A/C retain their state.
Q05/Q09 govern exact rediscovery and queued-event consequences.

### F07 / P1: Active/Passive, Update Rates And Advisories Are Ignored

[Ordinary subscription](D:/workspace/gorti/gorti/cppsdk/src/dlc/RTIambassadorImpl.cpp:676)
and [DDM subscription](D:/workspace/gorti/gorti/cppsdk/src/dlc/RTIambassadorImpl.cpp:1636)
drop `active` and update-rate arguments. The declaration wire/domain has no
matching active/update-rate state. Six
[advisory toggle methods](D:/workspace/gorti/gorti/cppsdk/src/dlc/RTIambassadorImpl.cpp:2060)
are empty; the scope pair only writes `scope_advisory_recorded_`, which is not
read to gate delivery. The server
[declaration transitions](D:/workspace/gorti/gorti/rti/internal/hla/declaration/manager.go:263)
generate advisories based on subscriber presence, not the discarded mode.

Barrier each passive/active and enabled/disabled phase across publisher and
subscribers, including last-active removal, inherited subscriptions, object and
interaction variants, and DDM. Test real named update rates and invalid names.
Do not equate passive subscription with necessarily receiving no data; Q06
supplies its actual consequences and switch defaults.

### F08 / P1: Support Services And Automatic Resign Still Return Placeholders

The [automatic directive](D:/workspace/gorti/gorti/cppsdk/src/dlc/RTIambassadorImpl.cpp:1754)
getter always returns `NO_ACTION`; its setter is empty.
[Known-class/object-name/object-handle](D:/workspace/gorti/gorti/cppsdk/src/dlc/RTIambassadorImpl.cpp:1805)
queries always throw `NotConnected`, even after join.
[Update-rate queries](D:/workspace/gorti/gorti/cppsdk/src/dlc/RTIambassadorImpl.cpp:1849),
[available-dimension queries](D:/workspace/gorti/gorti/cppsdk/src/dlc/RTIambassadorImpl.cpp:1941),
dimension name/upper-bound/region dimension-set methods at 1965-1985, and
[getTimeFactory](D:/workspace/gorti/gorti/cppsdk/src/dlc/RTIambassadorImpl.cpp:2128)
also have unconditional failure paths. Existing class/attribute/interaction/
parameter/federate lookups and enum conversions are real and should not be
counted as missing.

The server's [object instance support service](D:/workspace/gorti/gorti/rti/internal/hla/support/service.go:43)
uses federation-global name/handle lookup without a caller knowledge parameter.
Wiring C++ directly to it would not by itself establish known-object semantics.
Test valid and wrong-class/unknown values in each service's permitted connection
state, local deletion, dimension limits, and the negotiated time factory. Test
automatic resign through abnormal disconnect/cleanup, not only a future setter/
getter round trip. Q01/Q02/Q09 supply defaults, triggers and legal states.

### F09 / P1: Accepted Immediate Callback Model Is Not Implemented

[connect](D:/workspace/gorti/gorti/cppsdk/src/dlc/RTIambassadorImpl.cpp:281)
writes `callback_model_`; no dispatch path reads it. The
[reader](D:/workspace/gorti/gorti/cppsdk/src/RtiAmbassador.cpp:2529) queues events,
and evoke/tick dispatches them. A client accepted with `HLA_IMMEDIATE` still
needs to evoke. The repaired synthetic-disable gate at
[drainOneSynthesized_](D:/workspace/gorti/gorti/cppsdk/src/dlc/RTIambassadorImpl.cpp:238)
must be credited: N08's earlier bypass is no longer the current implementation.

Fresh immediate-mode clients must receive a ready event without any evoke call;
fresh evoked-mode clients must not dispatch until evoked. Include synthetic and
stream events, disable/resume, reconnect, failed resign, callback re-entry,
thread identity and shutdown while queued. Review the unconditional synthetic-
before-stream draining at 2076-2104 for starvation and time-budget behavior.
Q01/Q09 decide mandatory models and exact timing; silently ignoring an accepted
mode is not evidence for either model.

### F10 / P1: Time Targets Can Poison Pending Requests; Full Time Oracle Missing

**Integration update:** the NaN poisoning defect below is now corrected and
covered by domain and actual-RPC regressions for all five advance modes. Current
`+Inf` sentinel acceptance is retained. The remaining time-state and independent
oracle findings in this section are still open.

[checkAdvanceTarget](D:/workspace/gorti/gorti/rti/internal/hla/time/lookahead.go:28)
only tests `requested < current`. A NaN target passes this comparison.
[dispatchAdvance](D:/workspace/gorti/gorti/rti/internal/hla/time/advance.go:249)
then records it and marks the request pending; the
[grant comparisons](D:/workspace/gorti/gorti/rti/internal/hla/time/advance.go:159)
cannot satisfy TAR/NMR comparisons against NaN. The
[gRPC TAR route](D:/workspace/gorti/gorti/rti/internal/adapters/grpc/time.go:161)
does not add numeric validation. Test raw-wire NaN for each primitive, assert
rejection before state changes and a valid subsequent request. Q07 controls the
standard error and treatment of infinity/final-time values; do not blindly ban
all special values as a substitute for that review.

The domain has real per-mode grant logic, TSO queues and delivery-failure
reservation handling. Nevertheless, [enable state](D:/workspace/gorti/gorti/rti/internal/hla/time/regulation.go:66)
changes immediately, while C++ synthesizes enable callbacks after a separate
[time query](D:/workspace/gorti/gorti/cppsdk/src/dlc/RTIambassadorImpl.cpp:1333).
There is no represented enable-pending handshake in that state. Float64-only
service narrowing and wire time do not establish integer-time support.

T15/T19 need independent, separate allowed-partial-order oracles for TAR, TARA,
NMR, NMRA and FQR: three regulators plus a nonregulator, equal-time traffic,
zero lookahead, late enable, pending-enable calls, lookahead changes, GALT/LITS,
failed grant delivery, asynchronous RO gating, and retraction before/after
delivery eligibility. FQR is not proved by a queue-empty single-federate walk.
Top-of-file historical comments differ from current grant predicates; neither
comments nor another RTI's single scheduling choice are normative evidence.

### F11 / P0: DDM Registration And Region Parameters Are Not End-To-End

Update: the [DDM registration milestone](ieee1516-2010-ddm-registration.md)
replaces the zero-handle RPC and all three SDK register-then-associate paths
with a validated registration transaction. It also corrects initial discovery
and ordinary/regional reflection composition. Per-call interaction and update
request regions below remain unresolved; the original observations are retained
for traceability.

[RegisterObjectInstanceWithRegions RPC](D:/workspace/gorti/gorti/rti/internal/adapters/grpc/ddm.go:296)
builds an unused map and returns success with handle `0` without registration
or association. [Go](D:/workspace/gorti/gorti/rti/pkg/federate/ddm.go:175) and
[Python](D:/workspace/gorti/gorti/pysdk/rti1516e/ddm.py:289) first register an ordinary
object, then call that no-op RPC and return the first handle: their requested
regions never become associations. The
[standard C++ overloads](D:/workspace/gorti/gorti/cppsdk/src/dlc/RTIambassadorImpl.cpp:1591)
instead register then call real association, but lack rollback if association
fails and can publish discovery before association is established.

Both [region interaction send](D:/workspace/gorti/gorti/rti/internal/adapters/grpc/ddm.go:445)
and [region update request](D:/workspace/gorti/gorti/rti/internal/adapters/grpc/ddm.go:484)
explicitly ignore the supplied region handles. C++ timed
[sendInteractionWithRegions](D:/workspace/gorti/gorti/cppsdk/src/dlc/RTIambassadorImpl.cpp:1699)
also discards the tag and returns an invalid retraction handle. Test two
disjoint regions through every binding, prove differing recipients/request
owners, and inject an invalid later binding into a multi-pair call. Assert no
leaked object/name/discovery on failure and real retraction identity where
applicable. Existing positive overlap tests cannot cover parameters never used.

### F12 / P1: DDM Range And Context Validation Are Incomplete

Update: numeric bounds now reject reversed and over-limit values before pending
state changes. Registration validates region creators and retained FOM dimension
context. The other association/subscription contexts and full pair-vector
semantics below remain open. See the DDM registration milestone for evidence.

[SetRangeBounds](D:/workspace/gorti/gorti/rti/internal/hla/ddm/manager.go:339)
checks region ownership and dimension membership but writes arbitrary bounds;
commit copies them without comparing lower/upper or the known dimension maximum.
C++ only rejects lower greater than upper. Direct Go/Python/wire callers bypass
even that check. Use max 100 and valid, inverted, over-limit and boundary ranges;
assert pending/committed state unchanged after each invalid request.

[DDM subscriptions](D:/workspace/gorti/gorti/rti/internal/hla/ddm/manager.go:425)
check region existence but do not validate the creator or class/attribute
dimension context. [Association](D:/workspace/gorti/gorti/rti/internal/hla/ddm/manager.go:491)
checks region ownership, not object existence/current attribute ownership or
FOM attribute context. The gRPC adapters do not supply the missing object/FOM
checks. C++ splits pair-vector operations into sequential RPCs, so an invalid
later pair can follow earlier mutations. Add foreign-region, unknown object/
attribute, inherited class, mixed-valid batch and duplicate-pair oracles. Q09
must settle endpoint/empty-set/default-region and exact batch semantics; existing
closed-open overlap comments do not settle them.

### F13 / P1: Attribute Update Requests Use Registration Owner

[Instance request](D:/workspace/gorti/gorti/rti/internal/hla/object/request_update.go:64)
routes every requested attribute to `inst.owner`, not its current owner.
[Class request](D:/workspace/gorti/gorti/rti/internal/hla/object/request_update.go:97)
matches exact class only and likewise uses the registrant; caller knowledge and
attribute validation are absent there. After A transfers one attribute to B,
requesting it still notifies A even though update authorization now requires B.
Split callbacks by authoritative attribute ownership and retain tags. Test mixed
ownership, inherited class, invalid attributes and failed callback delivery;
resolve precise recipient and exception rules through Q02/Q05. This object-
service task is distinct from N04 tag/result plumbing.

## Whole-Service Evidence Inventory

"Recorded pass" refers to the implementation document's earlier commands, not a
new pass of this changing tree. Test existence is useful evidence of an oracle
only after inspecting what it observes. Every row remains open at full scope.

| Service / matrix coverage | Implemented and tested subset | Unresolved groups for a whole-RTI gate |
|---|---|---|
| Federation management and synchronization; C03/C04, Q01-Q04 | Real lifecycle, federate names/types, generation fencing, six-action explicit resign; M24 contract tests check state, sparse handles, transferred delete privilege, name reuse and failed journal/reservation preservation. M8/M37 and synchronization tests cover registration, subsets and failed participants. C++ wire fixtures verify actual forwarding/failed-resign reader survival. | Additional FOM modules/custom MIM and class identity are main-owned. Complete per-service legal-state/exception and synchronization membership-change matrix, automatic resign, callback failures and save/restore integration. Do not infer all cleanup from enum tests. |
| Declaration; C08, Q06 | Publication/subscription storage, partial unpublish/unsubscribe, late discovery and start/stop/turn callbacks have domain/M37/C++ tests. | F07; coherent inherited declarations and advisory transition rules, passive/active distinction, update rates, switch defaults, DDM/default-region interactions, negative-state and delivery-failure cases. |
| Object management and transportation; C01/C02/C05, N03, Q05 | Registration/reservation, update/interaction, deletion, known-recipient removal, ordinary retraction and new transportation request/query contracts exist. Transportation integration tests check FOM defaults, current ownership, batch rejection and publisher-specific queries. | F04-F06/F13; actual transport/order effects, metadata, local knowledge, ownership-aware request routing, inherited class view and all name-reservation failure payloads. Distinguish new API from old Go/Python transport routes. |
| Ownership; C03/N01-N04/N07, Q02 | Acquisition precondition/coherent-publication tests and actual-server C++ path; explicit resign contracts; two-phase persistence regression. Additional query tests are present in the changing tree. | N04 worker integration is pending verification, not duplicated here. Audit all divest/cancel/release/query states, RTI-owned versus unowned outcomes, callbacks, automatic cleanup, failure atomicity and Save/Restore admission. Query test presence is not a fresh pass. |
| Time; C10/C05, Q01/Q07 | TAR/TARA/NMR/NMRA/FQR functions; queues, GALT/LITS, lookahead and asynchronous delivery. Domain delivery-failure tests assert TSO requeue, reservation-before-WAL, and committed time before grant visibility; M3/M7/M21/M22/M25/M37/M38 supply scenarios. | F09/F10; primitive-specific normative oracles, pending enable, exact offered time representations/factory, zero/equal boundaries, retraction states, stalled/failed delivery, late membership, persistence and every transport path. |
| Save/restore; C07/N05-N07/N08, Q04 | Explicit begun/progress/status vectors and participant failure; reserve-before-load, prepared decode/rollback and empty-versus-absent snapshots. Unit/wire/persistence tests inspect actual state; C++ actual-server tests inspect begun/status protocol. | F01-F03; complete RTI state, isolation from normal services, restored identities, scheduled time, failed initiation/delivery, missing participants, failure reasons and legal service-state matrix. |
| DDM; N11/C08, Q09 | Regions, commit, overlap, association/unassociation, deletion-in-use and scoped fanout exist with domain and M10/M23/C++ fixtures. | F11/F12; actual SDK region registration, per-call send/request regions, creator/FOM/ownership validation, pair atomicity, default/inherited region semantics, active/update-rate behavior, timed tags/retraction and support lookups. |
| Support and callback management; C09/N08/N09, Q01/Q09 | Several FOM/federate lookups, enum conversions, handle decode, real evoked stream delivery and repaired synthetic-disable behavior. M25/M35 and C++ runtime/status tests cover subsets. | F08/F09; unconditional lookup/factory failures, per-federate known class, automatic-resign policy, immediate delivery, re-entry/thread/teardown contracts, callback budgets, independent handle encoding/ABI checks. |
| MOM; C06/N10, Q08 | Four-byte switches, validate-before-mutate, target-aware adjustment and independent framed uint64 vectors. MOM unit tests cover sender/target and malformed values. | Main-owned standard-MIM identity/inheritance must pass production positive tests. Default dispatcher registers switch handlers and four counter-request families, not a mapped complete MIM catalog. Counter handlers still query `sender` at `handlers_request.go:87`; report recipient policy, service/exception reporting exclusions, periodic/requested attributes and failure reporting remain open. |
| FOM/MIM/OMT, time and data encodings; C04/C10/N12, Q01/Q03/Q07 | N12 metadata vocabulary/structure/reference correction has recorded parser and independent XSD evidence; official MIM bytes match. Several encoding vectors/round trips exist. | Main's identity/merge work; complete structural/semantic/cardinality/namespace/modular inventory, stable handles/caches after extension, custom MIM constraints, dimension/update-rate/order/transport declarations. Independently authored nested alignment/encoding vectors; no full OMT inference from XSD or codec round trips. |
| Standard binding and product profiles; C01-C10, Q01/Q10 | C++ declarations, exception classes, local gRPC contract endpoints and product-library runtime tests; Go/Python product SDK scenarios. | Official-header client compilation, link/load/runtime and ABI provenance for each claimed compiler/platform; all overloads/parameters/exceptions/callback models/time factories. Java/WSDL applicability unknown; no implementation evidence asserted. Go/Python cross-binding tests are not independent RTIs. |
| Framework/rules and federation/federate responsibilities; Q01/Q03/Q10 | Engineering interfaces and model handling provide RTI evidence only. | Obtain RTI-specific rule obligations from all three texts. Federates save application state and obey service-use rules; federation authors supply models/agreements. Neither arbitrary federate correctness nor federation semantic interoperability follows from an RTI pass. |

## C/N Reconciliation With Concurrent Work

| Existing IDs | Current audit disposition |
|---|---|
| C01, N03 | Four standard transportation methods are now present and new validation is tested historically. Actual delivery and legacy-route authority remain F04/F05. Delete privilege correction is present; do not repeat the old global-delete-owner finding. |
| C02, C05, C08, C09, C10 | Still open through F04/F06-F10, F11/F12. Exposing time headers or support declarations is not implementation. |
| C03, N01 | Explicit resign composition and sparse-handle enumeration are corrected in source with stateful M24 tests and recorded runs. Automatic resign and remaining ordinary ownership obligations stay open. |
| N02 | Acquisition validation/coherent publication fixes and their recorded tests are real. Query tests and further ownership edits appearing now require new settled-tree evidence. |
| N04 | `ConfirmDivestitureWithTag` and `DivestIfWantedWithResult` are appearing in the current worker's source. This audit records in-flight work, not a remaining unchanged old implementation or verified closure. |
| C04, C06, N10 | Main owns FOM/MIM identity/inherited lookup. Switch decoder/target unit fixes are credited. Production success and full MOM behavior remain unclosed until integrated tests, regardless of a prior fail-closed regression pass. |
| C07, N05-N07 | Reservation, operation serialization, prepared decode/rollback and two-phase snapshot repairs are present and have recorded bounded tests. Full-state restore/isolation/scheduling/reasons remain F01-F03. |
| N08 | Synthetic-disable bypass repaired; tests inspect disabled/resumed status callbacks. Do not reopen the obsolete sequential bypass. Cross-queue ordering/concurrency and N09 remain separate. |
| N09, N11 | Still source-confirmed, F09/F12. |
| N12 | Bounded metadata parsing correction credited; no implication of full FOM/OMT compliance. |

## Missing Or Weak Oracles

| Current evidence | What it misses / necessary replacement |
|---|---|
| [Local-delete fixture](D:/workspace/gorti/gorti/cppsdk/tests/dlc/conformance/om_local_delete/federate_subscriber.cpp:109) | Calls delete and only watches for absence of remove. A no-op passes. Query local knowledge and drive a subsequent event/rediscovery transition while a peer remains unaffected. |
| [Active/passive subscriber](D:/workspace/gorti/gorti/cppsdk/tests/dlc/conformance/dm_pub_sub_active_passive/federate_subscriber.cpp:54) and publisher at 78 | Separate traces/sleeps cannot tell whether the one start callback came from the passive or active call. Use shared acknowledged phases and assert both presence and absence in each. |
| [Save/restore fixture](D:/workspace/gorti/gorti/cppsdk/tests/dlc/conformance/fm_save_restore_roundtrip/federate.cpp:118) | Registers/updates before saving but only observes restore callbacks afterward. New `test_save_restore_dlc_integration.cpp:73` improves participant/status assertions, not complete RTI restored-state continuation. |
| [Callback bridge tests](D:/workspace/gorti/gorti/cppsdk/tests/dlc/conformance/_runtime/test_callback_bridge.cpp:66) | Selected callbacks ignore the tag and test conversion without a server. End-to-end distinguish nonempty source tags, all callback overloads and metadata rather than accept synthesized defaults. Recheck ownership converter expectations after N04. |
| [DDM Go surface tests](D:/workspace/gorti/gorti/rti/tests/integration/spec/M23/ddm_go_sdk_test.go:37) and `ddm_missing_test.go:33` | Reflection proves method names/types exist, not that region inputs affect recipients. Test actual registration/association and negative mixed requests through wire/SDK. |
| [Time walker](D:/workspace/gorti/gorti/cppsdk/tests/dlc/conformance/tm_tar_tara_fqr_nmra/federate_walker.cpp:84) | Single-federate grants with explicit evokes do not distinguish contested queue semantics, enable-pending or immediate callbacks. Add T15/T19/T20 adversarial multi-federate scenarios. |
| [Handle/RO normalization](D:/workspace/gorti/gorti/cppsdk/tests/dlc/conformance/_harness/normalize.py:63) | Replaces every numeric handle with the same placeholder and sorts RO buckets. This cannot prove identity correlations or any ordering erased by normalization. Use a bijective per-run handle mapping and preserve raw causal assertions. |
| [Fair-comparison projection](D:/workspace/gorti/gorti/verification/gorti-go-fair/projection.go:20) | Workload summaries omit tags, exception identity, supplemental data and ownership transitions. Keep them as benchmark/differential summaries, not whole-service oracles. |
| [Fixture expectations](D:/workspace/gorti/gorti/cppsdk/tests/dlc/conformance/EXPECTED_VERDICTS.txt:21) | `FULL` means fixture-local trace equality; two explicit skipped workflows still need applicable-case evidence. A changed golden requires independent justification, not automatic promotion. |

For every applicable obligation, require a real-server standard-binding case
where possible, positive/negative state/input cases, observable post-state,
callback identity/payload, exception identity and relevant failure/concurrency
coverage. A fake gRPC endpoint is valuable for exact binding conversion but does
not test the production service. Domain tests are valuable for deterministic
interleavings but do not prove binding delivery. Both layers are needed.

## Current Gate Audit

Executed here, from `D:/workspace/gorti/gorti`:

```powershell
python -B scripts/ieee2010_quality_gate.py --claim full-2010 --check
```

Observed at `2026-09-19T02:34:53Z`: exit **1**, `verdict=FAIL`, `mode=check`,
`commands_started=0`, `conformance_established=false`, `certification=false`.
Manifest canonical-JSON hash:
`c6a47a19f8f0c606cb4d2cf4e2f9acc16747bf456de2d217318139851653f020`.
Failures include incomplete normative inventory, missing clause/review evidence,
missing independent campaign evidence and unresolved production-MOM/Q01 rows.
This is the expected honest readiness result, not a runtime test failure.

The [manifest](D:/workspace/gorti/gorti/engineering/verification/ieee1516-2010-evidence.json:5)
selects acquisition, MOM switches/handles, FDD metadata, descriptor and
architecture regressions on the current Go host. Time, Save/Restore, DDM,
support, declaration/callback-model and C++/Python campaigns are not selected.
`--claim full-2010` changes prerequisites; it does not discover new service tests.
The script checks hashes and declared review/source IDs, but cannot authenticate
reviewer independence, snapshot identity, oracle correctness or inventory truth.
Schema v1 also requires executable tests for applicable rows and has no reviewed
inspection-only evidence form; a whole-standard campaign needs that extension.
Keep engineering guards distinct from normative obligations rather than invent
IEEE clauses for architecture/descriptor tests to satisfy the current schema.

The separate [CI wrapper](D:/workspace/gorti/gorti/scripts/ci-gates.sh:63)
explicitly runs only four runtime binaries, omitting the newer federation,
transportation and bridge-error runtime tests from that stage. Its lockfile
compile uses repository headers; its sweep permits explicit SKIP and documented
PARTIAL expectations. It is useful engineering regression infrastructure, not
G0-G4 completion. No automatic CI change is made or proposed as already enabled.

There is also an inverted-result trap: the current
[fixture CMake registration](D:/workspace/gorti/gorti/cppsdk/tests/dlc/conformance/CMakeLists.txt:75)
sets `WILL_FAIL TRUE` for the general fixture drivers, and
[lockfile CMake](D:/workspace/gorti/gorti/cppsdk/tests/dlc/lockfile/CMakeLists.txt:59)
does the same for compile checks. Those legacy expected-failure registrations
are separate from the positive `_runtime` tests and shell sweep. A generic
`ctest` pass can therefore include expected failures. The candidate commands
below deliberately select positive runtime/integration tests and use the shell
sweep for fixture traces; a whole-RTI aggregator must inspect test properties and
real case verdicts, not accept expected failure as satisfied behavior.

## Candidate Whole-RTI Campaign

These commands are **not executed by this audit**. They are a reproducible
starting campaign over existing tests, not an invented ready-made full suite.
Missing F01-F13/T01-T22 cases and the normative inventory must be implemented,
reviewed and added before a whole-RTI PASS is possible.

Use a frozen content-addressed copy of the final dirty/untracked source, or a
settled release commit. Do not test moving worker outputs as a release. Record
compiler/runtime/dependency versions, generated bindings, binary hashes,
FOM/MIM inputs, journal/save settings, transport paths, callback models and time
factories. Resolve each `U` to `M`, `C(trigger)` or justified `NA`, and identify
RTI versus federate versus federation responsibility.

Suggested initial profile is Linux/WSL C++ plus Go/Python product bindings,
without asserting that this is a normatively sufficient binding scope. Windows
native and every other advertised profile need separate runs, not an inference
from WSL. In a dedicated disposable test host/copy, with the repository at the
shown path and dependencies already installed:

```bash
cd /mnt/d/workspace/gorti/gorti
export EVIDENCE=/tmp/gorti-whole-rti-2010-candidate
mkdir -p "$EVIDENCE"
set -o pipefail
git rev-parse HEAD > "$EVIDENCE/head.txt"
git status --short > "$EVIDENCE/status.txt"
git diff --binary HEAD > "$EVIDENCE/tracked.patch"
git ls-files -co --exclude-standard -z | sort -zu | xargs -0 sha256sum > "$EVIDENCE/source-files.sha256"
go version
python3 --version
cmake --version
c++ --version

# Prerequisites only: currently expected exit 1, no runtime tests.
python3 -B scripts/ieee2010_quality_gate.py --claim full-2010 --check > "$EVIDENCE/full-readiness.json"

# Existing bounded gate: only the named Go regressions, not whole RTI.
python3 -B scripts/ieee2010_quality_gate.py --run > "$EVIDENCE/bounded-gate.json"

# Current source-tree regression and race baselines, with Go test cache disabled.
go test -json -count=1 -timeout=300s ./rti/... ./tests/... ./verification/... ./examples/... > "$EVIDENCE/go.jsonl" 2> "$EVIDENCE/go.stderr"
go test -race -json -count=1 -timeout=300s ./rti/... > "$EVIDENCE/go-race.jsonl" 2> "$EVIDENCE/go-race.stderr"

# Production server and product-library/fixture builds.
go build -o bin/rtid ./rti/cmd/rtid
export RTID_BINARY="$PWD/bin/rtid"
cmake -S cppsdk -B cppsdk/build -DCMAKE_BUILD_TYPE=Release -DDLC_CONFORMANCE_RED_EXPECTED=ON
cmake --build cppsdk/build --parallel
cmake --build cppsdk/build --target dlc_conformance --parallel
ctest --test-dir cppsdk/build --show-only=json-v1 > "$EVIDENCE/ctest-inventory.json"
ctest --test-dir cppsdk/build -R '^conformance__runtime_' --no-tests=error --output-on-failure --output-junit "$EVIDENCE/cpp-runtime.xml"
ctest --test-dir cppsdk/build -R '_integration$' --no-tests=error --output-on-failure --output-junit "$EVIDENCE/cpp-integration.xml"

# Existing fixture traces. Retain each printed run directory and raw role logs.
bash scripts/ci-gates.sh sweep 2>&1 | tee "$EVIDENCE/cpp-sweep.log"

# Requires installed pysdk[dev] and stubs matching the frozen protocol.
python3 -B -m pytest pysdk/tests -ra --junitxml="$EVIDENCE/python-sdk.xml"
python3 -B -m pytest tests/conformance/rti/ivct-subset -v -ra --junitxml="$EVIDENCE/python-ivct-subset.xml"
```

Capture **each command's exit code**, stdout/stderr and enumerated case results;
the command block is not an automated fail-closed aggregator. Do not continue
to interpret results after a failed build as if they used fresh binaries.
Use a new evidence directory per immutable candidate. The tracked patch and
source hash list do not themselves archive untracked bytes: retain the actual
frozen snapshot plus generated sources and binary/toolchain artifacts. If the
local build uses Conan, provide its pinned toolchain file to CMake as described
by the existing wrapper. The explicit source-tree Go roots avoid the known
unrelated root-level pytest temporary-directory ACL enumeration problem.

**Isolation warning:**
[run_fixture.sh:126](D:/workspace/gorti/gorti/cppsdk/tests/dlc/conformance/_harness/run_fixture.sh:126)
kills processes by `rtid --listen`/`rtid -listen` pattern, not exclusively its own
PID, and several fixtures hard-code port 8080. Do not run the sweep beside the
parallel implementation workers or another active RTI. A different `--port`
alone does not make it isolated. Omit `--capture`, which writes fixture captures
back into the source tree. The two SKIP workflows require their README-specific
manual choreography or replacement harnesses and explicit verdicts.

For a narrowed post-fix iteration, existing package commands are:

```bash
go test -json -count=1 -timeout=120s ./rti/internal/hla/time ./rti/tests/integration/spec/M3 ./rti/tests/integration/spec/M7 ./rti/tests/integration/spec/M21 ./rti/tests/integration/spec/M22 ./rti/tests/integration/spec/M38
go test -json -count=1 -timeout=120s ./rti/internal/hla/save_restore ./rti/tests/unit/save_restore ./rti/internal/adapters/savebundle ./rti/tests/integration/spec/M9 ./rti/tests/integration/spec/M24
go test -json -count=1 -timeout=120s ./rti/internal/hla/ddm ./rti/internal/hla/object ./rti/internal/hla/declaration ./rti/internal/adapters/grpc ./rti/tests/integration/spec/M23 ./rti/tests/integration/spec/M25 ./rti/tests/integration/transportation
```

G2 additionally needs an **independently authored executable client** built with
the official `cpp-api/cpp/src` headers first and with repository replacement
headers excluded from its compile search. The current
[transportation probe](D:/workspace/gorti/gorti/cppsdk/tests/dlc/conformance/_runtime/transportation_api_probe.cpp:1)
contains four compile-time signatures, and its CMake target links the product
headers; it is not that executable campaign. Define a separate client build,
retain include/link maps, factory/load/ABI results, and all tested overloads.
Official historical headers may need a compatible language/toolchain profile;
any adaptation must be reviewed and preserved, never silently replaced with
gorti headers. No nonexistent independent-runner command is claimed available.

A future aggregator should join the reviewed obligation inventory to exact
executed case IDs and inspection records across all required profiles. Missing,
failed, skipped, xfailed, stale or unexecuted applicable cases must block the
claimed scope. A raw process exit 0 cannot override them. Mandatory branches of
conditional cases need an explicit trigger result, and zero-case selections
must fail. Preserve tags, handles, state and allowed partial orders before any
display-only normalization. Add mutation/no-op probes to review the oracle: a
discarded active flag, region list, tag, local delete or restore application must
make its corresponding test fail.

## Normative Questions Still Open

| Matrix gate | Closure needed, independently of a code fix |
|---|---|
| Q01 | Authorized 2010 conformance provisions/corrections; permitted RTI scope, bindings/platforms, callback models, time factories and mandatory/conditional classifications. |
| Q02 | Ownership/resign preconditions and order, current authority, tag source, unowned/RTI-owned distinction, cancellation/release semantics, automatic disconnect cleanup and errors. |
| Q03 | RTI versus model-author OMT responsibilities, allowed modular/MIM changes, inheritance and namespace/encoding/model constraints. |
| Q04 | Scheduled saves, permitted calls/transitions, complete saved state, restored identities, participants, failure/abort semantics and generation/recreation policy. |
| Q05 | Local knowledge/rediscovery, order/transport changes, pending confirmations, callback overloads and metadata, retraction eligibility, update-request recipients. |
| Q06 | Active/passive and inherited declarations, switches and edge transitions, DDM interactions and update-rate selection. |
| Q07 | Each time primitive's allowed order/bounds, pending enables, GALT/LITS, special values, lookahead, asynchronous delivery and retraction. |
| Q08 | Full applicable MIM behavior, target/recipient rules, periodic/requested attributes, counters, reporting exclusions and failure channels. |
| Q09 | Per-service legal connection states, support knowledge/normalization, DDM endpoints/context/default regions, callback timing/threading/re-entry and time budgets. |
| Q10 | Scope and subject of independent verification/interoperability, and actual external assessment or certification evidence if claimed. |

## Independent Test Subject And External Evidence

Primary web pages were selectively rechecked on 2026-09-19. The upstream
[IVCT framework](https://github.com/IVCTool/IVCT_Framework) describes testing HLA
federates for interoperability; the
[HLA_BASE suite](https://github.com/IVCTool/TS_HLA_BASE) checks declarations,
service use, objects/interactions and encoding against the federate's CS/SOM.
Thus the RTI can be the test environment rather than the assessed subject.
Running that suite on gorti does not automatically make gorti the subject of a
complete RTI conformance assessment. A custom RTI-directed suite needs its own
obligation mapping, independently reviewed oracles and provider/binding proof.

The local Python `ivct-subset` is not upstream IVCT. Its
[README](D:/workspace/gorti/gorti/tests/conformance/rti/ivct-subset/README.md:24)
explicitly excludes interactions, DDM, MOM, save/restore and external-RTI
interoperability, and documents a conditional late-discovery xfail. Treat its
counts as manifest context, not proof of covered or uncovered standard clauses.

Independently written federates on gorti can test gorti as the subject when the
oracle checks its services. Porting the same client to another named RTI gives
portability/differential evidence; it is not simultaneous RTI wire
interoperability. Multiple gorti SDKs share a backend. Independent test authorship,
independent review, external organizational assessment and certification are
different claims. Match/disagreement with a reference RTI must be interpreted
against permitted standard behavior, not used as a normative oracle by itself.

The current [IEEE ICAP page](https://standards.ieee.org/products-programs/icap/)
describes program-specific plans, laboratories and certificate issuance. These
limited checks did **not establish an available complete 2010 RTI certification
offering**; they do not prove that none exists. Obtain written assessor/program
confirmation naming the RTI subject, edition, service/binding/options coverage,
exclusions, independence and report/certificate before adopting such a gate.
The quality-gates document's L0-L4/G0-G5 distinctions remain appropriate.

## Candidate Next Parallel Code Tasks

No task is dispatched and no ownership of another worker's files is assumed.
Agree domain/wire contracts first where shared C++/protobuf files would overlap.

| Priority / proposed task | Bounded acceptance and coordination |
|---|---|
| P0: Complete RTI persistence and operation admission | F01/F02: inventory every live-state owner; snapshot/replay a coherent state and reconnect identity contract. Assert post-restore object/name/ownership/DDM/time continuation and concurrent-service isolation. Coordinate main's FOM identity and N04 snapshot fields. |
| P0: DDM real registration and scoped operations | F11/F12: one validated object/association commit; wire/Go/Python/C++ agree on returned handle and region effects. Distinct-region recipients and invalid-later-pair rollback tests must fail the current stub. |
| P0: Delivery metadata and order/transport pipeline | F04/F05: common callback envelope plus validated shared old/new transport routes; preserve tags and legal metadata through all paths. Coordinate N04 fields rather than editing ownership plumbing independently. |
| P1: Local knowledge and object request routing | F06/F13: local-delete transition consumed by support/delivery; current attribute-owner request callbacks, inherited visibility and mixed ownership tests. Reuse registry knowledge instead of adding a competing cache. |
| P1: Declaration modes/advisories/update rates | F07: persist mode/rate/switch state and implement reviewed transitions; shared phase barriers prove both positive and negative callbacks. Depends on main's inherited FOM metadata. |
| P1: Time admission and protocol | F10: first reject non-orderable targets without poisoning state; then pending-enable/factory and each advance/retraction oracle. Keep scheduling questions explicitly Q07 until text review. |
| P1: C++ support/callback lifecycle | F08/F09: real lookups/time factory, agreed immediate-mode behavior, automatic-resign lifecycle and callback-thread/shutdown tests. Split domain and binding work to avoid concurrent edits to the same ambassador file. |
| P1: Save timing/failure metadata | F03: scheduled coordination and end-to-end reason enums with failed-delivery state tests; coordinate the persistence task, not a second save state machine. |
| P1: MOM catalog and independent oracle/gate work | After main's identity fix, real-MIM success for every supported request/attribute family, target/recipient/reporting tests; independently authored official-API clients and full requirement/evidence aggregation. This can proceed without changing production CI policy. |

Close an engineering defect only after its negative regression fails the old
implementation and its positive/stateful tests pass the integrated candidate.
Close a normative row only after applicability and independent oracle review are
also complete. Neither parallel worker progress nor this audit is a full-RTI
completion claim.
