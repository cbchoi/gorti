# Control Callback and Unreported Result Recovery

Updated: 2026-10-01. Branch: `codex/ieee2010-feature-completion`.

This campaign extends the previous discovery/data checkpoint work to control
callbacks, known callback results and queued SDK query notifications. Recovery
distinguishes an unentered callback from an executed callback and from a result
already accepted by the server. It does not infer a result from queue admission.
The final integrated source and actual restarted-server campaigns passed. This
record separates those results from intermediate failures and component tests.

## Invocation States

| Saved state | Restore action | Work that must not repeat |
|---|---|---|
| Pending, body not entered | Deliver the saved body under fresh authorization | No source authorization may be reused |
| Body finished, known result not accepted | Report the saved success or exception under a fresh receipt | Entry, body, activation and lifecycle effects |
| Result accepted, SDK acknowledgement uncertain | Deliver a non-authorizing completion marker | Body, Entry and result-report RPC |
| Body running or result unknown | Reject checkpoint export or restore preflight | No success or failure may be invented |
| Pure RPC result queued locally | Restore a typed notification after the active restore terminal | Original query RPC and server-side callback accounting |

The last row preserves local FIFO and multiplicity. It does not establish the
original total order between locally queued notifications and server streams.

## Implementation

The server callback ledger assigns a stable invocation identity and records the
invocation phase. The identity is correlation data, not authorization. Server
ledger images contain no callback or ownership receipts. Recognized private SDK
wire images can retain source headers for validation/correlation, but these are
not reissued as target authority; session authentication tokens are excluded.
Pending callbacks receive new
generation-, membership- and epoch-scoped authorization when restored. Historical
save/restore carriers do not mutate the active target restore lifecycle.

The additive `CallbackRecovery` wire message distinguishes result-only replay
and completion markers. RPC service declarations are unchanged. Descriptor
projection tests retain the historical message contract while allowing the new
fields. Private GRS1 runtime images use the `server-v2` profile and known outcomes;
supported older images retain their explicit reader paths.

Ownership and synchronization obligations are reconciled with saved domain state
before publication. Obsolete, unentered advisory callbacks are retired using
their source guards. Completion markers remain bounded but cannot be evicted
while needed for delivery or reconnect. Capacity failures roll back preparation.

Transportation confirmation saves an authority-free intent, its permanently
projected attribute set and explicit activation phase. Source validation uses the
saved FOM, object and ownership graph. Fresh tickets and capacity leases are
prepared before domain import and installed without fallible admission after
import. Applying an already-activated intent only acknowledges its cached result;
it does not repeat the transport change. Result-only recovery needs no activation
ticket, including when the source completed-ticket cache has already evicted it.

An unexpected terminal-publication error after domain import now records a failed
restore and returns `ErrRestoreStateCommitted`, preserving the underlying error.
It does not write a restored-success journal event or resume time advancement.
This diagnostic explicitly says that imported state was not rolled back; expected
transportation preparation failures are handled before that boundary.

Go, Python and Java SDKs dispatch result-only recovery before normal callback
entry. They retain known exceptions, retry failed reporting and discard matching
pending results on a completion marker. Each SDK explicitly tracks validated
restore readiness; a rejected or unreceived runtime stage cannot report success.

Python and Java also export typed pure local notifications. Arbitrary closures
and unexecuted local activation work are rejected rather than silently omitted.
Go query APIs are return-only and do not accept the local-notification field.
The shared reader validates required fields, exact integers, unique identities,
ordering, profile-specific kinds and bounded image size. Only current participant
references are rebound; historical post-restore and execution metadata remain
unchanged.

## Executed Evidence

Raw transcripts and executables are outside Git under `D:/workspace/gorti`.
Counts from overlapping campaigns are not added together.

| Campaign | Result | Scope |
|---|---|---|
| Go SDK frozen source | 190 distinct top-level tests, repeated three times; zero failures/skips | Native SDK and full SDK race campaigns |
| Python frozen source | 530 selected tests passed | Recovery, local notifications, readiness and transportation outcome-only dispatch |
| Java frozen source | 453 non-live tests passed; zero failures/errors/skips | SDK build, central codec exchange and actual-kind outcome-only dispatch |
| Central local codec | Both factories accepted actual Python and Java producer images | Independent SDK decode/re-encode checks after central rebinding |
| Terminal-publication failure | 94 distinct domain tests repeated three times; full domain race repeated three times passed | Eight injected failure cases plus positive controls in both time families |
| Final integrated Go source | 3,317 top-level tests; 9,764 test/subtest pass events; 78 tested packages; zero failures/skips | Explicit product/test roots, not an inaccessible root-wide enumeration |
| Final focused Linux race | All five selected packages passed, repeated three times | Object, gRPC, application server, SDK-state adapter and save/restore; not a whole-repository race run |
| Transportation actual TCP | 16 cases passed three times | Both factories x attribute/interaction x pending, activated-unentered, known outcome and evicted source ticket |
| Go final-binary SDK | Six actual TCP cases passed three times | Fresh source/target clients and restarted servers; includes readiness guards |
| Python final-binary SDK | Four cases passed in the final rerun | Streamed controls/results and pure local results, both factories, fresh clients and stopped/restarted servers |
| Java final-binary SDK | Six cases passed; zero failures/errors/skips | Four streamed-result and two local-result cases; 12 fresh child JVMs and 12 server runs |

Final executable: `D:/workspace/gorti/rtid-control-outcomes-final-20261001.exe`.
Its SHA-256 was verified before and after each language campaign:
`647a999b02cfc81fcd9521c0a96c9e06a440c5a0719735c95ab4d889a9084fbb`.
Owned server and client processes were stopped after each campaign. No production
source changed between the final integrated source run, build and SDK campaigns.
The Python rerun changed only a redundant test-side file-enumeration assertion.

Native reproduction uses Go 1.26.5 on Windows:

```text
go test ./rti/... ./tests/... ./examples/... ./verification/... -json -count=1
```

The focused race campaign used Ubuntu-24.04, Go 1.22.2 and CGO. Its selection was
`TestControl|TestCallbackCheckpoint|TestTransportationCheckpoint|TestRecovery|TestTerminalPublication|TestLocalNotification`,
repeated three times. Reported package times were object 10.713s, gRPC 1.374s,
application server 23.785s, SDK state 8.516s and save/restore 1.061s. These are test
execution times, not RTI performance measurements.

Exact final evidence locations:

- `D:/workspace/gorti/control-outcome-full-root-round4-20261001.jsonl`
- `D:/workspace/gorti/control-outcome-final-focused-race-20261001.log`
- `D:/workspace/gorti/transportation-checkpoint-bounded-freeze-x3-20261001.jsonl`
- `D:/workspace/gorti/go-sdk-control-outcome-final-live-20261001.log`
- `D:/workspace/gorti/p647r-17255b13`
- `D:/workspace/gorti/java-control-outcomes-final-live-20261001.log`

The final descriptor golden is
`d96d5162bbaf25a45848172f0de8b5fbd75c81b2c69770f41ef4b614a9d5ff67`;
the unchanged RPC golden is
`58eac12e34a95e35a32a522a88b14a9a9a1d1f8787578d7cac4f4657cb36504d`.
Historical descriptor projections passed in the integrated source campaign.

## Intermediate Results

The earlier 3,276-test integrated pass predates the final review fixes and is not
the final-source verdict. Round 3 failed two capacity assertions under an interim
acknowledgement-exclusion policy and an older test expecting successful restore
state after terminal failure. The final policy counts all live and leased
authority, including unconsumed acknowledgements. The old failure test now checks
retained committed state, an explicit error and failed protocol state together.
Round 4 passed without suppressing those cases.

The first final-binary Python campaign passed three cases and failed an immediate
`rglob` file-presence assertion in the Integer64 case. The source client had
observed `FederationSaved`; subsequent inspection found the committed bundle and
history. Fixture review reproduced normal Python file checks returning false for
264/274-character Windows paths and found the files through extended-path checks.
An unchanged-fixture rerun under a shorter evidence root passed all four cases.
The redundant glob assertion was then removed; the latest four-case rerun completed actual restoration
from a fresh server and checked the saved identities, exact time, callbacks and
results. This is stronger persistence evidence, not a claim that the original run
passed or that an RTI defect was diagnosed. Its failed transcript remains in
`D:/workspace/gorti/python-control-local-final-647a999b-20261001-a4898d41`.

## Review Cycle

Plan: inventory served callback bodies and separate body, outcome and completion
states. Do: add regressions before changing ledger, SDK and restore composition.
Review: test failed publication, stale capabilities, reconnect ordering, capacity,
malformed state, real SDK-produced images and fresh-process restoration.
Reflect: preserve failure transcripts and reject unknown work rather than making
unsupported exactly-once claims.

Review reproduced stale advisory recovery, fabricated outcome metadata,
undeliverable completion markers, marker eviction, capacity overbooking,
unbounded local deduplication history and restore-success reports after failed
staging. The fixes have dedicated regressions. A further transportation review
identified nested activation authorization that requires fresh target issuance;
its integrated precommit reservation and actual TCP evidence are recorded above.
Review also corrected a one-shot lease-cleanup race and the interim omission of
unconsumed acknowledgement capacity. No failed transcript was relabeled as a pass.

## Assessment Boundary

Known finished callbacks can be recovered without executing their bodies again.
This does not make arbitrary user side effects transactionally exactly-once:
an application crash during a callback still leaves an unknown result, and
checkpoint export during an executing callback fails closed.

The supplied IEEE redlines remain private and are not redistributed. The full
2010 quality-gate readiness check remains unsuccessful because the applicable
normative inventory, clause-to-test traceability and independent evidence are
incomplete. This report records implementation evidence, not certification or a
whole-standard conformance verdict.

The final metadata-only readiness check is
`D:/workspace/gorti/control-outcome-final-full-readiness-20261001.json`:
`verdict: FAIL`, `conformance_established: false`. It checks full-assessment
prerequisites, does not execute RTI tests, and is distinct from the successful
source, race and actual-server campaigns above.
