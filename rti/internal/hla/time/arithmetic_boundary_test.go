package time

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestFloat64BoundPositiveIntervalChangesTime(t *testing.T) {
	for _, tc := range []struct{ at, interval float64 }{
		{1, math.SmallestNonzeroFloat64}, {1 << 53, 1}, {math.MaxFloat64, math.SmallestNonzeroFloat64},
	} {
		want := core.LogicalTime(math.Nextafter(tc.at, math.Inf(1)))
		got := LBTS([]RegulatingFederate{{Handle: 1, Time: core.LogicalTime(tc.at), Lookahead: core.LogicalTime(tc.interval)}})
		if got != want {
			t.Errorf("bound(%g,%g) = %g, want %g", tc.at, tc.interval, got, want)
		}
	}
}

func TestFloat64OutgoingAndGALTUseRepresentablePositiveLookahead(t *testing.T) {
	for _, advancing := range []bool{false, true} {
		m, err := New(Options{Clock: core.NewManualClock(zeroTime()), Outbox: nopOutbox{}})
		if err != nil {
			t.Fatal(err)
		}
		at := core.LogicalTime(1 << 53)
		m.states.getOrCreateLocked("fed", 1).regulating = true
		m.states.getOrCreateLocked("fed", 1).lookahead = 1
		ns := extOf(m).getOrCreateLocked("fed", 1)
		ns.currentTime, ns.requestedTime, ns.pendingNER, ns.mode = at, at, advancing, ModeTARA
		if err := m.ValidateOutgoingTSO("fed", 1, at); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
			t.Errorf("advancing=%t: same-time send admitted despite positive lookahead: %v", advancing, err)
		}
		next := core.LogicalTime(math.Nextafter(float64(at), math.Inf(1)))
		if err := m.ValidateOutgoingTSO("fed", 1, next); err != nil {
			t.Fatal(err)
		}
		if got, defined := m.QueryGALT("fed", 2); !defined || got != next {
			t.Errorf("advancing=%t: GALT=(%g,%t), want (%g,true)", advancing, got, defined, next)
		}
	}
}

func TestFloat64RetractionUsesRepresentablePositiveLookahead(t *testing.T) {
	m, service, _ := ieeeRetractionManager(t)
	at := core.LogicalTime(1 << 53)
	next := core.LogicalTime(math.Nextafter(float64(at), math.Inf(1)))
	admitRetraction(t, service, 10, next)
	m.states.getOrCreateLocked("fed", 1).regulating = true
	m.states.getOrCreateLocked("fed", 1).lookahead = 1
	extOf(m).getOrCreateLocked("fed", 1).currentTime = at
	if err := service.RetractWithError(context.Background(), "fed", 1, 10); !errors.Is(err, core.ErrRetractionTooLate) {
		t.Fatalf("timestamp at computed send floor must be too late: %v", err)
	}
}
