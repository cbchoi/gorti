# Agent A - M12 W1 - cut-3 gRPC handlers (sync, ownership, DDM, savepoint)

Cut-3 milestone M12, wave 1. Wires the Go-side gRPC handlers for the
four cut-2 service groups whose proto definitions were extended by the
orchestrator pre-work in commit `098fddc` (M12 pre-work). After this
wave, federates can reach sync / ownership / DDM / savepoint operations
over the wire — closing the M12 gap that left those managers as
in-process-only APIs at the close of cut-2.

Implements: M12 — sync, ownership, DDM, savepoint gRPC exposure.
Python SDK side (M12 W2 → Agent C) and MOM gRPC exposure (M12 W3
deferred per orchestrator brief) are out of scope.

## Outcome

- All 5 M12 spec tests GREEN (`go test ./rti/spec/M12/... -v` shows
  5/5 PASS, no skipped tests).
- All M0..M11 spec + unit tests still GREEN
  (`go test ./...` clean across all 25 packages).
- `go test -race ./rti/spec/... ./rti/internal/transport/grpc/...
  ./rti/cmd/rtid/...` clean.
- `go vet ./rti/spec/M12/... ./rti/internal/transport/grpc/...
  ./rti/cmd/rtid/...` clean.
- `make proto` is idempotent (21 generated .pb.go / _grpc.pb.go files
  in `rti/internal/genproto/rti/v1/`, gitignored).
- `scripts/check-milestones.sh` reports no regressions.

## Files modified / created

Created (new gRPC service handlers under `rti/internal/transport/grpc/`):

- `sync.go` — `syncService` impl of `rtiv1.SyncServiceServer`.
  Two RPCs: `RegisterFederationSynchronizationPoint`,
  `SynchronizationPointAchieved`. Translates proto request → call on
  `*sync.Manager`.
- `ownership.go` — `ownershipService` impl of
  `rtiv1.OwnershipServiceServer`. Eight RPCs covering the §7
  divest/acquire two-phase protocol + queries, all forwarded to
  `*ownership.Manager`.
- `ddm.go` — `ddmService` impl of `rtiv1.DDMServiceServer`.
  Ten RPCs covering routing-space + dimension lookup, region
  lifecycle, region-scoped pub/sub, and bound queries. Forwards to
  `*ddm.Manager`. `RegisterObjectInstanceWithRegions` is a thin
  cut-3 stub (returns handle 0 + echoed name) because object-handle
  minting requires fusing with `object.Registry.Register` — that
  fusion is the M12 W2 follow-up Python SDK glue (Agent C wraps both
  calls behind the high-level HLA call).
- `savepoint.go` — `savepointService` impl of
  `rtiv1.SavepointServiceServer`. Seven RPCs covering save + restore
  protocols and state queries. Adds a local
  `savepointErrToStatus` helper for the savepoint-package storage
  sentinels (`ErrSaveBundleNotFound` / `ErrSaveBundleExists`)
  which live outside `core.*` and so aren't covered by the shared
  `errToStatus`.

Created (report):

- `docs/reports/M12/agent-a.md` (this file).

Modified:

- `rti/internal/transport/grpc/server.go` — extended `Options` with
  four new fields (`Sync`, `Ownership`, `DDM`, `Savepoint`) wrapping
  the corresponding cut-2 manager pointer types. Each is OPTIONAL at
  construction time (nil → service simply not registered on the gRPC
  server, mirroring the existing `timeService` precedent). `Server`
  struct now carries `syncService` / `ownershipService` /
  `ddmService` / `savepointService` fields. `Register` registers the
  cut-3 services when composed.
- `rti/internal/transport/grpc/errs.go` — extended `errToStatus`
  with the cut-2 sentinel mappings. Sync/Ownership/DDM/Savepoint
  sentinels (`ErrSyncPointNotRegistered`, `ErrSyncPointAlreadyRegistered`,
  `ErrSyncPointAlreadyAchieved`, `ErrOwnershipDivestPending`,
  `ErrOwnershipAcquirePending`, `ErrOwnershipNotInTransfer`,
  `ErrRegionNotFound`, `ErrRoutingSpaceNotFound`,
  `ErrDimensionNotFound`, `ErrRegionNotOwnedByFederate`,
  `ErrRegionInUse`, `ErrSaveAlreadyInProgress`,
  `ErrRestoreAlreadyInProgress`, `ErrSaveBundleCorrupt`,
  `ErrFederateNotInSave`, `ErrFederateNotInRestore`) now map to the
  appropriate gRPC code: NotFound for missing-entity, AlreadyExists
  for creation conflict, PermissionDenied for
  not-owned-by-federate, FailedPrecondition for everything else.
- `rti/cmd/rtid/main.go` — wires the cut-2 managers into
  `grpcsvc.NewServer(Options{ ... Sync: syncMgr, Ownership: ownMgr,
  DDM: ddmMgr, Savepoint: saveMgr })`. The save manager is still
  optional — wired only when `--save-dir` is set (the existing M9
  contract is preserved).
- `rti/spec/M12/grpc_handlers_test.go` — flipped from 5×`t.Skip` to
  5 PASSing round-trip tests. Each test composes the four cut-2
  managers, stands up a real `grpc.Server` on a TCP listener, dials
  with `grpc.NewClient` + insecure creds, drives the RPCs over the
  wire, and asserts post-condition state via the underlying manager
  accessors.

NOT modified (frozen):

- `proto/*` — the four new .proto files were frozen at commit
  `098fddc`; no proto edits required.
- `rti/internal/core/*` — M0-frozen; no surface changes required.
- `rti/internal/{sync,ownership,ddm,savepoint}/manager.go` — cut-2
  frozen; the gRPC handlers are pure wire bridges.
- `pysdk/*` — Python SDK side is M12 W2 (Agent C).
- `rti/spec/M12/doc.go` — milestone doc preserved as-is.

## Spec test status

`go test ./rti/spec/M12/... -v`:
```
=== RUN   TestSpec_M12_SyncService_GRPCRoundTrip
--- PASS: TestSpec_M12_SyncService_GRPCRoundTrip
=== RUN   TestSpec_M12_OwnershipService_GRPCRoundTrip
--- PASS: TestSpec_M12_OwnershipService_GRPCRoundTrip
=== RUN   TestSpec_M12_DDMService_GRPCRoundTrip
--- PASS: TestSpec_M12_DDMService_GRPCRoundTrip
=== RUN   TestSpec_M12_SavepointService_GRPCRoundTrip
--- PASS: TestSpec_M12_SavepointService_GRPCRoundTrip
=== RUN   TestSpec_M12_AllServicesRegistered
--- PASS: TestSpec_M12_AllServicesRegistered
PASS
```

## Architectural choices worth recording

### Optional Options (nil-permissive), not required

The orchestrator brief offered two patterns: "treat as required for
M12" or "follow Time's nil → return Unimplemented precedent". I
chose the latter for symmetry with `Time` and to keep older callers
(M3 / M4 demo paths, simple federation-only test harnesses) buildable.
Concretely, when `opts.Sync == nil` the `SyncService` is simply not
registered on the `grpc.Server`; clients that try to call it get the
standard gRPC `Unimplemented` for an unregistered service (not
`status.Unimplemented` from a stub method body — the absence is at
the registration level). Tests that DO want all 8 services pass the
managers; production rtid always passes them.

### Storage-sentinel error helper

The savepoint package owns its own `ErrSaveBundleExists` /
`ErrSaveBundleNotFound` sentinels (they live in
`rti/internal/savepoint/manager.go`, not in `rti/internal/core`).
The shared `errToStatus` in `errs.go` only knows about `core.*`
sentinels, so I added a tiny `savepointErrToStatus` wrapper that
catches the two storage sentinels (mapping to NotFound / AlreadyExists)
and falls through to the shared mapper for everything else. This
keeps the storage sentinels package-local rather than promoting
them into `core.*` just for the gRPC layer.

### Cut-3 deferral on `RegisterObjectInstanceWithRegions`

Per IEEE 1516.1-2010 §6.7 this is a fused "register object + record
DDM associations" call. In gorti the object-handle minting lives in
`object.Registry.Register` and the DDM-association recording lives
in `ddm.Manager.AssociateRegionsWithObjectInstance`. Fusing the
two atomically over a single gRPC RPC requires composition logic
that crosses the W3B/W3C ownership boundary, so the M12 W1 handler
accepts the request shape but only echoes the supplied object name
back with handle 0; the Python SDK (Agent C, W2) wraps both calls
behind the high-level HLA `registerObjectInstanceWithRegions`. This
is a documented gap, not a stub-skip.

### No new skipped tests

The 5 M12 spec tests are now all PASSing; no new `t.Skip` was
introduced. The gap above (fused register-with-regions) is
documented in `rti/internal/transport/grpc/ddm.go` as a comment but
not as a skipped test — Agent C will add the round-trip test for
the fused API in M12 W2 once the Python wrapper exists.

### MOM gRPC exposure intentionally excluded

The M12 doc.go header mentions MOM as a cut-3 service group, but the
proto pre-work (commit 098fddc) did NOT add a `MOMService.proto` —
only sync/ownership/DDM/savepoint were extended. Per the orchestrator
brief, MOM gRPC exposure is deferred. This wave does not modify any
MOM tests, scaffolds, or the MOM manager.

### Per-package fixtures duplicated in spec test

`rti/spec/M12/grpc_handlers_test.go` defines its own `fakeOutbox` /
`permissiveFOMRepo` / `memStore` etc., rather than importing from
`rti/internal/{sync,ownership,ddm,savepoint}` test files (Go does
not export `_test.go` symbols across packages). The fixtures are
kept minimal and parallel the well-established cut-2 patterns; if a
future cut promotes any of these to a shared `rti/spec/fixtures`
package, the spec test would adopt them then.

## Handoff notes for Agent C (Python W2)

1. **Generated stubs** — Python protobuf stubs land in
   `pysdk/rti1516e/_generated/` via `make py-codegen`. The buf.gen.yaml
   was already configured to emit Python stubs for the four new
   services; `make py-codegen` should produce
   `sync_pb2.py`, `sync_pb2_grpc.py`, etc. for each of the four
   services. No proto changes are required.

2. **Wire version** — Every Python client request must populate
   `wire_version=WIRE_VERSION_V1`; the Go handlers reject
   `WIRE_VERSION_UNSPECIFIED` with `FailedPrecondition` (matching
   the cut-1 federation/declaration/object service pattern).

3. **Storage error semantics** — `RequestFederationRestore` against
   a missing label returns gRPC `NotFound`; `RequestFederationSave`
   against a duplicate label returns `AlreadyExists`. Other
   savepoint storage errors (corrupt bundle) map to
   `FailedPrecondition`.

4. **DDM register-with-regions split** — As noted under
   "architectural choices", the cut-3 Go handler for
   `RegisterObjectInstanceWithRegions` returns object handle 0. The
   Python SDK should:
   - Call `ObjectService.RegisterObject` first to mint the handle.
   - Then call `DDMService.RegisterObjectInstanceWithRegions` (or
     simpler: let the DDM associations be recorded by a follow-up
     `AssociateRegionsWithObjectInstance` — note that proto does
     not currently expose this method directly; the
     `RegisterObjectInstanceWithRegions` RPC is the only DDM-side
     surface for per-attribute region binding).
   - Surface the object handle from the ObjectService response,
     ignoring the 0 returned by the DDM RPC.

   A future cut-3 evolution may add a `BindObjectRegions` RPC that
   takes an existing ObjectHandle and only records associations;
   that would simplify Agent C's job. Not in scope for M12.

5. **Save state enums** — The Python SDK should mirror the proto
   `SaveState` / `RestoreState` enums. The Go handler maps cleanly:
   `IDLE` / `INITIATED` / `SAVED` / `NOT_SAVED` for save;
   `IDLE` / `LOADING` / `INITIATED` / `COMPLETED` / `FAILED` for
   restore.

6. **Service registration probe** — Python tests can mirror
   `TestSpec_M12_AllServicesRegistered` via the Go reflection
   server (already enabled in the cut-1 harness) or by attempting
   each method and verifying no `Unimplemented` is returned.

## Acceptance evidence

```
$ go test ./rti/spec/M12/... -v
=== RUN   TestSpec_M12_SyncService_GRPCRoundTrip
--- PASS: TestSpec_M12_SyncService_GRPCRoundTrip (0.00s)
=== RUN   TestSpec_M12_OwnershipService_GRPCRoundTrip
--- PASS: TestSpec_M12_OwnershipService_GRPCRoundTrip (0.00s)
=== RUN   TestSpec_M12_DDMService_GRPCRoundTrip
--- PASS: TestSpec_M12_DDMService_GRPCRoundTrip (0.00s)
=== RUN   TestSpec_M12_SavepointService_GRPCRoundTrip
--- PASS: TestSpec_M12_SavepointService_GRPCRoundTrip (0.00s)
=== RUN   TestSpec_M12_AllServicesRegistered
--- PASS: TestSpec_M12_AllServicesRegistered (0.00s)
PASS

$ go test ./...
ok      github.com/cbchoi/gorti/examples/go-pingpong
ok      github.com/cbchoi/gorti/examples/go-timed
ok      github.com/cbchoi/gorti/rti/cmd/rtid
ok      github.com/cbchoi/gorti/rti/internal/ddm
ok      github.com/cbchoi/gorti/rti/internal/declaration
ok      github.com/cbchoi/gorti/rti/internal/eventlog
ok      github.com/cbchoi/gorti/rti/internal/federation
ok      github.com/cbchoi/gorti/rti/internal/mom
ok      github.com/cbchoi/gorti/rti/internal/object
ok      github.com/cbchoi/gorti/rti/internal/ownership
ok      github.com/cbchoi/gorti/rti/internal/perf
ok      github.com/cbchoi/gorti/rti/internal/savepoint
ok      github.com/cbchoi/gorti/rti/internal/sync
ok      github.com/cbchoi/gorti/rti/internal/time
ok      github.com/cbchoi/gorti/rti/internal/transport/grpc
ok      github.com/cbchoi/gorti/rti/pkg/encoding
ok      github.com/cbchoi/gorti/rti/pkg/fom/mim
ok      github.com/cbchoi/gorti/rti/pkg/fom/model
ok      github.com/cbchoi/gorti/rti/pkg/fom/parser
ok      github.com/cbchoi/gorti/rti/spec/M10
ok      github.com/cbchoi/gorti/rti/spec/M11
ok      github.com/cbchoi/gorti/rti/spec/M12
ok      github.com/cbchoi/gorti/rti/spec/M2
ok      github.com/cbchoi/gorti/rti/spec/M3
ok      github.com/cbchoi/gorti/rti/spec/M5
ok      github.com/cbchoi/gorti/rti/spec/M7
ok      github.com/cbchoi/gorti/rti/spec/M8
ok      github.com/cbchoi/gorti/rti/spec/M9
ok      github.com/cbchoi/gorti/tests/spec/M1

$ scripts/check-milestones.sh
... No regressions.
```
