package time

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// SetDeletionLifecycle is a composition-time binding, before services start.
func (m *ManagerOf[T]) SetDeletionLifecycle(lifecycle core.DeletionLifecycle) {
	m.deletions = lifecycle
}

func internalTSO(event core.OutboundEvent) bool {
	internal, ok := event.(core.InternalTSOEvent)
	return ok && internal.InternalTSO()
}

func (m *ManagerOf[T]) prepareDeletionDelivery(fed core.FederationName, h core.FederateHandle, buffered []bufferedTSOEventOf[T]) core.OutboxReservation {
	if m.deletions == nil {
		return nil
	}
	if _, ok := m.deletions.(deferredDeletionPreparer); ok {
		return nil
	}
	events := make([]core.OutboundEvent, 0, len(buffered))
	for _, b := range buffered {
		events = append(events, b.event)
	}
	if recipient, ok := m.deletions.(interface {
		PrepareRecipientDeletionDelivery(core.FederationName, core.FederateHandle, []core.OutboundEvent) core.OutboxReservation
	}); ok {
		return recipient.PrepareRecipientDeletionDelivery(fed, h, events)
	}
	return m.deletions.PrepareDeletionDelivery(fed, events)
}

func (m *ManagerOf[T]) finishDeletionDelivery(fed core.FederationName) {
	if m.deletions != nil {
		m.deletions.FinishDeletionDelivery(fed)
	}
}

// Reserve callbacks before pinning object state, matching object admission's
// outbox -> object lock order. A legacy outbox has no rollback contract: commit
// lifecycle immediately after each successful Send, even if a later grant fails.
func (m *ManagerOf[T]) deliverBufferedEvent(ctx context.Context, fed core.FederationName, h core.FederateHandle, b bufferedTSOEventOf[T]) error {
	event := m.queuedMessage(ctx, fed, h, b)
	if event == nil {
		return nil
	}
	if _, ok := m.deletions.(deferredDeletionPreparer); ok {
		return m.deliverDeferredDeletionEvent(ctx, fed, h, b, event)
	}
	var reservation core.OutboxReservation
	if !internalTSO(b.event) {
		if out, ok := m.opts.Outbox.(core.ReservableOutbox); ok {
			var err error
			reservation, err = out.Reserve(ctx, fed, []core.OutboxDelivery{{Recipient: h, Event: event}})
			if err != nil {
				if reservation != nil {
					reservation.Release()
				}
				return err
			}
			if reservation == nil {
				return fmt.Errorf("time delivery: nil outbox reservation")
			}
			if reservation != nil {
				defer reservation.Release()
			}
		} else if err := m.opts.Outbox.Send(ctx, fed, h, event); err != nil {
			return err
		}
	}
	lifecycle := m.prepareDeletionDelivery(fed, h, []bufferedTSOEventOf[T]{b})
	if lifecycle != nil {
		defer lifecycle.Release()
	}
	if reservation != nil {
		if err := reservation.Commit(); err != nil {
			return err
		}
	}
	m.RecordRetractionDeliveries(fed, []core.MessageDeliveryOf[T]{{Recipient: h, Sender: b.sender, RetractionHandle: b.retractionHandle, Timestamp: b.timestamp, Event: event, ReceiveOrder: b.receiveOrder}})
	m.recordFlushDelivery(fed, h, b)
	if lifecycle != nil {
		return lifecycle.Commit()
	}
	return nil
}

// DiscardObjectDeletions removes obsolete timed removals after an immediate
// resign deletion. The caller must release object/ownership locks first.
func (m *ManagerOf[T]) DiscardObjectDeletions(fed core.FederationName, objects []core.ObjectHandle) {
	if len(objects) == 0 {
		return
	}
	set := make(map[core.ObjectHandle]bool, len(objects))
	for _, obj := range objects {
		set[obj] = true
	}
	ext := extOfOf[T](m)
	eval := ext.evaluatorLock(fed)
	eval.Lock()
	defer eval.Unlock()
	ext.mu.Lock()
	defer ext.mu.Unlock()
	for key, ns := range ext.states {
		if key.fed != fed {
			continue
		}
		keep := ns.tsoBuffer[:0]
		for _, b := range ns.tsoBuffer {
			if e, ok := b.event.(core.ObjectDeletionEvent); ok && set[e.DeletionObject()] {
				continue
			}
			keep = append(keep, b)
		}
		ns.tsoBuffer = append([]bufferedTSOEventOf[T](nil), keep...)
	}
}

func (m *ManagerOf[T]) deliverDeferredDeletionEvent(ctx context.Context, fed core.FederationName, h core.FederateHandle, b bufferedTSOEventOf[T], event core.OutboundEvent) error {
	var deliveries []core.OutboxDelivery
	if !internalTSO(b.event) {
		deliveries = append(deliveries, core.OutboxDelivery{Recipient: h, Event: event})
	}
	b.event = event
	prepared, err := m.reserveBufferedDeliveries(ctx, fed, h, []bufferedTSOEventOf[T]{b}, deliveries)
	if err != nil {
		return err
	}
	if prepared != nil {
		defer prepared.Release()
		if err := prepared.Commit(); err != nil {
			return err
		}
	}
	m.RecordRetractionDeliveries(fed, []core.MessageDeliveryOf[T]{{Recipient: h, Sender: b.sender, RetractionHandle: b.retractionHandle, Timestamp: b.timestamp, Event: event, ReceiveOrder: b.receiveOrder}})
	m.recordFlushDelivery(fed, h, b)
	return nil
}
