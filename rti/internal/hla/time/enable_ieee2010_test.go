package time

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type enableTestDelivery struct {
	h core.FederateHandle
	e core.OutboundEvent
}

type enableTestOutbox struct {
	deliveries []enableTestDelivery
	fail       bool
}

func (o *enableTestOutbox) Send(_ context.Context, _ core.FederationName, h core.FederateHandle, e core.OutboundEvent) error {
	if o.fail {
		return core.ErrFederateOverflow
	}
	o.deliveries = append(o.deliveries, enableTestDelivery{h, e})
	return nil
}

func enableTestManager(t *testing.T) (*Manager, *enableTestOutbox) {
	t.Helper()
	o := &enableTestOutbox{}
	m, err := New(Options{Clock: core.NewRealClock(), Outbox: o})
	if err != nil {
		t.Fatal(err)
	}
	return m, o
}

func enableCallback(t *testing.T, o *enableTestOutbox, h core.FederateHandle, name string, at core.LogicalTime) {
	t.Helper()
	var matched int
	for _, d := range o.deliveries {
		if d.h != h {
			continue
		}
		var got core.LogicalTime
		switch event := d.e.(type) {
		case *TimeRegulationEnabled:
			if name != "TimeRegulationEnabled" {
				continue
			}
			got = event.Time
		case *TimeConstrainedEnabled:
			if name != "TimeConstrainedEnabled" {
				continue
			}
			got = event.Time
		default:
			continue
		}
		matched++
		if got != at {
			t.Fatalf("%s time=%v want=%v", name, got, at)
		}
	}
	if matched != 1 {
		t.Fatalf("%s callbacks=%d want=1: %#v", name, matched, o.deliveries)
	}
}

// 8.2 chooses the minimum time whose first legal send cannot violate a peer's grant.
func TestIEEE2010EnableRegulationMinimumSafeTime(t *testing.T) {
	for _, strict := range []bool{false, true} {
		t.Run(fmt.Sprint(strict), func(t *testing.T) {
			ctx := context.Background()
			m, out := enableTestManager(t)
			if err := m.EnableConstrained(ctx, "f", 2); err != nil {
				t.Fatal(err)
			}
			advance := m.TimeAdvanceRequestAvailable
			if strict {
				advance = m.TimeAdvanceRequest
			}
			if err := advance(ctx, "f", 2, 10); err != nil {
				t.Fatal(err)
			}
			out.deliveries = nil
			if err := m.EnableRegulation(ctx, "f", 1, 2); err != nil {
				t.Fatal(err)
			}
			// Nextafter(8, 0) + 2 rounds to 10 and is the first legal
			// Float64 time. Section 8.2 requires that minimum, not 10 - 2.
			want := core.LogicalTime(math.Nextafter(8, 0))
			if strict {
				want = core.LogicalTime(math.Nextafter(8, math.Inf(1)))
			}
			got := m.Metrics("f", 1)
			if got.CurrentTime != want || !got.Regulating || got.Lookahead == nil || *got.Lookahead != 2 {
				t.Fatalf("unsafe/nonminimal enabled state: %+v want time=%v", got, want)
			}
			enableCallback(t, out, 1, "TimeRegulationEnabled", want)
			if err := m.ValidateOutgoingTSO("f", 1, want+2); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestIEEE2010EnableConstrainedWaitsForGALT(t *testing.T) {
	ctx := context.Background()
	m, out := enableTestManager(t)
	if err := m.TimeAdvanceRequestAvailable(ctx, "f", 1, 10); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableRegulation(ctx, "f", 2, 1); err != nil {
		t.Fatal(err)
	}
	out.deliveries = nil
	if err := m.EnableConstrained(ctx, "f", 1); err != nil {
		t.Fatal(err)
	}
	if got := m.Metrics("f", 1); got.Constrained || got.CurrentTime != 10 || len(out.deliveries) != 0 {
		t.Fatalf("constraint enabled before safe GALT: %+v callbacks=%v", got, out.deliveries)
	}
	if err := m.EnableConstrained(ctx, "f", 1); err == nil || err.Error() != "request for time-constrained pending" {
		t.Fatalf("duplicate pending constraint=%v", err)
	}
	if err := m.TimeAdvanceRequest(ctx, "f", 1, 11); err == nil || err.Error() != "request for time-constrained pending" {
		t.Fatalf("advance while constraint pending=%v", err)
	}
	if err := m.TimeAdvanceRequestAvailable(ctx, "f", 2, 10); err != nil {
		t.Fatal(err)
	}
	if got := m.Metrics("f", 1); !got.Constrained || got.CurrentTime != 10 {
		t.Fatalf("activation not resumed: %+v", got)
	}
	enableCallback(t, out, 1, "TimeConstrainedEnabled", 10)
}

func TestIEEE2010EnableRejectsAdvancingState(t *testing.T) {
	for _, regulation := range []bool{false, true} {
		t.Run(fmt.Sprint(regulation), func(t *testing.T) {
			ctx := context.Background()
			m, out := enableTestManager(t)
			if err := m.EnableConstrained(ctx, "f", 1); err != nil {
				t.Fatal(err)
			}
			if err := m.EnableRegulation(ctx, "f", 2, 0); err != nil {
				t.Fatal(err)
			}
			if err := m.TimeAdvanceRequest(ctx, "f", 1, 10); err != nil {
				t.Fatal(err)
			}
			out.deliveries = nil
			var err error
			if regulation {
				err = m.EnableRegulation(ctx, "f", 1, 1)
			} else {
				err = m.EnableConstrained(ctx, "f", 1)
			}
			if !errors.Is(err, core.ErrTimeAdvancingState) {
				t.Fatalf("enable during advance=%v", err)
			}
			if len(out.deliveries) != 0 {
				t.Fatal("rejected enable emitted callback")
			}
		})
	}
}

func TestIEEE2010EnableCallbackFailureDoesNotCommit(t *testing.T) {
	for _, regulation := range []bool{false, true} {
		t.Run(fmt.Sprint(regulation), func(t *testing.T) {
			ctx := context.Background()
			m, out := enableTestManager(t)
			out.fail = true
			var err error
			if regulation {
				err = m.EnableRegulation(ctx, "f", 1, 2)
			} else {
				err = m.EnableConstrained(ctx, "f", 1)
			}
			if err != nil {
				t.Fatalf("accepted asynchronous request=%v", err)
			}
			if got := m.Metrics("f", 1); got.Regulating || got.Constrained || got.CurrentTime != 0 {
				t.Fatalf("callback failure committed enable: %+v", got)
			}
			out.fail = false
			if err := m.tryGrantPending(ctx, "f"); err != nil {
				t.Fatal(err)
			}
			name := "TimeConstrainedEnabled"
			if regulation {
				name = "TimeRegulationEnabled"
			}
			enableCallback(t, out, 1, name, 0)
		})
	}
}

func TestIEEE2010EnableRegulationRoundedEqualLookahead(t *testing.T) {
	for _, peerTime := range []core.LogicalTime{10, 1e300} {
		t.Run(fmt.Sprint(peerTime), func(t *testing.T) {
			ctx := context.Background()
			m, out := enableTestManager(t)
			if err := m.EnableConstrained(ctx, "f", 2); err != nil {
				t.Fatal(err)
			}
			if err := m.TimeAdvanceRequest(ctx, "f", 2, peerTime); err != nil {
				t.Fatal(err)
			}
			out.deliveries = nil
			if err := m.EnableRegulation(ctx, "f", 1, peerTime); err != nil {
				t.Fatal(err)
			}
			at := m.Metrics("f", 1).CurrentTime
			if at+peerTime <= peerTime {
				t.Fatalf("first send at %v+%v violates strict peer", at, peerTime)
			}
			previous := core.LogicalTime(math.Nextafter(float64(at), math.Inf(-1)))
			if previous >= 0 && previous+peerTime > peerTime {
				t.Fatalf("enable time %v not minimal; %v is safe", at, previous)
			}
			enableCallback(t, out, 1, "TimeRegulationEnabled", at)
		})
	}
}

type enableTestReservableOutbox struct {
	*enableTestOutbox
	failCommit bool
	bounded    bool
	remaining  int
}

func (o *enableTestReservableOutbox) Reserve(_ context.Context, _ core.FederationName, ds []core.OutboxDelivery) (core.OutboxReservation, error) {
	if o.fail || o.bounded && len(ds) > o.remaining {
		return nil, core.ErrFederateOverflow
	}
	return &ieee2010ReviewReservation{commit: func() error {
		if o.failCommit {
			return core.ErrFederateOverflow
		}
		for _, d := range ds {
			o.deliveries = append(o.deliveries, enableTestDelivery{d.Recipient, d.Event})
		}
		if o.bounded {
			o.remaining -= len(ds)
		}
		return nil
	}}, nil
}

func TestIEEE2010EnableRegulationImplicitTARA(t *testing.T) {
	for _, mode := range []string{"legacy", "reserved", "failed-commit", "bounded-prefix"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			out := &enableTestOutbox{}
			reserved := &enableTestReservableOutbox{enableTestOutbox: out}
			var port core.Outbox = out
			if mode != "legacy" {
				port = reserved
			}
			m, err := New(Options{Clock: core.NewRealClock(), Outbox: port})
			if err != nil {
				t.Fatal(err)
			}
			if err = m.EnableRegulation(ctx, "f", 3, 1); err != nil {
				t.Fatal(err)
			}
			for _, h := range []core.FederateHandle{1, 2} {
				if err = m.EnableConstrained(ctx, "f", h); err != nil {
					t.Fatal(err)
				}
			}
			if err = m.TimeAdvanceRequestAvailable(ctx, "f", 2, 10); err != nil {
				t.Fatal(err)
			}
			activation := core.LogicalTime(math.Nextafter(8, 0))
			ro, first, equal, future := &scheduledOrderedEvent{}, &scheduledOrderedEvent{}, &scheduledOrderedEvent{}, &scheduledOrderedEvent{}
			r := m.ReserveTSO("f", []core.MessageDelivery{
				{Recipient: 1, Event: ro, ReceiveOrder: true},
				{Recipient: 1, Event: first, Timestamp: 5, Sender: 3},
				{Recipient: 1, Event: equal, Timestamp: activation, Sender: 3},
				{Recipient: 1, Event: future, Timestamp: 9, Sender: 3},
			})
			if len(r.Immediate()) != 0 {
				r.Release()
				t.Fatal("messages did not wait for implicit advance")
			}
			r.Commit(ctx)
			if err = m.TimeAdvanceRequestAvailable(ctx, "f", 3, 20); err != nil {
				t.Fatal(err)
			}
			out.deliveries = nil
			reserved.failCommit = mode == "failed-commit"
			reserved.bounded, reserved.remaining = mode == "bounded-prefix", 2
			if err = m.EnableRegulation(ctx, "f", 1, 2); err != nil {
				t.Fatal(err)
			}
			if reserved.failCommit || reserved.bounded {
				got := m.Metrics("f", 1)
				if got.Regulating || got.CurrentTime != 0 {
					t.Fatalf("incomplete batch enabled regulation: %+v", got)
				}
				want := 0
				if reserved.bounded {
					want = 2
				}
				if len(out.deliveries) != want {
					t.Fatalf("delivered prefix=%d want=%d", len(out.deliveries), want)
				}
				if err = m.EnableRegulation(ctx, "f", 1, 2); !errors.Is(err, ErrTimeRegulationPending) {
					t.Fatalf("duplicate request=%v", err)
				}
				reserved.failCommit, reserved.remaining = false, 2
				if err = m.RetryPendingDeliveries(ctx, "f"); err != nil {
					t.Fatal(err)
				}
			}
			if len(out.deliveries) != 4 || out.deliveries[0].e != ro || out.deliveries[1].e != first || out.deliveries[2].e != equal {
				t.Fatalf("RO/TSO/enable order=%#v", out.deliveries)
			}
			enableCallback(t, out, 1, "TimeRegulationEnabled", activation)
			if first.order != core.OrderTimestamp || equal.order != core.OrderTimestamp {
				t.Fatal("implicit TARA changed TSO to RO")
			}
			if got := m.Metrics("f", 1); got.CurrentTime != activation || !got.Regulating || got.TSOQueued != 1 || got.ROQueued != 0 {
				t.Fatalf("implicit TARA final state=%+v", got)
			}
		})
	}
}

func TestIEEE2010EnableKeepsReceiveGuaranteeSeparate(t *testing.T) {
	ctx := context.Background()
	m, out := enableTestManager(t)
	if err := m.EnableConstrained(ctx, "f", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.TimeAdvanceRequest(ctx, "f", 1, 10); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableRegulation(ctx, "f", 1, 0); err != nil {
		t.Fatal(err)
	}
	if err := m.ValidateOutgoingTSO("f", 1, 10); err != nil {
		t.Fatalf("first zero-lookahead send: %v", err)
	}
	if err := m.EnableRegulation(ctx, "f", 2, 0); err != nil {
		t.Fatal(err)
	}
	enableCallback(t, out, 2, "TimeRegulationEnabled", core.LogicalTime(math.Nextafter(10, math.Inf(1))))
	if err := m.DisableConstrained(ctx, "f", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableRegulation(ctx, "f", 3, 0); err != nil {
		t.Fatal(err)
	}
	enableCallback(t, out, 3, "TimeRegulationEnabled", 0)
}

func TestIEEE2010EnableConstrainedDoesNotInheritUnconstrainedGrant(t *testing.T) {
	ctx := context.Background()
	m, out := enableTestManager(t)
	if err := m.TimeAdvanceRequest(ctx, "f", 1, 10); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableConstrained(ctx, "f", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableRegulation(ctx, "f", 2, 0); err != nil {
		t.Fatal(err)
	}
	enableCallback(t, out, 2, "TimeRegulationEnabled", 10)
}

func TestIEEE2010EnableRegulationProtectsPartialTARDelivery(t *testing.T) {
	ctx := context.Background()
	out := &enableTestOutbox{}
	reserved := &enableTestReservableOutbox{enableTestOutbox: out}
	m, err := New(Options{Clock: core.NewRealClock(), Outbox: reserved})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.EnableConstrained(ctx, "f", 1); err != nil {
		t.Fatal(err)
	}
	message := &scheduledOrderedEvent{}
	r := m.ReserveTSO("f", []core.MessageDelivery{{Recipient: 1, Timestamp: 5, Event: message}})
	r.Commit(ctx)
	out.deliveries = nil
	reserved.bounded, reserved.remaining = true, 1
	if err = m.TimeAdvanceRequest(ctx, "f", 1, 10); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("grant failure=%v", err)
	}
	if got := m.Metrics("f", 1); got.CurrentTime != 0 || !got.Advancing || len(out.deliveries) != 1 {
		t.Fatalf("partial state=%+v callbacks=%v", got, out.deliveries)
	}
	reserved.bounded = false
	if err = m.EnableRegulation(ctx, "f", 2, 0); err != nil {
		t.Fatal(err)
	}
	enableCallback(t, out, 2, "TimeRegulationEnabled", 5)
	if err = m.ValidateOutgoingTSO("f", 2, 4); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Fatalf("new regulator can send before delivered TSO: %v", err)
	}
	if got := m.Metrics("f", 1); got.CurrentTime != 0 || !got.Advancing {
		t.Fatalf("pending TAR escaped new regulator floor: %+v", got)
	}
}

func TestIEEE2010EnableRegulationRetainsDeliveredPrefixAfterPeerResign(t *testing.T) {
	ctx := context.Background()
	out := &enableTestOutbox{}
	reserved := &enableTestReservableOutbox{enableTestOutbox: out}
	m, err := New(Options{Clock: core.NewRealClock(), Outbox: reserved})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.EnableRegulation(ctx, "f", 3, 1); err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{1, 2} {
		if err = m.EnableConstrained(ctx, "f", h); err != nil {
			t.Fatal(err)
		}
	}
	if err = m.TimeAdvanceRequestAvailable(ctx, "f", 2, 10); err != nil {
		t.Fatal(err)
	}
	first, future := &scheduledOrderedEvent{}, &scheduledOrderedEvent{}
	r := m.ReserveTSO("f", []core.MessageDelivery{{Recipient: 1, Timestamp: 5, Event: first}, {Recipient: 1, Timestamp: 8, Event: future}})
	r.Commit(ctx)
	if err = m.TimeAdvanceRequestAvailable(ctx, "f", 3, 20); err != nil {
		t.Fatal(err)
	}
	out.deliveries = nil
	reserved.bounded, reserved.remaining = true, 1
	if err = m.EnableRegulation(ctx, "f", 1, 2); err != nil {
		t.Fatal(err)
	}
	if len(out.deliveries) != 1 || out.deliveries[0].e != first {
		t.Fatal("expected exactly the first delivered TSO")
	}
	m.OnFederateResign(ctx, "f", 2)
	reserved.remaining = 1
	if err = m.RetryPendingDeliveries(ctx, "f"); err != nil {
		t.Fatal(err)
	}
	enableCallback(t, out, 1, "TimeRegulationEnabled", 5)
	if got := m.Metrics("f", 1); got.CurrentTime != 5 || !got.Regulating || got.TSOQueued != 1 {
		t.Fatalf("recomputed enable target ignored actual delivery: %+v", got)
	}
}

func TestIEEE2010EnableConstrainedResumesAfterPeerDisablesConstraint(t *testing.T) {
	ctx := context.Background()
	out := &enableTestOutbox{}
	reserved := &enableTestReservableOutbox{enableTestOutbox: out}
	m, err := New(Options{Clock: core.NewRealClock(), Outbox: reserved})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.TimeAdvanceRequestAvailable(ctx, "f", 1, 5); err != nil {
		t.Fatal(err)
	}
	if err = m.EnableRegulation(ctx, "f", 2, 1); err != nil {
		t.Fatal(err)
	}
	if err = m.EnableConstrained(ctx, "f", 2); err != nil {
		t.Fatal(err)
	}
	message := &scheduledOrderedEvent{}
	r := m.ReserveTSO("f", []core.MessageDelivery{{Recipient: 2, Timestamp: 2, Event: message}})
	r.Commit(ctx)
	reserved.bounded, reserved.remaining = true, 0
	if err = m.NextMessageRequest(ctx, "f", 2, 10); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("pending next-message=%v", err)
	}
	if err = m.EnableConstrained(ctx, "f", 1); err != nil {
		t.Fatal(err)
	}
	if m.Metrics("f", 1).Constrained {
		t.Fatal("activation bypassed peer's early next-message bound")
	}
	reserved.bounded = false
	out.deliveries = nil
	if err = m.DisableConstrained(ctx, "f", 2); err != nil {
		t.Fatal(err)
	}
	enableCallback(t, out, 1, "TimeConstrainedEnabled", 5)
	if got := m.Metrics("f", 2); got.Constrained || got.Advancing || got.CurrentTime != 10 {
		t.Fatalf("unconstrained pending advance was not resumed: %+v", got)
	}
	if message.order != core.OrderReceive {
		t.Fatal("disabled recipient's message did not switch to RO")
	}
}
