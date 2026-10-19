package time

import "github.com/cbchoi/gorti/rti/internal/hla/core"

// RegulatingFederate is one entry in the LBTS computation snapshot.
// Pure value type — no pointers — so callers can pass slice-of-value
// without aliasing concerns.
type RegulatingFederateOf[T core.LogicalTimeScalar] struct {
	Handle    core.FederateHandle
	Time      T
	Lookahead T
	Unbounded bool
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
func LBTSOf[T core.LogicalTimeScalar](set []RegulatingFederateOf[T]) T {
	return horizonScalar(LBTSBoundOf(set))
}

// LBTSBoundOf keeps Integer64 final time distinct from an unbounded horizon.
func LBTSBoundOf[T core.LogicalTimeScalar](set []RegulatingFederateOf[T]) core.TimeBoundOf[T] {
	bound := unboundedHorizon[T]()
	for _, rf := range set {
		if !rf.Unbounded {
			contribution := addHorizon(rf.Time, rf.Lookahead)
			if !scalarNaN(contribution.Limit) {
				bound = minimumHorizon(bound, contribution)
			}
		}
	}
	return bound
}

func (m *ManagerOf[T]) lbtsBound(set []RegulatingFederateOf[T]) core.TimeBoundOf[T] {
	if strategy, ok := m.opts.LBTSStrategy.(interface {
		BoundLBTS([]RegulatingFederateOf[T]) core.TimeBoundOf[T]
	}); ok {
		return strategy.BoundLBTS(set)
	}
	return core.TimeBoundOf[T]{Limit: m.opts.LBTSStrategy.LBTS(set)}
}
