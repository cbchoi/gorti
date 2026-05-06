# pyjevsim relay example -- cross-process

The same Generator -> Buffer -> Processor pipeline as
[`examples/pyjevsim-relay`](../pyjevsim-relay/README.md), but every
piece runs in its own process: `rtid` is a real subprocess with a real
gRPC listener, and each of the three federates is a separate Python
process talking to `rtid` over `grpc://127.0.0.1:<port>`.

```text
   +---------+  GenToBuffer  +--------+  BufferToProc  +-----------+
   | gen.py  | ============> | buf.py | =============> | proc.py   |
   +---------+               +--------+                +-----------+
        |                       |                          |
        |    grpc://127.0.0.1:N (port chosen at runtime)    |
        +-----------------+-----+--------+------------------+
                          |              |
                          v              v
                       +------------------+
                       | rtid (subprocess)|
                       +------------------+
```

This is the deployment shape that ships to production. The in-process
example is faster to iterate on and pedagogically clean for studying
DEVS<->HLA semantics; this one is closer to what an operator's
runbook will look like.

## When to pick which

| Scenario | Use this | Use `examples/pyjevsim-relay` |
|----------|----------|------------------------------|
| Learning DEVS<->HLA mapping | | yes |
| Debugging coupled-model state machine | | yes (one process, full PDB access) |
| Smoke-testing the gRPC wire format | yes | |
| Reproducing a production deployment topology | yes | |
| Practicing operator runbook (start/kill/inspect rtid) | yes | |
| Studying drop counts deterministically | | yes (in-process fan-out is lockstep) |

## Run it

```bash
# From the repo root
python3 examples/pyjevsim-relay-cross-process/runner.py

# Knobs (defaults shown)
python3 examples/pyjevsim-relay-cross-process/runner.py \
    --gen-messages 50 \
    --capacity 5 \
    --service-period 2 \
    --drain-ticks 30 \
    --tick-period 0.05 \
    --keep-tempdir          # leave logs + result JSON for inspection
```

Default-config sample output:

```text
runner: published=50  forwarded=29  dropped=21  received=29  residual=0  rtid_port=38719  verify=ok
```

Counts vary slightly across runs because cross-process timing is
*not* deterministic (see "Why drop counts drift" below). The
verification asserts the conservation law:

1. `published == forwarded ∪ dropped ∪ residual` -- every seq is
   accounted for somewhere.
2. `forwarded ∩ dropped == ∅` -- no seq is double-counted.
3. `received == forwarded` -- every seq the buffer released arrived
   at the processor.

## Files

| File | Role |
|------|------|
| `runner.py` | spawns rtid + the 3 federate processes; reads JSON results; verifies invariants |
| `_federate_common.py` | shared scaffolding: argparse, sys.path, federation spec, untimed driver loop |
| `generator_main.py` | the generator federate's `main()` -- spawned by the runner |
| `buffer_main.py` | the buffer federate's `main()` -- spawned by the runner |
| `processor_main.py` | the processor federate's `main()` -- spawned by the runner |
| `generator.py` `buffer.py` `processor.py` | model files (`CoupledModelProtocol` shape; near-copy of `pyjevsim-relay/`) |
| `relay-fom.xml` | FOM (verbatim copy of `pyjevsim-relay/relay-fom.xml`) |
| `test_relay_cross_process.py` | end-to-end pytest suite |

## Why we don't use `HLAFederate.step_once` here

The bridge's `HLAFederate.step_once` issues `next_message_request` on
every cycle. Real `rtid` (M3+) does not yet wire the time-service
gRPC handlers (`rti/internal/transport/grpc/server.go` line 144:
`timeService: nil`), so any cross-process call to
`next_message_request` hangs forever waiting for a grant that never
arrives. See `pysdk/rti1516e/_transport.py`'s module docstring:

> Time-management RPCs (NextMessageRequest etc.) are not wired --
> rtid's TimeService is nil at M2; the SDK records the call but does
> not dispatch it. The bridge's `_await_grant` would block forever
> in real gRPC mode; cross-language tests therefore avoid the
> time-managed code path and instead drive interactions directly.

This example takes that escape hatch: each federate's main runs an
*untimed* driver (`_federate_common.run_untimed_loop` /
`run_drain_only_loop`). Logical time still flows -- the model's
`time_advance` is consulted to size the per-tick wall-clock sleep --
but there is no LBTS / NER coordination. When `rtid` ships its
TimeService implementation, this example can be tightened to the
in-process variant's same-tick fan-out semantics by replacing the
untimed driver with `HLAFederate.step_once`.

## Why drop counts drift

Cross-process timing is racy by construction:

  - Each federate runs in its own asyncio event loop.
  - There is no shared clock; the buffer's "every other tick"
    pacing is a wall-clock period, not a logical-time period.
  - Generator emits, rtid forwards over a server-streaming RPC, the
    buffer's events queue receives -- each step has variable
    latency depending on the kernel scheduler.

So the exact split of (forwarded, dropped) varies across runs. The
*total* (`forwarded + dropped + residual`) is always equal to
`published` -- that's the conservation law the verifier checks, and
it holds across every run we've measured.

If you need deterministic drop counts (e.g. for a comparison study
across DEVS scheduling strategies), use the in-process variant.

## Tail-tick discipline

The runner spawns the buffer + processor first (so their
`subscribe_interaction_class` lands on rtid before the generator
starts publishing -- pre-subscription publishes are dropped server-
side), then sleeps 0.5s, then spawns the generator.

Each federate runs for a configured number of ticks. The buffer +
processor run for `gen_messages + drain_ticks + tail_ticks` ticks,
where `tail_ticks` provides slack so the buffer's last emit is still
delivered + drained by the processor before either resigns. The
generator stops at `gen_messages + drain_ticks` because it has no
incoming work past that point.

The tail size is sized in the runner (`buffer_tail = 20`,
`processor_tail = buffer_tail + 20`); the asymmetry exists because
the processor needs to drain *after* the buffer's final emit window
closes. If you tune `tick_period` very small you may need to bump
both tails.

## Debugging tips

### A federate hangs and the runner times out

  1. Re-run with `--keep-tempdir` to keep the per-federate log files
     after teardown.
  2. The path is printed at the end:
     `runner: tempdir kept at /tmp/pyjevsim-relay-cross-XXXXXX`.
  3. Inspect:
     - `<tempdir>/federate-logs/{generator,buffer,processor}.log`
       -- stdout + stderr from each federate's Python process.
     - `<tempdir>/rtid.log` -- rtid's structured JSON log.
     - `<tempdir>/{generator,buffer,processor}-result.json` -- the
       JSON the federate wrote on exit (missing means it crashed
       or was force-terminated mid-run).

### Accounting violation but everything ran to completion

The most common failure: `received != forwarded` with the buffer's
last forwarded seq missing from the processor. This is a tail-tick
timing race -- the buffer emitted on its last tick and the
processor's drain window ended before the message landed. Bump the
`processor_tail` constant in `runner.py` or use a longer
`--tick-period`.

### "address already in use" at rtid spawn

  - Another rtid instance is holding a port. The runner allocates
    `--listen`, `--metrics-listen`, and `--admin-listen` on free
    ports per-run, but if you have a stale rtid bound to the
    default `localhost:8443` admin port from a previous session,
    new rtid instances can still launch (their admin port is
    different).
  - To find leftovers on Linux:
    `ls /proc/*/cmdline 2>/dev/null | xargs -0 grep -l "bin/rtid" 2>/dev/null`
    then kill them.

### Cross-platform note (Windows)

Subprocess teardown on Windows differs from POSIX:

  - `start_new_session=True` is POSIX-only -- the runner skips it on
    Windows. The rtid + federate processes therefore inherit the
    runner's process group; a Ctrl-C at the runner sends `BREAK` to
    every child process, which the runner's `finally` block then
    reaps.
  - `Popen.terminate` on Windows calls `TerminateProcess`
    immediately rather than sending `SIGTERM`. Federates therefore
    don't get a chance to write a partial result file on Windows
    teardown -- run with `--keep-tempdir` if you need to debug a
    Windows-only timing issue.

End-to-end runs on Windows haven't been validated in CI yet. If you
hit a Windows-only failure, file a bug.

## Running the test suite

```bash
# From the repo root
python3 -m pytest examples/pyjevsim-relay-cross-process/

# A single test
python3 -m pytest examples/pyjevsim-relay-cross-process/test_relay_cross_process.py::test_relay_cross_process_default_config -v
```

The tests skip themselves when the Go toolchain is not on `PATH`
(`go build` is required to produce `bin/rtid`).

## What's deferred

  - **Time-managed cross-process variant** -- when rtid ships its
    TimeService gRPC handlers, the federates can switch to
    `HLAFederate.step_once` and the cross-process example will
    inherit the in-process variant's deterministic same-tick fan-out
    semantics.
  - **Multi-host deployment** -- this example is single-host
    (everyone dials `127.0.0.1`). For a multi-host deployment, swap
    the URL builder in `runner.py`'s `_spawn_federate` call.
  - **TLS / `grpcs://`** -- the runner uses plaintext gRPC. To dial
    a TLS-secured rtid, change the URL scheme to `grpcs://` and
    teach `_federate_common.federation_spec` to forward a `ca_cert`
    argument through `RtiConnection.connect`.
