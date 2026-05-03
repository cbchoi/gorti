# Agent A - M11 W1 - MOM runtime (HLAfederate / HLAfederation)

Cut-2 milestone M11. Wires the runtime side of the IEEE 1516-2010
Management Object Model: the standard MIM declares
`HLAmanager.HLAfederate` and `HLAmanager.HLAfederation` as object
classes (parsed in M1); this milestone registers per-federate /
per-federation MOM instances and updates their attributes on
lifecycle events.

Implements: FR-MOM-1, FR-MOM-2 (read-only MOM). FR-MOM-3
(MOM-driven control via interactions) deferred to cut-3 per
docs/srs.md §5.11.

## Outcome

- All 5 M11 spec tests GREEN (`go test ./rti/spec/M11/...`).
- All M0-M8 tests still GREEN (`go test ./...` clean).
- `go test -race ./rti/internal/mom/...` clean.
- `golangci-lint run ./rti/internal/mom/...` clean.
- 16 agent-owned unit tests in `rti/internal/mom/mom_test.go`
  covering: nil-Outbox rejection, name validation, FOM-module name
  extraction (with placeholder fallback for path-less modules),
  invalid-handle rejection, lazy federation create on join,
  deterministic sort of federate handle list, resign removal, destroy
  cleanup, no-op behavior on unknown federation/federate, counter
  increments, concurrent-increment correctness under `-race`, and
  deep-copy semantics on Query accessors.

## Files modified / created

Modified:
- `rti/internal/mom/manager.go` — frozen-shape stub bodies replaced
  with real implementation. `New` now validates Outbox; all methods
  guard with the manager mutex; counter increments use atomic ops
  on `uint32` fields so the hot path does not contend on the
  manager mutex.
- `rti/cmd/rtid/main.go` — composes `mom.Manager`; wires hooks into
  the federation manager (Join/Resign), the gRPC layer (Create/
  Destroy), and the object registry (per-federate counters).
  Helper functions extracted to keep `newRTID` under the cyclomatic
  complexity threshold (15).
- `rti/internal/transport/grpc/server.go` — added optional
  `OnDestroyFederationSuccess` field to `Options` (analogous to the
  existing `OnCreateFederationSuccess`).
- `rti/internal/transport/grpc/federation.go` — fires
  `onDestroyFederationSuccess` after a successful gRPC
  `DestroyFederation`.
- `rti/internal/federation/manager.go` — added optional
  `OnFederateJoined` / `OnFederateResigned` fields to `Options`;
  fired post-success at the same callsite as the eventlog append.
  M2-frozen surface preserved (only ADD).
- `rti/internal/time/manager.go` — added optional
  `OnTimeStateChanged` field to `Options`; fired after every
  successful Enable/DisableRegulation, Enable/DisableConstrained.
  M3-frozen surface preserved (only ADD).
- `rti/internal/object/registry.go` — added optional `OnUpdateSent`,
  `OnInteractionSent`, `OnReflectDelivered`, `OnInteractionDelivered`
  fields to `Options`. M2-frozen surface preserved (only ADD).
- `rti/internal/object/update.go` — fires `OnUpdateSent` post-success
  and `OnReflectDelivered` per-recipient inside `fanoutReflect`.
- `rti/internal/object/interaction.go` — symmetric to update.go for
  `OnInteractionSent` + `OnInteractionDelivered`.

Created:
- `rti/internal/mom/state.go` — per-federation `momState` +
  `federateSnapshot` + `federationSnapshot` value types,
  sorted-handle helpers, snapshot deep-copy, and FOM-module-name
  extraction (with `module-N` placeholder when `Path` is empty).
- `rti/internal/mom/mom_test.go` — 16 agent-owned unit tests.
- `docs/reports/M11/agent-a.md` (this report).

NOT modified (frozen):
- `proto/*`
- `rti/internal/core/*`
- `rti/spec/M11/{doc,fixtures,mom_test}.go`

## Cut-1 simplifications (documented gaps)

These are intentional and tracked for follow-up work in M11 W2 / M12.

### 1. No real subscriber fan-out

The orchestrator brief authorized cut-1 to skip emitting actual
`ReflectAttributeValues` envelopes to MOM subscribers. The
`Manager.Query{Federate,Federation}Attributes` accessors are the
authoritative gate for the spec tests. Real federate-side
subscription via the standard pub/sub APIs requires the
`object.Registry` to be aware of MOM classes
(`HLAobjectRoot.HLAmanager.HLAfederation` as a subscribable class),
which is bigger work than M11 cut-1 scopes.

The MOM `Manager` does construct a valid `core.Outbox` reference
(it is required at `New` time) so the follow-up work is purely
additive: when the object registry learns to enumerate MOM-class
attribute updates, it will dispatch through the same outbox the
cut-1 code already wires.

### 2. `HLAfederateType` defaults to empty string

The proto `JoinFederationRequest` does not yet carry a
`federate_type` field (proto FROZEN at M0). The federation
manager's `OnFederateJoined` hook receives only the federate name;
MOM's `FederateJoined` is invoked with `federateType=""`. Cut-3
proto evolution can add the field; the MOM manager already accepts
and stores it.

### 3. `HLAlogicalTime` always reports 0 from the time hook

The `time.Manager` does not yet track per-federate `currentTime`
independently of the grant pipeline (the value is implicit in the
`pending` request). M11 wires `OnTimeStateChanged` to fire on
Enable/Disable Regulation/Constrained, but the hook reports
`logicalTime=0`. The MOM manager's
`TimeStateChanged(... logicalTime ...)` parameter is honored — it
just receives 0 today. A future M7 follow-up that surfaces the
per-federate logical time will populate the field.

### 4. Counters are best-effort metrics, not strictly atomic with the underlying event

`IncrementUpdatesSent` etc. fire AFTER a successful eventlog
append + fan-out, so a crash between the eventlog write and the
counter increment can leave the MOM counter behind by one. This is
acceptable per the brief: counters are introspection, not
correctness-critical state.

### 5. No MOM event-log records

The orchestrator brief noted that MOM lifecycle (register/update/
remove) could optionally be recorded to the EventLog. The MOM
Options struct accepts `EventLog` but does not currently call
`Append` — the proto Event variants for MOM transitions are not
yet defined (same gap as the M8 sync/ownership transitions). The
field is wired and ready for the cut-3 follow-up that adds the
proto variants.

### 6. `FederationDestroyed` does not enforce empty-roster invariant

The federation manager's `DestroyFederation` already rejects
non-empty federations with `ErrFederationHasFederatesJoined`; the
MOM manager simply removes the snapshot when the hook fires. If
the gRPC handler ever introduces a `force-destroy` semantic, MOM
will need to clean up dangling federate snapshots — the current
`FederationDestroyed` implementation does this correctly (it
deletes the entire `momState`, federates included).

## Key design decisions

### Hook fields, not interfaces

For each manager that fires MOM hooks (federation, time, object),
the new field is a typed function value on `Options`, not an
interface. This matches the existing pattern (e.g. `OnRegister` on
`object.Options`, `OnCreateFederationSuccess` on `grpcsvc.Options`)
and keeps each hook independently nil-able. Tests that construct a
manager without wiring MOM see no behavioral change.

### Atomic counters on `uint32`

`federateSnapshot.{interactionsSent,interactionsReceived,
updatesSent,reflectionsReceived}` are `uint32` (not `uint64`)
because the IEEE 1516-2010 MOM standard declares these attributes
as `HLAcount` (32-bit). The increment paths take only the manager
RLock to look up the snapshot pointer, then use `atomic.AddUint32`
on the field — the manager mutex is NOT held during the increment.
The `TestConcurrentIncrement` test verifies 100 concurrent
increments all land under `-race`.

### Federation-name as map key

The `Manager.fed` map keys on `core.FederationName`, mirroring the
sync/ownership/object registries. Per-federation isolation falls
out for free.

### Lazy federation create on `FederateJoined`

If the `FederationCreated` hook was not wired (or fired
out-of-order), `FederateJoined` lazily creates an empty
`HLAfederation` snapshot. The join hook is the higher-priority
ground truth for "this federation is active". The
`TestFederateJoined_LazyFederationCreate` test pins this
behavior.

### Defensive deep-copy on Query accessors

`QueryFederationAttributes` returns a `FederationAttributes` value
with deep-copied `FederateHandles` and `FOMModuleNames` slices.
Callers may retain and even mutate the result without aliasing the
live state. The `TestSnapshotsAreDeepCopied` test pins this.

### No `time.Manager` in production server (yet)

`newRTID` does not currently construct a `time.Manager` — only the
demo paths in `timed.go` do. The `OnTimeStateChanged` hook is
defined and wired-ready on `time.Options`, but the production
server mode does not exercise it today. Adding `time.Manager` to
the production composition is a separate task (M7 follow-up); the
MOM manager is ready to receive the calls when the wiring lands.

## Acceptance evidence

```
$ go test ./rti/spec/M11/... -v
=== RUN   TestSpec_M11_FederationCreated_RegistersHLAfederation
--- PASS: TestSpec_M11_FederationCreated_RegistersHLAfederation
=== RUN   TestSpec_M11_FederateJoined_RegistersHLAfederate
--- PASS: TestSpec_M11_FederateJoined_RegistersHLAfederate
=== RUN   TestSpec_M11_FederateResigned_RemovesHLAfederate
--- PASS: TestSpec_M11_FederateResigned_RemovesHLAfederate
=== RUN   TestSpec_M11_TimeStateChanged_UpdatesAttributes
--- PASS: TestSpec_M11_TimeStateChanged_UpdatesAttributes
=== RUN   TestSpec_M11_FederationDestroyed_RemovesHLAfederation
--- PASS: TestSpec_M11_FederationDestroyed_RemovesHLAfederation
PASS

$ go test ./...                        # full suite
ok      github.com/cbchoi/gorti/rti/internal/mom
ok      github.com/cbchoi/gorti/rti/spec/M11
... (all 25 packages PASS)

$ go test -race ./rti/internal/mom/... ./rti/spec/M11/...
ok      github.com/cbchoi/gorti/rti/internal/mom
ok      github.com/cbchoi/gorti/rti/spec/M11

$ golangci-lint run ./rti/internal/mom/...
(no output, clean)
```
