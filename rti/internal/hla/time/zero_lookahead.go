package time

import "github.com/cbchoi/gorti/rti/internal/hla/core"

func strictAdvance(mode AdvanceMode) bool { return mode == ModeTAR || mode == ModeNER }

// recordGrantBound runs before changing currentTime or clearing mode. Under
// 1516.1-2010 8.1.4, a same-time Available grant cannot erase a strict grant;
// only forward advancement replaces that history. Retain it even at positive
// lookahead because the Time Granted constraint uses the current lookahead.
func (ns *nerStateOf[T]) recordGrantBound(t T) {
	if t > ns.currentTime {
		ns.strictZeroLookahead = false
	}
	if strictAdvance(ns.mode) {
		ns.strictZeroLookahead = true
	}
}

// outgoingTSOBound implements Table 3 (8.1.6) using projected actual lookahead
// while advancing, not the requested lookahead reduction. Caller holds both
// state locks. Strict history still excludes currentTime during a same-time
// Available request; for a later request its higher base already excludes it.
func outgoingTSOBoundOf[T core.LogicalTimeScalar](fs *federateStateOf[T], ns *nerStateOf[T]) (base, lookahead T, exclusive bool) {
	lookahead = fs.lookahead
	if ns == nil {
		return
	}
	base = ns.currentTime
	exclusive = ns.strictZeroLookahead
	if ns.pendingNER {
		base = ns.requestedTime
		lookahead = fs.projectedLookahead(base)
		exclusive = strictAdvance(ns.mode) || (ns.strictZeroLookahead && base == ns.currentTime)
	}
	// +Inf is the product final-time sentinel, not a finite IEEE timestamp.
	// Preserve its existing inclusive admission separately from finite bounds.
	exclusive = exclusive && lookahead == 0 && !scalarInfinite(base)
	return
}

// regulatingBoundSnapshot adapts the explicit exclusive bound to the existing
// float64-only LBTS strategy port. At zero lookahead, the next representable
// value is the lowest permitted outgoing timestamp. This is a numeric adapter,
// not the constant LogicalTimeInterval epsilon of 12.3. Nonzero lookahead and
// the product +Inf sentinel retain their existing arithmetic. In particular,
// all-zero pending TAR/NMR requests can grant at their excluded boundary.
func regulatingBoundSnapshotOf[T core.LogicalTimeScalar](h core.FederateHandle, fs *federateStateOf[T], ns *nerStateOf[T]) RegulatingFederateOf[T] {
	base, lookahead, exclusive := outgoingTSOBoundOf[T](fs, ns)
	unbounded := false
	if exclusive {
		var possible bool
		base, possible = successor(base)
		unbounded = !possible
	}
	return RegulatingFederateOf[T]{Handle: h, Time: base, Lookahead: lookahead, Unbounded: unbounded}
}
