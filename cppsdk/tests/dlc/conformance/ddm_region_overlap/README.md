# ddm_region_overlap — region intersection delivers updates

**Spec:** IEEE 1516.1-2010 §9.2 (`createRegion`), §9.3 (`commitRegionModifications`), §9.5 (`registerObjectInstanceWithRegions`), §9.8 (`subscribeObjectClassAttributesWithRegions`), §10.30 (`setRangeBounds`).

**API surface:** `createRegion(DimensionHandleSet)`, `RangeBounds`,
`registerObjectInstanceWithRegions` with a pair-vector,
`subscribeObjectClassAttributesWithRegions` with `active` and
`updateRate`, `commitRegionModifications(RegionHandleSet)`, and
`setRangeBounds` without a routing-space argument.

## Scenario

```
  Y
1000 ┌──────────────────────────┐
     │                          │
 750 │       ┌─────────┐        │ <— R_sub
     │       │ overlap │        │     X=[250,750]
 500 │   ┌───┼─────────┘        │     Y=[250,750]
     │   │   │                  │
 250 │   │   │                  │
     │   │ R_pub                │
   0 └───┴───┴──────────────────┘
     0  250  500              X
```

- **publisher (`pub`)** creates R_pub on dims [X, Y] with bounds `X=[0,500] Y=[0,500]`, registers `car-1` with that region, sends 3 RO updates of `Position`.
- **subscriber (`sub`)** creates R_sub with bounds `X=[250,750] Y=[250,750]`, subscribes `Vehicle.Position` with that region.

The intersection is the 250×250 square `X=[250,500] Y=[250,500]`. R_pub is non-empty there, so the RTI delivers all three publisher updates to the subscriber. (Whether values are *within* the intersection is moot — DDM filters on *region overlap*, not on attribute payload.)

## DLC API shape

- `createRegion` takes only a `DimensionHandleSet`; no routing-space
  argument is part of the IEEE 1516.1-2010 signature.
- `setRangeBounds` takes the region, dimension, lower bound, and upper
  bound without a routing-space argument.
- `subscribeObjectClassAttributesWithRegions` includes the mandatory
  `active` and `updateRate` parameters.

The fixture exercises only the DLC shapes, locking source-compat with IEEE 1516e ported federates.

## Files

- `federate_publisher.cpp`
- `federate_subscriber.cpp`
- `federation.fom.xml`
- `expected.publisher.log`
- `expected.subscriber.log`
- `test_ddm_region_overlap.cpp`

## Status

Publisher **FULL 13/13**; subscriber **FULL 11/11** against the committed
canonical goldens.

Discover fan-out is DDM-aware. `rti/internal/object/discover.go`
subscribersForDiscover unions the `Declarations.SubscribersFor`
recipients with the region-scoped subscribers, mirroring the reflect
path (`update.go` subscribersForReflect):

- publisher regions associated with the instance → overlap-tested
  recipients via `DDM.SubscribersForUpdate`;
- no publisher regions yet (the register-time case — gorti's
  register/associate split lands `associateRegionsForUpdates` AFTER
  §6.8 register) → default-region semantics via the new
  `DDM.RegionSubscribersFor` (the default region overlaps every
  subscriber region).

A federate subscribed ONLY via
subscribeObjectClassAttributesWithRegions therefore now receives the
§6.9 discoverObjectInstance that precedes its region-filtered REFLECTs.

The publisher name-reservation wait uses the §10.42 evoke-drain pattern.
