package time

import (
	"math"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// RegulatingFederate is one entry in the LBTS computation snapshot.
// Pure value type — no pointers — so callers can pass slice-of-value
// without aliasing concerns.
type RegulatingFederate struct {
	Handle    core.FederateHandle
	Time      core.LogicalTime
	Lookahead core.LogicalTime
}

// LBTS computes min(Time + Lookahead) over the regulating set.
// Returns core.PositiveInfinity when set is empty (no regulating
// federates → no advance constraint).
//
// Determinism: the result depends only on the multiset of contributions
// (Time + Lookahead), not on slice ordering. min over IEEE-754 doubles
// is associative and commutative for non-NaN values, and a federate
// with +Inf lookahead contributes +Inf, which loses to any finite
// peer (NFR-DET-1).
//
// FROZEN-SHAPE: signature and RegulatingFederate struct are M3 contract
// (rti/spec/M3/lbts_test.go).
func LBTS(set []RegulatingFederate) core.LogicalTime {
	if len(set) == 0 {
		return core.PositiveInfinity
	}
	min := math.Inf(1)
	for _, rf := range set {
		c := float64(rf.Time) + float64(rf.Lookahead)
		if c < min {
			min = c
		}
	}
	return core.LogicalTime(min)
}
