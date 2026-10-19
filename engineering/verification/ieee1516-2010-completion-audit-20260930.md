# IEEE 1516-2010 Whole-RTI Completion Audit

Date: 2026-09-30. Repository: `gorti`. Observed HEAD:
`518cb67f00858fcbec9ed86145d5b2360cfb2241`.

This is a source-based completion audit of a concurrently changing working tree,
not a release qualification or a certification. Only this new report and its
new companion inventory are owned by this audit. Production code, existing
evidence manifests, claim flags, and `docs/assets` are outside its edit scope.

Subsequent G06-G09 implementation, requested after this audit, is recorded in
the [separate synchronization milestone](ieee1516-2010-synchronization-completion-20260930.md).
The findings below describe the audited checkpoint, not a final frozen release.

## Early Verdict

**Completing the four currently assigned corrections is not sufficient for
whole-RTI conformance.** There are additional, concrete behavioral gaps with
active 2010 source support: MOM service execution, synchronization lifecycle,
DDM templates/realizations/copied regions, timed-save scheduling and replacement,
durable restore through the standard API, delayed subscription evaluation,
and management queue accounting. Empty targeted unsubscribe and the last-resigner
deletion rule also require attention.

These are finite engineering tasks. They must not be confused with the separate
question of whether every applicable normative obligation has been inventoried.
That whole-standard inventory is **not complete**. There is no defensible
percentage-complete or four-fixes-to-certification estimate at this checkpoint.

Integer64 production integration and unpublish ownership-loss handling are being
changed by the parent task. They are recorded as in-flight acceptance work,
not as unchanged missing implementations. The parent's reported TDD correction
for ownership loss is explicitly acknowledged below.

## Sources and Redline Method

All page numbers below are physical, one-based PDF pages, not printed page numbers.

| ID | Supplied local original | Pages | SHA-256 |
|---|---|---:|---|
| S0 | [IEEE 1516 redline](C:/Users/cbchoi/Downloads/1516_2010-2000_redline.pdf) | 41 | `6ac0b36add984f2f2e772b8c070f75e8172fd1cd3ab55dd65defe3066e46b086` |
| S1 | [IEEE 1516.1 redline](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf) | 588 | `789780a38898bd513fe1dbdb877433e6518dac8d0eb577056fc0bbe9e5b5a9d4` |
| S2 | [IEEE 1516.2 redline](C:/Users/cbchoi/Downloads/15162_2010-2000_redline.pdf) | 164 | `e0ec65cb99b9a9e47cb21cd1d003b78df76e18cddd5ab2f0fd1e3a60e5223f09` |

The three originals were hashed again and matched the local intake manifest.
Local page records are under
`D:/workspace/gorti/ieee2010-source-intake-20260921/`:
`1516-pages.jsonl`, `1516.1-pages.jsonl`, and `1516.2-pages.jsonl`.

The intake's `active_candidate` removes red-colored characters. It is a review
aid, not an authoritative clean edition. Retained black text and added blue text
were checked against original pages for the central findings. Original S1 pages
57, 59, 61, 63, 64, 95, 100, 191, 221, 222, 229, 288, 290, and 390 were visually
inspected during this audit. Red deleted passages were not treated as obligations.
The deleted region wording on page 222 must not override the active
template/specification rules.

The report paraphrases requirements and publishes locators, not licensed source
passages. Where retained and added language needs reconciliation, record that
specific interpretation issue and check the clean edition or applicable
correction. Do not use such a local ambiguity to declare the entire source
unavailable.

## Reconciliation with Existing Evidence

The [September 19 audit](ieee1516-2010-full-rti-audit.md) predates full source
intake. Its historical findings are not automatically current defects.
The [source review](ieee1516-2010-normative-review-20260921.md) resolves selected
normative decisions but does not inventory every obligation.
The [September 29 readiness report](ieee1516-2010-readiness-20260929.md) explicitly
leaves time, whole inventory, cross-generation SDK state, DDM realization, delayed
subscription evaluation, and MOM accounting work open.

The September 30 reports show actual subsequent progress:
[time selection and known classes](ieee1516-2010-time-selection-known-class-20260930.md),
[atomic ownership mapping](ieee1516-2010-ownership-atomic-20260930.md),
[DDM association epochs](ieee1516-2010-ddm-epochs-20260930.md), and
[exact time and callback receipts](ieee1516-2010-exact-time-callbacks-20260930.md).
Do not reopen the earlier absent dynamic-FOM, inactive-association, Float64
arithmetic, or in-memory-only Java checkpoint findings without new evidence.
Conversely, those bounded fixes do not discharge the gaps below.

Read-only inspection of `ieee1516-2010-evidence.json` found 66 requirement rows,
37 without a clause locator, and 198 test entries. Its normative coverage remains
false, its normative requirement-ID list empty, and its external evidence absent.
These are observations, not edits. Neither 198 passes nor a prior set of 43
evidence errors is a count of all normative obligations.

The parent's last reported campaign was 2,742 Go top-level tests, 12 race-package
checks, and 198 supported-gate passes. Associated reports also record broader
subtests and selected Java/Python campaigns. This audit did not rerun them and
does not present them as fresh results or whole-standard qualification.

### Service Census Is Not a Requirement Census

| S1 definitions | Service family | Definition count |
|---|---|---:|
| 4.2-4.32 | Federation management | 31 |
| 5.2-5.13 | Declaration management | 12 |
| 6.2-6.30 | Object management | 29 |
| 7.2-7.19 | Ownership management | 18 |
| 8.2-8.24 | Time management | 23 |
| 9.2-9.13 | Data distribution management | 12 |
| 10.2-10.44 | Support services | 43 |
| Total | Language-independent service definitions | 168 |

Each definition contains several argument, precondition, postcondition, exception,
and state-chart obligations. Common service-family rules, MOM tables, bindings,
encodings, FDD/MIM rules, and OMT constraints add further obligations.
The reported 172 Java signatures are a different API census, not proof that these
168 service definitions or their individual obligations are satisfied.

## Selected Completion Inventory

Status meanings:

- **Confirmed gap:** active source requirement and a concrete conflicting or absent
  production path were identified. Static findings still need executable negatives.
- **In flight:** parent changes are visible or reported; integrated acceptance is pending.
- **Assessment gap:** implementation may exist, but the required proof is not established.

This is a selected gap inventory, deliberately not labelled a complete normative
inventory. Each numbered item has a source locator, implementation observation,
and explicit acceptance criterion.

### G01: Per-Federation Integer64 Production Acceptance

**In flight.** S1 4.1.6 (p50), 12.4/Table 18 (pp338-340); S2 4.7.1 (p55).
The standard mappings require both supplied time representations. Generic
Integer64 managers, codecs, and persistence now exist. The observed
[application composition](../../rti/internal/application/server/run.go) also
contains Integer64 backends, typed save management, and an
`EnableInteger64`-controlled resolver. The older Float64-only-root verdict is
therefore stale. End-to-end acceptance of the production composition is still
needed; a switch alone is not that proof.

Acceptance: run concurrent Float64 and Integer64 federations in the assessed
server configuration. Verify creation/list/join factory identity; idle MOM time;
all five advances; queries; timestamped interactions, updates, deletion and
retraction; timed save; process restart and recreated-execution restore.
Use adjacent times above 2^53 and near 2^63-1, zero intervals, and overflow
negatives with no partial mutation. Test Go, Python, and the chosen Java binding.
Reject wrong representations before state changes. Proprietary Float64-only
optimized transports need explicit capability handling, not silent conversion.

### G02: Unpublish Guard and Ownership Loss

**In flight.** S1 5.3.3(f), 5.3.4(e) (pp94-95); 7.1.1 (pp155-156).
Ownership callback guards and prepare/commit ports are already present.
The parent reports that the actual TDD ownership-loss failure has been fixed
through declaration/object/ownership pins. Do not report ownership loss as an
unchanged missing implementation.

Acceptance: include a red-before/fixed-after ownership-loss case, then integrated
concurrency and failure tests. Only selected published attributes lose ownership.
The pending-callback guard covers qualifying attributes owned by another joined
federate, not caller-owned, unowned, or RTI-owned attributes indiscriminately.
Actual callback invocation, not enqueue or transport receipt, resolves the
applicable terminal obligation. Cover acquisition, cancellation, unavailable and
notification variants; ancestor-known mappings; explicit empty versus omitted
attribute sets; persistence; retry/duplicate acknowledgments; atomic reservation
failure; and reentrant callback service calls without lock inversion.

### G03: Empty Targeted Unsubscribe

**Confirmed gap.** S1 5.7 narrative and postconditions (pp100-101).
[Java ExchangeServices](../../javasdk/client/src/main/java/org/gorti/rti/ExchangeServices.java)
routes targeted unsubscribe through `removalAttributes`, which rejects an empty
set. [Declaration management](../../rti/internal/hla/declaration/manager.go)
also uses empty attributes to represent a whole-class removal. A valid explicitly
empty optional set is not the same operation as omission.

Acceptance: standard Java whole-class unsubscribe removes all subscriptions;
targeted unsubscribe with a supplied empty set succeeds without removing any.
Propagate presence through Go/Python and the wire boundary while preserving the
documented legacy omission path. Invalid class/membership still fails normally.
Verify no accidental advisory, discovery, subscription, or ownership change.

### G04: Global Delete and Resign Callback Ordering

**Assessment gap; parent changes in flight.** S1 4.10 (pp56-58), 6.14-6.15
(pp131-135), 7.1-7.16 (pp155-176), 12.6 (p340).
Deletion cleanup, mandatory ownership-receipt ledgers, and resign preparation now
interact across services. Parent changes include projection through the pinned
resign deletion plan. A historic nested-lock concern is not a newly reproduced
deadlock and must not be reported as one.

Acceptance: validate permitted partial orders rather than demanding one total
order copied from another RTI. Keep mandatory terminal ownership callbacks valid
while retiring object knowledge; suppress genuinely obsolete assumption work;
ensure callback-reentered services see the proper knowledge/ownership state.
Exercise all six resign directives, pending acquisitions/divestitures, authenticated
receipts, concurrent deletion/rediscovery, send failure, and save/restore.
Failed admission must leave membership, objects, ownership and queues unchanged.

### G05: Last-Resigner Deletion Rule

**Confirmed gap at inspected checkpoint.** S1 4.10 narrative (p57).
The active text requires deletion directive 2 for the last joined federate,
regardless of supplied directives. Inspected
[lifecycle resign](../../rti/internal/application/lifecycle/resign.go)
sets `deleteObjects` solely from the supplied action; the inspected federation
resign path did not add a last-member override.

Acceptance: a last member with delete privilege resigns using unconditional
divestiture; its deletable object and associated name/ownership/DDM/time state
must disappear. The federation execution/FDD remains until explicitly destroyed,
and a later join cannot rediscover that old object. Determine last membership
under the same lifecycle transaction, including simultaneous resign/join.
This is not a claim that every unrelated unowned object must be deleted.

### G06: Synchronization Membership and Label Retirement

**Confirmed gap.** S1 4.13 (p61), 4.14-4.15 (pp62-64).
[Synchronization management](../../rti/internal/hla/synchronization/manager.go)
captures the all-member set at registration, without lifecycle join/resign hooks.
Completed points remain in `points[label]` as `StateAchieved`.

Acceptance: join before all achievements adds and announces a member to an
all-member point; join after that boundary does not. Explicit nonempty subset
registration excludes unrelated later joins. Resign removes a member and can
allow synchronization to finish. Retire the registered label once the required
Federation Synchronized invocations have occurred, then permit reuse with a new
tag/set. Preserve historical reporting separately from active label registration.

### G07: Synchronization Negative Confirmation and Set Presence

**Confirmed gap, with one source interpretation issue.** S1 4.11-4.12 (pp59-60).
Duplicate registration emits failure but also returns a domain error; the
standard service's negative result is the registration-confirmation callback.
Non-nil empty sets in the manager take a different path from omitted sets.

Acceptance: duplicate registration produces the prescribed failed-confirmation
reason without a nonstandard synchronous duplicate-label exception or mutation.
Explicit empty and omitted sets both announce to all current members. Check the
precise late-join treatment against 4.13 wording. Page 59 also combines permission
for valid departed designators with a retained membership sentence; resolve that
specific tension against the clean edition/correction before choosing an oracle.

### G08: Synchronization Achievement Admission

**Confirmed gap.** S1 4.14.3(d), 4.14.5(a), 4.15 (pp62-64).
The manager can accept achievement for a registered point from a federate outside
its announced set, including inserting unsuccessful outsider achievements.

Acceptance: a joined but unannounced member receives the standard label-not-
announced exception, with no set/state change. The failed-to-synchronize set
contains exactly unsuccessful members of the synchronization set. Cover retries,
duplicate achievements, resign, and same-label reuse epochs.

### G09: Synchronization Callback Delivery

**Confirmed gap.** S1 4.12-4.15 (pp60-64).
The synchronization implementation ignores several outbox send errors and
commits state before best-effort emissions.

Acceptance: inject failures at the first and last confirmation/announcement/
synchronized recipient. No successful state transition silently loses a required
callback. Use reserved fanout or a durable retry obligation, with no duplicate
effect, no premature label retirement, and no grant of achievement eligibility
based solely on a callback that never reaches invocation.

### G10: DDM Region Templates versus Specifications

**Confirmed gap.** S1 9.1.1, 9.1.3.1(a,e,h,i) (pp220-223), 9.2-9.3
(pp229-230).
[CreateRegion](../../rti/internal/hla/ddm/manager.go) initializes committed full
ranges immediately. Neither initial per-dimension Set Range Bounds coverage nor
template/specification state is enforced by use/commit validation.

Acceptance: a newly created template cannot be used where a specification is
required. Partial initialization followed by commit raises InvalidRegion
atomically and retains pending values; all dimensions initialized without commit
is still a template; the first valid commit creates a specification. Later
partial modifications of that specification are allowed. Test the empty-dimension
case and every relevant DDM/support operation, and persist both states and pending
initialization coverage.

### G11: DDM Contextual Defaults and Excluded Dimensions

**Confirmed gap.** S1 9.1.2(d), 9.1.3.1(f,g) (pp221-222);
S2 4.6 and 6.2.6(4) (p84).
DDM materialization copies explicitly committed dimensions; FOM default metadata
does not drive contextual realization. [Dimension metadata validation](../../rti/pkg/fom/model/dimension_metadata.go)
checks lexical default syntax without the complete semantic range constraints.

Acceptance: realize a specification against each attribute/interaction's available
dimensions, adding applicable default ranges and excluding dimensions designated
excluded. An all-excluded empty realization overlaps nothing, including the default
region. Test proper subsets, per-attribute contexts, bounds at the upper limit,
invalid FDD defaults, empty-region versus default-region distinctions, modification
propagation, and save/restore.

### G12: Conveyed Immutable Region Copies

**Confirmed gap.** S1 9.1.7 (pp228-229).
Callback metadata currently conveys publisher region handles rather than immutable
copies of contextual update realizations. Java supplemental data exposes those
handles, and range queries reach mutable region state.

Acceptance: a receiving callback can query its conveyed designators using only
Get Range Bounds and Get Dimension Handle Set. Results equal the realization
used for overlap, including defaults, and remain unchanged if the sender commits
or deletes its specification while the callback is blocked. Mutating/reusing these
copies fails. Preserve copies until callback completion, not just stream receipt.
Represent present-empty default-region metadata distinctly from absent metadata.
Bound cleanup and checkpoint queued copies.

### G13: Delayed Subscription Evaluation

**Confirmed gap.** S1 8.1.8 (p191).
The FOM switch is retained, but inspected send-time candidate selection prunes
current non-subscribers. Later queue filtering cannot recover those candidates.
Existing enabled/disabled tests start with matching subscriptions and therefore
do not prove late-subscription behavior.

Acceptance: with the switch enabled, a joined recipient subscribing after send
but before TSO eligibility receives the applicable queued interaction/update.
Check subscription changes, inherited known classes, DDM realization changes,
FQR, retract and restore. Disabled evaluation permits the implementation to
evaluate earlier; do not invent a rule that disabled always must drop a message.
Keep the switch immutable according to federation creation rules.

### G14: Replacement of an Outstanding Save Request

**Confirmed gap.** S1 4.16 narrative and 4.16.4 (pp64-65).
[Save management](../../rti/internal/hla/save_restore/manager.go) rejects an
existing save entry even while it is only waiting for its scheduled time.

Acceptance: a valid new request replaces an outstanding, not-yet-initiated request
and cancels its time barriers; an invalid replacement leaves the old request
intact. A request after Initiate Federate Save correctly raises save-in-progress.
Cover timed-to-timed, timed-to-immediate, replacement near eligibility, and faults.

### G15: Timed-Save Requester Preconditions

**Confirmed gap.** S1 4.16.3(d), 4.16.5 (pp64-65).
The inspected save adapter/coordinator drops requester identity at the save-manager
call. Scheduled admission examines participants without proving that this
requester is regulating with a valid outgoing TSO timestamp, or has defined GALT
and a timestamp greater than that GALT.

Acceptance: retain authenticated caller identity to admission. Distinguish
requester-not-permitted, invalid timestamp and timestamp-already-passed exceptions.
Reject a nonregulating requester with undefined GALT even if other participants
could advance. Verify all constrained participants' logical times, equal-time
boundaries, zero lookahead, Integer64 overflow, and no state mutation on failure.

### G16: Per-Federate Scheduled-Save Initiation

**Confirmed gap.** S1 4.16-4.17 (pp64-66); 4.1.2 (p44).
The inspected scheduled-save path waits for a single all-participants-ready result
before broadcasting initiation. The source instead specifies eligibility of
each constrained participant, then initiation of nonconstrained participants when
all constrained participants are eligible.

Acceptance: independently advance two constrained members so the first receives
Initiate Federate Save at its own specified readiness boundary, without waiting
for the slower member. Distinguish TAR/NMR versus available/FQR strict boundaries.
Ensure all required earlier/equal TSO invocations precede initiation. Preserve
per-federate service admission and messages arriving from not-yet-saving members;
initiate nonconstrained members at the prescribed all-constrained boundary.

### G17: Standard-API Durable Recreated-Execution Restore

**Confirmed gap.** S1 4.1.2 (p44), 4.24-4.29 (pp73-79).
Same-execution replacement-JVM restore exists and is not being disputed.
[Cross-generation restore](../../rti/internal/hla/save_restore/cross_generation.go)
explicitly rejects nonempty private SDK runtime state. The standard label-only
request defaults to current-generation provenance; a custom saved-generation
argument is not the standard Java API.

Acceptance: save, stop the RTI/clients, recreate the execution, rejoin compatible
federate types, and restore by the standard label-only Java request. Resolve
saved provenance and rebind execution-scoped handles, callback identities,
known objects, retraction state and SDK queue state safely. Saved Integer64 values
remain exact. Wrong FDD/time factory/participant census, corruption and unknown
private formats fail atomically. Discard current messages and restore saved
messages at their required delivery states, without manual generation overrides.

### G18: MOM Receive/TSO Queue Lengths

**Confirmed gap.** S1 8.1.1 (p182), 8.1.7 (p189), 11.6/Table 6 (p297).
[Production MOM metrics](../../rti/internal/application/server/mom_metrics_sources.go)
count only waiting-RO time state; TSOQueued is not supplied through that path.
Ready outbox, streaming and client callback queues are not the same accounting
boundary as waiting time-manager entries. Missing metrics are not synthetic zeros,
but neither constitute implementation of required metrics.

Acceptance: follow one message identity through time queue, outbox, retry,
transport and SDK pending invocation, until invocation starts. Count reflection,
receive and removal messages by received order, not arbitrary control callbacks.
Test disabled callbacks, blocked callback bodies, duplicates, filtering, retract,
reconnect and save/restore without undercounting or double counting.

### G19: MOM HLAservice Execution Catalog

**Confirmed major gap.** S1 11.3(d) (p288), 11.4/Table 5 (pp289-294),
11.4.2 (pp290-291).
[MOM dispatcher](../../rti/internal/hla/mom/dispatch.go) registers switch, request,
catalog and timing handlers, but no HLAservice catalog. Unknown manager classes
return an empty result. The embedded standard MIM lists 32 HLAservice leaf classes
marked Subscribe; these invoke services on behalf of the designated federate.

Acceptance: one positive and one negative actual-server scenario for each of the
32 classes listed in the companion inventory. Compare target state, callbacks
and exceptions with a direct invocation by that target. A management sender must
not need to impersonate the target's transport token. Preserve target membership,
time factory, checkpoint state and optional-argument presence. Allow precisely the
MOM save/restore service interactions permitted by 11.4.2 while checkpoint service
admission otherwise remains enforced. Report errors rather than silent no-ops.

### G20: MOM Extension Dispatch

**Confirmed gap.** S1 11.3.1 (pp288-289), 11.4.1(l) (p290).
Catalog validation requires the exact predefined parameter count; dispatch uses
exact class names without promotion from valid subclasses.
The broader manager-prefix interception can swallow user-defined management
classes that are not RTI-consumed leaf descendants.

Acceptance: dispatch a valid subclass to its nearest predefined RTI-subscribed
leaf, process required predefined parameters, and ignore valid extension
parameters. Omitted optional HLAservice parameters preserve service omission
semantics. RTI-generated reports use predefined classes/parameters only.
Unrelated user management classes route normally, not as successful RTI no-ops.
Missing mandatory parameters still fail explicitly.

### G21: MOM Attribute Regional Routing

**Confirmed gap in the inspected attribute update path; report routing has partial
implementation.** S1 11.4.1(h-j) (p290).
[Instance updates](../../rti/internal/hla/mom/instances.go) and
[periodic metric updates](../../rti/internal/hla/mom/timing.go) use ordinary
UpdateAttributes without the required per-subject point update region.
There is already a dedicated regional report recipient calculation; do not call
all MOM regional routing absent.

Acceptance: regional subscriptions to HLAfederate attributes receive only the
subject selected by the normalized-federate point range, including late subscription
and periodic updates. Compare with normal global subscriptions. Separately verify
report subject filtering and the additional service-group dimension for service
invocation reports, including conveyed region-copy semantics and boundary points.
Audit Table 4-7 publication/subscription completeness instead of assuming the
partial catalog covers all MOM administration rules.

### G22: Java Standard Binding and Go/Python Callback Proof

**Assessment gap, plus the concrete targeted-unsubscribe defect in G03.**
S1 12.6 (p340), 12.8-12.11 (p341 onward), 13.2 (p390).
Forwarding every declared Java method does not prove argument presence, standard
exception types, callback overloads, data-element encoding or deployment/provider
behavior. Go/Python service-equivalence evidence remains important but those
bindings are not themselves the standard's Java/C++/WSDL mapping definitions.

Acceptance: compile an independently authored client against the unmodified
standard Java interfaces and the product provider; assess every applicable
overload, factory, handle, encoding and exception. Verify callbacks execute
serially on the evoking thread, enable/disable behavior, callback reentrancy
(excluding Connect, Disconnect, Join, Resign, Evoke and Evoke Multiple), and
disconnect/rejoin epochs. Compare Go/Python observations with the same source-
derived service oracle. Queue consumption and transport ACK are not substitutes
for ambassador invocation. EVOKED-only support is permitted; do not invent a
mandatory IMMEDIATE mode or a requirement to certify every shipped language.

### G23: Whole Normative Inventory and Qualification

**Assessment gap, not a missing production function.** S0 Clauses 5-6 (pp33-35);
S1 Clauses 4-13 and normative Annexes F-G (pp582-583);
S2 Clauses 3-7 and normative annexes as applicable, including 6.1-6.2 (pp82-84).

Acceptance: enumerate every applicable obligation and conditional trigger, not
only known failures. Classify RTI, federate, federation, model and tool subjects
separately. Map each applicable RTI obligation to reviewed implementation and
positive/negative/boundary/fault/concurrency tests as relevant. Resolve explicit
not-applicable decisions with source rationale. A zero-test, skipped, or manually
assumed row cannot close an implemented obligation.

S1 13.2 requires all specified joined-federate services and RTI callbacks, and at
least one standard language mapping. Its conformance label is tied to publicly
available verification results certified by a recognized authority. Internal
agent review is not externally independent assessment or that certification.
Availability of a recognized assessment program was not established in this
offline audit; no organization or certificate is invented.

### Original Clause Links

These links open the supplied original at the principal physical page for each
item; the item text gives additional pages and subclauses.

| Items | Principal active source |
|---|---|
| G01 | [S1 12.4 / Table 18](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=338), [S2 4.7.1](C:/Users/cbchoi/Downloads/15162_2010-2000_redline.pdf#page=55) |
| G02 | [S1 5.3.3-5.3.4](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=95) |
| G03 | [S1 5.7](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=100) |
| G04 | [S1 6.14](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=131), [S1 7.1](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=155) |
| G05 | [S1 4.10](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=57) |
| G06 | [S1 4.13](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=61), [S1 4.15](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=63) |
| G07 | [S1 4.11-4.12](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=59) |
| G08-G09 | [S1 4.14-4.15](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=62) |
| G10 | [S1 9.1.3.1](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=221), [S1 9.3](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=230) |
| G11 | [S1 9.1.2-9.1.3](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=221), [S2 6.2.6](C:/Users/cbchoi/Downloads/15162_2010-2000_redline.pdf#page=84) |
| G12 | [S1 9.1.7](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=229) |
| G13 | [S1 8.1.8](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=191) |
| G14-G16 | [S1 4.16-4.17](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=64) |
| G17 | [S1 4.1.2](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=44), [S1 4.24](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=73) |
| G18 | [S1 11.6 / Table 6](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=297) |
| G19-G20 | [S1 11.3-11.4](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=288) |
| G21 | [S1 11.4.1(h-j)](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=290) |
| G22 | [S1 12.6](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=340), [S1 12.11](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=342) |
| G23 | [S0 5-6](C:/Users/cbchoi/Downloads/1516_2010-2000_redline.pdf#page=33), [S1 13.2](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=390), [S2 6.1-6.2](C:/Users/cbchoi/Downloads/15162_2010-2000_redline.pdf#page=82) |

## How to Finish the Whole Inventory

The [new companion JSON](ieee1516-2010-completion-inventory-20260930.json) is a selected completion inventory with
`inventory_complete: false`. It must not replace the existing evidence manifest
or pretend to be the final normative matrix.

Use one row per independently testable obligation, with these fields:

| Field | Required meaning |
|---|---|
| Source | edition, original hash, clause/table/figure, page, paragraph or item |
| Active status | retained/added/deleted; original-page review and ambiguity resolution |
| Subject | RTI, federate, federation, FOM/SOM model, or authoring tool |
| Obligation | paraphrased behavior, mandatory/conditional status and trigger |
| Applicability | assessed binding/configuration; explicit source-based exclusions |
| Implementation | package/function and exact release/tree identity |
| Oracle | allowed outputs/partial orders, exceptions and forbidden side effects |
| Cases | positive, negative, boundary, concurrency, failure and persistence as applicable |
| Execution | exact binary/configuration/client identity; PASS/FAIL/SKIP and artifact provenance |
| Review | author/reviewer role and actual independence level |

Audit all common narratives and state charts in 4.1, 5.1, 6.1, 7.1, 8.1, 9.1 and
10.1; every subparagraph of the 168 definitions; MOM Tables 1-18; Java mapping
types and provider deployment; logical-time/data-element encoding; and normative
FDD/MIM/OMT relationships. S1 Annex G explicitly gives Clause 11 tables precedence
over discrepancies in the MIM XML. Neither XML census nor method census is the
normative final oracle.

S0 federation/federate rules are not automatically new RTI methods. Similarly,
S2 distinguishes model, tool and XML conformance. An RTI importing FOMs must obey
applicable model/FDD rules, but need not become an OMT authoring tool merely because
that tool has separate requirements. Deleted Ada mapping and DTD text do not
create current 2010 implementation obligations.

## Parallel TDD Completion Tracks

| Track | Items | Integration dependency |
|---|---|---|
| Exact time and ownership transaction acceptance | G01-G05 | Production composition and object-before-ownership pins |
| Synchronization lifecycle and callback delivery | G06-G09 | Membership lifecycle and actual callback invocation receipts |
| DDM model and realization semantics | G10-G12 | FOM metadata, object routing, SDK copied-region query lifecycle |
| Time/checkpoint behavior | G13-G17 | Exact time, per-federate delivery and versioned SDK state |
| MOM execution and accounting | G18-G21 | Typed target services, DDM and callback invocation accounting |
| Binding and whole-inventory assessment | G22-G23 | Independent source-derived oracles; final integrated release |

For each loop: Plan the exact active-clause oracle; Do a failing behavioral test
and scoped correction in the owning production task; Review source interpretation,
side effects and integrated regressions; Reflect by updating only the affected
obligation's evidence. This audit does not implement those corrections.

Do not infer elapsed implementation duration from this track table. In particular,
G23 requires a full source sweep and external qualification planning; completing
a finite list of behavior fixes does not mechanically complete it.

## Proof Boundaries and Final Disposition

Internal proof should pin a release candidate, source hashes, server configuration
and each SDK; replay source-derived tests through actual production endpoints;
exercise failure injection, concurrent lifecycle transitions and persisted
restart; and publish traceability/results without licensed passages or secrets.
Another RTI's outputs can supplement those tests but cannot replace the source
oracle or impose a total callback order where the standard permits alternatives.

An independently authored test suite or agent reviewer can improve internal
quality. Call it internal review unless organizationally independent assessment
really occurred. Recognized-authority certification and public verification
results remain necessary for the standard's formal conformance label; this
audit neither grants that label nor changes project claims.

**Disposition:** additional implementation work is genuinely required, beyond the
four in-flight corrections. Whole normative inventory coverage is unresolved.
No production changes, existing evidence edits, claim/certification toggles,
benchmark/test campaigns, downloads, commits, or `docs/assets` edits were made
by this audit. PDF inspection renderings remained in the external local intake.
