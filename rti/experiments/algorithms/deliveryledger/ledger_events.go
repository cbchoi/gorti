package deliveryledger

import (
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

// This is a bounded engineering classification, not a normative decision about
// every HLA callback. Only the current object-management user data callbacks are
// included. Control messages, MOM service callbacks and deletion markers do not
// become user data deliveries merely because they pass through the outbox.
func deliveryEventOrder(event core.OutboundEvent, stage DeliveryStage) (core.OrderType, bool, error) {
	e, ok := event.(*object.CallbackEvent)
	if !ok || e == nil {
		return core.OrderUnspecified, false, nil
	}
	switch e.Kind {
	case object.CallbackReflect, object.CallbackReceive, object.CallbackRemove:
	default:
		return core.OrderUnspecified, false, nil
	}
	if stage == DeliveryTimeBuffer {
		if e.LogicalTime == nil {
			return core.OrderUnspecified, true, ErrDeliveryOrder
		}
		return core.OrderTimestamp, true, nil
	}
	if e.Metadata != nil {
		switch e.Metadata.ReceivedOrder {
		case core.OrderReceive:
			return core.OrderReceive, true, nil
		case core.OrderTimestamp:
			if e.LogicalTime != nil {
				return core.OrderTimestamp, true, nil
			}
		default:
			return core.OrderUnspecified, true, ErrDeliveryOrder
		}
		return core.OrderUnspecified, true, ErrDeliveryOrder
	}
	if e.LogicalTime == nil {
		return core.OrderReceive, true, nil
	}
	// Sent order or presence of a timestamp cannot prove received order.
	return core.OrderUnspecified, true, ErrDeliveryOrder
}
