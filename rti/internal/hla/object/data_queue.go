package object

import "github.com/cbchoi/gorti/rti/internal/hla/core"

func (e *CallbackEventOf[T]) DataReceivedOrder() (core.OrderType, bool) {
	if e == nil || e.Kind != CallbackReflect && e.Kind != CallbackReceive && e.Kind != CallbackRemove {
		return core.OrderUnspecified, false
	}
	if e.Metadata == nil {
		return core.OrderUnspecified, true
	}
	return e.Metadata.ReceivedOrder, true
}

var _ core.DataQueueMessage = (*CallbackEvent)(nil)
var _ core.DataQueueMessage = (*IntegerCallbackEvent)(nil)
