# Callback, SDK Checkpoint and MOM Verification

Updated: 2026-10-01. Branch: `codex/ieee2010-feature-completion`.

This campaign continues the
[implementation record](ieee1516-2010-completion-progress-20261001.md).
It does not promote the supported-profile gate to whole IEEE conformance.
Source, runtime and actual-server evidence are recorded separately.

## Plan

- Persist unentered discovery/data callbacks across ready, transport and client stages.
- Prepare fresh callback capabilities before restored queues become visible.
- Publish the restored identity/epoch terminal before replayed data.
- Validate private SDK runtime formats and explicit identity references before
  reserving target identities. Reject unknown or corrupted formats atomically.
- Exercise every implemented typed MOM service through the real server and wire
  entry point, with state/callback oracles and negative admission scenarios.

Six parallel work areas own SDK state validation, Go, Python, Java, delivery
staging/independent review, and MOM services. Server composition and required
receipt replay are integrated separately.

## Implementation

`callback_pending` is an additive checkpoint component, not a new RPC. It stores
detached canonical Protobuf data and recipient/admission order, but never the
original callback capability. Independently admitted equal messages remain
distinct. Entered callbacks are excluded from data replay. Reconnect preserves
admission order rather than service sequence order. A publication lock pairs
ordinal assignment with queue publication without holding the ledger lock across
queue commit. Unknown fields, noncanonical wire bytes and duplicate JSON
keys/case aliases reject before restored state publication.

Restoration validates format, recipient roster, candidate FOM, scalar time and
pending copied-region leases before publication. The restore transition reserves
all terminal/replay capacity and new receipt entries together. Release preserves
the old queues and identities. Commit publishes the terminal first and fences
old callback receipts. An internal fresh execution handle must not be confused
with the original saved handle conveyed by `initiateFederateRestore`.

This component covers discovery and data callbacks (reflection, interaction and
removal). Unsupported pending required controls reject capture rather than
silently disappearing. The active save protocol's own lifecycle bookkeeping is
excluded. This safety policy is not complete control recovery.

### SDK Runtime And Session Identity

Go, Python and Java saves provide nonempty GRS1 private runtime envelopes through
the existing `client_state` field. The SDK-state adapter validates format,
checksum, source identity, scalar and embedded state before handle reservation.
Explicit historical producer references participate in the same target mapping.
Object/class/region/retraction sequence IDs are not renumbered. Translated state
is hydrated at the successful terminal, not initiation. Failed restoration does
not install the candidate. The original saved handle in initiation is distinct
from the target-scoped private envelope identity.

Data recovery belongs to the server: private pending metadata does not authorize
replay of old capabilities. SDKs fence held old-epoch callbacks and reject export
of unsupported opaque capabilities, executing bodies or unresolved outcomes
according to their binding policy. Empty legacy state remains accepted but does
not reconstruct private SDK caches.

Authenticated handleless DDM/Support queries and transportation activation now
resolve stable session name/generation/incarnation to the current restored handle.
The stable session record itself is unchanged; replacement memberships reject.

### MOM

Real server tests check all 32 implemented service leaves: positive effects and
successful service reports, plus unsigned, foreign-token, unknown-target and
undefined-parameter rejection, in both time families. Separate cases cover 15
standard report requests per factory, regional service-report routes,
malformed/exception reports and deferred transportation confirmation.

Traffic counters separate Reliable and BestEffort. Sender hooks capture accepted
transport before later overrides; mixed attributes count once per used transport
partition, without receiver-count multiplication, including no-subscriber sends.
Receiver counters use actual callback metadata at authenticated entry. Unknown
measurement remains unavailable without blocking the callback body. Reliable
snapshot v1 remains readable; v2 retains BestEffort. The unsupported-version
fixture now uses version 3, not valid version 2.

## Executed Evidence

| Campaign | Result | Boundary |
|---|---|---|
| Initial parent native subset | PASS: 10 distinct top-level tests x3, 81 test/subtest pass events | Earlier receipt codec/admission run, before final review fixes |
| Fresh server restoration | PASS: Float64/Integer64 by ready, client-unentered and client-entered; three repetitions | Two independently admitted equal interactions; reverse join order; saved source shutdown and new execution; old receipt rejection |
| Delivery and graph extension | PASS: nine new tests, three repetitions; focused Linux race PASS | Terminal-first pending batches, capacity/rollback, detached graph validation and explicit identity rewrite |
| Settled native Go roots | PASS: 3,226 top-level tests, 8,990 test/subtest pass events; zero failures/skips; 78 tested and 10 no-test packages | Native Go 1.26.5; rti, tests, examples, verification |
| Fresh server copied regions | PASS: both time families, three repetitions | Exact bounds, authenticated receiver activation/release, Integer64 TSO above 2^53 |
| Final callback checkpoint race | PASS: three repetitions | grpc/server checkpoint tests; Ubuntu-24.04 Go 1.22.2 |
| Final session package race | PASS: full package x3 | Stable identity, restored handle and replacement rejection |
| Independent checkpoint review | PASS: 16 distinct top-level tests x3, 48 top-level and 102 subtest pass events; native and race | Complete review selection without exclusions |
| Captured sender transportation race | PASS: three repetitions | New object-service transport partition hooks |
| Go SDK native/race | PASS: 174 native top-level tests; full SDK race x3 | Separate bounded SDK campaigns |
| Go SDK final executable | PASS: six factory cases | Actual cross-generation restore, three repetitions |
| Python SDK selected | PASS: 132 tests | Runtime/callback/restore selection, not whole SDK |
| Python SDK final executable | PASS: two factory cases | Fresh clients and recreated federation; server process retained |
| Python SDK actual server restart | PASS: two additional factory cases | Source server stopped and waited before target launch; same absolute save directory; fresh client processes and nonempty saved SDK queues |
| Java SDK selected | PASS: 418 tests; no failures/errors/skips | Non-live selection, including independent central codec exchange |
| Java SDK final executable | PASS: two factory cases | Fresh save/restore JVMs and recreated federation; server process retained |
| MOM actual-server focused | PASS: eight top-level tests x3, 1,422 test/subtest pass events | 32 leaves x two factories x3: 192 positive and 768 rejection leaf executions |
| MOM actual-server race | PASS: TestMOMFull selection x3 | Ubuntu-24.04 Go 1.22.2; 28.709s, no race warning; not whole-server race coverage |
| MOM traffic compatibility | PASS | Reliable v1, partitioned v2 and unsupported-version rejection |
| Existing supported-profile gate | PASS: 226/226 groups | Unchanged bounded Go manifest, not all new cases |
| Full-2010 readiness | FAIL; conformance_established false | Inventory/traceability and independent evidence remain incomplete |

Parent native transcript:
`D:/workspace/gorti/callback-restoration-parent-20261001.jsonl`.
Delivery/graph evidence:
`D:/workspace/gorti/delivery-savebundle-handoff-20261001.md`.
Counts from separate campaigns are not summed as independent requirements.
The settled native command is:

```text
go test ./rti/... ./tests/... ./examples/... ./verification/... -json -count=1
```

Root-wide `./...` could not enumerate the pre-existing inaccessible
`engineering/task-rl-106-vxunp6cq` directory. That failed enumeration is retained
separately, not called a passing root-wide run. Explicit product/test roots
completed without changing the unrelated directory.

Final SDK executable: `D:/workspace/gorti/rtid-callback-sdk-mom-assessed-20261001.exe`.
SHA-256: `37b7b7f3011907b7e86a4cf7d1991113e6d92334d1403495ea417bc12a2e2c19`.
Final evidence remains outside the repository:

- `D:/workspace/gorti/callback-sdk-mom-go-assessed-round3-20261001.jsonl`
- `D:/workspace/gorti/callback-checkpoint-integrated-race-assessed-20261001.log`
- `D:/workspace/gorti/session-current-caller-race-assessed-20261001.log`
- `D:/workspace/gorti/callback-checkpoint-review-handoff-20261001.txt`
- `D:/workspace/gorti/sent-transport-integrated-race-assessed-20261001.log`
- `D:/workspace/gorti/callback-sdk-mom-quality-assessed-20261001.json`
- `D:/workspace/gorti/callback-sdk-mom-full-readiness-assessed-20261001.json`
- `D:/workspace/gorti/go-sdk-runtime-handoff-20261001.md`
- `D:/workspace/gorti/python-runtime-handoff-20261001.md`
- `D:/workspace/gorti/python-runtime-server-restart-assessed-20261001.log`
- `D:/workspace/gorti/java-runtime-restoration-handoff-20261001.md`
- `D:/workspace/gorti/save-restore-runtime-port-handoff-20261001.md`
- `D:/workspace/gorti/mom-full-verification-20261001.md`
- `D:/workspace/gorti/mom-full-integrated-race-assessed-retry-20261001.md`

The unchanged manifest SHA-256 is
`5513aee07cbbd82c0a59199b98434ee22f3969405779d9a1c80e717049ee7edd`.
New restoration/MOM tests are separately executed evidence, not silently added
to this gate's existing requirement inventory.

## Review and Reflection

The first production fixture assumed one callback per transport batch and lost
the remaining events. It was corrected to retain each complete batch. The source
and terminal handles were also distinguished rather than changing the standard
saved-handle argument to satisfy an incorrect assertion. Failed intermediate
runs are not reclassified as passing production evidence.

Independent review reproduced reconnect reordering, optional measurement records
becoming mandatory recovery obligations, ambiguous JSON snapshots and a restored
copied-region query resolving the old session handle. Regressions preceded fixes.
MOM live assertions exposed hardcoded Reliable sender/receiver accounting and now
pass with actual transport partitions. The earlier restart fixture now invokes
both synchronization announcements before save and checks that an already-entered
announcement is not repeated after restore. The obsolete BestEffort-failure
fixture now exercises unknown transportation; positive BestEffort is tested live.

Full-standard assessment still requires the complete applicable requirements
inventory, clause-to-test traceability and independent evidence. The conformance
manifest remains fail-closed for the full-2010 claim.

## Remaining Acceptance

The subsequent [control and result recovery campaign](ieee1516-2010-control-outcomes-20261001.md)
updates the first three items below. They describe this earlier checkpoint, not
the implementation status of the subsequent campaign.

- Complete pending control/ownership/transportation capability recovery; explicit
  rejection is not recovery.
- Extend the general checkpoint safety policy to the separate ownership receipt
  ledger and validate recipient-known object classes against pending carriers.
- Establish durable entered-but-unreported outcome handling without repeating
  bodies; portable SDK export currently rejects unresolved outcomes.
- Extend restoration proof to every private cache and supported legacy format.
- Complete MOM clause/exception permutations and independent-client evidence;
  the literal implementation catalog is not every normative obligation.
- Complete the applicable IEEE 1516/1516.1/1516.2 inventory and independently
  reviewed execution evidence. No certification/conformance-label claim is made.
