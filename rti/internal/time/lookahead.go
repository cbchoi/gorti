package time

import "github.com/cbchoi/gorti/rti/internal/core"

// checkAdvanceTarget enforces the IEEE 1516.1-2010 §8.8 constraint on a
// time-advance request target: the requested logical time t must satisfy
//
//	t >= currentTime
//
// Lookahead does NOT constrain the advance target. Per §8.8 lookahead
// constrains a regulating federate's OUTGOING TSO message timestamps
// (sends must be >= currentTime + lookahead), not how far the federate
// may advance its own clock. Requesting an advance smaller than the
// lookahead (e.g. NER to 1.0 with lookahead 2.0 from time 0) is legal.
//
// History (M36 DB-1): the pre-M36 checkLookahead wrongly rejected any
// target below currentTime + lookahead with ErrTimeRequestInPast, which
// the tm_lookahead_change conformance fixture exposed (missing
// `REG: GRANT time=1.000000`). The federation-side causality guarantee
// never depended on that floor: a regulating federate's LBTS
// contribution is max(currentTime, pending requestedTime) + lookahead
// (see regulatingSnapshot), so peers cannot observe a regressing LBTS
// from a small advance target.
//
// Returns core.ErrTimeRequestInPast when t < currentTime. The
// comparison is strict (no epsilon); per SRS determinism rules,
// equality is granted.
func checkAdvanceTarget(currentTime, requested core.LogicalTime) error {
	if float64(requested) < float64(currentTime) {
		return core.ErrTimeRequestInPast
	}
	return nil
}
