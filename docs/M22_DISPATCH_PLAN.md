# M22 Dispatch Plan — TimeService completion (close M21 carryovers)

How the orchestrator dispatches the M22 tasks (TASK-221..245) to maximize parallel sub-agent throughput while keeping every wave orthogonal at the file level.

This document is FROZEN — only the orchestrator may edit. Companions: `docs/DISPATCH.md` (general protocol), `docs/M21_DISPATCH_PLAN.md` (predecessor), `docs/agent-a-rti-core.md` (Agent A brief), `docs/agent-c-pysdk.md` (Agent C brief), `docs/MILESTONE_CHECK.md` (probe definitions), `docs/srs.md` §10 (M22 row appended at end of plan).

---

## 1. Goal & non-goals

### Goal

Close the four documented carryovers from M21 so TimeService matches the IEEE 1516.1-2010 §8 surface end-to-end:

1. **Pysdk surface parity.** Pysdk `Federate` exposes only `enable_time_regulation` / `enable_time_constrained` / `next_message_request` today. M21 W3B flipped NER from no-op to real, but the other 7 wire-reachable methods (TAR, TARA, NMRA, FQR, ModifyLookahead, QueryLogicalTime, QueryLookahead, QueryLBTS) have no Python surface. Add them, mirroring `rti/pkg/federate/time.go`.

2. **`enable/disableAsynchronousDelivery`** (IEEE 1516.1 §8.16-8.17). Not in `manager.go` today; not on the wire. Implement the manager state, the wire RPCs, both SDKs, **and the underlying TSO delivery gate** that the toggle controls. Default = `false` per spec.

3. **NER+forced-grant race.** M21's pysdk regulator carries a retry-with-backoff workaround; M21's go-timed example carries a TAR-fallback workaround. Diagnose root cause; remove the workarounds.

4. **Spec-test parity.** All four gaps need spec-test coverage in `rti/spec/M22/` and `pysdk/tests/spec/m22/`.

### Non-goals

- **No new advance primitives.** All 5 advance primitives (NER/TAR/TARA/NMRA/FQR) shipped in M21. M22 only adds the async-delivery toggle + closes documented gaps.
- **No optimistic / lookahead-zero variants.** Tracked under M20.
- **No rework of the M3 forced-grant decision logic.** `advance.go::decideGrant`'s `clearPending=false` path is part of M3's pinned semantics. M22 only fixes the SDK-side handling of forced grants OR the manager-side ordering (decided by W3 diagnosis), not the decision rule itself.
- **No new wire event types.** The TSO delivery gate releases events through the existing `Outbox.Send` path; buffering happens server-side and is invisible to the wire.
- **No proto field renumbering.** Append-only — same constraint as M21.
- **No new federate-level API beyond the 9 methods listed in §2.** No `time_advance` callbacks; no `getCurrentLogicalTime` accessor distinct from `query_logical_time`; no helper methods.

### Why now

- M21 closed the wire path but left two visible workarounds in the showcase examples (TAR fallback in `examples/go-timed/`; retry-backoff in `examples/pyjevsim-time-advance/regulator_main.py`). Until those are removed, "TimeService is wired" reads as "TimeService works around itself."
- Pysdk's Federate class is the user-facing surface; missing 7 of 8 advance/query methods means the M21 wire work is invisible to most Python users.
- Async-delivery is the last cut-1/cut-2 control-plane primitive without a manager implementation. Closing it means TimeService is feature-complete against IEEE 1516.1 §8 (modulo M20's optimistic/lookahead-zero variants).

---

## 2. Surface design

### 2.1 Pysdk Federate method set

Mirrors `rti/pkg/federate/time.go` 1:1. Method names in Python snake_case; Rti1516eAmbassador surface (in `pysdk/rti1516e/standard.py`) uses IEEE 1516e camelCase.

| Federate method (snake_case) | 1516e ambassador (camelCase) | Wire RPC | Existing? |
|---|---|---|---|
| `enable_time_regulation(la)` | `enableTimeRegulation` | `EnableTimeRegulation` | ✓ |
| `disable_time_regulation()` | `disableTimeRegulation` | `DisableTimeRegulation` | NEW |
| `enable_time_constrained()` | `enableTimeConstrained` | `EnableTimeConstrained` | ✓ |
| `disable_time_constrained()` | `disableTimeConstrained` | `DisableTimeConstrained` | NEW |
| `modify_lookahead(la)` | `modifyLookahead` | `ModifyLookahead` | NEW |
| `next_message_request(t)` | `nextMessageRequest` | `NextMessageRequest` | ✓ |
| `next_message_request_available(t)` | `nextMessageRequestAvailable` | `NextMessageRequestAvailable` | NEW |
| `time_advance_request(t)` | `timeAdvanceRequest` | `TimeAdvanceRequest` | NEW |
| `time_advance_request_available(t)` | `timeAdvanceRequestAvailable` | `TimeAdvanceRequestAvailable` | NEW |
| `flush_queue_request(t)` | `flushQueueRequest` | `FlushQueueRequest` | NEW |
| `query_logical_time()` → `float` | `queryLogicalTime` → `float` | `QueryLogicalTime` | NEW |
| `query_lookahead()` → `float` | `queryLookahead` → `float` | `QueryLookahead` | NEW |
| `query_lbts()` → `(float, bool)` | `queryLBTS` → `(float, bool)` | `QueryLBTS` | NEW |
| `enable_asynchronous_delivery()` | `enableAsynchronousDelivery` | `EnableAsynchronousDelivery` | NEW (W2) |
| `disable_asynchronous_delivery()` | `disableAsynchronousDelivery` | `DisableAsynchronousDelivery` | NEW (W2) |

Note: `query_lbts` returns `(lbts, finite)`; if `finite == False` the caller treats `lbts` as undefined (matches Go SDK contract; see `rti/pkg/federate/time.go:159`).

### 2.2 Async-delivery proto deltas

Append to `proto/rti/v1/time.proto`. Field numbers in new messages follow the established convention.

```proto
service TimeService {
  // ... existing 13 RPCs from M21 ...

  // Asynchronous delivery toggle (M22).
  rpc EnableAsynchronousDelivery(EnableAsynchronousDeliveryRequest) returns (Empty);
  rpc DisableAsynchronousDelivery(DisableAsynchronousDeliveryRequest) returns (Empty);
}

message EnableAsynchronousDeliveryRequest {
  WireVersion wire_version = 1;
  string federation_name = 2;
  uint64 federate_handle = 3;
}

message DisableAsynchronousDeliveryRequest {
  WireVersion wire_version = 1;
  string federation_name = 2;
  uint64 federate_handle = 3;
}
```

15 RPCs total (13 from M21 + 2 from M22).

### 2.3 Async-delivery semantics (the substantive change)

Per IEEE 1516.1 §8.16-8.17:

- **Async delivery enabled** (M22 = explicit opt-in): TSO messages with timestamp `t` are delivered to the federate as soon as the message is produced, regardless of the federate's logical-time state. This matches gorti's current behavior since cut-1.
- **Async delivery disabled** (M22 default, per spec): TSO messages with timestamp `t` are buffered server-side until the federate either (a) has a pending advance request and `t ≤ pending.requestedTime`, or (b) reaches `currentTime ≥ t` via grant. RO messages are unaffected — always delivered immediately.

#### 2.3.1 TSO delivery gate

A new interface in `rti/internal/core/tso_gate.go`:

```go
// TSODeliveryGate decides whether a TSO outbound event may be
// delivered immediately (async on, OR async off + currentTime ≥ ts)
// or must be buffered (async off + currentTime < ts).
//
// Implemented by *time.Manager. Injected into object.Registry.Options.
type TSODeliveryGate interface {
    // ShouldDeliverNow reports whether a TSO event with the given
    // timestamp may be Sent immediately to the recipient federate.
    // When false, the event is enqueued in the gate's buffer (the
    // gate is responsible for releasing it on time advance).
    //
    // For RO events (timestamp == nil), callers do not consult the gate.
    ShouldDeliverNow(fed core.FederationName, h core.FederateHandle, ts core.LogicalTime) bool

    // BufferTSO enqueues a TSO event whose ShouldDeliverNow returned false.
    // The gate sends it via the supplied Outbox when the federate's time
    // catches up (advance grant arrives) or when async delivery is enabled.
    BufferTSO(ctx context.Context, fed core.FederationName, h core.FederateHandle, ts core.LogicalTime, evt core.OutboundEvent) error
}
```

`object.Registry`'s `update.go` and `interaction.go` consult the gate at the existing `Outbox.Send` call site for TSO events. Pseudocode:

```go
if deliveryTs != nil { // TSO
    if r.opts.TSOGate.ShouldDeliverNow(fed, sub, *deliveryTs) {
        _ = r.opts.Outbox.Send(ctx, fed, sub, evt)
    } else {
        _ = r.opts.TSOGate.BufferTSO(ctx, fed, sub, *deliveryTs, evt)
    }
} else { // RO
    _ = r.opts.Outbox.Send(ctx, fed, sub, evt)
}
```

Backwards-compat: when `Options.TSOGate` is nil, `Registry` falls back to direct `Outbox.Send` for TSO. This makes the gate optional in tests and in the in-process test fixtures.

#### 2.3.2 Buffer release path

The time manager's existing `emitGrant` (in `rti/internal/time/ner.go`) already runs after every advance grant. M22 adds a release pass: after state mutation, scan the federate's TSO buffer and send any event with `ts ≤ grant.time`.

```go
func (m *Manager) emitGrant(...) error {
    // (a) EventLog.Append — unchanged
    // (b) Outbox.Send(grant) — unchanged
    // (c) State mutation — unchanged
    // (d) NEW: release any buffered TSO events with ts ≤ t
    extOf(m).releaseBufferedTSO(ctx, fed, h, t)
    return nil
}
```

`releaseBufferedTSO` is a new method on the time manager's nerStore extension. Holds the buffer lock, drains qualifying events, releases lock, calls Outbox.Send for each (locks released before Send to avoid blocking the wire path under the buffer lock).

#### 2.3.3 Toggle release path

When a federate calls `EnableAsynchronousDelivery`, all currently-buffered TSO events for that federate must be released immediately (the spec requires async-on to be observable as soon as the call returns).

```go
func (m *Manager) EnableAsynchronousDelivery(ctx, fed, h) error {
    // (a) Set state.asyncDelivery = true under lock
    // (b) Drain the federate's TSO buffer; Send all
    // (c) Return
}
```

`DisableAsynchronousDelivery` is a state mutation only — does not retroactively recall already-delivered events.

#### 2.3.4 Buffer ordering + bound

- **Order**: events in the buffer are stored in arrival order (the order `BufferTSO` was called). When released on grant, they are sent in arrival order — the wire delivery FIFO is preserved.
- **No bound on buffer size** in M22. Federates with disabled async + indefinite stall would accumulate unbounded TSO events. Tracked as a §9 follow-up; not blocking M22.
- **No persistence**. Buffered events are lost on rtid restart. Documented in srs.md §10.4 (M22 row).

#### 2.3.5 Default state at federate join

A new federate joins with `asyncDelivery = false` per IEEE 1516.1 §8.17 (default off). This is a behavior change from cut-1 (which behaved as "always on"). **Migration**: existing examples must call `enable_asynchronous_delivery()` explicitly if they do not use time-managed advance and rely on TSO events arriving immediately.

The manager-level audit (W2 sub-task) enumerates existing examples that produce TSO events and decides per-example whether to (a) call enable on join, or (b) leave default off and verify the existing invariants still hold under the new semantics.

### 2.4 Error model — async delivery

Two new sentinels:

| Sentinel | Source | gRPC code | Detail string |
|---|---|---|---|
| `core.ErrTimeAlreadyAsynchronous` | `core/errors.go` (NEW) | `FailedPrecondition` | `time_already_asynchronous` |
| `core.ErrTimeNotAsynchronous` | `core/errors.go` (NEW) | `FailedPrecondition` | `time_not_asynchronous` |

Pysdk gets two new typed exceptions: `TimeAlreadyAsynchronous`, `TimeNotAsynchronous` (codes 708-709, continuing the M21 numbering 700-707).

### 2.5 NER+forced-grant race — diagnostic surface

W3 begins with reproduction, not implementation. The diagnostic test exists at `rti/spec/M22/ner_forced_grant_race_test.go` and exercises:

- Federation with 2+ regulators
- All issue NER(t) for the same t
- Server emits forced grants (`clearPending=false`, per `advance.go:161`) for whichever federate's LBTS allows partial advance
- Test asserts: *no* federate ever observes `ErrTimeAdvancingState` from a properly-issued NER

If the test fails, the root cause becomes investigable (server-side ordering vs SDK-side semantics). If the test passes, the M21 workarounds (TAR fallback in go-timed; retry-backoff in regulator_main.py) are unnecessary and removable.

#### 2.5.1 Hypotheses (in priority order; pinned in plan, decision made by sub-agent during W3)

**H1 — SDK-side semantics misuse (most likely).** The federate is reissuing NER after a *forced* (partial) grant, when the spec says the federate should keep waiting on the same NER until full advance is reached. Forced grants tell the federate "you may now consume messages with timestamp ≤ T" but `pendingNER` correctly stays true. **Fix**: SDK tracks `requestedTime`; in `wait_for_grant` / `Events()`, distinguish forced grants (`grant.time < requestedTime`) from full grants and only iterate the cycle on full grants.

**H2 — Server-side state-mutation ordering.** `emitGrant` orders `Outbox.Send` before state mutation (`pendingNER = false`). For a *full* grant (clearPending=true), this means: federate sees grant on wire, immediately issues next NER, server still observes pendingNER=true → ErrTimeAdvancingState. **Fix**: hold the nerStore lock across the full Send + state-mutation block, OR move state mutation to before Send. The latter risks NFR-DET-1 if Send fails (state advances without delivered grant); the former adds wire-call latency under lock. Decision in W3.

**H3 — Both.** SDK fix lands the partial-grant semantics; server fix lands the lock-widening for the full-grant case. Both are needed if both code paths produce the symptom.

The W3 sub-agent's deliverable is: (1) reproduction test, (2) hypothesis ruled in/out by inspecting `manager.go::emitGrant` behavior in the test fixture, (3) the chosen fix landed with regression test.

### 2.6 Idempotency, concurrency, ordering

- **Idempotency**: re-invoking `EnableAsynchronousDelivery` while already async returns `FailedPrecondition` with `time_already_asynchronous`. SDK should NOT retry on this code.
- **Concurrency**: the time manager's nerStore extension is internally goroutine-safe. The new TSO buffer adds a per-federate slice protected by the existing `ext.mu`.
- **Ordering**: TSO buffer release is FIFO per federate. RO events bypass the buffer entirely.

---

## 3. Acceptance criteria (exit gate)

Every bullet is a probe `make verify` or `scripts/check-milestones.sh M22` must pass.

1. **Pysdk Federate class exposes all 15 time methods** listed in §2.1. Confirmed by `pysdk/tests/spec/m22/test_pysdk_time_surface.py` introspecting `Federate.__dict__`.
2. **Rti1516eAmbassador exposes all 15 corresponding camelCase methods.** Confirmed by `pysdk/tests/spec/m22/test_ambassador_surface.py`.
3. **`examples/pyjevsim-time-advance/regulator_main.py` no longer carries the retry-on-`TimeAdvancingState` backoff loop.** The M21 W4B workaround is gone; the example uses `next_message_request` directly with no retry. Verified by literal-string check in scripts/check-milestones.sh and by the example's own runner test.
4. **`examples/go-timed/regulator_main.go` either reverts to NER (if W3 root cause permits) or stays on TAR (if W3 finds NER inherently incompatible with the example's cycle pattern).** W3 sub-agent decides; CHANGELOG documents the call.
5. **`EnableAsynchronousDelivery` and `DisableAsynchronousDelivery` are reachable cross-process from both Go and Python.** Confirmed by `rti/spec/M22/async_delivery_test.go::TestSpec_M22_ToggleReachable`.
6. **TSO buffering is observable.** With async off and a federate that has not advanced, TSO updates from a producer with `t > federate.currentTime` are NOT delivered until the federate advances past `t`. RO events are delivered immediately regardless. Confirmed by `rti/spec/M22/async_delivery_test.go::TestSpec_M22_TSOBufferedUntilGrant`.
7. **Toggle release works.** With async off and TSO events buffered, calling `EnableAsynchronousDelivery` releases all buffered events immediately. Confirmed by `TestSpec_M22_EnableReleasesBuffer`.
8. **Default is async off.** A freshly-joined federate has `asyncDelivery = false`. Confirmed by manager-level test `time_async_test.go::TestAsyncDeliveryDefaultOff`.
9. **NER+forced-grant race no longer reproducible.** `rti/spec/M22/ner_forced_grant_race_test.go` runs the multi-federate NER cycle and asserts no `ErrTimeAdvancingState` ever surfaces from a properly-issued NER.
10. **Migration audit complete.** `docs/M22_DISPATCH_PLAN.md` §10 lists every example that produces TSO events and documents its M22 status (calls `enable_asynchronous_delivery` on join / unaffected by the default flip / has explicit time advance).
11. **Spec test `rti/spec/M22/time_service_completion_test.go` is green.** Binds AC §3.1-3.10 invariants.
12. **`bash scripts/check-milestones.sh` reports `M22: DONE (N/N)`** with all probes green.

---

## 4. Wave structure

```
                                M22 START
                                    │
    ┌───────────────────────────────┼───────────────────────────────┐
    │                               │                               │
    │   W1   — pysdk surface parity (Agent C)                       │
    │           pysdk/connection.py + _transport.py + standard.py   │
    │           tests/test_time_client.py                           │
    │           orthogonal at file level to W2/W3                   │
    │                               │                               │
    │   W2   — async delivery (Agent A + Agent C)                   │
    │           proto/rti/v1/time.proto extension                   │
    │           rti/internal/core/{errors.go,tso_gate.go}           │
    │           rti/internal/time/{manager.go,asyncdelivery.go,    │
    │                              states.go}                       │
    │           rti/internal/transport/grpc/{time.go,errs.go}       │
    │           rti/internal/object/{registry.go,update.go,        │
    │                                interaction.go}                │
    │           rti/pkg/federate/time.go + pysdk surface            │
    │           specs at rti/spec/M22/ + pysdk/tests/spec/m22/      │
    │                               │                               │
    │   W3   — NER+forced-grant race diagnosis + fix (Agent A)      │
    │           rti/spec/M22/ner_forced_grant_race_test.go          │
    │           workaround removal in 2 examples                    │
    │                               │                               │
    │   W4   — acceptance gate + docs (orchestrator)                │
    │           rti/spec/M22/* + pysdk/tests/spec/m22/*             │
    │           srs.md M22 row + CHANGELOG +                        │
    │           scripts/check-milestones.sh M22 probe               │
    │                               │                               │
                                    ▼
                       M22 DONE per srs.md §10
```

W1 and W3 are independent of each other and of W2's first half (proto + manager). They can run concurrently. W2's second half (object.Registry integration + spec tests) depends on W2 first half. W4 depends on all three.

### 4.1 Suggested dispatch order

If serial: W1 → W2 → W3 → W4. W1 first because it's mechanical and unblocks the example workaround removal in W3.

If parallel: dispatch W1 (Agent C) and W3 (Agent A) immediately; W2 starts when proto extension can land cleanly without conflicting with W1's pysdk additions (W1 only adds methods, W2 also only adds methods — no file conflict, but the buf-generated stubs are regenerated; serialize the codegen step).

---

## 5. File ownership (orthogonality matrix)

| File | W1 | W2 | W3 | W4 |
|---|---|---|---|---|
| `pysdk/rti1516e/connection.py` | ADD methods | ADD 2 methods | — | — |
| `pysdk/rti1516e/_transport.py` | ADD dispatch | ADD 2 dispatch | — | — |
| `pysdk/rti1516e/standard.py` | ADD camelCase | ADD 2 camelCase | — | — |
| `pysdk/rti1516e/_grpc_errors.py` | — | ADD 2 typed | — | — |
| `pysdk/tests/test_time_client.py` | EXTEND | EXTEND | — | — |
| `examples/pyjevsim-time-advance/regulator_main.py` | — | — | EDIT (drop backoff) | — |
| `examples/go-timed/regulator_main.go` | — | — | EDIT (decide NER vs TAR) | — |
| `proto/rti/v1/time.proto` | — | EXTEND | — | — |
| `rti/internal/core/errors.go` | — | ADD 2 sentinels | — | — |
| `rti/internal/core/tso_gate.go` | — | NEW FILE | — | — |
| `rti/internal/time/manager.go` | — | EXTEND (Enable/Disable methods) | possibly EDIT (W3-H2) | — |
| `rti/internal/time/asyncdelivery.go` | — | NEW FILE | — | — |
| `rti/internal/time/states.go` | — | ADD `asyncDelivery` field | — | — |
| `rti/internal/time/ner.go` | — | EDIT `emitGrant` (release pass) | possibly EDIT (W3-H2) | — |
| `rti/internal/transport/grpc/time.go` | — | ADD 2 RPC handlers | — | — |
| `rti/internal/transport/grpc/errs.go` | — | ADD 2 sentinel mappings | — | — |
| `rti/internal/object/registry.go` | — | EXTEND `Options` (TSOGate) | — | — |
| `rti/internal/object/update.go` | — | EDIT (consult gate) | — | — |
| `rti/internal/object/interaction.go` | — | EDIT (consult gate) | — | — |
| `rti/pkg/federate/time.go` | — | ADD 2 methods | — | — |
| `rti/cmd/rtid/main.go` | — | wire TSO gate composition | — | — |
| `rti/spec/M22/*.go` | — | NEW (W2 spec tests) | NEW (race test) | NEW (acceptance gate) |
| `pysdk/tests/spec/m22/*.py` | — | NEW (W2 spec tests) | — | NEW (acceptance gate) |
| `docs/srs.md` | — | — | — | EDIT (M22 row) |
| `CHANGELOG-MASTERPLAN.md` | — | — | — | EDIT (M22 close entry) |
| `scripts/check-milestones.sh` | — | — | — | EDIT (M22 probe) |

---

## 6. Tasks

### W1 — Pysdk surface parity

- **TASK-221** (Agent C, `pysdk/rti1516e/connection.py`) — Add 7 methods to `Federate` class mirroring `rti/pkg/federate/time.go`: `disable_time_regulation`, `disable_time_constrained`, `modify_lookahead`, `time_advance_request`, `time_advance_request_available`, `next_message_request_available`, `flush_queue_request`, `query_logical_time`, `query_lookahead`, `query_lbts`. Each method is a `_dispatch` call to the transport with the federate's handle and the method-specific args. Return types: `None` for advance/modify methods; `float` for queries; `tuple[float, bool]` for `query_lbts`.
- **TASK-222** (Agent C, `pysdk/rti1516e/_transport.py`) — Add corresponding dispatch methods (`_disable_time_regulation`, etc.) and route them in the `_dispatch_grpc` switch (lines ~248-260 today). Pattern: copy `_next_message_request` (lines ~457+) and substitute the proto request type. Wire calls to the existing `time_pb2_grpc.TimeServiceStub` (added in M21 W3B).
- **TASK-223** (Agent C, `pysdk/rti1516e/standard.py`) — Add corresponding camelCase methods to `Rti1516eAmbassador` class. Pattern: copy `nextMessageRequest` (line ~192).
- **TASK-224** (Agent C, `pysdk/tests/test_time_client.py`) — Add unit tests for each new method asserting the correct gRPC RPC is invoked with the correct request fields. Use the existing `MockTimeStub` pattern from M21 W3B's tests.
- **TASK-225** (Agent C, `pysdk/tests/spec/m22/test_pysdk_time_surface.py`, NEW FILE) — Surface introspection test: `Federate.__dict__` exposes all 15 methods listed in §2.1; `Rti1516eAmbassador.__dict__` exposes all 15 corresponding camelCase methods.

### W2 — Async delivery

- **TASK-226** (Agent A, `proto/rti/v1/time.proto`) — Append `EnableAsynchronousDelivery` + `DisableAsynchronousDelivery` RPCs and 2 request messages per §2.2. Run `buf generate` and `make py-codegen`; verify generated stubs compile.
- **TASK-227** (Agent A, `rti/internal/core/errors.go`) — Add `ErrTimeAlreadyAsynchronous` and `ErrTimeNotAsynchronous` sentinels.
- **TASK-228** (Agent A, `rti/internal/core/tso_gate.go`, NEW FILE) — Define `TSODeliveryGate` interface per §2.3.1.
- **TASK-229** (Agent A, `rti/internal/time/states.go`) — Add `asyncDelivery bool` field to per-federate state. Default `false` per §2.3.5.
- **TASK-230** (Agent A, `rti/internal/time/asyncdelivery.go`, NEW FILE) — TSO buffer + release machinery. Per-federate `[]bufferedTSOEvent`; `releaseBufferedTSO(ctx, fed, h, t)` drains qualifying events and sends them. Buffer lock independent of nerStore lock to avoid contention.
- **TASK-231** (Agent A, `rti/internal/time/manager.go`) — Add `EnableAsynchronousDelivery` and `DisableAsynchronousDelivery` manager methods. Per §2.3.3, Enable also drains the buffer.
- **TASK-232** (Agent A, `rti/internal/time/ner.go::emitGrant`) — After state mutation, call `releaseBufferedTSO(ctx, fed, h, t)`. Holds buffer lock briefly; releases before sending each event.
- **TASK-233** (Agent A, `rti/internal/time/manager.go`) — Implement `TSODeliveryGate` interface on `*Manager`: `ShouldDeliverNow` returns true when (a) async on, OR (b) async off + currentTime ≥ ts. `BufferTSO` enqueues to per-federate buffer.
- **TASK-234** (Agent A, `rti/internal/transport/grpc/time.go`) — Add `EnableAsynchronousDelivery` + `DisableAsynchronousDelivery` RPC handlers. Wrap manager errors via `wrapStatusErr`.
- **TASK-235** (Agent A, `rti/internal/transport/grpc/errs.go`) — Add 2 sentinel mappings per §2.4.
- **TASK-236** (Agent A, `rti/internal/object/registry.go`) — Extend `Options` with `TSOGate core.TSODeliveryGate` (optional; nil → bypass). Document fallback in registry comment.
- **TASK-237** (Agent A, `rti/internal/object/update.go` + `interaction.go`) — At each `Outbox.Send` site for TSO events, consult the gate per §2.3.1 pseudocode. RO events bypass.
- **TASK-238** (Agent A, `rti/cmd/rtid/main.go`) — Composition: pass `timeMgr` as `TSOGate` when constructing `object.Registry`.
- **TASK-239** (Agent A, `rti/pkg/federate/time.go`) — Add `EnableAsynchronousDelivery` + `DisableAsynchronousDelivery` methods on `*Federate`.
- **TASK-240** (Agent C, `pysdk/rti1516e/_grpc_errors.py`) — Add `TimeAlreadyAsynchronous` (code 708) + `TimeNotAsynchronous` (code 709) typed exceptions. Update `_time_class_for(detail)` mapping.
- **TASK-241** (Agent A, `rti/spec/M22/async_delivery_test.go`, NEW FILE) — 4 spec tests: `TestSpec_M22_ToggleReachable`, `TestSpec_M22_TSOBufferedUntilGrant`, `TestSpec_M22_EnableReleasesBuffer`, `TestSpec_M22_AsyncDeliveryDefaultOff`. Per AC §3.5/3.6/3.7/3.8.

### W3 — NER+forced-grant race

- **TASK-242** (Agent A, `rti/spec/M22/ner_forced_grant_race_test.go`, NEW FILE) — Reproduce the multi-federate NER cycle pattern in a manager-level test. Assert no `ErrTimeAdvancingState` surfaces from properly-issued NER. Decide ruling on H1 vs H2 vs H3 based on observed behavior.
- **TASK-243** (Agent A, file determined by W3 diagnosis) — Land the chosen fix:
  - If H1: SDK fix in `rti/pkg/federate/` (and pysdk equivalent) to track `requestedTime` and gate cycle iteration on full grants.
  - If H2: server fix in `rti/internal/time/ner.go::emitGrant` to widen the lock OR reorder state mutation. Document NFR-DET-1 implications in the commit message.
  - If H3: both.
- **TASK-244** (Agent A + C, `examples/pyjevsim-time-advance/regulator_main.py` + `examples/go-timed/regulator_main.go`) — Remove the M21 workarounds. For pyjevsim-time-advance: drop the retry-on-`TimeAdvancingState` loop. For go-timed: revert TAR to NER if W3-H1 is the verdict and the SDK fix lands; otherwise stay on TAR and document why in the example README.

### W4 — Acceptance gate + docs

- **TASK-245** (Orchestrator, multi-file) — Acceptance test gate + docs:
  - `rti/spec/M22/time_service_completion_test.go` (NEW) — binds AC §3.1-3.10 invariants; assertion-based, not skipping.
  - `pysdk/tests/spec/m22/test_time_service_completion.py` (NEW) — Python-side AC binding.
  - `docs/srs.md` §10.4 — append M22 row.
  - `CHANGELOG-MASTERPLAN.md` — M22 close entry under cut 3.
  - `scripts/check-milestones.sh` — `check_m22()` function with N probes; literal-string regression check that the `TimeAdvancingState` retry-backoff comment cannot reappear in `regulator_main.py`.

---

## 7. Test plan summary

| Test | File | Asserts |
|---|---|---|
| Pysdk surface introspection | `pysdk/tests/spec/m22/test_pysdk_time_surface.py` | All 15 Federate methods + 15 Ambassador methods present |
| Pysdk RPC dispatch | `pysdk/tests/test_time_client.py` (extended) | Each new method invokes the right gRPC stub method with right fields |
| Async toggle reachable | `rti/spec/M22/async_delivery_test.go::TestSpec_M22_ToggleReachable` | Both RPCs return Empty cross-process |
| TSO buffered when async off | `...TestSpec_M22_TSOBufferedUntilGrant` | Producer sends TSO @ t=5; subscriber at currentTime=0 with async off does NOT receive until grant arrives at ≥5 |
| Toggle release | `...TestSpec_M22_EnableReleasesBuffer` | Buffered events drain on Enable |
| Default off | `time_async_test.go::TestAsyncDeliveryDefaultOff` (manager-level) | Fresh federate state has asyncDelivery=false |
| NER race reproduction + non-recurrence | `rti/spec/M22/ner_forced_grant_race_test.go` | After fix, multi-federate NER cycles run cleanly with no `ErrTimeAdvancingState` |
| Acceptance gate | `rti/spec/M22/time_service_completion_test.go` | All AC §3.x invariants |

---

## 8. Migration impact (default-flip audit)

The change from "TSO always delivered immediately" (cut-1 implicit behavior) to "TSO buffered when async off (default)" is observable for every example that produces or consumes TSO events. Examples to audit during W2:

| Example | Uses TSO? | M22 action |
|---|---|---|
| `examples/go-pingpong/` | Probably (interactions) | Audit; likely needs `enable_asynchronous_delivery` since no time advance |
| `examples/go-timed/` | TSO via TAR cycles | No action — federates advance time, so events flow at grant boundaries (the spec semantics) |
| `examples/pyjevsim/` (Producer/Consumer) | TSO interactions | Audit; the consumer must call `enable_asynchronous_delivery` (no time advance) |
| `examples/pyjevsim-relay-cross-process/` | TSO interactions | Audit; same |
| `examples/pyjevsim-relay/` | TSO interactions | Audit; same |
| `examples/pyjevsim-dashboard*/` | TSO updates | Audit; both sensor + dashboard call enable (no time advance) |
| `examples/pyjevsim-sync-points/` | TSO ticks between labels | Audit; the federates between labels call enable (rendezvous-driven, not time-driven) |
| `examples/pyjevsim-time-advance/` | TSO with NER | No action — time-advance-driven |

Audit deliverable: `examples/<name>/README.md` updated with a paragraph noting M22 default change + which call (if any) was added. Migration is one line per affected example.

---

## 9. Follow-ups (deferred to M23+)

- **TSO buffer bound + overflow policy** — M22 ships unbounded buffers. M23 should add a per-federate cap with documented overflow policy (drop-oldest? halt federation? log + drop?).
- **Buffer persistence across rtid restart** — M22 loses buffered events on restart. Spec compliance does not require persistence, but production deployments may.
- **`enableAsynchronousDelivery` per-class scoping** — IEEE 1516.1 §8.16 scopes the toggle federation-wide for a given federate; some implementations allow per-class. Not in M22.
- **Lookahead-zero / optimistic time variants** — M20.
- **NER `requestedTime` tracking in SDK** — if W3-H1 is the verdict, the SDK fix in TASK-243 introduces `requestedTime` tracking. Future SDK ergonomics may build on this (e.g., `next_message_request` returns a future that resolves on full grant, not partial).

---

## 10. M22 row append target (for W4 — for reference, do not edit srs.md before W4)

```markdown
| **M22** | Agent A + C | Complete TimeService surface (close M21 carryovers) | Pysdk Federate exposes all 15 time methods + 1516e ambassador parity; `enable/disableAsynchronousDelivery` reachable cross-process with TSO buffering + release semantics per IEEE 1516.1 §8.16-8.17 (default off per spec); NER+forced-grant race fixed (workarounds in `examples/pyjevsim-time-advance/` + `examples/go-timed/` removed); migration audit complete for all TSO-producing examples. **DONE 2026-MM-DD** — see `docs/M22_DISPATCH_PLAN.md` and `CHANGELOG-MASTERPLAN.md` |
```

---

## 11. Open design questions (must be resolved before W2 begins)

1. **Default flip — keep or revert.** The plan currently pins default=false per spec. Existing examples must opt in. Alternative: keep default=true (matches cut-1 behavior); make spec-compliant default=false a flag on the `JoinFederation` RPC. **Decision pending — orchestrator + user sign off before W2.**
2. **TSO buffer scope.** Per-federate (M22 plan) or per-(federate, class)? Per-federate is simpler and matches §8.16 wording. Per-(federate, class) would allow finer-grained buffering but requires class-id tracking through the gate. **Pinned: per-federate.**
3. **Lookahead-zero interaction.** A federate with lookahead=0 calling NER(t=currentTime) is a no-op or an error? Not in M22 scope; defer to M20.

---

## 12. Status tracking

This plan is updated only by the orchestrator. Per-wave commit messages reference TASK numbers. Sub-agents drop status lines into `docs/reports/M22/agent-{a,c}.md` as they close tasks.
