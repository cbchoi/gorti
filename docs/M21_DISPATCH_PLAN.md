# M21 Dispatch Plan — Complete TimeService gRPC wiring

How the orchestrator dispatches the M21 tasks (TASK-201..220) to maximize parallel sub-agent throughput while keeping every wave orthogonal at the file level.

This document is FROZEN — only the orchestrator may edit. Companions: `docs/DISPATCH.md` (general protocol), `docs/agent-a-rti-core.md` (Agent A brief), `docs/agent-c-pysdk.md` (Agent C brief), `docs/MILESTONE_CHECK.md` (probe definitions), `docs/srs.md` §10 (M21 row appended at end of plan).

---

## 1. Goal & non-goals

### Goal

Wire the **complete** time-management service over gRPC. The
in-process implementation in `rti/internal/time/manager.go` already
ships every primitive M3 + M7 promised — what's missing is the wire
contract for everything beyond `NextMessageRequest` (NER), plus the
federate SDK + example surface to drive it.

The work has **five** parts:

1. **Extend `proto/rti/v1/time.proto`** with the 4 cut-2 advance
   primitives (TAR, TARA, NMRA, FQR) and 3 query RPCs
   (QueryLogicalTime, QueryLookahead, QueryLBTS). Append-only — no
   field renumbering or message reshape.

2. **TimeServiceServer impl** that wraps `*time.Manager` and serves
   every RPC. New file `rti/internal/transport/grpc/time.go` mirrors
   the existing `syncService` (M12 W1) and `objectService` (M2 W3)
   shapes.

3. **Verify grant emission on the wire**. `core.Outbox` is the seam
   the manager already uses, and `streamService` already drains
   per-federate queues. M21 verifies the path covers TAR / TARA /
   NMRA / FQR grants (not only NER) and that no goroutine leaks on
   resign-during-pending.

4. **Federate SDK time surface — both languages.** Extend `rti/pkg/federate/`
   (Go, created in cut-3 Phase 3 prep) with every primitive + query.
   Flip `pysdk/rti1516e/_transport.py`'s time RPCs from no-op to
   real; the bindings already exist, they just short-circuit because
   rtid returns `Unimplemented` today.

5. **Cross-process showcase examples.** Restore `examples/go-timed/`
   (3 Go federates, lookaheads {0.5, 1.0, 2.0}, NER + TAR mixed) and
   `examples/pyjevsim-time-advance/` (3 Python federates) on top of
   the new wire path. Both are referenced in `docs/srs.md` §10
   acceptance criteria for M3 / M7 and were deleted in cut-3 because
   their cross-process invariants were unverifiable.

### Non-goals

- **No new time semantics.** M3 + M7 already implemented all 5 advance
  primitives in `manager.go`. M21 only exposes them on the wire.
  *Narrow exception*: M21 adds **one** Manager mutator,
  `Manager.ModifyLookahead`, because lookahead mutation is in the
  HLA-spec surface but `EnableRegulation` rejects re-calls
  (`ErrTimeAlreadyRegulating`) so there's no path to mutate today.
  Implementation is a stateStore field assignment, not a semantic
  change. Tracked in TASK-202b.
- **No `enableAsynchronousDelivery` / `disableAsynchronousDelivery`.**
  Not in `manager.go` today; would require new state. Tracked as the
  first follow-up after M21.
- **No optimistic / lookahead-zero variants** — M20.
- **No cross-process determinism retrofit, and no changes to
  `rti/cmd/rtid/timed.go`.** Cross-process timing is non-deterministic
  by construction (kernel scheduler, network buffering); M21's
  examples report counts, not bit-identical replays. The
  `-mode=timed-demo` shim in `rti/cmd/rtid/timed.go` and its test
  suite (`rti/cmd/rtid/timed_test.go`) stay untouched as the
  determinism reference. AC §3.10 binds this.

### Why now

- TimeService is the last cut-1 control-plane primitive without a wire
  path. Every other cut-1 service (FederationService,
  DeclarationService, ObjectService, StreamService) has been reachable
  cross-process since M2.
- The in-process implementation is mature: M3 closed cleanly, M7 spec
  tests are green, `manager.go` exposes a stable method set. M21 is
  purely "add the gRPC adapter + SDK wiring," not "design the
  semantics."

---

## 2. Surface design

This section pins every wire-visible decision before any sub-agent
starts. The implementation must conform; deviations require a plan
revision.

### 2.1 Manager → wire mapping

`rti/internal/time/manager.go` exposes 9 methods that need wire
exposure (3 already on the wire; 6 to be added). Plus 3 derived
queries from `core.TimeFederateState` / `TimeSnapshot`.

| Manager method | Existing wire? | M21 wire RPC | Request | Response |
|---|---|---|---|---|
| `EnableRegulation(fed, h, lookahead)` | ✓ | `EnableTimeRegulation` | `EnableRegulationRequest` (existing) | `Empty` |
| `DisableRegulation(fed, h)` | ✓ | `DisableTimeRegulation` | `DisableRegulationRequest` | `Empty` |
| `EnableConstrained(fed, h)` | ✓ | `EnableTimeConstrained` | `EnableConstrainedRequest` | `Empty` |
| `DisableConstrained(fed, h)` | ✓ | `DisableTimeConstrained` | `DisableConstrainedRequest` | `Empty` |
| `NextMessageRequest(fed, h, t)` | ✓ | `NextMessageRequest` | `NERRequest` | `Empty` |
| `NextMessageRequestAvailable(fed, h, t)` | NEW | `NextMessageRequestAvailable` | `NMRARequest` (NEW) | `Empty` |
| `TimeAdvanceRequest(fed, h, t)` | NEW | `TimeAdvanceRequest` | `TARRequest` (NEW) | `Empty` |
| `TimeAdvanceRequestAvailable(fed, h, t)` | NEW | `TimeAdvanceRequestAvailable` | `TARARequest` (NEW) | `Empty` |
| `FlushQueueRequest(fed, h, t)` | NEW | `FlushQueueRequest` | `FQRRequest` (NEW) | `Empty` |
| `ModifyLookahead(fed, h, lookahead)` — **NEW Manager method**, see TASK-202b | NEW | `ModifyLookahead` | `ModifyLookaheadRequest` (NEW) | `Empty` |
| `Snapshot(fed)` (federation-wide) | partial via AdminService | `QueryLBTS` | `QueryLBTSRequest` (NEW) | `QueryLBTSResponse{lbts double}` |
| `TimeFederateState.CurrentTime` | NEW | `QueryLogicalTime` | `QueryFederateTimeRequest` (NEW) | `QueryFederateTimeResponse{logical_time double}` |
| `TimeFederateState.Lookahead` | NEW | `QueryLookahead` | (same `QueryFederateTimeRequest`) | `QueryLookaheadResponse{lookahead double}` |

12 RPCs total: 5 existing + 7 new. All advance primitives return
`Empty`; the grant arrives later via `StreamService.Events` as a
`FederateEvent.grant` (oneof tag 14, existing).

### 2.2 Proto deltas

Append to `proto/rti/v1/time.proto`. Field numbers in new messages
mirror the existing convention (1=wire_version, 2=federation_name,
3=federate_handle, 4+=service-specific). RPC ordering in the service
block goes: enable/disable group, all 5 advance primitives, modify
lookahead, queries.

```proto
service TimeService {
  // Regulation / constrained (existing).
  rpc EnableTimeRegulation(EnableRegulationRequest) returns (Empty);
  rpc DisableTimeRegulation(DisableRegulationRequest) returns (Empty);
  rpc EnableTimeConstrained(EnableConstrainedRequest) returns (Empty);
  rpc DisableTimeConstrained(DisableConstrainedRequest) returns (Empty);

  // Advance primitives.
  rpc NextMessageRequest(NERRequest) returns (Empty);
  rpc NextMessageRequestAvailable(NMRARequest) returns (Empty);
  rpc TimeAdvanceRequest(TARRequest) returns (Empty);
  rpc TimeAdvanceRequestAvailable(TARARequest) returns (Empty);
  rpc FlushQueueRequest(FQRRequest) returns (Empty);

  // Lookahead modification (without re-enabling regulation).
  rpc ModifyLookahead(ModifyLookaheadRequest) returns (Empty);

  // Queries (synchronous reads; no callback).
  rpc QueryLogicalTime(QueryFederateTimeRequest) returns (QueryFederateTimeResponse);
  rpc QueryLookahead(QueryFederateTimeRequest) returns (QueryLookaheadResponse);
  rpc QueryLBTS(QueryLBTSRequest) returns (QueryLBTSResponse);
}

message NMRARequest  { /* same shape as NERRequest */ }
message TARRequest   { /* same shape as NERRequest */ }
message TARARequest  { /* same shape as NERRequest */ }
message FQRRequest   { /* same shape as NERRequest */ }

message ModifyLookaheadRequest {
  WireVersion wire_version = 1;
  string federation_name = 2;
  uint64 federate_handle = 3;
  double lookahead = 4;
}

message QueryFederateTimeRequest {
  WireVersion wire_version = 1;
  string federation_name = 2;
  uint64 federate_handle = 3;
}

message QueryFederateTimeResponse { double logical_time = 1; }
message QueryLookaheadResponse    { double lookahead = 1; }
message QueryLBTSRequest  {
  WireVersion wire_version = 1;
  string federation_name = 2;
}
message QueryLBTSResponse { double lbts = 1; bool finite = 2; }
```

The `finite` bool in `QueryLBTSResponse` is necessary because the
manager returns `core.PositiveInfinity` when no federate is
regulating — we don't pass `+Inf` over `double` since some clients
(notably older Python protobuf runtimes) handle Inf inconsistently.

### 2.3 Error model

The proto wire returns gRPC status codes; M21 uses the same mapping
shape M12 cut-3 established for service-group error mapping. The
detail string is set on the gRPC status's metadata
(`grpc-status-details-bin`); the SDKs parse it to construct typed
error classes.

#### 2.3.1 Manager error → wire mapping (what gets shipped)

These are sentinels the manager actually returns today; M21 wraps
them on the wire. The "M12 cut-3 mapping" column flags rows that
override the existing mapping in `rti/internal/transport/grpc/errs.go`.

| Manager sentinel | Source | gRPC code | Detail string | M12 cut-3 mapping conflict? |
|---|---|---|---|---|
| `core.ErrTimeAlreadyRegulating` | `core/errors.go` | `FailedPrecondition` | `time_regulation_already_enabled` | no — new mapping |
| `core.ErrTimeNotRegulating` | `core/errors.go` | `FailedPrecondition` | `time_regulation_not_enabled` | no |
| `core.ErrTimeAlreadyConstrained` | `core/errors.go` | `FailedPrecondition` | `time_constrained_already_enabled` | no |
| `core.ErrTimeNotConstrained` | `core/errors.go` | `FailedPrecondition` | `time_constrained_not_enabled` | no |
| `core.ErrTimeInvalidLookahead` | `core/errors.go` | `InvalidArgument` | `invalid_lookahead` | no |
| `core.ErrTimeRequestInPast` | `core/errors.go` | `InvalidArgument` | `logical_time_already_passed` | **YES — currently `FailedPrecondition`** in `errs.go`; TASK-202c re-maps to `InvalidArgument` and re-runs every other service-group test that depends on this code. |
| `time.ErrDuplicateNER` | `time/ner.go` (package-private) | `FailedPrecondition` | `in_time_advancing_state` | new — sentinel must be re-exported via a new `core.ErrTimeAdvancingState` alias (TASK-202b) so `errs.go` can match without importing the time package. |
| `core.ErrFederationHalted` (already mapped) | `core/errors.go` | `FailedPrecondition` | `federation_halted` | already mapped; M21 reuses |

#### 2.3.2 HLA-spec errors deferred (NOT mapped by M21)

These appear in the HLA spec but the manager does NOT produce them
today. M21 does **not** invent sentinels for them; the wire wrapper
will not emit these detail strings. SDKs do not define typed errors
for these.

| HLA error | Why deferred |
|---|---|
| `RequestForTimeRegulationPending` | Manager treats `EnableRegulation` as synchronous; no "pending" state. |
| `RequestForTimeConstrainedPending` | Same. |
| `InvalidLogicalTime` (NaN/±Inf t) | `dispatchAdvance` does not validate `t`. NaN is silently accepted. Either add validation in a follow-up or accept that bad input produces a manager-level oddity (not a wire error). Tracked as a §9 follow-up. |
| `FederateHandleNotKnown` (NotFound) | `dispatchAdvance` treats unknown federate as "not regulating, not constrained" → returns `ErrTimeNotRegulating`, not a NotFound. Plan accepts this; SDKs surface it as `ErrTimeRegulationNotEnabled`. |
| `FederationExecutionDoesNotExist` | `dispatchAdvance` does not currently check `federation_name`; if the manager has no state for it, the operation surfaces as `ErrTimeNotRegulating`. |

### 2.4 Grant delivery model

- Every advance primitive (NER, NMRA, TAR, TARA, FQR) returns
  `Empty` synchronously. The actual grant arrives as a
  `FederateEvent.grant` (oneof tag 14, existing) on the federate's
  `StreamService.Events` stream.
- The `TimeAdvanceGrant` proto message carries one field: `double
  time`. The federate-side SDK does not need to know which primitive
  triggered the grant; the next-step semantics depend on local model
  state, not the grant payload.
- Grant ordering is per-federate FIFO (the `streamService` per-federate
  queue is a regular FIFO). The manager guarantees at most one grant
  per outstanding request per federate.

No new oneof tags. No new wire event types.

#### 2.4.1 Stream-conversion gap (must be fixed)

`streamService.Events` (`rti/internal/transport/grpc/stream.go`)
converts events to wire frames via `toFederateEvent`, which only
handles values implementing `federateEventCarrier` (i.e. exposing
`Inner() *rtiv1.FederateEvent`). `time.TimeAdvanceGrant`
(`rti/internal/time/grant.go`) is a `core.OutboundEvent` but does NOT
implement `federateEventCarrier`. **Today**, when the manager calls
`Outbox.Send(grant)`, `toFederateEvent` returns
`errOutboundEventNotConvertible → codes.Internal` and the federate's
stream dies.

This is a **prerequisite to TASK-205 actually working**. Plan adds:

- **TASK-204b** — make `time.TimeAdvanceGrant` and `core.FederationHalted`
  satisfy the wire-conversion path. Cleanest implementation: define
  a shim in `rti/internal/transport/grpc/time.go` that wraps a
  `*time.TimeAdvanceGrant` in a struct implementing
  `federateEventCarrier`, OR extend `toFederateEvent` to type-switch
  on `*time.TimeAdvanceGrant` directly. Choice is the implementer's;
  test in TASK-205 asserts the grant arrives as `FederateEvent.grant`
  on the wire stream.

#### 2.4.2 Resign-during-pending: cleanup path

The plan previously claimed "the manager's existing `cleanupFederate`
path drops pending requests." **`cleanupFederate` does not exist**
in `rti/internal/time/`. The manager has no resign-signal hook today;
when a federate's stream is closed, its pending NER (in `nerStore`)
remains.

**TASK-204c** verifies whether the federation manager's resign path
already calls into the time manager. If not, TASK-204c either:
(a) wires a `Manager.OnFederateResign(fed, h)` hook called from the
federation manager's resign path, dropping pending advance state;
or (b) accepts that pending state is leaked-on-resign (memory only,
not goroutines) and tests confirm no goroutine leak. Implementer
picks based on what `federation.Manager` already exposes.

### 2.5 Idempotency, concurrency, ordering

- **Idempotency**: re-invoking `EnableTimeRegulation` while already
  regulating returns `FailedPrecondition` with `time_regulation_already_enabled`.
  The SDK should NOT retry on this code.
- **Concurrency**: a single federate's RPCs are serialised by a mutex
  on the SDK-side `Federate` struct (TASK-205½ owns introducing it
  if not present). Server-side `manager.go` is internally goroutine-safe.
- **Ordering**: a federate may not have multiple outstanding advance
  primitives at once. Server rejects the second one synchronously
  with `time.ErrDuplicateNER` → `in_time_advancing_state`.

### 2.6 Cut-2 cross-language compatibility

- New RPCs are append-only to the service. Old federates that don't
  know about TAR / TARA / NMRA / FQR / queries continue to work
  unchanged — the gRPC channel happily ignores unknown methods.
- New request messages are new types; nothing collides with existing
  proto numbers.
- The federate handle / federation name shape is identical to existing
  RPCs — no new wire-version negotiation needed. WIRE_VERSION_V1 stays.

### 2.7 SDK surface (Go and Python)

#### 2.7.0 Go SDK foundation prerequisite (TASK-205½)

The `rti/pkg/federate` package today contains only `doc.go` and
`events.go` (3 event types, no client). The time-mgmt SDK
(TASK-206) requires the foundation:

- `Connection` type wrapping `*grpc.ClientConn` + the cut-1 service
  stubs (FederationServiceClient, DeclarationServiceClient,
  ObjectServiceClient, StreamServiceClient, **TimeServiceClient**).
- `Federate` struct holding `(connection, federationName,
  federateHandle, mu sync.Mutex, eventStream, eventCh chan Event,
  cancelStream context.CancelFunc)`.
- `Connect(ctx, addr) (*Connection, error)`, `Connection.Close() error`.
- `Connection.JoinFederation(ctx, FederationSpec, federateName) (*Federate, error)`
  (creates federation if missing; idempotent; spawns the events
  goroutine that drains `StreamService.Events` into `Federate.eventCh`).
- `Federate.Resign(ctx) error` (cancels stream, sends ResignFederation, closes channel).
- `Federate.Events() <-chan Event`.
- `errors.go` with the typed errors per §2.3.1.

This is added by **TASK-205½** (between W2 and W3) so W3A can extend
rather than ground-up. ~600 LoC + bufconn tests.

#### 2.7.1 Go (`rti/pkg/federate/time.go`)

```go
func (f *Federate) EnableTimeRegulation(ctx context.Context, lookahead float64) error
func (f *Federate) DisableTimeRegulation(ctx context.Context) error
func (f *Federate) EnableTimeConstrained(ctx context.Context) error
func (f *Federate) DisableTimeConstrained(ctx context.Context) error
func (f *Federate) ModifyLookahead(ctx context.Context, lookahead float64) error

func (f *Federate) NextMessageRequest(ctx context.Context, t float64) error
func (f *Federate) NextMessageRequestAvailable(ctx context.Context, t float64) error
func (f *Federate) TimeAdvanceRequest(ctx context.Context, t float64) error
func (f *Federate) TimeAdvanceRequestAvailable(ctx context.Context, t float64) error
func (f *Federate) FlushQueueRequest(ctx context.Context, t float64) error

func (f *Federate) QueryLogicalTime(ctx context.Context) (float64, error)
func (f *Federate) QueryLookahead(ctx context.Context) (float64, error)
func (f *Federate) QueryLBTS(ctx context.Context) (lbts float64, finite bool, err error)

// Plus typed errors in errors.go that wrap the gRPC detail strings:
//   ErrTimeRegulationAlreadyEnabled, ErrTimeRegulationNotEnabled, ...
//
// The Events() channel (existing from cut-3 Phase 3 stub) delivers
// federate.TimeAdvanceGrant{Time float64} when grants arrive.
```

#### 2.7.2 Python (`pysdk/rti1516e/_transport.py` already has shapes; flip the dispatch flag)

```python
async def enable_time_regulation(self, lookahead: float) -> None:
async def disable_time_regulation(self) -> None:
async def enable_time_constrained(self) -> None:
async def disable_time_constrained(self) -> None:
async def modify_lookahead(self, lookahead: float) -> None:

async def next_message_request(self, t: float) -> None:
async def next_message_request_available(self, t: float) -> None:
async def time_advance_request(self, t: float) -> None:
async def time_advance_request_available(self, t: float) -> None:
async def flush_queue_request(self, t: float) -> None:

async def query_logical_time(self) -> float:
async def query_lookahead(self) -> float:
async def query_lbts(self) -> tuple[float, bool]:  # (lbts, finite)

# Typed exceptions in pysdk/rti1516e/_grpc_errors.py:
#   TimeRegulationAlreadyEnabled, TimeRegulationNotEnabled, ...
#
# Events stream already delivers TimeAdvanceGrant via the existing
# rti1516e.events.TimeAdvanceGrant dataclass (cut-1).
```

### 2.8 Example design

**`examples/go-timed/`** — 3 Go federate processes:

| Federate | Lookahead | Mode | Behavior |
|---|---|---|---|
| `fast` | 0.5 | regulating + constrained | Issues NER(t) every cycle; emits one Tick per grant |
| `normal` | 1.0 | regulating + constrained | Issues TAR(t) every cycle (different primitive on purpose, to exercise both) |
| `slow` | 2.0 | regulating + constrained | Issues NER(t) every cycle |

All three federates are both regulating AND constrained. The earlier
"constrained-only" sketch was unsafe — `manager.go` waffles on
whether constrained-only federates may NER, so M21 picks the
unambiguously-supported configuration.

**LBTS invariant** (TASK-211 verifier asserts):

Define `lbts(F) = min over regulating federates h of
(currentTime[h] + lookahead[h])` at the moment the manager makes a
grant decision for federate F (`rti/internal/time/lbts.go:32`).

Verifier asserts, per cycle:

1. Every federate received exactly one grant per request.
2. Per-federate grant times are **strictly monotonic** (not just
   non-decreasing — `decideGrant` requires `lb > ct` for non-forced
   grants, so two consecutive grants for the same federate cannot
   collide on time).
3. The grant time for federate F equals `min(t_requested, lbts(F))`
   for NER (where `lb > ct` is the gate); analogous boundaries for
   the other primitives per §2.4.

**`examples/pyjevsim-time-advance/`** — same shape, Python.

Each follows the cut-3 cross-process layout: `_run_common.sh`,
`rtid_run.sh`, three `*_run.sh` federate launchers, `verify_run.sh`,
`README.md`.

---

## 3. Acceptance criteria (exit gate)

Every bullet is a probe `make verify` or `scripts/check-milestones.sh M21` must pass.

1. **Proto matches §2.2 exactly.** `buf generate` and `make py-codegen` regenerate cleanly; TASK-201.4 enforces no field renumber.
2. **TimeService is registered.** A fresh `rtid` started with `--listen :8442` answers every `rti.v1.TimeService` RPC with a non-`Unimplemented` status. Confirmed via direct gRPC call from `rti/spec/M21/time_service_test.go`.
3. **All 5 advance primitives produce grants on the wire.** Spec test exercises NER, NMRA, TAR, TARA, FQR each in a 3-federate setup; each produces a grant within deadline.
4. **Error mapping is correct.** Spec test exercises every row of §2.3's table; gRPC status code + detail string match.
5. **`rti/pkg/federate/time.go` Go API works.** The new examples import it; spec test covers each method.
6. **Python SDK time RPCs flip from no-op to real.** Existing Python API works against bufconn rtid (TASK-208 strikes the "not wired" caveat in `_transport.py`).
7. **`examples/go-timed/` runs cross-process.** 3 federates with lookaheads {0.5, 1.0, 2.0}; runner spawns rtid + 3 Go subprocesses; each issues 10 advance requests; verifier PASSes.
8. **`examples/pyjevsim-time-advance/` runs cross-process.** Mirrors `examples/pyjevsim-relay-cross-process` shape; 3 Python federates; verifier PASSes.
9. **Cut-3 README caveats are struck.** The "Why we don't use HLAFederate.step_once here" section in `examples/pyjevsim-relay-cross-process/README.md`, `examples/pyjevsim/README.md`, `examples/pyjevsim-sync-points/README.md`, `examples/pyjevsim-dashboard-bridged/README.md` is rewritten to cite M21 and link to the time-advance examples.
10. **No regression on the in-process determinism harness.** `rti/cmd/rtid/timed_test.go` keeps passing across `-race -count=10`.
11. **Stream conversion handles `time.TimeAdvanceGrant`.** Spec test asserts a real grant (from a 2-federate NER setup) arrives as `FederateEvent.grant` on the wire — not as `codes.Internal` from `errOutboundEventNotConvertible`. (TASK-204b is the implementation.)
12. **`Manager.ModifyLookahead` exists** and updates lookahead without round-tripping through Disable/Enable. (TASK-202b is the implementation.)
13. **Federate scaffold landed.** `rti/pkg/federate/{federate,errors}.go` exposes `Connection`, `Federate`, `Connect`, `JoinFederation`, `Resign`, `Events()` channel — verified by an in-package smoke test against bufconn rtid before TASK-206 extends with time methods. (TASK-205½ is the implementation.)
14. **Stall → FederationHalted on the wire.** Regulating federate stops responding; stall fires within `StallTimeout`; all peer streams deliver `FederationHalted` event (oneof tag 99). (TASK-205 grows a case for this.)

---

## 4. Wave structure

```
              (cut-3 Phase 1 + 2 done; in-process M3 + M7 done)
                                │
                                ▼
   ┌───────────────────────────────────────────────────────────┐
   │ Wave 1 (1 sub-agent — proto extension first; serialises   │
   │         the rest because every later wave depends on the  │
   │         regen'd stubs)                                     │
   │   W1  — proto/rti/v1/time.proto + buf generate            │
   │           + make py-codegen                  (TASK-201)    │
   └───────────────────────────────────────────────────────────┘
                                │
                                ▼
   ┌───────────────────────────────────────────────────────────┐
   │ Wave 2 (NOT parallel — TASK-202b/c precede 202)           │
   │   W2-pre — Manager.ModifyLookahead method  (TASK-202b)    │
   │            + ErrTimeRequestInPast → InvalidArgument       │
   │            re-mapping in errs.go        (TASK-202c)       │
   │   W2A — TimeServiceServer impl + tests   (TASK-202        │
   │            rti/internal/transport/grpc/time*.go + 203)    │
   │   W2B — rtid main wiring + grants on the wire             │
   │           (TASK-204 — main.go wiring;                     │
   │            TASK-204b — toFederateEvent for                │
   │              time.TimeAdvanceGrant + FederationHalted;    │
   │            TASK-204c — resign-on-pending hook;            │
   │            TASK-205 — grant-path verification incl.       │
   │              stall→FederationHalted)                      │
   └───────────────────────────────────────────────────────────┘
                                │
                                ▼
   ┌───────────────────────────────────────────────────────────┐
   │ Wave 2½ (single sub-agent — Federate scaffold)             │
   │   W2½ — rti/pkg/federate Connection + Federate +          │
   │         Connect/Join/Resign + Events() channel +          │
   │         errors.go                       (TASK-205½)       │
   └───────────────────────────────────────────────────────────┘
                                │
                                ▼
   ┌───────────────────────────────────────────────────────────┐
   │ Wave 3 (2 parallel — depend on W2 + W2½)                   │
   │   W3A — Go SDK time + tests                  (TASK-206    │
   │           rti/pkg/federate/time*.go              + 207)   │
   │   W3B — Python SDK flip + tests              (TASK-208    │
   │           pysdk/rti1516e/_transport.py +         + 209)   │
   │           pysdk/rti1516e/_grpc_errors.py                  │
   └───────────────────────────────────────────────────────────┘
                                │
                                ▼
   ┌───────────────────────────────────────────────────────────┐
   │ Wave 4 (2 parallel — depend on W3)                         │
   │   W4A — examples/go-timed cross-process       (TASK-210   │
   │           regulator_main.go + 3 *_run.sh          + 211)  │
   │           + verify_run.sh + tests                          │
   │   W4B — examples/pyjevsim-time-advance         (TASK-212  │
   │           cross-process, Python                   + 213)  │
   └───────────────────────────────────────────────────────────┘
                                │
                                ▼
   ┌───────────────────────────────────────────────────────────┐
   │ Wave 5 (1 sub-agent — gate)                                │
   │   W5  — rti/spec/M21/* + pysdk/tests/spec/m21/* +         │
   │         strike caveats in cut-3 READMEs +                 │
   │         srs.md M21 row + CHANGELOG +                      │
   │         scripts/check-milestones.sh probe   (TASK-214..220)│
   └───────────────────────────────────────────────────────────┘
                                │
                                ▼
                        M21 DONE per srs.md §10
```

Critical path: 5 waves. Sub-agent runtime ~5–10 min each → wall-time ~30–60 min vs. ~110+ for serial.

---

## 5. File ownership per wave

Within each wave, sub-agents touch disjoint files. Cross-wave shared
files (the orchestrator-seeded stubs) are EXTENDED but not RESHAPED
by sub-agents.

### Wave 1 — proto extension (single agent; serialises rest)

| Sub-agent | Tasks | Owned files | Dependencies |
|---|---|---|---|
| **W1** proto + codegen | TASK-201 | EXTEND: `proto/rti/v1/time.proto`. RUN: `buf generate` + `make py-codegen`. AUTO-EXTEND (gitignored): `rti/internal/genproto/rti/v1/time*.{go,pb}`, `pysdk/rti1516e/_generated/rti/v1/time_pb2{,_grpc,.pyi}.py`. | None — proto is leaf. |

### Wave 2 — server (sequenced)

| Sub-agent | Tasks | Owned files | Dependencies |
|---|---|---|---|
| **W2-pre** Manager method + error remap | TASK-202b + 202c | EXTEND: `rti/internal/time/regulation.go` (or new file) — add `Manager.ModifyLookahead(ctx, fed, h, lookahead) error` that calls `stateStore.modifyLookahead(...)`. EXTEND: `rti/internal/time/state_store.go` (or where state lives) — add `modifyLookahead` mutator. EXTEND: `rti/internal/transport/grpc/errs.go` — re-map `core.ErrTimeRequestInPast` to `InvalidArgument` per §2.3.1; add aliases for `time.ErrDuplicateNER` (or re-export as `core.ErrTimeAdvancingState`). NEW: `rti/internal/time/modify_lookahead_test.go`. | W1 (regen'd stubs). |
| **W2A** TimeServiceServer | TASK-202 + 203 | NEW: `rti/internal/transport/grpc/time.go`, `rti/internal/transport/grpc/time_test.go`. EXTEND: `rti/internal/transport/grpc/server.go` — add `newTimeService(opts.Time)` factory call when `opts.Time != nil`. (Note: `Options.Time` already exists as `core.TimeManager`; W2A may either keep that type or change to `*time.Manager` — choice is the implementer's, but document the choice in the task PR.) | W2-pre. |
| **W2B** rtid wiring + grants on the wire | TASK-204, 204b, 204c, 205 | EXTEND: `rti/cmd/rtid/main.go` — compose `*time.Manager` and pass via `Options.Time`. EXTEND: `rti/internal/transport/grpc/stream.go` (TASK-204b) — extend `toFederateEvent` to convert `*time.TimeAdvanceGrant` and `*time.FederationHalted` payloads, OR provide a wrapper struct in `time.go` implementing `federateEventCarrier`. EXTEND: federation-manager resign path (TASK-204c) — call `time.Manager.OnFederateResign` if added; otherwise document the leak as memory-only and demonstrate goroutine-leak-free with a test. NEW: `rti/cmd/rtid/time_grant_test.go` (TASK-205) — verifies grants flow for every primitive AND stall→FederationHalted reaches every peer. | W2A (server.go shape). |

### Wave 2½ — Go federate SDK foundation (single agent, blocking)

| Sub-agent | Tasks | Owned files | Dependencies |
|---|---|---|---|
| **W2½** federate scaffold | TASK-205½ | NEW: `rti/pkg/federate/federate.go` — `Connection`, `Federate`, `Connect`, `JoinFederation`, `Resign`, `Events()` per §2.7.0. NEW: `rti/pkg/federate/errors.go` — typed errors per §2.3.1. NEW: `rti/pkg/federate/federate_test.go` — bufconn smoke for connect/join/resign/events round-trip. EXTEND: `rti/pkg/federate/events.go` — make sure `TimeAdvanceGrant` etc. are emitted by the events goroutine. | W2A + W2B (server reachable, grants on wire). |

### Wave 3 — SDK surface (parallel)

| Sub-agent | Tasks | Owned files | Dependencies |
|---|---|---|---|
| **W3A** Go SDK time | TASK-206 + 207 | NEW: `rti/pkg/federate/time.go`, `rti/pkg/federate/time_test.go`. EXTEND: `rti/pkg/federate/errors.go` (typed errors specific to time per §2.3.1). | W2½. |
| **W3B** Python SDK time-flip | TASK-208 + 209 | EXTEND: `pysdk/rti1516e/_transport.py`, `pysdk/rti1516e/_grpc_errors.py`. NEW: `pysdk/tests/spec/m21/test_time_client.py`. (Details in TASK-208.) | W2A (server reachable). |

### Wave 4 — examples (parallel)

| Sub-agent | Tasks | Owned files | Dependencies |
|---|---|---|---|
| **W4A** go-timed | TASK-210 + 211 | NEW dir `examples/go-timed/`: `regulator.go` (DEVS-shape model), `regulator_main.go` (federate subprocess entry), `runner.go` (orchestrator), `time-advance-fom.xml`, `_run_common.sh`, `rtid_run.sh`, `fast_run.sh`, `normal_run.sh`, `slow_run.sh`, `verify_run.sh`, `README.md`, `runner_test.go`. | W3A. |
| **W4B** pyjevsim-time-advance | TASK-212 + 213 | NEW dir `examples/pyjevsim-time-advance/`: mirrors `pyjevsim-sync-points/` Python layout — `regulator.py` (model), `regulator_main.py`, `runner.py`, `_federate_common.py`, `time-advance-fom.xml`, `_run_common.sh`, `rtid_run.sh`, three `*_run.sh`, `verify_run.sh`, `README.md`. | W3B. |

### Wave 5 — gate (single agent)

| Sub-agent | Tasks | Owned files | Dependencies |
|---|---|---|---|
| **W5** M21 gate | TASK-214..220 | NEW: `rti/spec/M21/time_service_test.go`, `pysdk/tests/spec/m21/test_time_service_cross_language.py`. EDIT: the 4 cut-3 cross-process example READMEs cited in AC §3.9 (caveat strike). EDIT: `docs/srs.md` (M21 row), `CHANGELOG-MASTERPLAN.md` (M21 close), `scripts/check-milestones.sh` (M21 probe). | All previous waves. |

---

## 6. Tasks

### TASK-201 — Extend proto & regen

**File**: `proto/rti/v1/time.proto` (extend per §2.2). Run `buf generate` + `make py-codegen`.

**Test cases (compilation-level)**:

| Case | Expected |
|---|---|
| 201.1 | `buf lint` clean. |
| 201.2 | `buf generate` produces a non-empty `rti/internal/genproto/rti/v1/time.pb.go` containing `TimeServiceServer` interface with all 12 methods. |
| 201.3 | `make py-codegen` produces `pysdk/rti1516e/_generated/rti/v1/time_pb2.py` containing the new request / response types (`NMRARequest`, `TARRequest`, `TARARequest`, `FQRRequest`, `ModifyLookaheadRequest`, `QueryFederateTimeRequest`, `QueryLBTSRequest`, `QueryFederateTimeResponse`, `QueryLookaheadResponse`, `QueryLBTSResponse`). |
| 201.4 | `git diff` against the existing proto adds rows to the service block but does NOT renumber any existing field or RPC ordering. (Append-only invariant — orchestrator-frozen.) |

### TASK-202b — Manager.ModifyLookahead method

**Files**: `rti/internal/time/regulation.go` (or new `lookahead.go` extension), `rti/internal/time/state_store.go`, `rti/internal/time/modify_lookahead_test.go` (new). Plus re-export `time.ErrDuplicateNER` as `core.ErrTimeAdvancingState` so `errs.go` can map without a circular import.

`Manager.ModifyLookahead(ctx, fed, h, lookahead) error`:

- Validates lookahead per `validateLookahead` (≥0, finite).
- Looks up federate's regulating state. If not regulating → `core.ErrTimeNotRegulating`.
- Mutates the lookahead in `stateStore` (one-line field write under the existing mutex).
- Calls `fireTimeStateChanged(ctx, fed, h)`.
- Returns nil.

Implementation note: this is a **field write**, not a state-machine transition. The §1 non-goals exception applies — adding the mutator does not change any time-management semantics; it just exposes the existing field for mutation.

**Test cases** (modify_lookahead_test.go):

| Case | Expected |
|---|---|
| 202b.1 | ModifyLookahead before EnableRegulation → `ErrTimeNotRegulating`. |
| 202b.2 | EnableRegulation(la=1.0); ModifyLookahead(la=2.0); Snapshot lookahead = 2.0. |
| 202b.3 | ModifyLookahead(NaN) / negative / +Inf → `ErrTimeInvalidLookahead`. |
| 202b.4 | ModifyLookahead while NER pending → currently undefined; pin to "OK" (mutation does not affect the pending request's grant gate, which captured lookahead at NER call). Pin behavior; test it. |

### TASK-202c — Error mapping update in errs.go

**File**: `rti/internal/transport/grpc/errs.go`.

Re-map per §2.3.1:

- `core.ErrTimeRequestInPast` → `codes.InvalidArgument` (was `FailedPrecondition`).
- Add `core.ErrTimeAdvancingState` (the re-export from TASK-202b) → `codes.FailedPrecondition` with detail `in_time_advancing_state`.

**Test cases**: extend `errs_test.go` with two cases asserting the new code mapping. Re-run every existing service-group test to confirm no regression on the mapping change (specifically: no other code path relies on `ErrTimeRequestInPast → FailedPrecondition`; grep before merging).

### TASK-202 — TimeServiceServer wrapper

**File**: `rti/internal/transport/grpc/time.go` (new).

```go
type timeService struct {
    rtiv1.UnimplementedTimeServiceServer
    mgr *time.Manager
}

func newTimeService(mgr *time.Manager) *timeService { ... }
```

Each RPC: validate request fields → translate to manager call → translate manager error to gRPC status (per §2.3 mapping) → return `Empty` (or query response).

**Test cases — handled by TASK-203 below.**

### TASK-203 — TimeServiceServer test suite

**File**: `rti/internal/transport/grpc/time_test.go` (new).

Use the same bufconn harness pattern as `sync_test.go`. Tests are RPC-shape level; manager state changes are verified via the new query RPCs.

| Case | Expected |
|---|---|
| 203.1 | EnableTimeRegulation(la=1.0); QueryLookahead → 1.0. |
| 203.2 | EnableTimeRegulation twice → second returns `FailedPrecondition` + detail `time_regulation_already_enabled`. |
| 203.3 | DisableTimeRegulation when not enabled → `FailedPrecondition` + detail `time_regulation_not_enabled`. |
| 203.4 | EnableTimeConstrained — same enable-twice / disable-when-disabled pattern. |
| 203.5 | EnableTimeRegulation with negative lookahead → `InvalidArgument` + detail `invalid_lookahead`. |
| 203.6 | EnableTimeRegulation with non-existent federate handle → `FailedPrecondition` + detail `time_regulation_not_enabled` (manager treats unknown federate as "not regulating"; see §2.3.2 — there is no `federate_not_found` from the time path). |
| 203.7 | NER(t=10), then NER(t=20) before grant → second returns `FailedPrecondition` + detail `in_time_advancing_state`. |
| 203.8a | **NER strict gate**: regulator(la=1.0) at t=0, peer regulator(la=0.5) at t=0. NER(t=0.5). Grant should **not** fire (LBTS=0.5, requested=0.5, NER requires `LBTS > t`). |
| 203.8b | **NMRA inclusive gate**: same setup, NMRA(t=0.5). Grant **fires** at t=0.5 (NMRA accepts `LBTS >= t`). |
| 203.8c | **TAR multi-pending incremental**: 3 regulators all pending TAR(t=10). Each receives an incremental grant at the LBTS (`min(currentTime+lookahead) over remaining`), not a forced grant; grant times converge. |
| 203.8d | **TARA**: like TAR but with the NMRA-style inclusive gate; covers the "advance to t even when a peer's send is exactly at t" case. |
| 203.8e | **FQR drains queued events before grant**: subscriber federate queues 3 receive-interactions; subscriber issues FQR(t=5). Subscriber's events stream delivers all 3 receives first, then `TimeAdvanceGrant`. |
| 203.8f | **FQR grant time = LBTS, not requested**: regulator A LBTS=2; subscriber FQR(t=10). Grant time is 2, not 10. (Pin cut-1 simplification per `decideGrant`.) |
| 203.9 | NER with logical_time < currentTime+lookahead → `InvalidArgument` + detail `logical_time_already_passed`. (Note: `ErrTimeRequestInPast` triggers on `t < currentTime + lookahead`, NOT on `t < currentTime` — different from HLA-spec wording. TASK-202c re-maps this.) |
| 203.10 | ModifyLookahead while regulating → OK; QueryLookahead reflects the change. |
| 203.11 | QueryLogicalTime before any advance → returns 0.0 (federation start time). |
| 203.12 | QueryLBTS with no regulators → `finite=false`, `lbts=0`; manager returns `core.PositiveInfinity` internally; wrapper translates `+Inf → (false, 0.0)`. |
| 203.13 | QueryLBTS with one regulator(la=1.0) at t=5 → `finite=true`, `lbts=6.0`. |
| 203.14 | QueryLookahead **after DisableRegulation** → `FailedPrecondition` + `time_regulation_not_enabled` (post-disable lookahead is not meaningful; pin this rather than returning 0.0 silently). |
| 203.15 | Federation halted mid-RPC: trigger stall → all subsequent time RPCs from any federate return `FailedPrecondition` + detail `federation_halted`. |
| 203.16 | Concurrent NERs from 3 federates (all regulating+constrained): server delivers grants to all 3 within deadline; the LBTS-min invariant holds. Run under `-race -count=10`. |

### TASK-204 — rtid main wiring

**Files**: `rti/cmd/rtid/main.go` (extend).

`Options.Time` already exists in `server.go` (typed `core.TimeManager`). TASK-204 is just composing the manager in main.go and passing it through. No struct shape change needed (W2A may or may not change the type — see W2A note).

**Test case (204.1)**: starting `rtid` with no flags (default config) exposes `TimeService` on the gRPC port — observable via `time_test.go`'s harness asserting RPCs do not return `Unimplemented`.

### TASK-204b — Stream wire conversion for time events

**Files**: `rti/internal/transport/grpc/stream.go` (extend `toFederateEvent`), and / or `rti/internal/transport/grpc/time.go` (add a `federateEventCarrier`-implementing wrapper for `*time.TimeAdvanceGrant`).

**Why**: see §2.4.1. Without this, every grant the manager emits dies as `codes.Internal` at the stream boundary.

**Test cases**:

| Case | Expected |
|---|---|
| 204b.1 | `toFederateEvent(*time.TimeAdvanceGrant{Time: 5.0})` returns a `*rtiv1.FederateEvent{event: &FederateEvent_Grant{Grant: &TimeAdvanceGrant{Time: 5.0}}}` (or equivalent shape) — no error. |
| 204b.2 | `toFederateEvent(*core.FederationHalted)` (or whatever the time package emits at halt) returns the proto `FederationHalted` event (oneof tag 99). |
| 204b.3 | End-to-end: a regulating federate observes a grant on its `StreamService.Events()` stream after its NER lands. (Spec test in TASK-205.) |

### TASK-204c — Resign-during-pending hook

**Files**: `rti/internal/time/manager.go` (add `OnFederateResign(fed, h)` hook), `rti/internal/federation/manager.go` (call into it from the resign path).

**Why**: see §2.4.2. Plan previously claimed `cleanupFederate` exists; it doesn't.

Implementer's choice:

(a) Add the hook + wire it from federation manager. Drop pending advance state. (Preferred — matches HLA spec semantics.)

(b) Document that pending state leaks on resign (memory only, not goroutines). Add a goroutine-leak-free test. (Acceptable cut.)

**Test cases**:

| Case | Expected |
|---|---|
| 204c.1 (option a) | Federate calls NER(5), then resigns. After resign, the federation's `Snapshot` shows no pending request for that federate. |
| 204c.2 (either option) | After 100 join/NER/resign cycles, `runtime.NumGoroutine()` is stable; no event-stream goroutines linger. Run under `-race -count=10`. |

### TASK-205 — Grant emission verification

**File**: `rti/cmd/rtid/time_grant_test.go` (new).

Verifies the wire path covers every primitive AND that stall fires correctly cross-process.

| Case | Setup | Expected |
|---|---|---|
| 205.1 | 2 federates, A reg+const(la=1.0), B reg+const(la=0.5). A NER(10), B NER(10). Both subscribe Events. | Both receive `TimeAdvanceGrant{0.5}` on their wire streams. |
| 205.2 | Same setup, A TAR(10) instead of NER. | A receives `TimeAdvanceGrant{0.5}`. |
| 205.3 | Same setup, A NMRA(10), B NMRA(10). | Both receive grants — NMRA inclusive vs NER strict boundary visible. |
| 205.4 | Same setup, A TARA(10), B TARA(10). | Both receive grants. |
| 205.5 | Subscriber-only federate FQR(5) with 3 receive-events queued. | Wire stream delivers 3 receives FIRST, then `TimeAdvanceGrant`. |
| 205.6 | Federate calls NER(5), then resigns before grant fires. | No grant on (closed) events stream; goroutine count stable across `-race -count=10`. |
| 205.7 | Federate calls TAR(5), then resigns before grant fires. | Same. |
| 205.8 | Federate calls NMRA / TARA / FQR mid-pending and resigns. | Same — repeat 205.6/205.7 for each remaining primitive. |
| 205.9 | Federate's events stream drops mid-flight (client cancel). | Manager's grant callback does NOT block; OTHER federates still receive grants. |
| 205.10 | **Stall → FederationHalted**: 2 federates A reg(la=1.0), B reg(la=0.5). A NER(10); B never advances. After `StallTimeout` (configured to 1s for the test), both A and B receive `FederationHalted` (oneof tag 99) on their wire streams. |

### TASK-205½ — Federate SDK foundation

**Files**: `rti/pkg/federate/federate.go` (new), `rti/pkg/federate/errors.go` (new), `rti/pkg/federate/federate_test.go` (new). EXTEND: `rti/pkg/federate/events.go` to make `TimeAdvanceGrant` and `FederationHalted` actually emitted by the events goroutine.

Implements §2.7.0:

- `Connection`, `Connect`, `Close`.
- `FederationSpec`, `FOMModule`.
- `Federate` (with `mu sync.Mutex`, federation/handle/name fields, eventCh, streamCancel).
- `Connection.JoinFederation` (idempotent CreateFederation + JoinFederation; spawns events drain goroutine).
- `Federate.Resign` (cancel stream, send ResignFederation, close eventCh).
- `Federate.Events() <-chan Event`.
- All cut-1 declaration / send-interaction methods needed by go-pingpong (already designed in cut-3 Phase 3 prep, just unimplemented).
- Typed errors per §2.3.1: `ErrTimeRegulationAlreadyEnabled`, `ErrTimeRegulationNotEnabled`, `ErrTimeConstrainedAlreadyEnabled`, `ErrTimeConstrainedNotEnabled`, `ErrTimeInvalidLookahead`, `ErrLogicalTimeAlreadyPassed`, `ErrTimeAdvancingState`, `ErrFederationHalted`.

**Test cases (federate_test.go)**:

| Case | Expected |
|---|---|
| 205½.1 | Connect + Close cycle; no goroutine leak across `-race -count=10`. |
| 205½.2 | JoinFederation on a fresh rtid — federation auto-created, federate gets handle, Events() channel returns a non-nil read-only chan. |
| 205½.3 | JoinFederation when federation already exists — succeeds (idempotent). |
| 205½.4 | Resign → events channel closes within 1s; subsequent Events() reads return zero-value `Event` (closed-channel semantics). |
| 205½.5 | Background goroutine that's draining the stream exits cleanly on Resign — no leaks. |
| 205½.6 | Errors: typed-error round-trip (server returns `FailedPrecondition` + detail `time_regulation_already_enabled` → SDK surfaces `ErrTimeRegulationAlreadyEnabled`). One case suffices; TASK-207 covers the rest per-method. |

### TASK-206 — Go federate SDK time surface

**Files**: `rti/pkg/federate/time.go` (new), `rti/pkg/federate/time_test.go` (new). EXTENDS: `rti/pkg/federate/errors.go` (time-specific typed errors if not landed by TASK-205½).

Implements §2.7.1. Bufconn rtid in tests.

### TASK-207 — Go SDK test suite

| Case | Expected |
|---|---|
| 207.1 | EnableTimeRegulation, then QueryLookahead returns the same value. |
| 207.2 | EnableTimeRegulation twice → `errors.Is(err, federate.ErrTimeRegulationAlreadyEnabled)`. |
| 207.3 | NER without enabling regulation → `ErrTimeRegulationNotEnabled` (per manager — see §2.3.1). Pin behavior here, not aspirationally. |
| 207.4 | Two federates exchanging NER → both Events() channels deliver `TimeAdvanceGrant{Time}` within 5s. |
| 207.5 | Each of TAR, TARA, NMRA, FQR — issue + grant arrives with the per-primitive boundary expectation from 203.8a-f. |
| 207.6 | Federate Resigns mid-NER → no goroutine leak under `-race -count=10`. |
| 207.7 | Resign mid-pending for each of NER/TAR/TARA/NMRA/FQR — no goroutine leak. (Mirrors TASK-205.6-205.8 at SDK level.) |
| 207.8 | QueryLBTS with no regulators returns `(_, false, nil)` (the `finite` bool is false). |
| 207.9 | ModifyLookahead → QueryLookahead reflects new value. |
| 207.10 | After DisableRegulation, QueryLookahead returns `(0, ErrTimeRegulationNotEnabled)` per 203.14. |

### TASK-208 — Python SDK time-flip

**File**: `pysdk/rti1516e/_transport.py` (extend).

Flip the dispatch path; the existing python methods already shape
the requests correctly. Removes the "Time-management RPCs are not
wired" caveat from the module docstring.

**File**: `pysdk/rti1516e/_grpc_errors.py` (extend).

Adds 11 typed exception classes per §2.3.

### TASK-209 — Python SDK test suite

**File**: `pysdk/tests/spec/m21/test_time_client.py` (new).

| Case | Expected |
|---|---|
| 209.1 | `await fed.enable_time_regulation(1.0)` against bufconn rtid does NOT raise; subsequent `await fed.next_message_request(5.0)` returns. |
| 209.2 | Cross-language **Go regulator + Python constrained**: Python receives `TimeAdvanceGrant` on its events stream. |
| 209.3 | Cross-language **Python regulator + Go constrained**: Go receives `TimeAdvanceGrant`. (Symmetric inverse of 209.2.) |
| 209.4 | Cross-language **mixed primitives**: Go TAR + Python NMRA in the same federation; both grants arrive. |
| 209.5 | Python `enable_time_regulation` twice → raises `TimeRegulationAlreadyEnabled` (typed). |
| 209.6 | Each of TAR / TARA / NMRA / FQR Python-side — issue + grant arrives with the per-primitive boundary semantics from 203.8a-f. |
| 209.7 | `await fed.query_lookahead()` returns the lookahead set in the previous regulation enable. |
| 209.8 | `await fed.query_lbts()` returns `(0.0, False)` when no regulator. |
| 209.9 | `await fed.modify_lookahead(2.0)` → `query_lookahead` returns 2.0. |

### TASK-210 — examples/go-timed cross-process

**Dir**: `examples/go-timed/`.

Layout per §2.8 + §5 W4A.

### TASK-211 — go-timed runner test

**File**: `examples/go-timed/runner_test.go`.

| Case | Expected |
|---|---|
| 211.1 | End-to-end run with 3 federates and 10 advance cycles each completes within 30s. |
| 211.2 | Verifier confirms each federate received exactly 10 grants. |
| 211.3 | Per-federate grant times are **strictly monotonic** (consecutive grants for the same federate satisfy `g_{i+1} > g_i`; `decideGrant` enforces `lb > ct` for non-forced grants). |
| 211.4 | Per-cycle grant time for federate F equals `min(t_requested, lbts(F))` per the invariant in §2.8. |

### TASK-212 — examples/pyjevsim-time-advance cross-process

**Dir**: `examples/pyjevsim-time-advance/`.

Mirrors `pyjevsim-sync-points/` Python layout.

### TASK-213 — pyjevsim-time-advance test

**File**: `examples/pyjevsim-time-advance/test_time_advance.py`.

Same shape as 211.

### TASK-214 — Go spec test

**File**: `rti/spec/M21/time_service_test.go`.

Binds the M21 acceptance criteria §3 to executable assertions, Go side.

### TASK-215 — Python spec test

**File**: `pysdk/tests/spec/m21/test_time_service_cross_language.py`.

Same, Python side. Covers AC §3.6, §3.8 explicitly (cross-language).

### TASK-216..219 — README caveat strikes

Strike the "Why we don't use HLAFederate.step_once here" sections in:
- TASK-216: `examples/pyjevsim-relay-cross-process/README.md`
- TASK-217: `examples/pyjevsim/README.md`
- TASK-218: `examples/pyjevsim-sync-points/README.md`
- TASK-219: `examples/pyjevsim-dashboard-bridged/README.md`

Each becomes a one-paragraph note: "Time management is now wired (M21 — TimeService). The cross-process driver still uses the untimed loop here for simplicity, but federates that need NER / TAR / etc. should call them directly via the SDK; see `examples/go-timed` and `examples/pyjevsim-time-advance` for the full pattern."

### TASK-220 — Doc gate

**Files**: `docs/srs.md` (extend §10 Milestones table), `CHANGELOG-MASTERPLAN.md` (M21 close entry), `scripts/check-milestones.sh` (add M21 probe).

The `check-milestones.sh M21` probe verifies, in addition to running the spec tests:

- Grep the 4 cut-3 cross-process example READMEs (cited in AC §3.9) for the literal string `"Why we don't use HLAFederate.step_once"`. Probe FAILS if found — guards against regression.
- Grep `proto/rti/v1/time.proto` for `validWireVersion` references in every new RPC handler stub (or assert via `time_test.go` that an unset wire-version returns `InvalidArgument`). Guards against an implementer landing a query RPC without the wire-version check.

`docs/srs.md` §10 row to append:

```markdown
| **M21** | Agent A + C | Complete TimeService gRPC wiring + time-managed cross-process examples | rti.v1.TimeService exposes all 5 advance primitives (NER/NMRA/TAR/TARA/FQR) + regulation/constrained controls + 3 query RPCs cross-process; rti/pkg/federate exposes Go time-mgmt API with typed errors; pysdk time RPCs flip from no-op to real with typed exceptions; examples/go-timed and examples/pyjevsim-time-advance both run cross-process with PASS verifiers. |
```

---

## 7. Test plan summary (M21 test cases at a glance)

| Wave | Sub-agent | Test count | Coverage |
|---|---|---|---|
| W1 | proto + codegen | 4 | proto append-only invariant; gen stable |
| W2-pre | ModifyLookahead + errs remap | 4 + 2 = 6 | Mutator semantics; new error code mapping |
| W2A | TimeServiceServer | 16 (incl. per-primitive boundary 203.8a-f, halted, concurrency) | Each RPC × error; per-primitive boundaries; concurrency |
| W2B | wiring + stream conv + resign + grants | 1 + 3 + 2 + 10 = 16 | Server registered; stream wire conversion; resign hook; all 5 primitives produce grants; per-primitive resign-during-pending; stall→FederationHalted |
| W2½ | Federate scaffold | 6 | Connect/Join/Resign round-trip; events drain goroutine leak-free; typed-error round-trip |
| W3A | Go SDK time | 10 | Round-trip, typed errors, per-primitive grants, post-disable query, leak-free |
| W3B | Python SDK | 9 | Flip from no-op; cross-language smoke (both directions + mixed primitives); per-primitive |
| W4A | go-timed example | 4 | Per-federate grant counts; strict monotonicity; LBTS invariant |
| W4B | pyjevsim-time-advance | 4 | Mirrors W4A, Python side |
| W5 | spec + docs | 8+ | AC coverage end-to-end + 2 verifier strings |

**Total**: 87+ test cases across the milestone.

---

## 8. Risks & mitigations

| Risk | Mitigation |
|---|---|
| Proto extension breaks an in-tree consumer that pinned old codegen. | The `_generated` directories are gitignored — every contributor regenerates locally. CI runs `buf generate` + `make py-codegen` before tests. Contributors with pinned wheels need to `pip install -e './pysdk[dev]'` again. |
| `*time.Manager` API has signatures the wire wrappers can't hit cleanly (e.g. context types, error shapes). | TASK-202 owner audits the manager surface first; if shims are needed they live alongside `time.go` in the grpc package, NOT in the manager itself. The manager stays test-coverage-stable. |
| `core.ErrTimeRequestInPast` re-mapping (TASK-202c) breaks an existing service-group test that relies on the old `FailedPrecondition` mapping. | Implementer greps `errs.go` callers and runs the FULL grpc test suite before merging W2-pre. The plan's risk: this is the only mapping change, so blast radius is the time path; verify with a fresh `go test -race ./rti/internal/transport/grpc/...`. |
| Stream conversion change (TASK-204b) regresses delivery for OTHER event types (object reflects, sync events, etc.) | Test extension covers only the new types; existing `streamService` tests must remain green. CI's `make verify` runs the full transport test suite. |
| ModifyLookahead violates §1's "no new semantics" non-goal. | The exception is documented in §1 and constrained to a single mutator (no new state, no new transitions). If this turns out to require touching the time-state machine, escalate as plan revision. |
| Federate scaffold (TASK-205½) is a sizable independent piece (~600 LoC) that's blocking W3 — risk of W3 starvation. | W2½ is the milestone's critical path. Sub-agent assigned must be A-team; orchestrator schedules accordingly. |
| Grant ordering across primitives — does TAR's grant interleave correctly with peer NER grants? | Outbox is per-federate FIFO and the manager guarantees at most one outstanding request per federate. Cross-primitive ordering is therefore federate-local — it matches in-process behavior. TASK-205 has explicit cross-primitive tests. |
| Cross-language grant arrival ordering — Go server emits grant; Python client may be slow to drain Events(). | Tests use a 5-second deadline per assertion and explicit `await fed.events.get()` rather than racing the event loop. |
| Wall-clock flakiness in cross-process tests. | Tests assert counts and orderings, not timings. The runner uses `--keep-tempdir` to leave logs for debugging; test timeouts are 30s end-to-end (vs ~6s nominal). |
| TASK-204's edits to `rti/cmd/rtid/main.go` accidentally break the in-process `-mode=timed-demo` test suite. | AC §3.10 gates `rti/cmd/rtid/timed_test.go` green throughout M21; the `*time.Manager` instance composed in main.go is shared between demo modes and the new gRPC service, not duplicated. |
| `pysdk/rti1516e/_transport.py` time-RPC stubs ARE no-ops, not silent-failures — flipping them might surface latent bugs in the bindings. | TASK-208 owner enables one RPC at a time, runs `pytest pysdk/tests/spec/m21/`, then proceeds. Any bug is a regression to fix here, not a separate milestone. |

---

## 9. Out of scope (explicitly deferred)

- **Async-delivery toggles** (`enableAsynchronousDelivery` /
  `disableAsynchronousDelivery`). Not implemented in `manager.go`
  today; would require new state. Tracked as immediate post-M21
  follow-up.
- **Optimistic time variants** (LITS, lookahead-zero) — M20.
- **Cross-language determinism harness for time-managed examples.** A
  faked-wall-clock harness could provide bit-identical replays
  cross-process; M21 doesn't ship one. The cut-3 deletion of
  `examples/go-timed/{determinism,replay,stall}_test.go` is
  intentional — those targeted the in-process shim, whose semantics
  remain covered by `rti/cmd/rtid/timed_test.go`.
- **DDS data-plane time advance** — M19's DDS adapter (Phase 1a) is
  control-plane only; the time-management work all routes through
  gRPC regardless of data-plane choice.
