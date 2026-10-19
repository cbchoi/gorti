# Internal assessment defect corrections

Date: 2026-10-02. This increment corrects reproduced defects from the
[internal assessment](ieee1516-2010-internal-assessment-20261002.md).
The earlier failed results are retained as historical evidence. Correcting
these defects does not complete the normative obligation inventory or establish
whole-RTI conformance.

Final outcome: all executed cases in the final selected Python, Java, Windows
Go and expanded Linux race campaigns passed. The Python campaign has one
unavailable C++ interop prerequisite skip, which supplies no conformance evidence.

## Corrections

| Boundary | Correction | Regression contract |
| --- | --- | --- |
| Python DDM query | Retain the joined session token and attach it to bounds queries | Owner succeeds; absent, malformed, unknown, stale and foreign-federation tokens remain rejected; another member cannot impersonate the owner |
| Python interaction advisories | Remove the branch-local handle import that shadowed dispatcher-wide names | Both interaction-on/off callbacks use typed handles without requiring an earlier transportation callback |
| Python in-process event pump | Schedule foreign-thread enqueue on the async reader's loop | Parked-reader tests fail before the fix and pass afterwards with the same timeout; direct queue producers and transport producers retain FIFO |
| Python resignation lifecycle | Retire the callback stream and session only after the resignation RPC succeeds, then await server EOF | Rejected, cancelled and invalid resignations retain membership and retry capability; a typed EOF signal is deferred without hiding unrelated stream errors |
| Python Layer-2 and replacement streams | Retire callback dispatch only after successful resignation and match token/task identity before local cleanup | Direct and pump delivery survive rejection; stale replies and old carriers/EOF cannot retire or contaminate a replacement membership |
| Server resignation and rediscovery | Refresh ownership-dependent discoveries after roster and declaration teardown, separately from terminal admission accounting | Float64 and Integer64 controls exclude the departed subscriber, preserve surviving ownership and delete the final member's object |
| Java callback switches | Reject enable/disable during save and restore before changing the switch | Source-derived SaveInProgress/RestoreInProgress probes and state-preservation controls |
| Java restore completion | Distinguish an idle restore from an active restore with unvalidated SDK state | Idle completion reports RestoreNotRequested; active completion still requires validated runtime state |
| FOM creation and default MIM | Reject empty creation arrays at the Java standard API boundary; remove the composition shortcut that omitted MIM | Every Java creation overload rejects before creation RPC; valid creation and empty additional join modules retain MIM; Go/Python zero-module extension retains MIM |

The Java creation boundary uses CouldNotOpenFDD for missing FOM input. The
independent source-derived negative contract requires rejection, without
claiming that IEEE prescribes this exact exception for a zero-length array.
Additional join modules are optional and are not subject to this creation guard.

Go/Python product interfaces retain their existing zero-module creation
extension. They do not substitute for the standard Java creation contract.
Both Float64 and Integer64 executions made through this extension now provide
the default MIM. Its merge does not change the historical ordered-source digest.
No wire schema, generated descriptor, persisted format or authentication
requirement was weakened by these corrections.

## Executed core and MIM checks

| Campaign | Result |
| --- | --- |
| Windows Go RTI, tests and examples | 3,241 top-level tests passed; 9,670 test/subtest passes; 76 tested packages and 10 packages without tests; zero failures or behavioral skips |
| Linux HLA, delivery and composition with race detector | 1,084 top-level tests passed; 2,679 test/subtest passes; 12 tested packages and one without tests; zero failures, behavioral skips or reported races |
| Python default-MIM extension, old executable control | Both time-family cases failed with NameNotFound for the missing MIM class |
| Python default-MIM extension, corrected executable | Both time-family cases passed |
| Existing independent public Python TCP contracts | Six passed against the corrected executable |
| Corrected DDM input and contextual-default controls | 15 passed against the corrected executable; independent diagnostic controls also passed against both old and new executables |
| Updated selector and callback-entry metric scenarios | 58 Python cases passed, including Float64/Integer64 lifecycle, unsupported selectors, pre-entry zero and post-entry delivery counts |
| Intermediate complete selected Python HLA campaign, live enabled | 1,961 passed, 17 failed, one unavailable C++ publisher skipped; 1,979 cases in 133 selected files, zero errors; retained before the remaining corrections |
| Java external-original-API selected regression suites | 115 passed with zero failures/skips on both old and corrected servers; overlapping control execution, not 230 distinct requirements |
| Latest whole Java live Maven campaign | 535 passed; zero failures, errors or skips |
| Updated TM observer contracts, both full files | 74 passed; the 12 previously failing cases retain their timestamp rejection, metadata, queue and ordering checks |
| Updated M12 synchronization/save/restore, both full files | Six passed; includes raw-announcement rejection, reentrant achievement after ENTRY, restore staging and restored identity |
| Resignation and ownership recovery TDD | Correct-schema red run: 10 failed and two passed; final full focus files: 40 passed, zero skips |
| Resignation transport regression | 16 passed; separate ownership/callback/lifecycle regression: 258 passed |
| Integrated Python rerun after remaining observer/recovery corrections | 1,989 passed, two failed, one unavailable C++ publisher skipped; 1,992 cases in 133 selected files, zero errors; both failures occur during Layer-2 ownership teardown |
| Retired-recipient correction, complete affected Python files | Nine passed against the final server, including both integrated failures |
| Additional SDK resignation review | 87 bounded tests passed, including actual direct/pump callback invocation after live rejection and successful retry |
| Final Linux HLA, delivery, composition, lifecycle and server race campaign | 1,342 top-level tests and 3,908 test/subtest passes in 14 tested packages; zero failures, behavioral skips or reported races |
| Final configured Windows Go RTI, tests and examples | 3,242 top-level tests and 9,673 test/subtest passes in 76 tested packages; zero failures or behavioral skips |
| Final combined Python HLA campaign, live enabled | 2,002 passed, zero failures/errors, one unavailable C++ publisher skipped; 2,003 cases in 134 selected files |
| Final Java campaign against the final server | 535 passed across 56 executed suites, zero failures/errors/skips; current Running suites and fresh reports agree |

The Windows command is `go test -json -count=1 -timeout=180s ./rti/... ./tests/... ./examples/...`.
The Linux command uses `-race` on `./rti/internal/hla/...`,
`./rti/internal/adapters/delivery` and `./rti/internal/application/composition`.
The final expanded race campaign additionally includes
`./rti/internal/application/lifecycle` and `./rti/internal/application/server`.
This is not a whole-platform SDK release campaign.

The corrected server is `D:/workspace/gorti/rtid-defect-fix-20261002.exe`,
SHA-256 `04711f8c7338ed7acf51480b44e862babb455db638d0fad1bd7e62e105596acc`.
The retained control is `rtid-internal-assessment-20261002.exe`,
SHA-256 `6c9cf2c18c06236a909e900d9d70e58db6fa31e1d6708e81400e44f5ab17d979`.

Raw evidence stays outside Git under `D:/workspace/gorti/`:
`defect-go-all-20261002.jsonl`, `defect-go-race-20261002.jsonl`,
`defect-fom-live-20261002-green.xml` and its server artifacts,
and `defect-fom-live-20261002-old-control.xml` with separate control artifacts.
The initial new Python test's incorrect positional argument was corrected to
the public API's keyword-only time selector; its failed execution is also
retained, not represented as a product defect.

## Reviewed test corrections

The DDM no-shared-axis failure came from specifications with omitted dimension
defaults, not missing region handles in the Python request. Existing product
fallback supplies full ranges, so contextual realization adds the other axis
and the regions overlap. IEEE 1516.1-2010 9.1.3.1(f)(2), PDF page 222, requires
declared defaults to be added before the overlap rules in 9.1.4, PDF page 225.
The no-shared-axis fixture now explicitly declares Excluded. Its original
non-delivery assertion remains. Separate full, excluded and narrow default
controls cover both delivery and rejection; the omitted-value control is
identified as product compatibility behavior, not a normative default for an
incomplete FDD. No production routing algorithm was changed for this finding.

Three selector scenarios still treated Integer64 as unsupported after its
production activation. They now exercise both supported families and reject
genuinely unknown selectors, preserving the no-created-state checks and
asserting exact integer factories/values. The two evoked MOM tests now require
available zero before callback ENTRY and two after actual invocation, rather
than requiring the counter to be unavailable. Their subsequent delivery,
callback-order and completed-accounting assertions are executed unchanged.
These are reviewed input/oracle corrections, not claimed production fixes.

Raw DDM diagnosis and integrated results are under
`D:/workspace/gorti/defect-ddm-routing-20261002/`. Updated Python selector and
MOM results are `defect-oracle-fix-20261002.xml` and its log/artifacts outside
Git. Earlier failing versions remain in the internal assessment evidence.

## Review follow-up

Independent review identified a re-enable deadlock in the first Java guard:
marking save/restore active at stream arrival prevents an initially disabled
participant from enabling callbacks to receive initiation. Callback switch
guards must start at user callback ENTRY, separately from transport admission
and SDK hydration. The final regression coverage includes an initially disabled
participant and preserves the active-state rejection after initiation entry.

Restore complete/not-complete also need the service-specific SaveInProgress
path before any local idle-restore readiness failure. The response guard keeps
admitted save state separate from callback switch state, without allowing an
unvalidated SDK restore to report successful completion.

The standard Java callback-switch declarations do not include NotConnected.
Disconnected switching is rejected defensively with the declared RTIinternalError,
without claiming an IEEE-prescribed exception mapping. Connected, nonjoined
switching remains valid. The original standard interface is unchanged.

The latest whole Java live command uses the current provider, corrected server
and configured runtime fixtures. It executes all 535 selected cases without
skips. External-original-API compilation keeps the previously documented two
Java 11 factory-discovery overlays explicit. The first external attempt failed
factory discovery without those overlays; it is retained, along with
intermediate lifecycle/teardown failures and their corrected reruns. No earlier
failed result is overwritten or counted as a pass. Raw Java evidence is under
`D:/workspace/gorti/defect-java-fix-20261002/`, including
`final-whole-live.log` and `external-final-summary.json`.

Python query verification includes actual resign/rejoin credential rotation:
the new member can query a newly owned region, while a retained old DDM client
is rejected. Authentication tests do not replace the server's identity checks.
The in-process pump's initial timing-sensitive test sometimes passed without a
fix; only the added parked-reader failures and debug-enabled green tests are
used as evidence of the wakeup correction.

## Intermediate selected Python rerun

After the auth, advisory, pump, MIM and reviewed oracle changes, the selected
HLA files were run together with live execution enabled. This avoids summing
overlapping focus runs or changing earlier results retroactively. There are
17 failures in that intermediate run: two historical synchronization participation cases,
one restore participation case, two same-time grant-index cases, ten RO
queue/raw-receipt cases and two ownership-restore cases. None was marked
xfail, skipped or converted to success. The sole skip requires an unavailable
C++ publisher and supplies no C++ verification evidence.

The execution used the corrected hash-pinned executable above. Legacy fixture
builders were directed to that same executable before test imports, with the
original builder retained for its own fixture test. The selection, raw log,
JUnit outcome and server artifacts are outside Git at
`D:/workspace/gorti/defect-python-final-20261002/`.

## Remaining-observer review

The fresh 17-failure run differs from the original assessment: the nine
malformed-timestamp cases now reach and pass their rejection/no-effect checks,
then fail the queue-empty check after the valid control message is only read
from the raw stream. The tests now assert it is still queued, invoke the real
handler through the existing authenticated `dispatch_event` path, assert one
entry and then require queue zero. The three-message MOM scenario checks
domain waiting, raw delivery without entry, actual FIFO handler entries and
terminal queue zero separately. No invalid request or subsequent assertion was
removed. IEEE 1516.1-2010 8.1.1/8.1.7, PDF pages 182/189, distinguish receipt
through data callback invocation; 11.6 Table 6, PDF page 297, defines MOM queue
attributes.

The two restored same-time scenarios retain their history, but locate the
returned grant and delivered message by object identity in the fresh request
window. Equal TimeAdvanceGrant values no longer select an earlier recovered
grant. The message-before-grant requirement remains asserted for queued data.
The full two-file run passes 74 cases. Evidence:
`D:/workspace/gorti/defect-tm-observer-fix-20261002/`.

Both M12 synchronization scenarios now prove that raw announcement alone does
not authorize achievement, and that authenticated ENTRY permits reentrant
achievement. They then check actual FederationSynchronized delivery. Save
responds after InitiateFederateSave; restore responds after matching validated
InitiateFederateRestore and queries terminal state after FederationRestored.
Reconnect and saved-handle identity checks remain. Their full two-file run
passes six cases. Evidence:
`D:/workspace/gorti/defect-m12-sequence-fix-20261002/`.

## Resignation and ownership recovery

The SDK previously cancelled the callback stream before requesting resignation.
When pending acquisition caused the default action to be rejected, the member
remained joined but lost its delivery stream. A live negative control now checks
the exact OwnershipAcquisitionPending exception, retained session and stream,
subsequent synchronization delivery, unchanged ownership and successful explicit
CANCEL_THEN_DELETE_THEN_DIVEST retry. Cancellation and invalid-action controls
also preserve local membership state.

Successful resignation closes the server outbox. The SDK awaits that EOF rather
than cancelling a live gRPC call. An intermediate cancellation-after-success
implementation reproduced a polling-thread Event-loop-closed error and a later
federation creation timeout; these failed runs remain preserved. The final
natural-EOF implementation passes the complete affected live files without
changing timeouts. Only the private typed clean-EOF signal is deferred during
the RPC. Failure or cancellation replays it; ordinary errors, including one
with the same text, remain visible even on success.

Ownership restore tests now separate raw replay from authenticated handler
ENTRY. Requested confirmation and waiting-offer controls each cover originally
invoked and originally uninvoked callbacks. Invocation identity survives restore,
receipts rotate and the handler body executes exactly once. Ownership-state,
valid-transfer and withdrawn-acquirer negative assertions remain. Fixture
cleanup uses an explicit cancellation/divest action, while the separate live
negative case continues to prove default-resign rejection.

Evidence, including red runs, intermediate teardown failures and final results,
is under `D:/workspace/gorti/defect-resign-ownership-fix-20261002/`.

The next combined execution is retained at
`D:/workspace/gorti/defect-python-complete-20261002/`. It passes the previous
17 failing scenarios but exposes two Layer-2 ownership teardown failures:
`test_two_phase_divest_parks_until_confirm` and
`test_n04_production_tags_result_and_no_followup_queries`. Both report an
unavailable federate outbox during final resignation. The 1,989 passes do not
close this historical combined campaign.

The unavailable handle belongs to the retired subscriber, not the publisher
making the final resignation. Ownership's post-commit observer refreshed
discoveries while the departing subscriber's publication/subscription entries
were still present. This recreated knowledge just removed by the object cleanup,
and the publisher's final deletion later tried to reserve the retired outbox.
A server-composition test reproduces both time families before the fix.

Terminal admission accounting still completes after atomic fanout commit.
Ownership-dependent discovery and offer refresh now run through the optional
ResignFinalizer after membership and declaration teardown, under the same
exclusive federation operation boundary. No unavailable-recipient error is
ignored and no reservation is made partial. The complete two affected Python
files pass nine cases against the final server. Evidence:
`defect-retired-recipient-red-20261002.log`,
`defect-retired-recipient-green-20261002.log` and
`defect-retired-recipient-python-green-20261002.xml` outside Git.

This final server is `D:/workspace/gorti/rtid-defect-final-20261002.exe`, SHA-256
`24b9c82590543159e6234c8dc7d473b6ee1699329c34677d1779035aad115b05`.
The earlier corrected executable and all failed runs remain preserved.

Independent SDK review found the same acceptance-boundary problem in the
Layer-2 ambassador: it cancelled the event pump or removed the direct sink
before RPC success. Both are now retired only after successful context exit.
Live controls verify the original tagged synchronization callback actually
executes after rejection, in both delivery modes, and then exercise explicit
resignation retry.

The stream cleanup also captures its original task and authenticated token.
Replacing either during an awaited resignation cannot cause the new task,
session, callback state or generation to be retired. The old managed drainer
continues to consume natural EOF but does not deliver old carriers or terminal
signals into the replacement sink. Red controls cover local teardown, stale
authentication, replacement tasks, deferred EOF and a queued old carrier;
failures and the final 87-case bounded pass are retained at
`D:/workspace/gorti/defect-resign-review-20261002/`.
An additional 35-case restore regression passes, including actual fresh-process
checkpoint restore for both time families and subsequent callbacks. Restore
moves the same authenticated token with its managed task; the new identity
guard therefore continues to accept the validated terminal and restored stream.

The first final Go rerun omitted the configured Python and official-schema
paths: the Windows Store Python alias failed the M5 fixture and three schema
oracles skipped. That run is retained as an execution-configuration failure,
not a product pass. The configured rerun uses GORTI_PYTHON, GORTI_FDD_PYTHON
and GORTI_FDD_XSD explicitly. Expanded Linux race evidence is
`D:/workspace/gorti/defect-final-go-race-20261002.jsonl`.
The successful configured Windows result is
`D:/workspace/gorti/defect-final-go-all-configured-20261002.jsonl`.

## Final integrated execution

The final Python selection includes both new resignation regression files and
all previously selected HLA files. Live execution remains enabled, and both
modern and legacy fixture builders use the same final server hash. It passes
2,002 cases in one execution, with no failures or errors. Its sole skipped
C++ publisher case is not credited. Raw results and server/save artifacts are
outside Git at `D:/workspace/gorti/defect-python-final-complete-20261002/`.
No overlapping focus runs are added to that count.

The Java provider was rerun against the final server, without changing its
source after the earlier SDK corrections. All 535 cases in 56 executed suites
pass. The count is reconciled with actual Running-suite entries and newly
generated XML, rather than retained reports. Evidence and owned-process cleanup
are at `D:/workspace/gorti/defect-java-backend-final-20261002/`. This campaign
mixes unit, in-process and live tests; 535 is not a count of TCP scenarios or
independent normative requirements.

## Assessment boundary

The reproduced findings covered by these campaigns now have corrections,
reviewed oracle dispositions and passing final integrated results. Reviewed
oracle corrections do not turn historical failed executions into passes.
The complete applicable IEEE obligation inventory and whole-binding semantic
review remain open, independently of external assessment.
