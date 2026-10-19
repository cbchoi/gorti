package time

import (
	"context"
	"fmt"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type deferredDeletionPreparer interface {
	PrepareDeferredDeletionDelivery(context.Context, core.FederationName, core.FederateHandle, []core.OutboundEvent) (core.OutboxReservation, []core.OutboxDelivery, error)
}

// One reservation admits mandatory ownership outcomes before messages/grant.
// The object/ownership preparation precedes the outbox lock, never nests another
// queue reservation beneath an existing reservation for the same recipient.
func (m *ManagerOf[T]) reserveBufferedDeliveries(ctx context.Context, fed core.FederationName, h core.FederateHandle, buffered []bufferedTSOEventOf[T], deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	var lifecycle core.OutboxReservation
	var outcomeCount int
	if prepare, ok := m.deletions.(deferredDeletionPreparer); ok {
		events := make([]core.OutboundEvent, 0, len(buffered))
		for _, b := range buffered {
			events = append(events, b.event)
		}
		var outcomes []core.OutboxDelivery
		var err error
		lifecycle, outcomes, err = prepare.PrepareDeferredDeletionDelivery(ctx, fed, h, events)
		if err != nil {
			return nil, err
		}
		deliveries = append(outcomes, deliveries...)
		outcomeCount = len(outcomes)
	}
	var queue core.OutboxReservation
	if len(deliveries) > 0 {
		out, ok := m.opts.Outbox.(core.ReservableOutbox)
		if !ok {
			if outcomeCount != 0 {
				if lifecycle != nil {
					lifecycle.Release()
				}
				return nil, fmt.Errorf("atomic deletion outcomes require a reservable outbox")
			}
			queue = &legacyDeletionDelivery{ctx: ctx, fed: fed, out: m.opts.Outbox, deliveries: deliveries}
		} else {
			var err error
			queue, err = out.Reserve(ctx, fed, deliveries)
			if err != nil || queue == nil {
				if queue != nil {
					queue.Release()
				}
				if lifecycle != nil {
					lifecycle.Release()
				}
				if err == nil {
					err = fmt.Errorf("time delivery: nil outbox reservation")
				}
				return nil, err
			}
		}
	}
	if lifecycle == nil {
		return queue, nil
	}
	return &deletionFanoutReservation{lifecycle: lifecycle, queue: queue}, nil
}

type legacyDeletionDelivery struct {
	ctx        context.Context
	fed        core.FederationName
	out        core.Outbox
	deliveries []core.OutboxDelivery
}

func (r *legacyDeletionDelivery) Commit() error {
	for _, delivery := range r.deliveries {
		if err := r.out.Send(r.ctx, r.fed, delivery.Recipient, delivery.Event); err != nil {
			return err
		}
	}
	return nil
}
func (*legacyDeletionDelivery) Release() {}

type deletionFanoutReservation struct {
	lifecycle core.OutboxReservation
	queue     core.OutboxReservation
	once      sync.Once
	err       error
}

func (r *deletionFanoutReservation) Commit() error {
	r.once.Do(func() {
		if owners, ok := r.lifecycle.(interface{ CommitOwnership() error }); ok {
			r.err = owners.CommitOwnership()
			if r.err != nil {
				return
			}
		}
		if r.queue != nil {
			r.err = r.queue.Commit()
		}
		if admission, ok := r.lifecycle.(interface{ SetDeletionAdmissionError(error) }); ok {
			admission.SetDeletionAdmissionError(r.err)
		}
		if r.err == nil {
			r.err = r.lifecycle.Commit()
		}
	})
	return r.err
}

func (r *deletionFanoutReservation) Release() {
	r.once.Do(func() {})
	if r.queue != nil {
		r.queue.Release()
	}
	r.lifecycle.Release()
}
