# pyjevsim-object-tracking — time-managed Object Management demo

The first cross-process gorti example that exercises **both** Object
Management (§6) AND Time Management (§8) simultaneously, with reflects
delivered into a pyjevsim-style coupled model via `external_transition`.

## What it demonstrates

- 1 **producer** federate registers a `Vehicle` object instance and
  updates its `Position` + `Speed` attributes at every TimeAdvanceGrant.
  Updates carry timestamps (TSO delivery), so rtid coordinates the
  delivery order across subscribers.
- 2 **tracker** federates subscribe to `Vehicle.Position + Vehicle.Speed`,
  receive the reflects via the events stream, and feed them into a
  pyjevsim-style coupled model's `external_transition(port, payload)`
  — the canonical "incoming external input message" hook.

All 3 federates are **managed by rtid's TimeService**:

  | Role     | Time-management opt-in                     |
  |----------|---------------------------------------------|
  | producer | `enable_time_regulation(la=1.0)` + `enable_time_constrained` |
  | tracker  | `enable_time_constrained` (constrained-only)               |

The producer drives LBTS (`producer.contribution = currentTime + lookahead`).
Trackers don't regulate — they're pure subscribers, so they don't
over-constrain the federation. Each cycle:

  1. Producer: `update_attributes(timestamp=t)`. Reflect is buffered
     in trackers' TSO buffer (M22 W2) because tracker.currentTime < t.
  2. Producer: `next_message_request_available(t)`. Grant fires; producer
     advances to t.
  3. Trackers: `next_message_request_available(t)`. Grant fires;
     `releaseBufferedTSO` drains the buffered reflect into the trackers'
     events stream as part of the grant cycle.
  4. Tracker `drain_events_into_model` routes the reflect into the
     coupled model's `external_transition("reflect:Vehicle", attrs)`.

## Run — automated (one command)

```bash
python3 examples/pyjevsim-object-tracking/runner.py
```

Default: 5 cycles, tick_step=1.0, lookahead=1.0. Override with `--cycles`,
`--tick-step`, `--rtid-binary`, `--workdir`, `--log-level` (see
`examples/RUNNER_LOGGING.md`).

## Run — manual (each federate in its own terminal)

For interactive experimentation. Three terminals:

```bash
# Terminal 1 — start rtid
./bin/rtid --listen :8442

# Terminal 2 — start a tracker (blocks on Discover until producer registers)
examples/pyjevsim-object-tracking/tracker_run.sh tracker-A

# Terminal 3 — start the producer (registers the Vehicle 1.5s after publish)
examples/pyjevsim-object-tracking/producer_run.sh
```

Order: rtid → trackers → producer. Trackers must subscribe before the
producer registers (rtid does not replay registrations to late
subscribers in cut-3); the producer's 1.5 s post-publish sleep covers
the join+subscribe window.

Both scripts honor `RTID_URL` (default `grpc://127.0.0.1:8442`) and
`RTID_VENV` (default `<repo>/.m21-venv`) env overrides, and pass any
remaining flags through to the Python entry point — e.g.,
`./tracker_run.sh tracker-B --cycles 10 --tick-step 0.5`. Result JSONs
land in `examples/pyjevsim-object-tracking/.run/manual/`.

Sample output (each `[name] ...` line is tee'd to BOTH the parent's
console AND `<workdir>/<name>.log`):

```
[runner]    working directory: examples/pyjevsim-object-tracking/.run/20260510-074817
[rtid]      time=2026-05-10T07:48:17Z level=INFO msg="rtid serving" grpc=[::]:42835
[producer]  producer[producer]: registered Vehicle (handle=1) — trackers should now Discover
[tracker-A] tracker[tracker-A]: discovered 1 Vehicle instance(s); entering NMRA loop
[tracker-B] tracker[tracker-B]: discovered 1 Vehicle instance(s); entering NMRA loop
[producer]  producer[producer]: done — 3 updates
[tracker-A] tracker[tracker-A]: done — 3 reflects, 1 discovers
[tracker-B] tracker[tracker-B]: done — 3 reflects, 1 discovers
[runner]    cycles=3 federates={producer=producer(3) tracker-A=tracker(3) tracker-B=tracker(3)} verify=ok
```

## How rtid governs the cycle

Confirm by inspecting `<workdir>/rtid.log` — every grant + every
attribute update is recorded server-side. To prove rtid (not the
federates) drives the schedule, kill rtid mid-run (`pkill -9 rtid`):
the trackers' `drain_events_into_model` will hang at the grant
deadline, proving they can't synthesize grants themselves.

## Files

| File | Role |
|---|---|
| `vehicle-fom.xml` | FOM declaring `Vehicle` with `Position` + `Speed` (HLAfloat64BE, TimeStamp order) |
| `_federate_common.py` | Shared `wait_for_full_grant`, `wait_for_discover`, `drain_events_into_model`, name-resolution adapter |
| `producer_main.py` | Producer federate — registers Vehicle; updates attributes (TSO) → NMRA per cycle |
| `tracker_main.py` | Tracker federate — subscribes; pyjevsim-style `VehicleTracker` model with `external_transition` |
| `runner.py` | Spawns rtid + producer + 2 trackers; tee'd logs; `<example>/.run/<timestamp>/` workdir |

## Why this example exists

Pre-this-commit, no example demonstrated Object Management + Time
Management together cross-process:

- `pyjevsim-time-advance/` — TimeService cycles only, no objects.
- `pyjevsim-dashboard*/` — Object Management only, untimed (relies on
  `enable_asynchronous_delivery` to bypass the TSO gate).
- `pyjevsim-relay-cross-process/` — Interactions only, untimed.
- `pyjevsim/` — Pub/sub of interactions only.

This example fills the gap: federation-wide time coordination + object
attribute updates + pyjevsim-style external-input integration.

## Known timing pin

The runner sleeps 0.6 s after spawning the producer before spawning the
trackers, and the producer sleeps 1.5 s after publishing before
registering. This setup barrier ensures trackers join + subscribe
before the producer's first update fans out (rtid does NOT replay
prior registrations to late subscribers in cut-3). Without it, the
trackers race past producer's early cycles. A future cut should use
HLA synchronization points (M8) instead of sleeps.
