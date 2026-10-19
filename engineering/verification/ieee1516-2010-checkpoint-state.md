# Same-Roster Checkpoint State

Date: 2026-09-19. Scope: immediate checkpoints in the current single-server
runtime, with unchanged federation generation, FOM and participant identities.
This is an engineering milestone for audit findings F01-F03, not a declaration
of complete IEEE 1516-2010 Save/Restore conformance.

## State Coverage

The previous production composition registered only synchronization, ownership,
MOM and DDM snapshots. It could report restoration while objects, declarations
and logical time still described the post-save execution. The event-log field
was empty and was not replayed to reconstruct the missing state.

Production now requires all seven state keys:

| State key | Saved RTI state |
|---|---|
| `object` | Instances, names, registrants, allocation/sequence counters, discoveries, scopes, name reservations and transport overrides. Derived name indexes and management-class caches are rebuilt. |
| `declaration` | All four tracked object/interaction publication/subscription tables. Derived subscriber caches are rebuilt; installed hooks remain application dependencies. |
| `time` | Regulation, constrained state, lookahead, logical time, pending requests and modes, asynchronous delivery, halt/stall state, and queued TSO callbacks with binary payload/tag and retraction metadata. |
| `mom` | Federation/federate attributes and counters, switches and reporting flags, MOM object identities and resolved class/attribute handles. |
| `ownership` | Existing ownership and pending negotiation snapshots, including the preceding cycle's two-phase state and tags. |
| `ddm` | Existing regions, pending/committed ranges and association/subscription snapshots. |
| `sync` | Existing synchronization-point snapshots. |

The object registry does not cache last attribute values. Federates must save
their model memory and owned values themselves. Declaration active/passive and
update-rate semantics are not currently represented by the implementation and
are not supplied merely by adding a snapshot. Process configuration, algorithm
implementations and injected dependencies are not serialized.

New object/declaration/time codecs are versioned JSON inside the existing bundle
framing. MOM adds a distinguishable versioned extension around the prior
Protobuf payload, retaining legacy reading and the floating-point `+Inf`
sentinel. No public Protobuf descriptor, RPC or SDK signature changes are made
by this milestone. New MOM snapshots are not downgrade-compatible with older
server readers.

## Restore Transaction

1. Reserve the restore protocol, validate provenance and the unchanged roster,
   and require every configured snapshot key. Unknown snapshot keys are rejected.
2. Decode, validate and detach each snapshot without modifying live state.
3. Notify participants. An initiation-notification failure discards the prepared
   state and ends the operation as failed.
4. Wait for all participant reports. Failure or abort discards prepared state.
5. Reserve queue space for every success callback, revalidate provenance and
   roster, commit prepared state, then commit the callback reservation.
   Reservation failure leaves live state unchanged and reports restore failure.
   An unexpected adapter error after state commit is explicitly reported as
   committed state, never as a rollback.

`RequiredSnapshots` selects this production policy. Bare manager compositions
without it retain legacy partial-snapshot behavior for compatibility; those
configurations must not be presented as complete runtime restoration. Required
state configurations also require an explicit membership resolver and an
atomically reservable outbox.

The adapter acquires leases in this order:

```text
checkpoint admission -> membership/lifecycle -> protocol -> service state
```

SavepointService control calls take the exclusive checkpoint lease for one
handler invocation. Ordinary federation-scoped unary calls, mutation-stream
frames and admin mutations take shared admission before membership. They are
rejected while the protocol is active. Callback consumption stays available.
Mutation leases are released before potentially blocking ACK transmission.
Admission entries count holders and waiters and are reclaimed when idle, so
rejected requests with unique federation names do not grow the lock table.
The same admission boundary must be used by direct in-process integrations.
There is currently no production stall-polling loop; adding one requires the
same guard around its mutations.

This guard does not save unread network frames, delivered callback queues or
client-side buffers. Applications must pause producers, drain acknowledged
sends/callbacks and close mutation streams before a checkpoint, while continuing
to consume checkpoint callbacks. A future restore epoch is needed to reject
stale frames independently of this cooperative protocol.

## Plan, Do, Review, Reflect

| Phase | Evidence |
|---|---|
| Plan | Split object, declaration, time, MOM, transport admission and production integration into separate work areas. Keep protocol coordination and evidence integration separate. |
| Do | Add domain DTOs, prepared codecs and behavioral tests. Initial missing APIs failed compilation. Executed negative cases reproduced lost restore-initiation notification, duplicate-field acceptance, and dangling transport state after object deletion before corrections. |
| Review | Independent review found terminal-callback admission after state commit, typed-nil disabled-service wiring, roster-only strict configuration, retained admission entries, admin generation selection, registration rollback residue and duplicate/Unicode JSON-key aliases. Corrections have dedicated regression tests. Real-server tests supplement snapshot equality. |
| Reflect | Preserve the bounded same-roster claim. Required-state rejection repairs false success; it does not implement participant rebinding, scheduled saves or arbitrary transport replay. Keep the full-conformance gate failing until its evidence prerequisites exist. |

## Executed Verification

- Windows Go: the final uncached `./rti/... ./tests/... ./verification/... ./examples/...`
  regression campaign passed, including all four production checkpoint tests.
- Linux Go race: uncached `./rti/...` passed, including the production checkpoint
  package; targeted worker race campaigns also passed.
- Real production server subprocess and two joined federates: object/name and
  declaration restoration, logical-time rollback, queued TSO-before-grant
  delivery, failure/abort preservation and subsequent retry passed. These are
  two federate clients, not a distributed-host benchmark.
- The production scenarios also passed five repeated Windows executions.
  Their initial pre-implementation RED run was not captured; do not present
  that integration test as a separately observed RED/GREEN cycle.
- Evidence-gate implementation tests: 28 passed. The bounded profile now
  includes explicit checkpoint test names; execution transcripts are stored
  outside Git. Final gate results are recorded in the implementation progress.

No SDK implementation was changed. No fresh C++/Python campaign, performance
comparison, Windows-native C++ run or independent conformance assessment is
claimed for this milestone.

Bundle writing now enforces the same version and 16 MiB manifest/1 GiB optional
event-log limits as reading, and rejects short writes. This prevents publishing
a successful save that its own reader cannot load. Oversized TSO-backed
snapshots therefore fail saving; streaming a larger checkpoint is future work.

## Remaining Work

Scheduled logical-time saves, restored identity rebinding, restart recovery,
transport/callback epochs, all legal service-state combinations and independent
interoperability oracles remain open. Required-key checks do not constitute a
complete cross-service consistency proof for an arbitrarily edited bundle.
The DDM operation gaps and other audit findings are separate work items. The
three full normative texts and corrections are still needed to classify and
verify all mandatory and conditional obligations.
