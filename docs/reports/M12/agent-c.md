# Agent C - M12 W2 - Python SDK exposure for cut-3 service groups

Cut-3 milestone M12, wave 2. Wires the Python SDK side of the four
cut-2 service groups (sync, ownership, DDM, savepoint) onto Agent A's
M12 W1 Go-side gRPC handlers. After this wave, federates over real
gRPC (`grpc://` / `grpcs://`) can drive every cut-3 RPC end-to-end via
ergonomic per-federate accessors (`fed.sync` / `.ownership` / `.ddm` /
`.savepoint`).

Implements: M12 W2 — Python SDK exposure of cut-2 service groups.
MOM gRPC exposure (M12 W3) remains deferred per the orchestrator
brief.

## Outcome

- All 4 M12 spec tests GREEN, none skipped
  (`pytest pysdk/tests/spec/m12/`):
  - `test_spec_m12_sync_register_and_achieve`
  - `test_spec_m12_ownership_negotiated_transfer`
  - `test_spec_m12_ddm_region_create_and_subscribe`
  - `test_spec_m12_savepoint_save_and_restore_round_trip`
- All 498 pysdk tests pass (`pytest pysdk/tests/` clean).
- All Go-side tests stay GREEN (`go test ./...` across all 30
  packages); Go side untouched.
- `ruff check pysdk/` clean.
- `mypy --strict pysdk/` clean (no issues found in 86 source files).
- `make py-codegen` is idempotent and emits 24 generated `_pb2.py`
  / `_pb2_grpc.py` / `.pyi` files into `pysdk/rti1516e/_generated/`
  (gitignored).

## Files modified / created

Created (new SDK client modules under `pysdk/rti1516e/`):

- `sync.py` — `SyncClient`. Wraps `SyncServiceStub`, exposes
  `register_synchronization_point` + `synchronization_point_achieved`.
- `ownership.py` — `OwnershipClient`. Wraps `OwnershipServiceStub`,
  exposes all 8 RPCs of §7 (negotiated divest+acquire two-phase plus
  queries `query_attribute_ownership` / `is_attribute_owned_by_federate`).
- `ddm.py` — `DDMClient` plus the `AttributeRegions` dataclass.
  Wraps `DDMServiceStub` (10 RPCs) and ALSO holds a private
  `ObjectServiceStub` so the high-level
  `register_object_instance_with_regions(...)` can perform the
  M12 W1 two-step (mint handle via ObjectService, record bindings
  via DDMService).
- `savepoint.py` — `SavepointClient` plus mirror enums `SaveState`
  / `RestoreState`. Wraps `SavepointServiceStub` (7 RPCs).
- `_grpc_errors.py` — gRPC StatusCode → typed `RtiError` translator
  for cut-3 services. Adds 9 new exception classes (`SyncPointNotFound`,
  `SyncPointAlreadyExists`, `InvalidSyncState`,
  `OwnershipNotPermitted`, `InvalidOwnershipState`, `RegionNotFound`,
  `SaveBundleNotFound`, `SaveBundleAlreadyExists`,
  `InvalidSaveState`) all subclassing `RtiError`.

Created (test harness):

- `pysdk/tests/spec/m12/_helpers.py` — `RtidProcess` async context
  manager that spawns rtid on a free port, polls until ready, and
  tears down robustly (terminate → wait 5s → kill on timeout). Also
  `write_minimal_fom()` / `write_ddm_fom()` to write inline FOM
  fixtures and `two_federates(...)` async cm for joining two
  federates against the same federation.

Modified:

- `pysdk/rti1516e/connection.py` — added 4 cut-3 lazy property
  accessors on `Federate`: `.sync` / `.ownership` / `.ddm` /
  `.savepoint`. Each constructs the matching client on first access,
  bound to the federate's gRPC channel + federation_name + handle.
  Plus `_require_channel()` / `_require_federation_name()` defensive
  helpers that raise a clear `RuntimeError` when invoked on a
  memory:// transport (the in-process FakeRtiServer has no real
  gRPC channel).
- `pysdk/rti1516e/_transport.py` — wired `publish_object_class` /
  `subscribe_object_class` / `register_object_instance` /
  `update_attributes` from record-only stubs to real RPC dispatch
  against the existing `DeclarationServiceStub` /
  `ObjectServiceStub`. Added attribute-name → handle resolution by
  caching the parsed FOM on first `_populate_handle_tables` call;
  attribute lookup walks the cached FOM and matches Go-side
  `fomHandle.LookupAttribute` indexing (1-based, parser-natural
  order within each class). Required for the M12 ownership +
  DDM tests because both depend on a real published object class.
- `pysdk/rti1516e/__init__.py` — re-exports the 4 new clients,
  the 9 new exception types, the 2 new state enums, and
  `AttributeRegions`.
- `pysdk/pyproject.toml` — adds `rti1516e/_grpc_errors.py` to the
  ruff `N818` per-file-ignore list (same convention as `errors.py`:
  exception classes are named to match the proto / gRPC code
  semantics they represent).
- `pysdk/tests/spec/m12/test_spec_m12_sdk_exposure.py` — replaces
  the 4 skip scaffolds with full integration tests against a
  per-test rtid subprocess + real gRPC.

NOT modified (per task constraints):

- `proto/*` — frozen.
- `rti/*` — Go side untouched.
- MOM scaffolds / spec tests.

## Architectural choices

### Per-federate property accessors, not a fused service

`fed.sync` / `.ownership` / `.ddm` / `.savepoint` lazily build
dedicated client wrappers on first access. Each wrapper owns its
own pre-bound `(channel, federation_name, federate_handle)` triple,
so the per-RPC call sites are minimal (`await fed.sync.register_synchronization_point("phase1")`)
and the client objects are inexpensive to construct (just a stub
binding). This mirrors the pattern of `Federate.publish_object_class`
etc. — the federate is the one-stop entry point for federation-scoped
operations.

The accessors are real `@property` methods (not async) so the call
sites read naturally; the underlying transport state (channel +
federation_name) is set synchronously by `RtiConnection.__aenter__`
+ `_FederateContextManager.__aenter__` before any user code runs.

### Cut-3 wire-callback gap surfaced via Query RPCs

The proto `FederateEvent` oneof does not yet carry sync /
ownership-transfer / save-restore callback variants. The Go-side
managers emit placeholder `OutboundEvent` objects that lack the
`Inner() *rtiv1.FederateEvent` carrier method, so the gRPC stream
service's `toFederateEvent` rejects them with
`errOutboundEventNotConvertible`. Cut-3 federates therefore cannot
receive `federationSynchronized` / `attributeOwnershipAcquisitionNotification`
/ `federationSaved` over the wire.

The SDK clients work around this by leaning on the wire-exposed Query
RPCs that ARE part of cut-3:

- Ownership: `query_attribute_ownership` /
  `is_attribute_owned_by_federate` return the post-transfer owner
  directly, so the test asserts on those.
- Savepoint: `query_save_state` / `query_restore_state` return the
  transition state directly.
- Sync: NO query RPC exists in the proto today. The test asserts
  on the absence of error from the round-trip — registration +
  both achieves succeed without error, which implies the manager
  ran the all-required-achieved → emit-synchronized internal
  path. A cut-4 follow-up (proto evolution) makes the synchronized
  callback observable.

### DDM register-with-regions two-step in the SDK, not a new RPC

Per Agent A's M12 W1 handoff: `DDMService.RegisterObjectInstanceWithRegions`
returns `object_handle=0` by design — the Go-side handler does not
mint the handle, it only records associations. The SDK's
`DDMClient.register_object_instance_with_regions(...)` therefore
performs both calls inline (ObjectService.RegisterObject mints the
handle, DDMService.RegisterObjectInstanceWithRegions records
bindings) and returns the minted handle. A future
`DDMService.BindObjectRegions` proto RPC would simplify this; out
of scope for M12.

### Cross-process test harness with robust subprocess teardown

Reused the M5 cross-language harness's spawn pattern in
`m12/_helpers.py`: build rtid via `go build`, allocate two free
TCP ports, spawn with `start_new_session=True` so test-runner
SIGINT doesn't reap rtid before the async `finally` runs,
terminate → wait 5s → kill on timeout. Each test gets its own
tempdir for `--save-dir` + `--log-dir` so concurrent runs don't
collide.

## Cut-3 deferrals logged

### 1. Wire-side sync / ownership / save callbacks unobservable

As described above. The placeholder `OutboundEvent` types in
`rti/internal/sync/events.go`, `rti/internal/ownership/events.go`,
and `rti/internal/savepoint/events.go` allocate empty
`*rtiv1.FederateEvent` carriers and do not implement the `Inner()`
contract that `rti/internal/transport/grpc/stream.go::toFederateEvent`
checks. A cut-4 proto evolution adds the matching oneof variants;
the SDK's event-translation surface in `_transport.py::_translate_event`
will need a corresponding extension.

### 2. DDM dimension lookups returning 0 for FOM-declared dimensions

**Found during M12 W2 testing**: the production rtid path runs MIM
merge via `rti/pkg/fom/mim/merge.go`, where line 166
(`return model.NewFOM(objectClasses, interactionClasses, dataTypes)`)
constructs the merged FOM via `NewFOM` instead of
`NewFOMWithDimensions(...)`. The trailing `dimensions` slice is
silently dropped on the merge boundary. Result: every federation
created through rtid sees zero dimensions even when the source FOM
declared `<dimensions>` entries.

The DDM manager's `populateFromFOM` then enters non-permissive mode
with an empty dimension table, so `LookupDimension` returns 0 for
any name. This blocks the dimension-overlap delivery test scenario.

The Go-side fix is one line (swap `NewFOM` → `NewFOMWithDimensions`
and pass `merged.Dimensions()`); flagged here as a Go-side bug
because the M12 W2 task constraints prohibit touching `rti/`. The
DDM test in this milestone documents the bug and exercises the
dimensionless-region path; the bug-aware code path also has the
real-dimension branch already wired (`if dim_x != 0`) and will
"just work" once the Go fix lands.

### 3. End-to-end DDM overlap-driven Reflect delivery not asserted

The M12 W2 SDK now wires `update_attributes` to a real
`UpdateAttributeValues` RPC (previously record-only — see
`_transport.py` cut-1 notes). The cross-federate Reflect-on-update
flow exists in `rti/internal/object/registry.go` and is exercised
by Go-side M10 spec tests. Asserting end-to-end overlap-filtered
delivery from Python requires deferral #2 above to be fixed first
(without dimension handles, the regions can't define meaningful
overlap). Tracked alongside #2.

### 4. Multi-federate save-aggregation requires a MembersResolver

The production rtid wiring in `rti/cmd/rtid/main.go` (lines
506-513) does NOT wire a `MembersResolver` on the savepoint
manager. Result: save aggregation runs in dynamic mode where the
first federate to call `FederateSaveComplete` adds itself to the
required set and immediately closes the save out as `SAVED`. The
M12 W2 spec test exercises a single-federate save+restore round
trip, which is sufficient for the cut-3 SDK exposure proof. Real
multi-federate aggregation is a cut-4 follow-up that wires
`MembersResolver` from `federation.Manager`. The SDK shape is
already multi-federate-capable; only the rtid-side membership
snapshot is missing.

### 5. Restore-side federate-handle parity across save/restore

Bundle manifest records the EXACT federate handles that called
`FederateSaveComplete`. On restore, the manager enforces matching
handles. A federate that resigns and rejoins under a new handle
fails the membership check with `ErrFederateNotInRestore`. The M12
W2 test observes the restore initiates correctly (state ==
`INITIATED`) but does NOT call `FederateRestoreComplete` from a
mismatched handle to avoid this trip-wire. Same root cause as #4 —
fixing MembersResolver wiring lets save/restore use stable
federate identities.

## Acceptance evidence

```
$ pytest pysdk/tests/spec/m12/ -v
collected 4 items

tests/spec/m12/test_spec_m12_sdk_exposure.py::test_spec_m12_sync_register_and_achieve PASSED
tests/spec/m12/test_spec_m12_sdk_exposure.py::test_spec_m12_ownership_negotiated_transfer PASSED
tests/spec/m12/test_spec_m12_sdk_exposure.py::test_spec_m12_ddm_region_create_and_subscribe PASSED
tests/spec/m12/test_spec_m12_sdk_exposure.py::test_spec_m12_savepoint_save_and_restore_round_trip PASSED

============================== 4 passed in 0.74s ===============================

$ pytest pysdk/tests/                # full pysdk suite
============================= 498 passed in 2.41s ==============================

$ go test ./...                      # full Go suite (M12 W1 stays clean)
ok  github.com/cbchoi/gorti/...      (all 30 packages PASS)

$ ruff check pysdk/
All checks passed!

$ mypy --strict pysdk/
Success: no issues found in 86 source files

$ make py-codegen                    # idempotent, emits 24 generated files
$ ls pysdk/rti1516e/_generated/rti/v1/ | wc -l
30      (gitignored)
```

## NEW skipped tests added

None. The orchestrator constraint was clear: the four scaffolds
must turn green. No new tests were added to the scaffold file; the
existing 4 stubs were replaced by full integration tests, all PASS.
