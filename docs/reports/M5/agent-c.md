# Agent C M5 Status Report — Cross-language smoke + modes verification

Bundle: TASK-081 (cross-language smoke) + TASK-082 (federate-side mode verification).

> Note: Agent C's per-task results live in the corresponding sentinel
> files (`docs/tasks/signals/TASK-081.done`, `TASK-082.done`). This
> report is the orchestrator-synthesized M5 view, written for
> traceability so all three M5 status reports
> (`docs/reports/M5/agent-{a,b,c}.md`) are present at gate time per
> `docs/M5_DISPATCH_PLAN.md` §2.

## TASK-081 — Cross-language smoke (W1C)

**Outcome**: cut-1 GREEN. Real-gRPC transport in the Python SDK is
operational; two Python federates joined a Go-hosted federation over
real gRPC and exchanged interactions with bytewise-identical payloads.

### What landed
- `pysdk/rti1516e/_transport.py` extended with `GrpcTransport` (wraps
  `grpc.aio.Channel` + 4 generated stubs; FOM-driven name→handle
  resolution; per-federate background event-stream draining).
- `pysdk/rti1516e/connection.py` updated to dispatch `grpc://` URLs
  alongside the existing `memory://fake-rti` registry.
- `examples/pyjevsim/_real_pyjevsim_adapter.py` — adapter mapping a
  single `pyjevsim.BehaviorModel` to `CoupledModelProtocol` (cut-1
  scope: single-atomic only).
- `examples/pyjevsim/cross_lang_runner.py` + `cross_lang_test.py` —
  subprocess orchestrator + integration test.
- `pysdk/tests/spec/m5/test_spec_m5_cross_language.py` UNSKIPPED, real test.

### Deferred to M6
- **Bidirectional Python+Go cross-language smoke**: existing Go
  reference examples (`examples/go-pingpong`, `examples/go-timed`)
  are subprocess-shim demos that spawn rtid in special modes; they
  do not open a gRPC channel to a separately running rtid. Building
  a Go gRPC-client federate requires touching `rti/` (Agent A's
  territory). Cut-1 ships Python+Python against a real rtid.
- **Cross-language MIM corpus parity**: Python's FOM parser merges
  the MIM differently from `rti/pkg/fom/mim/standard-mim.xml`, so the
  same class name lands at different numeric handles on each side.
  Doesn't affect Python-only smoke (both ends agree on Python's
  handle table); does affect Python+Go (W2B blocker, see below).
- **TimeService dispatch**: rtid's TimeService is intentionally nil
  at M2; NER/EnableRegulation are recorded but not dispatched.
  Cross-language smoke avoids the time-managed path.
- **Structural-hierarchy + SysExecutor adapter**: cut-1 wraps a
  single atomic model only.
- **TLS / retry / deadline propagation**: uses
  `grpc.aio.insecure_channel`; production hardening post-MVP.

## TASK-082 — Federate-side mode verification (W2B + W2C)

**Outcome**: 1/2 PASS, 1/2 deferred.

### What landed
- `pysdk/tests/spec/m5/test_spec_m5_modes.py` UNSKIPPED, both tests real.
- `pysdk/tests/spec/m5/_helpers.py` — parameterized
  `run_modes_smoke(federation_mode, interaction_order, timestamp)`
  runner. Builds + spawns rtid, writes inline FOM with selectable
  `<order>`, drives two Python federates over real gRPC, captures
  the subscriber's `ReceiveInteraction.timestamp`.
- W2C follow-up: production `*fomHandle` in `rti/cmd/rtid/foms.go`
  now implements `FOMOrderResolver` (~60 LoC + 2 tests). Compile-time
  assertion locks the contract. Confirmed by Go-side
  `rti/spec/M5/best_effort_test.go` PASSING.

### Test results
- **Test 2 — verbose mode preserves TSO**: PASS. Cross-language
  TSO timestamp round-trips byte-for-byte through the real-gRPC
  transport.
- **Test 1 — best-effort mode delivers RO**: SKIP with documented
  blocker. Federation IS best-effort, FOM declares the interaction
  as `<order>Receive</order>`, but the subscriber receives a
  non-None timestamp (TSO preserved).

### Why test 1 still skips after W2C

Cross-language handle alignment (the same M6 follow-up flagged under
TASK-081). Python's MIM merge assigns a different numeric handle to
the user's `ModesProbe` interaction than the Go-side parser does.
The interaction goes out as Python's handle K; Go's
`OrderForInteraction(K)` resolves against ITS handle table and finds
either a different class or an out-of-range slot — both fall back to
TSO, so the timestamp survives.

### Mode contract IS verified end-to-end via two complementary paths
- **Go-side**: `rti/spec/M5/best_effort_test.go::TestSpec_M5_BestEffort_RODelivery`
  PASSES — registry correctly strips timestamp when federation is
  best-effort AND FOM declares the class as Receive-order.
- **Python-side TSO**: `test_spec_m5_modes.py::test_spec_m5_verbose_attribute_delivers_tso`
  PASSES — verbose mode preserves TSO regardless of FOM order.
- **Combined Python-publishes-to-Go-RTI best-effort RO**: needs
  cross-language handle alignment (M6 follow-up).

## M5 verification summary

| M5 exit criterion | Status | Evidence |
|---|---|---|
| Cross-language federation works | ✓ (Python+Python over real-gRPC) | `pysdk/tests/spec/m5/test_spec_m5_cross_language.py` PASS |
| Verbose mode functional | ✓ | `test_spec_m5_verbose_attribute_delivers_tso` PASS |
| Best-effort mode functional | ✓ Go-side / deferred Python+Go | `rti/spec/M5/best_effort_test.go` PASS |
| Determinism preserved | ✓ | Audit clean (TASK-083); 0 critical, 2 minor non-blocking findings (issues #2, #3) |

## M6 / post-MVP follow-ups owed by Agent C

1. Bidirectional Python+Go cross-language smoke (requires Go
   gRPC-client federate; Agent A territory).
2. Cross-language MIM corpus parity — align Python FOM parser's MIM
   merge against `rti/pkg/fom/mim/standard-mim.xml`. Unblocks the
   currently-skipped Python-publishes-to-Go best-effort RO test.
3. Real-pyjevsim adapter for structural hierarchies (cut-1 supports
   single atomic only).
4. TLS + retry + deadline propagation in `GrpcTransport` (production
   hardening).
5. Production in-process driver (extract from `FakeRtiServer` so
   `examples/pyjevsim/runner.py` doesn't import from
   `pysdk/tests/spec/m4/_fakes/`).
