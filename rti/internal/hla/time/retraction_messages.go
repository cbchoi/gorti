package time

import (
	"context"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func cloneRetractionMessageOf[T core.LogicalTimeScalar](message RetractionMessageStateOf[T]) RetractionMessageStateOf[T] {
	message.Delivered = slices.Clone(message.Delivered)
	return message
}

func compareRetractionIDs(a, b uint64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// ReserveRetractionMessage records the original send timestamp independently
// of recipient count. The caller already holds originating service admission.
func (m *ManagerOf[T]) ReserveRetractionMessage(ctx context.Context, fed core.FederationName, sender core.FederateHandle, id uint64, timestamp T) (core.OutboxReservation, error) {
	if !validPersistentTimeOf[T](timestamp) {
		return nil, core.ErrTimeInvalidLogicalTime
	}
	reservation, err := m.ReserveRetractionID(ctx, fed, sender, id)
	if err != nil {
		return nil, err
	}
	r := reservation.(*retractionIDReservationOf[T])
	m.retractions.mu.Lock()
	r.message = &RetractionMessageStateOf[T]{ID: id, Timestamp: timestamp}
	m.retractions.mu.Unlock()
	return r, nil
}

// RecordRetractionDeliveries follows successful outbox acceptance while the
// evaluator lease is held. It takes no time-state locks and is safe under ext.mu.
// Pending originating reservations are included because delivery commits before
// the corresponding ID reservation on the object-management path.
func (m *ManagerOf[T]) RecordRetractionDeliveries(fed core.FederationName, deliveries []core.MessageDeliveryOf[T]) {
	if len(deliveries) == 0 {
		return
	}
	m.retractions.mu.Lock()
	defer m.retractions.mu.Unlock()
	f := m.retractions.federations[fed]
	if f == nil {
		return
	}
	for _, d := range deliveries {
		if d.ReceiveOrder || d.RetractionHandle == 0 || d.Recipient == 0 || internalTSO(d.Event) {
			continue
		}
		p := f.producers[d.Sender]
		if p == nil {
			continue
		}
		message := p.messages[d.RetractionHandle]
		if message == nil && p.pending[d.RetractionHandle] != nil {
			message = p.pending[d.RetractionHandle].message
		}
		if message == nil || message.Retracted || slices.Contains(message.Delivered, d.Recipient) {
			continue
		}
		message.Delivered = append(message.Delivered, d.Recipient)
		slices.Sort(message.Delivered)
	}
}

// ForgetRetractionRecipient drops callbacks for a federate that has resigned.
// It does not release the originating IDs or change surviving recipients.
func (m *ManagerOf[T]) ForgetRetractionRecipient(fed core.FederationName, recipient core.FederateHandle) {
	m.retractions.mu.Lock()
	defer m.retractions.mu.Unlock()
	if f := m.retractions.federations[fed]; f != nil {
		for _, p := range f.producers {
			for _, message := range p.messages {
				message.Delivered = slices.DeleteFunc(message.Delivered, func(h core.FederateHandle) bool { return h == recipient })
			}
		}
	}
}
