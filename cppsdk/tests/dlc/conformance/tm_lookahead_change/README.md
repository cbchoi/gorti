# tm_lookahead_change — modifyLookahead propagates to GALT

**Spec:** IEEE 1516.1-2010 §8.19 (`modifyLookahead`), §8.16 (`queryGALT`), §8.20 (`queryLookahead`).

**API surface:** bool-return plus `LogicalTime` out-parameter for
`queryGALT`, `LogicalTimeInterval` out-parameter for `queryLookahead`,
and a `LogicalTimeInterval` argument for `modifyLookahead`.

## Scenario

- **regulator** joins, enables regulation with lookahead **2.0**, advances to t=1.0, calls `modifyLookahead(0.5)`, advances to t=2.0.
- **observer** is constrained-only; samples GALT via `queryGALT` three times (after-enable, after-first-advance, after-modify).

The observer's golden shows GALT shifting **2.0 → 3.0 → 2.5**:
- After enable: 0 + lookahead(2.0) = **2.0**
- After regulator reaches t=1.0: 1.0 + lookahead(2.0) = **3.0**
- After regulator `modifyLookahead(0.5)`: 1.0 + lookahead(0.5) **(because modify was issued while at t=1)** = **2.5**

That third value is the witness: the observer sees the regulator's *new* lookahead propagated through GALT (per §8.19), not the old 2.0.

## Query API shape

`queryGALT` returns **bool** (whether GALT is defined) and writes the
time to an **out-param**. `queryLookahead` similarly takes a
`LogicalTimeInterval&` out-param. These signatures let undefined GALT be
reported independently from the output value.

## Files

- `federate_regulator.cpp`
- `federate_observer.cpp`
- `federation.fom.xml`
- `expected.regulator.log`
- `expected.observer.log`
- `test_tm_lookahead_change.cpp`

## Status

Regulator **FULL 9/9**; observer **FULL 7/7** — fixture total **FULL
16/16**. Both roles must be diff-identical to the canonicalized,
spec-derived goldens, so this is SPEC-FULL.

`rti/internal/time/lookahead.go` does not floor the advance target at
currentTime+lookahead; `checkAdvanceTarget` requires target >= currentTime,
per §8.10/§8.8 —
lookahead constrains OUTGOING TSO timestamps, not advance targets).
TAR(1.0) under lookahead=2.0 is accepted, so:

- regulator: `REG: GRANT time=1.000000` is present (9/9);
- observer: `after-first-advance` probe reads GALT 3.000000 = regulator
  t=1 + lookahead 2.0 (7/7).

The regulator's fixture-side try/catch around TAR(1.0) is dormant because
the call does not throw.

### Golden semantics

`expected.regulator.log` expects `galt_defined=0 galt=0.000000` for
`STATE phase=after-enable` and `phase=after-modify`. IEEE 1516.1-2010 §8.16
defines GALT relative to the time-stamped messages OTHER regulating
federates can send — a federate's own regulation is excluded, so the
SOLE regulating federate (the observer here is constrained-only) has
undefined GALT and §8.16 queryGALT must return false. A self-inclusive
GALT would be incorrect. gorti implements the
self-exclusive spec semantics (verified: solo regulator run returns
defined=0; the constrained observer simultaneously sees defined=1
value=2.000000). The observer golden values 2.0/3.0/2.5 are
self-consistent with spec semantics.

### Fixture behavior

- Regulator wraps TAR(1.0) in try/catch so the remainder of the run is
  captured if a regression makes the call throw; the golden GRANT line
  remains required.
- Wall-clock pacing: regulator holds 1.5 s/1.5 s/3 s between steps and
  the launcher gates the observer's start on the regulator's
  after-enable STATE, so the observer's three probes land in the
  intended windows.
- Evoke-drain (`evokeMultipleCallbacks(0.05, 0.1)`) in all wait loops.
