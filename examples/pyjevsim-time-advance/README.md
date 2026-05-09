# pyjevsim-time-advance — cross-process

Three Python federates (fast / normal / slow) with different
lookaheads run NER (NextMessageRequest) cycles against a real `rtid`
daemon. Restored to life in M21 — was deleted in cut-3 because
pysdk's NER was a no-op (TimeService was nil server-side); M21 W2A
wired the gRPC handler, M21 W2B closed the grant-on-the-wire gap,
and M21 W3B flipped pysdk's NER dispatcher from no-op to real.

## Prerequisites

Same as `examples/pyjevsim-relay-cross-process/` — see its
[README](../pyjevsim-relay-cross-process/README.md#prerequisites)
for the full setup. From repo root:

```bash
pip install -e './pysdk[dev]'
make py-codegen
```

## Run it -- orchestrated (runner.py)

```bash
python3 examples/pyjevsim-time-advance/runner.py
```

Sample output:

```text
runner: cycles=10 per-federate-grants={fast=10 normal=10 slow=10} verify=ok
```

## Run it -- manually (shell scripts)

5 terminals: rtid, three federates, then verify.

```bash
cd examples/pyjevsim-time-advance

./rtid_run.sh          # Terminal 1
./fast_run.sh          # Terminal 2
./normal_run.sh        # Terminal 3
./slow_run.sh          # Terminal 4
./verify_run.sh        # Terminal 5 — after federates exit
```

## Notes on the cycle pattern

Pysdk currently exposes only **NER** (TASK-208's flip-from-no-op
covered the 3 methods that connection.py already binds). NER's
"sole-pending forced grant + KEEP-pending" semantics make multi-
federate cycle loops racy: a federate's first NER lands sole-pending,
gets a forced grant at LBTS, but `pendingNER` stays true server-side.
The next cycle's NER then races with the manager's state-mutation
and can hit `ErrTimeAdvancingState`.

The regulator's loop **retries** on `TimeAdvancingState` with
exponential backoff (`regulator_main.py`); within a few attempts
the state-mutation completes and the new NER takes hold. The Go-side
counterpart at `examples/go-timed/` sidesteps this entirely by using
**TAR** (which clears pending on every grant) — pysdk doesn't yet
expose TAR. Adding the other 4 advance primitives + queries to
pysdk/connection.py is post-M21 follow-up.

## What's deferred

  - **TAR/TARA/NMRA/FQR Python bindings.** Available in the Go SDK
    (W3A) but not yet in pysdk's connection.py. M21 W3B's scope was
    "flip the existing python methods from no-op to real" — that
    covered the 3 methods bound today; the other 10 are mechanical
    additions and tracked as a post-M21 follow-up.
  - **Strict-monotonic grant times.** As in the Go example, the
    manager only guarantees non-decreasing across cycles when LBTS
    doesn't strictly advance.
