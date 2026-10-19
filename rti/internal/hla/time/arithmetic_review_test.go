package time

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestReviewMinimumEnableTimeUsesDistinguishableArithmetic(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		current, lookahead, peer, want core.LogicalTime
		exclusive                      bool
	}{
		{"strict-at-2pow53", 1 << 53, 1, 1 << 53, 1 << 53, true},
		{"strict-epsilon", 1, core.LogicalTime(math.SmallestNonzeroFloat64), 1, 1, true},
		{"inclusive-rounded-seed", 0, core.LogicalTime(math.SmallestNonzeroFloat64), 1, core.LogicalTime(math.Nextafter(1, 0)), false},
		{"inclusive-large-rounded-seed", 0, 1, 1<<53 + 2, 1 << 53, false},
		{"inclusive-noninvertible-addition", 0, 2, 10, core.LogicalTime(math.Nextafter(8, 0)), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bound := addTimeBound(tc.want, tc.lookahead)
			if bound < tc.peer || (tc.exclusive && bound == tc.peer) {
				t.Fatal("review expected minimum is not legal")
			}
			if tc.want > tc.current {
				previous := core.LogicalTime(math.Nextafter(float64(tc.want), math.Inf(-1)))
				previousBound := addTimeBound(previous, tc.lookahead)
				if previousBound > tc.peer || (!tc.exclusive && previousBound == tc.peer) {
					t.Fatalf("predecessor %g is also legal: expected minimum is wrong", previous)
				}
			}
			got, possible := minimumEnableTime(tc.current, tc.lookahead, tc.peer, tc.exclusive)
			if !possible || got != tc.want {
				t.Fatalf("minimum enable=%g,%t want=%g (send bound %g)", got, possible, tc.want, bound)
			}
		})
	}
}

func TestReviewRegulationEnableDoesNotWaitForSpuriousRoundedAdvance(t *testing.T) {
	m, out := enableTestManager(t)
	at := core.LogicalTime(1 << 53)
	ext := extOf(m)
	// The requester is already constrained at T. One constrained peer has
	// promised no more receives at T; a separate regulator makes GALT exactly T.
	m.states.getOrCreateLocked("fed", 1).constrained = true
	ext.getOrCreateLocked("fed", 1).currentTime = at
	peer := m.states.getOrCreateLocked("fed", 2)
	peer.constrained = true
	peer.preservedReceiveTime = &at
	ext.getOrCreateLocked("fed", 2).currentTime = at
	m.states.getOrCreateLocked("fed", 3).regulating = true
	ext.getOrCreateLocked("fed", 3).currentTime = at
	if err := m.EnableRegulation(context.Background(), "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	got := m.Metrics("fed", 1)
	if !got.Regulating || got.CurrentTime != at {
		t.Fatalf("legal same-time enable was delayed by raw arithmetic: %+v", got)
	}
	enableCallback(t, out, 1, "TimeRegulationEnabled", at)
}

func TestReviewStrictAndAvailableGrantsAtLargePositiveLookahead(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeTAR, ModeTARA, ModeNER, ModeNMRA, ModeFQR} {
		t.Run(mode.String(), func(t *testing.T) {
			m, out := enableTestManager(t)
			at := core.LogicalTime(1 << 53)
			fs := m.states.getOrCreateLocked("fed", 1)
			fs.regulating, fs.constrained, fs.lookahead = true, true, 1
			extOf(m).getOrCreateLocked("fed", 1).currentTime = at
			if err := m.dispatchAdvance(context.Background(), "fed", 1, at, mode); err != nil {
				t.Fatal(err)
			}
			var grants int
			for _, delivery := range out.deliveries {
				if grant, ok := delivery.e.(*TimeAdvanceGrant); ok {
					grants++
					if grant.Time != at {
						t.Fatalf("grant=%g want=%g", grant.Time, at)
					}
				}
			}
			if grants != 1 {
				t.Fatalf("positive representable lookahead must release same-time grant: grants=%d", grants)
			}
			if err := m.ValidateOutgoingTSO("fed", 1, at); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
				t.Fatalf("same-time send with positive lookahead: %v", err)
			}
			floor := core.LogicalTime(math.Nextafter(float64(at), math.Inf(1)))
			if err := m.ValidateOutgoingTSO("fed", 1, floor); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReviewLBTSEqualityRemainsStrictForTARAndNMR(t *testing.T) {
	at := core.LogicalTime(1 << 53)
	bound := LBTS([]RegulatingFederate{{Handle: 2, Time: at, Lookahead: 1}})
	for _, mode := range []AdvanceMode{ModeTAR, ModeTARA, ModeNER, ModeNMRA, ModeFQR} {
		t.Run(mode.String(), func(t *testing.T) {
			m, out := enableTestManager(t)
			m.states.getOrCreateLocked("fed", 1).constrained = true
			extOf(m).getOrCreateLocked("fed", 1).currentTime = at
			peer := m.states.getOrCreateLocked("fed", 2)
			peer.regulating, peer.lookahead = true, 1
			extOf(m).getOrCreateLocked("fed", 2).currentTime = at
			if err := m.dispatchAdvance(context.Background(), "fed", 1, bound, mode); err != nil {
				t.Fatal(err)
			}
			var grants int
			for _, delivery := range out.deliveries {
				if _, ok := delivery.e.(*TimeAdvanceGrant); ok {
					grants++
				}
			}
			want := 1
			if mode == ModeTAR || mode == ModeNER {
				want = 0
			}
			if grants != want {
				t.Fatalf("grant at exact send floor=%d want=%d", grants, want)
			}
		})
	}
}

func TestReviewTimeBoundZeroAndLegacyInfinitySemantics(t *testing.T) {
	for _, at := range []float64{0, math.SmallestNonzeroFloat64, 1, 1 << 53, math.MaxFloat64} {
		if got := addTimeBound(core.LogicalTime(at), 0); got != core.LogicalTime(at) {
			t.Fatalf("zero bound(%g)=%g", at, got)
		}
	}
	for _, pair := range [][2]float64{{math.Inf(1), 0}, {math.Inf(1), 1}, {1, math.Inf(1)}, {math.Inf(-1), 1}, {1, math.Inf(-1)}, {math.Inf(-1), math.Inf(1)}} {
		t.Run(fmt.Sprint(pair), func(t *testing.T) {
			want := pair[0] + pair[1]
			got := float64(addTimeBound(core.LogicalTime(pair[0]), core.LogicalTime(pair[1])))
			if got != want && !(math.IsNaN(got) && math.IsNaN(want)) {
				t.Fatalf("legacy infinity changed: %g want=%g", got, want)
			}
		})
	}
	if got := addTimeBound(core.LogicalTime(math.MaxFloat64), core.LogicalTime(math.SmallestNonzeroFloat64)); got != core.PositiveInfinity {
		t.Fatalf("overflow bound=%g", got)
	}
	if got := LBTS(nil); got != core.PositiveInfinity {
		t.Fatalf("empty LBTS=%g", got)
	}
	if got := LBTS([]RegulatingFederate{{Handle: 1, Time: core.PositiveInfinity}, {Handle: 2, Time: 10, Lookahead: 1}}); got != 11 {
		t.Fatalf("infinite regulator hid finite floor: %g", got)
	}
}
