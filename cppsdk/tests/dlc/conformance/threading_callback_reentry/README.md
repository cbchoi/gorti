# threading_callback_reentry — ambassador re-entry inside a callback must throw

**Spec:** IEEE 1516.1-2010 §10 (Support Services) — RTI services are not re-entrant from callback context. **`docs/DLC_DIVERGENCE_CATALOGUE.md §17.2`** — `CallNotAllowedFromWithinCallback` is the spec-mandated exception class for this case.

**Owns catalogue rows:** **17.2** (`CallNotAllowedFromWithinCallback` exception class + the runtime check that fires it). Also exercises 4.20 (reflect callback shape) and 11.3 (`updateAttributeValues` with mandatory tag).

## Why this fixture is critical

This is the **only fixture in the M31 suite** that exercises catalogue row 17.2. Other fixtures lock the exception class via `static_assert` lockfile tests, but only this one watches the RTI runtime actually throw it.

Without a runtime witness, an implementation could ship the exception class as a static type and never raise it — passing every lockfile while violating the spec. The fixture catches that drift.

## Scenario

- **publisher** registers `car-1`, sends one RO `Position=42.0` update, resigns.
- **subscriber** subscribes `Vehicle.Position`. Inside its `reflectAttributeValues` callback, it calls `amb->updateAttributeValues(theObject, theAttributeValues, theUserSuppliedTag)` — a re-entry attempt.

Expected RTI behavior (per §10): the ambassador throws `rti1516e::CallNotAllowedFromWithinCallback`. The subscriber catches it, logs `CAUGHT exception=CallNotAllowedFromWithinCallback`, and continues normally to resign.

## What the golden enforces

| Subscriber golden line | Spec sentence |
|---|---|
| `REFLECT Position=...` | §6.11 — callback fires |
| `ATTEMPT_REENTRY service=updateAttributeValues` | (test harness witness — federate is about to call amb) |
| `CAUGHT exception=CallNotAllowedFromWithinCallback` | §10 + catalogue 17.2 — exact spec exception type |
| `RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST` | §4.10 — re-entry guard does not put ambassador into bad state; subsequent (non-reentered) call succeeds |

A run producing `CAUGHT exception=UNEXPECTED(...)` is a bug. A run with no `CAUGHT` line at all (the re-entry call returned successfully) is a worse bug.

## Files

- `federate_publisher.cpp`
- `federate_subscriber.cpp` (the actual subject of the test)
- `federation.fom.xml`
- `expected.publisher.log`
- `expected.subscriber.log`
- `test_threading_callback_reentry.cpp`
