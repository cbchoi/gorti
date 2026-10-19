package time

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

type timeObserverState struct {
	handle                  core.FederateHandle
	regulating, constrained bool
	lookahead, current      core.LogicalTime
}

func timeObserverLocksReleased(t *testing.T, m *Manager, fed core.FederationName) bool {
	t.Helper()
	ext := extOf(m)
	for name, lock := range map[string]interface {
		TryLock() bool
		Unlock()
	}{"evaluator": ext.evaluatorLock(fed), "regulation": &m.states.mu, "advance": &ext.mu} {
		if !lock.TryLock() {
			t.Errorf("time-state observer runs with %s lock held", name)
			return false
		}
		lock.Unlock()
	}
	return true
}

func newTimeObserverManager(t *testing.T, out core.Outbox) (*Manager, *mom.Manager, *[]timeObserverState) {
	t.Helper()
	ctx := context.Background()
	observer, err := mom.New(mom.Options{Outbox: nopOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{1, 2, 3} {
		if err := observer.FederateJoined(ctx, "fed", h, "observer", "test"); err != nil {
			t.Fatal(err)
		}
	}
	var notifications []timeObserverState
	var m *Manager
	m, err = New(Options{
		Clock: core.NewManualClock(zeroTime()), Outbox: out,
		OnTimeStateChanged: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, regulating, constrained bool, lookahead, current core.LogicalTime) {
			if !timeObserverLocksReleased(t, m, fed) {
				return
			}
			live := m.Metrics(fed, h)
			if live.CurrentTime != current || live.Regulating != regulating || live.Constrained != constrained || (live.Lookahead != nil && *live.Lookahead != lookahead) {
				t.Errorf("incoherent observer state: time=%v lookahead=%v regulating=%v constrained=%v; live=%+v", current, lookahead, regulating, constrained, live)
			}
			notifications = append(notifications, timeObserverState{h, regulating, constrained, lookahead, current})
			if err := observer.TimeStateChanged(ctx, fed, h, regulating, constrained, lookahead, current); err != nil {
				t.Error(err)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { managerExt.Delete(m) })
	return m, observer, &notifications
}

func TestIEEE2010TimeObserverRefreshesMOMAfterGrant(t *testing.T) {
	ctx := context.Background()
	m, observer, notifications := newTimeObserverManager(t, nopOutbox{})
	if err := m.EnableRegulation(ctx, "fed", 1, 5); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.ModifyLookahead(ctx, "fed", 1, 2); err != nil {
		t.Fatal(err)
	}
	*notifications = nil
	if err := m.TimeAdvanceRequest(ctx, "fed", 1, 2); err != nil {
		t.Fatal(err)
	}
	attrs, ok := observer.QueryFederate("fed", 1)
	if !ok || attrs.Lookahead != 3 || attrs.LogicalTime != 2 || !attrs.TimeRegulating || !attrs.TimeConstrained {
		t.Errorf("MOM after grant = %+v, found=%v; want lookahead=3 time=2 and both flags", attrs, ok)
	}
	if len(*notifications) != 1 {
		t.Fatalf("grant notifications = %v, want one", *notifications)
	}
	if err := m.ModifyLookahead(ctx, "fed", 1, 4); err != nil {
		t.Fatal(err)
	}
	attrs, _ = observer.QueryFederate("fed", 1)
	if attrs.Lookahead != 4 || attrs.LogicalTime != 2 {
		t.Errorf("modification reset observer time: %+v", attrs)
	}
}

func TestIEEE2010TimeObserverReportsCurrentTimeWithoutNewGrant(t *testing.T) {
	ctx := context.Background()
	m, observer, _ := newTimeObserverManager(t, nopOutbox{})
	if err := m.EnableRegulation(ctx, "fed", 1, 5); err != nil {
		t.Fatal(err)
	}
	if err := m.TimeAdvanceRequest(ctx, "fed", 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := m.ModifyLookahead(ctx, "fed", 1, 6); err != nil {
		t.Fatal(err)
	}
	attrs, _ := observer.QueryFederate("fed", 1)
	if attrs.LogicalTime != 2 || attrs.Lookahead != 6 {
		t.Fatalf("observer state = %+v, want time=2 lookahead=6", attrs)
	}
}

var errTimeObserverGrant = errors.New("observer test grant failure")

type timeObserverOutbox struct{ fail core.FederateHandle }

func (o *timeObserverOutbox) Send(_ context.Context, _ core.FederationName, h core.FederateHandle, e core.OutboundEvent) error {
	if isEnableCallback(e) {
		return nil
	}
	if h == o.fail {
		return errTimeObserverGrant
	}
	return nil
}

type timeObserverReservable struct{ *timeObserverOutbox }
type timeObserverReservation struct {
	out        *timeObserverOutbox
	deliveries []core.OutboxDelivery
}

func (o timeObserverReservable) Reserve(_ context.Context, _ core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	if r, ok := enableFixtureAdmission(deliveries); ok {
		return r, nil
	}
	return &timeObserverReservation{out: o.timeObserverOutbox, deliveries: deliveries}, nil
}

func (r *timeObserverReservation) Commit() error {
	for _, delivery := range r.deliveries {
		if delivery.Recipient == r.out.fail {
			return errTimeObserverGrant
		}
	}
	return nil
}
func (*timeObserverReservation) Release() {}

func TestIEEE2010TimeObserverPreservesSuccessfulGrantsBeforeFailure(t *testing.T) {
	for _, reserved := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "reserved"}[reserved], func(t *testing.T) {
			ctx := context.Background()
			out := &timeObserverOutbox{}
			var port core.Outbox = out
			if reserved {
				port = timeObserverReservable{out}
			}
			m, observer, notifications := newTimeObserverManager(t, port)
			for _, h := range []core.FederateHandle{1, 2, 3} {
				if err := m.EnableRegulation(ctx, "fed", h, 1); err != nil {
					t.Fatal(err)
				}
			}
			for _, h := range []core.FederateHandle{1, 2} {
				if err := m.EnableConstrained(ctx, "fed", h); err != nil {
					t.Fatal(err)
				}
				if err := m.ModifyLookahead(ctx, "fed", h, 5); err != nil {
					t.Fatal(err)
				}
				if err := m.ModifyLookahead(ctx, "fed", h, 2); err != nil {
					t.Fatal(err)
				}
				if err := m.TimeAdvanceRequest(ctx, "fed", h, 2); err != nil {
					t.Fatal(err)
				}
			}
			*notifications = nil
			out.fail = 2
			if err := m.DisableRegulation(ctx, "fed", 3); !errors.Is(err, errTimeObserverGrant) {
				t.Fatalf("grant failure = %v", err)
			}
			first, _ := observer.QueryFederate("fed", 1)
			second, _ := observer.QueryFederate("fed", 2)
			if first.LogicalTime != 2 || first.Lookahead != 3 || second.LogicalTime != 0 || second.Lookahead != 5 {
				t.Errorf("partial grant observers: committed=%+v rolled_back=%+v", first, second)
			}
			for _, notification := range *notifications {
				if notification.handle == 2 {
					t.Error("rolled-back grant emitted an observer notification")
				}
			}
			*notifications = nil
			out.fail = 0
			if err := m.tryGrantPending(ctx, "fed"); err != nil {
				t.Fatal(err)
			}
			second, _ = observer.QueryFederate("fed", 2)
			if second.LogicalTime != 2 || second.Lookahead != 3 || len(*notifications) != 1 || (*notifications)[0].handle != 2 {
				t.Fatalf("retry observer=%+v notifications=%+v", second, *notifications)
			}
		})
	}
}

func TestIEEE2010TimeObserverCanReenterAfterGrant(t *testing.T) {
	ctx := context.Background()
	m, observer, _ := newTimeObserverManager(t, nopOutbox{})
	if err := m.EnableRegulation(ctx, "fed", 1, 5); err != nil {
		t.Fatal(err)
	}
	original := m.opts.OnTimeStateChanged
	reentered := false
	m.opts.OnTimeStateChanged = func(ctx context.Context, fed core.FederationName, h core.FederateHandle, regulating, constrained bool, lookahead, current core.LogicalTime) {
		if !timeObserverLocksReleased(t, m, fed) {
			return
		}
		original(ctx, fed, h, regulating, constrained, lookahead, current)
		if current == 2 && !reentered {
			reentered = true
			if err := m.ModifyLookahead(ctx, fed, h, 7); err != nil {
				t.Error(err)
			}
		}
	}
	if err := m.TimeAdvanceRequest(ctx, "fed", 1, 2); err != nil {
		t.Fatal(err)
	}
	attrs, _ := observer.QueryFederate("fed", 1)
	if !reentered || attrs.LogicalTime != 2 || attrs.Lookahead != 7 {
		t.Fatalf("observer re-entry=%v final MOM=%+v", reentered, attrs)
	}
}
