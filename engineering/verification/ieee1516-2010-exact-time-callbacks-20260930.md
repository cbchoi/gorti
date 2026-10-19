# Exact Time Layers and Ownership Callback Lifetimes

Date: 2026-09-30
Status: implementation increment, not complete IEEE 1516-2010 conformance

## Scope

This record supersedes the implementation boundaries, not the historical results,
in [the preceding DDM/knowledge-epoch record](ieee1516-2010-ddm-epochs-20260930.md).

| Area | Current implementation | Remaining boundary |
| --- | --- | --- |
| Object knowledge | Server and SDK object-knowledge epochs; fenced local deletion; obsolete ownership-assumption suppression | Exhaustive callback/lifecycle coverage across every binding |
| Ownership results | Independent authenticated invocation receipts; retained acquired/unavailable/cancellation outcomes; deterministic bounded identities and retry | Declaration unpublication integration and global deletion ordering |
| Integer64 | Generic exact domains, queues, unary handlers, callbacks, journals, checkpoint codecs/graphs, and SDK wire values | Production per-federation backend composition and optimized transports; factory creation remains disabled |
| Restore | Persisted terminal obligations, atomic scope rebinding and epochs newer than both live and saved identities | Exhaustive normative save/restore assessment and independent qualification |

## Plan

The supplied IEEE 1516.1-2010 redline was used to review retained clauses, rather
than treating deleted annex APIs as current requirements. Clause 12.4 and Table 18
require exact standardized time representations. Clause 12.6 distinguishes callback
invocation and reentrant service execution. Clause 5.3 has pending-acquisition and
ownership-loss requirements for unpublication; the transaction foundation is
implemented, but declaration wiring is not closed by this increment.

The implementation preserves existing Float64 callers while giving each time,
object and save coordinator one authoritative scalar representation. Integer64
values must never pass through Float64. Enabling federation creation is a separate
acceptance step, not an automatic consequence of having generic managers.

## Implementation

### Exact Time

`hla/time.ManagerOf[T]`, `hla/object.RegistryOf[T]` and
`hla/save_restore.ManagerOf[T]` use the same algorithms for Float64 and Integer64.
Existing constructors and instantiated aliases retain source compatibility with
the module's Go 1.22 language setting. `TimeBoundOf[T]` represents an unbounded
horizon separately from finite `MaxInt64` final time.

The wire adds representation-tagged values without changing existing field tags
or RPC signatures. Optional absence remains RO; an exact zero remains a present
timestamp. Mixed pairs, missing Integer64 selectors, malformed values and wrong
representations are rejected before service effects. Standardized Float64 final
time remains finite. Historical Float64 `+Inf` behavior is retained only on
legacy-compatible paths and is not the standardized exact representation.

`grpc.NewServerOf[T]` registers typed Object, Time, DDM and Savepoint unary
handlers. Integer64 construction rejects the still-Float64-only LocalLRC,
confirmed-object and interaction-stream optimizations. This constructor does not
enable production Integer64 federation creation. The production `rtid` composition
still uses Float64 managers; its factory guard remains deliberately unchanged.

Savebundle adapters preserve exact queued messages, pending deletions, time state,
retraction history, MOM values and durable save history. Integer formats carry
their factory name and reject missing required scalars. Existing Float64 snapshots
and replay fixtures remain readable. Eventlog codecs now encode typed object and
grant records instead of falling through to a sequence-only record; object replay
uses the exact timestamp pair. Passthrough replay records are not claimed to have
newly implemented state effects.

MOM queries encode the exact time and interval in their nested wire values.
Integer64 zero, adjacent values above `2^53` and `MaxInt64` survive direct calls,
gRPC and Protobuf round trips. Missing, mixed or contradictory pairs fail rather
than returning a fabricated Float64 zero. Legacy Float64 snapshots, including
historical `+Inf`, keep their original representation.

### Actual Callback Invocation

Mandatory ownership receipts are independent of optional MOM accounting. Their
scope contains federation generation, recipient and callback epoch. A stable
callback ID identifies an outcome; a delivery attempt identifies a particular
send. Queue admission and successful stream send do not complete a negotiated
recipient's obligation. Only its authenticated invocation report does.

Failed admission/send retains the outcome. An early report cannot clear an
attempt that ultimately failed to send. Retry preserves the callback ID, creates
a new delivery attempt and permits SDK deduplication without dropping the pending
report. Legacy clients retain their existing weaker queue-admission boundary;
that compatibility path is not evidence for actual-invocation semantics.

Numeric object-knowledge epochs also cross the wire. Fenced local deletion retires
the corresponding SDK knowledge lifetime. Old buffered ownership assumptions can
then be suppressed without suppressing a later discovery or mandatory terminal
outcome. Raw event consumers must use the invocation-aware dispatch/report API
when enabling receipts; merely reading an event queue is not a callback invocation.

### Restore Identity

Ownership snapshots retain enabled sessions, completed IDs and pending terminal
outcomes. A manifest-aware restore preparation reads saved scopes before state
import and reserves each new queue epoch above both the current and saved value.
Unknown recipients and exhausted epochs fail during preparation, without changing
the queue or identities. The returned epoch map cannot alter the reserved state.

After prepared state import, the ownership domain validates the entire scope
rebind and commits it atomically before the reserved membership/terminal queue
publication. Commit and release are idempotent. This closes the rejoin alias where
a saved epoch of one could otherwise collide with a newly joined queue advancing
from zero to one. Old transport attempts and invocation reports do not survive
the new scope.

## Review

Regression tests exposed and corrected these integration defects:

- Legacy Float64 final-time requests, queries, callbacks and grant journals were
  inadvertently rejected by finite exact-value codecs.
- Early codec rejection lost the IEEE exception trailer; NaN and past legacy
  targets now retain their existing domain-specific exception classification.
- Float64 MOM import accepted conflicting exact/legacy pairs, and malformed
  exact metric bytes silently fell back to legacy values.
- A post-publication, per-session restore rebind could fail or partially update
  scopes. Restore now reserves fresh epochs and applies one atomic scope plan.
- Python receipt state was shared accidentally by federates using one transport.
  Ledgers are now selected by recipient and rebound only for the restored member.
  Delayed fenced-delete replies are checked against the current session, scope
  and restore lifetime before they can retire object knowledge.
- An acknowledgement error could replace an asynchronous handler's exception.
  The handler exception remains primary and the failed receipt remains retryable.
- A best-effort cluster notification batch could attempt another peer after its
  deadline. Cancellation is now checked before dialing and dispatch; deadline or
  cancellation replies end the batch. This is an adapter lifecycle regression,
  not a claim that the experimental cluster implements standard RTI semantics.

Integer64 tests include adjacent values above `2^53`, all five advance modes,
finite `MaxInt64` versus unbounded bounds, pending lookahead, RO metadata,
retraction, timed save, identity rewriting and persistence restart. These are
layer and composition tests, not an enabled Integer64 `rtid` experiment.

The production callback test uses the assembled server over gRPC: it checks
authentication, cross-member rejection, unavailable callbacks, invocation reports
during save, duplicate reports, an uninvoked outcome persisted in a checkpoint,
stale reports after restore, retransmission and eventual completion.

## Executed Verification

The final integration rerun, including architecture checks and official FDD-schema
oracles, passed 70 Go packages with 2,742 passing top-level tests and 7,031 passing
test/subtest events, no failures and no skipped tests. The Linux race rerun passed
all 12 affected packages, including the Go SDK.

Earlier runs skipped three schema oracles because their original temporary input
directory no longer existed. The official supplementary archive was retrieved
again and its SHA-256 matched the preceding source record:
`e6343f40b066d51b7f435c9aae6788be24f5d585e370d972c032ab0b503e27c1`.
The recovered `IEEE1516-FDD-2010.xsd` also matched
`8846b46dc0ee8ee1299c0e854e65630cf4de18375f6ce1200abd904e37f5a3c1`.
The final rerun set `GORTI_FDD_XSD` and `GORTI_FDD_PYTHON` explicitly, using
Windows builds of `xmlschema==4.3.2`, `elementpath==5.1.4` and `lxml==6.1.3`.
These supplementary files and dependencies remain outside Git.

Execution logs remain outside Git:

- `D:/workspace/gorti/go-exact-callback-accepted-20260930.jsonl`
- `D:/workspace/gorti/race-exact-callbacks-accepted-20260930.log`
- `D:/workspace/gorti/fdd-oracles-recovered-20260930.jsonl`
- `D:/workspace/gorti/quality-exact-callback-plan-20260930.json`
- `D:/workspace/gorti/quality-exact-callback-accepted-20260930.json`
- `D:/workspace/gorti/quality-exact-callback-full-accepted-20260930.json`

The final production binary was built as
`D:/workspace/gorti/java-tools/rtid-exact-callbacks-accepted.exe` with SHA-256
`63f42476f4f01e311e858cc1bc9ac9f7d9df655b8e15c06b23e2043e351fc0cf`.
This build check does not activate the still-disabled Integer64 factory.

The earlier check-only result is `CHECK_ONLY` (exit 2): that invocation executes
no tests and is not a PASS. The first executed gate also exposed a stale RO-filter
test name. The manifest now references the actual inherited-interaction and retained
known-object-class regression; this test was executed successfully. The recovered
schema run also exposed the peer-deadline boundary described above. The final
executed `supported-profile` gate is `PASS` (exit 0): all 198 required groups ran
and passed, with no reported errors. Its output explicitly retains
`conformance_established: false` and `certification: false`.
The gate's own 28 unit tests and the Java ambassador-generation consistency
check also passed; neither extends the declared conformance scope.

### SDK Verification

The final binary was used for the selected Python localhost, logical-time selection
and receipt-regression campaign: 58 tests passed, with no failures or skips. The
Java Maven reactor passed 429 tests with no failures, errors or skips, including
40 live-server cases. Both campaigns use local test FOMs. Separate JVM checkpoint
replacement is included in the Java cases; other two-participant SDK scenarios
must not be described as independent federate-process comparisons.

The independent Python review regressions passed five cases for shared-transport
ledger isolation, stale epoch-zero deletion after handle-changing restore, and
synchronous/asynchronous handler exception retention with retry deduplication.
The original review first reproduced the defects in controlled transport probes.
The regression red sensitivity run reconstructed those defects in memory after
the fixes landed; it was not a pre-fix checkout test. Final source hashes did not
change across the independent green run.

A broader Python run recorded 1,547 passes, 44 skips and four failures before the
final review fixes. Two failures require the optional RL NumPy dependency, one
requires the missing mypy tool, and one reports lint errors in the separate dirty
RL implementation. These were not converted to passes or removed from the run.
The opt-in live tests were executed separately against the final binary. This
record does not claim a clean whole-Python-suite or whole-repository lint result.
An August server binary caused earlier pyjevsim example failures; rebuilding that
artifact and rerunning the five determinism/delivery cases passed. An explicit
fresh-binary three-message run also observed identical published/received values
and zero exit codes for both participant processes.

SDK logs outside Git:

- `D:/workspace/gorti/sdk-live-20260930/python-accepted-live.log`
- `D:/workspace/gorti/sdk-live-20260930/python-sdk-unit-corrected.log`
- `D:/workspace/gorti/sdk-live-20260930/python-pyjevsim-fresh-suite.log`
- `D:/workspace/gorti/sdk-java-live-20260930/maven-accepted-verify.log`
- `C:/Users/cbchoi/AppData/Local/Temp/gorti-ownership-review-20260930/regression-final-green.log`

The full-2010 prerequisite check remains `FAIL` (exit 1), with incomplete normative
coverage and no independent whole-RTI evidence. This is separate from the passing
bounded implementation regression.

The executable evidence manifest adds bounded exact-layer, terminal-invocation
and restored-scope groups. It still has no independent whole-RTI evidence and
cannot award complete conformance.

## Next Work

1. Compose and select authoritative per-federation Float64/Integer64 backends in
   the production server, including exact initial MOM state for idle federates,
   administrative queries, lifecycle admission and service dispatch.
2. Migrate confirmed streams, LocalLRC and related optimized paths, or make their
   negotiated limitations explicit without routing integer requests to Float64.
3. Connect unpublication to the prepared ownership transaction and distinguish
   omitted whole-class arguments from an explicitly empty attribute set across
   wire and SDK overloads.
4. Complete global deletion/resignation callback ordering and an independent
   clause-by-clause, multi-binding conformance campaign.

No performance ranking or complete IEEE conformance claim is made here.
