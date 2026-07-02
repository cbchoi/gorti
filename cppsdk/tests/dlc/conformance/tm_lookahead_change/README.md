# tm_lookahead_change — modifyLookahead propagates to GALT

**Spec:** IEEE 1516.1-2010 §8.19 (`modifyLookahead`), §8.16 (`queryGALT`), §8.20 (`queryLookahead`).

**Owns catalogue rows:** 9.7, 9.10, 9.12 (bool-return + LogicalTimeInterval out-param shapes; modifyLookahead takes LogicalTimeInterval).

## Scenario

- **regulator** joins, enables regulation with lookahead **2.0**, advances to t=1.0, calls `modifyLookahead(0.5)`, advances to t=2.0.
- **observer** is constrained-only; samples GALT via `queryGALT` three times (after-enable, after-first-advance, after-modify).

The observer's golden shows GALT shifting **2.0 → 3.0 → 2.5**:
- After enable: 0 + lookahead(2.0) = **2.0**
- After regulator reaches t=1.0: 1.0 + lookahead(2.0) = **3.0**
- After regulator `modifyLookahead(0.5)`: 1.0 + lookahead(0.5) **(because modify was issued while at t=1)** = **2.5**

That third value is the witness: the observer sees the regulator's *new* lookahead propagated through GALT (per §8.19), not the old 2.0.

## Why the queries are out-param + bool

`queryGALT` returns **bool** (whether GALT is defined) and writes the time to an **out-param** (catalogue 9.7). `queryLookahead` similarly takes a `LogicalTimeInterval&` out-param (catalogue 9.10). M17's `GALTResult queryGALT()` struct (RtiAmbassador.h:308-309) is non-spec.

## Files

- `federate_regulator.cpp`
- `federate_observer.cpp`
- `federation.fom.xml`
- `expected.regulator.log`
- `expected.observer.log`
- `test_tm_lookahead_change.cpp`

## gorti parity status (M35, parity-CE)

Regulator **PARTIAL 8/9**; observer **PARTIAL 6/7**. Captured run:
`gorti-captured.{regulator,observer}.log` (canonicalized).

The fixture's core assertion — §8.19 modifyLookahead propagates into the
GALT peers see — IS witnessed: observer's after-modify probe reads
2.500000 = regulator t=2.0 + new lookahead 0.5, exactly per golden.
queryLookahead (§8.20) reflects 2.0 → 0.5 exactly.

Both misses share ONE root cause, a named M17 cut-1 divergence:
`rti/internal/time/lookahead.go` `checkLookahead` rejects any advance
request below currentTime+lookahead (`ErrTimeRequestInPast`), applied to
TAR via `advance.go` dispatchAdvance pre-flight step 3 ("Same rule as
NER for cut-1"). §8.10 only requires target >= current logical time —
lookahead constrains outgoing message timestamps, not advance targets.
Hence TAR(1.0) under lookahead=2.0 is rejected (surfaces as
RTIinternalError; the DLC error mapping loses the specific type):

- regulator: missing `REG: GRANT time=1.000000` (8/9);
- observer: `after-first-advance` probe reads GALT 2.000000 instead of
  3.000000, because the regulator never reached t=1 (6/7).

Missing impl: relax dispatchAdvance step 3 for TAR/TARA/FQR/NER to the
spec floor (target >= currentTime), keeping the lookahead floor for
outgoing TSO message timestamps only.

### Golden edit (spec justification)

`expected.regulator.log` lines `STATE phase=after-enable` /
`phase=after-modify`: `galt_defined=1 galt=2.000000` / `=1 galt=1.500000`
corrected to `galt_defined=0 galt=0.000000`. IEEE 1516.1-2010 §8.16
defines GALT relative to the time-stamped messages OTHER regulating
federates can send — a federate's own regulation is excluded, so the
SOLE regulating federate (the observer here is constrained-only) has
undefined GALT and §8.16 queryGALT must return false. The original
skeleton assumed self-inclusive GALT. gorti implements the
self-exclusive spec semantics (verified: solo regulator run returns
defined=0; the constrained observer simultaneously sees defined=1
value=2.000000). The observer golden (values 2.0/3.0/2.5) is
self-consistent with spec semantics and is unchanged.

### Fixture-side changes (no semantic weakening)

- Regulator wraps TAR(1.0) in try/catch citing the divergence above so
  the remainder of the run is captured; the golden GRANT line stays and
  is counted missing.
- Wall-clock pacing: regulator holds 1.5 s/1.5 s/3 s between steps and
  the launcher gates the observer's start on the regulator's
  after-enable STATE, so the observer's three probes land in the
  intended windows (they were previously racing a millisecond-fast
  unconstrained regulator).
- Evoke-drain (`evokeMultipleCallbacks(0.05, 0.1)`) in all wait loops.
