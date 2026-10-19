# IEEE 1516-2010 Conformance Execution Plan

Date: 2026-09-21
Updated: 2026-09-22
Status: In progress. The first bounded W1/W3/W4 implementation batch is recorded
in the [execution report](../verification/ieee1516-2010-first-batch-20260921.md).
A [second bounded batch](../verification/ieee1516-2010-second-batch-20260921.md)
continues W1/W4 with outgoing admission, zero-lookahead history, DDM and ownership
confirmation. Exact time representation (W2) and the complete normative inventory
remain open.
A [third bounded batch](../verification/ieee1516-2010-third-batch-20260922.md)
addresses pending GALT and strict range admission. The
[RO queue work](../verification/ieee1516-2010-ro-queue-20260922.md) now implements
separate waiting/release state, save/restore v4 and application-owned retries.
Fresh Windows/Linux Go and Python campaigns, including Linux race checks, passed
and are recorded there; earlier test counts are not reused as evidence for the
new code. The [order and advance batch](../verification/ieee1516-2010-order-advance-20260922.md)
implements profile-independent FOM preferred order, finite advances without
either time mode and same-current-time TSO waiting/target selection. Fresh
Windows/Linux Go and Python campaigns passed, including Linux race checks;
snapshot/restore and membership guards have separate verification scopes.
The [flush, activation and retraction batch](../verification/ieee1516-2010-flush-enable-retract-20260922.md)
implements the next FQR, safe engagement and retraction scope. Its own report
records passing Windows/Linux Go and Python campaigns, including Linux race
checks. Current-subscription reevaluation and actual-delivery target selection
are covered for FQR/NMR/NMRA. Full delayed subscription evaluation and
cross-stage MOM accounting remain open alongside the residuals listed below.
No whole-milestone or full-conformance completion is implied.
Branch: `codex/ieee2010-feature-completion`
Priority: Go/Python behavior and server correctness first; Java is the selected
standard language mapping. C++ feature completion is deferred.

## Scope And Authority

This is the current execution sequence for the remaining work. It supersedes
the batch order and historical status tables in the
[feature-completion plan](ieee1516-2010-feature-completion-plan.md), while
preserving its R0-R9 work-order and M0-M7 milestone references.
W0-W9 below are project milestones, not IEEE clause numbers.

Use the [source-based review](../verification/ieee1516-2010-normative-review-20260921.md)
for source identities, clause locators and applicability decisions, and the
[Python-first corrections](../verification/ieee1516-2010-python-behavior-20260921.md)
for the latest implemented behavior. A corrected bounded finding is not reopened
merely because an older report still lists it as a gap.

Target all applicable RTI obligations of IEEE 1516-2010, 1516.1-2010 and
1516.2-2010. Separate RTI, federate, federation-author and model-tool duties.
Read retained/added 2010 redline text, excluding deleted older-edition text.
Confirm applicable corrections; obtain a clean base passage only where a
specific redline ambiguity remains. Store source locators and paraphrases, not
licensed source publications, in Git.

Keep three outcomes distinct:

1. Python-first engineering closure: corrected server behavior and complete
   advertised Python service paths, with Go compatibility regressions.
2. Full internal RTI assessment: every applicable obligation, including the
   selected IEEE Java language mapping, has reviewed release-specific evidence.
3. External assessment/certification: a separate actual report or certificate
   supports only its stated scope. Internal or multi-agent review is not this.

The [quality gates](../verification/ieee1516-2010-quality-gates.md) remain the
claim policy. Do not use the standard's "HLA Conforming" label based on an
internal test campaign. No current certification-service availability is assumed.

## Starting Point

These are historical starting-point results from the original plan, not tests
rerun while updating it. Later implementation reports record their own scopes;
the counts below are not results for the RO queue or order/advance changes.

| Evidence | Recorded result | Boundary |
|---|---|---|
| Windows Python-first campaign | 1,221 passed, no skips | One C++ prerequisite case excluded explicitly; not a full standard binding assessment |
| Windows Go and Linux Go race campaigns | Each: 73 tested packages, 2,221 top-level tests; no skips/failures; no Linux race warnings | Same tests on two platforms, not 4,442 unique tests |
| Linux new Python cases | 47 passed against a real race-enabled server | Not the full Python campaign; new paired participants share one interpreter |
| Bounded Go evidence manifest | 137 groups, 578 required test-name entries; 42 resolved records and one unresolved record | Not an exhaustive normative requirement inventory |
| Full readiness | Coverage incomplete; no registered exhaustive normative rows or independent evidence | No conformance percentage or whole-standard PASS |

Retain the completed fixes: future-TSO safety under asynchronous delivery,
regulating-only RO reception, constraint-disable retry/drain, LITS including the
currently computed GALT, received retraction metadata, positive exact update-rate
admission, and additive object-region subscriptions with per-region options.
Their remaining surrounding requirements appear below.

## Milestones

| ID | Work / legacy mapping | Prerequisite for closure | Required result |
|---|---|---|---|
| W0 | Exhaustive normative inventory and applicability / R0, M0/M7 | Reviewed source sections and current implementation disposition | Every normative section, service, callback, error, encoding and conditional rule accounted for; no unresolved applicability at final assessment |
| W1 | Time-management state and delivery semantics / R8, M4 | Reviewed time requirement slice from W0 | Source-derived boundary tests and live Python callback ordering pass |
| W2 | Exact standardized time representations / R6, M4/M5 | W1 state contract; reviewed wire/persistence design | Integer64 and float64 work end to end without lossy conversion or silent downgrade |
| W3 | Federation, FDD and MIM admission/lifetime / R1/R2/R8, M1/M4 | Reviewed federation/model requirement slice | Valid composition accepted, forbidden changes rejected atomically, execution-lifetime state correct |
| W4 | DDM, object knowledge and ownership / R8, M4 | Reviewed DDM/ownership slice; W3 dimension contract | Routing, discovery, exceptions and multi-attribute outcomes match the reviewed rules |
| W5 | Pending-message accounting and MOM / R3/R4, M2/M3 | W1 received-order rules; W2 shared protocol and W6 invocation contracts | Correct pending totals through callback invocation and complete applicable MOM behavior |
| W6 | Remaining service states and Python API/callback coverage / R7/R8, M4/M5 | Tests can start now; final pass after W1-W5 | No unexplained missing operation, ignored argument, wrong exception or callback in the declared Python profile |
| W7 | Complete IEEE Java language mapping / R5, M5 | Stable service/time contracts; shared Go/Python correction dependencies closed | Official-interface clients compile, link and execute the entire selected Java mapping |
| W8 | Integrated release evidence and internal assessment / R9, M6/M7 | W0-W7 complete for the candidate | G0-G4 satisfied, all applicable rows supported by current evidence, reviewed claim and documentation |
| W9 | External assessment, when available / M7, G5 | Assessor agrees RTI/2010 scope; completed dossier before final submission | Actual scoped report/certificate; otherwise retain the internal-assessment wording |

Only W9 depends on securing an external assessment arrangement. Contact and
scope preparation can start earlier; lack of an arrangement must not halt W0-W8.
W7 selects Java under the [Java binding plan](ieee1516-2010-java-binding-plan.md).
Its transport, type and independent-client work may proceed in parallel with
Go/Python development. Final closure still depends on the shared backend work;
C++ completion is not a prerequisite for this selected mapping.

### W0: Build The Complete Requirement Denominator

- Inventory all normative sections and tables, not only discovered methods or
  sentences containing "shall". Include definitions that constrain behavior,
  pre/postconditions, exception conditions, callback obligations and conditional
  mappings. Mark informative material and other responsible subjects explicitly.
- Use one stable ID per independently verifiable obligation. Record source hash,
  clause/table/page, paraphrase, responsible subject, applicability/trigger,
  implementation, binding/platform, verification method, test/review IDs,
  evidence identity, reviewer, status and dependencies.
- Separate implementation status from proof status: known defect, source-reviewed
  gap needing reproduction, unverified behavior, implemented/tested subset,
  verified obligation, and justified non-applicability. "Unsupported" is not a
  justification for excluding a mandatory obligation.
- Reconcile the existing FM/FO/TM/OD/CP findings against the latest corrections.
  Continue through synchronization, save/restore, declaration, support and all
  remaining MOM/encoding rules. New findings create work; they do not disappear
  because W1-W7 already have names.
- Separate engineering checks from normative obligations. Do not give an `ENG`
  regression a fabricated clause to make the evidence script accept it.
- Extend existing evidence ingestion only where needed for actual Python/Java
  execution and legitimate inspection evidence. The current Go-only manifest and
  script are not an all-binding dossier, and currently require executed tests for
  every applicable row. Reviewed non-executable obligations need an explicit
  verification method, not a dummy passing test or blanket waiver.

Exit: a second reviewer checks section-by-section completeness and every
applicability decision. W0 supplies reviewed slices throughout development; its
final closure is required before W8, not before the first defect correction.

### W1-W2: Time Semantics Before Representation Migration

W1 acceptance cases:

- Regulating/constrained/asynchronous-delivery combinations, idle versus pending
  advancement, RO eligibility, same-current-time messages and future TSO.
  Include the nonregulating, nonconstrained requester: 8.1.3 and 8.1.6 permit
  advancement without requiring either time mode. Establish membership at the
  service boundary rather than treating time engagement as membership.
- TAR, TARA, NMR, NMRA and FQR: required messages precede grants; allowed same-time
  partial orders follow the located rule rather than one observed schedule.
- Zero lookahead: strict/inclusive send bounds per advance mode and grant history,
  including consecutive same-time requests. Do not encode these with an arbitrary
  fixed floating-point tolerance. Also reject a representable timestamp below a
  positive-lookahead floor even when the difference is less than `1e-9`.
- Requested versus actual lookahead, rejected modification while advancing where
  specified, gradual reductions, and snapshot/restore of the relevant state.
  Hold an advance pending with another regulating federate and assert the pending
  state before testing modification; a single participant may grant immediately.
- Derive GALT's greatest available bound independently from the reviewed rules,
  including a genuinely pending peer TAR whose requested horizon exceeds its
  current-time horizon. Consistency among GALT, LITS and MOM is necessary but
  does not excuse mutually consistent, overly conservative results. Test peer
  resign, early grant, failed delivery admission and retraction before grant.

Use existing float64 cases to establish the state machine first. A worker may
design W2's types and independent vectors in parallel, but must not concurrently
rewrite the same time-manager files. Check concurrent send/advance/lookahead
linearization and lock ordering explicitly; a race-detector pass alone does not
prove an atomic observation. Never hold service-state locks across user callbacks.

#### RO Queue Work And Remaining Closure

The current implementation has separate per-federate RO and TSO queues. RO
reflections, interactions and removals become eligible when the receiver is
nonconstrained OR advancing OR asynchronous. An original timestamp remains in
the callback but is not an RO release bound. Discovery and control callbacks
bypass this gate. All still-applicable queued RO messages must precede TAG for
each advance mode, including after a grant was initially blocked by a peer.

Initial recipient fanout remains atomic. Releasing an accepted backlog larger
than transport capacity uses bounded prefixes and retains the unsent suffix.
Failed release preserves pending requests and withholds TAG. An application-owned
worker resumes marked work under checkpoint and generation admission, without
requiring the caller to repeat an advance. Asynchronous-enable and
constraint-disable keep their new setting under capacity pressure and finish
draining in the background; other drain errors roll back the flag. Review these
caller-visible distinctions explicitly, including a recovered RPC error followed
by the original request's grant.

Time-state v4 persists RO queues with original timestamps, payloads, tags and
metadata; v1-v3 compatibility remains. Restore validates typed references,
rebinds federate identities and replaces live queues before resuming at the
terminal identity/epoch boundary. Scheduled-save readiness includes eligible
RO release. These changes do not require new RPCs or a new bundle manifest.

The release filter rechecks current reflection/interaction subscriptions and
retains removal semantics for previously discovered objects. This does not close
8.1.8: the enabled Delay Subscription Evaluation Switch also requires retaining
candidates excluded by send-time subscriptions. Retaining region identities does
not establish the full DDM region-extent semantics for delayed delivery. The
bounded preferred-order and advance changes below are separate from the
historical explicit-order RO scenarios.

Closure requires public-path retry under bounded production outbox capacity,
no duplicate prefixes or early TAG, mixed RO/TSO metadata, subscription changes,
save/rejoin/restore identity replacement and worker teardown tests. The RO report
records that integration scope and its settled Go/Python and race results. Keep
the new order/advance results in the separate report below. Do not infer a
performance non-regression or whole-W1/IEEE completion from focused tests.

#### Bounded Order And Advance Batch

Implemented scope, with fresh Windows/Linux regression results in the
[new report](../verification/ieee1516-2010-order-advance-20260922.md):

- FOM Receive/TimeStamp preference applies across all federation profiles;
  explicit attribute-instance and publisher interaction-class overrides take
  precedence. Mixed updates preserve per-attribute order, and deletion uses
  `HLAprivilegeToDeleteObject`. Untimestamped sends remain RO; original timestamps
  on RO callbacks do not impose TSO eligibility.
- Joined callers may use finite TAR, TARA, NMR, NMRA and FQR with neither time
  mode enabled. Unconstrained callers are not bounded by GALT and do not use
  incoming messages to select an earlier next-message target. Target,
  pending-request and scheduled-save rules remain applicable.
- Constrained same-current-time TSO remains queued in the time-granted state,
  even with asynchronous delivery enabled. NMR/NMRA target selection includes
  queued messages at current time and releases them before the resulting TAG.
- Separate snapshot/restore cases retain advance-only logical time and held
  same-current-time TSO without emitting callbacks during import. Separate
  production membership cases reject unknown/resigned callers before mutation;
  accepting a manager call without time modes does not prove membership safety.

Do not move new results into the historical RO report or reuse its campaign
counts. The new report holds passing final execution results, including Linux
race checks. These corrections do not complete W1.

#### Residual Priorities

The remaining review findings are not closed by the bounded batch. Priority is
execution order, not a reduction in the required conformance scope.

| Priority | Residual | Required follow-up |
|---|---|---|
| Current batch | FQR, safe mode activation and retraction | See the separate flush/activation/retraction record for implemented behavior and current verification, including delivered callbacks and deletion ownership |
| P1 | Corrupt past-TSO invariant | Reject invalid queued TSO earlier than current logical time, including restored state, without delivery or time regression |
| P2 | Full delayed subscription evaluation and DDM region extents | Retain eligible send-time-excluded candidates when required and establish region-extent semantics beyond retained region handles |
| W2 | Exact time and `+Inf` | Keep standardized representations, final values and infinity behavior separate from finite float64 state-machine evidence |

#### W2 Acceptance Cases

- Select and propagate time kind at federation creation, join and factory access.
  Keep integer time and interval arithmetic exact through send/update/delete,
  comparisons, LBTS, grants, lookahead, retraction, MOM and Python callbacks.
- Test `2^53 - 1`, `2^53`, `2^53 + 1`, adjacent large values, initial/final values,
  legal interval bounds and overflow. Distinct integers must never coalesce via
  `double`, JSON numbers or a float-based snapshot DTO.
- Define standardized finite float64 final time separately from internal/product
  infinity sentinels. Test epsilon and arithmetic when an addition would round
  back to its operand; follow the reviewed mapping rather than host defaults.
- Exercise exact encodings with independent byte vectors; mixed-kind and
  unsupported-factory requests fail before mutation with the correct error.
- Carry types through immediate/timed save, restart, restore and historical log
  replay. Specify old float64 fixture interpretation and snapshot versioning.
- Preserve existing field numbers and float64 call forms; use reviewed additive
  version/capability handling where wire changes are needed. An old peer must not
  ignore a new time kind and claim success. Regenerate all language outputs under
  one owner, even while C++ feature work is deferred.

Exit: both representations pass the same applicable state-machine scenarios
through a real server, plus legacy float64 compatibility. A local time factory
test alone does not close W2.

### W3-W4: Model Admission And Service State

W3 starts with FM02/FM10 and FO04-FO10 from the source review:

- Standard-facing create requires the reviewed module inputs and default MIM
  behavior. Keep any empty-model developer extension clearly separate.
- Reject extra attributes/parameters added to an already complete class; retain
  legal new classes/subclasses and same-call completion of scaffolding.
- Accept permitted description-only differences and MIM extensions, while
  rejecting prohibited redefinitions and operational conflicts.
- Test namespace, context, singleton cardinality, trailing document content,
  default dimension ranges, partial tables and runtime switches against schema
  and independently authored semantic cases.
- Verify last-member object cleanup and all resign directives while retaining
  federation-lifetime FDD. Failed joins must not change handles, model, members,
  MOM publication or saved state. Include concurrent join/query/restore.

W4 starts with OD03/OD04/OD07/OD11/OD12 and the latest remaining-work list:

- Proper-subset dimensions and omitted-dimension routing; additive interaction
  regional subscriptions; active/passive and ordinary/regional independence.
- Immediate discovery on a qualifying late regional subscription, without an
  artificial update; local deletion followed by allowed rediscovery.
- Named and unnamed update-rate queries, with prescribed results/errors rather
  than an unexplained internal error.
- Ownership visibility and exceptions for unknown objects, undefined attributes,
  publication state and combined invalid inputs. Only assert precedence when the
  standard determines it; preserve allowed alternatives otherwise.
- Acquisition/divestiture/cancellation with callbacks in transit, competing
  requests, tags and all-or-nothing mixed-attribute failure.

Exit: state, recipients, metadata, errors and rollback are observed through the
production service path and Python binding; restore/reconnect preserves the
correct state. Preserve the already-fixed additive object-region behavior.

### W5-W6: Invocation Boundaries And Remaining Services

Define the callback contract early in W6, before W5 finalizes accounting:
queued, admitted, invoked, completed, suppressed and invalidated are distinct
states. Inspect Python's actual dispatch paths, callback disabling/enabling,
re-entry rules, exceptions, session fencing and teardown.

The existing Python outcome reports are emitted after callback return, may be
capacity-limited, and do not cover automatic invocation reporting for the async
event iterator. Define the boundary for both the standard-style ambassador and
the lower-level iterator; do not treat iterator enqueue as user callback invocation
or reuse a completion receipt as proof of invocation-start accounting. Existing
receipt loss must not silently turn a healthy-profile metric into a false zero.

For MOM pending messages, count applicable reflection/interaction/removal
messages once per recipient until the reviewed delivery/invocation boundary.
Do not count discovery, grants or ownership callbacks as RO/TSO messages. Queue
handoffs must not add/remove an outstanding item; invocation, not callback return,
is the terminal boundary. Preserve sent order separately from received order.

The RO queue implementation supplies a narrower current metric: messages still
held in the time-domain RO queue, including unsent retry work. MOM reads it from
the time snapshot, not from all outbox envelopes; a custom source is used only
without a time manager. It excludes already released transport/SDK messages and
therefore does not close the end-to-end W5 accounting requirement above.

Tests must observe known pending sets in server queues, outboxes and SDK queues,
including a blocked callback and queries at the invocation boundary. Verify
conservation through cancellation/retraction, batching, failed fanout, duplicate
or stale receipts, reconnect, resign and restore generations. Define a coherent
query observation boundary; eventual telemetry alone is not proof of an exact
count. Prefer cumulative/batched accounting; no per-message blocking confirmation
is introduced merely to produce a metric.

Complete MOM's applicable catalog from the pinned MIM and reviewed Clause 11:
queries/reflections, periodic/requested updates, controls, service reports,
exceptions, identity and lifecycle. Distinguish legitimate undefined values from
missing implementation. Existing outbox diagnostics are not complete totals.

W6 also closes the remaining normative rows for declaration, synchronization,
save/restore, support, encodings and SDK translation. Audit every advertised
Python operation/input/error/callback, not only method presence. Cross-service
cases include timed save during advancement, queued TSO and retraction after
restore, ownership transfer during resign, synchronization participant failure,
dynamic FOM rollback and reconnect with disabled callbacks.

Assign these W6 subpackages explicitly when workers become available:

| Subpackage | Owner / boundary | Required additional integration cases |
|---|---|---|
| W6-SAVE | Integration owner with a dedicated save/restore worker; exclusive `save_restore` files, shared `savebundle` changes serialized | Multi-participant success/failure/abort, retry and storage failure; restore one consistent FOM/MIM, ownership, regional, time and callback state |
| W6-DECL/SUPPORT | Routing/ownership worker after W4; exclusive declaration/support files | Advisory switches, inheritance, ordinary/regional independence, update-rate queries, knowledge-scoped handles and error identities |
| W6-SYNC | Federation worker after W3; exclusive synchronization files | Registration/achievement, joining and resigning participants, failed participants, membership snapshot rules and callbacks |
| W6-PY | Python/live worker | All service forwarding, encodings and callback models, including re-entry, suppression, errors and generation fences |

Do not expand concurrent writers to the shared object package, snapshot DTOs or
Python dispatch files merely to increase the worker count.

Exit: each required row has a reviewed observable oracle or an appropriate
inspection argument; successful no-ops and shared-code-only oracles are rejected.

### W7: Java Standard Binding Alongside Go/Python

Implement the IEEE 1516.1-2010 Java mapping as the selected standard binding,
following the [staged Java plan](ieee1516-2010-java-binding-plan.md). Go/Python
remain the primary product interfaces. Preserve the existing C++ implementation
and evidence, but do not make further C++ work a prerequisite for Java or count
its excluded tests as Java evidence. WSDL is not an additional selected target.

Use the pinned official `hla.rti1516e` API, with implementation classes outside
that package. Compile and run independent clients against the external API
baseline; capture JAR/class origins and any modern-JDK factory-loader adaptation.
Verify service overloads, checked exceptions, handles and collections, encodings,
RTI/time factory discovery and replacement, callback models and reentrancy on
each claimed runtime. Map actual server events to callbacks; successful enable
RPCs must not synthesize activation callbacks. A Java gRPC/product SDK foundation
does not complete the standard facade.

Exit: the complete selected mapping is verified, including live two-time-kind
execution and Java/Go/Python interoperation; no required Java path is hidden by
a skip, generic stub or custom replacement API. Until then, a bounded Go/Python
milestone may close but the full internal RTI assessment cannot.

## Parallel Schedule And Ownership

```text
W0 requirement inventory/review -----------------------------> W8
 |--> W1 time state --> W2 exact-time integration -----+
 |--> W3 FOM/MIM/lifetime -----------------------------+
 |--> W4 DDM/ownership -------------------------------+--> W6 closure
 |--> W6 Python callback contract --> W5 MOM/accounting+      |
 +--> W7 Java foundation/types/mapping ----------------------+
                       W0-W7 release evidence ---------------> W8 --> W9
```

The diagram is a work sequence, not permission for concurrent writes to shared
files. W5 needs W1 and W6's dispatch contract plus agreed shared W2/W5 wire and
persistence contracts. It does not need to wait for all integer64 implementation;
accounting and typed-time work may overlap within their exclusive file boundaries.

| Workstream | Initial assignment | Write boundary / coordination |
|---|---|---|
| A: requirements/review | W0 and independent oracle review | Requirement records and evidence summaries; no implementation edits during review |
| B: time | W1, then W2 domain algorithms | Time package and focused tests; shared type migration with integration owner |
| C: model/federation | W3 | Model/parser/MIM/exporter; lifecycle/composition edits handed to integration owner |
| D: routing/ownership | W4 | DDM/ownership and focused tests; object-package ownership agreed before changes |
| E: Python/live verification | W6 contract/tests, then bindings | Python SDK and distinct tests; one owner for connection/event dispatch files |
| F: accounting/MOM | W5 design/oracles, then implementation | Delivery and MOM packages after callback/time contracts are agreed |
| Java binding work | W7 J0-J6 as capacity permits | Java-only source/tests with exclusive owners; shared wire/session changes handed to integration owner |
| Integration owner | W2 shared types, wire, composition and persistence | Only owner regenerating protocol outputs; serializes shared DTO, server and restore changes |

Use up to six bounded workers plus integration when tools/resources permit.
Assign exact files before each batch, not just directory names. A separate
reviewer checks each change after implementation; same-team review is not external
certification. W7 may run alongside Go/Python work within Java-only boundaries;
it must coordinate, not compete, for shared SDK/protocol changes.

### First Batch

1. A creates the complete section index and classifies the known findings; begins
   the time/model/DDM requirement slices without waiting for the entire inventory.
2. B establishes source-derived oracles for idle RO, same-time, zero-lookahead
   and lookahead/GALT behavior, reproduces failures where present, and implements
   W1 corrections in small independently reviewed changes. Same-time and
   pending-horizon observations are not preclassified as reproduced defects.
3. C reproduces forbidden complete-class growth and description-only rejection;
   adds legal new-class/scaffolding controls before W3 fixes.
4. D adds proper-subset dimensions, additive interaction subscription and divest
   exception cases before W4 fixes.
5. E prepares independent-process Python observations and W6 callback contracts;
   F prepares message-accounting invariants and measurement-boundary tests.
6. Integration reviews the W2 time/wire/persistence design. Implementation starts
   only after the overlapping W1 state changes are integrated.

The first batch does not claim all W0-W6 milestones complete. Its closeout names
the exact corrected obligations, unresolved cases and next integration dependency.

## Plan, Do, Review, Reflect

Apply this loop to every bounded requirement set, then to the integrated milestone:

1. Plan: name the clause, applicability, oracle, positive/negative/boundary cases,
   affected files, compatibility risk and observable completion condition.
2. Do: demonstrate a behavioral failure before fixing a known gap, implement the
   smallest owning-layer change and run focused/live tests. An environment failure
   is not behavioral RED; an already-correct case adds coverage, not a fictitious fix.
3. Review: another reviewer checks source interpretation, state transitions,
   failure atomicity and test sensitivity. Use independent vectors and targeted
   mutations to detect a removed operation, dropped field or early callback/grant.
4. Reflect: fix findings, rerun the affected paths and update the requirement
   disposition with evidence. Unresolved findings keep the milestone open.

Never obtain closure by weakening a required assertion, converting a failure to
a skip/xfail, counting test collection as execution, or relabeling a source review
as a runtime pass. Partial ordering is asserted only where the standard requires
it; reference-RTI equality is not the oracle for every permitted schedule.

## W8-W9: Integration And Completion Evidence

- Reuse existing Go, Python and official-interface campaigns. Run all applicable
  Python tests on Windows and Linux, not only the new Linux subset; run full Go
  regressions and Linux race checks. Add actual separate-process Python/Python
  and Go/Python federates, adding Java/Java, Java/Python and Java/Go for W7.
  Use N=2 for basic ordering and N=3/4 for competing peers.
  Larger scaling/soak runs are engineering evidence, not IEEE-mandated sizes.
- Test supported transport/callback modes, reconnect, queue pressure, partial
  failures, process restart and save/restore. Do not force identical callback
  totals where a reviewed best-effort mode permits loss.
- Compare selected identical FOM/scenario inputs with a pinned reference RTI as
  diagnostic evidence. Investigate disagreements against the source, not by
  copying the reference implementation. This is not cross-vendor wire compatibility
  or full RTI certification.
- Preserve wire/log/save-bundle compatibility fixtures and old float64 clients.
  Run selected-binding platform/build/runtime checks and applicable engineering
  sanitizers. Performance and race criteria remain separate from normative rules.
- Reuse DEVStone-HLA for hot-path changes with matching semantics, FOM bytes,
  seed, processes, choreography, callback/logging conditions and measurement
  boundaries. Retain the existing proposed five warm-ups, 30 paired measurements
  and balanced AB/BA order; review paired confidence intervals and regressions.
  A faster invalid behavior is not an acceptable baseline or optimization.
- Capture the actual source including dirty/untracked relevant inputs, binaries,
  tools, configuration, expected/executed case IDs, verdicts and source/artifact
  hashes. Run focused development tests directly; final settled-candidate evidence
  is not a prerequisite staging gate for every research experiment.
- Keep raw outputs under an external run directory, for example
  `D:/workspace/gorti/ieee2010-conformance-artifacts/<run-id>/`, outside the nested
  Git repository. Commit tests, scenarios, obligation records and concise reviewed
  summaries, not raw logs or licensed publications. Retain enough artifacts for
  independent reproduction; a dirty HEAD identifier alone is insufficient.
- Reconcile SRS, SDD, IDD, STD, user documentation and examples with the candidate.
  Obtain a complete independent coverage review before the internal assessment;
  an automated `full-2010` prerequisite check cannot establish interpretation or
  semantic truth. No automatic CI policy change is part of this plan.
- Prepare an external dossier and confirm the actual subject, edition, binding,
  methods, independence and deliverable with a potential assessor. Obtain its
  final report/certificate before using the corresponding external claim.

W8 fails if any applicable requirement is unimplemented, unverified, failed,
silently skipped or has unresolved applicability. A non-executable obligation may
close only with its specifically reviewed inspection/analysis evidence. Every
piece of release evidence must match the same assessed candidate/profile.

## Progress And Re-Estimation

Report after each loop: obligations newly verified, reproduced defects fixed,
open defects, unverified rows, unresolved applicability, stale/missing evidence,
and milestones blocked by shared work. Test pass counts remain separate.

Do not give a whole-standard completion percentage before W0 establishes a
reviewed complete denominator. Once available, report mandatory and triggered
conditional obligations separately, with non-applicable decisions visible.
These coverage figures are not a probability of correctness or certification.

Re-estimate effort after the first batch provides the section inventory, a W1
closeout, and a reviewed W2 migration design. W2 (time representation), W5
(cross-stage accounting) and W7 (standard binding) are the largest current risks.
Additional normative findings may expand the work; external assessment lead time
is estimated separately after an assessor confirms availability and scope.
