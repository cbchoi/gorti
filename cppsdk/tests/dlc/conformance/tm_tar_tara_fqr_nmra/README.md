# tm_tar_tara_fqr_nmra — all 4 advance primitives in one federate

**Spec:** IEEE 1516.1-2010 §8.8 (`nextMessageRequest`), §8.9 (`nextMessageRequestAvailable`), §8.10 (`timeAdvanceRequest`), §8.11 (`timeAdvanceRequestAvailable`), §8.12 (`flushQueueRequest`), §8.13 (`timeAdvanceGrant`).

**API surface:** `LogicalTime const&` parameter on every advance call
and `LogicalTime const&` on the grant callback.

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

## Purpose

The fixture locks the **shape of the call site**: the federate passes a
`HLAfloat64Time const&` rather than a raw `double`. It also locks the
**shape of the callback**, whose grant argument is `LogicalTime const&`.
Compile-time enforcement happens in the lockfile TU
`cppsdk/tests/dlc/lockfile/core/test_rtiambassador_time.cpp`; this fixture
is the runtime witness.

## Files

- `federate_walker.cpp`
- `federation.fom.xml`
- `expected.walker.log`
- `test_tm_tar_tara_fqr_nmra.cpp`

## Status

**FULL 15/15.** The canonicalized run must byte-match the spec-derived golden.

All five §8 advance primitives (TAR §8.10, TARA §8.11, FQR §8.12,
NMR §8.8, NMRA §8.9) delegate through the §8 wire path and
each produces a §8.13 grant at exactly the requested target
(`time=%.6f` match). §8.3/§8.6 enable acks arrive at time=0.000000 as
synthesized post-RPC callbacks.

Wait loops use the §10.42 evoke-drain pattern
(`evokeMultipleCallbacks(0.05, 0.1)`)
because sleep-only or single-evoke loops can miss stream callbacks.
