# IEEE 1516-2010 Service Completion Increment

Date: 2026-09-29. Branch: `codex/ieee2010-feature-completion`.

This record describes a working-tree implementation increment. It is not a
full RTI conformance assessment or an external certification. Existing G0-G4
requirements and evidence obligations remain in force; none is waived to obtain
a passing result.

Later arithmetic, DDM validation, ownership continuation and Java restore-query
changes are recorded in the [boundary follow-up](ieee1516-2010-boundary-followup-20260929.md).
The counts below remain the evidence for this earlier increment.

## Implemented

### Standard Java Service Mapping

All 172 standard `RTIambassador` signatures now forward to typed provider methods.
The final 21 method names comprise fourteen support operations, six transportation
operations and additive region association. The official interface declarations
were not changed. `service-coverage.tsv` measures mapping presence, not semantic
coverage of all permitted inputs, states, callbacks and exceptions.

The support implementation includes update rates, automatic resign directives,
handle/service-group normalization and all four advisory switches. Legal support
queries retain membership/authentication checks during saves and active restores;
advisory mutations and region-state operations retain their checkpoint restrictions.
Ordinary subscriptions now participate in attribute-scope callback reconciliation.

### Atomic DDM Calls

Grouped wire requests preserve heterogeneous attribute/region pairs, including
explicit empty products. The server validates every supplied handle before
association, subscription or registration changes. Associations add to existing
bindings; grouped empty unassociation cannot clear unrelated bindings. Regional
update requests coalesce attributes per recipient and reserve callback delivery.
Registration errors preserve object handles, name reservations and existing state.

Legacy flattened request fields remain readable. New grouped fields cannot be
combined with their flattened counterparts. Descriptor projection tests retain
the preceding schema exactly after removing reviewed additions.

### Ownership

Cancellation errors now carry precise standard exception metadata. Invalid mixed
attribute sets fail before mutation. Divest-if-wanted does not issue a confirmation
request after ownership has already transferred. Java reports predefined RTI-owned
attributes using the corresponding standard callback.

Unconditional divestiture progresses queued acquirers, preserves remaining waiters
and solicits eligible knowledgeable publishers. New singleton-attribute callbacks
preserve separation between distinct requests. The production composition uses
the publisher resolver, including ancestor publication candidates, rather than
enumerating only subscribers. Broader ownership obligations remain below.

### Transportation Confirmation

Java transportation changes now prepare an authenticated request and activate it
at evoked confirmation entry. Messages sent before that entry retain the previous
transportation type. The activation ticket is bound to membership incarnation,
federation generation and callback epoch. Duplicate activation is idempotent within
the bounded replay window; foreign, expired and retired tickets fail explicitly.

Ownership loss removes affected attributes from pending changes permanently, even
if they are reacquired before confirmation. Attribute overrides reset to FDD
defaults after ownership transfer; unpublished interaction queries report the FDD
default without cancelling a pending interaction confirmation. The three added
wire operations preserve legacy immediate-change RPC behavior.

Prepared requests and completed replay entries are bounded. A lost prepare reply
can retain its reservation until lifecycle cleanup, and pending capabilities are
invalidated rather than persisted on restore. Those cases remain part of the
outstanding complete callback-state work.

### Persistent Java Runtime State

`federateSaveComplete` can carry a private SDK checkpoint. It is persisted in the
same save bundle as the federation snapshots and returned only to the matching
participant on restore initiation. New Java ambassadors can recover object/name
knowledge, retraction bookkeeping, staged DDM ranges, synchronization labels and
time-policy state without retaining the original JVM heap.

The data-only format is versioned, membership-bound and checksummed. Decoding
rejects truncation, unknown versions, invalid counts, duplicate identifiers,
invalid policy flags and trailing data. Runtime data is limited to 1 MiB per
participant and 8 MiB per federation. It excludes session tokens and application
model state. A failed decode cannot acknowledge successful restore; the caller
can still abort the restore.

MOM service/callback reports exclude private checkpoint bytes and opaque session,
receipt and activation capabilities, including nested messages. Writer-side
collection limits now match the reader's limits; encoding cannot acknowledge a
save image that is rejected solely for an excessive collection count on restore.

New save manifests record participant types as well as names. A replacement
participant with a different type is rejected before restore begins. Legacy
manifests retain their former behavior when type/runtime metadata is absent.
Private Java runtime state is currently generation-scoped; cross-generation
translation is rejected before commit, not silently accepted.

Restore initiation can complete restoration from inside its application callback.
The matching invocation receipt survives successful restore commit and follows
the restored federate identity; failed preparation leaves the receipt unchanged.
Unrelated stale receipts are discarded. Java retries reporting only the matching
restore-initiation outcome after identity rebinding, without replaying the user
callback or fabricating a successful invocation. Live tests cover unchanged
handles and fresh ambassadors rejoining in reverse order.

### Time Values And MOM Metadata

Both standard Java time factories are discoverable. Integer64 values use exact
integer arithmetic and encoding, including adjacent values above `2^53` and
checked final-time overflow. This adds local factory/value support only: the
server, wire time services and Java federation selection still need an exact
representation-aware Integer64 path.

RTI-originated MOM reflections and interactions now carry explicit receive-order
and reliable-transport metadata. They do not invent a producing federate or
message-retraction identity. This fixes actual Java callback delivery, not the
separate outstanding MOM pending-message accounting requirement.

Late MOM discovery is now recorded in the object registry after successful
delivery. This keeps scope, discovery and checkpoint references consistent.
Failed discovery delivery cannot produce the accompanying reflection or record
object knowledge. The previously failing production save-history scenario with
an active MOM subscription passed thirty consecutive runs after the correction.

## Verification

Focused tests first reproduced missing services, partial DDM semantics,
ownership notification errors, illegal support-service fencing, missing MOM
metadata and the loss of Java runtime state on replacement. The implementation
was then checked against those failures, real-server Java sessions and a separate
replacement JVM. Review added malformed-checkpoint and participant-type cases.

Final integration used Go 1.26.5, Java 11.0.31, Maven 3.9.16 and the existing
Python 3.14/gRPC 1.84 environment. Windows Java tests ran against the newly built
production `rtid`; race tests ran under WSL Ubuntu with GCC. No new live Java
case was disabled or excluded to obtain a passing result.

| Campaign | Result |
|---|---|
| Unfiltered Java Maven reactor, with real RTI endpoint and FOM | 397 passed, no failures/errors/skips; 32 real-server tests |
| Complete `go test ./rti/... -count=1 -timeout=300s` | 67 tested packages passed; 11 packages contain no tests |
| WSL Linux race run: object, ownership, DDM, save/restore, MOM, gRPC, Protobuf, savebundle/storage, application/server | All 10 packages passed |
| Python savepoint, ownership, live DDM and callback metadata selection, rebuilt RTI | 11 passed |
| Java generated ambassador/coverage consistency | Passed |
| Current and historical Protobuf descriptor/RPC projections | Passed |

The replacement-JVM test actually terminates the saving JVM and starts a second
JVM against the same production server. It verifies restored object knowledge,
logical time, regulation state and ownership-dependent updates. It does not test
destroying and recreating the federation execution.

The full-2010 prerequisite check was also executed. It returned `FAIL` with
`conformance_established=false`: the complete normative inventory, independently
reviewed applicability and requirement-linked executed evidence are missing.
That check started no test commands and is not included in the regression pass
counts. Its complete result is retained outside Git as
`D:/workspace/gorti/ieee-full-readiness-20260929.json`.

Final evidence is retained outside Git under `D:/workspace/gorti`:
`java-completion-final-20260929/` contains the Maven transcript, copied XML reports
and server logs; `go-completion-all-final-20260929.log`,
`go-completion-race-final-20260929.log` and
`python-completion-final-20260929.log` contain the other final transcripts.
Earlier red/green evidence remains separately available, including
`java-replacement-red-20260929/`, `java-checkpoint-process-20260929/` and
`go-completion-all-20260929.log`.

Initial runs exposed real MOM discovery/save and restore receipt defects as well
as obsolete Send-only test outboxes and callback-type assumptions. Those tests
were corrected without disabling the scope advisories; RO tests still require
reflections to remain queued. A Python discovery attempt selected the Windows
Store alias, and an earlier Python environment had an older gRPC runtime. The
final full Go campaign explicitly selected the working interpreter through
`GORTI_PYTHON`. Windows lacked the C compiler needed for Go race instrumentation.

SHA-256 identities of tested artifacts:

| Artifact | SHA-256 |
|---|---|
| `rtid-java-completion.exe` | `59023b40fbfc6c7ff57aa7bb4dd5481c36b85ba15b3bc5d8d24b3135219ea754` |
| Java client JAR | `9582c7e55546f4d9e57606ed16ca5b2efe7e025c462ac61856c1515d90b99c84` |
| Java wire JAR | `082f25c2b405772623d5144b0fdb025a94a8da732bd82ce160626c543a83b43b` |
| Java API JAR | `a05fb23b73724c0dcabf0f0d68f90311355a5090ab783bda756f6708f8e58718` |

## Remaining Obligations

The [normative readiness audit](ieee1516-2010-readiness-20260929.md) provides source
locators and distinguishes mandatory requirements from legitimate mapping choices.
The following are significant remaining work, not an exhaustive obligation count:

1. Exact Integer64 federation selection, wire values, queues, grants, timestamped
   services, queries, MOM encoding and persistent snapshots, alongside Float64.
2. Complete normative obligation inventory across the three standards, including
   tables, conditional behavior, state transitions, errors and object-model rules;
   then immutable release evidence and requirement-by-requirement review.
3. Label-based restore across recreated federation executions, translation of
   private SDK state and full persistence/replay of uninvoked callback state.
   Legal lookups during the final callback-epoch hydration window also need review.
4. DDM template/specification lifecycle, realization defaults, immutable conveyed
   regions, and associations made before ownership acquisition.
5. Full delayed-subscription evaluation, including messages sent before a receiver
   subscribes, and the associated queue, retraction and restore behavior.
6. MOM RO/TSO counts through outbox, transport and SDK queues until callback
   invocation, without gaps or double counting.
7. Remaining ownership state-chart paths, continuing solicitation, known ancestor
   publication eligibility, callback-admission failures and cross-request batching.
8. A complete Go/Python/Java semantic campaign and Linux Java execution. The
   focused tests here do not replace those release obligations.

API mapping completion and successful regression tests do not close these items.
The repository must not claim full IEEE conformance or external certification on
the basis of this increment.
