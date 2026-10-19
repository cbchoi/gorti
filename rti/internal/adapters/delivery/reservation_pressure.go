package delivery

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

// ReservationPressure rejects an unpublished transfer which would fit into an
// empty queue. Subscribers own the raw receive channel, so callers retry after
// a bounded delay rather than relying on an unobservable channel-read signal.
type ReservationPressure struct{ Err error }

func (e *ReservationPressure) Error() string { return e.Err.Error() }
func (e *ReservationPressure) Unwrap() error { return e.Err }

func (m *MultiOutbox) reservationPressure(fed core.FederationName, recipient core.FederateHandle, deliveries []core.OutboxDelivery) error {
	err := fmt.Errorf("%w: federation %q federate %d", core.ErrFederateOverflow, fed, recipient)
	// Check the entire fanout against empty queues; an oversized atomic transfer
	// cannot be fixed by draining and must remain a terminal reservation error.
	type occupancy struct{ queued, scratch int }
	counts := make(map[core.FederateHandle]occupancy)
	for _, d := range deliveries {
		count := counts[d.Recipient]
		count.scratch++
		_, grant := d.Event.(*timepkg.TimeAdvanceGrant)
		if count.scratch >= m.batchSize || grant {
			count.queued++
			count.scratch = 0
		}
		if count.queued > m.bufferSize {
			return err
		}
		counts[d.Recipient] = count
	}
	return &ReservationPressure{Err: err}
}
