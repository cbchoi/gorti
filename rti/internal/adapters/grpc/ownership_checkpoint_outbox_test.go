package grpc

import (
	"context"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

type deferredOwnershipCheckpointIdentity struct {
	*ownership.CallbackEvent
	scope core.OwnershipCallbackScope
	id    uint64
}

func (e *deferredOwnershipCheckpointIdentity) Inner() *rtiv1.FederateEvent {
	return &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_OwnershipUnavailable{OwnershipUnavailable: &rtiv1.AttributeOwnershipUnavailable{ObjectHandle: 100, AttributeHandles: []uint64{1}}}}
}
func (e *deferredOwnershipCheckpointIdentity) OwnershipCallbackIdentity() (core.OwnershipCallbackScope, uint64, bool) {
	return e.scope, e.id, true
}

func TestOwnershipCheckpointOutboxRefreshesPrecommitIdentityBeforePublication(t *testing.T) {
	r, raw, _ := requiredCallbackFixture(t, 8)
	o := NewOwnershipCheckpointOutbox(raw, func() *CallbackReceipts { return r })
	ctx := context.Background()
	ch, closeQueue, err := raw.Subscribe(ctx, "f", 2)
	if err != nil {
		t.Fatal(err)
	}
	defer checkTransportCleanup(t, closeQueue)
	event := &deferredOwnershipCheckpointIdentity{CallbackEvent: &ownership.CallbackEvent{}, scope: core.OwnershipCallbackScope{Federation: "f", Recipient: 2, FederationGeneration: 5, CallbackEpoch: 7}}
	p, err := o.Reserve(ctx, "f", []core.OutboxDelivery{{Recipient: 2, Event: event}})
	if err != nil {
		t.Fatal(err)
	}
	if r.pending != 1 {
		t.Fatal("general receipt not reserved before ownership commit")
	}
	select {
	case <-ch:
		t.Fatal("callback escaped before identity commit")
	default:
	}
	event.id = 11
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	p.Release()
	wire, err := EncodeOutboundEvent((<-ch)[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(wire.CallbackReceipt) != 32 || wire.OwnershipReceipt == nil || wire.OwnershipReceipt.CallbackId != 11 || wire.OwnershipReceipt.CallbackEpoch != 7 || wire.OwnershipReceipt.DeliveryAttempt != 0 {
		t.Fatal("precommit cell was frozen or already authorized", wire)
	}
	r.mu.Lock()
	entry := r.entries[string(wire.CallbackReceipt)]
	if entry == nil || !entry.required || !entry.admitted || entry.event.OwnershipReceipt.CallbackId != 11 {
		t.Error("general source ledger lost ownership relationship")
	}
	r.mu.Unlock()
}

func TestOwnershipCheckpointOutboxUnassignedIdentityRollsBack(t *testing.T) {
	r, raw, _ := requiredCallbackFixture(t, 8)
	o := NewOwnershipCheckpointOutbox(raw, func() *CallbackReceipts { return r })
	event := &deferredOwnershipCheckpointIdentity{CallbackEvent: &ownership.CallbackEvent{}, scope: core.OwnershipCallbackScope{Federation: "f", Recipient: 2, FederationGeneration: 5}}
	p, err := o.Reserve(context.Background(), "f", []core.OutboxDelivery{{Recipient: 2, Event: event}})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Commit(); err == nil {
		t.Fatal("zero strict ID published")
	}
	p.Release()
	if r.pending != 0 {
		t.Fatal("unassigned identity left live receipt authority")
	}
	for _, stat := range raw.OutboxStats() {
		if stat.QueueDepth != 0 {
			t.Fatal("invalid callback escaped")
		}
	}
}
