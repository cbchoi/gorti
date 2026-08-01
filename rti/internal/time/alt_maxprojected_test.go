package time

import (
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestMaxProjectedLBTS_NameAndDeterminismFlag pins the registry key and
// the determinism claim. If either changes the docs and the registry
// pre-registration in research.Default() must be updated in lockstep.
func TestMaxProjectedLBTS_NameAndDeterminismFlag(t *testing.T) {
	s := MaxProjectedLBTSStrategy()
	if got := s.Name(); got != "max-projected" {
		t.Errorf("Name(): got %q, want %q", got, "max-projected")
	}
	if !s.DeterminismPreserving() {
		t.Errorf("DeterminismPreserving(): got false, want true (max is order-independent)")
	}
}

// TestMaxProjectedLBTS_EmptySet_ReturnsPositiveInfinity mirrors the
// default LBTS contract: no regulators → no advance constraint.
func TestMaxProjectedLBTS_EmptySet_ReturnsPositiveInfinity(t *testing.T) {
	got := MaxProjectedLBTSStrategy().LBTS(nil)
	if !math.IsInf(float64(got), 1) {
		t.Errorf("LBTS(nil): got %v, want +Inf", got)
	}
}

// TestMaxProjectedLBTS_DiffersFromDefault_OnNonTrivialInput is the key
// pedagogical test: on a federation with three regulating federates at
// distinct (Time, Lookahead) projections, the default returns the min
// projected time and the alt returns the max. The two MUST disagree on
// this kind of input — that disagreement is what makes the alt useful
// for what-if studies.
func TestMaxProjectedLBTS_DiffersFromDefault_OnNonTrivialInput(t *testing.T) {
	set := []RegulatingFederate{
		{Handle: 1, Time: 10, Lookahead: 1}, // projection 11 (min)
		{Handle: 2, Time: 20, Lookahead: 2}, // projection 22
		{Handle: 3, Time: 30, Lookahead: 3}, // projection 33 (max)
	}
	def := DefaultLBTSStrategy().LBTS(set)
	alt := MaxProjectedLBTSStrategy().LBTS(set)
	if def != core.LogicalTime(11) {
		t.Errorf("default LBTS: got %v, want 11 (min projection)", def)
	}
	if alt != core.LogicalTime(33) {
		t.Errorf("max-projected LBTS: got %v, want 33 (max projection)", alt)
	}
	if def == alt {
		t.Errorf("default and max-projected agree (%v); the alt is meaningless if it never differs", def)
	}
}

// TestMaxProjectedLBTS_OrderIndependent verifies the determinism claim:
// permuting the input slice MUST yield the same LBTS. If this ever
// fails we have to flip DeterminismPreserving() to false.
func TestMaxProjectedLBTS_OrderIndependent(t *testing.T) {
	a := []RegulatingFederate{
		{Handle: 1, Time: 7, Lookahead: 0.5},
		{Handle: 2, Time: 4, Lookahead: 2},
		{Handle: 3, Time: 9, Lookahead: 1},
	}
	b := []RegulatingFederate{a[2], a[0], a[1]}
	c := []RegulatingFederate{a[1], a[2], a[0]}

	r1 := MaxProjectedLBTSStrategy().LBTS(a)
	r2 := MaxProjectedLBTSStrategy().LBTS(b)
	r3 := MaxProjectedLBTSStrategy().LBTS(c)
	if r1 != r2 || r2 != r3 {
		t.Errorf("LBTS not order-independent: %v %v %v", r1, r2, r3)
	}
	if r1 != core.LogicalTime(10) { // 9+1 is max
		t.Errorf("LBTS: got %v, want 10", r1)
	}
}
