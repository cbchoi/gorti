// Package ddm implements IEEE 1516.1-2010 §6 Data Distribution
// Management — routing spaces + regions + region-scoped publish/
// subscribe.
//
// M10 deliverable. FROZEN-shape per docs/srs.md FR-DDM-1..6.
//
// Conceptual model:
//
//   - A "routing space" is a multi-dimensional coordinate space declared
//     in the FOM (1516.2-2010 Annex A <dimensions> + <dimension>).
//   - A "region" is a per-dimension axis-aligned hyperrectangle within
//     a routing space. Federates create regions and modify their bounds.
//   - "subscribeWithRegions" scopes a subscription to a set of regions:
//     the federate only receives updates whose publisher's region(s)
//     overlap with at least one subscribed region.
//   - "updateAttributeValuesWithRegions" / "registerObjectInstanceWithRegions"
//     associate publisher-side regions with the published data; the RTI
//     filters at the producer's overlap-test boundary.
//
// Performance contract (FR-DDM-6): DDM filtering MUST NOT make non-DDM
// workloads slower (zero-cost when no regions are in play). Workload
// baseline at federation size 25 with 100 regions recorded in
// docs/reports/M10/agent-a.md.
//
// Spec test contract: rti/spec/M10/.
package ddm
