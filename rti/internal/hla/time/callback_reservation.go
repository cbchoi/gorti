package time

import (
	"context"
	"errors"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// Reserve data followed by the time callback. On capacity pressure, publish a
// bounded data prefix first, retaining the unsent suffix on failure.
func (m *ManagerOf[T]) reserveTimeCallback(ctx context.Context, fed core.FederationName, h core.FederateHandle, out core.ReservableOutbox, release []bufferedTSOEventOf[T], callback core.OutboundEvent, kind string) (core.OutboxReservation, []bufferedTSOEventOf[T], error) {
	var reservation core.OutboxReservation
	deliveries := make([]core.OutboxDelivery, 0, len(release)+1)
	for _, buffered := range release {
		if internalTSO(buffered.event) {
			continue
		}
		if event := m.queuedMessage(ctx, fed, h, buffered); event != nil {
			deliveries = append(deliveries, core.OutboxDelivery{Recipient: h, Event: event})
		}
	}
	deliveries = append(deliveries, core.OutboxDelivery{Recipient: h, Event: callback})
	var err error
	reservation, err = m.reserveBufferedDeliveries(ctx, fed, h, release, deliveries)
	if errors.Is(err, core.ErrFederateOverflow) {
		if reservation != nil {
			reservation.Release()
		}
		// A legal domain queue can exceed transport capacity. Transfer a
		// bounded prefix, retaining the unsent suffix and the pending callback.
		if err := m.deliverBufferedSequence(ctx, fed, h, release); err != nil {
			return nil, release, err
		}
		release = nil
		reservation, err = out.Reserve(ctx, fed, []core.OutboxDelivery{{Recipient: h, Event: callback}})
	}
	if err != nil || reservation == nil {
		if reservation != nil {
			reservation.Release()
		}
		m.restoreBufferedMessages(fed, h, release)
		if err == nil {
			err = fmt.Errorf("time %s: nil outbox reservation", kind)
		}
		return nil, release, err
	}
	return reservation, release, nil
}
