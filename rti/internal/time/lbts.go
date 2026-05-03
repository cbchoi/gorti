package time

import "github.com/cbchoi/gorti/rti/internal/core"

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
// Determinism: tie-break by FederateHandle ascending. Callers iterate
// the set in handle-sorted order to ensure reproducible behavior across
// runs (NFR-DET-1).
//
// FROZEN-SHAPE: Agent A implements per the algorithm; tests in
// rti/spec/M3/lbts_test.go drive it as a property over random sets.
func LBTS(set []RegulatingFederate) core.LogicalTime {
	_ = set
	return core.PositiveInfinity
}
