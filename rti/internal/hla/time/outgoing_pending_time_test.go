package time

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestOutgoingTSOUsesRequestedTimeWhileAdvancing(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeNER, ModeNMRA, ModeTAR, ModeTARA, ModeFQR} {
		t.Run(mode.String(), func(t *testing.T) {
			ctx := context.Background()
			m, err := New(Options{Clock: core.NewManualClock(zeroTime()), Outbox: nopOutbox{}})
			if err != nil {
				t.Fatal(err)
			}
			for h, lookahead := range map[core.FederateHandle]core.LogicalTime{1: 2, 2: 0} {
				if err := m.EnableRegulation(ctx, "fed", h, lookahead); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
				t.Fatal(err)
			}
			if err := m.ValidateOutgoingTSO("fed", 1, 2); err != nil {
				t.Fatalf("before advancing: %v", err)
			}
			// Inspect the admitted request before evaluation; FQR normally
			// completes immediately even when a peer is stationary.
			if err := m.recordAdvance("fed", 1, 10, mode); err != nil {
				t.Fatal(err)
			}
			if !m.Metrics("fed", 1).Advancing {
				t.Fatal("peer did not hold the advance request")
			}
			// IEEE 1516.1-2010 8.1.7 uses requested time while advancing.
			for _, timestamp := range []core.LogicalTime{2, 10, 11.5} {
				if err := m.ValidateOutgoingTSO("fed", 1, timestamp); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
					t.Errorf("timestamp %v: got %v, want invalid logical time below 10+2", timestamp, err)
				}
			}
			for _, timestamp := range []core.LogicalTime{12, 15} {
				if err := m.ValidateOutgoingTSO("fed", 1, timestamp); err != nil {
					t.Errorf("timestamp %v at/above requested time + lookahead: %v", timestamp, err)
				}
			}
		})
	}
}

func TestOutgoingTSOBoundEasesAfterEarlyNextMessageGrant(t *testing.T) {
	ctx := context.Background()
	m, err := New(Options{Clock: core.NewManualClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	for h, lookahead := range map[core.FederateHandle]core.LogicalTime{1: 2, 2: 0} {
		if err := m.EnableRegulation(ctx, "fed", h, lookahead); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.BufferTSO(ctx, "fed", 1, 5, &fakeRetractEvent{}); err != nil {
		t.Fatal(err)
	}
	if err := m.NextMessageRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	if err := m.ValidateOutgoingTSO("fed", 1, 7); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Errorf("pending request accepted timestamp 7: %v", err)
	}
	if err := m.ModifyLookahead(ctx, "fed", 2, 8); err != nil {
		t.Fatal(err)
	}
	if snapshot := m.Metrics("fed", 1); snapshot.Advancing || snapshot.CurrentTime != 5 {
		t.Fatalf("expected early grant to 5: %+v", snapshot)
	}
	if err := m.ValidateOutgoingTSO("fed", 1, 7); err != nil {
		t.Fatalf("after early grant, timestamp 5+2 must be accepted: %v", err)
	}
}
