# Flush, Time Activation And Retraction

Date: 2026-09-22
Status: Implemented and verified in the bounded Go/Python profile on Windows and Linux.

This change follows the [order and advance batch](ieee1516-2010-order-advance-20260922.md).
It addresses FQR, safe time-mode activation and message retraction. Earlier
campaign results are historical, not evidence for this change.

## Behavioral Oracle

The supplied IEEE 1516.1-2010 redline provides the active 2010 requirements.
Licensed publications and extracted text remain outside Git. References below
identify paraphrased requirements; page numbers are PDF pages.

| Scope | Source | Required behavior |
| --- | --- | --- |
| FQR | 8.1.8, 8.12-8.13, 191, 206-208 | Release all queued applicable RO and TSO, then grant the minimum of request, applicable GALT and actually delivered TSO times; allow unchanged-time completion |
| Filtered next message | 8.1.8, 8.10-8.11, 191, 203-205 | Reevaluate NMR/NMRA after a safely eligible timestamp group is entirely discarded; do not grant at an undelivered message's time or discard unsafe later groups early |
| Time regulation | 8.2-8.3, 192-194 | Choose the smallest safe activation time without breaking constrained peers' delivery guarantees; constrained activation has implicit TARA delivery semantics |
| Time constraint | 8.5-8.6, 197-198 | Retain the request until a safe activation callback can complete it; request acceptance is not activation |
| Pending activation | 8.8-8.12 preconditions | Reject advances while either enable request is pending; distinguish pending requests from already-enabled state |
| Retract | 8.21, 215-216 | Require original sender, valid TSO designator and current regulation; timestamp must exceed current/requested time plus actual current lookahead |
| RequestRetraction | 8.22, 216-217 | Notify delivered-message recipients, remove queued copies and preserve sent-RO passels of mixed updates |
| Retracted deletion | 8.21, 215 | Reconstitute the object with the attribute owners captured at deletion invocation |
| Recipient object knowledge | 6.15.4, 135 | Received-RO removal forgets immediately; speculative FQR TSO removal retains knowledge until its timestamp is reached or constraint is disabled |
| Timed save | 4.16-4.17, 64-66 | Use the actual next grant and service-specific boundary; a scheduled save must not block a legal noncrossing FQR |

Constraint-enable equality is an explicit interpretation: completion uses
`currentTime <= GALT`. Section 8.6 also describes a waiting case using "beyond".
Retain this ambiguity for final normative review, not as unquestioned exhaustive
conformance evidence.

## Implementation

FQR retains received TSO classification while allowing speculative delivery
beyond its grant. A partial delivery remembers the minimum already published
timestamp; retry cannot raise the grant after removing the earliest queue entry.
Internal deletion deadlines are not user FQR messages.
Messages are reevaluated against current subscriptions when eligible, in both
Delay Subscription Evaluation settings. FQR publishes the retained messages
before selecting its grant, so discarded timestamps do not lower that grant.
NMR/NMRA release only a safely eligible timestamp group and recompute after
filtering. A later unsafe group stays queued; an already-delivered prefix still
sets the target after retry.

Enable requests have pending states and real `TimeRegulationEnabled` and
`TimeConstrainedEnabled` callbacks. Go and Python consume server callbacks;
unary success indicates acceptance. Callback pressure retains pending work.
Strict receive guarantees are independent of outgoing zero-lookahead history.
Float64 safe-time selection terminates even when lookahead subtraction loses
precision near zero. Ordered delivery prefixes protect subsequent mode
activation even before the final grant or enabled callback has been admitted.
For a constrained federate, a pending regulation enable can cross a timed-save
boundary as an implicit TARA. The save precedes the enabled callback; completion
resumes after the save outcome. Unconstrained activation is not held behind an
irrelevant save-time ceiling.

The retraction ledger records original TSO timestamps and delivered recipients,
including unconstrained RO reception. Retraction uses actual current lookahead,
not lookahead projected for a future outgoing send. Callback reservation, queue
cancellation and deletion cleanup share evaluator admission. Deferred deletion
retains authoritative object state until its internal deadline, and saves the
original owners for legal retraction after speculative delivery.
Recipient knowledge is tracked independently of sender-side retirement. A
temporary query-only record preserves the name and class for a receiver whose
TSO removal has not yet expired; it does not permit further object updates.
The record is removed when no receiver retains that knowledge. Go SDK caches
follow the same boundary and recover knowledge after legal retraction.

Time component version 5 stores new pending/delivery state; versions 1-4 remain
readable. Retraction component version 2 stores active message state while
preserving generation-scoped ID history and rebinding saved identities.
Object component version 3 stores recipient knowledge and deletion identity;
versions 1-2 remain readable without inventing missing historical information.
Enable callback variants are additive wire changes. Existing RPCs and fields
remain intact; descriptor tests explicitly approve the additions.
This is wire compatibility, not old-client semantic compatibility. Updated Go
and Python clients use the actual server callbacks. The C++ binding still
synthesizes enable completion after unary success and is not validated for the
new pending-activation behavior.

## Plan, Do, Review, Reflect

1. Plan: derive boundary oracles independently from active 2010 clauses and split
   activation, retraction, persistence, SDK and live tests by ownership.
2. Do: reproduce baseline failures before implementation, then integrate queue,
   grant, delivery and saved-state changes.
3. Review: inspect callback visibility, rollback, capacity failure, time
   guarantees, floating-point termination and restored identities.
4. Reflect: correct obsolete fixtures only where their expectations contradict
   the source. Retain positive callback, atomicity and ordering assertions.

Initial FQR tests reproduced missing full-queue release, missing earliest-message
grant bounds, waiting behind a stationary peer and retry loss of the delivered
minimum. Those four top-level tests subsequently passed ten repetitions.
Enable and retraction tests have separate RED/GREEN records.

Review also reproduced an understated GALT when an unconstrained peer had a
pending FQR. Only constrained FQR requests can complete early at peer GALT;
unconstrained requests retain their requested horizon. The corrected regression
passed with the enable tests. Another review separated an already-delivered
ordered prefix from the last granted time, preventing safe activation from
regressing behind the prefix after a peer resigns.

The first full Windows Python run matched all 1,445 selected test identities:
1,444 passed and one timed-save case failed. It used a server built before the
scheduled-save integration was complete and is diagnostic evidence, not the
final campaign. Three subsequent source-derived live cases reproduced incorrect
recipient object knowledge after RO removal, reaching a speculative deletion's
time, and disabling constraint. They extend the retraction review beyond merely
observing a callback.

Those cases and the earlier timed-save case passed in a fresh 44-case live run.
Further review reproduced two FQR failures after interaction unsubscription:
the raw queued minimum was used even when that message should not be delivered.
The correction separates actual flush delivery from grant selection; full DSE
support for a subscriber absent from the original fanout remains separate work.
Independent review found the corresponding NMR/NMRA issue. Six failing boundary
cases were reproduced, then the corrected two top-level tests and eight subcases
passed ten repetitions, followed by the entire time package. The latest focused
Python run passed 64 cases including explicit DSE=false/true FQR filtering,
retraction, activation, timed save and SDK callback races.

## Integrated Verification

All campaigns below use the integrated code after the subscription-filtering
corrections, rather than the earlier diagnostic binaries.

| Campaign | Result |
| --- | --- |
| Windows Go 1.26.5 | 74 test packages; 2,458 top-level tests and 3,312 subtests passed |
| Ubuntu 24.04 Go 1.22.2 race | Identical 5,770 test identities passed; zero race warnings |
| Windows Python 3.14 | 1,459 passed; one explicitly excluded C++ interoperability case |
| Ubuntu Python 3.12.3 | 1,459 passed; same explicitly excluded C++ interoperability case |
| Quality-gate implementation | 28 tests passed |

Neither Go campaign has failed or skipped test cases; 11 additional packages
contain no tests. Every required test entry in the current evidence manifest
(185 groups, 812 entries) appears as passed in both Go logs. This is an audit of
the full regression transcripts, not a separate execution of the gate's
`--run` mode. Metadata validation returns `CHECK_ONLY` with no errors. The
`full-2010` readiness check deliberately remains `FAIL`: the complete normative
inventory, applicability and independent evidence are not established.

The Go command covers `./rti/... ./tests/... ./examples/... ./verification/...`
with fresh `-count=1`; Linux also uses `-race`. Python uses the maintained HLA
inventory, actual locally built servers and official FDD XSD validation where
required. Python collection identities are checked separately from pass counts.
Both Python runs exactly match the same 1,459 selected identities, including
class-based and parameterized cases: zero missing, unexpected, duplicate,
failed, errored or skipped cases. The full maintained inventory has 1,460 cases;
the sole excluded case is
`pysdk/tests/spec/m17/test_cpp_python_interop.py::test_spec_m17_7_cpp_publisher_python_subscriber`.
Concurrent unrelated RL-application tests are not reclassified into this HLA
campaign.

Raw logs use the external prefix
`D:/workspace/gorti/ieee2010-fqr-enable-retract-20260922-`; they are not repository
artifacts. Windows Go uses `go-c.jsonl`, Windows Python uses `python-full-b`, and
Linux uses run ID `q_wyzxgn`. No performance comparison, external RTI certification
or complete IEEE 1516-2010 conformance follows from these tests. These are local
dirty-working-tree development results, not a signed release-source attestation.
No test server from either final campaign remained running after completion.

## Remaining Scope

Whole-standard obligation coverage, complete delayed subscription evaluation
including DDM extents, corrupt past-TSO state validation, exact standardized time
representations, complete language mapping and independent assessment remain
separate work. The [execution plan](../design/ieee1516-2010-conformance-execution-plan.md)
and full-conformance readiness gate continue to expose those gaps.

Retraction ID/origin history is still retained for the federation lifetime and
restore checks. Completed object-knowledge records are reclaimed, but compacting
the longer-lived message history is separate work; this campaign makes no
long-duration memory or throughput claim.
