# pyjevsim sync-points example -- cross-process

Three federates (`alpha`, `beta`, `gamma`) rendezvous at named sync
labels, run a brief exchange, then rendezvous again at
`end_simulation`, then resign. The canonical HLA bootstrap pattern,
demonstrated against a real `rtid` over `grpc://`.

```text
            register   achieve(all)   synchronized
   alpha ──┬─ phase ─┬──── achieve──┬──── (gate) ──┐
   beta  ──┤  start  │   _simulation│              │
   gamma ──┴────────┴──────────────┴──────────────┴──▶  RUN N ticks
                                                        │
                                                        │  Tick interactions
                                                        ▼
            register   achieve(all)   synchronized
   alpha ──┬─ phase ─┬──── achieve──┬──── (gate) ──┐
   beta  ──┤   end   │   _simulation│              │
   gamma ──┴────────┴──────────────┴──────────────┴──▶  RESIGN
```

Each `participant_main.py` runs as its own Python subprocess; rtid
is a real subprocess; everything talks `grpc://`. Sync RPCs go
through the wired SyncService (M12 W1); the
`SynchronizationPointAnnounced` and `FederationSynchronized` events
arrive on each federate's event stream (proto FederateEvent oneof
tags 20 / 21). The runner-as-oracle workaround the original example
used (M12 deferral #1) is no longer needed.

## Prerequisites

Identical to `examples/pyjevsim-relay-cross-process` -- see its
[Prerequisites](../pyjevsim-relay-cross-process/README.md#prerequisites)
section. One-time setup, **from the repo root**:

```bash
pip install -e './pysdk[dev]'
make py-codegen
```

## Run it -- orchestrated (runner.py)

```bash
# From the repo root
python3 examples/pyjevsim-sync-points/runner.py

# Knobs (defaults shown)
python3 examples/pyjevsim-sync-points/runner.py \
    --running-ticks 10 \
    --tick-period 0.05 \
    --keep-tempdir
```

Sample output:

```text
runner: federates=3  labels=['start_simulation', 'end_simulation']  sent={alpha=10 beta=10 gamma=10}  rtid_port=43781  verify=ok
```

`verify=ok` means: every federate achieved + synchronized at both
labels and emitted exactly `running_ticks` Tick interactions in
order.

## Run it -- manually (shell scripts)

5 terminals: rtid, three participants, then verify:

```bash
cd examples/pyjevsim-sync-points

# Terminal 1
./rtid_run.sh

# Terminals 2, 3, 4 -- start in any order; the JOIN_SETTLE delay
# (default 1.5s) ensures all three are joined before any registers
# the sync point.
./alpha_run.sh
./beta_run.sh
./gamma_run.sh

# Terminal 5 -- after all three exit
./verify_run.sh
```

Each participant script prints a `DONE` summary on exit:

```text
alpha_run: DONE — achieved=2 synchronized=2 sent_ticks=10  result=...
beta_run:  DONE — achieved=2 synchronized=2 sent_ticks=10  result=...
gamma_run: DONE — achieved=2 synchronized=2 sent_ticks=10  result=...
```

`verify_run.sh` then checks the cross-result invariants and prints
`PASS` / `FAIL` for each.

Tunables (env-overridable):

| var | default | meaning |
|---|---|---|
| `RUNNING_TICKS` | 10 | ticks between start and end labels |
| `TICK_PERIOD` | 0.05 | wall-clock seconds per tick |
| `JOIN_SETTLE` | 1.5 | delay after join before registering sync point |
| `RENDEZVOUS_TIMEOUT` | 20.0 | per-rendezvous deadline |
| `RESULT_DIR` | /tmp/pyjevsim-sync-cross | where result JSONs land |

## Files

| File | Role |
|------|------|
| `runner.py` | spawns rtid + 3 participant processes; reads JSONs; verifies |
| `_federate_common.py` | scaffolding: argparse, sync-aware wait helpers, running-phase loop |
| `participant_main.py` | participant entry point (`--name alpha\|beta\|gamma`) |
| `participant.py` | DEVS coupled-model file (Tick emitter; runner-driven oracle methods are unused but kept for backward compat) |
| `sync-points-fom.xml` | FOM declaring the `Tick` interaction class |
| `_run_common.sh` | shell defaults + `report_result` helper |
| `rtid_run.sh` | rtid daemon launcher |
| `alpha_run.sh` `beta_run.sh` `gamma_run.sh` | per-participant federate launchers |
| `verify_run.sh` | cross-result invariant check after a manual run |

## Why no time-managed variant?

Same reason as
[`examples/pyjevsim-relay-cross-process`](../pyjevsim-relay-cross-process/README.md#why-we-dont-use-hlafederatestep_once-here):
real rtid (M3+) does not yet wire the time-service gRPC handlers
(`timeService: nil`). Cross-process therefore uses an untimed driver
for the running phase. When rtid ships TimeService, the running-phase
loop can be tightened to use `HLAFederate.step_once`.

## Debugging tips

### A federate hangs on rendezvous

The rendezvous waits for `SynchronizationPointAnnounced` /
`FederationSynchronized` events with a per-rendezvous deadline
(default 20s, env-overridable as `RENDEZVOUS_TIMEOUT`). A timeout
usually means one of the three federates didn't actually join in
time -- check the federate logs (`runner.py --keep-tempdir` keeps
them):

```text
<tempdir>/federate-logs/{alpha,beta,gamma}.log
<tempdir>/rtid.log
```

### `ALREADY_REGISTERED` errors

Expected for two of the three federates — exactly one wins the
register-synchronization-point race. The `register_or_swallow`
helper drops the error silently. If you see it in a log it's just
narration; the rendezvous still proceeds via the announced event.
