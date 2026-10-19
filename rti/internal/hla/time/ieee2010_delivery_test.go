package time

import (
	"context"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func newIEEE2010DeliveryManager(t *testing.T) (*Manager, *scheduledAnyOutbox) {
	t.Helper()
	out := &scheduledAnyOutbox{}
	m, err := New(Options{Outbox: out, Clock: core.NewManualClock(stdtime.Unix(1, 0))})
	if err != nil {
		t.Fatal(err)
	}
	return m, out
}

func TestIEEE2010AsyncDoesNotReleaseFutureTSO(t *testing.T) {
	ctx := context.Background()
	m, out := newIEEE2010DeliveryManager(t)
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	e := &scheduledOrderedEvent{}
	r := m.ReserveTSO("fed", []core.TSOBufferedDelivery{{Recipient: 1, Timestamp: 10, Event: e}})
	r.Commit(ctx)
	if err := m.EnableAsynchronousDelivery(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 0 || m.ShouldDeliverNow("fed", 1, 10) {
		t.Fatal("asynchronous delivery released future TSO before time advancement")
	}
	if err := m.TimeAdvanceRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 2 || out.events[0] != e || e.order != core.OrderTimestamp {
		t.Fatalf("TSO must precede TAG and retain timestamp order: events=%v order=%v", out.events, e.order)
	}
	if _, ok := out.events[1].(*TimeAdvanceGrant); !ok {
		t.Fatalf("last callback is %T, not TAG", out.events[1])
	}
}

func TestIEEE2010RegulationDoesNotConstrainIncomingMessages(t *testing.T) {
	ctx := context.Background()
	m, _ := newIEEE2010DeliveryManager(t)
	if err := m.EnableRegulation(ctx, "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	e := &scheduledOrderedEvent{}
	r := m.ReserveTSO("fed", []core.TSOBufferedDelivery{{Recipient: 1, Timestamp: 10, Event: e}})
	defer r.Release()
	if len(r.Immediate()) != 1 || len(r.Buffered()) != 0 || e.order != core.OrderReceive {
		t.Fatalf("regulating-only receiver must receive RO immediately: immediate=%d buffered=%d order=%v", len(r.Immediate()), len(r.Buffered()), e.order)
	}
}

func TestIEEE2010DisableConstrainedReleasesQueuedMessagesAsRO(t *testing.T) {
	ctx := context.Background()
	m, out := newIEEE2010DeliveryManager(t)
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	e := &scheduledOrderedEvent{}
	r := m.ReserveTSO("fed", []core.TSOBufferedDelivery{{Recipient: 1, Timestamp: 10, Event: e}})
	r.Commit(ctx)
	if err := m.DisableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 1 || out.events[0] != e || e.order != core.OrderReceive {
		t.Fatalf("disabled constraint must release RO: events=%v order=%v", out.events, e.order)
	}
}

func TestIEEE2010LITSIncludesGALT(t *testing.T) {
	ctx := context.Background()
	m, _ := newIEEE2010DeliveryManager(t)
	if _, finite := m.QueryLITS("fed", 1); finite {
		t.Fatal("LITS must be undefined without GALT or incoming messages")
	}
	if err := m.EnableRegulation(ctx, "fed", 2, 4); err != nil {
		t.Fatal(err)
	}
	if lits, finite := m.QueryLITS("fed", 1); !finite || lits != 4 {
		t.Fatalf("empty queue LITS=(%v,%v), want GALT=4", lits, finite)
	}
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	for _, timestamp := range []core.LogicalTime{8, 2} {
		r := m.ReserveTSO("fed", []core.TSOBufferedDelivery{{Recipient: 1, Timestamp: timestamp, Event: &scheduledOrderedEvent{}}})
		r.Commit(ctx)
		want := min(timestamp, 4)
		lits, finite := m.QueryLITS("fed", 1)
		snapshot := m.Metrics("fed", 1)
		if !finite || lits != want || snapshot.LITS == nil || *snapshot.LITS != want {
			t.Fatalf("LITS query/metrics must use min(GALT, queue): query=(%v,%v) metrics=%+v want=%v", lits, finite, snapshot, want)
		}
	}
}
