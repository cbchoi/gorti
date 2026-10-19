package rtiv1

import (
	"math"
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestOwnershipCallbackMetadataRoundTrip(t *testing.T) {
	original := &FederateEvent{
		Seq:                  7,
		CallbackReceipt:      []byte("independent-MOM-receipt"),
		OwnershipReceipt:     &OwnershipCallbackReceipt{FederationGeneration: 2, CallbackEpoch: 0, CallbackId: math.MaxUint64, DeliveryAttempt: 3},
		ObjectKnowledgeEpoch: math.MaxUint64,
		Event:                &FederateEvent_OwnershipAcquisitionCancelled{OwnershipAcquisitionCancelled: &ConfirmAttributeOwnershipAcquisitionCancellation{ObjectHandle: 9, AttributeHandles: []uint64{1, 2}}},
	}
	wire, err := proto.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	decoded := &FederateEvent{}
	if err := proto.Unmarshal(wire, decoded); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(original, decoded) {
		t.Fatal("callback metadata/payload changed")
	}
	report := &ReportOwnershipCallbackInvocationRequest{WireVersion: WireVersion_WIRE_VERSION_V1, FederationName: "fed", FederateHandle: 1, Receipt: decoded.OwnershipReceipt}
	wire, err = proto.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	decodedReport := &ReportOwnershipCallbackInvocationRequest{}
	if err := proto.Unmarshal(wire, decodedReport); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(report, decodedReport) || decodedReport.Receipt == nil || decodedReport.Receipt.CallbackEpoch != 0 {
		t.Fatal("initial epoch receipt lost")
	}
}

func TestOwnershipCallbackNegotiationAndFenceRoundTrip(t *testing.T) {
	for _, original := range []proto.Message{
		&OwnershipCallbackScopeRequest{WireVersion: WireVersion_WIRE_VERSION_V1, FederationName: "fed", FederateHandle: 1, FederationGeneration: 2, CallbackEpoch: 0},
		&LocalDeleteObjectInstanceFence{RetiredKnowledgeEpoch: math.MaxUint64, FederationGeneration: 2, CallbackEpoch: 0},
	} {
		wire, err := proto.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		decoded := original.ProtoReflect().New().Interface()
		if err := proto.Unmarshal(wire, decoded); err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(original, decoded) {
			t.Fatal("negotiation/fence fields changed")
		}
	}
}
