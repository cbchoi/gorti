package grpc

import (
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

func TestMOMCallbacksCarryAuthoritativeReceiveOrderMetadata(t *testing.T) {
	for _, event := range []core.OutboundEvent{&mom.ReflectEvent{}, &mom.InteractionEvent{}} {
		wire, err := EncodeOutboundEvent(event)
		if err != nil {
			t.Fatal(err)
		}
		metadata := wire.GetReflect().GetMetadata()
		if wire.GetReceive() != nil {
			metadata = wire.GetReceive().GetMetadata()
		}
		if metadata == nil || metadata.SentOrder == nil || metadata.ReceivedOrder == nil || metadata.TransportationType == nil {
			t.Fatalf("MOM callback missing metadata: %v", wire)
		}
		if metadata.GetSentOrder() != rtiv1.OrderType_ORDER_TYPE_RECEIVE || metadata.GetReceivedOrder() != rtiv1.OrderType_ORDER_TYPE_RECEIVE || metadata.GetTransportationType() != rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE {
			t.Fatalf("incorrect RTI-originated MOM callback semantics: %v", metadata)
		}
		if metadata.ProducingFederate != nil || metadata.MessageRetractionHandle != nil {
			t.Fatalf("fabricated federate identity/retraction for MOM: %v", metadata)
		}
	}
}

func TestRestoreRuntimeStateWireEncodingIsDetached(t *testing.T) {
	state := []byte("private-runtime")
	wire, err := EncodeOutboundEvent(&savepoint.InitiateRestoreEvent{FederateHandle: 7, FederateName: "one", ClientState: state})
	if err != nil {
		t.Fatal(err)
	}
	state[0] = 'X'
	if string(wire.GetRestoreInitiate().GetClientState()) != "private-runtime" || wire.GetRestoreInitiate().GetFederateHandle() != 7 {
		t.Fatalf("lost/aliased restore runtime state: %v", wire)
	}
}
