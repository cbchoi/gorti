package federate

import (
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"testing"
)

func TestMetadataPresenceAndRateOverloads(t *testing.T) {
	if metadataFromWire(nil) != nil {
		t.Fatal("legacy metadata invented")
	}
	order := rtiv1.OrderType_ORDER_TYPE_RECEIVE
	producer := uint64(7)
	wire := &rtiv1.ObjectCallbackMetadata{SentOrder: &order, ProducingFederate: &producer, ReceivedRegions: &rtiv1.ReceivedRegions{}}
	got := metadataFromWire(wire)
	if got.SentOrder == nil || *got.SentOrder != OrderReceive || got.ReceivedOrder != nil || got.MessageRetractionHandle != nil || got.ReceivedRegions == nil {
		t.Fatalf("presence lost: %+v", got)
	}
	producer = 99
	if *got.ProducingFederate != 7 {
		t.Fatal("metadata aliases wire")
	}
	f := &Federate{}
	for _, rate := range []*string{nil, new(string)} {
		event := f.translate(&rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_TurnUpdatesOn{TurnUpdatesOn: &rtiv1.TurnUpdatesOnForObjectInstance{ObjectHandle: 4, AttributeHandles: []uint64{2}, UpdateRateDesignator: rate}}}).(TurnUpdatesOnForObjectInstance)
		if (event.UpdateRateDesignator == nil) != (rate == nil) {
			t.Fatal("rate overload presence lost")
		}
	}
}
