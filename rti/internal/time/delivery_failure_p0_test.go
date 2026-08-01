package time

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

type deliveryFailureEvent struct{ seq uint64 }

func (e *deliveryFailureEvent) Seq() uint64 { return e.seq }

type failOnceDeliveryOutbox struct {
	mu     sync.Mutex
	fail   bool
	events []core.OutboundEvent
}

type rejectingReservationOutbox struct{}

type observingReservationOutbox struct {
	onCommit func([]core.OutboxDelivery)
}

type observingReservation struct {
	deliveries []core.OutboxDelivery
	onCommit   func([]core.OutboxDelivery)
}

func (*observingReservationOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	return errors.New("unexpected legacy send")
}

func (o *observingReservationOutbox) Reserve(_ context.Context, _ core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	return &observingReservation{
		deliveries: append([]core.OutboxDelivery(nil), deliveries...),
		onCommit:   o.onCommit,
	}, nil
}

func (r *observingReservation) Commit() error {
	if r.onCommit != nil {
		r.onCommit(r.deliveries)
	}
	return nil
}

func (*observingReservation) Release() {}

func (*rejectingReservationOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	return errors.New("unexpected Send after rejected reservation")
}

func (*rejectingReservationOutbox) Reserve(context.Context, core.FederationName, []core.OutboxDelivery) (core.OutboxReservation, error) {
	return nil, core.ErrFederateOverflow
}

func (o *failOnceDeliveryOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, event core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.fail {
		o.fail = false
		return core.ErrFederateOverflow
	}
	o.events = append(o.events, event)
	return nil
}

func TestEmitGrant_RequeuesTSOAndWithholdsGrantOnDeliveryFailure(t *testing.T) {
	outbox := &failOnceDeliveryOutbox{fail: true}
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: outbox})
	if err != nil {
		t.Fatal(err)
	}
	ext := extOf(mgr)
	ext.mu.Lock()
	ns := ext.getOrCreateLocked("fed", 1)
	ns.pendingNER = true
	ns.requestedTime = 1
	ns.mode = ModeTAR
	ns.tsoBuffer = append(ns.tsoBuffer, bufferedTSOEvent{timestamp: 1, event: &deliveryFailureEvent{seq: 41}})
	ext.mu.Unlock()

	if err := mgr.emitGrant(context.Background(), "fed", 1, 1, true); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("emitGrant = %v, want ErrFederateOverflow", err)
	}
	ext.mu.Lock()
	if ns.currentTime != 0 || !ns.pendingNER || len(ns.tsoBuffer) != 1 {
		t.Fatalf("state after failure = time %v pending %v buffered %d", ns.currentTime, ns.pendingNER, len(ns.tsoBuffer))
	}
	ext.mu.Unlock()

	if err := mgr.emitGrant(context.Background(), "fed", 1, 1, true); err != nil {
		t.Fatal(err)
	}
	outbox.mu.Lock()
	if len(outbox.events) != 2 || outbox.events[0].Seq() != 41 {
		t.Fatalf("delivered events = %v, want TSO then grant", outbox.events)
	}
	if _, ok := outbox.events[1].(*TimeAdvanceGrant); !ok {
		t.Fatalf("second event = %T, want TimeAdvanceGrant", outbox.events[1])
	}
	outbox.mu.Unlock()
	ext.mu.Lock()
	if ns.currentTime != 1 || ns.pendingNER || len(ns.tsoBuffer) != 0 {
		t.Fatalf("state after retry = time %v pending %v buffered %d", ns.currentTime, ns.pendingNER, len(ns.tsoBuffer))
	}
	ext.mu.Unlock()
}

func TestP0EmitGrantReservationFailurePrecedesWALAndWithholdsGrant(t *testing.T) {
	log := &recordingGrantLog{}
	mgr, err := New(Options{
		Clock: core.NewFakeClock(zeroTime()), Outbox: &rejectingReservationOutbox{}, EventLog: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	ext := extOf(mgr)
	ext.mu.Lock()
	ns := ext.getOrCreateLocked("fed", 1)
	ns.pendingNER = true
	ns.requestedTime = 1
	ns.mode = ModeTAR
	ns.tsoBuffer = append(ns.tsoBuffer, bufferedTSOEvent{timestamp: 1, event: &deliveryFailureEvent{seq: 41}})
	ext.mu.Unlock()

	if err := mgr.emitGrant(context.Background(), "fed", 1, 1, true); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("emitGrant = %v, want ErrFederateOverflow", err)
	}
	if records := log.snapshot(); len(records) != 0 {
		t.Fatalf("grant WAL records = %d, want 0", len(records))
	}
	ext.mu.Lock()
	defer ext.mu.Unlock()
	if ns.currentTime != 0 || !ns.pendingNER || len(ns.tsoBuffer) != 1 {
		t.Fatalf("state after admission failure = time %v pending %v buffered %d", ns.currentTime, ns.pendingNER, len(ns.tsoBuffer))
	}
}

func TestEmitGrantCommitsTimeStateBeforeGrantVisibility(t *testing.T) {
	var mgr *Manager
	observedCommittedState := false
	outbox := &observingReservationOutbox{}
	outbox.onCommit = func(deliveries []core.OutboxDelivery) {
		if len(deliveries) != 1 {
			t.Fatalf("deliveries = %d, want one grant", len(deliveries))
		}
		if _, ok := deliveries[0].Event.(*TimeAdvanceGrant); !ok {
			t.Fatalf("delivery = %T, want TimeAdvanceGrant", deliveries[0].Event)
		}
		// emitGrant holds ext.mu across Commit, so inspect the protected state
		// directly to pin the visibility ordering without recursively locking.
		ns := extOf(mgr).getLocked("fed", 1)
		observedCommittedState = ns != nil && ns.currentTime == 1 && !ns.pendingNER
	}
	var err error
	mgr, err = New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: outbox})
	if err != nil {
		t.Fatal(err)
	}
	ext := extOf(mgr)
	ext.mu.Lock()
	ns := ext.getOrCreateLocked("fed", 1)
	ns.pendingNER = true
	ns.requestedTime = 1
	ns.mode = ModeTAR
	ext.mu.Unlock()

	if err := mgr.emitGrant(context.Background(), "fed", 1, 1, true); err != nil {
		t.Fatal(err)
	}
	if !observedCommittedState {
		t.Fatal("grant became visible before currentTime/pending state committed")
	}
}
