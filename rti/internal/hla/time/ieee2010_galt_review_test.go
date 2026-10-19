package time

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func reviewGALTManager(t *testing.T, lookaheads ...core.LogicalTime) *Manager {
	t.Helper()
	m, _ := newIEEE2010DeliveryManager(t)
	t.Cleanup(func() { managerExt.Delete(m) })
	for i, lookahead := range lookaheads {
		h := core.FederateHandle(i + 1)
		if err := m.EnableRegulation(context.Background(), "fed", h, lookahead); err != nil {
			t.Fatal(err)
		}
		if err := m.EnableConstrained(context.Background(), "fed", h); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func reviewGALTQueue(t *testing.T, m *Manager, sender, recipient core.FederateHandle, at core.LogicalTime, id uint64) {
	t.Helper()
	r, err := m.ReserveOutgoingTSO("fed", sender, at, []core.TSOBufferedDelivery{{
		Recipient: recipient, Timestamp: at, Event: &scheduledOrderedEvent{}, Sender: sender, RetractionHandle: id,
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	if len(r.Buffered()) != 1 {
		t.Fatal("review fixture must queue a future TSO message")
	}
	r.Commit(context.Background())
}

func reviewGALTRequire(t *testing.T, m *Manager, h core.FederateHandle, want core.LogicalTime) {
	t.Helper()
	got, defined := m.QueryGALT("fed", h)
	if !defined || got != want {
		t.Fatalf("GALT(%d) = (%v, %t), want (%v, true)", h, got, defined, want)
	}
}

// An unqueued future arrival from a third regulator cannot lower the querying
// federate's minimum: the third regulator's own send floor is already present.
func TestIEEE2010GALTReviewThirdRegulatorFutureArrival(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeNER, ModeNMRA} {
		t.Run(mode.String(), func(t *testing.T) {
			m := reviewGALTManager(t, 0, 2, 3)
			if err := m.dispatchAdvance(context.Background(), "fed", 2, 10, mode); err != nil {
				t.Fatal(err)
			}
			if !m.Metrics("fed", 2).Advancing {
				t.Fatal("peer must remain pending before the future arrival")
			}
			reviewGALTRequire(t, m, 1, 3)
			reviewGALTQueue(t, m, 3, 2, 3, 91)
			reviewGALTRequire(t, m, 1, 3)
		})
	}
}

func TestIEEE2010GALTReviewRetractionRaisesChainedPendingBound(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeNER, ModeNMRA} {
		t.Run(mode.String(), func(t *testing.T) {
			m := reviewGALTManager(t, 0, 0, 0)
			reviewGALTQueue(t, m, 1, 2, 4, 91)
			for _, h := range []core.FederateHandle{2, 3} {
				if err := m.dispatchAdvance(context.Background(), "fed", h, 10, mode); err != nil {
					t.Fatal(err)
				}
				if !m.Metrics("fed", h).Advancing {
					t.Fatal("both NMR peers must remain pending")
				}
			}
			before, after := core.LogicalTime(4), core.LogicalTime(10)
			if mode == ModeNER {
				before = core.LogicalTime(math.Nextafter(4, math.Inf(1)))
				after = core.LogicalTime(math.Nextafter(10, math.Inf(1)))
			}
			reviewGALTRequire(t, m, 1, before)
			if removed := m.RetractMessage("fed", 1, 91); removed != 1 {
				t.Fatalf("retracted %d buffered messages, want 1", removed)
			}
			reviewGALTRequire(t, m, 1, after)
		})
	}
}

// Querying a bound is not enough: grant order must respect the early NMR
// completion even when the requesting federate sorts before that peer.
func TestIEEE2010GALTReviewGrantDoesNotOvertakeEarlyPeer(t *testing.T) {
	for _, requester := range []core.FederateHandle{1, 3} {
		for _, mode := range []AdvanceMode{ModeTAR, ModeTARA} {
			t.Run(fmt.Sprintf("requester_%d/%s", requester, mode), func(t *testing.T) {
				m := reviewGALTManager(t, 1, 1, 1)
				early := core.FederateHandle(2)
				other := core.FederateHandle(3)
				if requester == 3 {
					other = 1
				}
				reviewGALTQueue(t, m, requester, early, 4, 91)
				for _, h := range []core.FederateHandle{early, other} {
					if err := m.NextMessageRequest(context.Background(), "fed", h, 10); err != nil {
						t.Fatal(err)
					}
					if !m.Metrics("fed", h).Advancing {
						t.Fatal("peer must remain pending before request")
					}
				}
				reviewGALTRequire(t, m, requester, 5)
				if err := m.dispatchAdvance(context.Background(), "fed", requester, 9, mode); err != nil {
					t.Fatal(err)
				}
				if err := m.ValidateOutgoingTSO("fed", early, 5); err != nil {
					t.Fatalf("peer must be permitted to send at 4 + lookahead 1: %v", err)
				}
				if got := m.Metrics("fed", requester); got.CurrentTime != 0 || !got.Advancing {
					t.Fatalf("grant overtook possible timestamp 5: requester current=%v advancing=%t; early peer=%+v", got.CurrentTime, got.Advancing, m.Metrics("fed", early))
				}
			})
		}
	}
}

func TestIEEE2010GALTReviewFailedGrantAdmissionKeepsQueuedFloor(t *testing.T) {
	m := reviewGALTManager(t, 0, 2, 20)
	reviewGALTQueue(t, m, 1, 2, 4, 91)
	if err := m.NextMessageRequest(context.Background(), "fed", 2, 10); err != nil {
		t.Fatal(err)
	}
	reviewGALTRequire(t, m, 1, 6)
	m.opts.Outbox = &rejectingReservationOutbox{}
	if err := m.emitGrant(context.Background(), "fed", 2, 4, true); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("grant admission returned %v, want overflow", err)
	}
	if got := m.Metrics("fed", 2); got.CurrentTime != 0 || !got.Advancing || got.TSOQueued != 1 {
		t.Fatalf("failed admission changed pending NMR or its queue: %+v", got)
	}
	reviewGALTRequire(t, m, 1, 6)
}
