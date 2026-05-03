package ddm

// regionsOverlap reports whether ANY publisher region overlaps ANY
// subscriber region (FR-DDM-5).
//
// Per IEEE 1516.1-2010 §6: two regions overlap iff every dimension
// they share has overlapping ranges. A dimension that appears in only
// ONE of the two regions is treated as a wildcard on the other side
// (the missing side covers the full range), so the per-dimension test
// degenerates to "always overlap" when one side doesn't constrain it.
//
// Cut-2 algorithm: O(P * S * D) double-loop over publisher × subscriber
// region pairs. Adequate for the M10 acceptance bar (size 25 federation
// with 100 regions; see docs/reports/M10/agent-a.md). An interval-tree
// optimization is tracked as M10 W2 follow-up if the perf numbers
// warrant it.
func regionsOverlap(pub, sub []regionBounds) bool {
	for _, p := range pub {
		for _, s := range sub {
			if regionPairOverlap(p, s) {
				return true
			}
		}
	}
	return false
}

// regionPairOverlap is the per-pair overlap test: two regions overlap
// iff every dimension they BOTH constrain has overlapping ranges. A
// dimension only one side declares is treated as full-range on the
// other side (always overlaps).
func regionPairOverlap(a, b regionBounds) bool {
	for d, ra := range a {
		rb, ok := b[d]
		if !ok {
			// Wildcard on b's side: any value of dim d is in range.
			continue
		}
		if !ra.Overlap(rb) {
			return false
		}
	}
	// Symmetry note: dimensions present in b but not in a are
	// treated as wildcards on a's side and trivially overlap. No
	// extra loop needed — the asymmetric per-dimension iteration
	// over a is sufficient because every "must-fail" condition
	// requires both sides to constrain the dimension.
	return true
}
