package deliveryledger

import (
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

type unclassifiedEvent struct{ sequence uint64 }

func (e *unclassifiedEvent) Seq() uint64 { return e.sequence }

func ledgerScope() DeliveryScope {
	return DeliveryScope{Federation: "fed", Generation: 7, Federate: 2, Incarnation: 13, CallbackEpoch: 0}
}

func newTestLedger(t *testing.T, capacity int) *DeliveryLedger {
	t.Helper()
	l, err := NewDeliveryLedger(ledgerScope(), capacity)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func ledgerEvent(kind object.CallbackKind, order core.OrderType) *object.CallbackEvent {
	e := &object.CallbackEvent{Kind: kind, Sequence: 11, Metadata: &core.ObjectCallbackMetadata{ReceivedOrder: order}}
	if order == core.OrderTimestamp {
		ts := core.LogicalTime(100)
		e.LogicalTime = &ts
	}
	return e
}

func admitTestEvent(t *testing.T, l *DeliveryLedger, order core.OrderType, stage DeliveryStage) DeliveryTicket {
	t.Helper()
	ticket, err := l.Admit(ledgerEvent(object.CallbackReflect, order), stage)
	if err != nil {
		t.Fatal(err)
	}
	if !ticket.Valid() {
		t.Fatal("admitted user callback has no delivery ticket")
	}
	return ticket
}

func assertConservation(t *testing.T, l *DeliveryLedger) ObservedDeliveryCounts {
	t.Helper()
	s := l.Inspect()
	outstanding := s.OutstandingRO + s.OutstandingTSO
	if s.Admitted != s.Invoked+s.Canceled+s.Invalidated+outstanding {
		t.Fatalf("admission conservation failed: %+v", s)
	}
	var staged uint64
	for _, count := range s.ByStage {
		staged += count
	}
	if staged != outstanding {
		t.Fatalf("stage conservation failed: %+v", s)
	}
	return s
}

func TestDeliveryLedgerTransferDoesNotReadmit(t *testing.T) {
	l := newTestLedger(t, 8)
	first := admitTestEvent(t, l, core.OrderTimestamp, DeliveryTimeBuffer)
	// The same producer sequence may identify multiple split callbacks.
	second := admitTestEvent(t, l, core.OrderTimestamp, DeliveryTimeBuffer)
	ro := admitTestEvent(t, l, core.OrderReceive, DeliveryOutbox)
	if first == second {
		t.Fatal("split callbacks share a transfer identity")
	}
	if err := l.Transfer(first, DeliveryTimeBuffer, DeliveryOutbox, core.OrderReceive); err != nil {
		t.Fatal(err)
	}
	if err := l.Transfer(first, DeliveryTimeBuffer, DeliveryOutbox, core.OrderReceive); err != nil {
		t.Fatal(err)
	}
	if err := l.Transfer(first, DeliveryOutbox, DeliveryTransport, core.OrderReceive); err != nil {
		t.Fatal(err)
	}
	if err := l.Transfer(first, DeliveryTransport, DeliveryCallbackQueue, core.OrderReceive); err != nil {
		t.Fatal(err)
	}
	s := assertConservation(t, l)
	if s.Admitted != 3 || s.OutstandingRO != 2 || s.OutstandingTSO != 1 || s.Invoked != 0 {
		t.Fatalf("handoff/receipt of frame was counted as invocation: %+v", s)
	}
	if err := l.Settle([]DeliveryTicket{first, first, ro}, DeliveryInvoked); err != nil {
		t.Fatal(err)
	}
	if err := l.Settle([]DeliveryTicket{first, ro}, DeliveryInvoked); err != nil {
		t.Fatal(err)
	}
	if err := l.Settle([]DeliveryTicket{second, second}, DeliveryCanceled); err != nil {
		t.Fatal(err)
	}
	s = assertConservation(t, l)
	if s.Invoked != 2 || s.Canceled != 1 || s.OutstandingRO+s.OutstandingTSO != 0 {
		t.Fatalf("duplicate outcomes changed accounting: %+v", s)
	}
}

func TestDeliveryLedgerClassifiesReceivedOrderAndExcludesControls(t *testing.T) {
	l := newTestLedger(t, 8)
	var nilObject *object.CallbackEvent
	for _, event := range []core.OutboundEvent{nil, nilObject, &timepkg.TimeAdvanceGrant{}, &object.CallbackEvent{Kind: object.CallbackDeleteMarker}, &object.CallbackEvent{Kind: object.CallbackDiscover}, &unclassifiedEvent{sequence: 1}} {
		ticket, err := l.Admit(event, DeliveryOutbox)
		if err != nil || ticket.Valid() {
			t.Fatalf("control event %T admitted: %v %v", event, ticket, err)
		}
	}
	for _, kind := range []object.CallbackKind{object.CallbackReflect, object.CallbackReceive, object.CallbackRemove} {
		e := ledgerEvent(kind, core.OrderTimestamp)
		e.Metadata.ReceivedOrder = core.OrderReceive
		if ticket, err := l.Admit(e, DeliveryOutbox); err != nil || !ticket.Valid() {
			t.Fatalf("RO delivery of timestamped callback: %v", err)
		}
	}
	buffered := ledgerEvent(object.CallbackReceive, core.OrderTimestamp)
	buffered.Metadata.ReceivedOrder = core.OrderReceive
	if _, err := l.Admit(buffered, DeliveryTimeBuffer); err != nil {
		t.Fatal(err)
	}
	s := assertConservation(t, l)
	if s.Admitted != 4 || s.OutstandingRO != 3 || s.OutstandingTSO != 1 {
		t.Fatalf("wrong actual order or control classification: %+v", s)
	}
}

func TestDeliveryLedgerMissingOrderLosesCoverage(t *testing.T) {
	l := newTestLedger(t, 4)
	e := ledgerEvent(object.CallbackReceive, core.OrderTimestamp)
	e.Metadata = nil
	if ticket, err := l.Admit(e, DeliveryOutbox); !errors.Is(err, ErrDeliveryOrder) || ticket.Valid() {
		t.Fatalf("timestamp alone fabricated received order: %v", err)
	}
	if !l.Inspect().CoverageLost {
		t.Fatal("missing classification did not invalidate observation coverage")
	}
}

func TestDeliveryLedgerBoundedAndCoverageLossIsSticky(t *testing.T) {
	l := newTestLedger(t, 1)
	first := admitTestEvent(t, l, core.OrderReceive, DeliveryOutbox)
	if ticket, err := l.Admit(ledgerEvent(object.CallbackReflect, core.OrderReceive), DeliveryOutbox); !errors.Is(err, ErrDeliveryCapacity) || ticket.Valid() {
		t.Fatalf("capacity loss = %v, want explicit failure", err)
	}
	if err := l.Settle([]DeliveryTicket{first}, DeliveryInvoked); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		ticket := admitTestEvent(t, l, core.OrderReceive, DeliveryOutbox)
		if err := l.Settle([]DeliveryTicket{ticket}, DeliveryInvoked); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Settle([]DeliveryTicket{first}, DeliveryInvoked); err != nil {
		t.Fatal(err)
	}
	s := assertConservation(t, l)
	if s.Admitted != 101 || s.Invoked != 101 || !s.CoverageLost {
		t.Fatalf("bounded reuse hid coverage loss: %+v", s)
	}
}

func TestDeliveryLedgerEpochCloseFencesLateOutcomes(t *testing.T) {
	for _, transition := range []string{"restore", "resign", "reconnect", "recreate"} {
		t.Run(transition, func(t *testing.T) {
			old := newTestLedger(t, 4)
			ticket := admitTestEvent(t, old, core.OrderTimestamp, DeliveryTimeBuffer)
			admitTestEvent(t, old, core.OrderReceive, DeliveryCallbackQueue)
			old.Close()
			old.Close()
			if err := old.Settle([]DeliveryTicket{ticket}, DeliveryInvoked); !errors.Is(err, ErrDeliveryClosed) {
				t.Fatalf("late outcome = %v", err)
			}
			if _, err := old.Admit(ledgerEvent(object.CallbackReflect, core.OrderReceive), DeliveryOutbox); !errors.Is(err, ErrDeliveryClosed) {
				t.Fatalf("closed admission = %v", err)
			}
			s := assertConservation(t, old)
			if !s.Closed || s.Invalidated != 2 || s.Invoked != 0 {
				t.Fatalf("epoch invalidation: %+v", s)
			}
			scope := ledgerScope()
			switch transition {
			case "restore":
				scope.CallbackEpoch++
				scope.Federate = 5
			case "resign":
				scope.Incarnation++
			case "reconnect": // Even recreated local state with equal scope must fence stale tickets.
			case "recreate":
				scope.Generation++
			}
			current, err := NewDeliveryLedger(scope, 4)
			if err != nil {
				t.Fatal(err)
			}
			newTicket := admitTestEvent(t, current, core.OrderReceive, DeliveryOutbox)
			if err := current.Settle([]DeliveryTicket{newTicket, ticket}, DeliveryInvoked); !errors.Is(err, ErrDeliveryIdentity) {
				t.Fatalf("mixed epoch batch = %v", err)
			}
			s = assertConservation(t, current)
			if s.Invoked != 0 || s.OutstandingRO != 1 {
				t.Fatalf("bad batch partially applied: %+v", s)
			}
		})
	}
}

func TestDeliveryLedgerRejectsInvalidTransfers(t *testing.T) {
	l := newTestLedger(t, 2)
	ticket := admitTestEvent(t, l, core.OrderTimestamp, DeliveryTimeBuffer)
	if err := l.Transfer(ticket, DeliveryOutbox, DeliveryTransport, core.OrderTimestamp); !errors.Is(err, ErrDeliveryTransition) {
		t.Fatalf("wrong source stage = %v", err)
	}
	if err := l.Transfer(ticket, DeliveryTimeBuffer, DeliveryOutbox, core.OrderUnspecified); !errors.Is(err, ErrDeliveryOrder) {
		t.Fatalf("unknown received order = %v", err)
	}
	s := assertConservation(t, l)
	if s.OutstandingTSO != 1 || s.ByStage[DeliveryTimeBuffer] != 1 {
		t.Fatalf("invalid transfer mutated accounting: %+v", s)
	}
	if err := l.Transfer(ticket, DeliveryTimeBuffer, DeliveryOutbox, core.OrderTimestamp); err != nil {
		t.Fatal(err)
	}
	if err := l.Transfer(ticket, DeliveryOutbox, DeliveryTimeBuffer, core.OrderTimestamp); !errors.Is(err, ErrDeliveryTransition) {
		t.Fatalf("backwards handoff = %v", err)
	}
}
