package time

import (
	"math"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// maxProjectedLBTS is a Phase-4 reference alternative LBTSStrategy. It
// computes max(Time + Lookahead) over the regulating set instead of min,
// producing an aggressive (non-conservative) LBTS that LETS grants fire
// based on the slowest-allowed peer rather than the fastest-allowed one.
//
// # When a researcher would want this
//
// Studying optimistic / non-conservative time advance protocols. The
// classical Chandy-Misra-Bryant LBTS is a conservative LOWER bound;
// flipping the reduction operator turns it into an upper bound, which
// in HLA terms is "advance as if every peer is at its maximum promised
// time". This violates the IEEE 1516-2010 strict-HLA causality
// guarantee (a TSO message from a peer with smaller projected time can
// arrive in a federate's past), but it is a useful what-if probe for
// optimistic-rollback research, latency-vs-correctness sweeps, and
// pedagogical demonstrations of why the spec mandates min.
//
// The algorithm intentionally matches the default's signature and edge
// cases (empty set → +Inf) so a researcher can swap it in via a single
// line of TOML and immediately see how grants change in the M3/M4
// replay tests; the rejection path (or replay-skip path under
// per-impl-opt-in) demonstrates the determinism gate working AS
// configured even though this particular alt happens to preserve
// determinism.
//
// # Determinism
//
// max over IEEE-754 doubles is associative and commutative for non-NaN
// values, just like min. The result depends only on the multiset of
// (Time + Lookahead) contributions, not on slice ordering. This impl
// therefore reports DeterminismPreserving() == true: it is
// non-conservative, but it IS deterministic, so a researcher can use it
// under strict mode to study what optimistic advance would look like
// without losing replay determinism.
//
// The non-preserving illustration lives on the ownership.RandomAcquirer
// alt (alt_randomacquirer.go), which is the cleaner case because the
// non-determinism source (math/rand) is unmistakable.
type maxProjectedLBTS struct{}

// LBTS computes max(Time + Lookahead) over the regulating set. Returns
// core.PositiveInfinity for an empty set to mirror the default's
// "no regulating federates → no advance constraint" convention.
func (maxProjectedLBTS) LBTS(set []RegulatingFederate) core.LogicalTime {
	if len(set) == 0 {
		return core.PositiveInfinity
	}
	max := math.Inf(-1)
	for _, rf := range set {
		c := float64(rf.Time) + float64(rf.Lookahead)
		if c > max {
			max = c
		}
	}
	return core.LogicalTime(max)
}

// Name returns "max-projected", the registry key under which Phase 3's
// research.Default() pre-registers this alt.
func (maxProjectedLBTS) Name() string { return "max-projected" }

// DeterminismPreserving returns true: max is order-independent over
// IEEE-754 non-NaN doubles, so this alt honors NFR-DET-1.
func (maxProjectedLBTS) DeterminismPreserving() bool { return true }

// Compile-time assertion: maxProjectedLBTS satisfies LBTSStrategy.
var _ LBTSStrategy = (*maxProjectedLBTS)(nil)

// MaxProjectedLBTSStrategy returns the Phase-4 reference alt LBTS
// implementation. The research registry's Default() constructor calls
// this to register the alt under "max-projected".
func MaxProjectedLBTSStrategy() LBTSStrategy { return maxProjectedLBTS{} }
