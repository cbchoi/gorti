package time

import (
	"context"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func (m *ManagerOf[T]) recordBufferedRetractionDeliveries(_ context.Context, fed core.FederationName, h core.FederateHandle, release []bufferedTSOEventOf[T]) {
	var deliveries []core.MessageDeliveryOf[T]
	for _, b := range release {
		if b.receiveOrder || b.retractionHandle == 0 || internalTSO(b.event) {
			continue
		}
		deliveries = append(deliveries, core.MessageDeliveryOf[T]{
			Recipient: h, Timestamp: b.timestamp, Event: b.event,
			Sender: b.sender, RetractionHandle: b.retractionHandle,
		})
	}
	m.RecordRetractionDeliveries(fed, deliveries)
}
