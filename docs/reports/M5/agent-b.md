# Agent B M5 Status Report — Determinism Audit (TASK-083)

**Date**: 2026-05-03
**Author**: agent-b (codex-sandbox)
**Milestone**: M5 — End-to-end / verification
**Role at this gate**: Verifier (TASK-083 produces audit findings, no source code)

---

## Methodology

Audit covered every top-level directory called out by `docs/M5_DISPATCH_PLAN.md` §3 (W1B) and the TASK-083 brief:

- `proto/` — declarative; N/A.
- `rti/` — Go server, including `cmd/rtid`, `internal/{federation,declaration,object,time,eventlog,transport,core}`, `pkg/{fom,encoding}`.
- `pysdk/` — Python SDK, including `rti1516e/{encoding,fom}`, `pyjevsim_bridge/`, `tests/spec/`.
- `examples/` — `go-pingpong/`, `go-timed/`, `pyjevsim/`.
- `tests/` — fixtures only; out of scope per brief.

Patterns probed (with the literal `git grep` invocations used):

| Pattern | Command |
|---|---|
| Wall-clock reads (Go) | `git grep -n "time.Now()" rti/ examples/` |
| Wall-clock reads (Python) | `git grep -n "time\.time\|time\.monotonic\|datetime\.now\|datetime\.utcnow" pysdk/ examples/` |
| RNG construction (Go) | `git grep -n "math/rand\|crypto/rand" rti/`; then `git grep -nE "rand\.New\|rand\.Seed\|NewSource" rti/` |
| RNG (Python) | `git grep -nE "import random\|from random" pysdk/ examples/` |
| Map iteration (Go) | `git grep -nE "for [a-zA-Z_]+, [a-zA-Z_]+ := range " rti/ \| grep -v _test.go` |
| Dict iteration (Python) | `git grep -nE "for [a-zA-Z_]+, [a-zA-Z_]+ in [^:]+\.items\(\)" pysdk/ examples/` |
| Set ordering (Python) | `git grep -nE "set\(\)\|frozenset\(" pysdk/ examples/` |
| Multi-channel selects (Go) | `git grep -nE "^\s*case <-" rti/ \| grep -v _test.go` |
| Goroutines outside tests (Go) | `git grep -nE "go func\\(" rti/ \| grep -v _test.go` |
| asyncio task ordering (Python) | `git grep -nE "asyncio\.gather\|asyncio\.wait\|asyncio\.create_task\|asyncio\.ensure_future" pysdk/ examples/` |

Time spent: ~45 minutes focused grep + sense-checking each hit. Triaged ~80 raw map-iteration hits down to two genuine action items by reading each call site and asking "is the iteration order observable?" — the codebase has been highly disciplined and most map iterations are followed by `slices.Sort` / `sort.Slice` / `sortedHandles(...)` per D-2.

---

## Findings

### Critical (block M5 gate)

(none — see Conclusion)

### Major (file as bug; defer fix to post-MVP if not blocking)

(none — see Conclusion)

### Minor / informational (filed, deferred to post-MVP)

- **#2** `pysdk/pyjevsim_bridge/time_advance.py:254` — `for port_name, payload in outputs.items()` over a CPython dict whose iteration order drives the wire-visible ordering of `send_interaction` calls. Insertion-order is preserved in CPython 3.7+ so the current `Producer` (single-port) is deterministic in practice; latent risk for any future user `CoupledModel` that returns multi-port output. **Recommended fix**: `for port_name in sorted(outputs):`. Also worth adding a Python-side D-2 entry to `docs/CODING_CONVENTIONS.md` §3.
- **#3** `rti/internal/eventlog/multiplex.go:223` — `for fed, w := range m.writers` in `Close()` selects the "first error" non-deterministically when multiple per-federation writers fail. Correctness unaffected (all writers still close); only the returned error message is non-deterministic. **Recommended fix**: sort federation names before the close loop.

### Reviewed-and-cleared (no action)

These all came up in grep but were verified compliant or out of scope:

- **`rti/internal/time/ner.go:128,302` and `rti/internal/time/stall.go:58`** — map iteration immediately followed by `sort.Slice` / `sortedHandles`; explicit D-2 comments at the call sites.
- **`rti/internal/declaration/manager.go`** — every `Subscribers/PublishersFor` query funnels through `sortedHandles(...)`. Comment trail at lines 44, 281, 297, 314 explicitly documents the contract.
- **`rti/internal/federation/manager.go:351`** — `List()` materializes federation names then `sort.Strings`.
- **`rti/internal/object/update.go:72`** — `for attr := range attrs` is used only for an all-or-nothing ownership boolean check; iteration order does not affect the result.
- **`rti/internal/object/update.go:138`, `rti/internal/object/interaction.go:88`, `rti/internal/eventlog/replayer.go:335,361`, `rti/internal/transport/grpc/object.go:52,74`** — all are map-to-map defensive copies; output is itself a map, so iteration order does not affect downstream consumers.
- **`rti/pkg/encoding/variant_record.go:35,45`** — boundary computation (max, order-invariant) and defensive map copy.
- **`rti/pkg/encoding/fixed_record.go:64,85` and `rti/pkg/fom/parser/cycle.go`** — iterate slices/sorted-key lists, not maps.
- **`rti/internal/core/clock.go:22`** — sole `time.Now()` call in production Go code, inside the `RealClock.Now()` adapter (the documented interface boundary). `forbidigo` permits this file.
- **`rti/internal/declaration/manager_concurrent_test.go`, `rti/internal/declaration/manager_test.go`, `rti/internal/federation/manager_test.go`, `rti/spec/M3/determinism_test.go`** — every `math/rand` use is `rand.New(rand.NewSource(<explicit seed>))`; never the global. D-5 compliant.
- **`examples/go-pingpong/main.go:90`, `examples/go-timed/main.go:91`** — `time.Now()` is used only for human-readable elapsed-time stats; the example forwards `--deterministic` to `rtid` which selects `FakeClock` for the eventlog. Acceptable; not in any state-affecting path.
- **Selects in `rti/cmd/rtid/{main,pingpong}.go` and `rti/internal/transport/grpc/stream.go`** — every multi-arm `select` pairs a single data channel with a single context-cancellation arm. Both arms are mutually exclusive in semantics (event vs. shutdown); not a D-3 violation.
- **Goroutines in `rti/cmd/rtid/main.go:423,424`** — server.Serve loops; lifecycle only, no observable output ordering. `rti/cmd/rtid/pingpong.go:211` — the pong worker reads from a single channel; the data ordering it produces is a function of the channel, not the goroutine.
- **`pysdk/rti1516e/encoding/fixed_record.py`, `variant_record.py`** — `fields` is a `list[tuple[str, Codec]]` (declaration-ordered); `variants` is a dict but only `.values()` is reduced (max boundary) or accessed by key. No order-sensitive iteration.
- **`pysdk/rti1516e/fom/parser.py`** — every `set` use is membership tracking (`seen`, `visited`, `reported`); the only iteration over the set on line 566 is `sorted(parents.get(...))`. D-2 equivalent satisfied.
- **`pysdk/pyjevsim_bridge/port_mapping.py:45`** — iterates a user-passed mapping dict but writes results into two output dicts that are subsequently consumed only via `.get()`; iteration order is not observable.
- **`pysdk/tests/test_standard_ambassador.py:121`** — `import time as _time` and `time.monotonic()` are inside test code, used as a deadline poll; not in production path.
- **`examples/pyjevsim/runner.py`** — fan-out cursor walks `server.calls` (a list, FIFO); subscribers are an explicit list. The determinism witness in `determinism_test.py` hashes a tuple of (list, list, int), all insertion-ordered. Verified: the M4 `test_determinism_10x_same_seed` already exercises this and passes.
- **No `asyncio.gather` / `create_task` / `ensure_future` calls** in production `pysdk/` or `examples/` outside the server-loop lifecycle in `pysdk/rti1516e/standard.py`. Task-execution-order non-determinism is therefore not on the table.

---

## Issues filed

Two issues filed via `gh issue create` against `github.com/cbchoi/gorti`:

- https://github.com/cbchoi/gorti/issues/2 — bug(M5-audit): pysdk bridge iterates output_handler() dict without sorting
- https://github.com/cbchoi/gorti/issues/3 — bug(M5-audit): MultiplexWriter.Close map-iter makes 'first error' non-deterministic

Both are labeled `bug`. The `verification:M5` and `determinism` label additions failed (the agent's PAT does not have `addLabelsToLabelable` permission); the orchestrator should add those labels post-merge.

---

## Conclusion

**M5 gate: NOT BLOCKED on determinism grounds.** Zero critical findings, zero major findings, two minor findings already filed and recommended for post-MVP fix. The codebase has been disciplined: every observable map iteration in the Go core path is preceded or followed by an explicit sort, every RNG is explicitly seeded, the only `time.Now()` in production lives in `RealClock.Now()` which is the documented D-1 boundary, and selects are limited to the data/cancellation pattern. The two findings are real but small: one is a CPython-implementation-detail dependency in the bridge (latent until a multi-port user model appears), and one is a non-deterministic error-selection in `MultiplexWriter.Close` whose only observable effect is which error message wins when several writers fail simultaneously. Recommend the orchestrator advance the M5 gate and dispatch fixes for #2 and #3 as low-priority follow-ups.
