# ddm_region_mod_in_flight — region modify mid-stream fires scope advisories

**Spec:** IEEE 1516.1-2010 §9.5 (`registerObjectInstanceWithRegions`), §9.6 (`associateRegionsForUpdates` / `commitRegionModifications` shape), §6.17 (`attributesInScope`), §6.18 (`attributesOutOfScope`), §6.27 (`enableAttributeScopeAdvisorySwitch`), §10.30 (`setRangeBounds`).

**Owns catalogue rows:** 4.23 (`attributesInScope` / `attributesOutOfScope` callbacks), 10.4 (`associateRegionsForUpdates` pair-vector), 13.10 (`enableAttributeScopeAdvisorySwitch` — 8 advisory toggles).

## Scenario

- **publisher** registers `sensor-1` with region `Channel=[40,60]` and emits 6 RO `Value` updates ~50 ms apart.
- **subscriber** starts subscribed with region `Channel=[0,50]` (overlap with publisher = `[40,50]`). After ~180 ms (i.e. roughly between updates 3 and 4) it shrinks its region to `Channel=[0,30]` and commits — overlap with publisher (`[40,60]`) is now empty.

Expected log shape:

1. `IN_SCOPE` advisory fires once after discover (overlap exists).
2. `REFLECT Value=1..3` arrive.
3. Subscriber commits the shrunk region.
4. `OUT_OF_SCOPE` advisory fires (overlap dropped).
5. Reflects 4..6 are filtered out — subscriber log has none past the modify.

## Why this fixture exists

Catalogue **row 4.23** flags `attributesInScope` / `attributesOutOfScope` as absent in M17. The spec uses these advisories specifically when a region's modification crosses an overlap boundary (§6.17-§6.18). This fixture is the only one in the M31 suite that crosses an in-flight boundary, so it locks the timing of the advisory fire.

`enableAttributeScopeAdvisorySwitch` is one of 8 advisory-switch enable methods (catalogue 13.10) that M17 lacks entirely; the fixture exercises one of them to keep that surface alive in the lockfile.

## Timing tolerance

The driver does not assert exact interleave — only that:
- All 6 publisher UPDATE lines appear (in `expected.publisher.log`).
- Subscriber sees `IN_SCOPE` then ≥1 reflect, then `MODIFY_REGION`, then `OUT_OF_SCOPE`.
- No `REFLECT` appears in the subscriber log after `OUT_OF_SCOPE`.

Real Pitch capture (TASK-350) will record the actual witnessed sequence; `scripts/check-spec-traceability.sh` (TASK-362) validates the spec cites.

## Files

- `federate_publisher.cpp`
- `federate_subscriber.cpp`
- `federation.fom.xml`
- `expected.publisher.log`
- `expected.subscriber.log`
- `test_ddm_region_mod_in_flight.cpp`
