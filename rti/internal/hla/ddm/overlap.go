package ddm

// regionsOverlap reports whether ANY publisher region overlaps ANY
// subscriber region (FR-DDM-5).
//
// IEEE 1516.1-2010 9.1.4: explicit regions must share at least one
// dimension, and every shared dimension must have overlapping ranges.
// Dimensions present on only one side do not constrain the overlap test.
//
// Cut-2 algorithm: O(P * S * D) double-loop over publisher × subscriber
// region pairs. Adequate for the M10 acceptance bar (size 25 federation
// with 100 regions; see engineering/verification/performance-milestones.md). An interval-tree
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
// iff at least one dimension is shared and all shared ranges overlap.
// The default region is handled by callers, not an empty explicit region.
func regionPairOverlap(a, b regionBounds) bool {
	shared := false
	for d, ra := range a {
		rb, ok := b[d]
		if !ok {
			continue
		}
		shared = true
		if !ra.Overlap(rb) {
			return false
		}
	}
	return shared
}
