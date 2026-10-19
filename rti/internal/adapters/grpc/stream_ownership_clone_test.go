package grpc

import (
	"bytes"
	"context"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/protobuf/proto"
)

func TestOwnershipReceiptBatchPreservesInputAndEnvelope(t *testing.T) {
	receipts, _, _, _, _ := receiptAdapterForDomain(t)
	input := &rtiv1.FederateEventBatch{Ready: true, Events: []*rtiv1.FederateEvent{{
		Event: &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{
			InteractionClassHandle: 1, Parameters: map[uint64][]byte{2: {3, 4}},
		}},
	}}}
	input.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
	before := proto.Clone(input).(*rtiv1.FederateEventBatch)
	sink := &fakeBatchStream{ctx: context.Background()}
	stream := &ownershipReceiptBatchesStream{StreamService_EventBatchesServer: sink, receipts: receipts, fed: "fed", handle: 2}
	if err := stream.Send(input); err != nil {
		t.Fatal(err)
	}
	if len(sink.sent) != 1 || !proto.Equal(sink.sent[0], before) || !proto.Equal(input, before) {
		t.Fatal("batch content, ready marker, unknown fields or input changed")
	}
	// The next receipt adapter may edit the transmitted copy, never the outbox.
	sent := sink.sent[0]
	sent.Events[0].CallbackReceipt = []byte("new receipt")
	sent.Events[0].GetReceive().Parameters[2][0]++
	sent.ProtoReflect().GetUnknown()[2]++
	if !proto.Equal(input, before) || bytes.Equal(sent.ProtoReflect().GetUnknown(), input.ProtoReflect().GetUnknown()) {
		t.Fatal("transmitted batch aliases retained input")
	}
}

func BenchmarkOwnershipReceiptDataBatch(b *testing.B) {
	input := &rtiv1.FederateEventBatch{}
	for range 64 {
		input.Events = append(input.Events, &rtiv1.FederateEvent{
			Event: &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{
				InteractionClassHandle: 1, Parameters: map[uint64][]byte{2: make([]byte, 64)},
			}},
		})
	}
	sink := &discardBatchStream{ctx: context.Background()}
	stream := &ownershipReceiptBatchesStream{StreamService_EventBatchesServer: sink, receipts: &OwnershipCallbackReceipts{}, fed: "fed", handle: 2}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := stream.Send(input); err != nil {
			b.Fatal(err)
		}
	}
}
