package savepoint

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// reserveRestoreTerminal reserves the complete success fanout before any
// prepared state is committed. Strict construction requires ReservableOutbox.
func (m *ManagerOf[T]) reserveRestoreTerminal(ctx context.Context, fed core.FederationName, label string, recipients []core.FederateHandle, retractionHints map[core.FederateHandle]uint64) (core.OutboxReservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	deliveries := make([]core.OutboxDelivery, 0, len(recipients))
	for _, recipient := range recipients {
		event := federationRestoredEvent(label)
		event.NextRetractionHandle = retractionHints[recipient]
		deliveries = append(deliveries, core.OutboxDelivery{Recipient: recipient, Event: event})
	}
	reservation, err := m.opts.Outbox.(core.ReservableOutbox).Reserve(ctx, fed, deliveries)
	if err != nil {
		if reservation != nil {
			reservation.Release()
		}
		return nil, fmt.Errorf("savepoint: reserve restore completion callbacks: %w", err)
	}
	if reservation == nil {
		return nil, fmt.Errorf("savepoint: restore completion reservation is nil")
	}
	return reservation, nil
}
