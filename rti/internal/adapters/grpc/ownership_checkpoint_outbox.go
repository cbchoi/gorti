package grpc

import (
	"context"
	"fmt"
	"slices"
	"sync"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/protobuf/proto"
)

// OwnershipCheckpointOutbox reserves general invocation identity before queue
// publication, but reads the ownership identity cell only at commit.
type OwnershipCheckpointOutbox struct {
	next     core.ReservableOutbox
	receipts func() *CallbackReceipts
}

func NewOwnershipCheckpointOutbox(next core.ReservableOutbox, receipts func() *CallbackReceipts) *OwnershipCheckpointOutbox {
	return &OwnershipCheckpointOutbox{next: next, receipts: receipts}
}

func (o *OwnershipCheckpointOutbox) Send(ctx context.Context, fed core.FederationName, h core.FederateHandle, event core.OutboundEvent) error {
	p, err := o.Reserve(ctx, fed, []core.OutboxDelivery{{Recipient: h, Event: event}})
	if err != nil {
		return err
	}
	defer p.Release()
	return p.Commit()
}

func (o *OwnershipCheckpointOutbox) Reserve(ctx context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	if o == nil || o.next == nil || o.receipts == nil || o.receipts() == nil {
		return nil, fmt.Errorf("ownership checkpoint: general ledger unavailable")
	}
	r := o.receipts()
	p := &ownershipCheckpointPublication{reservation: &mandatoryCallbackReservation{receipts: r}}
	wireDeliveries := make([]core.OutboxDelivery, 0, len(deliveries))
	for _, delivery := range deliveries {
		event, err := toDispatchFederateEvent(delivery.Event)
		if err != nil || event == nil {
			p.Release()
			if err == nil {
				err = fmt.Errorf("ownership checkpoint: unconvertible callback")
			}
			return nil, err
		}
		ticket, rollback, err := r.PrepareRequired(ctx, fed, delivery.Recipient, event)
		if err != nil {
			p.Release()
			return nil, err
		}
		p.reservation.tickets = append(p.reservation.tickets, ticket)
		p.reservation.rollbacks = append(p.reservation.rollbacks, rollback)
		event.CallbackReceipt = slices.Clone(ticket)
		if _, ok := delivery.Event.(core.OwnershipCallbackIdentity); ok {
			p.identities = append(p.identities, ownershipCheckpointPublicationIdentity{source: delivery.Event, event: event, ticket: ticket})
		}
		wireDeliveries = append(wireDeliveries, core.OutboxDelivery{Recipient: delivery.Recipient, Event: &requiredCallbackEvent{event: event}})
	}
	queue, err := o.next.Reserve(ctx, fed, wireDeliveries)
	if err != nil || queue == nil {
		p.Release()
		if err == nil {
			err = fmt.Errorf("ownership checkpoint: queue reservation unavailable")
		}
		return nil, err
	}
	p.reservation.queue = queue
	return p, nil
}

type ownershipCheckpointPublicationIdentity struct {
	source core.OutboundEvent
	event  *rtiv1.FederateEvent
	ticket []byte
}

// RefreshOwnershipCheckpointIdentity must run after ownership commit fills its
// identity cell, but before queue publication. Generic mixed deletion fanouts
// use this same hook; merely wrapping the ownership manager's outbox is not enough.
func RefreshOwnershipCheckpointIdentity(source core.OutboundEvent, event *rtiv1.FederateEvent) error {
	identity, ok := source.(core.OwnershipCallbackIdentity)
	if !ok {
		return nil
	}
	if event == nil {
		return core.ErrOwnershipCallbackReceipt
	}
	scope, id, strict := identity.OwnershipCallbackIdentity()
	event.OwnershipReceipt = nil
	if strict {
		if id == 0 || scope.FederationGeneration == 0 {
			return core.ErrOwnershipCallbackReceipt
		}
		event.OwnershipReceipt = &rtiv1.OwnershipCallbackReceipt{FederationGeneration: scope.FederationGeneration, CallbackEpoch: scope.CallbackEpoch, CallbackId: id}
	}
	return nil
}

type ownershipCheckpointPublication struct {
	mu          sync.Mutex
	reservation *mandatoryCallbackReservation
	identities  []ownershipCheckpointPublicationIdentity
	done        bool
	err         error
}

func (p *ownershipCheckpointPublication) Commit() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done {
		return p.err
	}
	p.done = true
	for _, identity := range p.identities {
		if err := RefreshOwnershipCheckpointIdentity(identity.source, identity.event); err != nil {
			p.reservation.Release()
			p.err = err
			return p.err
		}
		r := p.reservation.receipts
		r.mu.Lock()
		if entry := r.entries[string(identity.ticket)]; entry != nil && entry.event != nil {
			entry.event.OwnershipReceipt = nil
			if identity.event.OwnershipReceipt != nil {
				entry.event.OwnershipReceipt = proto.Clone(identity.event.OwnershipReceipt).(*rtiv1.OwnershipCallbackReceipt)
			}
		}
		r.mu.Unlock()
	}
	p.err = p.reservation.Commit()
	return p.err
}

func (p *ownershipCheckpointPublication) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.done {
		p.done = true
		p.reservation.Release()
	}
}
