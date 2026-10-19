package server

import (
	"context"
	"errors"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

// Ownership state uses registered-class coordinates; a callback uses the class
// conveyed to its recipient. Managers release object leases before delivery.
type ownershipOutbox struct {
	next    core.Outbox
	project func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) ([]core.AttributeHandle, error)
	// capture guards only unsolicited assumptions, whose old knowledge lifetime
	// can be forgotten. Terminal callbacks stay on project until their separate
	// precommit/receipt obligation is implemented.
	capture func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (object.KnownAttributeProjection, error)
}

func (o ownershipOutbox) Send(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, event core.OutboundEvent) error {
	projected, err := o.projectEvent(ctx, fed, recipient, event)
	if err != nil {
		return err
	}
	return o.next.Send(ctx, fed, recipient, projected)
}

func (o ownershipOutbox) Reserve(ctx context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	reservable, ok := o.next.(core.ReservableOutbox)
	if !ok {
		return nil, errors.New("ownership callback reservation unavailable")
	}
	projected := make([]core.OutboxDelivery, len(deliveries))
	for i, delivery := range deliveries {
		event, err := o.projectEvent(ctx, fed, delivery.Recipient, delivery.Event)
		if err != nil {
			return nil, err
		}
		projected[i] = core.OutboxDelivery{Recipient: delivery.Recipient, Event: event}
	}
	return reservable.Reserve(ctx, fed, projected)
}

func (o ownershipOutbox) projectEvent(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, event core.OutboundEvent) (core.OutboundEvent, error) {
	callback, ok := event.(*ownership.CallbackEvent)
	if !ok {
		return event, nil
	}
	if callback.RetainedProjection != nil {
		projected := *callback
		projected.Attributes = slices.Clone(callback.RetainedProjection.Attributes)
		projected.Tag = slices.Clone(callback.Tag)
		return &projected, nil
	}
	if o.capture != nil && callback.Kind == ownership.CallbackAssumption {
		projection, err := o.capture(ctx, fed, recipient, callback.Object, callback.Attributes)
		if err != nil {
			return nil, err
		}
		projected := *callback
		projected.Attributes = projection.Attributes()
		projected.Tag = slices.Clone(callback.Tag)
		return delivery.GuardCancellableEvent(&projected, projection, object.ErrStaleObjectKnowledge), nil
	}
	if o.project == nil {
		return nil, errors.New("ownership callback projection unavailable")
	}
	attrs, err := o.project(ctx, fed, recipient, callback.Object, callback.Attributes)
	if err != nil {
		return nil, err
	}
	projected := *callback
	projected.Attributes = slices.Clone(attrs)
	projected.Tag = slices.Clone(callback.Tag)
	return &projected, nil
}
