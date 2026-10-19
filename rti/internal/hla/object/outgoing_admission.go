package object

import "github.com/cbchoi/gorti/rti/internal/hla/core"

func messageDeliveryOf[T core.LogicalTimeScalar](recipient, sender core.FederateHandle, timestamp *T, event core.OutboundEvent, retraction uint64) core.MessageDeliveryOf[T] {
	delivery := core.MessageDeliveryOf[T]{Recipient: recipient, Sender: sender, Event: event, ReceiveOrder: timestamp == nil}
	if timestamp != nil {
		delivery.Timestamp = *timestamp
		delivery.RetractionHandle = retraction
	}
	return delivery
}

func (r *RegistryOf[T]) reserveOutgoingTSO(fed core.FederationName, sender core.FederateHandle, timestamp *T, deliveries []core.MessageDeliveryOf[T]) (core.TSOBufferReservationOf[T], error) {
	if gate, ok := r.opts.TSOGate.(core.AtomicOutgoingTSODeliveryGateOf[T]); ok && timestamp != nil {
		return gate.ReserveOutgoingTSO(fed, sender, *timestamp, deliveries)
	}
	return r.opts.TSOGate.(core.ReservableTSODeliveryGateOf[T]).ReserveTSO(fed, deliveries), nil
}
