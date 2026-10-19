package time

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type ieeeRetractionService interface {
	ReserveRetractionMessage(context.Context, core.FederationName, core.FederateHandle, uint64, core.LogicalTime) (core.OutboxReservation, error)
	RetractWithError(context.Context, core.FederationName, core.FederateHandle, uint64) error
	RecordRetractionDeliveries(core.FederationName, []core.MessageDelivery)
}

func ieeeRetractionManager(t *testing.T) (*Manager, ieeeRetractionService, *ieeeRetractionOutbox) {
	t.Helper()
	out := &ieeeRetractionOutbox{}
	m, err := New(Options{Clock: core.NewManualClock(zeroTime()), Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := any(m).(ieeeRetractionService)
	if !ok {
		t.Fatal("IEEE 8.21 originating-time validation service missing")
	}
	return m, s, out
}

func admitRetraction(t *testing.T, s ieeeRetractionService, id uint64, timestamp core.LogicalTime) {
	t.Helper()
	r, err := s.ReserveRetractionMessage(context.Background(), "fed", 1, id, timestamp)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestIEEE2010RetractConditions(t *testing.T) {
	for _, tc := range []struct {
		name                                     string
		timestamp, current, requested, lookahead core.LogicalTime
		advancing, regulating                    bool
		sender                                   core.FederateHandle
		id                                       uint64
		allowed                                  bool
	}{
		{"greater-granted", 8, 3, 0, 4, false, true, 1, 10, true},
		{"equal-granted", 7, 3, 0, 4, false, true, 1, 10, false},
		{"less-granted", 6, 3, 0, 4, false, true, 1, 10, false},
		{"greater-advancing", 15, 3, 10, 4, true, true, 1, 10, true},
		{"equal-advancing", 14, 3, 10, 4, true, true, 1, 10, false},
		{"less-advancing", 13, 3, 10, 4, true, true, 1, 10, false},
		{"not-regulating", 8, 3, 0, 4, false, false, 1, 10, false},
		{"different-sender", 8, 3, 0, 4, false, true, 2, 10, false},
		{"unknown-id", 8, 3, 0, 4, false, true, 1, 11, false},
		{"zero-id", 8, 3, 0, 4, false, true, 1, 0, false},
		{"equal-zero-lookahead", 3, 3, 0, 0, false, true, 1, 10, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, s, out := ieeeRetractionManager(t)
			admitRetraction(t, s, 10, tc.timestamp)
			m.states.mu.Lock()
			m.states.getOrCreateLocked("fed", tc.sender).regulating = tc.regulating
			m.states.getOrCreateLocked("fed", tc.sender).lookahead = tc.lookahead
			m.states.mu.Unlock()
			ext := extOf(m)
			ext.mu.Lock()
			ns := ext.getOrCreateLocked("fed", tc.sender)
			ns.currentTime, ns.pendingNER, ns.requestedTime, ns.mode = tc.current, tc.advancing, tc.requested, ModeTARA
			ext.getOrCreateLocked("fed", 3).tsoBuffer = []bufferedTSOEvent{{timestamp: tc.timestamp, sender: 1, retractionHandle: 10, event: &RequestRetraction{}}}
			ext.mu.Unlock()
			err := s.RetractWithError(context.Background(), "fed", tc.sender, tc.id)
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v err=%v", tc.allowed, err)
			}
			if !tc.allowed {
				want := core.ErrRetractionTooLate
				if !tc.regulating {
					want = core.ErrTimeNotRegulating
				} else if tc.sender != 1 || tc.id != 10 {
					want = core.ErrRetractionHandleInvalid
				}
				if !errors.Is(err, want) {
					t.Fatalf("typed error=%v, want %v", err, want)
				}
			}
			ext.mu.Lock()
			remaining := len(ext.getLocked("fed", 3).tsoBuffer)
			ext.mu.Unlock()
			if (remaining == 0) != tc.allowed {
				t.Fatalf("rejection mutated queue: remaining=%d", remaining)
			}
			if len(out.deliveries) != 0 {
				t.Fatal("undelivered-only message generated a callback")
			}
		})
	}
}

func TestIEEE2010RetractActualLookaheadReduction(t *testing.T) {
	m, s, _ := ieeeRetractionManager(t)
	admitRetraction(t, s, 1, 14)
	m.states.mu.Lock()
	fs := m.states.getOrCreateLocked("fed", 1)
	fs.regulating, fs.lookahead, fs.reducingLookahead = true, 10, true
	fs.lookaheadTarget, fs.lookaheadOriginTime, fs.lookaheadOriginValue = 1, 0, 10
	m.states.mu.Unlock()
	ext := extOf(m)
	ext.mu.Lock()
	ns := ext.getOrCreateLocked("fed", 1)
	ns.currentTime, ns.pendingNER, ns.requestedTime, ns.mode = 0, true, 3, ModeTARA
	ext.mu.Unlock()
	// Retract uses actual current lookahead 10, not projected 7 or requested 1.
	if err := s.RetractWithError(context.Background(), "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	admitRetraction(t, s, 2, 13)
	if err := s.RetractWithError(context.Background(), "fed", 1, 2); !errors.Is(err, core.ErrRetractionTooLate) {
		t.Fatalf("actual boundary accepted: %v", err)
	}
}

func TestIEEE2010RetractionDeliveredCallbackAtomicRetry(t *testing.T) {
	m, s, out := ieeeRetractionManager(t)
	ctx := context.Background()
	if err := m.EnableRegulation(ctx, "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	out.deliveries = nil
	admitRetraction(t, s, 10, 20)
	s.RecordRetractionDeliveries("fed", []core.MessageDelivery{
		{Recipient: 2, Sender: 1, RetractionHandle: 10, Timestamp: 20},
		{Recipient: 3, Sender: 1, RetractionHandle: 10, Timestamp: 20},
		{Recipient: 2, Sender: 1, RetractionHandle: 10, Timestamp: 20},
	})
	ext := extOf(m)
	ext.mu.Lock()
	ext.getOrCreateLocked("fed", 4).tsoBuffer = []bufferedTSOEvent{{timestamp: 20, sender: 1, retractionHandle: 10, event: &RequestRetraction{}}}
	ext.mu.Unlock()
	out.fail = core.ErrFederateOverflow
	if err := s.RetractWithError(ctx, "fed", 1, 10); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("overflow=%v", err)
	}
	ext.mu.Lock()
	remaining := len(ext.getLocked("fed", 4).tsoBuffer)
	ext.mu.Unlock()
	if remaining != 1 || len(out.deliveries) != 0 {
		t.Fatal("failed callback admission consumed retraction")
	}
	out.fail = nil
	if err := s.RetractWithError(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	if len(out.deliveries) != 2 {
		t.Fatalf("callbacks=%d", len(out.deliveries))
	}
	for i, d := range out.deliveries {
		e, ok := d.Event.(*RequestRetraction)
		if d.Recipient != core.FederateHandle(i+2) || !ok || e.Sender != 1 || e.RetractionHandle != 10 {
			t.Fatalf("callback=%+v", d)
		}
	}
	if err := s.RetractWithError(ctx, "fed", 1, 10); !errors.Is(err, core.ErrRetractionTooLate) {
		t.Fatalf("repeated retraction error=%v", err)
	}
	if len(out.deliveries) != 2 {
		t.Fatal("repeated retraction duplicated callback")
	}
}

type ieeeRetractionOutbox struct {
	deliveries []core.OutboxDelivery
	fail       error
}

func (o *ieeeRetractionOutbox) Send(_ context.Context, _ core.FederationName, h core.FederateHandle, e core.OutboundEvent) error {
	if o.fail != nil {
		return o.fail
	}
	o.deliveries = append(o.deliveries, core.OutboxDelivery{Recipient: h, Event: e})
	return nil
}
func (o *ieeeRetractionOutbox) Reserve(ctx context.Context, _ core.FederationName, ds []core.OutboxDelivery) (core.OutboxReservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if o.fail != nil {
		return nil, o.fail
	}
	return &ieeeRetractionReservation{out: o, ds: append([]core.OutboxDelivery(nil), ds...)}, nil
}

type ieeeRetractionReservation struct {
	out  *ieeeRetractionOutbox
	ds   []core.OutboxDelivery
	done bool
}

func (r *ieeeRetractionReservation) Commit() error {
	if !r.done {
		r.done = true
		r.out.deliveries = append(r.out.deliveries, r.ds...)
	}
	return nil
}
func (r *ieeeRetractionReservation) Release() { r.done = true }
