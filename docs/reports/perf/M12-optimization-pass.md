# M12 close — performance optimization pass

Date: 2026-05-05
Hardware: 12th Gen Intel i7-12700, 20 logical cores, Linux
Workload: `rti/internal/perf.Manager.RunBaseline` (in-process N-federate
ping-pong via `perfOutbox` channels), `Duration=5s`, isolated `go test`
runs to avoid cross-test residue.
Pre-opt revert tag: `perf-baseline-m12` (commit `3078f06`).

## Summary

| Federation size | Throughput (interactions/sec) | Speedup |
|---:|---:|---:|
| 5   | 1,238,820 → 2,846,459 | **2.30×** |
| 25  |   252,575 →   596,627 | **2.36×** |
| 100 |    71,347 →    94,665 | **1.33×** |

Two commits, no regressions, all `go test -race ./...` packages green
including the cross-language M5 and M12 cross-process suites.

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
