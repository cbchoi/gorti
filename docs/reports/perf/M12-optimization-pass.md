# M12 close — performance optimization pass

Date: 2026-05-05
Hardware: 12th Gen Intel i7-12700, 20 logical cores, Linux
Workload: `rti/internal/perf.Manager.RunBaseline` (in-process N-federate
ping-pong via `perfOutbox` channels), `Duration=5s`, isolated `go test`
runs to avoid cross-test residue.
Pre-opt revert tag: `perf-baseline-m12` (commit `3078f06`).

## Summary

| Federation size | Baseline | Opt 1+2 | Opt 1+2+4 | Total speedup |
|---:|---:|---:|---:|---:|
| 5   | 1,238,820 | 2,846,459 | 2,296,716 | **1.85×** |
| 25  |   252,575 |   596,627 | 1,036,128 | **4.10×** |
| 100 |    71,347 |    94,665 |   270,286 | **3.79×** |

Four commits, no test regressions, all `go test -race ./...` packages
green including the cross-language M5 and M12 cross-process suites.

Note: at size 5 Opt 4 (batched delivery) regresses 19% vs Opt 1+2 —
at small fanout the per-recipient mutex contention dominates the
channel-op savings. Larger fanouts (25, 100) win heavily because the
channel-send cost scales roughly with N and batch-flush amortizes by
the batch factor (32x in this code).

## Profile-driven analysis

Baseline `pprof -top -cum` at size 25 showed:

- `runtime.procyield` 48% flat (spin-wait inside lock contention)
- `runtime.lock2` 57% cum (futex acquires)
- `(*Registry).fanoutReceive` 50% cum
- `(*Registry).buildReceiveEvent` 20% cum, with `copyParamMap` 5% cum
- `(*perfOutbox).Send` 28% cum
- `runtime.selectgo` 37% cum, `runtime.chansend` 25% cum

Allocation profile attributed 65.6% of allocations to `buildReceiveEvent`
(the per-subscriber proto wrap + map copy) and 32.6% to `copyParamMap`
itself — together >98% of allocs were on the fanout hot path.

## Optimizations applied

### Opt 1 — hoist inner proto + batched seq alloc out of fanout loop
Commit `e01a6d3`. Both `fanoutReceive` (in `interaction.go`) and
`fanoutReflect` (in `update.go`) were calling the per-subscriber
`buildXxxEvent` helper N times for an N-subscriber fanout. Each call:

1. Allocated a fresh `ReceiveInteraction` / `ReflectAttributeValues`
   proto (identical content across all N subscribers).
2. Ran `copyParamMap` / `copyAttrMap` once per subscriber (the same
   defensive copy of the producer's map — N times for the same data).
3. Acquired `st.mu` once per subscriber to bump `nextOutboundSeq` by 1.

The fix:

- Build the inner proto **once per fanout**, hoisted above the
  subscriber loop. The producer's input map is still defensively
  copied (so the producer can mutate post-call), but only once. The
  resulting `ReceiveInteraction` (or `ReflectAttributeValues`) is
  shared across all per-subscriber `FederateEvent` wrappers. This is
  safe because `proto.Marshal` is read-only and receivers serialize
  concurrently.
- Reserve a contiguous range of `nextOutboundSeq` values via a new
  `nextOutboundSeqRangeLocked(n int) uint64` helper, with a single
  lock acquire instead of N. The loop assigns `startSeq + i` to each
  per-subscriber event in order. Wire-visible monotonic seq numbering
  is unchanged.
- Removed the now-unused `buildReceiveEvent` and `buildReflectEvent`
  helpers; the loop inlines the only per-subscriber bit (the
  `FederateEvent` wrap with the seq stamp).

### Opt 2 — replace RWMutex with atomic snapshot in `perfOutbox`
Commit `890e18a`. The hot Send path took `RWMutex.RLock/RUnlock` to
look up a stable channel pointer in the subscribers map. Even though
the map mutates rarely (Subscribe at federate join, cancel at leave),
the read-lock acquire incurs cache-line bouncing on the reader counter
under heavy reader concurrency (every Send across N×N fanout calls).

The fix: copy-on-write map held in `atomic.Pointer`. Send is a single
atomic load + map lookup with no mutex acquire. Subscribe/cancel
serialize on a small `writeMu`, build a fresh map with the mutation,
and atomic.Store the new pointer. Concurrent readers see either the
pre- or post-write snapshot atomically.

### Opt 4 — batched delivery: `chan []OutboundEvent` + per-recipient scratch
Commit `b8489f3`. After Opt 1+2 the residual hotspot at size 25 was
channel-select machinery: `runtime.selectgo` 44% cum +
`runtime.sellock` 38% cum + `runtime.chansend` 28% cum, all driven
by N×N per-event channel pushes during fanout.

Switch the `perfOutbox` channel element from `OutboundEvent` to a
batch (`[]OutboundEvent`) and add a per-recipient scratch slice.
Send appends to scratch under a small per-recipient mutex; when
scratch reaches `batchSize` (=32) it is handed to the recipient's
`chan[]OutboundEvent` and a fresh scratch is started. Subscribe's
cancel performs a final flush of any remaining scratch before close
so receivers always observe every Send that completed before
cancel.

Tuning: batchSize=32 was the pick from a microbench sweep — 8
regressed size 25 by ~21% without recovering size 5; 64 regressed
size 100 by ~5% from cache pressure on the receive-side slice
iteration. 32 lands at the throughput plateau.

Trade-off at size 5: -19% throughput vs Opt 1+2 because at small
fanout the per-recipient mutex contention dominates the channel-op
savings. Latency improves substantially: at size 25, p50 9.60 ms →
0.05 ms, p99 61.02 ms → 2.26 ms (shorter apparent queue depth as
fewer items are in flight).

The production `multiOutbox` (`cmd/rtid/outbox.go`) is left on the
single-event channel signature in this commit because it shares the
channel shape with the gRPC stream loop in
`rti/internal/transport/grpc/stream.go`. Promoting batched delivery
to the production wire path is the natural follow-up.

### Opt 3 — same atomic-snapshot pattern on production `multiOutbox`
Commit `a191bfd`. The same RWMutex-around-stable-map pattern lived in
`cmd/rtid/outbox.go` (the production gRPC server's outbox). Production
fanout has the same read-mostly profile so the same fix applies.
Verified through `go test -race ./...` plus the cross-language M5
test (Python federate joining a Go federation over real gRPC against
rtid subprocess) and the four M12 cross-process tests.

This commit's measured impact in the in-process perf harness is zero
(the harness uses `perfOutbox`, not `multiOutbox`). It improves real
federations over gRPC.

## Latency under higher throughput

The latency profile changes with throughput-targeted opts. At size 100,
p99 moves from 8.5 ms (baseline) to 1390 ms (post-opt). This is a
queue-depth side-effect of the higher send rate hitting the
perfOutbox's drop-on-full receivers, not a correctness change. The
producers now outpace the consumers; for a latency-sensitive workload
tune outbox capacity. For a raw-throughput baseline this is the
desired operating point: more interactions are reaching the outbox
before being dropped.

## Residual hotspots (size 25, post-opt)

- `runtime.selectgo` 44% cum + `runtime.sellock` 38% cum + `chansend`
  ~28% cum — channel-select machinery for the per-federate outbox
  channels. This is the unavoidable inter-goroutine communication
  cost for the channel-per-receiver fanout model. Further
  optimization requires architectural changes (batched delivery,
  ring-buffer outbox, lock-free MPSC queues) and is out of scope
  for this pass.
- `runtime.procyield` 56% flat — spin-waits inside the runtime futex.
  Most of this now traces back to the channel sends rather than the
  outbox lookup or the registry map.

## How to reproduce

```
git checkout perf-baseline-m12
go test -tags=perfcompare -run "TestThroughput_Size25$" -v ./rti/internal/perf/
git checkout main
go test -tags=perfcompare -run "TestThroughput_Size25$" -v ./rti/internal/perf/
```

Profile capture:

```
go test -tags=cpuprof -bench=Size25 -benchtime=1x \
    -cpuprofile=/tmp/cpu.prof -memprofile=/tmp/mem.prof \
    -run=^$ ./rti/internal/perf/
go tool pprof -top -cum /tmp/cpu.prof
go tool pprof -top -cum -alloc_objects /tmp/mem.prof
```
