# pyjevsim time-advance example — three regulators with different lookaheads

Three-federate time-management example running on the
``pyjevsim_bridge``:

```text
                    Heartbeat (HLAfloat64BE: federate's now)
       ┌──fast──▶
       │          lookahead = 0.5
   ────┼──mid───▶
       │          lookahead = 1.0
       └──slow──▶
                  lookahead = 2.0
```

Every federate emits one Heartbeat per cycle and advances its
logical clock by ``step`` (default 1.0). The runner records the
LBTS contribution table per tick + the per-federate grant times,
and verifies the documented time-management invariants.

## What you'll learn

- **LBTS = min(current + lookahead) over the regulating set.**
  Mirror the rule the rti server-side enforces in
  ``rti/internal/time/lbts.go::LBTS``; read it off the tick trace.
- **Lookahead's effect on grant ordering.** The federate with the
  smallest ``current + lookahead`` is the one whose grant fires
  earliest in any tick.
- **NER (next-message-request) cycle semantics.** The bridge calls
  ``next_message_request(now + ta)`` on each cycle and waits for
  the matching ``TimeAdvanceGrant``. The in-process transport
  auto-grants at the requested time so the trace is clean and
  reproducible.
- **Driving ``enable_time_regulation`` from the runner.** The
  bridge does not auto-enable regulation (it's a federation
  bootstrap step, not a per-tick call); the runner drives it
  once via the bridge's underlying ``Federate`` handle.

## Federates

### Regulator (×3)

Identical model wrapped three times with different
``(name, step, lookahead)`` triples. Implements the bridge's
``CoupledModelProtocol`` via duck typing:

- ``time_advance()``: returns ``step`` (constant per federate).
- ``output_handler()``: emits one ``Heartbeat`` per cycle with the
  federate's current ``now`` packed as 8-byte big-endian double.
- ``internal_transition()``: advances ``now`` by ``step``.

The runner snapshots the federate's ``now`` + ``lookahead`` before
each tick to compute LBTS contributions; that's the trace this
example pivots around.

## Run it

```bash
# From the repo root
python3 examples/pyjevsim-time-advance/runner.py

# Knobs (defaults shown)
python3 examples/pyjevsim-time-advance/runner.py \
    --ticks 6 \
    --la-fast 0.5 \
    --la-mid 1.0 \
    --la-slow 2.0 \
    --step 1.0 \
    --verbose
```

Default-config output:

```text
runner: ticks=6  final_lbts= 5.50  ner=18  heartbeats=18  verify=ok
```

A verbose trace looks like:

```text
tick= 0  lbts= 0.50  earliest=fast  contribs=fast= 0.50, mid= 1.00, slow= 2.00  after=fast= 1.00, mid= 1.00, slow= 1.00
tick= 1  lbts= 1.50  earliest=fast  contribs=fast= 1.50, mid= 2.00, slow= 3.00  after=fast= 2.00, mid= 2.00, slow= 2.00
...
```

The ``contribs`` column is exactly the
``[]RegulatingFederate`` ``Time + Lookahead`` snapshot the rti's
``LBTS`` function consumes server-side; ``earliest`` is the
``min`` of that snapshot.

## Verification invariants

1. **LBTS is monotonic non-decreasing.** Logical time never goes
   backwards globally.
2. **Earliest federate is consistent with contributions.** The
   federate flagged ``earliest_federate`` in every row in fact has
   the minimum ``current + lookahead`` in that row.
3. **At tick 0, ``"fast"`` is the earliest.** Sanity-anchors the
   default-config pedagogical claim.
4. **Per-federate counts match.** Each federate emits exactly
   ``ticks`` heartbeats and issues exactly ``ticks`` NER calls.

## Tuning knobs

| Knob | Effect |
|---|---|
| ``--ticks N`` | Number of cycles per federate |
| ``--la-fast 0.5`` | Lookahead of the "fast" regulator |
| ``--la-mid 1.0`` | Lookahead of the "mid" regulator |
| ``--la-slow 2.0`` | Lookahead of the "slow" regulator |
| ``--step T`` | Time-advance step (constant; same for every regulator at this cut) |

Pedagogical follow-up: try ``--la-fast 2.0 --la-slow 0.1``. The
federate NAMED ``"fast"`` is now the slowest contributor and the
federate named ``"slow"`` is the earliest. The default verify()
rejects this on rule (3); it's the test
``test_time_advance_swap_makes_slow_earliest`` that asserts this
relabelling behaviour.

## What's deferred

- **TAR (time_advance_request).** The bridge currently uses NER
  only. TAR has different cycle semantics — every grant is at the
  requested time regardless of whether external messages arrived
  in flight, so a TAR variant of this example would need a
  bridge-level switch. Out of scope at this cut.
- **Cross-federate causal chains.** The three regulators don't
  consume each other's heartbeats; they're independent emitters.
  A causal-chain variant would let the receivers' state advance
  trigger NER from the dependent federate; that's the same wiring
  the relay example exercises (see
  ``examples/pyjevsim-relay/buffer.py``) but with TM constraints.
- **Async vs sync time advance.** Every federate in this example
  advances at every outer-tick. Real federations have federates
  that block waiting for messages from constrained peers; the
  ``rti/internal/time/`` ``LBTSStrategy`` interface is the place
  to plug alternative grant policies (see
  ``docs/research-platform-howto.md``).
- **Ner-with-pending-events.** The bridge's
  ``time_advance.py::step_once`` already handles "external arrived
  earlier than ta → no internal cycle this round" — exercised in
  the relay example. This time-advance example deliberately
  omits cross-federate externals to keep the trace clean.
