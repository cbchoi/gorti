package time

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type nextMessageFilterEvent struct{ id uint64 }

func (e *nextMessageFilterEvent) Seq() uint64 { return e.id }

type nextMessageFilter struct {
	allowed map[uint64]bool
	calls   map[uint64]int
}

func (f *nextMessageFilter) FilterQueuedMessage(_ context.Context, _ core.FederationName, _ core.FederateHandle, e core.OutboundEvent) core.OutboundEvent {
	f.calls[e.Seq()]++
	if f.allowed[e.Seq()] {
		return e
	}
	return nil
}

func nextMessageFilterSetup(t *testing.T, port core.Outbox, times []core.LogicalTime) (*Manager, *nextMessageFilter) {
	t.Helper()
	ctx := context.Background()
	m, err := New(Options{Clock: core.NewRealClock(), Outbox: port})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.EnableRegulation(ctx, "f", 2, 1); err != nil {
		t.Fatal(err)
	}
	if err = m.EnableConstrained(ctx, "f", 1); err != nil {
		t.Fatal(err)
	}
	f := &nextMessageFilter{allowed: map[uint64]bool{}, calls: map[uint64]int{}}
	m.SetQueuedMessageFilter(f)
	for i, at := range times {
		id := uint64(i + 1)
		f.allowed[id] = true
		r, err := m.ReserveOutgoingTSO("f", 2, at, []core.MessageDelivery{{Recipient: 1, Sender: 2, Timestamp: at, Event: &nextMessageFilterEvent{id: id}}})
		if err != nil {
			t.Fatal(err)
		}
		r.Commit(ctx)
	}
	return m, f
}

func TestIEEE2010NextMessageFilteredEarliestReevaluatesTarget(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeNER, ModeNMRA} {
		for _, scenario := range []string{"later", "none", "unsafe-later"} {
			t.Run(mode.String()+"/"+scenario, func(t *testing.T) {
				ctx := context.Background()
				out := &enableTestOutbox{}
				m, filter := nextMessageFilterSetup(t, out, []core.LogicalTime{3, 7})
				peerTime := core.LogicalTime(10)
				if scenario == "unsafe-later" {
					peerTime = 4
				}
				if err := m.TimeAdvanceRequestAvailable(ctx, "f", 2, peerTime); err != nil {
					t.Fatal(err)
				}
				out.deliveries = nil
				filter.allowed[1] = false
				filter.allowed[2] = scenario == "later"
				if err := m.dispatchAdvance(ctx, "f", 1, 10, mode); err != nil {
					t.Fatal(err)
				}
				if scenario == "unsafe-later" {
					if got := m.Metrics("f", 1); got.CurrentTime != 0 || !got.Advancing || got.TSOQueued != 1 || len(out.deliveries) != 0 {
						t.Fatalf("unsafe next group advanced or disappeared: %+v callbacks=%v", got, out.deliveries)
					}
					if filter.calls[1] != 1 || filter.calls[2] != 0 {
						t.Fatalf("unsafe group evaluated early: %v", filter.calls)
					}
					filter.allowed[2] = true
					if err := m.TimeAdvanceRequestAvailable(ctx, "f", 2, 10); err != nil {
						t.Fatal(err)
					}
				}
				var received []core.OutboundEvent
				for _, d := range out.deliveries {
					if d.h == 1 {
						received = append(received, d.e)
					}
				}
				wantTime := core.LogicalTime(7)
				wantCallbacks := 2
				if scenario == "none" {
					wantTime, wantCallbacks = 10, 1
				}
				if len(received) != wantCallbacks {
					t.Fatalf("callbacks=%v want=%d", received, wantCallbacks)
				}
				grant, ok := received[len(received)-1].(*TimeAdvanceGrant)
				if !ok || grant.Time != wantTime {
					t.Fatalf("grant=%v want=%v", received[len(received)-1], wantTime)
				}
				if wantCallbacks == 2 && received[0].Seq() != 2 {
					t.Fatalf("wrong delivered message=%v", received[0])
				}
				if filter.calls[1] != 1 || filter.calls[2] != 1 {
					t.Fatalf("filter not evaluated once per eligible message: %v", filter.calls)
				}
			})
		}
	}
}

func TestIEEE2010NextMessageFilteredRetryKeepsDeliveredPrefix(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeNER, ModeNMRA} {
		t.Run(mode.String(), func(t *testing.T) {
			ctx := context.Background()
			out := &enableTestOutbox{}
			reserved := &enableTestReservableOutbox{enableTestOutbox: out}
			m, filter := nextMessageFilterSetup(t, reserved, []core.LogicalTime{3, 3, 7})
			if err := m.TimeAdvanceRequestAvailable(ctx, "f", 2, 10); err != nil {
				t.Fatal(err)
			}
			out.deliveries = nil
			reserved.bounded, reserved.remaining = true, 1
			if err := m.dispatchAdvance(ctx, "f", 1, 10, mode); !errors.Is(err, core.ErrFederateOverflow) {
				t.Fatalf("partial delivery=%v", err)
			}
			if len(out.deliveries) != 1 || out.deliveries[0].e.Seq() != 1 {
				t.Fatalf("prefix=%v", out.deliveries)
			}
			filter.allowed[1], filter.allowed[2] = false, false
			reserved.remaining = 1
			if err := m.RetryPendingDeliveries(ctx, "f"); err != nil {
				t.Fatal(err)
			}
			if len(out.deliveries) != 2 {
				t.Fatalf("callbacks=%v", out.deliveries)
			}
			grant, ok := out.deliveries[1].e.(*TimeAdvanceGrant)
			if !ok || grant.Time != 3 {
				t.Fatalf("delivered-prefix target lost: %v", out.deliveries[1].e)
			}
			if m.Metrics("f", 1).TSOQueued != 1 || filter.calls[3] != 0 {
				t.Fatalf("later message consumed during prefix grant: calls=%v", filter.calls)
			}
		})
	}
}
