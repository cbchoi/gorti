# ddm_region_overlap — region intersection delivers updates

**Spec:** IEEE 1516.1-2010 §9.2 (`createRegion`), §9.3 (`commitRegionModifications`), §9.5 (`registerObjectInstanceWithRegions`), §9.8 (`subscribeObjectClassAttributesWithRegions`), §10.30 (`setRangeBounds`).

**Owns catalogue rows:** 10.1 (`createRegion(DimensionHandleSet)` — no routing-space arg), 10.2 (`class RangeBounds` with getters/setters), 10.3 (`registerObjectInstanceWithRegions` with pair-vector), 10.5 (`subscribeObjectClassAttributesWithRegions` with `active`+`updateRate` params), 10.9 (`commitRegionModifications(RegionHandleSet)`), 13.8 (`setRangeBounds` without routing-space arg).

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

## Why these DLC shapes matter

- M17's `createRegion(RoutingSpaceHandle, vector<DimensionHandle>)` (catalogue 10.1) carries a non-1516 `RoutingSpaceHandle`. The DLC form takes a `DimensionHandleSet` only.
- M17's `setRangeBounds(routingSpace, region, dim, lower, upper)` (catalogue 13.8) likewise carries the spurious routing-space arg.
- M17's `subscribeObjectClassAttributesWithRegions(cls, set, set)` (catalogue 10.5) is missing the `active` and `updateRate` parameters that the spec mandates.

The fixture exercises only the DLC shapes, locking source-compat with Pitch-ported federates.

## Files

- `federate_publisher.cpp`
- `federate_subscriber.cpp`
- `federation.fom.xml`
- `expected.publisher.log`
- `expected.subscriber.log`
- `test_ddm_region_overlap.cpp`

## gorti parity status (M35, parity-CE)

Publisher **FULL 13/13**; subscriber **PARTIAL 10/11**. Captured run:
`gorti-captured.{publisher,subscriber}.log` (canonicalized).

The fixture's core assertion — §9 region-filtered delivery through the
R_pub∩R_sub overlap — WORKS end-to-end: createRegion, setRangeBounds,
commitRegionModifications, registerObjectInstanceWithRegions (parity-CA
fused as §6.8 register + §9.6 associateRegionsForUpdates),
subscribeObjectClassAttributesWithRegions, and all three region-routed
REFLECTs arrive with exact values.

Missing event (exactly one): `SUB: DISCOVER name=car-1`.

Root cause — discover fanout is not DDM-aware:
`rti/internal/object/discover.go` fanoutDiscover selects recipients from
`Declarations.SubscribersFor` only, while the reflect path
(`rti/internal/object/update.go` subscribersForUpdate, M10 FR-DDM-3..6)
replaces that set with `DDM.SubscribersForUpdate` when the producer has
region associations. A federate subscribed ONLY via
SubscribeObjectClassAttributesWithRegions (ddm.Manager tables; no
declaration.Manager entry) therefore gets every region-filtered REFLECT
but never the §6.9 discoverObjectInstance that HLA requires to precede
them. Missing impl: make fanoutDiscover consult the DDM subscription
tables (or have the DDM subscribe also record a declaration
subscription) — one-line recipient-set union plus determinism-order
merge.

Fixture-side change: publisher name-reservation wait moved to the
§10.42 evoke-drain pattern. No golden edits.
