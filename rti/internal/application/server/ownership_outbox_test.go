package server

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

type ownershipProjectionSink struct {
	events  []core.OutboundEvent
	failure error
}

func (o *ownershipProjectionSink) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, event core.OutboundEvent) error {
	o.events = append(o.events, event)
	return o.failure
}

func TestOwnershipOutboxProjectsEachRecipientWithoutMutatingSource(t *testing.T) {
	sink := &ownershipProjectionSink{}
	projection := ownershipOutbox{next: sink, project: func(_ context.Context, _ core.FederationName, recipient core.FederateHandle, _ core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, error) {
		return []core.AttributeHandle{attrs[0] + core.AttributeHandle(recipient)}, nil
	}}
	source := &ownership.CallbackEvent{Kind: ownership.CallbackAcquisitionNotification, Object: 7, Attributes: []core.AttributeHandle{20}, Tag: []byte{9}, Sequence: 4}
	for _, recipient := range []core.FederateHandle{1, 2} {
		if err := projection.Send(context.Background(), "f", recipient, source); err != nil {
			t.Fatal(err)
		}
	}
	for i, event := range sink.events {
		got := event.(*ownership.CallbackEvent)
		if got == source || got.Attributes[0] != core.AttributeHandle(21+i) || got.Sequence != 4 {
			t.Fatalf("projection=%+v", got)
		}
		got.Tag[0] = 0
	}
	if source.Attributes[0] != 20 || source.Tag[0] != 9 {
		t.Fatal("projection mutated shared callback")
	}
}

func TestOwnershipOutboxDoesNotDeliverUnmappedHandles(t *testing.T) {
	failure := errors.New("projection unavailable")
	sink := &ownershipProjectionSink{}
	o := ownershipOutbox{next: sink, project: func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) ([]core.AttributeHandle, error) {
		return nil, failure
	}}
	if err := o.Send(context.Background(), "f", 2, &ownership.CallbackEvent{}); !errors.Is(err, failure) || len(sink.events) != 0 {
		t.Fatalf("failed projection delivered: %v, %+v", err, sink.events)
	}
}

type ownershipProjectionReservableSink struct {
	ownershipProjectionSink
	reservations int
	deliveries   []core.OutboxDelivery
}
type ownershipProjectionReservation struct{}

func (ownershipProjectionReservation) Commit() error { return nil }
func (ownershipProjectionReservation) Release()      {}
func (o *ownershipProjectionReservableSink) Reserve(_ context.Context, _ core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	o.reservations++
	o.deliveries = deliveries
	return ownershipProjectionReservation{}, nil
}

func TestOwnershipOutboxReservesWholeProjectedFanout(t *testing.T) {
	failure := errors.New("unmapped recipient")
	sink := &ownershipProjectionReservableSink{}
	fail := true
	o := ownershipOutbox{next: sink, project: func(_ context.Context, _ core.FederationName, h core.FederateHandle, _ core.ObjectHandle, _ []core.AttributeHandle) ([]core.AttributeHandle, error) {
		if fail && h == 2 {
			return nil, failure
		}
		return []core.AttributeHandle{core.AttributeHandle(h) + 10}, nil
	}}
	source := &ownership.CallbackEvent{Attributes: []core.AttributeHandle{7}}
	deliveries := []core.OutboxDelivery{{Recipient: 1, Event: source}, {Recipient: 2, Event: source}}
	if r, err := o.Reserve(context.Background(), "f", deliveries); !errors.Is(err, failure) || r != nil || sink.reservations != 0 {
		t.Fatalf("partial fanout admitted: %v", err)
	}
	fail = false
	r, err := o.Reserve(context.Background(), "f", deliveries)
	if err != nil || r == nil || sink.reservations != 1 || len(sink.deliveries) != 2 {
		t.Fatalf("reservation=%v error=%v", r, err)
	}
	for i, d := range sink.deliveries {
		if d.Event.(*ownership.CallbackEvent).Attributes[0] != core.AttributeHandle(i+11) {
			t.Fatal("reservation used wrong coordinates")
		}
	}
	if source.Attributes[0] != 7 {
		t.Fatal("reservation changed source callback")
	}
}
