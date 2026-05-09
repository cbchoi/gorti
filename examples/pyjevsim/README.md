# pyjevsim example -- cross-process

Two pyjevsim-style coupled models (a `Producer` that emits a
monotonic sequence number every tick, and a `Consumer` that records
every arrival) federated through `rtid`. Each federate runs as its
own Python subprocess; `rtid` is a real subprocess with a real gRPC
listener; everything talks `grpc://`.

```text
   +-------------+   ProducerOutput   +-------------+
   | producer.py | ================>  | consumer.py |
   +-------------+                    +-------------+
         |                                  |
         |   grpc://127.0.0.1:8442 (or runner-chosen port)
         +----------------+-----------------+
                          |
                          v
                   +-------------+
                   |    rtid     |
                   +-------------+
```

This is the M4 flagship demo, converted to the cross-process
deployment shape. The pattern mirrors
[`examples/pyjevsim-relay-cross-process`](../pyjevsim-relay-cross-process/README.md)
exactly so contributors who learn one example understand the other
immediately. The only structural differences are:

  - 2 federates instead of 3 (no buffer/relay stage)
  - The accounting invariant simplifies to `received == published`
    (no drops; no buffer; no relay)

## Prerequisites

Identical to
[`examples/pyjevsim-relay-cross-process`](../pyjevsim-relay-cross-process/README.md#prerequisites)
-- the federates import `rti1516e.connection`, which transitively
imports `grpc`. The pysdk's gRPC bindings under
`pysdk/rti1516e/_generated/` are gitignored; every contributor
regenerates them locally.

One-time setup, **run from the repo root** (not from this example
directory -- there is no `pysdk/` here):

```bash
pip install -e './pysdk[dev]'    # grpcio + protobuf + grpcio-tools
make py-codegen                  # regenerate pysdk/rti1516e/_generated/
```

Symptom-to-fix table:

| Error at federate startup | Missing step |
|---|---|
| `ModuleNotFoundError: No module named 'grpc'` | `pip install -e ./pysdk` |
| `No module named 'grpc_tools'` (during `make py-codegen`) | install with the `[dev]` extra |
| `Protocol message JoinFederationRequest has no "federate_type" field` | `make py-codegen` (bindings stale) |

The Go toolchain also needs to be on `PATH` -- the runner and the
shell scripts both build `bin/rtid` on first run.

## Run it -- orchestrated (runner.py)

The default workflow. One process spawns rtid, the two federates,
and tears them all down at the end.

```bash
# From the repo root
python3 examples/pyjevsim/runner.py

# Knobs (defaults shown)
python3 examples/pyjevsim/runner.py \
    --ticks 50 \
    --drain-ticks 30 \
    --tick-period 0.05 \
    --keep-tempdir          # leave logs + result JSON for inspection
```

Default-config sample output:

```text
runner: published=50  received=50  rtid_port=39145  verify=ok
```

The verifier asserts `received == published`. With one publisher,
one subscriber, and gRPC's in-order delivery guarantee, this should
hold exactly -- it's a stronger invariant than the relay's
"conservation law" because there's no buffer to drop on overflow
and no race between concurrent publishers. If `verify` is anything
other than `ok`, something is structurally wrong: the subscription
didn't land before the first publish, a federate crashed mid-loop,
or the network dropped a message.

## Run it -- manually (shell scripts)

For when you want to launch each federate in its own terminal so you
can watch its stdout, kill it with Ctrl-C, restart it, etc.

The scripts use **fixed ports** (8442 listen, 8443 admin, 9090
metrics) instead of the runner's free-port discovery. Override any
port via env: `RTID_LISTEN_PORT=9000 ./rtid_run.sh`.

> Note: these defaults are the *same* fixed ports the relay-cross-process
> scripts use. Don't run both examples at the default ports
> simultaneously -- pick one or override the other.

Required launch order -- consumer must subscribe before the
producer publishes, otherwise pre-subscription publishes are dropped
server-side:

```bash
cd examples/pyjevsim

# Terminal 1 (keep running)
./rtid_run.sh

# Terminal 2 -- consumer first
./consumer_run.sh

# Terminal 3 -- producer last
./producer_run.sh

# Terminal 4 -- after both federates exit (~6s):
./verify_run.sh
```

Each federate script prints a final `DONE` or `FAILED` line when its
python exits, with the result count inline -- so you don't have to
read the JSON to know whether the federate did its job:

```text
producer_run: DONE — published=50  result=/tmp/.../producer-result.json
consumer_run: DONE — received=50  result=/tmp/.../consumer-result.json
```

`verify_run.sh` then checks `received == published` and prints
`PASS` / `FAIL`; on FAIL it lists the first few unaccounted seqs
so you can correlate with the federate logs.

Result JSON files land at
`/tmp/pyjevsim-cross/{producer,consumer}-result.json` by default.
Override with `RESULT_DIR=/path/to/dir`. Tunables (`TICKS`,
`DRAIN_TICKS`, `TICK_PERIOD`, `CONSUMER_TAIL_TICKS`) are
env-overridable too; see `_run_common.sh` for the full list.

## Files

| File | Role |
|------|------|
| `runner.py` | spawns rtid + the 2 federate processes; reads JSON results; verifies `received == published` |
| `_federate_common.py` | shared scaffolding: argparse, sys.path, federation spec, untimed driver loop |
| `producer_main.py` | the producer federate's `main()` -- spawned by runner.py or producer_run.sh |
| `consumer_main.py` | the consumer federate's `main()` -- spawned by runner.py or consumer_run.sh |
| `producer.py` `consumer.py` | DEVS coupled-model files (port-mapped to FOM interaction classes via `_federate_common.emit_outputs_now`/`drain_externals_now`) |
| `pyjevsim-fom.xml` | FOM declaring the `ProducerOutput` interaction class |
| `_run_common.sh` | sourced by all shell scripts; defaults for ports, URL, tunables, result/log dirs, rtid binary path, plus the `report_result` helper used by federate scripts |
| `rtid_run.sh` | launches the rtid daemon at fixed ports for manual federate runs |
| `producer_run.sh` `consumer_run.sh` | launch the corresponding federate against an already-running rtid; print `DONE`/`FAILED` summary on exit |
| `verify_run.sh` | cross-result invariant check after a manual run; exits 0 on PASS, 1 on FAIL |
| `_real_pyjevsim_adapter.py` | adapter mapping a real `pyjevsim.StructuralModel` onto the bridge's `CoupledModelProtocol`. Not used by the cross-process runner directly; available for users wiring real pyjevsim coupled models. |
| `cross_lang_runner.py` `cross_lang_test.py` | M5 cut-1 cross-language smoke (rtid out-of-process, pub+sub as asyncio tasks in one Python process). Subset of what the new `runner.py` does; kept for the spec-test reference. |

## Why this example uses an untimed driver

Producer/Consumer is a pure pub/sub demo — no LBTS / NER coordination
needed. The example uses `_federate_common.run_untimed_loop` /
`run_drain_only_loop`; logical time still flows (the model's
`time_advance` is consulted), but the federation doesn't care about
time advance. Tightening to `HLAFederate.step_once` is possible
post-M21 (TimeService is now wired) but adds machinery without
changing what this example demonstrates.

For an example that *does* exercise time advance, see
`examples/pyjevsim-time-advance/` (M21 W4B).

## Debugging tips

### A federate hangs and the runner times out

Re-run with `--keep-tempdir`, then read the per-federate logs:

```bash
python3 examples/pyjevsim/runner.py --keep-tempdir
# tempdir path printed at end; logs at:
#   <tempdir>/federate-logs/{producer,consumer}.log
#   <tempdir>/rtid.log
```

### `received != published` from verify_run.sh

Most common cause: the producer started publishing before the
consumer's `subscribe_interaction_class` round-tripped to rtid.
Pre-subscription publishes are dropped server-side. Restart with
the consumer launched first (the `runner.py` does this with a
0.5-second sleep between the consumer spawn and the producer
spawn).

### "address already in use" at rtid spawn

Another rtid instance is holding `:8442`, `:8443`, or `:9090`.
Override one of `RTID_LISTEN_PORT`, `RTID_ADMIN_PORT`,
`RTID_METRICS_PORT`. To find leftover rtid processes on Linux:

```bash
ls /proc/*/cmdline 2>/dev/null | xargs -0 grep -l "bin/rtid" 2>/dev/null
```

## What's deferred

  - **Time-managed cross-process variant** -- when rtid ships its
    TimeService gRPC handlers, the federates can switch to
    `HLAFederate.step_once` and inherit the deterministic same-tick
    semantics the original (now removed) in-process runner had.
  - **Multi-host deployment** -- this example is single-host
    (everyone dials `127.0.0.1`). For multi-host, change the URL
    in `_spawn_federate` (runner) or `RTID_URL` (shell scripts).
  - **TLS / `grpcs://`** -- plaintext gRPC for now. To dial a
    TLS-secured rtid, change the URL scheme to `grpcs://` and teach
    `_federate_common.federation_spec` to forward a `ca_cert`
    argument through `RtiConnection.connect`.
