# Agent A M5 Status Report — Perf Baseline (TASK-080)

Bundle: TASK-078 (gRPC handler soak) + TASK-079 (perf harness) + TASK-080 (perf run + report).

## Methodology

**Harness** (`rti/internal/perf/baseline.go`, TASK-079):

- In-process runtime: `federation.Manager` + `declaration.Manager` +
  `object.Registry` + a multi-federate buffered outbox (`perfOutbox`,
  bufferSize 8192). The event log is INTENTIONALLY OMITTED in
  measurement mode — the underlying `eventlog.Writer` has no internal
  mutex on `nextSeq` (it relies on the federation manager being the
  sole serializing caller in production), and a tight-loop perf
  workload trips the race detector. Both `federation.Manager` and
  `object.Registry` accept `EventLog: nil` per their cut-1 relaxation.
  Event-log behavior remains covered by the standard M2/M3 spec
  tests + the soak harness.
- Workload: every federate publishes AND subscribes to a single
  `InteractionClassHandle = 1`. Each federate runs a sender goroutine
  that loops `objReg.SendInteraction` with an 8-byte parameter
  carrying the send-time `int64` nanos. Each federate also runs a
  receiver goroutine that drains its outbox channel and records
  `now - sendNs` for every received event.
- Latency = end-to-end "send call returns -> peer's receiver pops the
  event off its outbox channel and decodes the embedded send-time
  nanos". Throughput = `total SendInteraction returns / wall-clock
  elapsed`.
- Outbox overflow contract: drop on full receiver channel rather than
  crash. Drops show up implicitly as a smaller delivered-sample count
  vs. `InteractionsSent`. Visible in size 5 numbers below.

**Runner**: `rti/cmd/perf-baseline` (build tag `perf`). The
orchestrator dispatch named `examples/go-pingpong/perf_main.go`, but
the Go internal-package rule blocks `examples/*` from importing
`rti/internal/perf`. The runner therefore lives under `rti/cmd/...`
where the import is legal; a thin shim at
`examples/go-pingpong/perf_main.go` (build tag `perf`) `exec`s the
real binary so the documented invocation path still works.

**Run conditions** (this report):

- Duration per size: 10s.
- Sizes: 2, 5, 25, 100 (all four — no deferral).
- Host: 12th Gen Intel Core i7-12700, 20 logical cores, Linux 6.17,
  Go 1.22.
- Build: `go build -tags=perf -o /tmp/perf-baseline
  ./rti/cmd/perf-baseline`.
- Invocation: `/tmp/perf-baseline -duration=10s >
  docs/reports/M5/perf-baseline.json`.

## Results

| Size | Sent | Throughput (i/s) | p50 (ms) | p99 (ms) | Notes |
|---:|---:|---:|---:|---:|---|
| 2   | 30,657,535 |   3,035,333 | 0.044 |  0.125 | sender↔receiver, no fanout amplification |
| 5   | 12,931,607 |   1,253,641 | 10.16 | 15.04  | overflow regime: senders saturate the 8192 outbox; queue dwell-time dominates |
| 25  |  1,927,545 |     184,191 |  2.90 | 23.22  | lock contention dominates (see profile below) |
| 100 |    512,584 |      48,863 |  3.22 | 33.81  | per-federate ~489 i/s |

Raw JSON: [perf-baseline.json](./perf-baseline.json).

All sizes satisfy the spec invariants (`schema_version=1`,
`duration_seconds>0`, `interactions_sent>0`, `throughput_per_second>0`,
`p99 >= p50 >= 0`).

## Observations

- **Throughput peak at size 2** (3.0 M i/s). The expected single
  receiver path; latency p99 stays under 130 µs. As fanout grows the
  per-call work grows linearly (one `Outbox.Send` per subscriber)
  AND lock contention on the per-federate state Mutex shows up.
- **Size 5 is the queue-saturation knee.** Throughput per receiver
  (~250k events/s = 1.25M sent × 4 fanout / 5 receivers) exceeds
  what the receiver goroutine can drain through `extractSendNanos +
  channel pop + slice-append`. Outbox dwell time pushes p50 to
  10ms — this is queueing latency, not RTI fanout latency. A more
  aggressive overflow drop (smaller bufferSize) would trade
  throughput for latency; the 8192 buffer is the bias toward
  throughput.
- **Size 25 throughput collapse (~7×)**: as fanout grows, lock
  contention on the registry's per-federation state Mutex dominates.
  Profile (next section) confirms `runtime.lock2` /
  `runtime.procyield` are >50% of CPU.
- **Size 100 stays at ~49k i/s with p50 ~3ms.** 100 federates each
  doing ~490 i/s comfortably meets the SRS NFR-PERF-1 budget
  (federate-to-federate sub-10ms p50 at 100 federates — we measure
  3.2ms p50, ~3× under).
- **No regressions vs M3**: M3 didn't establish a numeric baseline;
  this is the first reproducible measurement.
- **TASK-078 soak confirmation**: 5-second smoke against the real
  gRPC server with 5 federate goroutines exchanging mixed RPCs:
  245,828 total calls, 0 codes.Unknown, 0 panics, goroutine delta 0.
  Run: `SOAK_DURATION=5s go test -tags=soak ./rti/internal/transport/grpc/
  -run TestSoak -v`. Production-grade 10-minute run available via
  `SOAK_DURATION=10m`.

## TASK-084 decision input

CPU profile captured via `rti/internal/perf/cpu_bench_test.go` (build
tag `cpuprof`), 5s benchmark each at sizes 25 and 100, on the same
host:

```
go test -tags=cpuprof -bench=Size25  -benchtime=1x -cpuprofile=/tmp/cpu25.out  -run=^$ ./rti/internal/perf/
go test -tags=cpuprof -bench=Size100 -benchtime=1x -cpuprofile=/tmp/cpu100.out -run=^$ ./rti/internal/perf/
go tool pprof -list 'binary\.' /tmp/cpu25.out
```

| Size | Encoding share (encoding/binary, % of total CPU) |
|---:|---:|
| 25  | 0.21% (`bigEndian.Uint64` 0.20% + `PutUint64` 0.014%)  |
| 100 | 0.11% (`bigEndian.Uint64` 0.11%; PutUint64 below 1ms threshold) |

Both well under the trigger thresholds (>5% at size 25 OR >10% at
size 100). The hot path at both sizes is dominated by:

1. `runtime.lock2` / `runtime.procyield` (channel and select-case
   contention): ~50% of total CPU.
2. `runtime.selectgo` / `runtime.sellock` (channel select machinery
   in `chansend` / `selectnbsend`): ~30%.
3. `runtime.mallocgc` (per-event allocation: `outboundEvent`,
   `copyParamMap`, `*rtiv1.FederateEvent`): ~5-8%.
4. `sort.Slice` (latency-percentile post-processing in the harness
   itself, NOT a steady-state cost): ~3-4%.

**Recommendation: TASK-084 should be CANCELLED.**

Architecture supports the data: Go encoders in `rti/pkg/encoding/`
are mostly inlined `binary.BigEndian.Put*` / proto-wire `AppendVarint`
calls — no heap allocs in the hot path, no codec dispatch overhead,
no reflection. Optimizing them would yield <1% global throughput at
size 25/100. The dominant cost is the channel-fanout machinery
(send N times -> select read N times); that's not encoding work.

The cpu_bench_test.go file is left in-tree (build-tag-gated, no impact
on default test runs) so Agent B / future contributors can re-validate
in one command if architecture changes invalidate the conclusion.

## Files shipped

- `rti/internal/perf/baseline.go` (extended; FROZEN schema preserved)
- `rti/internal/perf/baseline_test.go` (NEW unit tests)
- `rti/internal/perf/events.go` (NEW; rtiv1.FederateEvent type alias)
- `rti/internal/perf/foms.go` (NEW; in-process permissive FOM repo)
- `rti/internal/perf/outbox.go` (NEW; multi-federate measurement outbox)
- `rti/internal/perf/cpu_bench_test.go` (NEW; build tag `cpuprof`;
  TASK-084 re-validation helper)
- `rti/internal/transport/grpc/load_test.go` (NEW; build tag `soak`)
- `rti/spec/M5/soak_test.go` (UNSKIPPED; orchestrator-authorized)
- `rti/cmd/perf-baseline/main.go` (NEW; build tag `perf`; the
  TASK-080 binary)
- `examples/go-pingpong/perf_main.go` (NEW; build tag `perf`; thin
  shim that exec's the real binary)
- `docs/reports/M5/agent-a.md` (this file)
- `docs/reports/M5/perf-baseline.json` (raw output of the run)

## Test status

- `go test ./rti/spec/M5/... -run TestSpec_M5_Perf` — 2/2 GREEN.
- `SOAK_DURATION=5s go test -tags=soak
  ./rti/internal/transport/grpc/... -run TestSoak -timeout 60s` —
  PASS (245k calls, 0 unknowns, delta 0).
- `SOAK_DURATION=2s go test -tags=soak ./rti/spec/M5/... -run
  TestSpec_M5_Soak` — PASS (2.1M calls, delta 0).
- `go test -race ./rti/internal/...` — clean.
- `go build -tags=perf ./rti/cmd/perf-baseline` — clean.
- `go build -tags=perf ./examples/go-pingpong` — clean.
- All M0–M4 + Wave 1 tests still GREEN.

## Pre-existing finding (non-blocking, FYI)

While building the perf harness I discovered that
`rti/internal/eventlog.Writer.Append` has no internal mutex on
`nextSeq` — it relies on its caller (the federation manager / object
registry under normal cut-1 wiring) being the sole serializing
goroutine per federation. The race detector trips when concurrent
federates within ONE federation drive `SendInteraction` in parallel
(which the perf harness does deliberately for measurement). In
production, multiple goroutines serving the same federation's gRPC
handlers WILL hit the same race; today's tests don't exercise that
combination because the soak harness uses one federation per worker
(see `rti/internal/transport/grpc/load_test.go`'s soakFederate
design — `fedName := "soak-fed-%d"`). The perf harness sidesteps
the bug by passing `EventLog: nil`. A future task can add a Mutex
around `Writer.nextSeq` + the Sink.Write call; this is a one-line
fix but is OUT OF SCOPE for M5 W2A and the orchestrator should
schedule it for M6 or as a follow-up under the `eventlog` brief.
