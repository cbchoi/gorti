# IEEE 1516-2010 Feature Completion Plan

Date: 2026-09-21
Revision: 6, continuation sequence after Python-first behavioral corrections
Status: In progress on `codex/ieee2010-feature-completion`
Target: IEEE 1516-2010, IEEE 1516.1-2010 and IEEE 1516.2-2010

Current execution order and milestone exit criteria are in the
[conformance execution plan](ieee1516-2010-conformance-execution-plan.md).
It prioritizes Python and supersedes the historical batch ordering and status
tables below, without discarding the R0-R9/M0-M7 scope. Consult the
[latest Python-first results](../verification/ieee1516-2010-python-behavior-20260921.md)
before treating any older finding or test count here as current. This planning
revision does not close an implementation or conformance requirement.

## Objective And Completion Rules

The first parallel implementation batch is recorded in
[the feature-completion checkpoint](../verification/ieee1516-2010-feature-completion-20260920.md).
It adds bounded MOM sources and stronger verification, not whole-plan completion.

Complete the RTI functionality, advertised bindings and verification needed for
a defensible 2010 release. This revision preserves M0-M7 identifiers and replaces
their initial execution order with the remaining work orders below. The old
eight-attribute backlog is no longer an accurate list of absent implementations.

Keep three decisions separate:

| Decision | Required result |
|---|---|
| Engineering feature completion | Every item in the reconciled product feature inventory is implemented and verified on its declared profiles; no unexplained stub, successful no-op, omitted input or fabricated result. |
| Full RTI conformance assessment | Every applicable obligation from the authorized 2010 texts has a reviewed disposition and evidence for the same release. Unresolved applicability blocks this decision. |
| External verification or certification | The actual external report or certificate, with its issuer, scope and version. Internal reviews and reference-RTI comparisons are not this evidence. |

Do not narrow the inventory merely to make the gate pass. A service's presence
in a header, a passing test count, or an `unavailable` result is not functional
completion. Legitimately undefined values must be distinguished from missing
implementation, lost observation coverage and unsupported legacy inputs.

The existing [quality gates](../verification/ieee1516-2010-quality-gates.md)
remain the claim policy. The milestones below operationalize that policy; their
IDs are project identifiers, not IEEE clauses.

## Normative-Source Progress

The three 2010 redline publications are now available and registered in
[the source-based review](../verification/ieee1516-2010-normative-review-20260921.md).
R0 now concerns completing the obligation inventory and evidence review, not
waiting for all standard text. Deleted earlier-edition text is excluded; any
ambiguous passage receives a specific base-publication check. Applicable
corrections still need confirmation.

IEEE 1516.1 Clause 13.2 requires at least one standard language API mapped under
Clause 12, so completing the selected C++ mapping is a valid direction without
making Java and WSDL simultaneous prerequisites. The same clause still requires
all specified services and callbacks. Clause 13 reserves the "HLA Conforming"
label for publicly available, recognized-authority-certified results. Internal
tests do not grant that label.

The first source-based pass also resolves two earlier planning assumptions.
Both standardized integer64 and float64 time representations are required by
1516.1 Clause 12.4; R6 is not merely an optional product target. MOM RO/TSO queue
lengths count pending messages until delivery/callback invocation, not every SDK
callback or application completion. Source-backed gaps in asynchronous delivery,
zero-lookahead constraints, gradual lookahead reduction, LITS and model admission
are listed in the review and take precedence over cosmetic metadata expansion.

## Third-Batch Progress

The [third-batch record](../verification/ieee1516-2010-batch3-20260920.md) tracks
the current work. R1 preserves transportation declarations and exact update rates
through export and fresh JSON reconstruction. R2 now has a separate selected-MIM
role in Go/Python/C++, repository state, MOM and additive V2 create/join inputs;
the expectation is checked before join mutation. Its accepted MIM profile is
structurally compatible with the embedded model, not unrestricted replacement.

R3 adds opt-in production outbox diagnostics, not complete RO/TSO totals. R4
reports actual resign outcomes before retiring MOM subjects and corrects signed
rejection attribution. R6 adds exact C++ integer64 values and factories, while
server time remains float64. R7 forwards Python query session metadata and adds
explicit MIM input. Complete strict C++ services, typed-time backend propagation,
cross-stage delivery accounting and remaining model/catalog coverage stay open.

The tables below retain historical planning observations. Read them alongside
the batch records rather than treating each original observation as a present
implementation gap. Engineering closure and normative R0 assessment remain
separate decisions.

## Second-Batch Progress

The [second-batch record](../verification/ieee1516-2010-batch2-20260920.md)
contains current implementation and test results. R1 now retains additional
dimension and descriptive metadata through joins and restore. R4 supplies a
session-authenticated caller resolver with generation/incarnation fencing and
checkpoint admission reuse. R7 forwards Python federate type and validates the
supported float64 time designator locally. R8 corrects replay byte comparison.

R3 remains an experimental ledger with no production totals. R5 has a strict
C++ foundation, not a complete service binding; the full official-header harness
still has failures. R2, R6, remaining R1/R4/R7/R8 work and normative R0 closure
are not completed. The tables below retain the original planning baseline;
consult the second-batch record before interpreting those observations as the
current implementation state.

## Verified Starting Point

Use the [first-batch checkpoint](../verification/ieee1516-2010-feature-completion-20260920.md)
and its source-bound evidence. The earlier
[implementation record](../verification/ieee1516-2010-remaining-services-20260920.md)
remains the history for dynamic FOM joins, restore/rebinding, timed deletion,
ownership denial, atomic name release, authenticated automatic resignation,
LocalLRC recovery and SDK callback work. Do not reclassify these as absent.

Latest completed campaigns, not rerun for this planning revision:

| Campaign | Result | Remaining boundary |
|---|---|---|
| Windows four-root Go | 72 tested packages; 2,075 top-level passes | Eight explicit skips; not all repository packages |
| Linux four-root race | 72 tested packages; 2,076 top-level passes; no race warnings | Seven explicit skips |
| Linux Python HLA | All 1,079 selected cases passed, no skips or source drift | Explicit HLA scope; not full normative coverage or upstream pyjevsim-engine validation |
| Manual Go gate | All 122 groups / 533 required entries passed | Bounded engineering requirements |
| Official-header C++ harness | 14 passed; three failed | Strict ownership types, exception declarations, integer64 factory linkage |
| Full-2010 readiness | Rejected with 39 prerequisite errors | These are not 39 missing functions |

The current-source reference is the final Python campaign's 2,142-file manifest,
identity `0d04895dad96958498688517fd504776ddb15e32a1b8aa79c8d7acca50488b94`.
The working tree remains a development checkout, not an immutable release.
M0 must create a new candidate inventory without overwriting the original
2,285-entry discovery ledger's baseline hashes or historical dispositions.

Source anchors for the remaining work:

| Area | Current source |
|---|---|
| Missing MOM sources and service attribution | `rti/internal/application/server/mom_metrics_sources.go`, `run.go` |
| Metric encoding, history and reports | `rti/internal/hla/mom/`, `rti/internal/adapters/grpc/mom_receipts.go` |
| Authoritative FOM model and snapshots | `rti/pkg/fom/`, `rti/internal/application/composition/`, `rti/internal/adapters/savebundle/` |
| Time and delivery state | `rti/internal/hla/core/time.go`, `rti/internal/hla/time/`, `rti/internal/adapters/delivery/` |
| Binding/time factory | `cppsdk/src/dlc/RTIambassadorImpl.cpp`, `rti/pkg/federate/`, `pysdk/rti1516e/` |
| Current evidence gate | `engineering/verification/ieee1516-2010-evidence.json`, `scripts/ieee2010_quality_gate.py` |

Generated `Unimplemented*Server` embedding and optional embedded-adapter fallbacks
are not automatically missing production functionality. Check actual production
composition and observable behavior before classifying each match.

## Remaining Work Orders

These are executable project work packages, not IEEE clause numbers. A source
gap is not automatically a reproduced standard violation. Each package must
name its normative or independently reviewed engineering oracle before closure.

| ID / priority | Existing milestone | Work and current evidence | Required completion result |
|---|---|---|---|
| R0 / P0 | M0, M7 | Three supplied 2010 redlines are available; complete clause-level review, confirm correction applicability, distinguish RTI/federate/model responsibilities and reconcile current source with the baseline ledger. | Every applicable obligation has a locator, owner, implementation path, oracle and review; no unresolved applicability at final assessment. |
| R1 / P1 | M1, M4 | Complete FOM metadata retention and FDD export. The exporter documents upstream loss and rejects absent switches/order/transport metadata. | Parse, merge, dynamic join, snapshot, restore and export preserve applicable semantics; independent XSD plus semantic-vector tests pass. |
| R2 / P1 | M1, M4, M5 | Give MIM selection an explicit end-to-end role. C++ currently prepends it to ordinary FOM modules; production merges the embedded MIM and reports a fixed designator. | A reviewed permitted alternate-MIM case works through SDK/server/MOM/restore; invalid changes fail atomically; no ignored MIM argument or false designator. |
| R3 / P1 | M2 | Implement complete `HLAROlength` / `HLATSOlength` accounting. Production has no RO provider and does not fill TSO totals; elapsed-time accounting already exists. | Independently checked queue conservation across applicable delivery stages, received order, retraction, drain, reconnect and restore, with no double counting. |
| R4 / P1 | M3 | Close MOM catalog and lifecycle reporting, including legitimate caller attribution, post-resign reporting and restore publication. Current composition does not supply `ResolveCaller`. | Every applicable attribute/interaction/parameter and reporting switch has a real stateful oracle; no healthy-profile fabricated or permanently missing source. |
| R5 / P1 | M5 | Implement a strict claimed C++ 2010 binding alongside a separately identified modern profile. Current C++17 ownership aliases and exception declarations fail official-header comparisons. | Unmodified official client headers compile, link and execute against the strict library; exact types, all overloads, callbacks and exception contracts pass on each claimed toolchain. |
| R6 / P1 | M4, M5 | Implement mandatory integer64 as well as float64 federation execution (1516.1 12.4, PDF 338-339); a local integer64 factory alone does not satisfy the backend requirement. | Exact integer values above 2^53 work through services, queues, retraction, save/restore, encoders and SDK callbacks; float64 compatibility and standardized final/epsilon behavior remain tested. |
| R7 / P1 | M5 | Complete Go/Python product parity and standard-style facade inputs. Python currently omits federate type/time designator and has no matching custom-MIM/time-factory surface. | An explicit operation/parameter/exception/callback matrix has no unexplained dropped input or missing advertised operation; compatibility aliases retain old call forms. |
| R8 / P0 for reproduced defects | M4 | Audit service state machines, exception precedence, atomicity and callback partial orders. Most services exist; this is coverage closure and defect-driven correction. | All applicable state/exception rows pass production-path tests, including concurrency, failure and restoration; no successful no-op or stub-friendly oracle. |
| R9 / P0 release | M6, M7 | Unify evidence ingestion; close skips/sanitizers, mixed-language and fault campaigns, performance budgets and release review. | One frozen candidate passes every applicable profile; an independently reviewed obligation-to-evidence dossier supports the final claim. |

R0's source/contract work uses the supplied texts now. Complete the inventory
while correcting reproduced gaps and building independent tests; do not wait
for all of R0 before doing R1/R3/R5.

### R1-R2: Model And MIM Contract

Retain identification, time declarations, service utilization, tags,
synchronizations, notes, descriptive semantics/update conditions, dimension
datatype/default metadata and absent-versus-empty distinctions where applicable.
Review foreign-extension handling explicitly. Preserve semantic information,
not arbitrary source formatting; an XML byte-for-byte round trip is not the goal.
Apply the same model through parser, merger, committed FOM handle, persistence,
restoration and FDD export. A serializer alone cannot repair discarded input.

Specify per-field merge/conflict/default rules from reviewed requirements.
Do not infer missing switch values from current runtime fallbacks. Add legal
implicit/default fixtures once their rules are established. Keep failed joins
invisible and ensure restore updates both query results and subscribed MOM data.

For MIM selection, define explicit role/designator/provenance in the application
contract and, if needed, a reviewed additive wire change. Test omitted/default,
explicit standard, permitted alternate and prohibited-redefinition cases.
Persist the chosen MIM; derive MOM reporting from that state. R1 and R2 share
model/parser ownership and must not edit those files concurrently.

### R3-R4: Delivery And MOM Contract

Count pending reflection, interaction and removal messages per recipient until
delivery/callback invocation (1516.1 8.1.1-8.1.2, 8.1.7 and Table 6). Do not
count grants/discovery/ownership/synchronization callbacks, stop counting at SDK
enqueue, or wait for application callback completion. The standard specifies
the metric, not a particular telemetry or acknowledgement protocol.
First map which implementation stages still hold messages within that boundary.
Use an accounting identity such as generation, federate incarnation, callback
epoch and sequence; use unique transfer identities rather than summing snapshots
of multiple queues. A handoff changes location, not the number of outstanding
items. Cover pre-fanout cancellation, per-recipient admission, batch splits,
duplicate acknowledgements, late acknowledgements and restore invalidation.

For the chosen boundary, independently assert: admitted items equal completed
items plus canceled/invalidated items plus outstanding items. Do not count
internal control messages as user RO/TSO events. Retain or reconstruct complete
coverage after recovery, and never invoke a user callback again to fix a metric.
Keep cumulative/batched progress rather than adding a synchronous RPC per event.

Generate the MOM catalog from the pinned MIM, separating abstract class nodes
from executable services. Preserve caller identity before successful resignation
removes state; never invent an issuer for a service without a legitimate caller.
Test callback suppression, failed callbacks, periodic and requested updates,
inherited subscriptions/targets, publication failures and reporting feedback.
Review current host withholding and elapsed-time measurement policies in R0;
their working implementations are retained unless that review requires changes.

### R5-R7: Binding And Time Contract

Use a separately built strict binding front end with an explicit compiler/ABI
profile, backed by the same service implementation as the modern C++ profile.
Prototype this boundary before committing to its build arrangement. Do not
silently substitute modern ownership types or erase exception specifications
while claiming the strict profile. C++14 probe success does not establish a
separate C++03/DLC/platform claim; each advertised profile needs its own evidence.

For time, define a federation-level implementation identity plus typed time and
interval operations. Carry the identity through create/list/join/getTimeFactory,
comparisons, lookahead arithmetic, LBTS, grants, retractions, snapshots and all
callbacks. Test signed extrema, overflow, infinities where applicable, and
independent byte encodings. Reject mixed/unsupported kinds before state mutation.
Do not implement integer64 by converting through a double, including in snapshots.

Existing float64 clients remain a compatibility test target. New wire features
need capability/version negotiation: an old server must not silently discard a
MIM/time-kind field and report success. Preserve field numbers and old fixtures;
change descriptor goldens only with a reviewed compatibility explanation.

R7 audits Python's `standard.py` separately from its lower-level API: correct
forwarding of federate type, model modules and time identity, plus factory and
handle-decode equivalents. Python/Go are product bindings, not the normative C++
binding. Java/WSDL and custom-time obligations are explicit R0 decisions; a
required additional binding becomes a separately owned work package, not a
waived release item.

### R8: Service-Level Acceptance Matrix

| Service family | Required stateful cases beyond a successful call |
|---|---|
| Federation, synchronization | Connection and membership transitions; all resign actions; failure participants; dynamic join; destroy/recreate generation fences. |
| Declaration and object | Inheritance; active/passive subscriptions; update-rate delivery; transport/order changes; discovery, local deletion and rediscovery; timestamp deletion and retraction. |
| Ownership | Competing acquisitions; negotiated/forced transfer; cancellation/denial; publication preconditions; tags, delete privilege and atomic multi-attribute outcomes. |
| DDM | Pending/committed ranges; inclusive boundaries; default/dimensionless cases; region ownership/context and multi-region rollback. |
| Time | TAR/TARA/NMR/NMRA/FQR; pending enablement; equal timestamps; zero/modified lookahead; GALT/LITS; asynchronous delivery; loss/retraction before grant; each admitted time representation. |
| Save/restore | Immediate/timed save; permitted calls at each phase; abort/retry; injected storage failure; queued events; generation, identity and factory restoration; migration/corruption. |
| Support and callbacks | Name/handle encoding and knowledge; exact errors; callback models and reentry; callback disable/enable; disconnect/resign/restore races. |
| MOM | Query versus reflection; counters and elapsed intervals; service/callback reports; control switches; lifecycle attribution and correct selected MIM. |

Each row expands into positive, invalid-state, boundary, concurrent and failure
cases as applicable. The independent oracle must detect a removed operation,
dropped argument, wrong recipient, wrong exception or early grant. Classify a
legitimate ordering freedom as a partial order, not a required total sequence.

## Milestones

### M0: Inventory, Scope And Reproducible Baseline

Priority: P0. Prerequisite for partitioning implementation work.
Remaining work: R0. Baseline discovery exists; exhaustive semantic and normative
mapping is not closed. Preserve the original ledger and create a versioned
candidate with reviewed disposition transfers.

1. Capture a content-addressed source snapshot including relevant untracked
   files, generated bindings and tool versions without reverting unrelated work.
   Continue on `codex/ieee2010-feature-completion`; use isolated worktrees only
   where needed for assigned work, without discarding the current changes or
   rewriting history. Archive raw results outside Git as before.
2. Reconcile C01-C10, N01-N12, F01-F13 and Q01-Q10 with current implementation
   records. Assign each finding: fixed with evidence, reproduced defect,
   unverified behavior, normative question, or justified non-applicability.
3. Inventory every service overload, callback, parameter, exception, lifecycle
   state, MOM attribute/interaction, encoding and factory. Trace each from SDK
   through wire/adapters to domain state and observable callbacks.
4. Record language/platform profiles explicitly. Start with advertised Go,
   Python and C++ profiles; determine Java/WSDL obligations from the actual
   conformance provisions rather than assuming either an exemption or a duty.
5. Complete the supplied-text review and confirm applicable corrections.
   Map requirements by locator and responsible component: RTI, federate, FOM
   author or federation. Do not copy licensed standards into the repository.

Inventory fields: ID, source/version/locator, service, overload, applicability,
implementation path, SDK/platform, oracle, required tests, evidence digest,
reviewer, status and blocking dependency.

Exit: engineering inventory has no untriaged row; baseline is reproducible.
The separate normative portion remains open until all applicability questions
are reviewed. Engineering work can proceed without pretending that portion passed.

### M1: MOM Host, Current FDD And Last-Save History

Priority: P1. Depends on M0's engineering inventory.
Remaining work: R1/R2 plus final host/history applicability and lifecycle review.
Host, durable save history, retained-model export and live join/save/late-subscribe
publication are implemented and regression-tested. Do not implement them twice.

| Attribute | Implementation | Acceptance |
|---|---|---|
| `HLAfederateHost` | Explicit client host reporting/configuration through authenticated session metadata or a reviewed additive service. Avoid collecting unrelated host/user information. Do not substitute a peer IP for a host name without a justified contract. | All SDKs report the configured value; wrong-session updates fail; missing or intentionally withheld data has a distinct state. Resolve policy versus normative requirements before a full claim. |
| `HLAcurrentFDD` | Export the realized merged FOM model as valid FDD XML, including applicable MIM/model content. Read one committed revision, not concatenated input modules. | Independent schema validation and semantic round trip; inheritance, types, dimensions and rates survive; successful joins update it, failed joins do not; restore selects the restored model; concurrent queries do not mix revisions. |
| `HLAlastSaveName`, `HLAlastSaveTime` | Persist authoritative completed-save history at the defined successful-save publication boundary. Use versioned optional state with explicit migration behavior. | Immediate/timed success, failure, abort, retry, restart and restore; preserve the previous successful result after failure. Distinguish a known no-save-yet state from an old snapshot with unknown history. |

Exit: four attributes have actual typed sources and live MOM object updates as
well as query results. Both positive values and legitimate undefined values are
tested against independent encoding expectations.

### M2: Delivery And Elapsed-Time Accounting

Priority: P1; highest concurrency and performance risk. Depends on M0.
Remaining work: R3 and measurement-policy review. Elapsed-state accumulation,
per-attribute publication baselines and restore fencing already exist; complete
RO/TSO accounting does not.

Complete `HLAROlength` and `HLATSOlength`; retain and verify the existing
`HLAtimeGrantedTime` and `HLAtimeAdvancingTime` implementation against the
reviewed requirements and combined lifecycle cases.

- Define measurement boundaries from the MIM and reviewed service semantics
  before coding. Socket write, queue admission and user callback invocation are
  different events; do not use the time buffer alone as the total delivery queue.
- Introduce typed, bounded accounting ports. Track the applicable server time
  queue, outbox and SDK delivery states without double counting movements between
  them. Separate received order from sent order and from internal control events.
- Use generation, membership identity, callback epoch and sequence fences for
  delivery acknowledgements. Cover batching, duplicates, cancellation, loss,
  retraction, locally deleted objects, resign, reconnect and restore invalidation.
- Keep accounting out of the hot path where possible through bounded batching
  or cumulative progress. Do not add one blocking confirmation per callback just
  to implement a metric. Reporting failure must not replay a user callback.
- Retain the injected monotonic clock and granted/advancing interval accounting.
  The MIM describes elapsed time since the last attribute update, with the first
  update measured since join. Keep per-attribute publication baselines; a read
  or failed publication must not silently discard an interval.
- Store durations and reviewed history, not process-local monotonic timestamps.
  Specify restart/restore, overflow, multiple consumers and coverage-loss rules.

Tests: controlled queue occupancy across all stages, independent conservation
checks, deterministic fake-clock intervals, report failure/retry, callback
models, restored epochs, and concurrent query/drain/snapshot operations.

Exit: all four attributes report correct values in healthy supported profiles;
failure-induced missing coverage is explicit and recovers under a defined
protocol. Race and hot-path allocation/latency checks pass.

### M3: Complete MOM Service And Lifecycle Reporting

Priority: P1. Can start alongside M1/M2; final integration depends on both.
Remaining work: R4, including integration of R1-R3 and factory identity from R6.

1. Generate an attribute/interaction/parameter coverage inventory from the
   pinned official MIM. Audit every required update, adjustment, request and
   report path, not only the eight known attributes or direct query RPCs.
2. Resolve caller attribution for operations without a federate handle in the
   request. Attribute only when the service contract provides a legitimate
   identity; never assign arbitrary federation members to fill a counter.
3. Capture the evidence needed for successful resign before state destruction
   and publish the required report at a reviewed lifecycle boundary, without
   retaining credentials or resurrecting the resigned MOM instance.
4. Exercise inherited targets, regional routing, report recipients, failures,
   periodic updates, switch combinations and reporting-loop exclusions.
5. Verify baseline migration, counter overflow, disabled callbacks, unreported
   callbacks and receipt exhaustion. Preserve the distinction between legacy
   admission/fanout counters and actual invocation counters.

Exit: every MIM row has an implementation and a stateful end-to-end oracle, or
an independently reviewed applicable exception. No report self-feedback loop,
invented success, silent omission or permanent missing source remains in the
healthy declared profile.

### M4: Service Semantics And Time Representation

Priority: P0 for any reproduced semantic defect; otherwise P1 coverage work.
Runs alongside M1-M3 after M0, using normative decisions as they become available.
Remaining work: R1/R2 model semantics, R6 time representation and R8 stateful
coverage. Existing services are corrected only when a reviewed test reproduces
a defect or identifies an unmet applicable obligation.

| Service group | Required closure work |
|---|---|
| FM, synchronization | Legal connect/join/resign/destroy states, all six resign actions, session/restore identity, synchronization membership and failed participants. |
| Declaration, object, ownership | Inheritance and active/passive behavior; update-rate effects; every order/transport change; knowledge and rediscovery; timestamp deletion/retraction; competing acquisition, denial, cancellation and atomic results. |
| DDM and FOM/OMT | Pending versus committed ranges, endpoints/default regions, dimensionless cases, context/ownership checks and multi-region atomicity; namespaces, modular consistency, custom MIM constraints and full applicable semantic model validation. |
| Save/restore | Immediate/timed saves; permitted calls during each phase; abort, timeout, I/O failure and partial failure; complete state/queues/identity; exact source selection, schema migration and corruption rejection. |
| Support and callbacks | Legal connection states, knowledge-scoped lookups, normalization, all error identities, callback enable/disable, re-entry and evoked/immediate execution boundaries. |
| Time management | Separate oracles for TAR, TARA, NMR, NMRA and FQR; pending enablement, grants, equal timestamps, zero/changed lookahead, GALT/LITS, asynchronous delivery, retraction and failure before grant. |

Current logical time is `float64`; the C++ factory advertises HLAfloat64Time.
Review required factories and pending-enable callback behavior explicitly.
If integer/custom representations are applicable or part of the approved
product inventory, implement a time-kind abstraction across comparisons,
arithmetic, wire encoding, queues, snapshots and SDKs. An integer wrapper over
`float64` is not acceptable: test values above 2^53, overflow, extrema and exact
round trips. Preserve existing float64 wire fields and reject unsupported kinds
explicitly. Do not infer normative factory obligations from header distribution.

Exit: each applicable state-transition/exception row is verified through the
real service path. Tests assert legal callback partial orders, not an arbitrary
global event sequence. Any new defect reopens its milestone and regression gate.

### M5: Binding And SDK Completion

Priority: P1. Contract scaffolding can run early; final results depend on M3/M4.
Remaining work: R5-R7. The float64 constructor and named selectors are already
fixed; strict type/exception contracts and integer64 linkage remain open.

- Build a parity table for all applicable operations, overloads, encodings,
  factories, exceptions and callbacks. Complete the Go/Python product APIs and
  each claimed standard binding without treating similarly named APIs as equal.
- Compile, link and execute independently authored C++ clients against the
  official claimed headers. Record include and link provenance so repository
  replacement headers cannot accidentally satisfy this check. Test advertised
  compiler/ABI/DLC profiles separately.
- Verify ordinary handles, retraction handles, strings, records, optional fields,
  supplemental metadata and caller identity using independent vectors.
- Test every advertised callback model, ordinary callback-to-service calls,
  forbidden re-entry, exceptions with empty messages, teardown and restore fences.
- Ensure standard-profile MOM accounting observes actual callbacks in each SDK;
  Go channel consumption alone cannot establish invocation.
- If M0 identifies a required missing binding, implement it as its own tracked
  work package. Do not silently waive Java/WSDL or add them without deciding the
  applicability and supported release profile.

Exit: the complete applicable SDK/profile matrix passes live-server tests.
Missing signatures, dropped inputs and callback translations are zero; documented
product extensions remain distinct from standard interfaces.

### M6: Integrated Quality And Performance Gates

Priority: P1. Harness work starts immediately; final runs follow source freeze.
Retain the existing local/manual validation policy. This plan does not expand
automatic CI workflows or commit raw execution logs.

1. Retain the now-passing 1,079-case Linux Python HLA campaign, including TLS,
   historical directories and independent-process language pairings. Expand its
   explicit expected IDs for new services without shrinking existing coverage.
   Rerun after source freeze on each claimed profile. Keep unrelated RL tests a
   separate scope; preserve the native Windows C++ prerequisite distinction.
2. Run complete Go tests, race and vet; C++ normal, ASan/UBSan and supported TSAN
   campaigns in a reproducible environment. Fix the sanitizer startup issue and
   dependency instrumentation; a skipped or unstartable sanitizer is not a pass.
3. Run live Go/Python/C++ pairings and mixed-language federations with
   N=2,4,8,16. Add independently implemented clients, process restart, delayed
   delivery, disconnection, duplicate reports, bounded queue exhaustion, storage
   failures and long-running teardown/leak checks.
4. Compare observable behavior with a pinned reference RTI, initially Portico.
   Normalize only permitted differences; preserve exception type, metadata,
   retraction identity, resulting state and required partial orders. A disagreement
   is investigated against requirements, not automatically copied from the peer.
5. Reuse the existing DEVStone-HLA harness to compare the frozen pre-change and
   candidate gorti versions. Hold FOM bytes, seed, processes, choreography,
   callback/accounting/logging settings and measurement boundaries constant.
   Use five warm-ups, thirty paired measured runs and balanced AB/BA ordering.
   Record completion time, throughput, latency distributions, memory and CPU.
6. Predeclare regression budgets before measurements. Proposed engineering gate:
   reject or investigate a paired 95% confidence interval demonstrating more
   than 5% completion-time degradation or throughput loss on a primary workload;
   investigate more than 10% p95 service-latency degradation. If inconclusive,
   collect more paired runs. These are project budgets, not IEEE requirements.
   Never disable a required semantic check to improve the result.

Exit: every required test in the release profile runs and passes, with no hidden
skip/xfail, unresolved race, leak, flaky failure or unexplained performance
regression. A reference RTI unable to run a scenario is recorded as such, not
compared against a different workload or counted as gorti proof.

Publish a per-profile matrix before final execution: Go/Python on Windows and
Linux; C++ on each advertised OS/compiler/ABI, including macOS if it remains a
supported release target. WSL is Linux evidence, not native Windows C++ evidence.
Unavailable hosts or sanitizer runtimes remain named blockers for their claimed
profiles; changing a release profile requires explicit review, not deletion of
failing tests. Investigate the current eight/seven Go skips individually: execute
required cases, or record a legitimate platform-specific applicability decision.

### M7: Release Evidence And Final Completion Decision

Priority: P0 release gate. Depends on M0-M6 and the settled source snapshot.

- Freeze source, binaries, dependencies, configuration, FOM/MIM inputs and test
  manifest. Rerun the whole applicable campaign on that exact release across
  declared operating systems and bindings.
- Extend the quality gate to ingest actual Go, Python and C++ results with
  explicit expected test IDs. Reject missing tests, stale source/manifest hashes,
  skipped required cases and missing oracle provenance; a manifest edit alone
  must never manufacture a pass.
- Keep mandatory conformance gates and project engineering gates distinct.
  Performance budgets and sanitizers are release-quality requirements, not
  invented IEEE clauses. The combined release decision requires both, while
  the report records which gate failed and why.
- Close each obligation with executable evidence or a reviewed non-executable
  verification argument. Resolve every mandatory/conditional applicability row;
  do not calculate conformance percentages from test counts.
- Update SRS, SDD, IDD, STD, public guides and examples from the same inventory.
  Keep raw execution logs outside Git and archive them with release hashes.
- Obtain independent review of the final coverage and claim wording. External
  organizational assessment and certification remain separate deliverables,
  contingent on an identified assessor or program accepting the actual RTI scope.

Exit: engineering completion has no open required item. Full-2010 wording is
allowed only when the normative inventory and its applicable evidence are also
complete; lack of the official texts blocks that wording, not ongoing coding.

## Dependency And Parallel Execution Plan

```text
R0 candidate inventory and shared contract freeze
  +--> R1 FDD model --> R2 explicit MIM --------+
  +--> R3 delivery ledger --> R4 MOM closure ---+--> R8 integrated semantics
  +--> R5 strict C++ facade -------------------+
  +--> R6 typed time --> R7 SDK completion -----+
  +--> R9 harness/environment preparation -----+--> R9 frozen release campaign
R0 normative closure -------------------------------------> final G0-G4 assessment
```

The arrows describe prerequisites for closure, not a ban on preparing tests in
parallel. R4's catalog/oracles can start before R3; R7's parity inventory can
start before time/MIM wire contracts are ready. R2 and R6 contract proposals are
reviewed together before either changes the shared protocol.

Use six bounded workstreams plus one integration owner:

| Workstream | Initial assignment | Write boundary / handoff |
|---|---|---|
| A: model | R1, then R2 model handling | FOM model/parser/merger/exporter; versioned DTO proposal to integration owner |
| B: delivery | R3 | Delivery accounting and focused tests; no independent changes to shared time types or generated wire code |
| C: MOM | R4 catalog, attribution and lifecycle | MOM services and tests; server/session integration through the integration owner |
| D: bindings | R5, then C++ part of R6/R7 | Strict/modern C++ front ends and official-header probes; preserve one shared backend |
| E: semantics | R8 independent oracles | Service contract tests and bounded disjoint fixes after ownership assignment; reviews B's invariants |
| F: verification | R0 trace review and R9 harness | Candidate ledger/evidence ingestion, sanitizer and cross-language environments; reviews D's contract evidence |
| Integration owner | R6 core contract, R2/R3/R4 wiring, Go/Python R7 | Shared core types, protobuf/version negotiation, application composition, lifecycle/snapshot coordination |

Before starting a batch, record exact file ownership and interface changes.
Workers submit tests before shared contracts change. Only the integration owner
regenerates protobuf outputs. Move Go/Python R7 implementation to freed workers
after their interfaces stabilize, without simultaneous edits to the same SDK.
Rotate reviewers; same-team review improves quality but is not external
organizational certification.

### Execution Batches

| Batch | Parallel work | Completion checkpoint |
|---|---|---|
| 0: contract preparation | R0 candidate reconciliation; R2/R6 protocol and profile decisions; R3 measurement boundary; R5 build feasibility; R9 environment preparation | Named rows, owners, proposed DTOs and observable oracles; unresolved normative assumptions are explicit |
| 1: highest-priority implementation | R1 FDD retention; R3 ledger; R5 strict binding; R4/R8 tests | Each package has meaningful RED-to-GREEN evidence and an independent review; integration uses frozen interfaces |
| 2: representation and lifecycle | R2 selected MIM; R6 exact time; R4 full lifecycle; R7 SDK forwarding | Cross-language model/time/restore scenarios pass; no silent downgrade or dropped inputs |
| 3: semantic closure | R8 expanded state/exception matrix; R0 final applicability; remaining R7 overload/callback work | Every applicable row has implementation plus a reviewed test or non-test argument |
| 4: release proof | R9 multi-host/profile/fault/race/sanitizer/reference/performance campaigns, followed by documents and review | Frozen candidate satisfies G0-G4 and project release gates; external assessment is separate |

The critical technical risks are R3's cross-stage accounting and R6's shared
time representation. Begin with bounded prototypes and negative tests before
broad changes. Re-estimate effort after the source review; unresolved corrections
and unavailable platforms are external dependencies, not development-time estimates.

Each milestone uses the same repeatable cycle:

1. Plan: select inventory rows, oracle, negative/boundary cases, dependencies and
   performance risks. Record unknown normative assumptions explicitly.
2. Do: demonstrate a meaningful failing test, implement the narrow change, and
   run focused tests. A compile error or already-correct behavior is not a
   claimed behavioral RED reproduction.
3. Review: a different reviewer checks source and test sensitivity; run live
   composition plus race/failure cases, not only mocks sharing implementation
   assumptions. Mutation checks should detect dropped fields and no-op services.
4. Reflect: turn findings into regressions, rerun after fixes, update inventory
   and evidence. Promote only after the milestone's exit criteria pass.

For each cycle, record selected row IDs, RED evidence, changed source identity,
focused/live results, review findings, fixes, performance impact where relevant
and the next decision. An already-correct case becomes added coverage, not a
fabricated defect. A failed or inconclusive check reopens the relevant package.
No required failing test may become a skip, xfail, exclusion or weaker assertion
solely to obtain a green gate. All raw logs remain outside the repository.

The second implementation batch has started this sequence with bounded R1,
R4, R7 and R8 changes and R3/R5 foundations. Continue from the remaining work
in its linked record, not by repeating already implemented features. Run core
experiments directly; auxiliary inventory or staging preparation must not block
research execution. Tests of changed service behavior remain part of the work.

## Final Completion Checklist

- Every mandatory and triggered conditional obligation has a reviewed outcome;
  every not-applicable decision has a normative rationale, not a missing feature.
- All required SDK/profile rows cover signatures, inputs, encodings, exceptions,
  post-state, callbacks, concurrency and persistence as applicable.
- Healthy supported cases have no fabricated values, successful no-ops,
  unexplained `unavailable` sources, dropped parameters or silent downgrade.
- Required tests execute on the exact candidate with zero fail/skip/xfail/missing
  cases; stable source, binary, dependency and manifest identities are recorded.
- Go race, applicable C++ sanitizers, resource-lifetime and performance budgets
  meet separately identified project release criteria.
- Current SRS, SDD, IDD, STD, guides and runnable examples match the release;
  evidence can be reproduced from archived inputs without relying on a dirty HEAD.
- Independent coverage review approves the G0-G4 self-assessment. Any external
  verification/certification wording requires its separate actual deliverable.

## Sources And Scope

The official pages identify the distinct framework/rules, federate interface,
and object-model specifications for the selected 2010 editions:
[IEEE 1516-2010](https://standards.ieee.org/ieee/1516/3744/),
[IEEE 1516.1-2010](https://standards.ieee.org/ieee/1516.1/3745/), and
[IEEE 1516.2-2010](https://standards.ieee.org/ieee/1516.2/3746/).
Checked 2026-09-20. Their public descriptions and the linked supplemental files
do not replace the full normative texts. The three user-supplied redlines and
the current-text review method are now registered in the September 21 report.

The pinned `rti/pkg/fom/mim/standard-mim.xml` supplies the attribute names and
public MIM descriptions used in M1-M3. See the existing source register and
quality-gate document for hashes and unresolved normative questions. Clause-level
decisions in this revision cite the supplied source review. No current external
certification-service availability is asserted by this plan.
