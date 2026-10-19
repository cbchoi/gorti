package time

import (
	"context"
	"errors"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type scheduledOrderedEvent struct {
	TimeAdvanceGrant
	order core.OrderType
}

func (e *scheduledOrderedEvent) SetReceivedOrder(order core.OrderType) { e.order = order }

type scheduledAnyOutbox struct {
	events []core.OutboundEvent
	fail   error
}

func (o *scheduledAnyOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, e core.OutboundEvent) error {
	if isEnableCallback(e) {
		return nil
	}
	if o.fail != nil {
		return o.fail
	}
	o.events = append(o.events, e)
	return nil
}

func TestTimeGateReceivedOrderAfterRejectedTSODelivery(t *testing.T) {
	ctx := context.Background()
	out := &scheduledAnyOutbox{}
	m, err := New(Options{Outbox: out, Clock: core.NewManualClock(stdtime.Unix(1, 0))})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	e := &scheduledOrderedEvent{}
	r := m.ReserveTSO("fed", []core.TSOBufferedDelivery{{Recipient: 1, Timestamp: 10, Event: e}})
	r.Commit(ctx)
	out.fail = errors.New("callback admission rejected")
	if err := m.EnableAsynchronousDelivery(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.TimeAdvanceRequest(ctx, "fed", 1, 20); !errors.Is(err, out.fail) {
		t.Fatal(err)
	}
	if len(out.events) != 0 || !m.Metrics("fed", 1).Advancing {
		t.Fatal("failed TSO delivery must retain the request and withhold TAG")
	}
	out.fail = nil
	if err := m.tryGrantPending(ctx, "fed"); err != nil {
		t.Fatal(err)
	}
	if e.order != core.OrderTimestamp || len(out.events) != 2 || out.events[0] != e {
		t.Fatalf("retry must deliver TSO before TAG: events=%v order=%v", out.events, e.order)
	}
}

func TestScheduledSaveBuffersFutureAsyncTSOUntilRelease(t *testing.T) {
	ctx := context.Background()
	out := &scheduledAnyOutbox{}
	m, err := New(Options{Outbox: out, Clock: core.NewManualClock(stdtime.Unix(1, 0))})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableAsynchronousDelivery(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	_, release, err := scheduleTM(t, m, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	if m.ShouldDeliverNow("fed", 1, 15) {
		t.Fatal("future TSO bypasses save via async delivery")
	}
	e := &scheduledOrderedEvent{}
	r := m.ReserveTSO("fed", []core.TSOBufferedDelivery{{Recipient: 1, Timestamp: 15, Event: e}})
	if len(r.Immediate()) != 0 || len(r.Buffered()) != 1 {
		r.Release()
		t.Fatal("save did not buffer future callback")
	}
	r.Commit(ctx)
	if err := m.DisableAsynchronousDelivery(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableAsynchronousDelivery(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 0 {
		t.Fatal("async toggle crossed save boundary")
	}
	release()
	if err := m.ResumeScheduledSave(ctx, "fed"); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 0 {
		t.Fatal("save resume delivered future TSO without time advancement")
	}
	if err := m.TimeAdvanceRequest(ctx, "fed", 1, 15); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 2 || out.events[0] != e || e.order != core.OrderTimestamp {
		t.Fatalf("resume callbacks: %+v order=%v", out.events, e.order)
	}
}

func TestTimeGateClassifiesReceivedOrderIndependentlyOfAsync(t *testing.T) {
	ctx := context.Background()
	out := &scheduledAnyOutbox{}
	m, err := New(Options{Outbox: out, Clock: core.NewManualClock(stdtime.Unix(1, 0))})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableRegulation(ctx, "fed", 2, 1); err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{1, 2, 3} {
		e := &scheduledOrderedEvent{}
		r := m.ReserveTSO("fed", []core.TSOBufferedDelivery{{Recipient: h, Timestamp: 10, Event: e}})
		want := core.OrderReceive
		if h == 1 {
			want = core.OrderTimestamp
		}
		if e.order != want {
			r.Release()
			t.Fatalf("recipient %d order=%v want=%v", h, e.order, want)
		}
		r.Commit(ctx)
		if h == 1 {
			if err := m.EnableAsynchronousDelivery(ctx, "fed", 1); err != nil {
				t.Fatal(err)
			}
			if e.order != core.OrderTimestamp || len(out.events) != 0 {
				t.Fatal("async toggle changed timestamp received order or released a future callback")
			}
		}
	}
}

func TestScheduledSaveParkedRequestsDoNotStall(t *testing.T) {
	ctx := context.Background()
	clock := core.NewManualClock(stdtime.Unix(1, 0))
	out := &scheduledAnyOutbox{}
	m, err := New(Options{Outbox: out, Clock: clock, StallTimeout: stdtime.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	ready, release, err := scheduleTM(t, m, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.TimeAdvanceRequest(ctx, "fed", 1, 20); err != nil {
		t.Fatal(err)
	}
	assertScheduleReady(t, ready, true)
	clock.Advance(2 * stdtime.Second)
	if n := m.CheckStalls(ctx); n != 0 {
		t.Fatal("save duration halted a deliberately parked request")
	}
	release()
	if err := m.ResumeScheduledSave(ctx, "fed"); err != nil {
		t.Fatal(err)
	}
	if m.Snapshot("fed").Federates[0].CurrentTime != 20 {
		t.Fatal("parked request did not resume")
	}
}
