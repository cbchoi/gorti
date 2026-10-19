package time

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// IEEE 1516.1-2010 8.12: neither the requested time nor GALT limits the
// messages flushed; they bound the subsequent grant, together with the
// earliest TSO delivered by this request.
func TestIEEE2010FQRGrantMinimum(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		current, requested, galt, next core.LogicalTime
		hasTSO                         bool
		want                           core.LogicalTime
	}{
		{"request", 0, 5, 10, 20, true, 5},
		{"GALT", 0, 10, 2, 20, true, 2},
		{"message", 0, 10, 20, 3, true, 3},
		{"same-time-message", 5, 10, 20, 5, true, 5},
		{"stationary-peer", 5, 10, 5, 0, false, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := decideGrant(ModeFQR, tc.current, tc.requested, tc.galt, false, tc.next, tc.hasTSO)
			if !d.fire || !d.clearPending || d.time != tc.want {
				t.Fatalf("decision=%+v, want one grant at %v", d, tc.want)
			}
		})
	}
}

func TestIEEE2010FQRFlushesAllMessagesBeforeGrant(t *testing.T) {
	ctx := context.Background()
	m, out := newIEEE2010DeliveryManager(t)
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableRegulation(ctx, "fed", 2, 2); err != nil {
		t.Fatal(err)
	}
	out.events = nil
	var expected []*scheduledOrderedEvent
	for _, ts := range []core.LogicalTime{20, 3, 9} {
		e := &scheduledOrderedEvent{TimeAdvanceGrant: TimeAdvanceGrant{Time: ts}}
		expected = append(expected, e)
		r := m.ReserveTSO("fed", []core.MessageDelivery{{Recipient: 1, Timestamp: ts, Event: e, Sender: 2}})
		r.Commit(ctx)
	}
	if err := m.FlushQueueRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 4 {
		t.Fatalf("callbacks=%v, want all 3 TSO then TAG", out.events)
	}
	for i, e := range []*scheduledOrderedEvent{expected[1], expected[2], expected[0]} {
		if out.events[i] != e || e.order != core.OrderTimestamp {
			t.Fatalf("callback %d=%v", i, out.events[i])
		}
	}
	if g, ok := out.events[3].(*TimeAdvanceGrant); !ok || g.Time != 2 {
		t.Fatalf("grant=%v", out.events[3])
	}
	if got := m.Metrics("fed", 1); got.TSOQueued != 0 || got.Advancing || got.CurrentTime != 2 {
		t.Fatalf("state=%+v", got)
	}
}

func TestIEEE2010FQRDoesNotWaitForStationaryPeer(t *testing.T) {
	ctx := context.Background()
	m, out := newIEEE2010DeliveryManager(t)
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableRegulation(ctx, "fed", 2, 0); err != nil {
		t.Fatal(err)
	}
	out.events = nil
	if err := m.FlushQueueRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 1 {
		t.Fatalf("callbacks=%v", out.events)
	}
	if g, ok := out.events[0].(*TimeAdvanceGrant); !ok || g.Time != 0 {
		t.Fatalf("grant=%v", out.events[0])
	}
	if m.Metrics("fed", 1).Advancing {
		t.Fatal("same-time FQR did not complete")
	}
}

func TestIEEE2010FQRRetryKeepsDeliveredMinimum(t *testing.T) {
	ctx := context.Background()
	out := &roBoundedOutbox{capacity: 1}
	m := newROTestManager(t, out)
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	out.queue, out.observed = nil, nil
	for _, ts := range []core.LogicalTime{3, 20} {
		e := &scheduledOrderedEvent{TimeAdvanceGrant: TimeAdvanceGrant{Time: ts}}
		r := m.ReserveTSO("fed", []core.MessageDelivery{{Recipient: 1, Timestamp: ts, Event: e}})
		r.Commit(ctx)
	}
	if err := m.FlushQueueRequest(ctx, "fed", 1, 10); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("pressure=%v", err)
	}
	if len(out.observed) != 1 || m.Metrics("fed", 1).CurrentTime != 0 {
		t.Fatal("failed flush must publish only a prefix, not grant")
	}
	for attempt := 0; attempt < 4 && len(m.PendingDeliveryRetries()) > 0; attempt++ {
		out.queue = nil
		if err := m.RetryPendingDeliveries(ctx, "fed"); err != nil && !errors.Is(err, core.ErrFederateOverflow) {
			t.Fatal(err)
		}
	}
	if len(out.observed) != 3 {
		t.Fatalf("callbacks=%v", out.observed)
	}
	if g, ok := out.observed[2].(*TimeAdvanceGrant); !ok || g.Time != 3 {
		t.Fatalf("retry forgot previously delivered minimum: %v", out.observed[2])
	}
	if m.Metrics("fed", 1).TSOQueued != 0 || len(m.PendingDeliveryRetries()) != 0 {
		t.Fatal("flush retry left queued work")
	}
}
