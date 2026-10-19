# Remaining Service Implementation

This record continues the [live-service checkpoint](ieee1516-2010-live-services-20260920.md).
It describes executable product behavior in the development working tree.
Whole-standard conformance remains a separate evidence decision.

## Implementation

| Area | Implemented behavior |
|---|---|
| Federation sessions | Join issues a private capability. Automatic resign settings authenticate the live membership. Stream loss starts a reconnection grace period; stale sessions cannot alter or clean up a replacement membership. Checkpoints defer cleanup and transient callback-capacity failure retries without changing the configured action. |
| FOM switch | The explicit automatic resign directive is parsed, validated and preserved through MIM composition. An omitted directive uses the documented product default `NoAction`. |
| Restore | An explicit source generation selects one exact saved execution. Source FOM and all typed state references are validated, target identity is pinned, and fresh handles replace source references across managers and historical retraction records. |
| Snapshot completeness | Synchronization failure history and automatic resign directives survive restore. Live authentication credentials and membership incarnations never come from saved state. Invalid cross-manager references fail before publication. |
| Object deletion | Timestamped deletion retains pending state and retraction identity. Accepted removal delivery is irreversible even if later grant delivery fails. Pending deletion, binary tags, late discovery and no-recipient progression survive checkpoints. |
| Ownership | Release denial keeps ownership, cancels matching acquisitions and groups unavailable callbacks. Complete-set validation and callback reservation precede mutation. |
| Names | Multiple object-name reservation release is one atomic service, not repeated single-name calls. |
| LocalLRC | Explicit reinitialization after restore uses an authenticated snapshot and an ordered callback marker. Retired public tickets never alias newly opened wire-stream sequences. Failure retains confirmed behavior. |
| Python callbacks | Explicit evoked and immediate models serialize callbacks independently of the network I/O loop. Evoked mode runs user callbacks only when evoked; allowed callback-to-service calls work without an I/O-loop deadlock. |
| SDK service surfaces | Go, Python and C++ expose additional ownership, support, save/restore, declaration and object operations. Callback metadata and exact atomic-operation results are retained. |
| MOM | Service outcomes are observed at handler completion, including persistent streams. Authenticated callback receipts measure actual SDK invocation separately from send/fanout acceptance. Regional service reports use committed regions and consistent normalized values. Queries distinguish actual measurements from unavailable coverage. |

## Implementation Cycle

1. Plan: identify unsupported paths and define state, callback, failure and
   compatibility boundaries before selecting changes.
2. Do: add failing regressions, then implement domain, adapter and SDK changes.
3. Review: inspect independent service boundaries and exercise actual server
   composition, not only recording substitutes.
4. Reflect: convert every concrete finding into a regression, revise the
   implementation and repeat focused verification before the integrated campaign.

Review corrections include:

- Rechecking session authentication under the retained subscription membership
  lease, including fail-closed behavior when an embedded composition lacks it.
- Binding signed explicit resign requests to the authenticated membership's
  generation, name and incarnation, with the shared lease released before the
  exclusive lifecycle operation. Both stream modes retain precise authentication
  failure status codes.
- Retrying transient outbox admission failure after automatic resign expiry,
  while leaving permanent ownership preconditions explicit.
- Using authoritative object knowledge for hydration rather than historical
  registration, which would resurrect a locally deleted object.
- Correcting deletion/ownership callback reservation lock order and preserving
  irreversible removal after partial legacy-outbox delivery.
- Validating historical reflected-object references inside saved MOM metrics.
- Preventing callback-accounting reports from recursively generating reports
  about their own management traffic.
- Allowing checkpoint observation without allowing ordinary HLA mutation during
  save/restore, so protocol callbacks can still report actual invocation.
- Preserving the typed `MessageCanNoLongerBeRetracted` exception through the
  object RPC instead of exposing a generic SDK internal error.
- Accepting snapshots of dimensionless regions that the existing DDM service
  can create, while retaining cross-manager region association validation.
- Reporting failed callbacks with empty exception messages without losing the
  original application error or replaying its callback.
- Removing a redundant C++ automatic-resign directive cache whose unsynchronized
  updates could race between synchronous callers.

## Compatibility

Wire changes are additive and historical descriptor projections remain required.
The current descriptor and test-group manifest identify the exact accepted
surface. Legacy scalar MOM RPC counters retain their fanout/admission meaning;
new MIM-encoded maps carry separately measured values and missing-value reasons.

Restoration requires sufficient state, not merely a readable bundle envelope.
Missing synchronization failure history or retraction history is not invented.
Cross-generation selection is explicit; the label-only default is unchanged.
Save bundles keep their existing outer format. Raw execution transcripts are
not persisted in the repository. Session credentials never enter a save bundle
or event journal.

Current descriptor SHA-256:
`f17fb42299c07a885b91a8458d5b2cddd76c913563d8808aa5fde58360591ace`.
Current RPC-list SHA-256:
`fd99e5c36f80c1455fa366405701f2921fe7a2eb9ab7845b68552afd51b70532`.
Five historical descriptor projections are compared byte-for-byte. These hashes
identify the wire surface, not the entire uncommitted working tree.

## Verification Status

| Campaign | Result | External evidence file |
|---|---|---|
| Windows Go 1.26.5, all four Go source roots | 71 tested packages passed | `remaining-services-go-reviewed-final.log` |
| Ubuntu-24.04 / Go 1.22.2, the same roots with `-race` | 71 tested packages passed; no race warning | `remaining-services-race-reviewed-final.log` |
| Final Go static analysis | Passed | `remaining-services-vet-reviewed-final.log` |
| Go SDK final signed-resign change | Full SDK passed; focused race tests passed 10 repetitions | `go-resign-session-full-federate.log`, `go-resign-session-race.log` |
| Python SDK selected campaign | 676 passed; 3 TLS cases skipped because `cryptography` is absent | `python-sdk-full-integration-final.log` |
| Python actual callback execution and receipt campaign | 29 passed: 4 actual localhost cases and 25 unit cases | `python-callback-receipt-live-final.log` |
| C++ runtime contracts | 200 individual tests across 12 executables passed | `cpp-final-frozen-runtime.log` |
| C++ production server scenarios | 2 passed with a freshly built signed-resign server | `cpp-final-signed-production.log`, matching XML |
| C++ lifecycle repetitions | 41 tests repeated 25 times: 1,025 passes | `cpp-final-frozen-repeat.log`, matching XML |
| Bounded quality gate | All 110 groups passed; 473 required entries, 472 distinct test names; zero errors | `remaining-services-bounded-gate-final.json` |
| Quality-gate implementation tests | 28 passed | `remaining-gate-unit-final.log` |
| Full-2010 claim prerequisite check | Rejected as expected: 38 unmet prerequisites; no commands executed | `remaining-full-claim-negative-final.json` |

The positive and negative gate runs use the same manifest SHA-256:
`422c7a56f654ac2d4b74a214826d70ab94e43925240e6cf6d5aae35088bdb02d`.
The 31 required identifiers are bounded engineering requirements, not a complete
normative clause inventory. The gate records both conformance and certification
as false; the supported-profile pass must not be relabeled as a whole-standard
pass.

The Python unit cases in the receipt campaign overlap the selected SDK campaign;
the totals are not additive. The Python selection includes the 22 historical
integration cases that previously failed to start, but is not all of
`pysdk/tests`. Its exact commands are in `python-sdk-final-verification.ps1`.
Legacy launchers now isolate all listeners and use fresh external build
artifacts. Two stale expectations were corrected to require canonical FOM names
and actual object discovery before lookup, not to bypass service assertions.

Actual localhost Python cases cover both callback models and both ingestion
paths, including unavailable accounting before dispatch and measured invocation
afterward. The restart restore test composes two new servers with different
generations and initial FOMs and verifies the restored cross-manager state.
Independent C++ review reruns passed 55 session/receipt/concurrency cases and
24 restore cases; these are repetitions, not additional unique test counts.

A targeted C++ ThreadSanitizer run reproduced the removed directive-cache race.
The post-fix instrumented process failed to start with an unexpected-memory-
mapping error. Its dependencies were not fully instrumented. Therefore this
record does not claim a clean whole-C++ sanitizer campaign. Normal concurrent
tests and independent source review passed.

Raw evidence is stored under `D:/workspace/gorti/`, outside version control.
The source-root commands avoid old, inaccessible `pytest-*` directories that
make an unrestricted `./...` scan fail before package discovery:

```sh
go test -p 4 -count=1 -timeout=180s ./rti/... ./examples/... ./tests/... ./verification/...
go test -race -p 4 -count=1 -timeout=180s ./rti/... ./examples/... ./tests/... ./verification/...
go vet ./rti/... ./examples/... ./tests/... ./verification/...
python -B scripts/ieee2010_quality_gate.py --run
python -B scripts/ieee2010_quality_gate.py --claim full-2010 --check
```

## Evidence Boundary

The implementation does not substitute for a reviewed inventory of mandatory
and conditional requirements from the official IEEE texts. Full-2010 claim
mode must remain closed until that inventory and independent evidence exist.
Remaining MOM implementation gaps are explicit:

| Attributes | Missing implementation |
|---|---|
| `HLAfederateHost` | An opt-in, authoritative client host source. |
| `HLAROlength`, `HLATSOlength` | A consistent delivery-queue ledger spanning server and SDK state. The time-manager buffer alone is not this total. |
| `HLAtimeGrantedTime`, `HLAtimeAdvancingTime` | Complete elapsed-time accounting across actual state transitions. |
| `HLAcurrentFDD` | An exporter for the realized merged FDD, not concatenated source modules. |
| `HLAlastSaveName`, `HLAlastSaveTime` | Authoritative durable last-save history. |

Those attributes return missing-value reasons, not synthetic zeros. Legacy
snapshots without measurement history and streams without complete authenticated
callback reporting also cannot supply exact invocation totals. Service reporting
still has caller-identity and post-resign observation boundaries: mapping every
RPC name does not prove that every lifecycle operation can produce a MOM report.
These are implementation gaps, separate from the missing normative audit.

No new performance ranking is established by this functional campaign.
