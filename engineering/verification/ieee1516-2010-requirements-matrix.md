# IEEE 1516-2010 Requirements and Verification Matrix

The [2026-10-02 whole-source requirement review](ieee1516-2010-requirements-review-20261002.md)
adds source-located service decompositions and separates recorded test passes
from requirement qualification. Exhaustive obligation and semantic coverage
remain open; the dated historical rows below are preserved.

The [2026-10-02 defect correction increment](ieee1516-2010-defect-corrections-20261002.md)
records current Python/Java fixes, default-MIM correction and reviewed test
input/oracle updates. It retains the failed assessment below rather than
retroactively converting it to a pass.

The [2026-10-02 internal assessment](ieee1516-2010-internal-assessment-20261002.md)
adds fresh broad execution and reproduced Python/Java failures. Its separate
normative review distinguishes RTI responsibilities from user-federate/model
obligations and leaves exhaustive coverage open. Historical passing subsets
below do not establish a passing full internal assessment.

The [2026-10-01 traceability increment](ieee1516-2010-traceability-20261001.md)
adds a separate source-located atomic inventory, machine-checked implementation
and literal-test links, fresh bounded execution and internal black-box evidence.
The historical defect rows below are not that exhaustive normative inventory.

Independent review: 2026-09-18. Target: IEEE 1516-2010, IEEE 1516.1-2010,
and IEEE 1516.2-2010, not their 2025 successors.

## Review boundary and verdict

### 2026-09-21 Normative-Source Checkpoint

All three 2010 redline publications are now available. The
[source-based review](ieee1516-2010-normative-review-20260921.md) records hashes,
clause/PDF-page locators, responsibility decisions and the new implementation
review. Its current-text review supersedes the earlier assumption that full
normative text was unavailable. In particular, 1516.1 Clause 13.2 requires at
least one standard language API, not all three published mappings, while Clause
13 restricts use of the "HLA Conforming" label. These scope decisions do not
close every obligation in Q01-Q10. Earlier dated defects and executions below
remain historical; consult the new review and
[batch-3 record](ieee1516-2010-batch3-20260920.md) for subsequent changes.

### Earlier Remaining-Service Checkpoint

The [remaining-service implementation record](ieee1516-2010-remaining-services-20260920.md)
is the earlier implementation and execution summary. It covers authenticated
automatic resign, restart restore across generations, deferred deletion,
ownership denial, atomic name release, LocalLRC reinitialization, callback
execution and MOM measurement boundaries. Earlier dated entries remain the
history of this matrix, not a list of all current defects. Complete normative
groups remain open until their requirements and evidence have been reviewed.

### 2026-09-20 Live-Service Checkpoint

The [live-service implementation record](ieee1516-2010-live-services-20260920.md)
updates dynamic FOM joins, timed save and identity restoration, update-rate
advisories, MOM sources, callback metadata and retraction-ID safety. Its bounded
gate passes 81 groups with 369 required test names. Earlier observations below
are retained as audit history; implementation corrections do not by themselves
close an entire normative requirement group. Full-2010 readiness still fails
for missing normative review and independent evidence.

### 2026-09-19 Checkpoint

The [whole-RTI audit](ieee1516-2010-full-rti-audit.md) supersedes any inference
that the earlier bounded corrections complete a service group. C06's production
class-identity/inherited-target defect is repaired for service/exception
reporting adjustments, including actual ObjectService RPC routing. N02 now also
has caller-scoped ownership-query rejection and read-only-state tests. N04 has
binary-tag and atomic divest-result corrections. N12 retains XML ancestry,
validates branch-local inheritance, and coalesces repeated module containers.
C10 now rejects NaN targets in all five advance modes before state mutation;
the complete time-management oracle remains open.
See the [implementation checkpoint](ieee1516-2010-implementation.md) and executable
manifest for the exact tested scope.

The subsequent [same-roster checkpoint milestone](ieee1516-2010-checkpoint-state.md)
adds the missing object/declaration/time snapshots and MOM runtime fields,
production service admission, deferred commit and terminal callback reservation.
Its real-server tests verify restored behavior rather than only protocol
callbacks. Changed identities and incomplete bundles are rejected. Scheduled
saves, identity rebinding and unread transport/callback state remain open; this
does not close the complete C07/Q04 service group.

The [DDM registration milestone](ieee1516-2010-ddm-registration.md) implements
one-call region registration across the server and Go/Python/C++ bindings,
validates registration context before mutation, preserves FOM dimension
references, and rejects reversed or over-limit bounds without changing pending
state. Production routing, failure atomicity and same-roster association restore
tests pass, as do the final Windows regression and Linux race campaigns.
The local/manual gate now requires 215 tests in 37 command groups. This partly
corrects audit F11/F12 and the N11 bounds defect, not the complete DDM service.
Per-call region sends/requests, the remaining ownership/context validation
matrix and Q09 endpoint/template semantics remain open.

MOM counter reports remain open: their `HLAcount` response parameter does not
match the standard MIM report declarations. Save/Restore beyond the bounded
checkpoint profile, DDM region-aware operations, callback metadata, local delete,
subscription modes/advisories, support placeholders and callback-model execution
also remain open. Normative Q01-Q10 are not closed by these engineering tests.

This is a prioritized completion and test-sufficiency matrix, not a completed
clause inventory or a conformance declaration. The full normative texts and
applicable corrections were not available. API declarations, the official MIM,
and the FDD schema establish useful obligations but cannot resolve all service
semantics, applicability, or conformance provisions.

Reviewed the assessment, implementation progress, production paths, and selected
test oracles. HEAD observed during review was
`3cf610c9bcd7953c91e1e38cdf9dd05155bf9e13`; the assessment names the older
`067082f` baseline. The working tree contains extensive pre-existing changes and
concurrent workers. Findings below describe the functions as read, not an
immutable release. Line anchors are observation aids; function names remain the
primary locator. A concurrent correction needs its own verification evidence.

The initial review wrote only this document and executed no runtime test
campaign. Its newly confirmed defects were source-confirmed, not claimed
reproduced by new executable tests. A subsequent user-authorized N12 parser
implementation and its executed tests are recorded in the N12 checkpoint below;
that follow-up changes only parser production code, parser tests/fixture, and this
matrix. No commits were made. Previously reported passing commands in the
progress document remain historical evidence, not results of this review.

Completion cannot be inferred from the current `FULL` fixture labels. In
particular, several inspected oracles do not distinguish an implemented service
from a no-op, ignore a parameter under test, or do not check restored state.

## Evidence classes and ownership

- **A: API evidence.** Official declarations establish names, overloads,
  parameters, callback shapes, enums, and declared exceptions. Their exception
  lists alone do not establish triggering conditions or precedence.
- **M: MIM/schema evidence.** Official XML establishes its explicit declarations,
  encodings, and embedded semantics. Schema validity is not complete OMT or
  federation correctness. The available FDD XSD explicitly describes itself as
  a relaxed version of the OMT schema, not the full OMT schema.
- **I: implementation evidence.** Source shows ignored inputs, lost state,
  missing validation, races, and test blind spots. An implementation consistency
  defect can be confirmed without inventing an IEEE clause.
- **N: normative-text gate.** The relevant full clauses, applicability and
  corrections must be attached before the associated standard assertion closes.
  `Q01`-`Q10` below identify the unresolved questions.

Priority P0 means repair state corruption, wrong authority, or misleading
acceptance before closing the current work. P1 means remaining service/API
completion. P2 means independent release evidence. These are engineering
priorities, not claims about IEEE severity.

Owners below follow the current work allocation: **Main** owns resign/ownership;
**Transport/C++** owns transportation and binding work; **Save** owns save/restore
server work. **Unassigned** explicitly means the roadmap still needs an owner;
it does not assign another worker's files. Cross-owner rows need an agreed
domain/wire/binding contract before either side declares completion.

The RTI owns applicable service state, validation, callback delivery, support,
and its persistence state. Federates own application state, correct use of
services, their save/restore participation, and applicable federate rules.
Federation authors own applicable FOM/model declarations and agreements. An RTI
test cannot establish compliance of arbitrary federates or federations.

The supported binding/platform claim must be explicit. The distribution contains
C++, Java, and WSDL artifacts; it does not by itself prove that every RTI must
implement every binding. Go/Python are product bindings, not evidence that a
standard binding is complete. Resolve the binding obligation through Q01 rather
than declaring either universal Java/WSDL requirements or universal exemptions.

## C01-C10 Completion Matrix

Acceptance IDs refer to the finite scenarios below. All rows remain open at the
service-group level, including rows with corrected sub-defects.

| ID / priority / owner | Observed implementation and remaining obligation | Dependencies | Bounded acceptance / evidence strength / normative gate |
|---|---|---|---|
| C01 / P0 / Transport/C++ | Official `RTIambassador.h` declares four request/query transportation methods absent from the initially inspected repository header. Existing `object/transport.go` records overrides; no inspected update/send consumer reads its accessors. `ChangeAttributeTransportType` checks original instance owner, not attribute owner; interaction change permits unpublished/undefined classes. Adding methods and confirmations alone cannot close transport semantics. | Main's ownership authority; FOM defaults; C05 metadata; publication validation; Q05. | T01, T02, T11. A+I. Exact pending-change states, completion timing, default/query behavior, and transport guarantees need N. Best-effort is allowed to arrive reliably; packet loss is not a required positive oracle. |
| C02 / P0 / Transport/C++ plus Unassigned object/declaration owner | `RTIambassadorImpl.cpp::changeAttributeOrderType`, `changeInteractionOrderType`, and `localDeleteObjectInstance` are no-ops in the inspected code. Per-federate object knowledge must support local deletion and object support queries, not just a global registry. | C03 attribute authority; C05 ordering metadata; C09 known-object lookup; declaration/DDM discovery. | T03, T04. A+I; N for rediscovery, queued messages, legal order transitions, and locally known class rules (Q05, Q09). |
| C03 / P0 / Main | Enum forwarding and failed-resign reader preservation are corrected in the progress record and covered by the local gRPC contract fixture. Production `run.go` still showed unconditional post-resign release and an action hook unable to return validation failure. New N01-N04 expose sparse-handle deletion, invalid ownership, authority, and atomic-result problems. | Shared operation admission/locking; delete privilege; ownership pending state; callbacks; Save's N07 snapshot contract. | T05-T08, T14. A+I; all six action preconditions/dispositions and permitted callback consequences require Q02. An enum-acceptance test is not cleanup coverage. |
| C04 / P1 / Unassigned FM/FOM; Transport/C++ binding | Both join overloads still ignore `additionalFomModules`. Federate name/type forwarding and default/float64 factory rejection are corrected sub-defects. Create-with-MIM and module merge behavior still need full-path coverage. | Parser/module repository; stable handles; C10 factory negotiation; Save bundle FOM identity; MOM module reporting. | T09, T10. A+M+I; Q01/Q03 for valid module extension, consistency, custom MIM constraints and required factories. Rejecting unsupported factories does not prove all mandatory factories exist. |
| C05 / P0 / Transport/C++ plus delivery owner | `FederateAmbassadorBridge.cpp:215-285` synthesizes empty reflect/interaction tags, fixed transport/order, invalid retraction handles, and empty supplemental info. Removal preserves tag but still fabricates timed metadata. Ownership acquisition/confirm tags are separately dropped (N04). | Common metadata contract across unary, stream, timed queue, LocalLRC, DDM, persistence and every advertised SDK; C01/C02/C08. | T11, T08, T15. A+I; Q05/Q07 for overload selection, retraction eligibility, sent versus received order, and optional supplemental fields. A timestamp's presence alone is not an overload oracle. |
| C06 / P1 / Unassigned MOM | Four-byte `HLAswitch` decoder and validate-before-mutate correction are present. `rti/tests/unit/mom/switches_test.go` covers malformed lengths/enumerators, omission, empty values and combined-switch atomicity. This closes the identified decoder defect only. N10 shows sender/target confusion; the default dispatcher registers switches and counter requests, not a demonstrated complete MOM catalog. | Real MIM/FOM parameter resolution; encoded handle contract; C05 supplemental metadata; service reporting/exception policy. | T16. M+I; Q08 for full MOM applicability, targeting/error/report behavior. `HLAreportingState` is declared `HLAboolean`, not `HLAswitch`, even though the inspected standard encodings both use four-byte 0/1. |
| C07 / P0 / Save and Transport/C++ | `federateSaveBegun` is a no-op; status queries discard polling results; restore-not-complete calls federation abort in C++. Server accepts save time but initiates immediately in the inspected path. N05-N07 show reservation/finalization/snapshot defects. Domain correction alone leaves the C++ callbacks/status vectors/failure reasons incomplete. | Main's membership/ownership state; transport/order snapshots; operation-wide guards; C10 scheduling; C05 callback pipeline; Q04. | T12-T14. A+I. Exact time gates, status vectors, participant transitions, failure reasons and restored identities require N. RTI is not responsible for serializing arbitrary federate application memory. |
| C08 / P1 / Unassigned declaration/DDM; Transport/C++ binding | C++ ignores active/passive and update-rate designators in ordinary and DDM subscriptions. Most advisory switches are empty; scope switch records state without demonstrating complete delivery gating. Active/passive `FULL` fixture cannot identify the phase that caused its one callback. | C02 known-object state; MOM switches; inherited subscription and update-rate declarations; C05. | T17. A+I; Q06 for default switch values, edge-triggered notifications, passive subscription consequences, update-rate selection and error behavior. |
| C09 / P1 / Main automatic resign; Transport/C++ support | Automatic-resign getter returns `NO_ACTION`; setter ignores its value. Known-class/object-name/object-handle/update-rate and dimension support methods contain unconditional failure paths even when connected/joined. Some other lookups already delegate correctly; do not count all support services as missing. | C03 automatic cleanup policy; C02 per-federate knowledge; C04 FOM; DDM dimensions; N08 callback gate. | T18, T05. A+I; Q02/Q09 for automatic resign triggers/defaults, support service states and normalization. Setter/getter round trip alone cannot establish crash/disconnect cleanup. |
| C10 / P1 / Unassigned time owner; Transport/C++ binding | Domain `core.LogicalTime` and wire time are floating point. C++ service conversion requires `HLAfloat64Time`; integer headers alone do not establish supported integer-time execution. Enable callbacks exist as synthesized callbacks after RPC/query, while server enables state directly. | C04 factory name; pending-enable protocol; C05 timing/retraction; C07 scheduled save; N08/N09 scheduling. | T15, T19. A+I; Q01/Q07. If integer time is claimed, prove adjacent values above 2^53 survive all paths without `double`. Which factories must be offered is unresolved, not assumed. |

## Newly Confirmed Defects and Cross-Worker Hazards

These are finer-grained findings discovered or confirmed during this review,
not additional claims that every possible semantic requirement was audited.

| ID / priority | Source-confirmed defect and concrete consequence | Owner / dependencies / acceptance |
|---|---|---|
| N01 / P0 | `application/server/run.go::deleteAllOwnedBy` at 2114-2134 probes only `1..InstanceCount+16`. After repeated registration/deletion, the remaining live handle can exceed that bound and survive a deleting resign. | Main; enumerate actual instances under the operation boundary. T05 sparse-handle case. No full-text inference is needed to show that the helper misses its intended objects. |
| N02 / P0 | `hla/ownership/manager.go::acquire` at 648-675 interprets a missing ownership key as unowned and inserts it. `adapters/grpc/ownership.go:162-195` passes object/attributes through; the server's common interceptor validates membership, not object/FOM/publication. A joined caller can create ownership for an unknown object or undefined attribute. Self-owned attributes are not rejected in the inspected classifier either. | Main; registry/FOM/publication validation before mutation. T06. API declares relevant exceptions; exact conditions/precedence need Q02. Avoid a purported fix that rejects genuinely unowned valid attributes. |
| N03 / P0 | Authority uses original registration identity: `hla/object/delete.go:54` checks `inst.owner`; `hla/object/transport.go:96` does likewise for attribute transportation. Transferred attribute ownership does not change these checks. Thus new attribute owner B cannot change its transport, while original registrant A can. Deletion must be audited against transferred `HLAprivilegeToDeleteObject`, not a generic object-owner surrogate. | Main + Transport/C++; common authoritative ownership lookup and atomic batch validation. T02, T07. MIM declares the delete-privilege attribute; exact deletion conditions require Q02/Q05. |
| N04 / P0 | C++ `confirmDivestiture` and `attributeOwnershipAcquisition` at 1041-1058 discard caller tags. `attributeOwnershipDivestitureIfWanted` at 1087-1099 reconstructs a historical result with later ownership queries. Server `DivestIfWanted` selects candidates, unlocks, then `completeTransfer` overwrites ownership without revalidating the selected pending request. A cancellation/competing transfer can interleave; post-query output can also omit an attribute divested then reacquired. | Main + Transport/C++; atomic result in RPC/domain contract and preserved tags. T08. Which tag is propagated for each ownership transition, cancel confirmation and release-denied semantics require Q02. Source establishes discarded input and split critical section independently. |
| N05 / P0 | `save_restore/manager.go::requestFederationRestore` applies snapshots at 868 before its second busy check/reservation at 899-911. Two loads can mutate state; the loser then reports already-in-progress. `applyManagerSnapshots` mutates managers sequentially and can fail after an earlier manager changed. | Save; reserve operation identity before load, validate/prepare all state or provide complete rollback. T13. Shared-state correctness defect independent of IEEE rollback wording; external failure-state obligations require Q04. |
| N06 / P0 | `recordFederateSave` calls external snapshot/storage work after `markFederateAndAggregate` leaves the active save terminal-but-present. Repeated final reports can finalize twice. `AbortSave` can remove it and allow a successor; old `finalizeSave` unconditionally deletes by federation key and emits old success. Save/restore initial checks inspect separate maps, allowing overlapping operations. | Save; per-operation identity, exclusive save/restore admission and terminal transition. T12/T13. Guard old callbacks and rollback as well as map deletion. Exact duplicate-service errors and abort outcomes need Q04. |
| N07 / P0 | `ownership/snapshot.go::PendingDivestState`, `ExportState`, and `ImportState` omit `pendingDivest.twoPhase`; `savebundle/ownership.go` and `proto/rti/v1/ownership.proto::PendingDivestEntry` omit it too. A restored negotiated two-phase divest becomes one-phase, so later acquisition can bypass confirmation. Snapshot equality over the reduced struct cannot detect this. | Main domain state + Save serialization, coordinated field/default policy. T14. Whether a particular pending transfer is permitted at a standard save point is Q04; internal snapshot semantic loss is already established. |
| N08 / P0 | `RTIambassadorImpl.cpp::evokeCallback`/`evokeMultipleCallbacks` drain the synthetic queue before M17. `disableCallbacks` only gates M17. A queued ownership query/report/time-enable callback therefore still executes after callbacks are disabled. | Transport/C++; one consistent callback gate for both queues. T20. Exact restrictions on the toggle and callback re-entry need Q09; the bypass itself is explicit. |
| N09 / P1 | `callback_model_` is written at `connect` and declared in the header, but has no inspected read. `RtiAmbassador.cpp` reader pushes events into `event_queue`; dispatch is in evoke/tick. Both accepted callback models therefore use evoked delivery in the observed implementation. Current immediate-model fixtures all evoke, masking this. | Transport/C++; immediate dispatch/lifecycle or standards-permitted explicit rejection, not silent acceptance. T20; Q01/Q09 for required supported models and exact scheduling conditions. |
| N10 / P1 | MOM federate adjustment handlers use `sender` at `handlers_switches.go:114,117,143,163`, ignoring inherited `HLAfederate`. Official MIM at 805-817 identifies a target joined federate and permits adjustment of another federate. A sending federate A targeting B instead changes A. The MIM's service-reporting feedback exclusion at 879-886 is also not checked by this handler. | Unassigned MOM owner. T16; target/encoding supported by M evidence, remaining error/report rules by Q08. Correct switch decoding does not fix targeting. |
| N11 / P1 / bounded fix verified | Original defect: `ddm/manager.go::SetRangeBounds` stored reversed and over-limit ranges in pending state. Server validation now rejects both before mutation and uses the same effective upper bound as region creation. Domain and production RPC tests verify pending/committed state preservation and declared-limit acceptance. | See the DDM registration milestone. T21's bounded validation defect is corrected; exact endpoint/degenerate-range/overlap and template semantics still require Q09. Do not infer full DDM conformance from existing closed-open implementation tests. |
| N12 / P1 / bounded fix verified | Original defect: optional FDD `time`/`serviceUtilization` and legitimate children were rejected by the element whitelist. Corrected in `parser/fdd_metadata.go`, `fdd_services.go`, `strict.go`, `walk.go`, and `datatype_ref.go`: context-specific acceptance, all 168 schema service names/fixed attributes, time structure and datatype reference validation. Global unknown-element rejection remains. | Parser follow-up; actual `Parse` positive/negative tests and independent official-XSD fixture validation pass, detailed below. This closes the bounded vocabulary/metadata handling defect, not all T09 modular FOM cases, Q03, general XSD or OMT conformance. |

High-priority N01-N09 and the test-oracle findings were sent to the main task as
they were confirmed. N05-N07 were also sent directly to the Save worker, including
abort-during-load/write, successor-operation identity, rollback-capture failure,
and a failing decoder that mutates before returning an error. Worker intent or a
new field alone is not independent closure evidence.

### Concurrent correction checkpoint

Before finishing this document, a second source read found changes by the
implementation workers: restore reserves `RestoreLoading` before I/O and checks
save/restore exclusivity; snapshot application has a prepare/commit path and
legacy rollback that includes a mutating failing decoder; save request,
completion, abort, and lifecycle cleanup use a per-federation operation lock.
The domain snapshot, save-bundle adapter and `PendingDivestEntry` now carry
`TwoPhase`/`two_phase`. These address the observed mechanisms in N05-N07.

N05-N07 remain historical source-confirmed findings with **correction observed,
acceptance pending**, not assertions that the old code is still present. This
review did not execute the workers' tests or establish all interleavings,
ordinary-service/save admission, restored identity, callback or compatibility
behavior. Re-run T12-T14 against the settled tree before closing those rows.
Other live edits may postdate this checkpoint.

### Integrated resign and transportation checkpoint

The earlier C03/N01 findings describe the pre-correction source. The composed
server now uses `PrepareResign` with object/ownership plans and one all-recipient
reservation before the lifecycle journal append. The unconditional post-resign
ownership release and numeric object-handle probe are gone. Deletion uses current
privilege ownership, cleans dependent state, and releases registered names only
on commit. Tests cover all six action paths, owned/pending rejection, sparse
handle 41, transferred privilege, queued acquisition, cancel/delete/divest,
known-object removal after unsubscribe, journal/reservation rollback, name reuse,
and retry. M24 passed 20 repeated Windows runs; M24, federation, object,
ownership, and composed-server race suites passed. This is bounded T05/T07
evidence, not closure of N02/N04, automatic resign, or Q02.

C01's four request/query methods and corresponding RPCs are present. C++ runtime
contracts and Go integration tests pass; the composition adapter now resolves
FOM transportation defaults including inherited attributes. Whole-message
routing, metadata, persistence and concurrency obligations remain open.
The service-group rows retain their broader open status; their initial source
observations must be read together with these correction checkpoints.

Final integration also forwards explicit save-begun and participant restore
failure through Go/Python/C++, preserves per-federate status vectors, and makes
the C++ synthetic callback queue obey callback disable/enable. The latter
corrects the identified N08 bypass, not N09 immediate-mode dispatch. Present
empty service snapshots now clear live state while absent legacy entries keep
their historical meaning. These are bounded C07/N05-N08 corrections; scheduled
saves, cross-service admission/isolation and complete restoration remain open.

Final evidence is recorded in the implementation progress document: all explicit
Go source-tree tests and `go test -race ./rti/...` passed, as did seven C++ runtime
contract targets, three actual-server integration targets, and 62 selected Python
tests (one POSIX-only test skipped on Windows). Full normative applicability and
independent conformance verification remain unresolved, regardless of these
regression results.

### N12 implementation verification

Follow-up date: 2026-09-18, at the user's explicit implementation request.
Production edits are confined to `rti/pkg/fom/parser`. The existing exported
`model.FOM` has no time-description or service-utilization metadata fields and was
not changed. Time descriptions are now decoded for reference validation against
the existing MIM/local datatype resolver; they do not select an RTI time factory.
Service-utilization metadata is checked in the XML token pass, not used to enable
services or advertise RTI capability.

`fdd_metadata_test.go` exercises the actual `Parse` API with empty/populated
sections, all 168 official service entries, MIM and local time references,
prefixed XML, optional/boolean service flags, and unchanged ordinary object
model data. Negative cases reject unknown/misplaced/nested elements, duplicate
sections/children, wrong child order/namespace, missing or wrong fixed service
attributes, malformed boolean values, unexpected text, unrelated unknown
elements, and empty/unresolved time datatype references. Failures return no FOM;
structural diagnostics retain module and source line. The new positive cases
were observed failing with FOM-009 before the production fix.

Executed successfully on Windows, Go 1.26.5, with an isolated temporary cache
because the default Go cache was inaccessible:

```powershell
$env:GOCACHE = Join-Path $env:TEMP 'gorti-n12-gocache-20260918'
go test ./rti/pkg/fom/parser -count=1
go test ./rti/pkg/fom/... -count=1
go test ./rti/pkg/fom/parser -run TestFDD2010Metadata -count=10
```

Independent schema verification used .NET `System.Xml.XmlReader` with
`ValidationType.Schema` and the official `IEEE1516-FDD-2010.xsd`, SHA-256
`8846b46dc0ee8ee1299c0e854e65630cf4de18375f6ce1200abd904e37f5a3c1`.
`parser/testdata/fdd2010-metadata.xml` (168 service entries plus populated time)
and its in-memory empty-time, empty-serviceUtilization, and both-empty variants
all validated. In-memory unknown-time-child and wrong-service-section variants
were rejected, as expected. The committed fixture and service catalog retain the
official schema's spellings, including `removeobjectinstance`, with attribution;
they are not derived from gorti's API method inventory.

Remaining boundary: this is not full schema validation, whole-document namespace
or cardinality validation, foreign-extension schema support, cross-module time
metadata merging, runtime factory negotiation, or full OMT conformance. Those
questions remain under Q03/C04/C10. No transportation, save/restore, ownership,
or other production paths were changed by this follow-up.

## Bounded Acceptance Catalog

These are proposed executable cases, not completed results except for the
explicitly recorded N12 metadata subset of T09 above. Run each remaining case
against a known binary/configuration and capture actual callbacks/values/exceptions. A
10-second test deadline is a harness liveness budget, not an IEEE timing rule.
Use explicit phase barriers and bounded queues; absence assertions require a
settled phase/delivery barrier, not only a sleep. Keep binary tags containing
`00`, `80`, and `ff`; preserve distinctions being tested in trace normalization.

| Test | Finite fixture and assertions | Oracle boundary |
|---|---|---|
| T01: official API and transport | Compile a client against the unmodified official 2010 C++ headers, excluding repository `include/RTI` from client include lookup. Instantiate via factory, link to the product library, call all four transport methods, record confirm/report callbacks. Two publishers use different transport for the same interaction class; query each publisher from a third federate. Exercise both transports, one invalid enum, unknown class/object/attribute, unpublished class, and a repeated request before confirmation. | Compile-only proves neither ABI nor runtime semantics. Verify declared exception types; pending-request expectations require Q05. Record compiler/ABI/header adaptation explicitly if official historical headers need compiler compatibility handling. |
| T02: transport after transfer | A registers object with x/y; B acquires x, A retains y. B changes x successfully; A changing x fails; A changes y. Request mixed x/y as A and verify failed batch leaves both unchanged. FOM declares non-default transport; query before override. One update contains two differently configured attributes; inspect resulting callback grouping/metadata and selected delivery path. | Do not assert both attributes must share one callback. Q05 controls permissible splitting, timing, and default reset on transfer. |
| T03: local deletion | A publishes/registers; B/C discover. B locally deletes; support queries using B's old handle/name must reflect lost knowledge as specified. C still knows the object and receives A's next update. Exercise B rediscovery/queued update using a phase barrier, B owning one attribute, pending B acquisition, and unknown object. | No-remove-callback alone is insufficient. Q05/Q09 determine permitted rediscovery and queue disposition. |
| T04: order changes | A sends object x and interaction I with initial FOM order, changes their order, then sends another tagged message while B is constrained and C is not. Record sent/received order and time overloads. Repeat after x transfers to B; include invalid order and mixed-owned-attribute failure without mutation. | Q05/Q07 establish actual ordering and timestamp behavior; no universal timestamp-to-TSO rule. |
| T05: all resign actions | Six official actions x five starting states: no obligations; only owned attributes; only queued acquisition; delete privilege on O plus owned attribute on another object; owned plus queued. Record membership, known objects, current owners, pending requests, peer callbacks, retry viability. For each rejected action, assert unchanged state and a live callback stream. Additional case registers/deletes 40 instances then resigns with one high-handle instance left. | Derive exact action outcome table from Q02. Required core distinctions: delete, divest, cancel, no action and combinations. Do not mistake successful enum forwarding for a valid outcome. |
| T06: ownership validation | With a real FOM and joined A/B, acquire a valid unowned x, then separately unknown object, undefined attribute, unpublished attribute, self-owned attribute, repeated acquisition, and [valid unowned x, invalid y]. Check state and callbacks, not only error. Repeat query/is-owned on unknown object and undefined attribute. | Expect no ghost records or valid-subset mutation on rejected batch. Exact standard exception/state ordering requires Q02; membership-only fixtures cannot prove object validation. |
| T07: delete privilege and queued ownership | A registers O, transfers only `HLAprivilegeToDeleteObject` to B. Test direct delete and deleting resign by A/B in separate fresh federations; keep a normal attribute on the other federate. Separately B/C queue for x, A unconditionally divests or resigns, and B cancels before A's operation. | Check one valid owner, no stranded/departed winner, correct object knowledge and callbacks. Do not require a particular B/C winner unless a clause fixes selection. Q02 determines completion sequence. |
| T08: ownership result, tags and interleaving | Three federates; two attributes, one wanted. Divest-if-wanted returns exactly the set transferred by that call. Force reacquisition before a would-be follow-up query. Barrier between candidate selection and transfer; cancel winner or perform competing divest, then release. Run negotiated request, acquire, confirm and cancel with distinct binary tags; verify callback identity/set/tag and no duplicate terminal notifications. | Result must describe the operation, not later current state. Use Q02 to fix per-transition tag source, cancel confirmation, and release-denied effects. |
| T09: modular join and MIM | Base module defines A; join module adds B and inherited attribute; joined peers resolve/use it without changing A's handles. Negative cases: missing module, conflicting duplicate, dimension/type conflict, invalid custom MIM. A failed join leaves membership/FOM/handles unchanged. Repeat with two simultaneous extensions. Schema-positive cases add `<time/>`, `<serviceUtilization/>`, and a populated `time/lookahead` to an independently XSD-valid FDD; verify they are not rejected as unknown elements. | Full Q03 merge/applicability rules, not simple union or blanket duplicate rejection. Check module identity in MOM and save compatibility. XSD validation and RTI acceptance are separate assertions. |
| T10: factory negotiation | Each create overload with empty/default, `HLAfloat64Time`, and unsupported name; failed creation leaves no federation. Join existing federation and inspect factory. Integer implementation, if offered, round-trips 2^53, 2^53+1, and 2^63-2 through update/time callback/save/restore. | Q01/Q07 decide mandatory factories and valid extrema. Unsupported-factory rejection is evidence for that case only. |
| T11: metadata paths | Two senders/one receiver, tagged object update, interaction, and delete. Exercise RO and timestamped service variants over unary, streaming and LocalLRC paths that the product exposes; repeat object/interaction with DDM and both transports. Inspect bytes, sent/received order, transport, producing-federate/region presence, and retraction handle round trip. | Use Q05 to select applicable callback overloads and presence conditions. Supplemental fields must neither disappear when required nor be fabricated when absent. Queue/snapshot codecs must preserve the same contract. |
| T12: save protocol | Three joined federates: request -> query each status -> begun for A only -> query -> complete A/B -> not-complete C -> failure reason/status. Separate successful save; separate abort before last completion; separate scheduled save with regulators at 0 and requested time 10; include calls outside save, repeated begun/completion and injected outbox/storage failure. | Expected status names/vector membership/reasons/time gate require Q04. Federate application-state save is fixture-owned, while RTI object/time/ownership/declaration state is checked separately. |
| T13: restore/terminal races | Two bundles with distinct manager states. Barrier first load before application; concurrent second restore must not mutate if rejected. Fail a late decoder after earlier state applied and after failing decoder itself mutates; assert complete rollback or atomic prepare/commit. Barrier final save write; duplicate completion; abort old save; start new save; release old writer; ensure no stale finalizer removes successor or emits contradictory success. Repeat abort/new restore while reader is blocked and save versus restore overlap. | Data integrity assertions need no guessed IEEE clause. Q04 supplies allowed callbacks/errors. Include rollback-image capture failure, rollback failure reporting, and operation identity across destroy/recreate. |
| T14: persistence continuation | Save with pending two-phase divest and tagged queued acquisition; restore and prove ownership remains with A until confirm, then changes once. Include transport/order overrides, pending/committed DDM ranges, advisory switches, time queue/retractions, and object knowledge. Rejoin under different current handles; then update, acquire, delete and query with the identity prescribed after restore. | Opaque snapshot byte equality is not a semantic oracle. Q04 decides legal save points and restored handle/name mapping; if a state cannot legally be saved, test its rejection and test codec round trip separately. |
| T15: time services | Three regulators plus one nonregulator; run TAR, TARA, NMR, NMRA, FQR separately with messages at 1 and 2, equal timestamps and zero lookahead. Include simultaneous requests, late enable, calls during enable-pending, failed grant delivery, modify lookahead, asynchronous RO gating and sender retract before/after delivery eligibility. | For each primitive derive its own allowed partial order, bound/grant and callback rules from Q07. Do not promote another RTI's scheduling choice to the only legal trace. |
| T16: MOM | Real official MIM, A targets B via inherited encoded `HLAfederate`. For each federate switch/reporting handler send 0/1, omitted, empty, 1-byte, 3-byte, 5-byte, value 2, and invalid target. A stays unchanged when B is targeted; mixed malformed update changes neither switch. Independently encode request/report counters and handles. Target B subscribed to service-invocation report and attempt enabling reporting. | MIM supports target and feedback-exclusion assertions; Q08 supplies exact failure/report channel and full applicable catalog. Exercise requested attribute updates and periodic timing as separate catalog rows, not covered by decoder tests. |
| T17: declaration/advisories | Publisher plus passive B and active C; barrier after each subscribe, unsubscribe and switch transition. Record advisories before enabling, after enabling, after disabling, with only passive subscriber, with active subscriber and after last active leaves. Repeat object/interaction and DDM variants; valid/unknown update-rate designator. | Shared phase markers prove which transition caused each callback. Q06 supplies switch defaults and notification conditions; passive subscription is not automatically no data delivery. |
| T18: support and automatic resign | For each implemented lookup, round-trip valid name/handle and reject a wrong-class/unknown value in disconnected, connected-unjoined and joined states. Include object known only to A, local-deleted B, dimension maximum and update-rate lookup. Set/get each resign directive, reject invalid enum, then exercise abnormal disconnect with owned object/pending acquisition. | Apply service-specific legal-state tables, not one global joined-only assumption. Q02/Q09 control automatic trigger and cleanup obligations. |
| T19: time representation | On offered float64 path test encode/decode reference vectors, 0, adjacent finite values, NaN and infinity handling at each applicable service. Check factory identity on joined SDK and save bundle. Run optional integer exactness tests from T10 only for a claimed or normatively required implementation. | Headers expose types; Q07 fixes admissible time values and arithmetic/encoding obligations. Do not silently coerce unsupported types. |
| T20: callback scheduling | Queue one synthetic report/query callback and one real stream callback; disable, evoke twice, verify neither fires; enable and verify exactly-once delivery. Fresh `HLA_IMMEDIATE` connection receives a ready callback without any evoke; fresh `HLA_EVOKED` does not dispatch until evoked. Repeat disconnect/reconnect, failed resign and callback re-entry cases. | Q09 supplies exact legal timing/re-entry/bounds. Deadlines are test infrastructure, not standard service-time requirements. Accepted callback model must not be ignored. |
| T21: DDM | Dimension max 100: try [1,9], [9,1], [0,101], [100,100]; inspect pending/query/commit and failed-batch invariance. Two regions, one invalid/not-owned, atomic commit; deletion while in use; association with wrong dimension set and transferred attribute. Test touching boundaries, default regions, subscribe inheritance and pre/post-commit delivery. | Q09 resolves inclusive endpoints, degenerate ranges, local versus committed query view and allowed effects. Existing closed-open overlap tests are implementation evidence only. |
| T22: independent release profile | Independent federate/client using official supported binding, real FOM and independently encoded values. Run the same bounded cases against gorti and a reference implementation with raw callback traces retained. Cover each declared OS/compiler/binding combination. | Comparing two separately run RTIs is portability/differential evidence, not cross-vendor RTI wire interoperability or certification. Q01/Q10 determine claim scope and any external program requirements. |

## Why Existing Tests Are Insufficient

| Evidence source | What it establishes | Specific missed assertion |
|---|---|---|
| `rti/tests/integration/spec/M24/resign_actions_test.go` | All six values reach a captured `OnFederateResigning` hook; invalid enum leaves membership. | No production cleanup hook, owned/pending preconditions, sparse handles, peer observations, or rollback on failed resign. |
| `cppsdk/tests/dlc/conformance/_runtime/test_federation_contract.cpp` | Local gRPC endpoint checks request fields, time-factory rejection, generation fencing and failed-resign callback-reader survival. | Endpoint does not implement production ownership cleanup or modular merge. Generation rejection does not establish a particular IEEE exception for a product-specific token. |
| `rti/tests/integration/spec/M23/transport_test.go` | Override stored/read, registrant accepted, other caller rejected, unspecified enum rejected. | No transferred attribute owner, FOM default, publication, callback confirmation/report, actual delivery-path selection or mixed-transport update. Its interaction test changes an unpublished class. |
| `cppsdk/tests/dlc/conformance/dm_pub_sub_active_passive/federate_publisher.cpp` and `federate_subscriber.cpp` | One publisher callback across a four-second window; separate subscriber trace records both calls. | One callback caused incorrectly by the passive call still matches. No cross-process phase assertion distinguishes passive from active. |
| `cppsdk/tests/dlc/conformance/om_local_delete/federate_subscriber.cpp` | Local delete returns; no remove callback during drain. | A no-op satisfies both. No subsequent knowledge lookup or rediscovery/update behavior. README still says RED while `EXPECTED_VERDICTS.txt` says FULL; inspect executable oracle rather than either label. |
| `cppsdk/tests/dlc/conformance/fm_save_restore_roundtrip/federate.cpp` | One-federate save/restore callback sequence. | Comment promises restored Counter=10 but code never checks it. No federateSaveBegun effect, status vector, scheduled save, negative completion, restored ownership/handle use, or concurrent operation. |
| `cppsdk/tests/dlc/conformance/_runtime/test_callback_bridge.cpp::AcquisitionNotificationSupplementsEmptyTag` | Explicitly expects zero-length synthesized tag. | Preserves known information loss rather than testing caller tag provenance. Similar converter-only tests cannot prove wire metadata exists. |
| `rti/internal/hla/save_restore/restore_test.go::TestRequestRestore_AlreadyInProgress` and `structured_snapshots_test.go` | Sequential already-busy rejection, manager snapshot invocation and marshal failure handling. | No two loads racing before reservation; no late decoder partial mutation; no stale save finalizer after abort/successor. |
| `rti/tests/unit/mom/switches_test.go` | Correct four-byte values and rejection/atomicity for decoder input. | Uses fake FOM and single sender 7; never targets another federate using inherited `HLAfederate`, and does not establish complete MOM behavior. |
| Immediate-model C++ fixtures above | Their callbacks can be received while explicitly evoking. | Cannot establish automatic `HLA_IMMEDIATE` dispatch or synthetic-queue disable behavior. |
| `tests/conformance/rti/ivct-subset/README.md` and fixtures | 35 selected Python-native tests inspired by IVCT. | Excludes interactions, DDM, MOM, save/restore, external RTI interoperability and much negative/ordering behavior. Late-discovery xfail is not passing conformance evidence. This is not the official IVCT harness or RTI certification. |
| `cppsdk/tests/dlc/conformance/EXPECTED_VERDICTS.txt` | Scenario-specific expectations; explicit race and cross-language skips. | `FULL` means the fixture's asserted trace passed, not all requirements for a service group. Skips require executable replacement evidence for any applicable obligation. |
| `verification/gorti-go-fair/projection.go::writeProjection` | Projects workload-specific payload/count/configuration summaries into four semantic records. | Does not retain callback tags, exception identity, supplemental metadata, complete raw callback timing or ownership state. Useful comparison artifact, insufficient oracle for C01-C10. |

## Completion Dependencies Beyond C01-C10

| Order / requirement family | Concrete dependency and missing verification record | Owner / evidence / acceptance |
|---|---|---|
| 0: normative applicability | Obtain all three full 2010 texts and applicable corrections; map every applicable RTI obligation, conditional obligation and excluded federate-only obligation to a clause. Existing service numbers in comments/goldens are not checked clause evidence. | Unassigned normative reviewer; Q01-Q10. No full-conformance gate can pass before this inventory. |
| 1: shared authority and mutation | Object-known state, current attribute/delete privilege, pending acquisitions, membership and save/restore guards must be consistent across unary and asynchronous paths. Invalid requests must not leave partial ownership/transport/FOM state. | Main with Transport/C++ and Save; N01-N07, T02/T05-T08/T13/T14. |
| 2: wire and callback contract | Return atomic ownership results and carry metadata, save progress/status/reasons, and callback model state before independently adapting SDKs. Preserve old wire interpretation or declare a versioned migration. | Transport/C++ + Main + Save; T01/T08/T11/T12/T20. Include event/snapshot codecs, not only protobuf declarations. |
| 3: all FM/declaration/object services | Synchronization registration failures/subsets/failed participants; reservation atomicity; inherited subscriptions, partial unpublish/unsubscribe, update requests, local knowledge, callbacks after lifecycle transitions. Existing positive fixtures are starting evidence, not complete state/exception tables. | Unassigned service owners; official API + full service clauses; T03/T09/T17/T18 plus per-operation legal/illegal-state rows. |
| 4: time and asynchronous admission | Five advance primitives, GALT/LITS, pending enable, zero lookahead, RO/TSO/retraction and queued LocalLRC completion. Verify dependent operations cannot overtake an uncommitted predecessor; exceptions required at service return cannot become silent asynchronous drops. | Unassigned time/delivery owner; `hla/time`, `adapters/grpc/local_lrc.go`, `interaction_stream.go`, `confirmed_object.go`; T11/T15/T19. No architectural replacement is assumed. |
| 5: FOM/OMT and MIM | Validate namespaces, structure/cardinality, inheritance, data-type/dimension references, modular consistency, update rates, order/transport declarations and custom MIM rules. `parser/strict.go` retains a legacy element-name whitelist with bounded contextual FDD metadata validation after the verified N12 fix. Other parser diagnosers exist, but this is not complete XSD/OMT verification. | Unassigned broader parser/model reviewer; official XSD/MIM, parser diagnosers, Q03. T09 metadata subset has passing evidence above; modular/merge, T10/T21 and full schema/OMT work remain open. Independent nested encoding/alignment vectors are still separate evidence. |
| 6: full MOM and DDM | Inventory every applicable MIM attribute/request/report/service with real target/payload and object update behavior. Verify DDM dimension contexts, limits, pending commit, ownership, inheritance/default regions and boundary overlap. | Unassigned MOM/DDM; Q08/Q09, T16/T21. Complete XML copied into the repo is not executed MOM behavior. |
| 7: independent claim evidence | Separate (a) independent official-API client, (b) independent encoding/oracle, (c) multi-vendor federate portability/interoperation under a declared setup, (d) differential reference-RTI run, and (e) external certification, if pursued. | Unassigned verification owner; Q01/Q10, T22. Preserve original traces and exact source/binary/configuration; identify product extensions separately. No HLA cross-vendor RTI wire protocol or universal standard-binding requirement is inferred. |

## Unresolved Normative Questions

| Gate | Full text needed before asserting the following |
|---|---|
| Q01: claim and binding scope | Conformance provisions across 1516, 1516.1 and 1516.2; mandatory versus conditional services/bindings; allowed supported-language/platform claim; required callback models and logical-time implementations; applicable corrections. C++/Java/WSDL distribution alone does not answer this. |
| Q02: ownership and resign | Exact six-action preconditions and operation order; delete-privilege treatment; pending acquirer selection/cancellation; required ownership callbacks and tag source; release-denied and RTI-owned/unowned distinctions; automatic resign default/trigger; legal error identities and any required precedence. |
| Q03: OMT/modularity | Allowed module extension, duplicate/merge/inheritance and MIM modification rules; which semantic model constraints the RTI must reject versus which are model-author obligations; namespace/schema requirements and encoding responsibilities. |
| Q04: save/restore | Scheduled save time conditions; begun/completion state transitions; status vector membership and names; negative completion versus abort; callback failure reasons; membership changes and legal save states; restored handle/name binding; required state and behavior after failure; whether restoration into a recreated federation must be supported. Current generation fencing may be intentional product policy but cannot silently narrow a required restore case. |
| Q05: object/transport/delivery | Transport-change pending/confirmation/query conditions, FOM defaults and transfer behavior; local deletion knowledge/rediscovery; order-change restrictions; callback overload selection, sent/received order, retraction eligibility and optional producer/region data. Reliable transport does not imply a particular network mechanism, and best-effort does not mandate loss. |
| Q06: declaration/advisories | Active/passive semantics, switch defaults and changes, start/stop/turn/scope trigger conditions, inherited declarations and DDM interactions, update-rate selection/lookup and invalid designators. |
| Q07: logical time | Valid initial/final/special values and required factories; pending enable/granted initial time; each TAR/TARA/NMR/NMRA/FQR rule; GALT/LITS; zero/lookahead changes; asynchronous delivery and retraction. A shared generic grant oracle is insufficient. |
| Q08: MOM | Applicable full MIM behavioral catalog, inherited target decoding, report recipients, error/failure reporting, periodic/requested attribute updates, service/exception-reporting exclusions, counters and encodings. The target semantics and reporting exclusion already present in the official MIM are evidence, but not the entire protocol. |
| Q09: support/DDM/callbacks | Legal connected/joined states and operation-specific exceptions; known-object class visibility; dimension normalization/range endpoints and maximums; query pending versus committed bounds; empty dimension sets; callback enable/disable/model timing, re-entry, and time-bound semantics. Do not accept implementation comments as normative exceptions. |
| Q10: independent verification | Applicable definition/evidence for interoperability and any external assessment/certification program. A reference RTI disagreement is a diagnostic, not an automatic standard violation; matching a reference trace is not certification. |

## Evidence Source Register

All repository paths below are relative to `D:/workspace/gorti/gorti`. Exact
official-artifact paths are included so another reviewer can repeat the checks.

| Source | Paths / checked evidence |
|---|---|
| Roadmap and prior claims | `engineering/verification/ieee1516-2010-conformance-assessment.md`; `engineering/verification/ieee1516-2010-implementation.md`. |
| Official archive | `C:/Users/cbchoi/AppData/Local/Temp/gorti-ieee1516-audit-20260918/1516.1-2010_downloads.zip`; SHA-256 rechecked: `e6343f40b066d51b7f435c9aae6788be24f5d585e370d972c032ab0b503e27c1`. |
| Official C++ API | `C:/Users/cbchoi/AppData/Local/Temp/gorti-ieee1516-audit-20260918/cpp-api/cpp/src/RTI/RTIambassador.h` (resign at 161; transport at 647-701; divest result at 804); same directory's `FederateAmbassador.h`, `Enums.h`, `Typedefs.h`, `Handle.h`, `time/`, `encoding/` define the remaining artifact inventory to check independently. No full-header ABI validation was run in this review. |
| Official MIM/schema/bindings | `C:/Users/cbchoi/AppData/Local/Temp/gorti-ieee1516-audit-20260918/interface/1516.1-2010_downloads/HLAstandardMIM.xml`; sibling `IEEE1516-FDD-2010.xsd`, `IEEE1516-2010_Java_API.zip`, `hla1516e.wsdl`. MIM and `rti/pkg/fom/mim/standard-mim.xml` SHA-256 both rechecked as `839f092b4b3e02a1363ec4463efcb1aae7fbfdfb17778b159065f277a3f2b023`. |
| Binding and callback paths | `cppsdk/include/RTI/RTIambassador.h`; `cppsdk/src/dlc/RTIambassadorImpl.cpp`; `cppsdk/src/dlc/RTIambassadorImpl.h`; `cppsdk/src/dlc/FederateAmbassadorBridge.cpp`; `cppsdk/src/dlc/M17Bridge.cpp`; `cppsdk/src/RtiAmbassador.cpp`. |
| Resign/ownership/object authority | `rti/internal/application/server/run.go`; `rti/internal/hla/federation/manager.go`; `rti/internal/hla/ownership/manager.go`; `rti/internal/hla/ownership/release.go`; `rti/internal/hla/ownership/snapshot.go`; `rti/internal/hla/object/delete.go`; `rti/internal/hla/object/transport.go`; `rti/internal/adapters/grpc/ownership.go`; `rti/internal/adapters/grpc/server.go`. |
| Save/restore and wire contracts | `rti/internal/hla/save_restore/manager.go`; `rti/internal/adapters/savebundle/ownership.go`; `rti/internal/adapters/grpc/savepoint.go`; `proto/rti/v1/ownership.proto`; `proto/rti/v1/savepoint.proto`; `proto/rti/v1/stream.proto`. |
| MOM/FOM/DDM/time | `rti/internal/hla/mom/handlers_switches.go`; `rti/internal/hla/mom/dispatch.go`; `rti/pkg/fom/parser/strict.go`; parser diagnostic files; `rti/internal/hla/ddm/manager.go`; `rti/internal/hla/ddm/state.go`; `rti/internal/adapters/grpc/ddm.go`; `rti/internal/hla/core/time.go`; `rti/internal/hla/time/manager.go`; `rti/internal/hla/time/validate_tso.go`. |
| Test sufficiency | Specific files named in the preceding test-evidence table; `rti/internal/hla/ownership/snapshot_test.go`; `rti/internal/hla/ddm/ddm_test.go`; `tests/conformance/rti/ivct-subset/README.md`; `cppsdk/tests/dlc/conformance/EXPECTED_VERDICTS.txt`; `verification/gorti-go-fair/projection.go`. |
| N12 follow-up | `rti/pkg/fom/parser/fdd_metadata.go`; `rti/pkg/fom/parser/fdd_services.go`; `rti/pkg/fom/parser/strict.go`; `rti/pkg/fom/parser/walk.go`; `rti/pkg/fom/parser/datatype_ref.go`; `rti/pkg/fom/parser/fdd_metadata_test.go`; `rti/pkg/fom/parser/testdata/fdd2010-metadata.xml`; executed parser/FOM test and official-XSD results in the N12 checkpoint. |

## Closure Record Required Per Row

### Acquisition And MOM TDD Checkpoint

The next bounded cycle corrects the invalid-acquisition paths in N02. A real
gRPC handler with production object, declaration and ownership managers now
rejects unknown/deleted/undiscovered objects, undefined attributes, unpublished
classes/attributes and self-owned attributes. Both acquisition modes check the
complete set before mutation, with no callback on rejection. Valid unowned
inherited attributes remain acquirable. Publication validation reads the full
set under one declaration lock. Ready transfers commit before external observers;
the object lock is released before callbacks. A deletion injected into the first
observer does not allow later acquisition code to recreate ownership.

Evidence is in `rti/tests/integration/ownership/acquisition_test.go`, including
12 negative RPC cases, a deterministic publication-swap regression, precise
callback metadata, deletion, and observer/re-entry checks. The C++ actual-server
ownership fixture now tests publication rejection followed by a successful retry.
This does not close T06's query/is-owned cases, all divest/cancel paths, operation
admission during Save/Restore, or Q02. The production application supplies the
acquisition-preparation port; isolated algorithm tests without a registry do not
prove these preconditions.

For C06/N10, switch handlers select the explicit inherited target, validate all
values and check target membership under the mutation lock. Domain tests cover
sender/target separation, malformed/missing/foreign/resigned targets, high-bit
64-bit handles and concurrent adjustment/resign. MOM `HLAhandle` encoding now
uses a variable byte array around the SDK's eight-byte opaque handle, including
report parameters, object attributes and handle-list elements. Independent
literal vectors replace legacy raw-four-byte expectations. This is an observable
payload correction, even though Protobuf descriptors are unchanged.

**Production target integration corrected on 2026-09-19:** the FOM adapter now
resolves inherited `HLAfederate`, retains qualified class identity, and reaches
the MOM dispatcher through ObjectService. Tests require a successful adjustment
of the addressed target for service/exception reporting; rejection alone no
longer passes. Counter-request targeting and report layouts, reporting exclusions
and failure-report behavior remain open. C06, T16 and Q08 as whole groups are not
closed.

The [executable evidence manifest](ieee1516-2010-evidence.json) marks the bounded
production-target regression resolved and keeps normative coverage unresolved.
A bounded regression PASS cannot promote it to complete conformance. Gate policy and
external evidence distinctions are in [quality gates](ieee1516-2010-quality-gates.md).

A row closes only with the full-text clause/applicability where marked N,
implementation revision and supported profile, executed test IDs and commands,
unprojected outputs and verdict, plus independent review of the oracle. Mark a
corrected decoder/API adapter separately from the whole service group. No
unrun case, expected failure, skipped scenario, worker report alone, or
lossy projected trace counts as passing an applicable requirement.
