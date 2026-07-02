# tm_ner_pair — NER cycle between regulator and constrained federate

**Spec:** IEEE 1516.1-2010 §8.8 (`nextMessageRequest`); §8.2-§8.6 (regulation/constrained enable+ack); §8.13 (`timeAdvanceGrant`); §6.12 (TSO `sendInteraction`); §6.13 (TSO `receiveInteraction`).

**Owns DLC catalogue rows:** 4.33, 4.34, 4.35 (async time enable callbacks, grant signature), 9.6 (`nextMessageRequest(LogicalTime const&)`), 9.11 (`enableTimeRegulation(LogicalTimeInterval)` async), 11.4/17.1 (mandatory tag on `sendInteraction`).

## Scenario

Two federates join the same execution:

- **regulator** publishes `Tick`, enables regulation (lookahead=1.0), then for `t = 1..5` issues `nextMessageRequest(t)`. On each grant it sends `Tick(seq=t)` with `theTime = t` (TSO).
- **constrained** subscribes to `Tick`, enables constrained, then mirrors the NER walk. It records every `receiveInteraction` callback with the LogicalTime delivered.

Termination: both federates resign with `CANCEL_THEN_DELETE_THEN_DIVEST` (§4.10); the regulator then attempts a best-effort `destroyFederationExecution`.

## Why the goldens enforce per-event spec sentences

Each line of `expected.regulator.log` / `expected.constrained.log` carries a `# §N.M` cite. The traceability lint (`scripts/check-spec-traceability.sh`, TASK-362) verifies every event line maps back to a spec sentence — so reviewers can tell "this is what the spec says" vs. "this is what Pitch happened to do".

Key invariants the goldens lock:

| Golden line | Spec sentence it enforces |
|---|---|
| `TIME_REGULATION_ENABLED` callback fires after `enableTimeRegulation` returns | §8.3 — enable is async; ack arrives via callback (catalogue 4.33). |
| `TIME_CONSTRAINED_ENABLED` callback before any subsequent grant | §8.6 — must precede the first grant (catalogue 4.34). |
| Constrained `RECV interaction=Tick time=N` **before** `GRANT time=N` | §8.14 — TSO messages with timestamp T are delivered AT logical time T, not after the grant; delivery order within a logical-time bucket is RTI-defined for RO but TSO is strict (§5.2.1 of DLC_COMPLIANCE_PROGRAM.md). |
| Grant timestamp equals NER request timestamp (since both feds NER to same T) | §8.13 — grant value is the new federate time. |
| `RECV` `order=TIMESTAMP` (not `RECEIVE`) | §6.13 — TSO overload was selected by RTI because the message was sent TSO and the receiver is constrained. |

## Run shape (post-impl, M32+)

```
$ ctest -L conformance -R conformance_tm_ner_pair --output-on-failure
```

In M31 the test fails to link with `undefined reference to rti1516e::*` — by design.

## Files

- `federate_regulator.cpp` — DLC-strict regulator federate
- `federate_constrained.cpp` — DLC-strict constrained federate
- `federation.fom.xml` — HLA-Evolved-strict FOM with `<switches>`
- `expected.regulator.log` — golden skeleton (`TBD-pitch-capture`)
- `expected.constrained.log` — golden skeleton (`TBD-pitch-capture`)
- `test_tm_ner_pair.cpp` — gtest driver (uses Agent C's `_harness/`)

## gorti parity status (M35, parity-CE)

Regulator **FULL 15/15**; constrained **PARTIAL 10/15**. Captured run:
`gorti-captured.{regulator,constrained}.log` (canonicalized).

Missing events (exactly five): the §6.13 TSO deliveries
`CON: RECV interaction=Tick time=N.000000 order=TIMESTAMP`, N=1..5.
Everything else — §8.5/§8.6 constrained enable, all five §8.8 NER
grants at exact targets, resign — matches.

### TSO-delivery analysis (why the RECVs are missing)

NOT an evoke-drain problem (all wait loops use
`evokeMultipleCallbacks(0.05, 0.1)`) and NOT a server gap. The break is
in the C++ SDK send path — the interactions ARE delivered, invisibly:

1. **Primary break** — `cppsdk/src/dlc/RTIambassadorImpl.cpp:811-831`:
   the timed §6.12 `sendInteraction(..., LogicalTime const&)` overload
   does `(void)theTime;` (line 818) and calls the untimed 3-arg
   `M17Bridge::sendInteraction` — every TSO send is degraded to RO.
2. **Structural** — `cppsdk/src/RtiAmbassador.cpp:2001-2017`: the M17
   client's only `sendInteraction` never sets
   `SendInteractionRequest.logical_time`; no timed variant exists
   (`include/rti1516e/RtiAmbassador.h:257`). `M17Bridge.cpp:346-360`
   likewise has no time parameter (and also drops the tag).
3. **Server is ready** — `rti/internal/object/interaction.go:113-172`:
   `fanoutReceive` has a working TSO gate
   (`ShouldDeliverNow`/`BufferTSO`, released by NextMessageRequest) but
   sees `ts == nil` and takes the immediate-RO branch (line 167),
   omitting `ReceiveInteraction.LogicalTime` on the wire (126-129).
4. **Visible symptom** — `cppsdk/src/dlc/FederateAmbassadorBridge.cpp:204-223`:
   with no wire timestamp it invokes the plain 6-arg
   `receiveInteraction` (line 220), not the 9-arg TSO overload the
   golden mandates — so the constrained federate receives the Ticks as
   RO through an overload this fixture (correctly) does not override.

Also implied: the constrained side's NER grants go to the full target
(no message-time truncation), consistent with an empty TSO queue.

Missing impl (out of parity-CE scope, `src/dlc` frozen): thread
LogicalTime through M17 client → M17Bridge → DLC `sendInteraction`;
the Go TSO gate and the 9-arg delivery path then engage as-is.

### Fixture-side changes (no golden edits)

- Regulator tolerates `FederationExecutionAlreadyExists` (§4.5 create is
  launcher-order idempotent, same as the constrained side).
- Constrained gates its NER loop on §8.16 `queryGALT` becoming defined
  (= a regulating federate exists); without it the constrained side is
  granted instantly before the regulator enables regulation.
- All wait loops use the §10.42 evoke-drain pattern.
