package server

import (
	"context"
	"fmt"
	"sync"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type conveyedRegionLifecycle interface {
	ConveyCopies(context.Context, core.FederationName, core.FederateHandle, []core.ConveyedRegionCopy) error
	EnterConveyedRegions(context.Context, core.FederationName, core.FederateHandle, []core.DDMRegionHandleCore) error
	ReleaseConveyedRegions(context.Context, core.FederationName, core.FederateHandle, []core.DDMRegionHandleCore) error
}

type conveyedCallbackOutbox struct {
	next    core.ReservableOutbox
	regions func() conveyedRegionLifecycle
}

func newConveyedCallbackOutbox(next core.ReservableOutbox, receipts func() *grpcadapter.CallbackReceipts, regions func() conveyedRegionLifecycle) *conveyedCallbackOutbox {
	return &conveyedCallbackOutbox{next: grpcadapter.NewSelectiveCallbackOutbox(next, receipts, func(event core.OutboundEvent) bool {
		regionCopy, ok := event.(core.ConveyedRegionEvent)
		return ok && len(regionCopy.ConveyedRegionCopies()) != 0 || grpcadapter.IsCheckpointCallback(event)
	}), regions: regions}
}

func (o *conveyedCallbackOutbox) Send(ctx context.Context, fed core.FederationName, h core.FederateHandle, event core.OutboundEvent) error {
	reservation, err := o.Reserve(ctx, fed, []core.OutboxDelivery{{Recipient: h, Event: event}})
	if err != nil {
		return err
	}
	defer reservation.Release()
	return reservation.Commit()
}

type conveyedLease struct {
	receiver core.FederateHandle
	handles  []core.DDMRegionHandleCore
}

func (o *conveyedCallbackOutbox) Reserve(ctx context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	prepared := &conveyedCallbackReservation{ctx: context.WithoutCancel(ctx), fed: fed}
	for _, delivery := range deliveries {
		event, ok := delivery.Event.(core.ConveyedRegionEvent)
		if !ok {
			continue
		}
		copies := event.ConveyedRegionCopies()
		if len(copies) == 0 {
			continue
		}
		if prepared.regions == nil {
			prepared.regions = o.regions()
			if prepared.regions == nil {
				prepared.Release()
				return nil, core.ErrRegionNotFound
			}
		}
		if err := prepared.regions.ConveyCopies(ctx, fed, delivery.Recipient, copies); err != nil {
			prepared.Release()
			return nil, err
		}
		lease := conveyedLease{receiver: delivery.Recipient, handles: make([]core.DDMRegionHandleCore, len(copies))}
		for i, regionCopy := range copies {
			lease.handles[i] = regionCopy.Designator
		}
		prepared.leases = append(prepared.leases, lease)
	}
	// Region preparation precedes raw queue locks to retain DDM -> delivery order.
	reservation, err := o.next.Reserve(ctx, fed, deliveries)
	if err != nil {
		prepared.Release()
		return nil, err
	}
	if reservation == nil {
		prepared.Release()
		return nil, fmt.Errorf("conveyed callback reservation unavailable")
	}
	prepared.next = reservation
	return prepared, nil
}

type conveyedCallbackReservation struct {
	mu      sync.Mutex
	done    bool
	err     error
	ctx     context.Context
	fed     core.FederationName
	regions conveyedRegionLifecycle
	leases  []conveyedLease
	next    core.OutboxReservation
}

func (r *conveyedCallbackReservation) Commit() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.done {
		r.err = r.next.Commit()
		if r.err != nil {
			r.releasePrepared()
		}
		r.done = true
	}
	return r.err
}

func (r *conveyedCallbackReservation) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return
	}
	r.releasePrepared()
	r.done = true
}

func (r *conveyedCallbackReservation) releasePrepared() {
	if r.next != nil {
		r.next.Release()
	}
	for _, lease := range r.leases {
		_ = r.regions.ReleaseConveyedRegions(r.ctx, r.fed, lease.receiver, lease.handles)
	}
}

func conveyedEventRegions(event *rtiv1.FederateEvent) []core.DDMRegionHandleCore {
	var metadata *rtiv1.ObjectCallbackMetadata
	switch {
	case event.GetReflect() != nil:
		metadata = event.GetReflect().GetMetadata()
	case event.GetReceive() != nil:
		metadata = event.GetReceive().GetMetadata()
	case event.GetRemove() != nil:
		metadata = event.GetRemove().GetMetadata()
	}
	values := metadata.GetReceivedRegions().GetRegionHandles()
	result := make([]core.DDMRegionHandleCore, len(values))
	for i, value := range values {
		result[i] = core.DDMRegionHandleCore(value)
	}
	return result
}

func conveyedCallbackEntryObserver(regions conveyedRegionLifecycle, next func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent) error) func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent) error {
	return func(ctx context.Context, fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent) error {
		if err := next(ctx, fed, h, event); err != nil {
			return err
		}
		if handles := conveyedEventRegions(event); len(handles) != 0 {
			return regions.EnterConveyedRegions(ctx, fed, h, handles)
		}
		return nil
	}
}

func conveyedCallbackOutcomeObserver(regions conveyedRegionLifecycle, next func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error) func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
	return func(ctx context.Context, fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent, success bool, exception string) error {
		if err := next(ctx, fed, h, event, success, exception); err != nil {
			return err
		}
		if handles := conveyedEventRegions(event); len(handles) != 0 {
			return regions.ReleaseConveyedRegions(ctx, fed, h, handles)
		}
		return nil
	}
}
