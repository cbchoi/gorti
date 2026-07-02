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

## gorti parity status (M36, agent-DA)

Regulator **FULL 15/15**; constrained **NEAR 15 events / 10/15 in
golden order**. Captured run:
`gorti-captured.{regulator,constrained}.log` (canonicalized).

M36 DA-1 closed the M35 parity-CE gap: the timed §6.12
`sendInteraction(..., LogicalTime const&)` now threads the
HLAfloat64Time double through `M17Bridge::sendInteractionTimed` onto
the wire's `SendInteractionRequest.logical_time`, and the delivery
bridge invokes the 9-arg retraction-handle TSO `receiveInteraction`
overload the golden mandates. All five §6.13 TSO deliveries
`CON: RECV interaction=Tick time=N.000000 order=TIMESTAMP` (N=1..5)
now appear.

### Residual (Go server — Agent DB territory)

The only remaining diff is intra-step ORDER: the golden mandates
`RECV time=N` BEFORE `GRANT time=N` (§8.14 — TSO messages at T are
delivered at T, before the grant); gorti emits the grant first:

    CON: GRANT time=1.000000
    CON: RECV interaction=Tick time=1.000000 order=TIMESTAMP

Root cause is server-side grant sequencing, not the C++ SDK (the M17
stream preserves server seq order): when the regulator's NER(N) grant
lands, the time manager immediately recomputes LBTS and grants the
constrained federate's NER(N) — but the regulator only SENDS Tick@N
after seeing its own grant, so the Tick@N reaches the server after the
constrained grant already went out; `ShouldDeliverNow` then releases it
immediately (constrained is already AT time N). Related: gorti does not
enforce the §8.1.2 lookahead floor on TSO sends (Tick@N sent from
logical time N with lookahead 1.0 would be rejected by Pitch) — the
lookahead-floor item already on the M36 Go backlog covers this.

### Fixture-side changes (no golden edits)

- Regulator tolerates `FederationExecutionAlreadyExists` (§4.5 create is
  launcher-order idempotent, same as the constrained side).
- Constrained gates its NER loop on §8.16 `queryGALT` becoming defined
  (= a regulating federate exists); without it the constrained side is
  granted instantly before the regulator enables regulation.
- All wait loops use the §10.42 evoke-drain pattern.

## M37 ED — §8.1.2 fixture fix + re-verdict (2026-07-02)

The regulator skeleton advanced FIRST (NER to t, grant) and THEN sent
Tick stamped t — from logical time t with lookahead 1.0 that is
ts < current+lookahead, illegal per §8.1.2. No server-side check
existed pre-M37, so it "passed"; the M37 EB-3 outgoing-TSO validation
correctly rejects it (InvalidLogicalTime), which deadlocked the pair.
Pitch would throw on the same call — the golden was never
Pitch-captured. Fixed to the legal NER cycle: at t-1 send Tick stamped
t (exactly current+lookahead — the legality boundary, inclusive), then
NER(t). Golden pair order amended accordingly (SEND before GRANT;
header note in expected.regulator.log).

Driver re-verdict (`_harness/run_fixture.sh tm_ner_pair`):

- vs main rtid (0ea07d1): regulator **FULL 15/15**; constrained
  **10/15** — the §8.14 GRANT-before-RECV swap on every step
  (rti/internal/time ner.go grant-before-delivery).
- vs main + M37 EB branch (EB-2 §8.14 drain-before-grant + EB-3
  validation): **constrained FULL 15/15, regulator FULL 15/15** — the
  fixture goes fully green once both the server order fix and the
  legal send pattern are in place. Committed captures are from this
  run.
