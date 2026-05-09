# M23 Dispatch Plan — ObjectManagement (§6) + DDM (§9) completion

How the orchestrator dispatches the M23 tasks (TASK-246..275) to maximize parallel sub-agent throughput while keeping every wave orthogonal at the file level.

This document is FROZEN — only the orchestrator may edit. Companions: `docs/DISPATCH.md`, `docs/M22_DISPATCH_PLAN.md` (predecessor), `docs/agent-a-rti-core.md`, `docs/agent-c-pysdk.md`, `docs/MILESTONE_CHECK.md`, `docs/srs.md` §10 (M23 row appended at end of plan).

---

## 1. Goal & non-goals

### Goal

Close the documented gaps in IEEE 1516.1-2010 §6 (Object Management Services) and §9 (Data Distribution Management Services) so the cut-3 service surface matches the spec.

The work has **two parts**:

1. **§6 Object Management completion.** Currently only `register_object_instance` / `update_attribute_values` / `send_interaction` are wired. M23 adds:
   - `delete_object_instance` + `RemoveObjectInstance` callback (proto slot at `stream.proto:33` exists but no consumer)
   - `local_delete_object_instance` (federate-local cleanup, no peer notification)
   - `request_attribute_value_update` (instance + class variants) + `ProvideAttributeValueUpdate` callback
   - `change_attribute_transportation_type` + `change_interaction_transportation_type`

2. **§9 DDM completion.** Currently 10 RPCs in the proto, 16 manager methods, but Go SDK has zero DDM coverage and 5 §9 services are missing across the stack:
   - `associateRegionsForUpdates` (manager exists; needs proto + SDKs)
   - `unassociateRegionsForUpdates` (everything missing)
   - `unsubscribeObjectClassAttributesWithRegions`
   - `unsubscribeInteractionClassWithRegions`
   - `sendInteractionWithRegions`
   - `requestAttributeValueUpdateWithRegions` (paired with §6 `request_attribute_value_update`)
   - **Go SDK DDM coverage** (mirror pysdk's 10 methods + the new ones)

### Non-goals

- **§6 name reservation** (`reserveObjectInstanceName` family — §6.2-§6.8). Lower priority; defer to M24.
- **§6 order-type changes** (`change_*_order_type` — §6.27, §6.28). Distinct from transport-type; defer to M24.
- **§6 attributes_in_scope / attributes_out_of_scope callbacks** (§6.31). DDM-driven; M10 territory; defer.
- **§7 ownership management resign-related work** (`releaseAllOwnedBy` for resign correctness). Will land in M24 alongside resign-action expansion.
- **No proto field renumbering.** Append-only — same constraint as M21/M22.
- **No new federation-management semantics.** M23 only adds object-/DDM-side services.

### Why now

- Object management is the most fundamental service group after federation/declaration. Without `delete_object_instance`, every example that registers objects leaks them until the federate resigns. The resign audit (post-M22) showed the leak compounds: even `RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES` doesn't actually divest. M23 closes the foundation.
- DDM has the surface bones (M10) but Go federates can't use it at all (no SDK). Cross-language feature parity is broken.
- `request_attribute_value_update` is the standard HLA "late joiner pulls initial state" pattern; missing it means new federates can't bootstrap from an existing federation cleanly.

---

## 2. Surface design

This section pins every wire-visible decision before any sub-agent starts. Implementations must conform; deviations require a plan revision.

### 2.1 §6 surface additions

#### 2.1.1 ObjectService proto deltas

```proto
service ObjectService {
  // existing
  rpc RegisterObjectInstance(RegisterObjectRequest) returns (RegisterObjectResponse);
  rpc UpdateAttributeValues(UpdateAttributeValuesRequest) returns (Empty);
  rpc SendInteraction(SendInteractionRequest) returns (Empty);

  // M23 additions
  rpc DeleteObjectInstance(DeleteObjectInstanceRequest) returns (Empty);
  rpc LocalDeleteObjectInstance(LocalDeleteObjectInstanceRequest) returns (Empty);
  rpc RequestAttributeValueUpdate(RequestAttributeValueUpdateRequest) returns (Empty);
  rpc RequestClassAttributeValueUpdate(RequestClassAttributeValueUpdateRequest) returns (Empty);
  rpc ChangeAttributeTransportationType(ChangeAttributeTransportRequest) returns (Empty);
  rpc ChangeInteractionTransportationType(ChangeInteractionTransportRequest) returns (Empty);
}

message DeleteObjectInstanceRequest {
  WireVersion wire_version = 1;
  string federation_name = 2;
  uint64 federate_handle = 3;
  uint64 object_handle = 4;
  optional double logical_time = 5;  // TSO delete (cut-1: logical_time may be omitted → RO)
  bytes user_supplied_tag = 6;        // §6.16; passed through to RemoveObjectInstance.tag (NEW field on the existing event)
}

message LocalDeleteObjectInstanceRequest {
  WireVersion wire_version = 1;
  string federation_name = 2;
  uint64 federate_handle = 3;
  uint64 object_handle = 4;
}

message RequestAttributeValueUpdateRequest {
  WireVersion wire_version = 1;
  string federation_name = 2;
  uint64 federate_handle = 3;
  uint64 object_handle = 4;
  repeated uint64 attribute_handles = 5;
  bytes user_supplied_tag = 6;
}

message RequestClassAttributeValueUpdateRequest {
  WireVersion wire_version = 1;
  string federation_name = 2;
  uint64 federate_handle = 3;
  uint64 object_class_handle = 4;
  repeated uint64 attribute_handles = 5;
  bytes user_supplied_tag = 6;
}

message ChangeAttributeTransportRequest {
  WireVersion wire_version = 1;
  string federation_name = 2;
  uint64 federate_handle = 3;
  uint64 object_handle = 4;
  repeated uint64 attribute_handles = 5;
  TransportationType transport_type = 6;  // NEW enum (Reliable / BestEffort) in common.proto
}

message ChangeInteractionTransportRequest {
  WireVersion wire_version = 1;
  string federation_name = 2;
  uint64 federate_handle = 3;
  uint64 interaction_class_handle = 4;
  TransportationType transport_type = 5;
}
```

`TransportationType` enum lands in `common.proto`:

```proto
enum TransportationType {
  TRANSPORTATION_TYPE_UNSPECIFIED = 0;
  TRANSPORTATION_TYPE_RELIABLE = 1;
  TRANSPORTATION_TYPE_BEST_EFFORT = 2;
}
```

#### 2.1.2 Stream event extensions

The existing `RemoveObjectInstance` event (`stream.proto:33`, oneof tag 12) gets the `user_supplied_tag` field appended:

```proto
message RemoveObjectInstance {
  uint64 object_handle = 1;
  optional double logical_time = 2;
  bytes user_supplied_tag = 3;  // NEW
}
```

A new event variant for `ProvideAttributeValueUpdate` (callback to producer when a peer requests an update). Append at oneof tag 15:

```proto
message FederateEvent {
  oneof event {
    DiscoverObjectInstance discover = 10;
    ReflectAttributeValues reflect = 11;
    RemoveObjectInstance remove = 12;
    ReceiveInteraction receive = 13;
    TimeAdvanceGrant grant = 14;
    ProvideAttributeValueUpdate provide_update = 15;  // M23
    // ... existing tags 20+ unchanged
  }
}

message ProvideAttributeValueUpdate {
  uint64 object_handle = 1;
  repeated uint64 attribute_handles = 2;
  bytes user_supplied_tag = 3;
}
```

### 2.2 §6 manager additions

`object.Registry` gains methods:

| Method | Behavior |
|---|---|
| `Delete(ctx, fed, h, obj, ts, tag) error` | Owner-only; emits `RemoveObjectInstance` to every subscriber via Outbox; removes the instance from `federationState.instances`; emits eventlog `objectDeleted` record. Errors: `ErrObjectHandleInvalid`, `ErrObjectNotOwned` (NEW sentinel), `ErrFederationNotFound`. |
| `LocalDelete(ctx, fed, h, obj) error` | Removes the instance from the federate's local view; no peer notification. The instance stays in `instances` if other federates still subscribe. (Cut-1 simplification: subscribers still see the instance; the producer's local view is stripped.) Manager-level: a per-federate "discovered set" tracks per-federate visibility. |
| `RequestAttributeValueUpdate(ctx, fed, h, obj, attrs, tag) error` | Resolves the producer (owner) of the requested attributes; emits one `ProvideAttributeValueUpdate` event to the owner with the requesting attrs + tag. Errors: `ErrObjectHandleInvalid`, `ErrAttributeNotPublishedByFederation` (NEW sentinel; fires when no federate publishes any of the requested attrs). |
| `RequestClassAttributeValueUpdate(ctx, fed, h, cls, attrs, tag) error` | Resolves all owners of any instance of the class with the requested attrs; emits one `ProvideAttributeValueUpdate` to each unique owner. |
| `ChangeAttributeTransportType(ctx, fed, h, obj, attrs, tt) error` | Per-instance, per-attribute transport-type override; stored in `instances[obj].transportTypes` (NEW per-attribute map). Owner-only. Affects future `UpdateAttributeValues` deliveries. |
| `ChangeInteractionTransportType(ctx, fed, h, cls, tt) error` | Per-publisher per-class override; stored in `declMgr` or `Registry.publishedTransport` (decision below). Affects future `SendInteraction` deliveries from this federate. |

**Design choice — transport-type storage**: per-publisher state lives on `*Registry` (a new `publishedAttrTransport map[(federate, object, attr)]TransportType` and `publishedInteractionTransport map[(federate, class)]TransportType`). NOT on `declaration.Manager`, which today only tracks publication membership, not transport overrides. Default value (zero) means "use FOM-declared default" (currently always Reliable; the existing `Outbox.Send` path doesn't switch transports yet, so the override is recorded but observable only via Snapshot until a future cut wires per-event transport selection).

> **Simplification for M23**: store the transport override; emit it on the eventlog; do NOT yet route differently in the multi-Outbox. Wire-level transport switching is M24+. The M23 acceptance test asserts the override is *recorded*, not that the wire actually switches.

### 2.3 §9 DDM surface additions

#### 2.3.1 Proto deltas

Append to `ddm.proto`:

```proto
service DDMService {
  // existing 10 RPCs unchanged

  rpc AssociateRegionsForUpdates(AssociateRegionsForUpdatesRequest) returns (Empty);
  rpc UnassociateRegionsForUpdates(UnassociateRegionsForUpdatesRequest) returns (Empty);
  rpc UnsubscribeObjectClassAttributesWithRegions(UnsubscribeOCAWithRegionsRequest) returns (Empty);
  rpc UnsubscribeInteractionClassWithRegions(UnsubscribeICWithRegionsRequest) returns (Empty);
  rpc SendInteractionWithRegions(SendInteractionWithRegionsRequest) returns (Empty);
  rpc RequestAttributeValueUpdateWithRegions(RequestAttributeValueUpdateWithRegionsRequest) returns (Empty);
}

message AssociateRegionsForUpdatesRequest {
  WireVersion wire_version = 1;
  string federation_name = 2;
  uint64 federate_handle = 3;
  uint64 object_handle = 4;
  repeated AttributeRegionPair attr_region_pairs = 5;
}
message AttributeRegionPair {
  uint64 attribute_handle = 1;
  repeated uint64 region_handles = 2;
}

message UnassociateRegionsForUpdatesRequest { /* same shape minus region_handles within each pair (only attribute handles + region handles to drop) */ }
message UnsubscribeOCAWithRegionsRequest { /* matches Subscribe shape */ }
message UnsubscribeICWithRegionsRequest { /* matches Subscribe shape */ }
message SendInteractionWithRegionsRequest { /* SendInteractionRequest + region_handles */ }
message RequestAttributeValueUpdateWithRegionsRequest { /* RequestClassAttributeValueUpdateRequest + region_handles */ }
```

#### 2.3.2 Manager additions

| Method | Behavior |
|---|---|
| `AssociateRegionsForUpdates` | Already exists as `AssociateRegionsWithObjectInstance`. M23 just wires the proto RPC. |
| `UnassociateRegionsForUpdates(ctx, fed, h, obj, attr_region_pairs)` | Drops associations matching the pairs; if attr_region_pairs is empty, drops ALL associations for the object. |
| `UnsubscribeObjectClassAttributesWithRegions(ctx, fed, h, cls, attr_region_pairs)` | Inverse of subscribe; drops the per-class subscription rows. |
| `UnsubscribeInteractionClassWithRegions(ctx, fed, h, cls, region_handles)` | Inverse of subscribe. |
| `SendInteractionWithRegions(ctx, fed, h, cls, params, region_handles, ts, tag)` | Filters subscribers via existing `InteractionSubscribersForSend` minus those whose subscription doesn't overlap the supplied regions. Uses existing `object.Registry.SendInteraction` with the filtered set. |
| `RequestAttributeValueUpdateWithRegions(ctx, fed, h, cls, attrs, region_handles, tag)` | Filters owners via DDM, then forwards to `object.Registry.RequestClassAttributeValueUpdate` with the filtered owner set. |

#### 2.3.3 Go SDK DDM (W4)

New file `rti/pkg/federate/ddm.go`. Mirrors pysdk's 10 existing methods plus the 6 new ones. Methods follow the existing `time.go` pattern: `*Federate.X(ctx, args) error`. Region handles surface as `uint64` (no opaque struct) — matches pysdk's choice and keeps the SDK contract minimal.

### 2.4 Error model

New sentinels in `rti/internal/core/errors.go`:

| Sentinel | gRPC code | Detail string |
|---|---|---|
| `ErrObjectNotOwned` | `PermissionDenied` | `object not owned by federate (cannot delete or change transport)` |
| `ErrAttributeNotPublishedByFederation` | `FailedPrecondition` | `no federate publishes any of the requested attributes` |
| `ErrObjectAlreadyDeleted` | `NotFound` | `object instance already deleted` |
| `ErrTransportTypeUnspecified` | `InvalidArgument` | `transport type must be Reliable or BestEffort` |

Pysdk typed exceptions extend the M22 700-709 range:
- `ObjectNotOwned` (710)
- `AttributeNotPublishedByFederation` (711)
- `ObjectAlreadyDeleted` (712)
- `TransportTypeUnspecified` (713)

### 2.5 Concurrency & ordering

- **Delete**: acquire `federationState.mu` for the duration of subscriber lookup + RemoveObjectInstance fanout + instance removal. Subsequent `UpdateAttributeValues` on the deleted obj returns `ErrObjectAlreadyDeleted`.
- **LocalDelete**: per-federate visibility map; no global state mutation.
- **RequestUpdate**: read-only on subscribers/declarations; emits one event per owner.
- **TransportType change**: write-only on per-instance/per-class map; no fanout.

---

## 3. Acceptance criteria (exit gate)

Every bullet is a probe `make verify` or `scripts/check-milestones.sh M23` must pass.

1. **`DeleteObjectInstance` reachable cross-process.** Confirmed by `rti/spec/M23/delete_test.go::TestSpec_M23_DeleteReachable`.
2. **`RemoveObjectInstance` callback delivered.** When federate A deletes an object that federate B subscribes to, B receives a `RemoveObjectInstance` event on its Events stream.
3. **`LocalDelete` only affects the caller.** Federate A local-deletes; A's local view loses the instance; B still sees it.
4. **`RequestAttributeValueUpdate` triggers `ProvideAttributeValueUpdate` callback** at the producer.
5. **`RequestClassAttributeValueUpdate` triggers callbacks at all class instance owners.**
6. **Transport-type changes are recorded** (visible in `ObjectSnapshot.TransportTypes` map). Wire transport switching is deferred.
7. **DDM Go SDK exposes all 16 methods** (10 existing + 6 new). Confirmed by `rti/spec/M23/ddm_go_sdk_test.go::TestACDDMGoSDKSurface`.
8. **DDM `Unsubscribe*WithRegions` flips the subscriber set.** After unsubscribe, the federate no longer receives reflects/interactions matching the region.
9. **DDM `SendInteractionWithRegions` filters by region overlap.**
10. **DDM `RequestAttributeValueUpdateWithRegions` filters owners by region.**
11. **Pysdk Federate exposes the 6 new §6 methods.** Confirmed by `pysdk/tests/spec/m23/test_pysdk_obj_surface.py`.
12. **Pysdk DDM exposes the 6 new §9 methods.** Confirmed by `pysdk/tests/spec/m23/test_pysdk_ddm_surface.py`.
13. **Spec test `rti/spec/M23/m23_completion_test.go` is green.** Binds AC §3.1-3.12 invariants.
14. **`bash scripts/check-milestones.sh` reports `M23: DONE (N/N)`** with all probes green.

---

## 4. Wave structure

```
                                M23 START
                                    │
    ┌───────────────────────────────┼───────────────────────────────┐
    │                               │                               │
    │   W1   — §6 delete + RemoveObjectInstance callback (Agent A)  │
    │           proto/object.proto + stream.proto                   │
    │           rti/internal/object/{registry.go,delete.go(NEW)}    │
    │           rti/internal/transport/grpc/{object.go,stream.go}   │
    │           rti/pkg/federate/{object.go(NEW),events.go}         │
    │           pysdk surface + tests                               │
    │                               │                               │
    │   W2   — §6 local_delete + request_update + provide callback  │
    │           rti/internal/object/{registry.go,request_update.go} │
    │           proto Stream extension for ProvideAttributeUpdate   │
    │           SDKs + tests                                        │
    │                               │                               │
    │   W3   — §6 change_*_transportation_type                      │
    │           rti/internal/object/transport.go (NEW)              │
    │           proto + SDKs + tests                                │
    │                               │                               │
    │   W4   — §9 DDM Go SDK (rti/pkg/federate/ddm.go NEW)          │
    │           Cross-language parity for the 10 existing RPCs      │
    │                               │                               │
    │   W5   — §9 DDM missing services                              │
    │           proto/ddm.proto extension                           │
    │           rti/internal/ddm/manager.go (5 new methods)         │
    │           rti/internal/transport/grpc/ddm.go (5 handlers)     │
    │           rti/pkg/federate/ddm.go (5 methods)                 │
    │           pysdk/rti1516e/ddm.py (5 methods)                   │
    │                               │                               │
    │   W6   — acceptance gate + docs (orchestrator)                │
    │           rti/spec/M23/* + pysdk/tests/spec/m23/*             │
    │           srs.md M23 row + CHANGELOG +                        │
    │           scripts/check-milestones.sh M23 probe               │
    │                               │                               │
                                    ▼
                       M23 DONE per srs.md §10
```

W1 → W2 → W3 sequential (all in object package); W4 independent of W2/W3; W5 depends on W2 (shares the request_update wire shape); W6 last.

---

## 5. File ownership (orthogonality matrix)

| File | W1 | W2 | W3 | W4 | W5 | W6 |
|---|---|---|---|---|---|---|
| `proto/rti/v1/object.proto` | EXTEND | EXTEND | EXTEND | — | — | — |
| `proto/rti/v1/stream.proto` | EXTEND (Remove tag) | EXTEND (provide_update) | — | — | — | — |
| `proto/rti/v1/common.proto` | — | — | EXTEND (TransportType) | — | — | — |
| `proto/rti/v1/ddm.proto` | — | — | — | — | EXTEND | — |
| `rti/internal/core/errors.go` | ADD 1 | ADD 2 | ADD 1 | — | — | — |
| `rti/internal/object/registry.go` | EXTEND | EXTEND | EXTEND | — | — | — |
| `rti/internal/object/delete.go` | NEW FILE | — | — | — | — | — |
| `rti/internal/object/request_update.go` | — | NEW FILE | — | — | — | — |
| `rti/internal/object/transport.go` | — | — | NEW FILE | — | — | — |
| `rti/internal/ddm/manager.go` | — | — | — | — | EXTEND | — |
| `rti/internal/transport/grpc/object.go` | EXTEND (3 RPCs) | EXTEND (3 RPCs) | EXTEND (2 RPCs) | — | — | — |
| `rti/internal/transport/grpc/ddm.go` | — | — | — | — | EXTEND (6 RPCs) | — |
| `rti/internal/transport/grpc/stream.go` | EXTEND (Remove conv) | EXTEND (Provide conv) | — | — | — | — |
| `rti/internal/transport/grpc/errs.go` | ADD | ADD | ADD | — | — | — |
| `rti/pkg/federate/object.go` | NEW FILE | EXTEND | EXTEND | — | — | — |
| `rti/pkg/federate/events.go` | EXTEND (Remove) | EXTEND (Provide) | — | — | — | — |
| `rti/pkg/federate/ddm.go` | — | — | — | NEW FILE | EXTEND (6 methods) | — |
| `rti/pkg/federate/errors.go` | ADD | ADD | ADD | — | — | — |
| `pysdk/rti1516e/connection.py` | EXTEND (1) | EXTEND (3) | EXTEND (2) | — | — | — |
| `pysdk/rti1516e/_transport.py` | EXTEND | EXTEND | EXTEND | — | EXTEND | — |
| `pysdk/rti1516e/standard.py` | EXTEND | EXTEND | EXTEND | — | — | — |
| `pysdk/rti1516e/ddm.py` | — | — | — | — | EXTEND (6) | — |
| `pysdk/rti1516e/events.py` | EXTEND (Remove) | EXTEND (Provide) | — | — | — | — |
| `pysdk/rti1516e/_grpc_errors.py` | ADD typed | ADD typed | ADD typed | — | — | — |
| `rti/spec/M23/*.go` | NEW (delete) | NEW (req_update) | NEW (transport) | NEW (ddm Go) | NEW (ddm missing) | NEW (acceptance gate) |
| `pysdk/tests/spec/m23/*.py` | NEW | NEW | NEW | — | NEW | NEW (acceptance gate) |
| `docs/srs.md` | — | — | — | — | — | EDIT |
| `CHANGELOG-MASTERPLAN.md` | — | — | — | — | — | EDIT |
| `scripts/check-milestones.sh` | — | — | — | — | — | EDIT |

---

## 6. Tasks

### W1 — §6 delete_object_instance + RemoveObjectInstance callback

- **TASK-246** Proto: add `DeleteObjectInstance` RPC to ObjectService; extend `RemoveObjectInstance` event with `user_supplied_tag` field. `buf generate` + `make py-codegen`.
- **TASK-247** Manager: `Registry.Delete(ctx, fed, h, obj, ts, tag)`. New file `rti/internal/object/delete.go`. Subscriber resolution uses existing `subscribersForReflect` then emits `RemoveObjectInstance` envelopes via Outbox. Producer-only (returns `ErrObjectNotOwned` for non-owners). Removes instance from federationState.instances.
- **TASK-248** Wire: `transport/grpc/object.go::DeleteObjectInstance` handler. Stream conversion: `transport/grpc/stream.go::toFederateEvent` already handles RemoveObjectInstance via the FederateEvent oneof — verify the carrier type satisfies the federateEventCarrier path.
- **TASK-249** Go SDK: `rti/pkg/federate/object.go` (NEW) — `Federate.DeleteObjectInstance(ctx, obj, tag, ts)`. Update `rti/pkg/federate/events.go` to deliver RemoveObjectInstance as a typed event (NEW `RemoveObjectInstance` struct).
- **TASK-250** Pysdk: `Federate.delete_object_instance(obj, tag, timestamp)` in `connection.py`; `_transport.py::_delete_object_instance` dispatch; `standard.py::deleteObjectInstance` ambassador. Event delivery via `events.py` `RemoveObjectInstance` dataclass.
- **TASK-251** Spec test: `rti/spec/M23/delete_test.go` — manager-level + cross-process via bufconn; verifies subscriber receives event, owner-only enforcement, double-delete returns ErrObjectAlreadyDeleted.

### W2 — §6 local_delete + request_attribute_value_update + ProvideAttributeValueUpdate callback

- **TASK-252** Proto: add `LocalDeleteObjectInstance` + `RequestAttributeValueUpdate` + `RequestClassAttributeValueUpdate` RPCs. Add `ProvideAttributeValueUpdate` to FederateEvent oneof at tag 15 + matching message.
- **TASK-253** Manager: `Registry.LocalDelete(ctx, fed, h, obj)`; `Registry.RequestAttributeValueUpdate(ctx, fed, h, obj, attrs, tag)`; `Registry.RequestClassAttributeValueUpdate(ctx, fed, h, cls, attrs, tag)`. New file `rti/internal/object/request_update.go`.
- **TASK-254** Wire: 3 new handlers in `transport/grpc/object.go`; stream conversion for `ProvideAttributeValueUpdate` event.
- **TASK-255** Go SDK: 3 new methods on `*Federate`; `ProvideAttributeValueUpdate` event type in `events.go`.
- **TASK-256** Pysdk: 3 new methods + dispatch + ambassador + `ProvideAttributeValueUpdate` event delivery.
- **TASK-257** Spec tests: `rti/spec/M23/local_delete_test.go` + `request_update_test.go`.

### W3 — §6 change_*_transportation_type

- **TASK-258** Proto: TransportationType enum in common.proto; ChangeAttributeTransport + ChangeInteractionTransport RPCs.
- **TASK-259** Manager: per-instance + per-class transport storage on `Registry`. Methods: `ChangeAttributeTransportType`, `ChangeInteractionTransportType`. New file `rti/internal/object/transport.go`. Snapshot exposes `TransportTypes` map per-instance.
- **TASK-260** Wire + SDKs + spec test.

### W4 — §9 DDM Go SDK (parallel to W1-W3 if needed)

- **TASK-261** New file `rti/pkg/federate/ddm.go`. 10 methods mirroring pysdk:
  `LookupRoutingSpace`, `LookupDimension`, `CreateRegion`, `SetRangeBounds`, `CommitRegionModifications`, `DeleteRegion`, `QueryBounds`, `SubscribeObjectClassAttributesWithRegions`, `SubscribeInteractionClassWithRegions`, `RegisterObjectInstanceWithRegions`.
- **TASK-262** Go SDK type adapters (RegionHandle uint64, RangeBound struct).
- **TASK-263** Spec test: `rti/spec/M23/ddm_go_sdk_test.go` — surface introspection + RPC mapping smoke checks.

### W5 — §9 DDM missing services

- **TASK-264** Proto: 6 new RPCs in ddm.proto (Associate, Unassociate, Unsubscribe×2, SendWithRegions, RequestUpdateWithRegions).
- **TASK-265** Manager: `UnassociateRegionsForUpdates`, `UnsubscribeObjectClassAttributesWithRegions`, `UnsubscribeInteractionClassWithRegions`, `SendInteractionWithRegions`, `RequestAttributeValueUpdateWithRegions`. Plus expose existing `AssociateRegionsWithObjectInstance` as the wired method.
- **TASK-266** Wire: 6 handlers in `transport/grpc/ddm.go`.
- **TASK-267** Go SDK + Pysdk: 6 methods each.
- **TASK-268** Spec test: `rti/spec/M23/ddm_missing_test.go`.

### W6 — Acceptance gate + docs

- **TASK-269** `rti/spec/M23/m23_completion_test.go` — binds all AC §3.1-§3.14.
- **TASK-270** `pysdk/tests/spec/m23/test_m23_completion.py`.
- **TASK-271** `docs/srs.md` §10.4: M23 row.
- **TASK-272** `CHANGELOG-MASTERPLAN.md`: M23 close entry.
- **TASK-273** `scripts/check-milestones.sh`: `check_m23()` with N probes.

---

## 7. Test plan summary

| Test | File | Asserts |
|---|---|---|
| Delete reachable + remove callback | `rti/spec/M23/delete_test.go` | DeleteObjectInstance RPC + RemoveObjectInstance event delivery |
| LocalDelete | `rti/spec/M23/local_delete_test.go` | Per-federate visibility |
| Request update | `rti/spec/M23/request_update_test.go` | ProvideAttributeValueUpdate callback to owner |
| Transport-type change | `rti/spec/M23/transport_test.go` | Override recorded in Snapshot |
| DDM Go SDK | `rti/spec/M23/ddm_go_sdk_test.go` | All 16 methods callable |
| DDM missing services | `rti/spec/M23/ddm_missing_test.go` | Unsubscribe flips the subscriber set; SendWithRegions filters; etc. |
| Pysdk surface | `pysdk/tests/spec/m23/test_pysdk_*.py` | Method introspection |
| Acceptance gate | `rti/spec/M23/m23_completion_test.go` + `test_m23_completion.py` | All AC §3.x |

---

## 8. Migration & follow-ups

- **No default behavior changes.** All M23 services are additive. Pre-M23 code paths (register/update/send) work unchanged.
- **Transport-type override is record-only in M23.** Wire-level transport switching tracked as M24+.
- **§6 name reservation, order-type changes, scope callbacks** deferred to M24.
- **§7 ownership resign-correctness** (`releaseAllOwnedBy`) deferred to M24.

---

## 9. M23 row append target (for W6 — for reference, do not edit srs.md before W6)

```markdown
| **M23** | Agent A + C | ObjectManagement (§6) + DDM (§9) completion | §6: delete_object_instance + RemoveObjectInstance callback wired end-to-end (proto slot was orphan); local_delete; request_attribute_value_update + ProvideAttributeValueUpdate callback (instance + class variants); change_attribute_transportation_type + change_interaction_transportation_type (record-only, wire switching deferred). §9: DDM Go SDK gains all 16 methods (was zero); 5 missing wire RPCs added (Associate/Unassociate, Unsubscribe×2, SendInteractionWithRegions, RequestAttributeValueUpdateWithRegions). **DONE 2026-MM-DD** — see `docs/M23_DISPATCH_PLAN.md` and `CHANGELOG-MASTERPLAN.md` |
```
