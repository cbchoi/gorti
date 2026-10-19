// Package m10spec contains the specification tests
// for milestone M10 — Data Distribution Management (DDM).
// See docs/srs.md §10.3 (cut 2) for the milestone gate.
//
// Cut-2 scope (FR-DDM-1..6):
//   - Routing-space declarations parsed from FOM (1516.2-2010 Annex A
//     <dimensions> + <dimension>); LookupRoutingSpace + LookupDimension
//     queries.
//   - Region lifecycle: CreateRegion → SetRangeBounds → CommitRegionModifications
//     → DeleteRegion.
//   - Region-scoped subscriptions: SubscribeObjectClassAttributesWithRegions,
//     SubscribeInteractionClassWithRegions.
//   - Region-scoped publishing: RegisterObjectInstanceWithRegions and
//     SubscribersForUpdate (the overlap-test entry point).
//   - Performance: zero-cost when no regions are in play; the baseline uses
//     federation size 25 with 100 regions.
//
// These tests preserve the observable M10 contract.
package m10spec
