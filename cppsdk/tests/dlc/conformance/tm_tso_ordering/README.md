# tm_tso_ordering — TSO strict order within identical logical time

**Spec:** IEEE 1516.1-2010 §8.13 (`timeAdvanceGrant`), §8.14 (TSO delivery semantics), §8.15 (canonical tie-break for messages with identical timestamps), §6.12 (TSO `sendInteraction`), §6.13 (TSO `receiveInteraction`).

**Owns catalogue rows:** 4.21 (`receiveInteraction` 3-overload set with TSO+retract variants), 17.1 (mandatory `theUserSuppliedTag` on TSO sends).

## Why this fixture is critical

Per **§5.2.1 of `docs/DLC_COMPLIANCE_PROGRAM.md`** and **§17.1 of `docs/DLC_DIVERGENCE_CATALOGUE.md`**, the canonicalization library distinguishes RO from TSO:

| Order mode | Within-LBTS bucket | Canonicalization treatment |
|---|---|---|
| RO (`RECEIVE`)    | RTI-defined | log_diff MAY sort the bucket |
| TSO (`TIMESTAMP`) | spec-mandated | log_diff MUST NOT sort the bucket |

If the diff harness mistakenly re-sorted TSO buckets (the easy thing to do for "deterministic comparison"), it would mask genuine TSO order bugs — exactly the kind of correctness regression the M31 lockfile must prevent. This fixture **locks the harness's TsoStrict mode** by exercising the only scenario where strict vs sorted produces a visibly different log: three publishers sending at identical logical time.

## Scenario

- **alice / bob / carol** are three identical publishers, each sending one `Tick(seq=1, source=$name)` at logical time **1.0**.
- **sub** is a single constrained subscriber that joins first, NER-walks to t=2.0, and records every `receiveInteraction` callback.

Subscriber golden shows three `RECV` lines with `time=1.000000`, in spec-canonical order: **alice → bob → carol** (per §8.15: ascending FederateHandle; publishers join in that order via test driver sequencing).

## Test driver coordination

The driver spawns the subscriber first (so it owns the federation create) and waits for its `JOIN` log line before launching publishers in the canonical name order. This pins the FederateHandle assignment order, which pins the §8.15 tie-break sort.

## Files

- `federate_publisher.cpp` — parameterized publisher (one binary, three runs)
- `federate_subscriber.cpp`
- `federation.fom.xml`
- `expected.{alice,bob,carol,subscriber}.log`
- `test_tm_tso_ordering.cpp`

## gorti parity status (M35, parity-CE)

Publishers alice/bob/carol **FULL 5/5 each**; subscriber **PARTIAL 5/9**
(+1 extra line). Captured run:
`gorti-captured.{alice,bob,carol,subscriber}.log` (canonicalized).
Golden is spec-derived (>2 federates exceeds the Pitch Free cap).

Subscriber diff vs golden:
- missing `SUB: RECV ... source={alice,bob,carol} order=TIMESTAMP` (3) —
  the §8.15 strict-TSO-order assertion, THE point of this fixture, is
  unobservable under gorti today;
- missing `SUB: GRANT time=2.000000`, extra `SUB: GRANT time=1.000000`.

Three distinct M17 gaps, in causal order:

1. **TSO sends degrade to RO** (same root cause as tm_ner_pair, see its
   README for the full trace): `cppsdk/src/dlc/RTIambassadorImpl.cpp:818`
   discards `theTime`; the M17 client never sets
   `SendInteractionRequest.logical_time`. The three Ticks are delivered
   immediately as RO through the plain 6-arg `receiveInteraction`
   overload, so no TIMESTAMP RECVs and no server-side TSO queue to order.
   Until fixed, gorti's §8.15 within-bucket FederateHandle tie-break
   (which its deterministic tryGrantPending handle-sorted iteration —
   advance.go candidateGrants, NFR-DET-1 — is well positioned to honour)
   cannot be witnessed.
2. **NER forced grant at LBTS** (documented interim semantics,
   `rti/internal/time/advance.go` decideGrant): the subscriber's sole
   pending NER(2.0) is force-granted at LBTS=1.0 with pending kept —
   hence the extra `GRANT time=1.000000`. (Spec §8.8 would also grant at
   1.0 here, but only WITH the three message deliveries; the golden
   models the no-pending-message case as a single full grant at 2.0.)
3. **Resign never re-evaluates pending grants**:
   `rti/internal/time/manager.go` OnFederateResign deletes the resigned
   regulator's state but does not re-run tryGrantPending, so the kept
   pending NER(2.0) is never re-granted after the last publisher
   resigns (LBTS -> +Inf) — the walk cannot reach t=2. The fixture
   bounds its drain loop (~6 s) to capture this without hanging.

Fixture-side changes (no golden edits): 2 s pre-send dwell in the
publishers so the launcher can start the subscriber before the T=1.0
sends; evoke-drain everywhere; bounded NER drain per gap 3.

## M36 agent-DA re-verdict — subscriber 5/9 -> 8/9; §8.15 tie-break WITNESSED

Gap 1 above is CLOSED (DA-1: LogicalTime threads DLC -> M17Bridge ->
wire; delivery invokes the 9-arg retraction-handle TSO overload). The
capture now shows all three

    SUB: RECV interaction=Tick time=1.000000 seq=1 source={alice,bob,carol} order=TIMESTAMP

in exact ascending-FederateHandle order — the §8.15 within-bucket
tie-break, THE point of this fixture, is witnessed for the first time.
Publishers stay FULL 5/5 each (their SENDs are now real TSO wire sends).

Residual (Go server, Agent DB):

- `SUB: GRANT time=1.000000` precedes the three RECVs — same
  `emitGrant` ordering defect as tm_ner_pair
  (rti/internal/time/ner.go:347-381 sends the TimeAdvanceGrant at :358
  BEFORE `releaseBufferedTSO` at :379; §8.14 requires delivery first).
- `SUB: GRANT time=2.000000` still missing — gap 3 above
  (OnFederateResign never re-runs tryGrantPending; LBTS -> +Inf after
  the last publisher resigns but the kept pending NER(2.0) is never
  re-granted). Both single-site fixes in rti/internal/time.
