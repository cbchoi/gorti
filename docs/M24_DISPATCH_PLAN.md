# M24 Dispatch Plan — FederationManagement (§4) completion

How the orchestrator dispatches the M24 tasks (TASK-274..290) to maximize parallel sub-agent throughput.

This document is FROZEN — only the orchestrator may edit. Companions: `docs/DISPATCH.md`, `docs/M23_DISPATCH_PLAN.md` (predecessor), `docs/agent-a-rti-core.md`, `docs/MILESTONE_CHECK.md`, `docs/srs.md` §10.

---

## 1. Goal & non-goals

### Goal

Close documented gaps in IEEE 1516.1-2010 §4 (Federation Management Services). The most painful gap is `resignFederationExecution`: 1 of 6 enum values accepted, **and even the accepted value (`UNCONDITIONALLY_DIVEST_ATTRIBUTES`) does not divest** — the manager just removes the federate from the roster; ownership records stay stale.

Two parts:

1. **ResignAction correctness + completeness** (W1 + W2). Land the missing ownership-release machinery, chain it into resign, then expand the accepted set to all 6 IEEE 1516.1 values. M23's `delete_object_instance` unblocks the `DELETE_OBJECTS` family.

2. **Three small additions** (W3):
   - `listFederationExecutionMembers` (§4.8) — manager already has `MembersOf`; just needs RPC + SDKs.
   - `abortFederationSave` (§4.28) — missing entirely.
   - `abortFederationRestore` (§4.30) — missing entirely.

### Non-goals

- **§4.2 connect / §4.4 disconnect** — gRPC channel handles this implicitly. The pysdk/Go SDK Connection types own the transport lifecycle; no explicit RPC needed.
- **§7 ownership service-group expansion**. M24 only adds `ReleaseAllOwnedBy` for resign. Wider §7 work (e.g., `attributeOwnershipDivestitureIfWanted`, `cancelNegotiatedAttributeOwnershipDivestiture`) is out of scope.
- **Distributed federation lifecycle** — M15+ territory.
- **No proto field renumbering**. Append-only.

### Why now

- M23 closed `delete_object_instance`. Without it, the resign actions involving DELETE were impossible to implement. M24 is the natural follow-up.
- The current state is the most surprising correctness gap left in cut-3: a federation that loses an owner federate has stale ownership records pointing at a non-existent handle. Existing examples don't hit it because they always finish their work before resigning, but real federations would.

---

## 2. Surface design

### 2.1 Ownership manager additions (W1)

```go
// ReleaseAllOwnedBy releases every attribute currently owned by the
// federate. Called from the federation-manager's OnFederateResigned
// chain when the resign action includes attribute divestiture.
//
// Returns the list of (object, attrs) released so the resign caller
// can emit any peer-visible notifications (cut-1 simplification: no
// notifications; the records are dropped silently and subscribers
// receive RemoveObjectInstance only if the resign action also
// includes DELETE_OBJECTS).
func (m *Manager) ReleaseAllOwnedBy(
    ctx context.Context,
    fed core.FederationName,
    h core.FederateHandle,
) []ReleasedAttributeSet
```

`ReleasedAttributeSet` bundles `(ObjectHandle, []AttributeHandle)`.

### 2.2 ResignAction enum expansion (W2)

`proto/rti/v1/common.proto`:

```proto
enum ResignAction {
  RESIGN_ACTION_UNSPECIFIED = 0;
  RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES = 1;
  RESIGN_ACTION_DELETE_THEN_DIVEST                = 2;  // M24
  RESIGN_ACTION_CANCEL_THEN_DELETE                = 3;  // M24
  RESIGN_ACTION_CANCEL_PENDING_OWNERSHIP          = 4;  // M24
  RESIGN_ACTION_NO_ACTION                         = 5;  // M24 — IEEE 1516.1 default in some readings
  RESIGN_ACTION_DELETE_OBJECTS                    = 6;  // M24 — standalone delete (no divest)
}
```

Manager dispatch (in `federation.Manager.ResignFederation`):

| Action | Behavior |
|---|---|
| `UNCONDITIONALLY_DIVEST_ATTRIBUTES` | call `ownership.ReleaseAllOwnedBy` |
| `NO_ACTION` | skip both delete + divest |
| `DELETE_OBJECTS` | call `object.Registry.Delete` for every owned instance |
| `DELETE_THEN_DIVEST` | DELETE_OBJECTS then RELEASE |
| `CANCEL_PENDING_OWNERSHIP` | call `ownership.CancelPendingFor(fed, h)` (NEW) |
| `CANCEL_THEN_DELETE_THEN_DIVEST` | CANCEL + DELETE + RELEASE |

The dispatch logic moves from `manager.go` (current single-line check) to a new `resign.go` file inside `rti/internal/federation/`.

### 2.3 §4.8 listFederationExecutionMembers (W3)

```proto
message ListFederationMembersRequest {
  WireVersion wire_version = 1;
  string federation_name = 2;
}

message ListFederationMembersResponse {
  repeated FederationMember members = 1;
}

message FederationMember {
  uint64 federate_handle = 1;
  string federate_name = 2;
  string federate_type = 3;
}
```

Manager: combine existing `MembersOf` + `FederateTypeOf` + the per-federate name lookup.

### 2.4 §4.28 + §4.30 Abort save/restore (W3)

```proto
service SavepointService {
  // ... existing 7 RPCs ...
  rpc AbortFederationSave(AbortFederationSaveRequest) returns (Empty);
  rpc AbortFederationRestore(AbortFederationRestoreRequest) returns (Empty);
}

message AbortFederationSaveRequest {
  WireVersion wire_version = 1;
  string federation_name = 2;
}

message AbortFederationRestoreRequest {
  WireVersion wire_version = 1;
  string federation_name = 2;
}
```

Manager: `savepoint.Manager.AbortSave(fed)` and `AbortRestore(fed)` clear in-progress save/restore state and signal participating federates via existing event channels.

### 2.5 Error model

New core sentinels:

| Sentinel | gRPC code | Detail |
|---|---|---|
| `ErrResignActionUnsupported` | InvalidArgument | `resign action not supported` |
| `ErrSaveNotInProgress` | FailedPrecondition | `no save in progress to abort` |
| `ErrRestoreNotInProgress` | FailedPrecondition | `no restore in progress to abort` |

Pysdk typed exceptions at codes 714-716 continuing M23's 710-713 range.

---

## 3. Acceptance criteria (exit gate)

1. **`UNCONDITIONALLY_DIVEST_ATTRIBUTES` actually divests.** After resign, `ownership.Manager.OwnerOf(obj, attr)` returns "no owner" for previously-owned attrs. Verified by `rti/spec/M24/resign_divest_test.go`.
2. **All 6 ResignAction values accepted** (5 new + 1 existing). Per-action behavior covered by `resign_actions_test.go`.
3. **`DELETE_OBJECTS` removes owned instances.** Subscribers receive `RemoveObjectInstance` for each.
4. **`CANCEL_PENDING_OWNERSHIP` clears pending divest/acquire** for the federate.
5. **`ListFederationMembers` reachable.** Returns federate_handle + federate_name + federate_type for every joined federate.
6. **`AbortFederationSave` returns the federation to "no save in progress"** state.
7. **`AbortFederationRestore` ditto for restore.**
8. **Pysdk surfaces all 3 new methods + the action parameter on resign_federation.**
9. **Go SDK same.**
10. **Spec test `rti/spec/M24/m24_completion_test.go` is green.**
11. **`scripts/check-milestones.sh` reports `M24: DONE (N/N)`.**

---

## 4. Wave structure

```
                                M24 START
                                    │
    ┌───────────────────────────────┼───────────────────────────────┐
    │                               │                               │
    │   W1   — ownership.ReleaseAllOwnedBy + resign chain wiring    │
    │           rti/internal/ownership/release.go (NEW)             │
    │           rti/cmd/rtid/main.go (chain into OnFederateResigned)│
    │                               │                               │
    │   W2   — ResignAction completeness                            │
    │           proto/rti/v1/common.proto (5 new enum values)       │
    │           rti/internal/federation/resign.go (NEW)             │
    │           ownership.Manager.CancelPendingFor (NEW)            │
    │           SDK action parameter on Resign                      │
    │                               │                               │
    │   W3   — list members + abort save/restore                    │
    │           proto/rti/v1/federation.proto + savepoint.proto     │
    │           manager additions + wire + SDKs                     │
    │                               │                               │
    │   W4   — acceptance gate + docs                               │
    │           rti/spec/M24/* + pysdk/tests/spec/m24/*             │
    │           srs.md M24 row + CHANGELOG +                        │
    │           scripts/check-milestones.sh M24 probe               │
    │                               │                               │
                                    ▼
                       M24 DONE per srs.md §10
```

---

## 5. Tasks

### W1 — ownership ReleaseAllOwnedBy + resign chain
- TASK-274: `rti/internal/ownership/release.go` (NEW): `ReleaseAllOwnedBy`. Iterates `m.owners` map, drops every entry where owner == h. Returns released set for caller use.
- TASK-275: `rti/cmd/rtid/main.go`: chain `ownMgr.ReleaseAllOwnedBy` into `chainOnFederateResigned`.

### W2 — ResignAction completeness
- TASK-276: `proto/rti/v1/common.proto`: uncomment + extend ResignAction enum.
- TASK-277: `rti/internal/core/errors.go` + `rti/internal/transport/grpc/errs.go`: add `ErrResignActionUnsupported`.
- TASK-278: `rti/internal/federation/resign.go` (NEW): per-action dispatch.
- TASK-279: `rti/internal/ownership/manager.go`: `CancelPendingFor(fed, h)` cancels divest+acquire pending for this federate.
- TASK-280: `rti/internal/federation/manager.go`: `ResignFederation` accepts all 6 actions, calls into the resign-dispatch.
- TASK-281: `rti/pkg/federate/federate.go`: `Resign(ctx)` gains optional action; default = UNCONDITIONALLY_DIVEST_ATTRIBUTES.
- TASK-282: `pysdk/rti1516e/connection.py`: same.

### W3 — list members + abort save/restore
- TASK-283: `proto/rti/v1/federation.proto`: ListFederationMembers RPC + messages.
- TASK-284: `proto/rti/v1/savepoint.proto`: AbortFederationSave + AbortFederationRestore.
- TASK-285: `rti/internal/savepoint/manager.go`: `AbortSave`, `AbortRestore`. New sentinels `ErrSaveNotInProgress`, `ErrRestoreNotInProgress`.
- TASK-286: Wire handlers (federation.go + savepoint.go).
- TASK-287: Go SDK: `Federate.ListFederationMembers`, savepoint client `AbortSave`, `AbortRestore`.
- TASK-288: Pysdk: same.

### W4 — Acceptance gate + docs
- TASK-289: `rti/spec/M24/*.go` (resign_divest, resign_actions, list_members, abort_save_restore, m24_completion).
- TASK-290: `pysdk/tests/spec/m24/`, srs.md row, CHANGELOG entry, check_m24() probe.

---

## 6. Test plan

| Test | Asserts |
|---|---|
| `resign_divest_test.go` | UNCONDITIONALLY_DIVEST actually divests |
| `resign_actions_test.go` | All 6 actions dispatch correctly |
| `list_members_test.go` | ListFederationMembers returns federate roster |
| `abort_save_restore_test.go` | Abort returns to "no save/restore in progress" |
| `m24_completion_test.go` | AC §3.1-3.11 surface bindings |

---

## 7. Migration impact

- **Resign behavior change**: pre-M24 `UNCONDITIONALLY_DIVEST_ATTRIBUTES` did nothing observable. Post-M24 it actually divests, which means peers that had recorded the federate as the owner of attributes no longer find it via `QueryOwnership`. This is the spec-correct behavior; existing examples don't depend on the broken pre-M24 semantics.
- **No default change in Resign**: SDKs default to `UNCONDITIONALLY_DIVEST_ATTRIBUTES` (matches pre-M24), so callers that don't pass an action see the same enum at the wire — but with the corrected post-M24 manager behavior.

---

## 8. M24 row append target (W4 — for reference)

```markdown
| **M24** | Agent A + C | FederationManagement (§4) completion + Resign correctness | `UNCONDITIONALLY_DIVEST_ATTRIBUTES` actually divests (pre-M24 was no-op); all 6 ResignAction values accepted (was 1); `ownership.Manager.ReleaseAllOwnedBy` + `CancelPendingFor` (NEW); `ListFederationMembers` (§4.8) + `AbortFederationSave` (§4.28) + `AbortFederationRestore` (§4.30) wired; SDKs gain action parameter on resign + 3 new methods. **DONE 2026-MM-DD** — see `docs/M24_DISPATCH_PLAN.md` and `CHANGELOG-MASTERPLAN.md` |
```
