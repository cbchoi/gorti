# ddm_region_mod_in_flight — region modify mid-stream fires scope advisories

**Spec:** IEEE 1516.1-2010 §9.5 (`registerObjectInstanceWithRegions`), §9.6 (`associateRegionsForUpdates` / `commitRegionModifications` shape), §6.17 (`attributesInScope`), §6.18 (`attributesOutOfScope`), §6.27 (`enableAttributeScopeAdvisorySwitch`), §10.30 (`setRangeBounds`).

**API surface:** `attributesInScope` / `attributesOutOfScope` callbacks,
the `associateRegionsForUpdates` pair-vector, and
`enableAttributeScopeAdvisorySwitch`.

## Scenario

- **publisher** registers `sensor-1` with region `Channel=[40,60]` and emits 6 RO `Value` updates ~50 ms apart.
- **subscriber** starts subscribed with region `Channel=[0,50]` (overlap with publisher = `[40,50]`). After ~180 ms (i.e. roughly between updates 3 and 4) it shrinks its region to `Channel=[0,30]` and commits — overlap with publisher (`[40,60]`) is now empty.

Expected log shape:

1. `IN_SCOPE` advisory fires once after discover (overlap exists).
2. `REFLECT Value=1..3` arrive.
3. Subscriber commits the shrunk region.
4. `OUT_OF_SCOPE` advisory fires (overlap dropped).
5. Reflects 4..6 are filtered out — subscriber log has none past the modify.

## Purpose

The spec uses `attributesInScope` and `attributesOutOfScope` specifically
when a region modification crosses an overlap boundary (§6.17-§6.18).
This fixture crosses that boundary in flight and locks the advisory
ordering.

`enableAttributeScopeAdvisorySwitch` is one of the eight advisory-switch
enable methods. The fixture exercises it together with the runtime scope
callbacks.

## Timing tolerance

The driver does not assert exact interleave — only that:
- All 6 publisher UPDATE lines appear (in `expected.publisher.log`).
- Subscriber sees `IN_SCOPE` then ≥1 reflect, then `MODIFY_REGION`, then `OUT_OF_SCOPE`.
- No `REFLECT` appears in the subscriber log after `OUT_OF_SCOPE`.

The exact interleave is intentionally not fixed;
`scripts/check-spec-traceability.sh` validates the spec citations.

## Files

- `federate_publisher.cpp`
- `federate_subscriber.cpp`
- `federation.fom.xml`
- `expected.publisher.log`
- `expected.subscriber.log`
- `test_ddm_region_mod_in_flight.cpp`

## Status

The fixture's core assertion — an IN-FLIGHT setRangeBounds +
commitRegionModifications changes routing mid-stream — WORKS: reflects
1..3 arrive through the [40,50] overlap, and after the shrink to [0,30]
reflects 4..6 are correctly filtered out (`update.go`
subscribersForUpdate re-evaluates the DDM overlap per update, so the
recommit takes effect immediately).

`SUB: DISCOVER name=sensor-1` arrives because
`rti/internal/object/discover.go` subscribersForDiscover unions the
declaration subscribers with the region-scoped subscribers (see
ddm_region_overlap README for the mechanism).

Per the golden header, the scenario "documents the contract, not the exact
interleave":
subscriber counts reflects and modifies its region after the 3rd
(rather than racing a fixed 180 ms pump), publisher spaces updates
150 ms apart, reservation wait uses evoke-drain.

**SPEC-FULL 28/28** (publisher 14/14, subscriber 14/14;
`_harness/run_fixture.sh ddm_region_mod_in_flight`). The §6.17/§6.18
attribute scope advisories are on the wire and bridged:
`SUB: IN_SCOPE attributes=[Value]`
fires on first overlap and `SUB: OUT_OF_SCOPE attributes=[Value]`
fires after the in-flight shrink to [0,30], with no reflects after.
No residual.
