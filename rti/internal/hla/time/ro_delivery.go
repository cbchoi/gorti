package time

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// SetQueuedMessageFilter binds the subscription port before services start.
func (m *ManagerOf[T]) SetQueuedMessageFilter(filter core.QueuedMessageFilter) {
	m.messageFilter = filter
}

func (m *ManagerOf[T]) queuedMessage(ctx context.Context, fed core.FederationName, h core.FederateHandle, b bufferedTSOEventOf[T]) core.OutboundEvent {
	if m.messageFilter != nil && !internalTSO(b.event) {
		return m.messageFilter.FilterQueuedMessage(ctx, fed, h, b.event)
	}
	return b.event
}

func (m *ManagerOf[T]) markDeliveryRetry(fed core.FederationName) {
	ext := extOfOf[T](m)
	ext.mu.Lock()
	if ext.deliveryRetries == nil {
		ext.deliveryRetries = make(map[core.FederationName]bool)
	}
	ext.deliveryRetries[fed] = true
	ext.mu.Unlock()
}

// PendingDeliveryRetries returns only federations with failed delivery work.
// The application owns retry scheduling and checkpoint/generation admission.
func (m *ManagerOf[T]) PendingDeliveryRetries() []core.FederationName {
	ext := extOfOf[T](m)
	ext.mu.Lock()
	defer ext.mu.Unlock()
	feds := make([]core.FederationName, 0, len(ext.deliveryRetries))
	for fed := range ext.deliveryRetries {
		feds = append(feds, fed)
	}
	slices.Sort(feds)
	return feds
}

// RetryPendingDeliveries resumes accepted messages and requests without a new
// federate advance call. Successful prefixes are never sent a second time.
func (m *ManagerOf[T]) RetryPendingDeliveries(ctx context.Context, fed core.FederationName) error {
	ext := extOfOf[T](m)
	ext.mu.Lock()
	delete(ext.deliveryRetries, fed)
	ext.mu.Unlock()
	err := m.ResumeScheduledSave(ctx, fed)
	if err != nil && !errors.Is(err, core.ErrFederationHalted) {
		m.markDeliveryRetry(fed)
	}
	return err
}

func (m *ManagerOf[T]) deliverBufferedSequence(ctx context.Context, fed core.FederationName, h core.FederateHandle, release []bufferedTSOEventOf[T]) error {
	for i, b := range release {
		if err := ctx.Err(); err != nil {
			m.restoreBufferedMessages(fed, h, release[i:])
			return err
		}
		if err := m.deliverBufferedEvent(ctx, fed, h, b); err != nil {
			m.restoreBufferedMessages(fed, h, release[i:])
			return err
		}
	}
	return nil
}

// The caller holds the federation evaluator lease. RO is independent of GALT,
// lookahead, and the message's optional original timestamp.
func (m *ManagerOf[T]) takeBufferedRO(fed core.FederationName, h core.FederateHandle) []bufferedTSOEventOf[T] {
	m.states.mu.Lock()
	defer m.states.mu.Unlock()
	ext := extOfOf[T](m)
	ext.mu.Lock()
	defer ext.mu.Unlock()
	ns := ext.getLocked(fed, h)
	if ns == nil || scheduledRecipientPausedLocked(ext, fed, h) {
		return nil
	}
	fs := m.states.getLocked(fed, h)
	if fs != nil && fs.constrained && !ns.pendingNER && !ns.asyncDelivery {
		return nil
	}
	release := ns.roBuffer
	ns.roBuffer = nil
	return release
}

func (m *ManagerOf[T]) restoreBufferedMessages(fed core.FederationName, h core.FederateHandle, events []bufferedTSOEventOf[T]) {
	var ro, tso []bufferedTSOEventOf[T]
	for _, event := range events {
		if event.receiveOrder {
			ro = append(ro, event)
		} else {
			tso = append(tso, event)
		}
	}
	if len(ro) != 0 {
		ext := extOfOf[T](m)
		ext.mu.Lock()
		ns := ext.getOrCreateLocked(fed, h)
		ns.roBuffer = append(ro, ns.roBuffer...)
		ext.mu.Unlock()
	}
	m.restoreBufferedTSO(fed, h, tso)
}

func (m *ManagerOf[T]) drainBufferedRO(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	release := m.takeBufferedRO(fed, h)
	if len(release) == 0 {
		return nil
	}
	if out, ok := m.opts.Outbox.(core.ReservableOutbox); ok {
		return m.drainReservedRO(ctx, fed, h, out, release)
	}
	for i, b := range release {
		event := m.queuedMessage(ctx, fed, h, b)
		if event == nil {
			continue
		}
		if err := m.opts.Outbox.Send(ctx, fed, h, event); err != nil {
			m.restoreBufferedMessages(fed, h, release[i:])
			return err
		}
	}
	return nil
}

func (m *ManagerOf[T]) drainReservedRO(ctx context.Context, fed core.FederationName, h core.FederateHandle, out core.ReservableOutbox, release []bufferedTSOEventOf[T]) error {
	deliveries := make([]core.OutboxDelivery, 0, len(release))
	for _, b := range release {
		if event := m.queuedMessage(ctx, fed, h, b); event != nil {
			deliveries = append(deliveries, core.OutboxDelivery{Recipient: h, Event: event})
		}
	}
	if len(deliveries) == 0 {
		return nil
	}
	reservation, err := out.Reserve(ctx, fed, deliveries)
	if errors.Is(err, core.ErrFederateOverflow) {
		if reservation != nil {
			reservation.Release()
		}
		return m.deliverBufferedSequence(ctx, fed, h, release)
	}
	if reservation != nil {
		defer reservation.Release()
	}
	if err == nil && reservation == nil {
		err = fmt.Errorf("RO drain: nil outbox reservation")
	}
	if err == nil {
		err = reservation.Commit()
	}
	if err != nil {
		m.restoreBufferedMessages(fed, h, release)
	}
	return err
}
