# IEEE 1516-2010 Implementation Progress

Updated: 2026-09-20.

Target: the 2010 editions of IEEE 1516, IEEE 1516.1, and IEEE 1516.2.
The 2025 edition is not an implementation target for this work.

This record supplements the [source assessment](ieee1516-2010-conformance-assessment.md).
It records tested changes, not a declaration of complete conformance. Work
was performed in the existing HLA-refactoring working tree; unrelated changes
were preserved. No release, commit, or benchmark comparison was produced.

## 2026-09-20 Service Implementation Checkpoint

The [service checkpoint](ieee1516-2010-service-completion-20260920.md) records
new caller-local object knowledge, declaration options and transactional
advisories, per-call DDM routing, real MOM traffic reports, bounded scheduled
checkpoints, FOM metadata, and C++ callback/support work. Read its remaining
implementation table before interpreting these changes as service completion.
Join-time FOM extension and full timed-save are still incomplete.

Windows full Go regression, Linux RTI race tests, and static analysis passed.
The manual gate passed 59 groups and 306 required top-level tests; focused
Python SDK tests passed 37 and gate-script tests passed 28. Full-readiness
correctly remains FAIL. Exact scope, transcripts, SDK results and outstanding
functions are recorded in the service checkpoint.

C++ Linux runtime fixtures passed 161 tests across ten executables after the
final library rebuild. A separate ten-case lifecycle stress set passed twenty
repetitions. These are not native Windows or production-server C++ campaigns.

## 2026-09-19 DDM Registration And Range Validation

The next bounded milestone addresses F11/F12 region registration, dimension
metadata/context and numeric bounds. All three SDK registration paths now use
one existing DDM RPC. The server validates complete bindings before creating
the object, initializes ownership before discovery, preserves state on rejected
requests and clears associations on deletion. Ordinary and regional reflection
recipients are combined. Matching-dimension registration and same-roster
association restore are exercised through a real server subprocess.

Final Windows full Go regression and WSL Linux race campaigns passed. The
manual gate passed **37 groups and 215 required top-level tests**, with no failed
or skipped required tests. Python's new focused SDK suite passed 19 tests;
gate-script regression passed 28. Full-readiness remains **FAIL** with no runtime
commands started and no conformance established. These are engineering test
counts, not normative coverage percentages.

Manifest SHA256:
`d3c927e793feb236280c87d3ccf0c9b51ff074d48686c7793d220fffdbc8dcce`.
Transcripts: `D:/workspace/gorti/ddm-evidence-20260919/bounded-gate.json` and
`D:/workspace/gorti/ddm-evidence-20260919/full-readiness.json`.

See the [DDM report](ieee1516-2010-ddm-registration.md) for the
plan/do/review/reflect record, SDK compatibility boundary, tested profiles and
open cases. The production audit observer remains non-interfering and reports
unrecordable scoped registrations; such journals are not complete DDM replay
evidence. Per-call region sends/requests, remaining DDM service contexts and
normative Q09 review are still open. No commit, push or benchmark was performed.

## Earlier 2026-09-19 Same-Roster Checkpoint State

The next milestone addresses audit F01-F03 with typed object/declaration/time
snapshots, complete tracked MOM runtime state, and required-state production
composition. The [checkpoint report](ieee1516-2010-checkpoint-state.md) records
the plan/do/review/reflect cycle, compatibility boundary and remaining work.

The production path now prepares all seven service snapshots before mutation,
waits for every participant's successful response, reserves all terminal
callbacks, revalidates provenance/roster, and commits. Abort, participant failure,
invalid snapshots or terminal reservation failure leave live RTI state unchanged.
Changed participant identities and missing required keys fail closed. Ordinary
production RPCs, stream mutations and admin lifecycle changes use checkpoint
admission; callback consumption and protocol completion remain available.

Review corrections cover disabled-service typed nils, idle admission-lock
retention, admin generation selection, failed object-registration name residue,
Unicode/duplicate JSON keys, unsupported bundle versions, size limits and short
writes. A final callback adapter failure after state commit is not described as
a rollback. External Protobuf descriptors/RPCs and SDK signatures are unchanged.

Final executed results for this working tree:

- Windows Go 1.26.5: uncached `go test -count=1 ./rti/... ./tests/... ./verification/... ./examples/...` passed.
- WSL Ubuntu 24.04 / Go 1.22.2: uncached `go test -race -count=1 ./rti/...` passed.
- Production subprocess tests passed for state restoration, pending TSO ordering,
  failed/aborted restoration and disabled SavepointService. The first three
  scenarios also passed five repeated Windows executions during integration.
- Local/manual evidence gate: **26 command groups and 158 required top-level
  tests passed**, with no failed or skipped required tests. These counts are not
  a percentage of normative IEEE obligations.
- Gate implementation regression: 28 Python standard-library tests passed.
- Full readiness check: **FAIL**, before executing tests, as intended while
  normative coverage and independent evidence are incomplete.

Gate manifest SHA-256:
`ffb2b7d7553472209ce87c30a80f376ff5962e0fc2eb60c96897719978d222ec`.
Transcripts remain outside Git:
`C:/Users/cbchoi/AppData/Local/Temp/gorti-ieee2010-checkpoint-gate-20260919.json`
and `gorti-ieee2010-checkpoint-full-readiness-20260919.json` in the same directory.
They record executed observations, not an authenticated release identity.

No new SDK-language, performance or external conformance campaign is claimed.
Scheduled saves, restored identity rebinding, transport epochs, restart recovery,
the remaining DDM/service defects and normative Q04 review are still open.
Applications must quiesce and drain mutation/callback work before checkpoints;
server-state snapshots do not capture unread network or client callback queues.

## Earlier 2026-09-19 Integration Checkpoint

The [whole-RTI audit](ieee1516-2010-full-rti-audit.md) identifies remaining
implementation defects separately from missing normative interpretation. Full
RTI conformance remains unestablished; the full standard texts are not available.

This checkpoint repairs the following independently reproduced paths:

- FOM and MIM XML retain each class's qualified ancestry. Equal local names in
  different branches no longer alias. Qualified lookup rejects an incorrect
  path; local-name lookup succeeds only when unambiguous. Attribute and parameter
  lookup walk the correct branch, including inherited `HLAfederate`.
- The production repository, ObjectService RPC, object registry and MOM
  dispatcher now deliver service/exception reporting adjustments to their
  explicit target. Tests use the embedded official MIM, a 64-bit target handle,
  malformed target encodings, and the LocalLRC wire entry point.
- Identical or empty class containers repeated across FOM modules are coalesced
  before handle allocation. Conflicting definitions and duplicate siblings are
  rejected with product diagnostic `FOM-014`. Merged inheritance is checked for
  duplicate members. This is not complete modular-FOM support or join-time module
  extension.
- Ownership queries validate caller object knowledge and defined attributes
  before taking the owner snapshot under the object lock. Unknown, unseen or
  deleted objects cannot be mistaken for valid unowned attributes. Query tests
  assert no journal append, ownership mutation or callback.
- Ownership confirmation/acquisition tags retain binary bytes. A new additive
  divest-with-result RPC returns the attributes committed by that operation,
  without a later query. Candidate selection and transfer commit share the
  ownership lock, before observers and callbacks.
- Go SDK handle tables use qualified identities and matching inherited member
  numbering. Unique local names remain usable, including existing callback name
  projections; ambiguous names require a qualified path. Tests compare server
  and SDK numeric handles from the same FOM/MIM.
- NaN advance targets are rejected before pending time-request mutation for
  TAR, TARA, NMR, NMRA and FQR. Domain tests cover fresh, advanced and pending
  states, callbacks and grant journals. Actual gRPC tests assert error trailers
  and successful subsequent progress. Existing past-time errors and the current
  `+Inf` final-time sentinel are preserved.

The two XML fixtures previously labeled cyclic/multiple-parent reused only a
local name, not a qualified class identity. They now live under
`tests/conformance/foms/good/` as positive regressions. Parser unit tests still
reject explicit cyclic and multiple-parent graphs in legacy programmatic models.

Compatibility details:

- Class ordering remains a stable sort by local name. Removing duplicate
  cross-module containers can change handles for formerly ambiguous models;
  all participants must load the same modules using the updated handle rules.
- Server class-name queries return qualified names for XML-backed classes.
  Programmatic models without qualified names retain their explicit names.
- Existing RPCs and fields remain present. New fields carry confirmation tags,
  acquisition-notification tags and the querying federate handle. The original
  empty-response divest RPC remains available alongside the result RPC.
- Production ownership queries now require the caller handle. Old raw requests
  omitting it are rejected instead of bypassing membership/object-knowledge
  validation. Updated SDK requests include it.

These corrections do not close all of MOM or Ownership. In particular, the
counter-report handlers still produce `HLAcount`, which is not the parameter
layout of the standard MIM report interactions. A recording-emitter unit test
cannot establish that those responses encode successfully with the production
emitter. Remaining items are tracked in the matrix and whole-RTI audit.

### Plan, Do, Review, Reflect

Plan: prioritize reproducible service defects and require a successful real
MIM/registry path, not only a manually named dispatcher invocation. Do: repair
identity, inheritance, ownership snapshots/tags/results and invalid time targets.
Review: cross-layer checks found the independent SDK leaf-name table and repeated
module-container problem; both received additional regressions and corrections.
Reflect: retain the whole-RTI gate as failing until remaining implementation
gaps, normative applicability and independent evidence are resolved. The next
major milestones are consistent Save/Restore state, complete callback metadata,
and atomic region-aware DDM operations. Each needs its own behavioral oracle and
cross-binding campaign before it can be closed.

### Cross-Binding Commands

The Python ownership campaign used Python 3.14.0, pytest 9.1.1,
pytest-asyncio 1.4.0, grpcio/grpcio-tools 1.84.0 and protobuf 7.36.2. With the
updated bindings generated, a freshly built `rtid` supplied as `RTID_BINARY`,
and `pysdk` on `PYTHONPATH`:

```text
python -m pytest pysdk/tests/test_ownership_n04.py pysdk/tests/test_ownership_n04_integration.py pysdk/tests/spec/m39/test_m39_event_translation.py pysdk/tests/spec/m39/test_m39_layer2_callbacks.py -q -p no:cacheprovider
```

All 24 cases passed, including production-server binary tags, exact committed
results, and the absence of follow-up ownership queries. The server fixture
terminates its process. Missing `RTID_BINARY` skips integration and is not an
acceptable substitute for this executed result.

The C++ checks ran the GoogleTest executables directly in WSL Ubuntu 24.04,
not through CTest. Build the current SDK/test targets and a Linux `rtid`, then:

```sh
cppsdk/build-ieee2010/tests/dlc/conformance/_runtime/test_conf__runtime_ownership_contract
cppsdk/build-ieee2010/tests/dlc/conformance/_runtime/test_conf__runtime_callback_bridge --gtest_filter='*Ownership*:*Acquisition*:*Divestiture*'
RTID_BINARY=/absolute/path/to/rtid cppsdk/build-ieee2010/tests/test_ownership_xfed_integration
```

The runs passed 3 standard-API-shaped contract cases, 5 callback cases and 5
production-server ownership cases. The latter includes
`OwnershipXFedIntegration.N04BinaryTagsAndAtomicDivestResult`. These are bounded
SDK/server tests, not a complete official-header or ABI conformance campaign.
Their output was inspected in the execution console, not stored as a permanent
release-evidence archive. Generated bindings and binaries remain outside tracked
source. No server process from these campaigns was left running.

### Integrated Verification (2026-09-19)

| Check | Executed result |
|---|---|
| Windows Go 1.26.5, `go test -count=1 ./rti/... ./tests/... ./verification/... ./examples/...` | PASS |
| WSL Ubuntu 24.04 / Go 1.22.2, `go test -race -count=1 ./rti/...` | PASS; no race report |
| Local evidence gate | PASS: 16 commands, 62 required top-level tests, no skipped required cases |
| Gate implementation unit tests | 28 PASS |
| Python ownership/callback selection | 24 PASS, including a production-server test |
| C++ ownership selection | 3 contract + 5 callback + 5 production-server cases PASS |
| `--claim full-2010 --check` | FAIL as required; zero test commands started, no conformance declaration |
| `git diff --check` | PASS |

Gate transcripts are outside Git under the current user's temporary directory:
`gorti-ieee2010-gate-20260919.json` and
`gorti-ieee2010-full-readiness-20260919.json`. They identify selected commands and
raw observations. The working tree remains uncommitted and is not an immutable
release-evidence snapshot. Performance, Windows-native C++, and a complete
independent RTI conformance campaign were not run in this checkpoint.

## Earlier Implemented Corrections

| Assessment area | Change | Evidence and remaining scope |
|---|---|---|
| C06: MOM switch encoding | Decode `HLAswitch` using four-byte `HLAinteger32BE`, reject invalid lengths/enumerators, distinguish omitted parameters from present empty encodings. Validate both federate switch values before mutating state. | Existing dispatch tests and new external contract tests cover all four switch-handler families, omitted values, malformed vectors, and partial-mutation rejection. This closes the identified decoder defect, not the whole MOM service audit. |
| C03: C++ resign argument | Map all six standard enum values explicitly to the different wire enum values. Preserve the legacy no-argument M17 overload. Reject unknown values in the SDK and federation manager. | A local gRPC test endpoint verifies the actual request received from the standard C++ API for every action. The server corrections below add action-specific state checks. |
| C03: failed resign | Keep the C++ callback reader alive until resign succeeds. | A gRPC endpoint rejects resign with `FederateOwnsAttributes`; a subsequent synchronization callback is delivered and resign can be retried. |
| C04: federate identity | Forward federate type independently from name. The unnamed standard overload sends an empty name, and the server assigns a unique name under the federation lock. | C++ wire tests and Go membership tests cover type preservation, two same-type unnamed joins, and a generated-name collision with an existing explicit name. Join-time FOM modules remain open. |
| C04/C10: logical-time factory selection | Accept the default or `HLAfloat64Time`; reject unsupported factories with `CouldNotCreateLogicalTimeFactory` before federation creation. | All three standard create overloads are checked. This does not implement integer time or establish which factories are mandatory under the normative conformance provisions. |
| Integration prerequisite: generation fencing | Preserve the generation returned by create; discover it for externally created federations; send it on C++ join/destroy. Cached generations are not silently replaced after a stale request fails. | C++ wire tests cover creation tokens, discovery, and rejection of a same-name replacement. Existing actual-server lifecycle tests exercise production fencing. |

The initial correction batch did not change Protobuf descriptors, RPCs,
event-log or save-bundle formats, or the standard C++ abstract API.
Internal M17 overloads were added without removing the existing ones.
Clients that previously sent a nonstandard one-byte MOM switch must now send
the standard four-byte representation.

## Verification

The list below records the initial batch. Later integrated results are recorded
in the final verification section at the end of this document.

- Windows, Go 1.26.5: `go test ./rti/...` passed, including the wire-descriptor
  golden test, HLA services, adapters, application, Go SDK, and spec suites.
- WSL Ubuntu 24.04, Go 1.22.2: `go test -race` passed for
  `./rti/internal/hla/mom`, `./rti/internal/hla/federation`,
  `./rti/tests/unit/mom`, and `./rti/tests/integration/spec/M24`.
- C++ verification uses GCC 13.3, CMake 3.28.3, Protobuf 3.21.12, and gRPC 1.51.1.
  The build now supports CMake's `FindProtobuf` fallback for system packages
  that do not ship a Protobuf config package.
- Final C++ rerun: all six `conformance__runtime` CTest targets passed.
  The new federation contract target contains eight cases; the other targets
  cover exceptions, callback conversion, encoding, MOM API shape, and bridge
  error translation.
- Actual-server C++ lifecycle tests passed, including two unnamed federates
  of the same type joining the newly built Linux server. These exercise real
  gorti services; the independent local gRPC endpoint above separately checks
  exact request fields and injected failure behavior.
- Python, Windows-native C++, full independent RTI conformance testing, and
  performance measurements were not run for this batch.

New tests are under `rti/tests/unit/mom` and
`cppsdk/tests/dlc/conformance/_runtime`. Existing affected fixtures were
updated in place. Generated C++ and build output remain gitignored.

## Server Resignation Contracts

The composed server now prepares object and ownership effects before writing
the resign journal record or removing federation membership. Preparation holds
the service locks while reserving capacity for all resulting callbacks. A
validation, reservation, or journal failure releases those resources without
changing membership, object state, ownership, or outbound callbacks.

- `NO_ACTION` and cancellation-only actions reject remaining owned attributes.
- Actions without acquisition cancellation reject pending acquisitions.
- Deletion enumerates actual object handles and checks the current
  `HLAprivilegeToDeleteObject` owner, including transferred privilege.
- Divestiture can satisfy a queued acquirer and emits its acquisition callback.
- Cancel/delete/divest handles attributes on objects that are not deleted, not
  only the departing federate's registered objects.
- Deleted objects lose all ownership and pending transfer records. Removal
  callbacks target federates that know the object, including a prior registrant
  after privilege transfer and subscribers that subsequently unsubscribe.
- Deleted names become reservable again on commit. An injected journal failure
  keeps the original name binding and registered-name reservation intact.
- Federation join/resign records now have a domain DTO that the journal codec
  can actually encode. Replay preserves all six resign actions, accepts the
  historical unspecified-action default, and rejects unknown recorded actions.

The production outbox reservation contract makes commit infallible for capacity
and lifecycle failures. An adapter violating that contract can still return an
internal commit error after accepted state mutation; validation rollback is not
a claim of recovery from arbitrary adapter defects or a process crash.

Evidence: `rti/tests/integration/spec/M24/resign_contract_test.go` covers all six
actions, failed preconditions, handle 41 with only one live object, transferred
privilege, queued acquisition, unsubscribe, combined cancel/delete/divest, and
injected reservation/journal failures followed by retry. The M24 suite passed
20 repeated Windows runs. WSL race checks passed for M24, federation, object,
ownership, and the composed server before the final integration rerun.
`resign_wire_contract_test.go` additionally verifies the actual gRPC
`FederateOwnsAttributes` and `OwnershipAcquisitionPending` exception trailers,
membership preservation, and successful retry; the M24 repeat run includes it.

This closes the identified resign composition defects. It does not close all
ownership services: invalid-object acquisition, ordinary divestiture atomicity,
ownership tag propagation, automatic resign policy, and normative edge cases
remain separate work in the [requirements matrix](ieee1516-2010-requirements-matrix.md).

## Transportation and FDD Metadata

The four missing standard C++ transportation request/query methods now have
matching RPCs and domain implementations. Changes validate the complete
attribute set, current ownership, FOM handles, and interaction publication before
mutation. Queries distinguish the queried publisher and read attribute/class
defaults from the FOM, including inherited attribute declarations. The C++
binding queues populated confirmation/report callbacks after successful RPCs.

The new transportation integration tests cover default values, transferred
ownership, mixed valid/invalid requests without partial mutation, publication,
other-publisher queries, wire responses, and exception trailers. The composition
tests also check inherited defaults and invalid handles. Both suites pass.
These changes do not yet connect transportation overrides to every outgoing
message's routing and callback metadata; that remains in C01/C05.

The FOM parser now accepts the optional 2010 FDD `time` and `serviceUtilization`
sections without allowing those names in arbitrary XML positions. It validates
the 168 service entries declared by the official FDD schema, fixed attributes,
metadata structure, and time datatype references. Actual parser tests include
positive and negative cases, with ten repeated runs. The positive fixture and
three empty-section variants were independently validated against the official
FDD XSD; two negative mutations were rejected. This is bounded FDD support, not
full OMT/schema validation or runtime time-factory selection.

## Wire Compatibility

The subsequent completion batch adds four transportation RPCs and two
Save/Restore participant-transition RPCs. Status-vector fields and the ownership
snapshot `two_phase` flag are append-only additions. No existing field number,
enum value, or RPC was removed or changed. The descriptor/RPC golden was updated
after reviewing these schema diffs. Event-log file layout is unchanged; the
existing lifecycle messages now receive real domain data. Legacy snapshots with
no `two_phase` field retain their previous one-phase interpretation.

Save completion now requires an explicit successful save-begun transition.
Go/Python/C++ bindings and affected success-path fixtures use the explicit protocol;
wire compatibility alone does not make a client that skips this transition
behaviorally compatible.

## Save and Restore

Restore now reserves its federation operation before reading a bundle or
applying snapshots. Production snapshot adapters decode all images before
commit; legacy adapters require rollback images, including rollback of a
decoder that mutates and then fails. Save/restore persistence, completion,
abort, and destruction are serialized per federation. Pending ownership
two-phase divestiture survives save/restore instead of becoming an immediate
one-phase transfer.

An explicitly present empty snapshot now restores empty service state. An
absent entry in an older bundle retains the legacy behavior. Tests save an empty
ownership manager, populate it afterward, restore, and assert the live ownership
is actually cleared; a separate test proves legacy absence does not clear it.

The server checks per-participant begun/completion transitions, accepts a
participant-specific restore failure, and returns save/restore status vectors.
Go exposes those typed vectors. Python and C++ forward explicit begun/failure
RPCs and deliver the vectors through ambassador callbacks, including
disabled/resumed callback behavior. C++ no longer maps a participant's restore
failure to federation-wide abort. The synthetic C++ callback queue now observes
the same disabled-callback gate as stream callbacks. The new gRPC and snapshot
tests check errors and actual state, not only callback labels.

Scheduled saves remain unsupported: a future save timestamp is now rejected
instead of incorrectly initiating immediately. Full ordinary-service admission
during save/restore, complete restored identities/state, and all failure-reason
callback metadata remain work for C07. The narrower concurrency/state repairs
above do not establish a complete federation-wide restore transaction.

## Implementation Cycle

| Phase | Outcome |
|---|---|
| Plan | Separate resign/ownership, transportation/C++ binding, Save/Restore, and requirements/FDD work. Define state and callback observations before accepting API coverage. |
| Do | Add service preparation boundaries, missing request/query methods, participant state transitions, journal DTOs, and contextual FDD metadata validation. |
| Review | Review found deleted-name reservations surviving resign and empty snapshots being mistaken for absent snapshots. It also identified existing fixtures whose assertions would pass a no-op implementation. |
| Reflect | Add failure-preservation/name-reuse and empty-state restoration regressions, integrate SDK protocol transitions, and retain uncovered cases in the requirements matrix instead of treating compilation or method counts as conformance. |

## Remaining Milestones

| Priority | Milestone | Exit criteria |
|---|---|---|
| P0 | Ownership and resign completion | Bounded explicit-resign, query and tag/result contracts are implemented. Complete other ownership preconditions, callback failure behavior, automatic resign policy, and normative edge-case review. |
| P0 | Object, declaration, and transport services | Complete C01/C02/C05/C08: missing transportation services, order changes, local delete, per-federate object knowledge, subscription/advisory behavior, and callback metadata. Test all delivery paths, including timed and streamed paths. |
| P0 | Federation management and persistence | Complete RTI-owned Save/Restore state, isolation and restored identity; join-time FOM modules, remaining MIM handling, failure/status callbacks and automatic resign directives. Validate old event logs and save bundles. |
| P1 | Time, MOM, and support completeness | Audit supported time representations, enable-pending states, TAR/TARA/NMR/NMRA/FQR, retraction, zero lookahead, MOM targeting/encodings, and remaining support lookups. Include three or more federates and failed delivery. |
| P1 | Normative traceability and independent verification | Obtain/review the three full standards and applicable corrections; resolve binding and conditional obligations; map every applicable requirement to tests. Execute independent clients and relevant interoperability suites. |

The former unconditional post-resign release hook and the bounded numeric
object-handle probe have been removed from the composed server. Tests now
exercise state and callback consequences rather than only enum acceptance.

Generation-error exception mapping is also not closed by the fencing test:
the test asserts rejection and preservation of the stale token, not a
particular standard exception for this gorti-specific wire extension.

Other open findings in the assessment are still open unless explicitly
listed as corrected above. Full conformance is not inferred from API method
counts, compilation, passing subset tests, or a performance benchmark.

## Earlier Integrated Verification (2026-09-18)

- Windows Go 1.26.5: `go test ./rti/... ./tests/... ./verification/... ./examples/...`
  passed after all domain, adapter, FOM, SDK/fixture and benchmark-helper changes
  were integrated.
- `go test ./tests/... ./verification/... ./examples/...` passed, including
  the architecture dependency checks, FOM/encoding tests, Go verification
  programs, and all Go example packages.
- The unqualified `go test ./...` could not enumerate an existing
  `pytest-joined-qual` temporary directory because of its Windows ACL. The
  directory was not deleted or modified; explicit source-tree tests above
  avoid this unrelated discovery problem.
- WSL Ubuntu 24.04, Go 1.22.2: final `go test -race ./rti/...` passed. The
  first broad run exposed a benchmark-helper timer/send-versus-channel-close
  race. The helper now serializes sends, final flush and close under its
  recipient lock and rejects stale timer generations. Its package race suite
  passed 20 repetitions; focused outbox regressions passed 200 repetitions.
  Existing measurement-mode overflow behavior is unchanged.
- C++: final `ctest -R conformance__runtime --output-on-failure` passed all
  seven targets. The federation contract target has 12 cases; transportation
  has six. These include explicit participant RPCs, status enum/vector
  conversion, failed/malformed responses, and disabled/resumed callbacks.
- C++ actual-server tests against newly built `rtid-final` passed:
  `lifecycle_integration`, `savepoint_integration` (12 cases), and
  `save_restore_dlc_integration` (two cases). Tests exercise Go's running RTI,
  separately from the local gRPC endpoints used for exact wire assertions.
- Python: the focused Save/Restore and ambassador regression selection passed
  62 tests. One real-server savepoint test was skipped by its POSIX-only guard
  on Windows. Ruff passed. This is not a full Python SDK/platform test campaign.
- `git diff --check` passed for the affected source and documentation trees.

No performance comparison, Windows-native C++ run, or full independent
conformance campaign was performed in this implementation cycle.

## Earlier TDD And Evidence-Gate Cycle (2026-09-18)

This section preserves the preceding cycle's observations. The 2026-09-19
checkpoint above supersedes its open inherited-target/query/tag status and
descriptor-compatibility statement; its test counts are historical.

The next cycle implements the acquisition and MOM checkpoints in the
[requirements matrix](ieee1516-2010-requirements-matrix.md). It adds the
[local/manual gate](../../scripts/ieee2010_quality_gate.py) and its
[bounded evidence manifest](ieee1516-2010-evidence.json). All results below apply
to the current working tree, not an immutable released or certified build.

| Phase | Recorded result |
|---|---|
| Plan | Define observable acquisition preconditions, sender/target separation and independent handle vectors. Separate bounded engineering tests from complete normative claims. |
| Do / RED | Twelve invalid acquisition requests initially succeeded and changed state or callbacks. The publication-swap test also initially succeeded. MOM tests reproduced sender mutation, high-handle truncation and malformed target-frame handling. Gate unit tests first failed before implementation. |
| Do / GREEN | Add object preparation and coherent publication validation, commit ready ownership before observers, target-aware MOM switch mutation and framed uint64 handles. Implement strict manifest validation and actual Go-test event checking. |
| Review | Independent code review found the torn publication read; its deterministic regression now passes. Callback lock release and delete-during-observer tests cover the integrated acquisition path. Official-MIM integration exposed unresolved inherited-parameter lookup, which remains an open obligation rather than a passing service claim. |
| Reflect | Preserve that production-MOM blocker in the evidence manifest. Do not promote bounded passing tests to complete conformance. Prioritize class identity/inherited parameter resolution next, followed by the remaining ownership query/divest/tag and callback contracts. |

### Executed Results

- Windows Go 1.26.5: `go test ./rti/... ./tests/... ./verification/... ./examples/...`
  passed after the M36 handle-list fixture was corrected. Existing cached
  unaffected package results were reused; the bounded gate below disables cache.
- WSL Ubuntu 24.04, Go 1.22.2: `go test -race ./rti/...` passed. This includes
  the new acquisition, declaration, MOM and integration tests. The Windows MOM
  environment lacked GCC; the successful Linux run supplies the race evidence.
- Ownership/declaration focused tests passed 20 repeated runs; MOM's concurrent
  adjustment/resign test passed 100 repetitions.
- Python standard-library tests for the gate: 28 passed.
- Actual bounded gate: six `go test -json -count=1` commands passed, covering
  23 required top-level tests and 230 passing events including subtests, with
  no skips. These are test counts, not a percentage of IEEE obligations covered.
- `--claim full-2010 --check` returned FAIL before any command execution because
  normative coverage, applicability and independent evidence are incomplete.
- C++ against newly built Linux `rtid-quality-gate`: `ownership_integration`,
  `ownership_xfed_integration` and `mom_integration` passed; the ownership
  interface lockfile target also passed. MOM's existing integration suite does
  not establish the unresolved standard-MIM target path.
- Formatting and `git diff --check` passed for affected source/documentation.

The gate JSON transcript is retained outside Git at
`C:/Users/cbchoi/AppData/Local/Temp/gorti-ieee2010-gate-final.json`. It records
the selected commands and raw observations, not authenticated source provenance.
Python SDK regression, Windows-native C++, performance comparisons and external
RTI conformance assessment were not executed in this cycle.

### Compatibility And Remaining Scope

Protobuf descriptors/RPCs and SDK method signatures are unchanged in this cycle.
Invalid acquisition requests now fail with mapped exception names instead of
silently creating ownership. The production server wires registry validation;
registry-free ownership algorithm tests do not establish those preconditions.

MOM payloads do change: an `HLAhandle` now carries a four-byte array count of 8
followed by the SDK's eight-byte big-endian handle. Reports, MOM attributes and
handle-list elements no longer truncate to uint32. Custom clients using the old
raw-four-byte MOM convention must change their encoding/decoding. Malformed or
raw-four-byte target inputs are rejected, not silently treated as a sender.

At the end of that earlier cycle, standard-MIM inherited target lookup, counter-request targeting, reporting
exclusions, failure-report interactions and the full MOM catalog remain open.
Ordinary ownership query/divest/cancel semantics, tag/result propagation,
save/restore admission, automatic resign and membership-guard interleaving
coverage also remain open. The new coherent publication check is not a lease
that holds publication state through the entire ownership commit.
