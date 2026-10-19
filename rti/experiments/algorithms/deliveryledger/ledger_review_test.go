package deliveryledger

import (
	"errors"
	"math/rand"
	"reflect"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func TestDeliveryLedgerFailedTransferInvalidatesCoverage(t *testing.T) {
	l := newTestLedger(t, 2)
	ticket := admitTestEvent(t, l, core.OrderReceive, DeliveryOutbox)
	if err := l.Transfer(ticket, DeliveryTransport, DeliveryCallbackQueue, core.OrderReceive); !errors.Is(err, ErrDeliveryTransition) {
		t.Fatalf("missing handoff = %v", err)
	}
	if !l.Inspect().CoverageLost {
		t.Fatal("missed intermediate handoff left observation coverage healthy")
	}
	if err := l.Settle([]DeliveryTicket{ticket}, DeliveryInvoked); err != nil {
		t.Fatal(err)
	}
	if !l.Inspect().CoverageLost {
		t.Fatal("completion repaired lost coverage")
	}
}

func TestDeliveryLedgerRejectsInvalidScopeAndCapacity(t *testing.T) {
	for _, field := range []string{"federation", "generation", "federate", "incarnation"} {
		scope := ledgerScope()
		switch field {
		case "federation":
			scope.Federation = ""
		case "generation":
			scope.Generation = 0
		case "federate":
			scope.Federate = 0
		case "incarnation":
			scope.Incarnation = 0
		}
		if _, err := NewDeliveryLedger(scope, 1); !errors.Is(err, ErrDeliveryIdentity) {
			t.Errorf("%s: %v", field, err)
		}
	}
	for _, capacity := range []int{-1, 0, MaxOutstanding + 1} {
		if _, err := NewDeliveryLedger(ledgerScope(), capacity); !errors.Is(err, ErrDeliveryCapacity) {
			t.Errorf("capacity %d: %v", capacity, err)
		}
	}
}

func TestDeliveryLedgerMetadataIsNotRetained(t *testing.T) {
	l := newTestLedger(t, 2)
	e := ledgerEvent(object.CallbackReceive, core.OrderTimestamp)
	ticket, err := l.Admit(e, DeliveryOutbox)
	if err != nil {
		t.Fatal(err)
	}
	e.Metadata.ReceivedOrder = core.OrderReceive
	e.LogicalTime = nil
	snapshot := l.Inspect()
	snapshot.OutstandingTSO = 0
	snapshot.ByStage[DeliveryOutbox] = 0
	if got := l.Inspect(); got.OutstandingTSO != 1 || got.ByStage[DeliveryOutbox] != 1 {
		t.Fatalf("aliased observation: %+v", got)
	}
	if err := l.Settle([]DeliveryTicket{ticket}, DeliveryCanceled); err != nil {
		t.Fatal(err)
	}
	if len(l.entries) != 0 {
		t.Fatal("completed payload/tombstone retained")
	}
}

func TestDeliveryLedgerRejectsUnknownBatchAtomically(t *testing.T) {
	for _, bad := range []DeliveryTicket{{}, {sequence: 1}} {
		l := newTestLedger(t, 2)
		valid := admitTestEvent(t, l, core.OrderReceive, DeliveryOutbox)
		if bad.sequence != 0 {
			bad.owner = l
			bad.sequence = valid.sequence + 1
		}
		if err := l.Settle([]DeliveryTicket{valid, bad}, DeliveryInvoked); !errors.Is(err, ErrDeliveryIdentity) {
			t.Fatalf("bad ticket accepted: %v", err)
		}
		if s := l.Inspect(); s.Invoked != 0 || s.OutstandingRO != 1 {
			t.Fatalf("partial application: %+v", s)
		}
	}
}

func TestDeliveryLedgerRetiredTicketsNeverReappear(t *testing.T) {
	l := newTestLedger(t, 1)
	ticket := admitTestEvent(t, l, core.OrderReceive, DeliveryOutbox)
	if err := l.Settle([]DeliveryTicket{ticket}, DeliveryCanceled); err != nil {
		t.Fatal(err)
	}
	if err := l.Settle([]DeliveryTicket{ticket}, DeliveryInvoked); err != nil {
		t.Fatal(err)
	}
	if err := l.Transfer(ticket, DeliveryOutbox, DeliveryTransport, core.OrderReceive); err != nil {
		t.Fatal(err)
	}
	s := assertConservation(t, l)
	if s.Canceled != 1 || s.Invoked != 0 || len(l.entries) != 0 {
		t.Fatalf("late outcome revived canceled event: %+v", s)
	}
}

func TestDeliveryLedgerTransferValidationAfterRetirement(t *testing.T) {
	for _, state := range []struct {
		name    string
		outcome DeliveryOutcome
	}{
		{"outstanding", 0},
		{"invoked", DeliveryInvoked},
		{"canceled", DeliveryCanceled},
	} {
		t.Run(state.name, func(t *testing.T) {
			for _, transfer := range []struct {
				name     string
				from, to DeliveryStage
				order    core.OrderType
				wantErr  error
			}{
				{"unspecified-order", DeliveryOutbox, DeliveryTransport, core.OrderUnspecified, ErrDeliveryOrder},
				{"invalid-source", 0, DeliveryTransport, core.OrderReceive, ErrDeliveryTransition},
				{"invalid-destination", DeliveryOutbox, 0, core.OrderReceive, ErrDeliveryTransition},
				{"skipped-stage", DeliveryOutbox, DeliveryCallbackQueue, core.OrderReceive, ErrDeliveryTransition},
			} {
				t.Run(transfer.name, func(t *testing.T) {
					l := newTestLedger(t, 1)
					ticket := admitTestEvent(t, l, core.OrderReceive, DeliveryOutbox)
					if state.outcome != 0 {
						if err := l.Settle([]DeliveryTicket{ticket}, state.outcome); err != nil {
							t.Fatal(err)
						}
						// Reuse capacity while the old ticket's late transfer arrives.
						admitTestEvent(t, l, core.OrderTimestamp, DeliveryTimeBuffer)
					}
					want := l.Inspect()
					var wantErr error
					if state.outcome == 0 {
						wantErr = transfer.wantErr
						want.CoverageLost = true
					}
					if err := l.Transfer(ticket, transfer.from, transfer.to, transfer.order); !errors.Is(err, wantErr) {
						t.Errorf("transfer = %v, want %v", err, wantErr)
					}
					if got := assertConservation(t, l); got != want {
						t.Errorf("snapshot = %+v, want %+v", got, want)
					}
				})
			}
		})
	}
}

func TestDeliveryLedgerTransferRejectsForeignAndClosedTickets(t *testing.T) {
	l := newTestLedger(t, 1)
	local := admitTestEvent(t, l, core.OrderReceive, DeliveryOutbox)
	foreignLedger := newTestLedger(t, 1)
	foreign := admitTestEvent(t, foreignLedger, core.OrderReceive, DeliveryOutbox)
	if err := foreignLedger.Settle([]DeliveryTicket{foreign}, DeliveryCanceled); err != nil {
		t.Fatal(err)
	}
	before := l.Inspect()
	for _, ticket := range []DeliveryTicket{{}, foreign, {owner: l, sequence: local.sequence + 1}} {
		if err := l.Transfer(ticket, 0, 0, core.OrderUnspecified); !errors.Is(err, ErrDeliveryIdentity) {
			t.Errorf("foreign/unissued transfer = %v", err)
		}
	}
	if got := l.Inspect(); got != before {
		t.Errorf("rejected tickets changed snapshot: %+v", got)
	}
	if err := l.Settle([]DeliveryTicket{local}, DeliveryCanceled); err != nil {
		t.Fatal(err)
	}
	l.Close()
	before = l.Inspect()
	if err := l.Transfer(local, 0, 0, core.OrderUnspecified); !errors.Is(err, ErrDeliveryClosed) {
		t.Errorf("closed transfer = %v", err)
	}
	if got := l.Inspect(); got != before {
		t.Errorf("closed transfer changed snapshot: %+v", got)
	}
}

func TestDeliveryLedgerSequenceExhaustionCannotWrap(t *testing.T) {
	l := newTestLedger(t, 2)
	l.sequence = ^uint64(0) - 1
	l.counts.Admitted, l.counts.Invoked = l.sequence, l.sequence
	ticket := admitTestEvent(t, l, core.OrderReceive, DeliveryOutbox)
	if ticket.sequence != ^uint64(0) {
		t.Fatal("final ticket not allocated")
	}
	if _, err := l.Admit(ledgerEvent(object.CallbackReceive, core.OrderReceive), DeliveryOutbox); !errors.Is(err, ErrDeliverySequence) {
		t.Fatalf("wrapped admission: %v", err)
	}
	if err := l.Settle([]DeliveryTicket{ticket}, DeliveryInvoked); err != nil {
		t.Fatal(err)
	}
	s := assertConservation(t, l)
	if s.Admitted != ^uint64(0) || s.Invoked != ^uint64(0) || !s.CoverageLost {
		t.Fatalf("counter overflow: %+v", s)
	}
}

func TestDeliveryLedgerIndependentConservationModel(t *testing.T) {
	// The oracle retains an append-only history and recomputes the expected
	// snapshot, unlike the production ledger's retiring map and accumulators.
	type row struct {
		ticket  DeliveryTicket
		stage   DeliveryStage
		tso     bool
		outcome DeliveryOutcome
	}
	l := newTestLedger(t, 4096)
	var history []row
	rng := rand.New(rand.NewSource(7102010))
	for step := 0; step < 4000; step++ {
		choice := rng.Intn(4)
		if len(history) == 0 || choice == 0 {
			tso := rng.Intn(2) == 0
			order, stage := core.OrderReceive, DeliveryOutbox
			if tso {
				order, stage = core.OrderTimestamp, DeliveryTimeBuffer
			}
			ticket := admitTestEvent(t, l, order, stage)
			history = append(history, row{ticket: ticket, stage: stage, tso: tso})
		} else {
			r := &history[rng.Intn(len(history))]
			if choice == 1 && r.outcome == 0 && r.stage < DeliveryCallbackQueue {
				order := core.OrderReceive
				if r.tso {
					order = core.OrderTimestamp
				}
				if r.stage == DeliveryTimeBuffer && rng.Intn(2) == 0 {
					order = core.OrderReceive
				}
				if err := l.Transfer(r.ticket, r.stage, r.stage+1, order); err != nil {
					t.Fatal(err)
				}
				r.stage++
				r.tso = order == core.OrderTimestamp
			} else if choice >= 2 {
				outcome := DeliveryInvoked
				if choice == 3 {
					outcome = DeliveryCanceled
				}
				if err := l.Settle([]DeliveryTicket{r.ticket, r.ticket}, outcome); err != nil {
					t.Fatal(err)
				}
				if r.outcome == 0 {
					r.outcome = outcome
				}
			}
		}
		want := ObservedDeliveryCounts{Scope: ledgerScope(), Admitted: uint64(len(history))}
		for _, r := range history {
			switch r.outcome {
			case DeliveryInvoked:
				want.Invoked++
			case DeliveryCanceled:
				want.Canceled++
			default:
				want.ByStage[r.stage]++
				if r.tso {
					want.OutstandingTSO++
				} else {
					want.OutstandingRO++
				}
			}
		}
		if got := l.Inspect(); !reflect.DeepEqual(got, want) {
			t.Fatalf("step %d: got %+v, want %+v", step, got, want)
		}
	}
	s := l.Inspect()
	l.Close()
	if got := assertConservation(t, l); got.Invalidated != s.OutstandingRO+s.OutstandingTSO {
		t.Fatalf("remaining history invalidation: %+v", got)
	}
}

func TestDeliveryLedgerConcurrentAdmissionAndOutcomes(t *testing.T) {
	l := newTestLedger(t, 64)
	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				ticket, err := l.Admit(ledgerEvent(object.CallbackReceive, core.OrderReceive), DeliveryOutbox)
				if err != nil {
					t.Error(err)
					return
				}
				if err := l.Transfer(ticket, DeliveryOutbox, DeliveryTransport, core.OrderReceive); err != nil {
					t.Error(err)
					return
				}
				if err := l.Settle([]DeliveryTicket{ticket, ticket}, DeliveryInvoked); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	for i := 0; i < 1000; i++ {
		assertConservation(t, l)
	}
	wg.Wait()
	s := assertConservation(t, l)
	if s.Admitted != 1600 || s.Invoked != 1600 || len(l.entries) != 0 || s.CoverageLost {
		t.Fatalf("concurrent accounting: %+v", s)
	}
}

func TestDeliveryLedgerConcurrentCloseAndDuplicateOutcomes(t *testing.T) {
	l := newTestLedger(t, 256)
	tickets := make([]DeliveryTicket, 256)
	for i := range tickets {
		tickets[i] = admitTestEvent(t, l, core.OrderReceive, DeliveryOutbox)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			if worker == 0 {
				l.Close()
				return
			}
			for _, ticket := range tickets {
				outcome := DeliveryInvoked
				if worker%2 == 0 {
					outcome = DeliveryCanceled
				}
				if err := l.Settle([]DeliveryTicket{ticket, ticket}, outcome); err != nil && !errors.Is(err, ErrDeliveryClosed) {
					t.Error(err)
				}
			}
		}(worker)
	}
	close(start)
	wg.Wait()
	s := assertConservation(t, l)
	if !s.Closed || s.Invoked+s.Canceled+s.Invalidated != 256 || len(l.entries) != 0 {
		t.Fatalf("close/outcome race: %+v", s)
	}
}
