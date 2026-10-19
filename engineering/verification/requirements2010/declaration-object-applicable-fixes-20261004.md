# Declaration/Object Applicable Increment, 2026-10-04

## Plan And Do

The three previously applicable-unlinked rows now have exact behavioral tests.
S1516.1 source/page references and incomplete source scope are unchanged.

| Requirement | Literal test | Result and scope |
| --- | --- | --- |
| DM2010-MOM-SERVICE-REPORT-SUBSCRIPTION | TestApplicableSupportReportingSubscriptionAdmission | Active/passive exact report subscription now rejects with exact spec trailer and unchanged declarations; disabled-switch retry succeeds. In-process actual server. |
| OM2010-ORPHAN-OBJECTS | TestApplicableSupportOrphanCannotRediscoverOrAcquire | All attributes unowned and all knowledge lost, then four members cannot rediscover or acquire; exact error and unchanged ownership. Pass. |
| OM2010-BEST-EFFORT-RATE-REDUCTION | TestApplicableSupportBestEffortRateReduction | Active/passive TwoHz best-effort recipients now drop the 100 ms excess passel, deliver current values at 500 ms, preserve the whole passel and reset immediately to default. First-red failure retained. |

Test paths respectively:
`rti/internal/application/server/applicable_support_admission_20261004_test.go`,
`rti/internal/hla/object/applicable_support_orphan_20261004_test.go`, and
`rti/internal/hla/object/applicable_support_rate_20261004_test.go`.

The reporting guard is shared by ordinary/regional public adapters and MOM
service dispatch, without editing the shared declaration publication manager.
No protobuf schema or numeric error-code inventory change was made. The exact
standard exception name is carried as metadata. Direct declaration-manager
calls, concurrent switch/subscription races, regional/ancestor end-to-end cases
and Java exception construction remain unverified.

The rate failure was a core outbound-delivery observation, not a selected Java
callback-entry campaign. The 2026-10-04 correction adds per-instance, recipient
and attribute last-successful immediate-delivery times, guarded by the existing
object delivery mutex. Reduction drops entire excess best-effort passels before
outbox reservation. Only successful delivery consumes the interval. Registry
name/registration logic was not changed; its instance struct gains rate state.

Two additional literals in the same rate test file now pass:
`TestApplicableSupportReliableRateDoesNotDrop` retains the excess reliable
update; `TestApplicableSupportRateFailedAdmissionCanRetry` injects separate
reservation and commit failures, verifies exact errors and no callbacks, then
successfully retries at the unchanged eligible clock time. The production
filter consults ordinary subscription options plus DDM bridge-provided regional
demands, but this tranche does not claim end-to-end regional rate coverage.
Buffered RO/TSO messages and SDK callback-entry timing are not throttled by this
immediate-delivery fix; those remain explicit unresolved facets of 6.1.12.

## Dated Corrections

The 2026-10-03 reproduced-defect notes remain explicitly historical. Current
row notes and links identify the parent's 2026-10-04 fixes:

- Exact `IllegalName`, `NameSetWasEmpty`, and
  `ObjectInstanceNameNotReserved` raw-RPC trailers in
  `TestApplicableR1LiveObjectNameAdmissionBoundaries`: current body inspected,
  four live passes reported by parent, not re-executed by this worker.
- `TestPrivilegeLifetimePersistenceAndReset` checks implicit/explicit/suppressed
  histories through detached restore and new publication lifetimes.
- `TestPrivilegeInitialOwnershipFollowsPublicationLifetime` checks ownership
  follows current privilege publication instead of unconditional registration.
- `TestApplicableR2ReservationSurvivesRegistrationDeletionAndRestore` is linked
  to the single reservation callback, single release and named-registration
  rows: reservation survives registration/deletion/restore, excludes a peer,
  permits holder re-registration, and is removed by explicit release.
- `mom_full_catalog_test.go` and `mom_service_execution_test.go` now compare
  exact publication sets including the implicit privilege, not smaller counts
  or a relaxed containment check. Both selected server tests pass freshly.

## Review

Evidence and exact commands are documented in
`support-mom-applicable-fixes-20261004.md`. Final
`corrected-final-focused.jsonl` has 15 top-level/425 nested passes and zero
failures/skips. `affected-regression-complete.jsonl` has 818 top-level/1,443
nested passes across all five affected packages, zero failures/skips; the final
focused run also covers the subsequently strengthened commit-failure assertion.

Historical `final-focused.jsonl` contains
12 passing/1 failing top-level cases and 423 passing nested cases, no skips.
`object-regression-final.jsonl` contains 209 passing/2 failing top-level cases,
405 passing/4 failing nested cases, no skips. Besides the rate failure, the
parent-added `TestIndependentNameReviewCallbackAdmissionFailureIsAtomic`
single/multiple unavailable/capacity variants fail; those concern parent-owned
reservation state and were neither edited nor excluded. Parent subsequently
fixed those cases; the fresh full package run is green. Earlier parent-added
released-live-name failures and intermediate implementation/fixture failures
remain in earlier raw logs.

## Reflect

Applicable-unlinked count changed from three to zero with 88 applicable rows
unchanged. Links increased from 135 to 146 (+11), including the three rate
oracles and the requested historical name/privilege corrections. Support/MOM
adds eight links, so the combined mapping increment is +19 links and eleven
newly linked rows, without shrinking the applicable denominator.
These are relevant assertions, not eleven verified requirements. Last-knowledge
loss via resign, all lifecycle/error combinations, public Java mapping,
buffered/evoked delivery rate timing, rate-clock behavior across save/restore,
reliable pacing and whole-source completeness remain open.
