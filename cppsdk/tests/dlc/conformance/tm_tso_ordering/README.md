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
