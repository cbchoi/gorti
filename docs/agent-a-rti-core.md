# Agent A Brief — RTI Core Services (claude-sandbox)

**Pre-reading required**: `docs/AGENTS.md`, `docs/srs.md`. Do not start work until you've read both.

---

## 1. Your Role

You own the RTI core services: the implementation of HLA federation lifecycle, declaration management, object/interaction handling, time management, the deterministic event log, and the gRPC service handlers that bind it all together.

This is the largest and most spec-dense component. Your work is gated by **strict adherence to IEEE 1516-2010** and the project's **determinism discipline** (`docs/AGENTS.md` §10).

## 2. Owned Paths (you may write here)

- `rti/cmd/rtid/` — the RTI binary entrypoint, flag parsing, top-level wiring.
- `rti/internal/federation/` — federation lifecycle, federate handle assignment.
- `rti/internal/declaration/` — pub/sub bookkeeping.
- `rti/internal/object/` — object instance registry, update/reflect, send/receive interaction.
- `rti/internal/time/` — time regulation, NER, LBTS calculation, advance grants.
- `rti/internal/eventlog/` — TSO event log writer + replay reader.
- `rti/internal/transport/grpc/` — gRPC service handler implementations (against contracts in `proto/`).
- Tests in any of the above.
- `examples/go-pingpong/`, `examples/go-timed/`.

## 3. Forbidden Paths

You may **read** but never **write**:

- `proto/**` (frozen contracts)
- `rti/internal/core/**` (frozen interfaces)
- `rti/pkg/fom/**` (Agent B owns)
- `rti/pkg/encoding/**` (Agent B owns)
- `pysdk/**` (Agent C owns)
- `docs/**` (orchestrator owns)
- `.github/**` (orchestrator owns)

If you need a change in any of these, open a `contract-change-request:` issue.

## 4. Milestone Deliverables

### M2 — Federation + Declaration + Object Mgmt + Event Log + gRPC handlers

Implements: **FR-FM-1..5, FR-DM-1..3, FR-OM-1..5, FR-EVT-1..3, FR-FOM-4, NFR-CRASH-1, NFR-OPS-1, NFR-OPS-2, IR-PROTO-1..3.**

Concrete deliverables:

- `rti/internal/federation/`
  - `Manager` type implementing `core.FederationStore` interface.
  - `CreateFederation(name, fomModules)` — calls into `rti/pkg/fom` for parsing/validation; rejects on error.
  - `JoinFederation(fedName, federateName)` — assigns deterministic handle; emits `FederateJoined` event to log.
  - `ResignFederation(handle, action)` — `UNCONDITIONALLY_DIVEST_ATTRIBUTES` only in cut 1; cleans up owned objects.
  - `DestroyFederation(name)` — rejected if joined > 0.
- `rti/internal/declaration/`
  - Per-federation pub/sub matrices (object class × attribute → publishers/subscribers; interaction class → publishers/subscribers).
  - Deterministic iteration (sorted handle order) when matching subscribers.
- `rti/internal/object/`
  - `Registry` per federation; deterministic object handle assignment (monotonic, log-recorded).
  - `Discover` callbacks fanned out in deterministic order.
  - Update/reflect path: encode via `rti/pkg/encoding`, route to subscribers, write to event log first (write-ahead).
  - Interaction path symmetric.
- `rti/internal/eventlog/`
  - `Writer`: length-prefixed Protobuf records, magic header `KDRTI\0\1\0`, version field, monotonic seq.
  - `Reader`: streaming iterator; `Replayer` re-feeds events through the same code path used by live operation.
- `rti/internal/transport/grpc/`
  - Service handlers binding `proto/rti/v1/*.proto` to the above components.
  - Bidi-stream handler for the data plane: dispatches inbound updates, fans out to subscribed streams.
- `rti/cmd/rtid/`
  - Flags: `--listen`, `--tls-cert`, `--tls-key`, `--metrics-listen`, `--log-level`, `--log-format`.
  - Wires Prometheus metrics handler on `--metrics-listen`.

**M2 exit criteria** (objective, testable):

1. `examples/go-pingpong/` — two in-process Go federates exchange 1000 interactions; runs to completion in <5 s.
2. Run the example **10 consecutive times** with same seed; assert event log files are byte-identical (use `sha256sum`).
3. Replay test: feed the M2 event log back through the RTI; new event log is byte-identical to original.
4. Federate handle assignment is deterministic across all 10 runs.
5. `go test ./rti/internal/...` green; coverage ≥80% on owned packages.
6. CI green (lint, vet, test, determinism harness).

### M3 — Time Management

Implements: **FR-TM-1..6, NFR-DET-1..2, NFR-PERF-3.**

- `rti/internal/time/`
  - `RegulationState` per federate: regulating flag + lookahead, constrained flag.
  - LBTS calculation: deterministic all-reduce across regulating federates; tie-break per `docs/AGENTS.md` §10.
  - NER request handling: enqueue, recompute LBTS, grant when condition met.
  - Wall-clock-free lookahead enforcement (use `core.Clock` interface).
  - Stall timeout: configurable per-federation, default 60 s; on fire, halt federation with diagnostic naming the stalled federate.
- `examples/go-timed/`: 3 federates with lookaheads {1.0, 2.0, 0.5}, NER advance over 100 logical ticks.

**M3 exit criteria**:

1. `examples/go-timed/` deterministic across 20 randomized scenarios (varying message timestamps within lookahead).
2. Stall test: kill one federate mid-run; timeout fires within 60 s ± 5 s; diagnostic identifies the killed federate.
3. Replay test: M3 event log replays byte-identical.
4. Coverage ≥80% on `rti/internal/time/`.

### M5 — End-to-end (you contribute)

You are responsible for:

- Hardening the gRPC handlers under cross-language load.
- Implementing `--mode=verbose` vs `--mode=best-effort` flag at federation create (per FR-OM-3, NFR-PERF-1..4).
- Recording perf baseline at sizes 2/5/25/100 (NFR-PERF-1, NFR-PERF-2, NFR-SCALE-2).

## 5. Verification Responsibilities (at OTHER agents' gates)

You do not just build — you also verify the other agents' work.

### At M1 gate (Agent B's milestone)

- Write 5 adversarial malformed FOMs that should be rejected. Examples to consider:
  - Missing required attribute on `HLAobjectRoot.HLAmanager.HLAfederate`.
  - Circular type reference in `dataTypes`.
  - Attribute with undeclared dataType.
  - Interaction class with non-existent parent.
  - Mixed-namespace XML (e.g. extra elements not in DIF).
- Run them against B's parser; file `verification:M1` issue listing pass/fail per case.
- Confirm `rti/pkg/encoding` is importable without panics from a minimal Go program.

### At M4 gate (Agent C's milestone)

- Run the live Go RTI from your M2/M3 work. Connect Agent C's pyjevsim example to it.
- Capture event log; replay it; confirm byte-identical.
- File `verification:M4` issue with replay diff (should be empty).

## 5.5 TDD Patterns for Your Domain

Read `docs/TDD.md` first. Below are domain-specific patterns for the components you own.

### Federation lifecycle
**Sequence tests** are your primary tool. Script command sequences and assert at each step:

```go
ops := []op{
    {Create, "demo", goodFOM, expectOK},
    {Join,   "demo", "alice", expectHandle(1)},
    {Join,   "demo", "bob",   expectHandle(2)},
    {Resign, "demo", 1,       expectOK},
    {Destroy,"demo",          expectErr(ErrFederationHasFederatesJoined)},
    {Resign, "demo", 2,       expectOK},
    {Destroy,"demo",          expectOK},
}
runOps(t, fedmgr, ops)
```

Concurrent-join determinism: post 50 join commands via 50 goroutines but with a deterministic input-order channel; assert handles 1..50 are assigned to a stable name order across 10 runs.

### Time management
- **LBTS as property test**: generate random regulating-set states; assert `lbts == min(currentTime[h] + lookahead[h])` for h in regulating; `+Inf` when empty.
- **NER grant as sequence test** with `FakeClock`: list of `(action, expected)` tuples, each tuple one assertion. Failures localize.
- **Stall detection**: inject `FakeClock`; advance past timeout; assert `FederationHalted{cause: stall, federate: H}` event appears.

### Object / interaction routing
Subscription matching tests use a fake `core.Outbox` that records what it received. Assert the recorded list matches the expected `(handle, attrs, ts)` tuples in deterministic order.

### gRPC handlers
Use small inline fakes of `core.FederationStore`, `core.TimeManager`, etc. — NOT mocking frameworks. Each handler test:
1. Happy path produces expected response.
2. Each documented error code is reachable from a defined input.
3. Idempotency where defined (e.g. resign of already-resigned federate).

Integration tests (`*_integration_test.go`, build tag `integration`) replace fakes with real implementations.

### Event log
- Round-trip property test: any sequence of events written then read produces identical events.
- Crash-mid-write: truncate file at record boundary → reader stops cleanly. Truncate mid-record → reader returns `io.ErrUnexpectedEOF` (no panic).
- Replay determinism: write log, replay through fresh RTI, assert second log byte-identical.

The orchestrator pre-writes specification tests for M2 and M3 under `tests/spec/M2/` and `tests/spec/M3/`. You cannot weaken them; you must make them pass. Your own unit tests cover detail; the spec tests cover the contract.

## 6. Spec Pointers (IEEE 1516)

For each module, the relevant standard sections:

- Federation Mgmt — IEEE 1516.1-2010 §4 (Federation Management Services).
- Declaration Mgmt — IEEE 1516.1-2010 §5.
- Object Mgmt — IEEE 1516.1-2010 §6.
- Time Mgmt — IEEE 1516.1-2010 §8 (especially §8.10–8.14 for NER, §8.16 for LBTS).
- Encoding Rules (you import; B implements) — IEEE 1516.2-2010 §4.

When the spec and Portico's behavior disagree, **trust the spec** and document the divergence in your PR.

## 7. Anti-Goals (Specific to You)

- Do not implement Ownership Mgmt, DDM, Save/Restore, or sync points in cut 1. SRS §9.
- Do not implement TAR in cut 1; only NER. (TAR comes in cut 2.)
- Do not add a "fast path" that bypasses the event log. The event log is on the critical path *by design* — it's the determinism guarantee.
- Do not introduce goroutine pools or worker patterns "for performance." Default to one goroutine per federate connection; revisit only if M5 baseline requires.
- Do not invent your own error codes. They live in `proto/rti/v1/errors.proto`.
- Do not edit `rti/pkg/fom/` or `rti/pkg/encoding/` even if you spot a bug. File an issue against Agent B.

## 8. When to Stop and Ask

- Any time the spec is ambiguous and Portico's behavior is the only guide.
- Any time you feel a contract change is needed.
- Any time the determinism harness fails on your branch and you can't explain why within 30 minutes.
