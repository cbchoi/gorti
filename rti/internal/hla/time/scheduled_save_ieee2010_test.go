package time

import (
	"context"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func ieeeScheduledManager(t *testing.T) (*Manager, *scheduledAnyOutbox) {
	t.Helper()
	out := &scheduledAnyOutbox{}
	m, err := New(Options{Outbox: out, Clock: core.NewManualClock(stdtime.Unix(1, 0))})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.states.enableConstrained("fed", 1); err != nil {
		t.Fatal(err)
	}
	return m, out
}

func ieeeScheduledGrants(out *scheduledAnyOutbox) []core.LogicalTime {
	var times []core.LogicalTime
	for _, event := range out.events {
		if grant, ok := event.(*TimeAdvanceGrant); ok {
			times = append(times, grant.Time)
		}
	}
	return times
}

func TestIEEE2010ScheduledSaveStrictEqualityPrecedesGrant(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeTAR, ModeNER} {
		t.Run(mode.String(), func(t *testing.T) {
			m, out := ieeeScheduledManager(t)
			ready, release, err := scheduleTM(t, m, 10, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			if err := m.dispatchAdvance(context.Background(), "fed", 1, 10, mode); err != nil {
				t.Fatal(err)
			}
			if grants := ieeeScheduledGrants(out); len(grants) != 0 {
				t.Fatalf("strict boundary grant preceded save: %v", grants)
			}
			assertScheduleReady(t, ready, true)
			if !m.Metrics("fed", 1).Advancing {
				t.Fatal("save consumed the pending advance")
			}
			release()
			if err := m.ResumeScheduledSave(context.Background(), "fed"); err != nil {
				t.Fatal(err)
			}
			if grants := ieeeScheduledGrants(out); len(grants) != 1 || grants[0] != 10 {
				t.Fatalf("resumed grants: %v", grants)
			}
		})
	}
}

func TestIEEE2010ScheduledSaveAvailableEqualityNeedsLaterAdvance(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeTARA, ModeNMRA, ModeFQR} {
		t.Run(mode.String(), func(t *testing.T) {
			m, out := ieeeScheduledManager(t)
			ready, release, err := scheduleTM(t, m, 10, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			if err := m.dispatchAdvance(context.Background(), "fed", 1, 10, mode); err != nil {
				t.Fatal(err)
			}
			if grants := ieeeScheduledGrants(out); len(grants) != 1 || grants[0] != 10 {
				t.Fatalf("available boundary grants: %v", grants)
			}
			assertScheduleReady(t, ready, false)
			if err := m.dispatchAdvance(context.Background(), "fed", 1, 20, mode); err != nil {
				t.Fatal(err)
			}
			assertScheduleReady(t, ready, true)
			if grants := ieeeScheduledGrants(out); len(grants) != 1 {
				t.Fatalf("crossing grant preceded save: %v", grants)
			}
		})
	}
}

func TestIEEE2010ScheduledSaveFQRFlushesBeyondFutureSave(t *testing.T) {
	m, out := ieeeScheduledManager(t)
	ctx := context.Background()
	if err := m.states.enableRegulation("fed", 2, 5); err != nil {
		t.Fatal(err)
	}
	ready, release, err := scheduleTM(t, m, 10, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	event := &scheduledOrderedEvent{}
	r := m.ReserveTSO("fed", []core.TSOBufferedDelivery{{Recipient: 1, Timestamp: 20, Event: event}})
	r.Commit(ctx)
	if err := m.FlushQueueRequest(ctx, "fed", 1, 30); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 2 || out.events[0] != event || event.order != core.OrderTimestamp {
		t.Fatalf("FQR did not flush future TSO before its grant: %v, order=%v", out.events, event.order)
	}
	if grants := ieeeScheduledGrants(out); len(grants) != 1 || grants[0] != 5 {
		t.Fatalf("FQR grants: %v", grants)
	}
	assertScheduleReady(t, ready, false)
	if m.Metrics("fed", 1).Advancing {
		t.Fatal("FQR waited for the save or another federate")
	}
}

func TestIEEE2010ScheduledSaveRegulatingOnlyMemberNeedNotAdvance(t *testing.T) {
	m, out := ieeeScheduledManager(t)
	ctx := context.Background()
	if err := m.states.enableRegulation("fed", 2, 30); err != nil {
		t.Fatal(err)
	}
	ready, release, err := scheduleTM(t, m, 10, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := m.TimeAdvanceRequest(ctx, "fed", 1, 20); err != nil {
		t.Fatal(err)
	}
	assertScheduleReady(t, ready, true)
	if grants := ieeeScheduledGrants(out); len(grants) != 0 {
		t.Fatalf("crossing grant preceded save: %v", grants)
	}
}

func TestIEEE2010ScheduledSaveNextMessageUsesActualGrant(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeNER, ModeNMRA, ModeFQR} {
		t.Run(mode.String(), func(t *testing.T) {
			m, out := ieeeScheduledManager(t)
			ctx := context.Background()
			ready, release, err := scheduleTM(t, m, 10, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			for _, stamp := range []core.LogicalTime{5, 20} {
				r := m.ReserveTSO("fed", []core.TSOBufferedDelivery{{Recipient: 1, Timestamp: stamp, Event: &scheduledOrderedEvent{}}})
				r.Commit(ctx)
			}
			if err := m.dispatchAdvance(ctx, "fed", 1, 30, mode); err != nil {
				t.Fatal(err)
			}
			assertScheduleReady(t, ready, false)
			if grants := ieeeScheduledGrants(out); len(grants) != 1 || grants[0] != 5 {
				t.Fatalf("next-message/FQR grant: %v", grants)
			}
			if err := m.dispatchAdvance(ctx, "fed", 1, 30, mode); err != nil {
				t.Fatal(err)
			}
			assertScheduleReady(t, ready, true)
			if grants := ieeeScheduledGrants(out); len(grants) != 1 {
				t.Fatalf("crossing request was granted during save: %v", grants)
			}
		})
	}
}

func TestIEEE2010ScheduledSaveIgnoresUnconstrainedCurrentTime(t *testing.T) {
	m, _ := ieeeScheduledManager(t)
	ctx := context.Background()
	if err := m.states.enableRegulation("fed", 2, 1); err != nil {
		t.Fatal(err)
	}
	if err := m.TimeAdvanceRequest(ctx, "fed", 2, 50); err != nil {
		t.Fatal(err)
	}
	ready, release, err := scheduleTM(t, m, 10, 1, 2)
	if err != nil {
		t.Fatalf("unconstrained logical time prevented a save: %v", err)
	}
	defer release()
	if err := m.TimeAdvanceRequest(ctx, "fed", 1, 20); err != nil {
		t.Fatal(err)
	}
	assertScheduleReady(t, ready, true)
}

func TestIEEE2010ScheduledSaveRetainsDeliveredNextMessageTarget(t *testing.T) {
	m, out := ieeeScheduledManager(t)
	ctx := context.Background()
	ready, release, err := scheduleTM(t, m, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	event := &scheduledOrderedEvent{}
	r := m.ReserveTSO("fed", []core.TSOBufferedDelivery{{Recipient: 1, Timestamp: 10, Event: event}})
	r.Commit(ctx)
	if err := m.NextMessageRequest(ctx, "fed", 1, 30); err != nil {
		t.Fatal(err)
	}
	assertScheduleReady(t, ready, true)
	if len(out.events) != 1 || out.events[0] != event {
		t.Fatalf("save must follow the boundary TSO and precede TAG: %v", out.events)
	}
	release()
	if err := m.ResumeScheduledSave(ctx, "fed"); err != nil {
		t.Fatal(err)
	}
	if grants := ieeeScheduledGrants(out); len(grants) != 1 || grants[0] != 10 {
		t.Fatalf("save forgot the NMR message already delivered: %v", grants)
	}
}

func TestIEEE2010ScheduledSaveGrantHoldBoundaries(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeTAR, ModeNER, ModeTARA, ModeNMRA, ModeFQR} {
		t.Run(mode.String(), func(t *testing.T) {
			m, _ := ieeeScheduledManager(t)
			_, release, err := scheduleTM(t, m, 10, 1, 2)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			for _, h := range []core.FederateHandle{1, 2} {
				if err := m.recordAdvance("fed", h, 20, mode); err != nil {
					t.Fatal(err)
				}
			}
			ext := extOf(m)
			lease := ext.evaluatorLock("fed")
			lease.Lock()
			defer lease.Unlock()
			ext.mu.Lock()
			defer ext.mu.Unlock()
			for _, at := range []core.LogicalTime{9, 10, 11} {
				want := at > 10 || (at == 10 && (mode == ModeTAR || mode == ModeNER))
				if got := m.holdGrantForScheduledSaveLocked(ext, "fed", 1, at); got != want {
					t.Fatalf("hold constrained grant %v=%v want=%v", at, got, want)
				}
				if m.holdGrantForScheduledSaveLocked(ext, "fed", 2, at) {
					t.Fatalf("held unconstrained grant %v before save initiation", at)
				}
			}
		})
	}
}
