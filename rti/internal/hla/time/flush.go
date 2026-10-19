package time

import (
	"context"
	"errors"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// FQR makes every queued user message eligible. Publish the filtered messages
// first so the grant minimum depends on actual delivery, not discarded entries.
// The federation evaluator lease remains held through the following grant.
func (m *ManagerOf[T]) flushPendingMessages(ctx context.Context, fed core.FederationName) error {
	ext := extOfOf[T](m)
	ext.mu.Lock()
	if barrier := ext.scheduledSaves[fed]; barrier != nil && !barrier.pausedAt.IsZero() {
		ext.mu.Unlock()
		return nil
	}
	var handles []core.FederateHandle
	for key, ns := range ext.states {
		if key.fed == fed && ns.pendingNER && ns.mode == ModeFQR && !scheduledRecipientPausedLocked(ext, fed, key.h) {
			handles = append(handles, key.h)
		}
	}
	ext.mu.Unlock()
	slices.Sort(handles)
	for _, h := range handles {
		if err := m.flushFederateMessages(ctx, fed, h); err != nil {
			return err
		}
	}
	return nil
}

// A transport-capacity failure can occur after the earliest TSO was published.
// Keep its time until the request completes, including across save/restore.
func (m *ManagerOf[T]) recordFlushDelivery(fed core.FederationName, h core.FederateHandle, b bufferedTSOEventOf[T]) {
	if b.receiveOrder || internalTSO(b.event) {
		return
	}
	m.recordOrderedDelivery(fed, h, b)
	ext := extOfOf[T](m)
	ext.mu.Lock()
	defer ext.mu.Unlock()
	ns := ext.getLocked(fed, h)
	if ns == nil || !ns.pendingNER || (ns.mode != ModeFQR && !ns.mode.usesNextMessageTarget()) {
		return
	}
	if ns.flushMinimum == nil || b.timestamp < *ns.flushMinimum {
		value := b.timestamp
		ns.flushMinimum = &value
	}
}

func (m *ManagerOf[T]) flushFederateMessages(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	ext := extOfOf[T](m)
	ext.mu.Lock()
	current := ext.getLocked(fed, h).currentTime
	ext.mu.Unlock()
	release := append(m.takeBufferedRO(fed, h), m.takeBufferedTSOThrough(fed, h, nil, true, &current)...)
	if len(release) == 0 {
		return nil
	}
	_, reservable := m.opts.Outbox.(core.ReservableOutbox)
	if !reservable {
		if err := m.deliverBufferedSequence(ctx, fed, h, release); err != nil {
			return err
		}
		return nil
	}
	delivered, deliveries := m.filteredFlushDeliveries(ctx, fed, h, release)
	var reservation core.OutboxReservation
	if len(deliveries) != 0 || len(delivered) != 0 {
		var err error
		reservation, err = m.reserveBufferedDeliveries(ctx, fed, h, delivered, deliveries)
		if errors.Is(err, core.ErrFederateOverflow) {
			if reservation != nil {
				reservation.Release()
			}
			if err := m.deliverBufferedSequence(ctx, fed, h, release); err != nil {
				return err
			}
			return nil
		}
		if err != nil {
			if reservation != nil {
				reservation.Release()
			}
			m.restoreBufferedMessages(fed, h, release)
			return err
		}
	}
	return m.commitFlushDeliveries(ctx, fed, h, release, delivered, reservation)
}

func (m *ManagerOf[T]) filteredFlushDeliveries(ctx context.Context, fed core.FederationName, h core.FederateHandle, release []bufferedTSOEventOf[T]) ([]bufferedTSOEventOf[T], []core.OutboxDelivery) {
	var delivered []bufferedTSOEventOf[T]
	var deliveries []core.OutboxDelivery
	for _, b := range release {
		event := m.queuedMessage(ctx, fed, h, b)
		if event == nil {
			continue
		}
		b.event = event
		delivered = append(delivered, b)
		if !internalTSO(event) {
			deliveries = append(deliveries, core.OutboxDelivery{Recipient: h, Event: event})
		}
	}
	return delivered, deliveries
}

func (m *ManagerOf[T]) commitFlushDeliveries(ctx context.Context, fed core.FederationName, h core.FederateHandle, release, delivered []bufferedTSOEventOf[T], reservation core.OutboxReservation) error {
	lifecycle := m.prepareDeletionDelivery(fed, h, delivered)
	if reservation != nil {
		if err := reservation.Commit(); err != nil {
			reservation.Release()
			if lifecycle != nil {
				lifecycle.Release()
			}
			m.restoreBufferedMessages(fed, h, release)
			return err
		}
		reservation.Release()
	}
	m.recordBufferedRetractionDeliveries(ctx, fed, h, delivered)
	for _, b := range delivered {
		m.recordFlushDelivery(fed, h, b)
	}
	if lifecycle != nil {
		if err := lifecycle.Commit(); err != nil {
			lifecycle.Release()
			return err
		}
		lifecycle.Release()
	}
	return nil
}
