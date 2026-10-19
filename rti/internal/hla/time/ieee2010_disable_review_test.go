package time

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var errIEEE2010ReviewDelivery = errors.New("review delivery rejected")

type ieee2010ReviewEvent struct {
	seq    uint64
	object core.ObjectHandle
	order  core.OrderType
}

func (e *ieee2010ReviewEvent) Seq() uint64                           { return e.seq }
func (e *ieee2010ReviewEvent) DeletionObject() core.ObjectHandle     { return e.object }
func (e *ieee2010ReviewEvent) SetReceivedOrder(order core.OrderType) { e.order = order }

type ieee2010ReviewOutbox struct {
	failSeq uint64
	events  []core.OutboundEvent
}

func (o *ieee2010ReviewOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, e core.OutboundEvent) error {
	if isEnableCallback(e) {
		return nil
	}
	if e.Seq() == o.failSeq {
		return errIEEE2010ReviewDelivery
	}
	o.events = append(o.events, e)
	return nil
}

type ieee2010ReviewReservableOutbox struct {
	*ieee2010ReviewOutbox
	failCommit bool
}

func (o *ieee2010ReviewReservableOutbox) Reserve(_ context.Context, _ core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	if r, ok := enableFixtureAdmission(deliveries); ok {
		return r, nil
	}
	fails := false
	for _, d := range deliveries {
		fails = fails || d.Event.Seq() == o.failSeq
	}
	if fails && !o.failCommit {
		return nil, errIEEE2010ReviewDelivery
	}
	return &ieee2010ReviewReservation{commit: func() error {
		if fails {
			return errIEEE2010ReviewDelivery
		}
		for _, d := range deliveries {
			o.events = append(o.events, d.Event)
		}
		return nil
	}}, nil
}

type ieee2010ReviewReservation struct {
	commit func() error
	done   bool
}

func (r *ieee2010ReviewReservation) Commit() error {
	if r.done {
		return nil
	}
	r.done = true
	return r.commit()
}
func (r *ieee2010ReviewReservation) Release() { r.done = true }

func TestIEEE2010DisableConstrainedFailureRetriesWithoutReenable(t *testing.T) {
	for _, mode := range []string{"legacy-first", "legacy-partial", "reserve", "commit"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			out := &ieee2010ReviewOutbox{failSeq: 2}
			var port core.Outbox = out
			wantDelivered := 0
			switch mode {
			case "legacy-first":
				out.failSeq = 1
			case "legacy-partial":
				wantDelivered = 1
			default:
				port = &ieee2010ReviewReservableOutbox{ieee2010ReviewOutbox: out, failCommit: mode == "commit"}
			}
			m, err := New(Options{Outbox: port, Clock: core.NewManualClock(zeroTime())})
			if err != nil {
				t.Fatal(err)
			}
			if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
				t.Fatal(err)
			}
			first, second := &ieee2010ReviewEvent{seq: 1}, &ieee2010ReviewEvent{seq: 2}
			reservation := m.ReserveTSO("fed", []core.TSOBufferedDelivery{
				{Recipient: 1, Timestamp: 10, Event: first},
				{Recipient: 1, Timestamp: 20, Event: second},
			})
			reservation.Commit(ctx)
			if err := m.DisableConstrained(ctx, "fed", 1); !errors.Is(err, errIEEE2010ReviewDelivery) {
				t.Fatalf("disable failure = %v", err)
			}
			state := m.Metrics("fed", 1)
			if !state.Constrained || state.CurrentTime != 0 || state.Advancing {
				t.Errorf("failed disable must preserve retryable constrained state: %+v", state)
			}
			if len(out.events) != wantDelivered || state.TSOQueued != uint64(2-wantDelivered) {
				t.Errorf("failed %s drain: delivered=%d queued=%d, want %d/%d", mode, len(out.events), state.TSOQueued, wantDelivered, 2-wantDelivered)
			}
			out.failSeq = 0
			if err := m.DisableConstrained(ctx, "fed", 1); err != nil {
				t.Fatalf("disable retry without re-enable failed: %v", err)
			}
			state = m.Metrics("fed", 1)
			if state.Constrained || state.TSOQueued != 0 || state.CurrentTime != 0 {
				t.Fatalf("retry state: %+v", state)
			}
			if len(out.events) != 2 || out.events[0] != first || out.events[1] != second {
				t.Fatalf("retry must deliver each event exactly once in order: %v", out.events)
			}
			if first.order != core.OrderReceive || second.order != core.OrderReceive {
				t.Fatalf("released orders = %v,%v, want RO", first.order, second.order)
			}
		})
	}
}

type ieee2010ReviewLifecycle struct {
	manager   *Manager
	committed int
	pending   int
	finished  int
	locked    bool
}

func (l *ieee2010ReviewLifecycle) PrepareDeletionDelivery(_ core.FederationName, events []core.OutboundEvent) core.OutboxReservation {
	count := 0
	for _, e := range events {
		if deletion, ok := e.(core.ObjectDeletionEvent); ok && deletion.DeletionObject() != 0 {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	return &ieee2010ReviewReservation{commit: func() error {
		l.committed += count
		l.pending += count
		return nil
	}}
}

func (l *ieee2010ReviewLifecycle) FinishDeletionDelivery(fed core.FederationName) {
	lease := extOf(l.manager).evaluatorLock(fed)
	// Detect wrong callback lock ordering without hanging the test process.
	if !lease.TryLock() {
		l.locked = true
		return
	}
	lease.Unlock()
	l.finished += l.pending
	l.pending = 0
}

func TestIEEE2010ResumeScheduledSaveFailureFinishesCommittedDeletion(t *testing.T) {
	ctx := context.Background()
	out := &ieee2010ReviewOutbox{failSeq: 2}
	m, err := New(Options{Outbox: out, Clock: core.NewManualClock(zeroTime())})
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := &ieee2010ReviewLifecycle{manager: m}
	m.SetDeletionLifecycle(lifecycle)
	if err := m.EnableRegulation(ctx, "fed", 3, 1); err != nil {
		t.Fatal(err)
	}
	_, release, err := m.ReserveScheduledSave("fed", 1, []core.FederateHandle{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	removed := &ieee2010ReviewEvent{seq: 1, object: 91}
	later := &ieee2010ReviewEvent{seq: 2}
	r := m.ReserveTSO("fed", []core.TSOBufferedDelivery{
		{Recipient: 1, Timestamp: 5, Event: removed},
		{Recipient: 2, Timestamp: 6, Event: later},
	})
	if len(r.Buffered()) != 2 || len(r.Immediate()) != 0 {
		r.Release()
		t.Fatal("save barrier must hold both callbacks")
	}
	r.Commit(ctx)
	release()
	if err := m.ResumeScheduledSave(ctx, "fed"); !errors.Is(err, errIEEE2010ReviewDelivery) {
		t.Fatalf("resume failure = %v", err)
	}
	if len(out.events) != 1 || out.events[0] != removed || lifecycle.committed != 1 {
		t.Fatalf("first removal was not committed before later failure: events=%v committed=%d", out.events, lifecycle.committed)
	}
	if lifecycle.finished != 1 || lifecycle.pending != 0 || lifecycle.locked {
		t.Errorf("resume error must finish committed hooks outside evaluator: %+v", lifecycle)
	}
	if queued := m.Metrics("fed", 2).TSOQueued; queued != 1 {
		t.Fatalf("failed recipient queue=%d, want 1", queued)
	}
	out.failSeq = 0
	if err := m.ResumeScheduledSave(ctx, "fed"); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 2 || out.events[1] != later || lifecycle.committed != 1 || lifecycle.finished != 1 || lifecycle.locked {
		t.Fatalf("resume retry duplicated delivery/hooks or held evaluator: events=%v lifecycle=%+v", out.events, lifecycle)
	}
}
