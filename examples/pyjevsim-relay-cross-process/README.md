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

## Prerequisites

The federate processes (and the runner) import `rti1516e.connection`,
which in turn imports `grpc` -- a real PyPI package that has to be
installed in your Python environment. The `sys.path` bootstrapping in
`_federate_common.py` makes the `rti1516e` package itself importable
without an install, but it cannot fabricate `grpc`.

The pysdk's gRPC bindings under `pysdk/rti1516e/_generated/` are
also gitignored -- every contributor regenerates them locally from
`proto/`. A stale or missing `_generated/` surfaces as a runtime
`Protocol message X has no "Y" field` AttributeError, not a clean
import error, which makes the symptom misleading until you know to
look for it.

One-time setup, **run from the repo root** (not from this example
directory -- there is no `pysdk/` here):

```bash
pip install -e './pysdk[dev]'    # grpcio + protobuf + grpcio-tools (codegen)
make py-codegen                  # regenerate pysdk/rti1516e/_generated/
```

If you only want the runtime deps and have already regenerated the
stubs (e.g. on a CI image where codegen is a separate step):

```bash
pip install 'grpcio>=1.60' 'protobuf>=7.34'
```

Symptom-to-fix table:

| Error at federate startup | Missing step |
|---|---|
| `ModuleNotFoundError: No module named 'grpc'` | `pip install -e ./pysdk` |
| `No module named 'grpc_tools'` (during `make py-codegen`) | install with the `[dev]` extra |
| `Protocol message JoinFederationRequest has no "federate_type" field` | `make py-codegen` (bindings stale vs `proto/`) |

The Go toolchain also needs to be on `PATH` -- the runner and the
shell scripts both build `bin/rtid` on first run.

## Run it -- orchestrated (runner.py)

The default workflow. One process spawns rtid, the three federates,
and tears them all down at the end.

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

## Run it -- manually (shell scripts)

For when you want to launch each federate in its own terminal so you
can watch its stdout, kill it with Ctrl-C, restart it, etc. The
runner.py path is better for repeated batch runs; the shell scripts
are better for operator-style debugging and as a runbook reference.

The scripts use **fixed ports** (8442 listen, 8443 admin, 9090
metrics) instead of the runner's free-port discovery, so each
federate knows exactly where to dial without a registry. Override any
port via env: `RTID_LISTEN_PORT=9000 ./rtid_run.sh`.

Required launch order -- consumers must subscribe before the
generator publishes, otherwise pre-subscription publishes are dropped
server-side:

```bash
cd examples/pyjevsim-relay-cross-process

# Terminal 1
./rtid_run.sh

# Terminal 2 -- consumers first
./processor_run.sh

# Terminal 3
./buffer_run.sh

# Terminal 4 -- generator last
./generator_run.sh
```

Result JSON files land at `/tmp/pyjevsim-relay-cross/{generator,
buffer,processor}-result.json` by default. Override with
`RESULT_DIR=/path/to/dir`. Tunables (`GEN_MESSAGES`, `CAPACITY`,
`SERVICE_PERIOD`, `DRAIN_TICKS`, `TICK_PERIOD`,
`BUFFER_TAIL_TICKS`, `PROCESSOR_TAIL_TICKS`) are env-overridable too;
see `_run_common.sh` for the full list.

The shell scripts do **not** verify the conservation law -- they
just run the federates. To check accounting after a manual run:

```bash
python3 -c "
import json, pathlib
d = pathlib.Path('/tmp/pyjevsim-relay-cross')
g = json.loads((d/'generator-result.json').read_text())
b = json.loads((d/'buffer-result.json').read_text())
p = json.loads((d/'processor-result.json').read_text())
pub, fwd, drp, res = set(g['published']), set(b['forwarded']), set(b['dropped']), set(b['queue_residual'])
print('published =', len(pub), ' forwarded|dropped|residual =', len(fwd|drp|res), ' match =', pub == fwd|drp|res)
print('received  =', len(p['received']), ' forwarded =', len(fwd), ' match =', set(p['received']) == fwd)
"
```

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
| `_run_common.sh` | sourced by the four shell scripts; defaults for ports, URL, tunables, result/log dirs, rtid binary path |
| `rtid_run.sh` | launches the rtid daemon at fixed ports for manual federate runs |
| `generator_run.sh` `buffer_run.sh` `processor_run.sh` | launch the corresponding federate against an already-running rtid (use `rtid_run.sh` first) |

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
