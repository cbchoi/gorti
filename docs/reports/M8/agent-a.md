# Agent A - M8 W1 - Synchronization Management + Ownership Management

Cut-2 milestone M8. Brings two service groups not in cut-1 to GREEN
behind the orchestrator-frozen-shape stubs:

- Synchronization Management (FR-SYN-1..4) - IEEE 1516.1-2010 §4.6-4.7.
- Ownership Management (FR-OWN-1..6) - IEEE 1516.1-2010 §7.

## Outcome

- All 9 M8 spec tests GREEN (3 of which had inner skip blocks that the
  orchestrator brief authorised Agent A to remove once wiring landed -
  all 3 unskipped and given concrete assertion bodies).
- All M0-M7 tests still GREEN: `go test ./...` clean.
- `go test -race ./rti/internal/sync/... ./rti/internal/ownership/...`
  clean.
- `golangci-lint run ./rti/internal/sync/... ./rti/internal/ownership/...`
  clean.
- New agent-owned unit tests:
  `rti/internal/sync/sync_test.go` (12 tests) and
  `rti/internal/ownership/ownership_test.go` (18 tests).

## Files modified / created

Modified:
- `rti/internal/sync/manager.go` (bodies replaced; signatures preserved)
- `rti/internal/ownership/manager.go` (bodies replaced; signatures preserved)
- `rti/internal/object/registry.go` (added optional `OnRegister` hook to
  `Options`; invoked after Discover fan-out)
- `rti/cmd/rtid/main.go` (composes `ownership.Manager` + `sync.Manager`;
  wires the `OnRegister` hook into `ownership.RegisterInitialOwnership`)
- `rti/spec/M8/ownership_test.go` (3 inner-skip blocks removed and given
  concrete assertion bodies; orchestrator-authorised)

Created:
- `rti/internal/sync/events.go` (placeholder eventRecord +
  outbound-event types)
- `rti/internal/ownership/events.go` (same pattern)
- `rti/internal/sync/sync_test.go`
- `rti/internal/ownership/ownership_test.go`
- `docs/reports/M8/agent-a.md` (this report)

NOT modified (frozen):
- `rti/internal/core/errors.go` (the 6 new sentinels are the contract)
- `rti/spec/M8/{doc,fixtures}.go` (orchestrator-frozen)
- `proto/*` (frozen; see "Cut-1 gaps" below)

## Key design decisions

### Sync: nil `requiredFederates` resolution

The orchestrator brief offered three options. Picked the second
(`MembersResolver` callback in `Options`) plus a documented "dynamic"
fallback when neither is supplied:

```go
type MembersResolver func(core.FederationName) []core.FederateHandle

type Options struct {
    Outbox   core.Outbox
    EventLog core.EventLog
    Members  MembersResolver  // optional
}
```

Resolution order in `Register(...., requiredFederates)`:

1. `requiredFederates != nil` -> use it verbatim (with defensive copy).
2. `requiredFederates == nil` AND `Options.Members != nil` -> snapshot
   `Members(fed)` at register time and freeze the set.
3. `requiredFederates == nil` AND `Options.Members == nil` -> "dynamic"
   mode: each `Achieve` call adds the federate to the required set,
   and the point completes as soon as that single federate calls
   `Achieve`. This is the documented cut-1 simplification used by
   `TestSpec_M8_Sync_Register_Happy` (which calls `Register(.., nil)`
   without configuring `Members` and expects success without a
   subsequent Achieve).

The `cmd/rtid` composition leaves `Members` nil at this cut - wiring
it requires a `federation.Manager.MembersOf(fed)` accessor that does
not yet exist; tracked as M8 W2 follow-up alongside gRPC exposure.

### Ownership: state lives in `ownership.Manager`

Picked the orchestrator brief's recommended Option B: `ownership.Manager`
holds its own `(fed, obj, attr) -> owner` map and exposes
`RegisterInitialOwnership(fed, owner, obj, attrs)` for the wiring layer
to call after a successful object registration.

Wired through a new OPTIONAL `OnRegister` hook on `object.Options`:

```go
// rti/internal/object/registry.go
type Options struct {
    // ...existing fields...
    OnRegister func(fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle)
}
```

`Register` invokes the hook after Discover fan-out. The cut-1 attribute
list forwarded to the hook is the same fixed `fanoutAttrProbe` range
the registry already uses for Discover - so initial-ownership coverage
matches Discover coverage. FOM-driven enumeration of "all attributes
of class" is the existing follow-up tracked at `fanoutAttrProbe`.

The hook is OPTIONAL so M2 frozen behaviour is preserved when nil:
existing `object.Registry` consumers (spec/M5, spec/M2, etc.) pass
`Options{}` without `OnRegister` and observe identical behaviour.

`cmd/rtid/main.go` wires:

```go
OnRegister: func(fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
    ownMgr.RegisterInitialOwnership(fed, owner, obj, attrs)
},
```

### Ownership: NegotiatedDivest <-> Acquire two-phase

State machine per `(obj, attr)`:

- `owners[k]` = current owner (cleared on UnconditionalDivest, replaced
  on transfer)
- `pendingDivests[k]` = owner is divesting; first acquirer triggers
  transfer
- `pendingAcquires[(k, acquirer)]` = acquirer queued; transfer fires
  when an owner divests

Both directions converge at `completeTransfer`:
- updates `owners`
- clears `pendingDivests[k]` and `pendingAcquires[(k, acquirer)]`
- emits divestiture-notification to old owner + acquisition-notification
  to new owner via Outbox
- appends a single `evtTransferred` record to EventLog

`NegotiatedDivest` opportunistically completes the transfer if a
queued Acquire already exists (matches the "if-wanted" spirit of §7.7);
`Acquire` on already-pending-divest fires immediately.

`DivestIfWanted` (§7.7) is `NegotiatedDivest` minus the pending state
record - it transfers to a queued acquirer if any, otherwise no-op.

`Cancel{Divest,Acquire}` clear the pending state and return
`ErrOwnershipNotInTransfer` when no matching pending entry exists.
Cut-1 simplification per orchestrator brief: §7.5 cancel-confirm
callbacks to subscribers are NOT emitted (subscribers learn the
divest is cancelled by the absence of a follow-on transfer).

## Cut-1 gaps (M8 W2 follow-ups)

The orchestrator brief explicitly authorised pragmatic deferrals when
gRPC handler exposure or EventLog WAL became hairy. Both bit:

### gRPC exposure

`proto/rti/v1/` does not define a SyncService or OwnershipService.
Adding RPCs requires a contract-change-request, so M8 W1 ships the
managers + cmd/rtid composition only - they are constructed and stored
on the `rtid` struct but no gRPC handler reaches them. Spec tests
exercise the managers directly via `rti/spec/M8/sync_test.go` +
`ownership_test.go`, which is the orchestrator's stated cut-1 target.

### EventLog write-ahead

`proto/rti/v1/eventlog.proto`'s `Event.body` oneof carries no sync /
ownership variants. The cut-1 implementation:

- Defines internal `eventRecord` types in each package that satisfy
  both `core.EventRecord` and `proto.Message` by lazily wrapping an
  empty `rtiv1.Event{Seq:N}`.
- Calls `EventLog.Append(...)` for every transition (register /
  achieve / synchronized / unconditional-divest / negotiated-divest /
  acquire / transferred / cancel-divest / cancel-acquire /
  divest-if-wanted) so the `permissiveEventLog` in spec fixtures
  receives one record per transition (and the agent-owned
  `TestEventLog_Appends*` tests cover that).
- Production-grade replay determinism (FR-SYN-4 / FR-OWN-6) requires
  proto extension - the marshalled bytes today are an empty Event with
  only Seq populated, which is sufficient to anchor TSO ordering but
  insufficient to reconstruct the transition. Tracked as M8 W2.

### Subscribers fan-out for NegotiatedDivest

`requestAttributeOwnershipAssumption` fan-out requires
`object.Registry -> declaration.Manager.SubscribersFor(fed, cls,
attrs)`. `ownership.Manager` does not know an object's class, so the
resolver is exposed as `Options.Subscribers SubscribersResolver`
keyed on `(fed, obj, attrs)`; the test fixture in
`TestSpec_M8_Ownership_NegotiatedDivest_AnnouncesAssumption` wires a
fake resolver that returns `{2, 3}`. Production wiring (a closure that
looks up the class via `object.Registry`, then calls
`declaration.SubscribersFor`) is the M8 W2 follow-up; cmd/rtid leaves
`Subscribers` nil for now and NegotiatedDivest is a no-fan-out
state-only operation in production.

### Halt-aware short-circuit

The brief mentioned `ErrFederationHalted` pre-flight checks. cut-1
sync/ownership do NOT consult halted state - a halt-source dependency
would require coupling to `time.Manager` (which currently owns the
halted flag). Spec tests do not exercise the halted case for sync /
ownership, so this is unblocking; tracked as M8 W2 alongside the
gRPC + proto-extension work.

## Test counts

```
$ go test ./rti/spec/M8/... -v
PASS: TestSpec_M8_Ownership_QueryAfterRegister
PASS: TestSpec_M8_Ownership_NegotiatedDivest_AnnouncesAssumption
PASS: TestSpec_M8_Ownership_AcquireAfterDivest_TransfersOwnership
PASS: TestSpec_M8_Ownership_DivestNotOwner_Rejected
PASS: TestSpec_M8_Ownership_QueryUnowned_ReturnsZeroFalse
PASS: TestSpec_M8_Sync_Register_Happy
PASS: TestSpec_M8_Sync_Register_Twice
PASS: TestSpec_M8_Sync_AchieveAll_FiresFederationSynchronized
PASS: TestSpec_M8_Sync_Achieve_TwiceRejected
PASS: TestSpec_M8_Sync_Achieve_UnregisteredRejected
ok    github.com/cbchoi/gorti/rti/spec/M8

$ go test ./rti/internal/sync/...  -> 12 tests PASS
$ go test ./rti/internal/ownership/...  -> 18 tests PASS
$ go test ./...                          -> all packages PASS
$ go test -race ./...                    -> all packages PASS
$ golangci-lint run ./rti/internal/sync/... ./rti/internal/ownership/...
  -> clean
```
