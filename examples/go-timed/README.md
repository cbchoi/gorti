# go-timed example -- cross-process

Three Go federates with different lookaheads run TAR (TimeAdvanceRequest)
cycles against a real `rtid` daemon. Each cycle issues an advance,
waits for the matching `TimeAdvanceGrant` on the federate's
`StreamService.Events` stream, then emits a `Tick` interaction at the
earliest valid TSO time (`grant + lookahead`). Returned to life in M21
after the deletion in cut-3 —
the wire path needed `TimeService` wired (M21 W2A) and grant emission
on the wire (W2B).

```text
   +--------+   +----------+   +--------+
   |  fast  |   |  normal  |   |  slow  |
   | la=0.5 |   |  la=1.0  |   | la=2.0 |
   +---+----+   +----+-----+   +----+---+
       |             |              |
       +-------------+--------------+
                     |
              grpc://127.0.0.1:8442
                     |
                     v
              +-------------+
              |    rtid     |
              | TimeService |
              +-------------+
```

All three federates use **TAR** (TimeAdvanceRequest) rather than the
mixed NER/TAR sketch in the M21 plan §2.8. TAR clears `pendingNER` on
every grant; NER's "sole-pending forced grant + KEEP pending" path
makes the cycle loop racy when peers haven't all issued their requests
yet. Per-primitive boundary semantics — including NER's strict gate
vs NMRA's inclusive gate — are tested at the manager level in
`rti/internal/transport/grpc/time_test.go` (TASK-203 cases 8a-f).

## Prerequisites

```bash
# From the repo root, builds rtid + the go-timed federate binary.
make build
go build -o bin/go-timed ./examples/go-timed
```

## Run it -- orchestrated (go test)

```bash
go test -timeout 60s ./examples/go-timed
```

The `runner_test.go` test spawns rtid + 3 federate subprocesses on
free ports, waits for all to write their result JSON, then verifies
the M21 acceptance invariants (TASK-211).

Default-config sample output:

```text
ok  github.com/cbchoi/gorti/examples/go-timed  0.31s
```

## Run it -- manually (shell scripts)

For when you want each federate in its own terminal so you can watch
its stdout, kill it, restart it, etc.

```bash
cd examples/go-timed

# Terminal 1
./rtid_run.sh

# Terminals 2, 3, 4 — order doesn't matter (federates self-coordinate
# via the federation's join + regulation handshake)
./fast_run.sh
./normal_run.sh
./slow_run.sh

# Terminal 5 — after all three federates exit
./verify_run.sh
```

Each federate writes its result JSON to
`/tmp/go-timed/{fast,normal,slow}-result.json`. Override
`RESULT_DIR=/path/to/dir` to redirect.

Tunables (env-overridable; see `_run_common.sh`):

| var | default | meaning |
|---|---|---|
| `CYCLES` | 10 | number of advance cycles per federate |
| `TICK_STEP` | 3.0 | logical-time advance per cycle (must exceed slow's lookahead 2.0) |
| `PRIMITIVE` | TAR | advance primitive (TAR works robustly; NER works for single-federate setups) |

## Verifier output

```text
verify_run: federates=3 cycles=10
  fast (la=0.5, TAR): grants=[1.5, 3, 5, 6, 8, 9, 11.5, 13, 14.5, 16]
  normal (la=1.0, TAR): grants=[0.5, 2, 5, 6.5, 8, 8, 9.5, 11.5, 12, 13.5]
  slow (la=2.0, TAR): grants=[0.5, 1.5, 3, 6, 8.5, 8.5, 8.5, 9, 9.5, 12]
verify_run: each federate got 10 grants               : PASS
verify_run: per-federate grants non-decreasing        : PASS
verify_run: per-cycle min grant non-decreasing (LBTS-ish) : PASS
```

Grant times overlap because TAR's incremental-grant path emits at
LBTS (federation-wide) when LBTS doesn't strictly exceed the
federate's currentTime. That's correct HLA behavior — the federation
hasn't advanced enough for that federate's request to be fully
satisfied yet, but the partial grant lets it catch up to peers.

## What's deferred

  - **NER + per-primitive boundary semantics in this example.** The
    plan's §2.8 sketch had fast/slow on NER and normal on TAR. NER's
    forced-grant + KEEP-pending path races the cycle loop. The
    boundary semantics live at the manager level (TASK-203). For an
    example demonstrating NER + multiple cycles, the federate would
    need to track currentTime locally (via QueryLogicalTime) and
    only issue a new NER once currentTime reaches the previous
    requested time. That's a future-cut SDK ergonomics improvement.
  - **Cross-process strict-monotonic grant times.** The plan's
    TASK-211.3 asked for strict monotonicity; the manager guarantees
    only non-decreasing across cycles when LBTS doesn't advance.
    Strict-monotonic would require a different cycle pattern that
    forces LBTS-progress every iteration.
  - **Multi-host topology.** This example is single-host. For
    multi-host, change `RTID_URL` and ensure rtid is reachable from
    each federate's host.
