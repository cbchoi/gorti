package time

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func newROTestManager(t *testing.T, out core.Outbox) *Manager {
	t.Helper()
	m, err := New(Options{Outbox: out, Clock: core.NewManualClock(zeroTime())})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func enqueueROTest(t *testing.T, m *Manager, seq uint64) *ieee2010ReviewEvent {
	t.Helper()
	e := &ieee2010ReviewEvent{seq: seq}
	r := m.ReserveTSO("fed", []core.MessageDelivery{{Recipient: 1, Event: e, Sender: 2, ReceiveOrder: true}})
	if len(r.Immediate()) != 0 || len(r.Buffered()) != 1 {
		r.Release()
		t.Fatalf("idle constrained receiver admitted RO immediately: immediate=%d buffered=%d", len(r.Immediate()), len(r.Buffered()))
	}
	r.Commit(context.Background())
	return e
}

func TestROEligibilityMatrix(t *testing.T) {
	for _, constrained := range []bool{false, true} {
		for _, regulating := range []bool{false, true} {
			for _, async := range []bool{false, true} {
				for _, advancing := range []bool{false, true} {
					t.Run(testROStateName(constrained, regulating, async, advancing), func(t *testing.T) {
						ctx := context.Background()
						m := newROTestManager(t, &ieee2010ReviewOutbox{})
						if constrained {
							if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
								t.Fatal(err)
							}
						}
						if regulating {
							if err := m.EnableRegulation(ctx, "fed", 1, 1); err != nil {
								t.Fatal(err)
							}
						}
						if async {
							if err := m.EnableAsynchronousDelivery(ctx, "fed", 1); err != nil {
								t.Fatal(err)
							}
						}
						if advancing && (constrained || regulating) {
							if err := m.recordAdvance("fed", 1, 10, ModeTAR); err != nil {
								t.Fatal(err)
							}
						}
						e := &ieee2010ReviewEvent{seq: 1}
						r := m.ReserveTSO("fed", []core.MessageDelivery{{Recipient: 1, Event: e, ReceiveOrder: true}})
						defer r.Release()
						want := !constrained || async || advancing
						if got := len(r.Immediate()) == 1; got != want {
							t.Errorf("immediate=%v want %v", got, want)
						}
						if e.order != core.OrderReceive {
							t.Errorf("RO changed to %v", e.order)
						}
					})
				}
			}
		}
	}
}

func testROStateName(c, r, a, p bool) string {
	name := ""
	for i, v := range []bool{c, r, a, p} {
		name += []string{"c", "r", "a", "p"}[i]
		if v {
			name += "1"
		} else {
			name += "0"
		}
	}
	return name
}

func TestROReleaseBeforeEveryAdvanceGrant(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeTAR, ModeTARA, ModeNER, ModeNMRA, ModeFQR} {
		t.Run(mode.String(), func(t *testing.T) {
			ctx := context.Background()
			out := &ieee2010ReviewOutbox{failSeq: 99}
			m := newROTestManager(t, &ieee2010ReviewReservableOutbox{ieee2010ReviewOutbox: out})
			if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
				t.Fatal(err)
			}
			first := enqueueROTest(t, m, 1)
			second := enqueueROTest(t, m, 2)
			if err := m.dispatchAdvance(ctx, "fed", 1, 10, mode); err != nil {
				t.Fatal(err)
			}
			if len(out.events) != 3 || out.events[0] != first || out.events[1] != second {
				t.Fatalf("RO must precede TAG exactly once: %v", out.events)
			}
			if _, ok := out.events[2].(*TimeAdvanceGrant); !ok {
				t.Fatalf("last event=%T", out.events[2])
			}
		})
	}
}

func TestROBlockedAdvanceStillReleasesMessages(t *testing.T) {
	ctx := context.Background()
	out := &ieee2010ReviewOutbox{failSeq: 99}
	m := newROTestManager(t, out)
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableRegulation(ctx, "fed", 2, 1); err != nil {
		t.Fatal(err)
	}
	e := enqueueROTest(t, m, 1)
	if err := m.TimeAdvanceRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 1 || out.events[0] != e || !m.Metrics("fed", 1).Advancing {
		t.Fatalf("blocked TAR must release RO without grant: %v", out.events)
	}
}

func TestROReleaseFailureRemainsRetryable(t *testing.T) {
	for _, operation := range []string{roOperationAsync, roOperationConstraint, roOperationGrant} {
		for _, failure := range []string{"reserve", "commit", "legacy"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				ctx := context.Background()
				out := &ieee2010ReviewOutbox{failSeq: 1}
				var port core.Outbox = out
				if failure != "legacy" {
					port = &ieee2010ReviewReservableOutbox{ieee2010ReviewOutbox: out, failCommit: failure == "commit"}
				}
				m := newROTestManager(t, port)
				if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
					t.Fatal(err)
				}
				e := enqueueROTest(t, m, 1)
				act := func() error {
					switch operation {
					case roOperationAsync:
						return m.EnableAsynchronousDelivery(ctx, "fed", 1)
					case roOperationConstraint:
						return m.DisableConstrained(ctx, "fed", 1)
					default:
						return m.TimeAdvanceRequest(ctx, "fed", 1, 10)
					}
				}
				if err := act(); !errors.Is(err, errIEEE2010ReviewDelivery) {
					t.Fatalf("release error=%v", err)
				}
				state := m.Metrics("fed", 1)
				if len(out.events) != 0 || state.CurrentTime != 0 || state.AsynchronousDelivery || !state.Constrained {
					t.Fatalf("failed release changed state: %+v events=%v", state, out.events)
				}
				out.failSeq = 99
				var err error
				if operation == roOperationGrant {
					err = m.tryGrantPending(ctx, "fed")
				} else {
					err = act()
				}
				if err != nil {
					t.Fatal(err)
				}
				want := 1
				if operation == roOperationGrant {
					want = 2
				}
				if len(out.events) != want || out.events[0] != e {
					t.Fatalf("retry duplicated/lost RO: %v", out.events)
				}
			})
		}
	}
}

func TestROAsyncDisableDoesNotRecallAndResignClearsQueue(t *testing.T) {
	ctx := context.Background()
	out := &ieee2010ReviewOutbox{failSeq: 99}
	m := newROTestManager(t, out)
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	first := enqueueROTest(t, m, 1)
	if err := m.EnableAsynchronousDelivery(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.DisableAsynchronousDelivery(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	enqueueROTest(t, m, 2)
	if len(out.events) != 1 || out.events[0] != first {
		t.Fatalf("async transition events=%v", out.events)
	}
	m.OnFederateResign(ctx, "fed", 1)
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableAsynchronousDelivery(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 1 {
		t.Fatalf("resigned receiver queue leaked: %v", out.events)
	}
}

func TestROPersistenceIsIndependentOfTSOBounds(t *testing.T) {
	ctx := context.Background()
	out := &ieee2010ReviewOutbox{failSeq: 99}
	m := newROTestManager(t, out)
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	enqueueROTest(t, m, 1)
	clone := func(event core.OutboundEvent) (core.OutboundEvent, error) {
		eventCopy := *event.(*ieee2010ReviewEvent)
		return &eventCopy, nil
	}
	state, err := m.ExportState("fed", clone)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Federates) != 1 || len(state.Federates[0].ROQueue) != 1 || len(state.Federates[0].Queue) != 0 {
		t.Fatalf("wrong queue snapshot: %+v", state)
	}
	metrics := m.Metrics("fed", 1)
	if metrics.ROQueued != 1 || metrics.TSOQueued != 0 || metrics.LITS != nil || metrics.GALT != nil {
		t.Fatalf("RO contaminated TSO metrics: %+v", metrics)
	}
	commit, err := m.PrepareImportState("restored", state, clone)
	if err != nil {
		t.Fatal(err)
	}
	state.Federates[0].ROQueue[0].Event.(*ieee2010ReviewEvent).seq = 88
	state.Federates[0].ROQueue = nil
	commit()
	commit()
	if len(out.events) != 0 {
		t.Fatal("restore must not expose a queued callback")
	}
	if err := m.ResumeScheduledSave(ctx, "restored"); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 0 {
		t.Fatal("restored idle constrained receiver released RO")
	}
	if err := m.EnableAsynchronousDelivery(ctx, "restored", 1); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 1 || out.events[0].Seq() != 1 {
		t.Fatalf("restore aliased caller state: %v", out.events)
	}
	m.OnFederationDestroyed("fed")
	if got := m.Metrics("fed", 1).ROQueued; got != 0 {
		t.Fatalf("destroy retained %d RO messages", got)
	}
}

func TestROAndTSODisableDrainIsAtomic(t *testing.T) {
	ctx := context.Background()
	out := &ieee2010ReviewOutbox{failSeq: 2}
	m := newROTestManager(t, &ieee2010ReviewReservableOutbox{ieee2010ReviewOutbox: out})
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	enqueueROTest(t, m, 1)
	r := m.ReserveTSO("fed", []core.MessageDelivery{{Recipient: 1, Timestamp: 10, Event: &ieee2010ReviewEvent{seq: 2}}})
	r.Commit(ctx)
	if err := m.DisableConstrained(ctx, "fed", 1); !errors.Is(err, errIEEE2010ReviewDelivery) {
		t.Fatalf("disable error=%v", err)
	}
	state := m.Metrics("fed", 1)
	if len(out.events) != 0 || state.ROQueued != 1 || state.TSOQueued != 1 || !state.Constrained {
		t.Fatalf("mixed drain was partial: %+v events=%v", state, out.events)
	}
	out.failSeq = 99
	if err := m.DisableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 2 || out.events[0].Seq() != 1 || out.events[1].Seq() != 2 {
		t.Fatalf("mixed retry=%v", out.events)
	}
}

type roSubscriptionFilter struct{ allowed bool }

func (f *roSubscriptionFilter) FilterQueuedMessage(_ context.Context, _ core.FederationName, _ core.FederateHandle, e core.OutboundEvent) core.OutboundEvent {
	if !f.allowed {
		return nil
	}
	eventCopy := *e.(*ieee2010ReviewEvent)
	eventCopy.seq += 10
	return &eventCopy
}

func TestROFilterRetryRetainsOriginalAndCanDiscard(t *testing.T) {
	for _, operation := range []string{roOperationAsync, roOperationGrant, roOperationConstraint} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			out := &ieee2010ReviewOutbox{failSeq: 11}
			m := newROTestManager(t, &ieee2010ReviewReservableOutbox{ieee2010ReviewOutbox: out})
			filter := &roSubscriptionFilter{allowed: true}
			m.SetQueuedMessageFilter(filter)
			if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
				t.Fatal(err)
			}
			original := enqueueROTest(t, m, 1)
			act := func() error {
				switch operation {
				case roOperationAsync:
					return m.EnableAsynchronousDelivery(ctx, "fed", 1)
				case roOperationConstraint:
					return m.DisableConstrained(ctx, "fed", 1)
				default:
					return m.TimeAdvanceRequest(ctx, "fed", 1, 10)
				}
			}
			if err := act(); !errors.Is(err, errIEEE2010ReviewDelivery) {
				t.Fatalf("failure=%v", err)
			}
			if original.seq != 1 || m.Metrics("fed", 1).ROQueued != 1 {
				t.Fatal("failed filtering changed original queue")
			}
			filter.allowed = false
			var err error
			if operation == roOperationGrant {
				err = m.tryGrantPending(ctx, "fed")
			} else {
				err = act()
			}
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if operation == roOperationGrant {
				want = 1
			}
			if len(out.events) != want || m.Metrics("fed", 1).ROQueued != 0 {
				t.Fatalf("unsubscribed RO retained/delivered: %v", out.events)
			}
		})
	}
}
