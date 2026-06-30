# tm_tar_tara_fqr_nmra — all 4 advance primitives in one federate

**Spec:** IEEE 1516.1-2010 §8.8 (`nextMessageRequest`), §8.9 (`nextMessageRequestAvailable`), §8.10 (`timeAdvanceRequest`), §8.11 (`timeAdvanceRequestAvailable`), §8.12 (`flushQueueRequest`), §8.13 (`timeAdvanceGrant`).

**Owns catalogue rows:** 9.6 (`LogicalTime const&` parameter on every advance call — replaces M17's `double`).

## Scenario

A single federate (`walker`) joins solo, enables regulation+constrained (lookahead 1.0), then walks the five advance primitives in fixed order:

| Step | Primitive | Spec § | Target | Expected grant |
|---|---|---|---|---|
| 1 | TAR  | §8.10 | 1.0 | 1.0 |
| 2 | TARA | §8.11 | 2.0 | 2.0 |
| 3 | FQR  | §8.12 | 3.0 | 3.0 |
| 4 | NMR  | §8.8  | 4.0 | 4.0 |
| 5 | NMRA | §8.9  | 5.0 | 5.0 |

Since the federate is solo (no other regulators), each request reaches GALT immediately and the grant equals the target. With identical lookahead (1.0) the targets are strictly monotonic — same scenario verifies parity across primitives.

## Why this fixture exists

Catalogue row **9.6** is BLOCKING: M17's `timeAdvanceRequest(double)` (RtiAmbassador.h:279-290) violates the spec. The fixture locks the **shape of the call site** (the federate passes a `HLAfloat64Time const&` rather than a raw `double`) and the **shape of the callback** (grant arg is `LogicalTime const&` per catalogue 4.35). Compile-time enforcement happens in the lockfile TU `cppsdk/tests/dlc/lockfile/core/test_rtiambassador_time.cpp` (Agent A). The fixture is the runtime witness.

## Files

- `federate_walker.cpp`
- `federation.fom.xml`
- `expected.walker.log`
- `test_tm_tar_tara_fqr_nmra.cpp`
