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
