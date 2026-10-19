package time

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// S1516.1 8.9 and 8.13: queued equality precedes TAG; later equality is legal.
func TestApplicableTimeSupportTARAQueuedAndFutureEquality(t *testing.T) {
	ctx := context.Background()
	m, out := newIEEE2010DeliveryManager(t)
	t.Cleanup(func() { managerExt.Delete(m) })
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	ro := &scheduledOrderedEvent{}
	before, equal := &scheduledOrderedEvent{}, &scheduledOrderedEvent{}
	r := m.ReserveTSO("fed", []core.MessageDelivery{
		{Recipient: 1, Event: ro, ReceiveOrder: true, Sender: 2},
		{Recipient: 1, Timestamp: 5, Event: equal, Sender: 2},
		{Recipient: 1, Timestamp: 4, Event: before, Sender: 3},
	})
	if len(r.Immediate()) != 0 || len(r.Buffered()) != 3 {
		r.Release()
		t.Fatal("idle constrained receiver must hold all three messages")
	}
	r.Commit(ctx)
	if err := m.TimeAdvanceRequestAvailable(ctx, "fed", 1, 5); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 4 || out.events[0] != ro || out.events[1] != before || out.events[2] != equal {
		t.Fatalf("queued RO and timestamp 4/5 must precede TAG: %v", out.events)
	}
	grant, ok := out.events[3].(*TimeAdvanceGrant)
	if !ok || grant.Time != 5 || ro.order != core.OrderReceive || before.order != core.OrderTimestamp || equal.order != core.OrderTimestamp {
		t.Fatalf("delivery metadata/grant changed: events=%v orders=%v/%v/%v", out.events, ro.order, before.order, equal.order)
	}
	futureEqual := &scheduledOrderedEvent{}
	r = m.ReserveTSO("fed", []core.MessageDelivery{{Recipient: 1, Timestamp: 5, Event: futureEqual, Sender: 3}})
	if len(r.Immediate()) != 0 || len(r.Buffered()) != 1 {
		r.Release()
		t.Fatal("post-TAG equality must be admitted but held until another advance")
	}
	r.Commit(ctx)
	if got := m.Metrics("fed", 1); got.CurrentTime != 5 || got.Advancing || got.TSOQueued != 1 || len(out.events) != 4 {
		t.Fatalf("post-TAG equality changed time or delivered early: %+v events=%v", got, out.events)
	}
	if err := m.TimeAdvanceRequestAvailable(ctx, "fed", 1, 5); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 6 || out.events[4] != futureEqual || futureEqual.order != core.OrderTimestamp {
		t.Fatalf("later equality must precede the next TAG exactly once: %v", out.events)
	}
	grant, ok = out.events[5].(*TimeAdvanceGrant)
	if !ok || grant.Time != 5 || m.Metrics("fed", 1).TSOQueued != 0 {
		t.Fatalf("same-time available advance did not drain equality and grant 5: %v", out.events)
	}
}
