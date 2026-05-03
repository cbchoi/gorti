package m3spec

import (
	"math"
	"sort"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

// TestSpec_M3_LBTS_EmptySet_ReturnsPositiveInfinity: with no regulating
// federates, LBTS = +Inf (no advance constraint).
//
// Implements: FR-TM-3.
func TestSpec_M3_LBTS_EmptySet_ReturnsPositiveInfinity(t *testing.T) {
	got := timepkg.LBTS(nil)
	if !math.IsInf(float64(got), 1) {
		t.Errorf("LBTS([]) = %v, want +Inf", got)
	}
}

// TestSpec_M3_LBTS_SingleFederate: with one regulator at Time=10,
// Lookahead=2, LBTS = 12.
//
// Implements: FR-TM-3.
func TestSpec_M3_LBTS_SingleFederate(t *testing.T) {
	got := timepkg.LBTS([]timepkg.RegulatingFederate{
		{Handle: 1, Time: 10.0, Lookahead: 2.0},
	})
	if got != core.LogicalTime(12.0) {
		t.Errorf("LBTS = %v, want 12.0", got)
	}
}

// TestSpec_M3_LBTS_MinAcrossFederates: LBTS is min(time + lookahead).
//
// Implements: FR-TM-3.
func TestSpec_M3_LBTS_MinAcrossFederates(t *testing.T) {
	cases := []struct {
		name string
		set  []timepkg.RegulatingFederate
		want core.LogicalTime
	}{
		{
			"two regulators, second wins",
			[]timepkg.RegulatingFederate{
				{Handle: 1, Time: 5.0, Lookahead: 4.0},  // 9.0
				{Handle: 2, Time: 7.0, Lookahead: 1.0},  // 8.0
			},
			8.0,
		},
		{
			"three regulators, mixed lookaheads",
			[]timepkg.RegulatingFederate{
				{Handle: 1, Time: 0.0, Lookahead: 1.0},  // 1.0
				{Handle: 2, Time: 0.5, Lookahead: 2.0},  // 2.5
				{Handle: 3, Time: 0.0, Lookahead: 0.5},  // 0.5  ← min
			},
			0.5,
		},
		{
			"all at same time + lookahead → that value",
			[]timepkg.RegulatingFederate{
				{Handle: 1, Time: 3.0, Lookahead: 1.5},
				{Handle: 2, Time: 3.0, Lookahead: 1.5},
				{Handle: 3, Time: 3.0, Lookahead: 1.5},
			},
			4.5,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := timepkg.LBTS(tc.set)
			if got != tc.want {
				t.Errorf("LBTS = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSpec_M3_LBTS_OrderIndependent: input order does not affect the
// result. Determinism (NFR-DET-1) requires LBTS to be a pure function of
// the set, not the slice ordering.
//
// Implements: FR-TM-3, NFR-DET-1.
func TestSpec_M3_LBTS_OrderIndependent(t *testing.T) {
	base := []timepkg.RegulatingFederate{
		{Handle: 7, Time: 2.0, Lookahead: 0.5},  // 2.5
		{Handle: 3, Time: 1.0, Lookahead: 1.0},  // 2.0
		{Handle: 11, Time: 0.0, Lookahead: 3.0}, // 3.0
		{Handle: 1, Time: 5.0, Lookahead: 0.5},  // 5.5
	}
	want := core.LogicalTime(2.0)

	// Original order
	if got := timepkg.LBTS(base); got != want {
		t.Errorf("orig: LBTS = %v, want %v", got, want)
	}
	// Reverse order
	rev := make([]timepkg.RegulatingFederate, len(base))
	for i, f := range base {
		rev[len(base)-1-i] = f
	}
	if got := timepkg.LBTS(rev); got != want {
		t.Errorf("reversed: LBTS = %v, want %v", got, want)
	}
	// Sorted-by-handle
	byHandle := append([]timepkg.RegulatingFederate(nil), base...)
	sort.Slice(byHandle, func(i, j int) bool { return byHandle[i].Handle < byHandle[j].Handle })
	if got := timepkg.LBTS(byHandle); got != want {
		t.Errorf("sorted: LBTS = %v, want %v", got, want)
	}
}

// TestSpec_M3_LBTS_ZeroLookaheadAllowed: lookahead = 0 is legal (HLA
// "zero-lookahead" federates) and contributes Time + 0 = Time.
//
// Implements: FR-TM-3.
func TestSpec_M3_LBTS_ZeroLookaheadAllowed(t *testing.T) {
	got := timepkg.LBTS([]timepkg.RegulatingFederate{
		{Handle: 1, Time: 5.0, Lookahead: 0.0},
		{Handle: 2, Time: 10.0, Lookahead: 1.0},
	})
	if got != core.LogicalTime(5.0) {
		t.Errorf("LBTS = %v, want 5.0 (federate 1's contribution)", got)
	}
}

// TestSpec_M3_LBTS_PositiveInfinityLookahead_HasNoEffect: a federate
// with +Inf lookahead never bounds LBTS (edge case — HLA permits +Inf
// lookahead to mark "I'll never send anything time-stamped from now"
// but LBTS is min, so other federates dominate).
//
// Implements: FR-TM-3.
func TestSpec_M3_LBTS_PositiveInfinityLookahead_HasNoEffect(t *testing.T) {
	got := timepkg.LBTS([]timepkg.RegulatingFederate{
		{Handle: 1, Time: 5.0, Lookahead: core.PositiveInfinity},
		{Handle: 2, Time: 0.0, Lookahead: 1.0}, // 1.0 ← still wins
	})
	if got != core.LogicalTime(1.0) {
		t.Errorf("LBTS = %v, want 1.0", got)
	}
}
