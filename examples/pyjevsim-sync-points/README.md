# pyjevsim sync-points example — canonical HLA bootstrap

Three federates rendezvous at named sync labels, run a brief
exchange, then rendezvous again at end-of-simulation, then resign.
This is the canonical HLA bootstrap pattern.

```text
            register   achieve(all)   synchronized
   alpha ──┬─ phase ─┬──── achieve──┬──── (gate) ──┐
   beta  ──┤  start  │   _simulation│              │
   gamma ──┴────────┴──────────────┴──────────────┴──▶  RUN N ticks
                                                        │
                                                        │  Tick interactions
                                                        │  per cycle
                                                        ▼
            register   achieve(all)   synchronized   ▼
   alpha ──┬─ phase ─┬──── achieve──┬──── (gate) ──┐
   beta  ──┤   end   │   _simulation│              │
   gamma ──┴────────┴──────────────┴──────────────┴──▶  RESIGN
```

## What you'll learn

- The canonical **bootstrap rendezvous pattern**: register a sync
  label, every federate votes "achieved", the federation observes
  "synchronized" once every required peer has voted.
- How to **gate progress on rendezvous** — the running phase
  doesn't start until the federation is synchronized on
  ``start_simulation``, and resign doesn't happen until everyone
  achieves ``end_simulation``.
- The **runner-as-orchestrator workaround** for the M12 wire-layer
  deferral that makes ``Federate.sync`` callbacks unobservable on
  the in-process transport.

## Federates

### Participant (×3)

Identical model wrapped three times under three federate names
(``alpha``, ``beta``, ``gamma``). The model exposes:

- ``achieve(label)``: vote "achieved" on a label. Appends to the
  ``achieved`` list. (Would be ``fed.sync.synchronization_point_achieved(label)``
  in a wired cut-4 world.)
- ``mark_synchronized(label)``: notify the federate that the
  federation is fully synchronized on a label. Appends to the
  ``synchronized`` list. (Would arrive as a
  ``FederationSynchronized`` event on ``fed.events()`` in a
  wired cut-4 world.)
- DEVS ``output_handler`` that emits one ``Tick`` interaction per
  cycle when the federate's ``running`` flag is True (and ``{}``
  otherwise — the bridge calls ``send_interaction`` only when
  ``output_handler`` returns a non-empty dict).

## The M12 wire-layer deferral and the workaround

Per ``docs/reports/M12/agent-c.md`` deferral #1, the proto
``FederateEvent`` oneof in M12 does not include sync-event
variants. As a result the rti's manager runs the
all-required-achieved → emit-synchronized internal path but the
``federationSynchronized`` callback **is not observable on the
wire**. The M12 spec test
``test_spec_m12_sync_register_and_achieve`` works around this by
asserting "no error from the four RPCs", which is the round-trip
evidence the manager ran.

This example takes the same workaround one step further: since
the in-process ``InProcessTransport`` has no scheduler at all
(let alone a sync manager), the runner is the **oracle** for
both the achieve voting AND the synchronized emission:

  1. Each ``Participant.achieve(label)`` call models the
     federate's voting intent.
  2. The runner explicitly drives every required peer's
     ``achieve(label)``.
  3. After every peer has voted, the runner calls
     ``Participant.mark_synchronized(label)`` on each peer to
     model the synchronized callback.
  4. Only then does the runner enter the running phase.

The test ``test_sync_points_phase_ordering_is_canonical`` asserts
the runner's phase log shows the strict ordering this gating
implies.

## Why not use ``Federate.sync`` directly?

Two reasons:

1. **Memory transport.** ``Federate.sync`` requires a real gRPC
   channel; the in-process ``memory://`` transport raises
   ``RuntimeError`` from the accessor. See
   ``Federate._require_channel`` in ``rti1516e/connection.py``.

2. **No callback even on real gRPC.** Even on a cross-process
   ``rtid``-backed run, the ``federationSynchronized`` callback
   has no wire variant in M12, so the federate would still need
   to gate on something other than the event stream. The M12
   test gates on "no error" — that's a placeholder for what cut-4
   makes a real event.

## Run it

```bash
# From the repo root
python3 examples/pyjevsim-sync-points/runner.py

# Knobs (defaults shown)
python3 examples/pyjevsim-sync-points/runner.py \
    --running-ticks 10 \
    --verbose
```

Default-config output:

```text
runner: federates=3  labels=['start_simulation', 'end_simulation']  running_ticks=10  interactions=30  verify=ok
```

A verbose trace shows the canonical phase ordering:

```text
phase: register  label='start_simulation'
phase: achieve_loop  label='start_simulation'
phase: synchronized  label='start_simulation'
phase: running_start
  tick=  0 alpha.sent=1 beta.sent=1 gamma.sent=1
  ...
phase: running_end
phase: register  label='end_simulation'
phase: achieve_loop  label='end_simulation'
phase: synchronized  label='end_simulation'
phase: resign_all
```

## Verification invariants

1. **Every federate achieved every label exactly once, in order.**
   ``alpha.achieved == beta.achieved == gamma.achieved ==
   ['start_simulation', 'end_simulation']``.
2. **Every federate observed the synchronized callback for every
   label exactly once.** Same equality on ``synchronized``.
3. **Running-phase Tick count is exact.** Each federate emits
   exactly ``running_ticks`` Tick interactions; total wire
   ``send_interaction`` count is ``3 * running_ticks``.
4. **Phase log shows canonical ordering.** The 9-element phase
   log matches the expected bootstrap → run → teardown sequence.

## Tuning knobs

| Knob | Effect |
|---|---|
| ``--running-ticks N`` | Number of Tick interactions per federate between rendezvous points (default 10; 0 is valid — verifies "no Ticks before sync") |

## What's deferred

- **Real wire-driven federationSynchronized event.** Tracked as
  M12 deferral #1; cut-4 evolves the proto ``FederateEvent``
  oneof to include sync variants. Once that lands, the runner
  here can be replaced with one that consumes
  ``fed.events()`` and reacts to ``FederationSynchronized``
  directly.
- **Wait-for-synchronized blocking primitive.** A real federation
  bootstrap typically blocks each federate at ``waitForSynchronized``
  semantics; the in-process transport cannot model this without
  a proper scheduler. The runner-driven gating here serves the
  same purpose at the orchestration level.
- **Cross-process variant.** Same gap as the dashboard example:
  works in-process with the orchestrator-as-oracle pattern;
  cross-process needs the cut-4 wire support.
- **Partial-rendezvous semantics.** A real federation might have
  some federates as "required" and others as optional. This
  example treats every federate as required; making it
  configurable is one knob away (``required_federates`` argument
  to the achieve loop) but not exercised here.
- **register_synchronization_point with a tag.** The M12 spec
  test exercises ``tag=b"hello"``; here the registration is just
  a phase-log entry without a tag because the runner can't
  meaningfully consume one.
