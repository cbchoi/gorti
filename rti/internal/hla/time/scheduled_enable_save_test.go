package time

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type scheduledEnableOutbox struct{ events []core.OutboundEvent }

func (out *scheduledEnableOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, event core.OutboundEvent) error {
	out.events = append(out.events, event)
	return nil
}

func scheduledEnableManager(t *testing.T) (*Manager, *scheduledEnableOutbox) {
	t.Helper()
	out := &scheduledEnableOutbox{}
	m, err := New(Options{Outbox: out, Clock: core.NewRealClock()})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.EnableConstrained(context.Background(), "fed", 1); err != nil {
		t.Fatal(err)
	}
	return m, out
}

func TestScheduledSavePendingRegulationImplicitAdvance(t *testing.T) {
	m, out := scheduledEnableManager(t)
	ctx := context.Background()
	if err := m.EnableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	if err := m.TimeAdvanceRequest(ctx, "fed", 2, 10); err != nil {
		t.Fatal(err)
	}
	// Exercise the coordinator's exact-boundary state directly. The public
	// RequestFederationSave timestamp precondition is not asserted by this test.
	ready, release, err := scheduleTM(t, m, 10, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := m.TimeAdvanceRequestAvailable(ctx, "fed", 2, 20); err != nil {
		t.Fatal(err)
	}
	ro, tso := &scheduledOrderedEvent{}, &scheduledOrderedEvent{}
	r := m.ReserveTSO("fed", []core.TSOBufferedDelivery{
		{Recipient: 1, Timestamp: 9, Event: ro, ReceiveOrder: true},
		{Recipient: 1, Timestamp: 9, Event: tso},
	})
	r.Commit(ctx)
	out.events = nil
	if err := m.EnableRegulation(ctx, "fed", 1, 0); err != nil {
		t.Fatal(err)
	}
	if m.Metrics("fed", 1).Regulating || len(out.events) != 0 {
		t.Fatal("implicit advance crossed the save before initiation")
	}
	assertScheduleReady(t, ready, true)
	if len(out.events) != 2 || out.events[0] != ro || out.events[1] != tso {
		t.Fatalf("implicit advance did not deliver save-boundary RO/TSO: %v", out.events)
	}
	if ro.order != core.OrderReceive || tso.order != core.OrderTimestamp {
		t.Fatalf("wrong received orders: RO=%v TSO=%v", ro.order, tso.order)
	}
	if err := m.RetryPendingDeliveries(ctx, "fed"); err != nil {
		t.Fatal(err)
	}
	if m.Metrics("fed", 1).Regulating || len(out.events) != 2 {
		t.Fatal("enable completed while the save was paused")
	}
	release()
	if err := m.ResumeScheduledSave(ctx, "fed"); err != nil {
		t.Fatal(err)
	}
	want := core.LogicalTime(math.Nextafter(10, math.Inf(1)))
	got := m.Metrics("fed", 1)
	if !got.Regulating || got.CurrentTime != want {
		t.Fatalf("pending regulation did not resume after save: %+v want=%v", got, want)
	}
	if len(out.events) != 3 {
		t.Fatalf("unexpected implicit advance callbacks: %v", out.events)
	}
	if enabled, ok := out.events[2].(*TimeRegulationEnabled); !ok || enabled.Time != want {
		t.Fatalf("implicit advance did not finish with regulation-enabled: %T", out.events[2])
	}
}

func TestScheduledSaveDoesNotHoldUnconstrainedEnable(t *testing.T) {
	for _, regulation := range []bool{false, true} {
		name := "constraint"
		if regulation {
			name = "regulation"
		}
		t.Run(name, func(t *testing.T) {
			m, out := scheduledEnableManager(t)
			ctx := context.Background()
			if err := m.EnableRegulation(ctx, "fed", 2, 1); err != nil {
				t.Fatal(err)
			}
			if err := m.TimeAdvanceRequestAvailable(ctx, "fed", 3, 20); err != nil {
				t.Fatal(err)
			}
			ready, release, err := scheduleTM(t, m, 10, 1, 2, 3)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			if err := m.TimeAdvanceRequestAvailable(ctx, "fed", 2, 30); err != nil {
				t.Fatal(err)
			}
			out.events = nil
			if regulation {
				err = m.EnableRegulation(ctx, "fed", 3, 1)
			} else {
				err = m.EnableConstrained(ctx, "fed", 3)
			}
			if err != nil {
				t.Fatal(err)
			}
			got := m.Metrics("fed", 3)
			if got.CurrentTime != 20 || regulation && !got.Regulating || !regulation && !got.Constrained || len(out.events) != 1 {
				t.Fatalf("scheduled save blocked an unconstrained mode change: %+v callbacks=%v", got, out.events)
			}
			if _, err := ready(); !errors.Is(err, ErrScheduledSaveTimeStateChanged) {
				t.Fatalf("captured mode change did not invalidate save: %v", err)
			}
		})
	}
}
