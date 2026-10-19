package federate

import (
	"context"
	"errors"
	"strings"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestOwnershipTerminalActualInvocationAndRetry(t *testing.T) {
	reports := 0
	fail := true
	f := remainingServicesFederate(t, func(_ context.Context, req any, info *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		if strings.HasSuffix(info.FullMethod, "ReportOwnershipCallbackInvocation") {
			reports++
			if fail {
				return nil, status.Error(codes.Unavailable, "retry")
			}
			receipt := req.(*rtiv1.ReportOwnershipCallbackInvocationRequest).Receipt
			if receipt.CallbackId != 1 || receipt.DeliveryAttempt == 0 {
				t.Fatal(receipt)
			}
		}
		return &rtiv1.Empty{}, nil
	})
	f.federationGeneration = 4
	f.ownershipReceiptsEnabled = true
	wire := &rtiv1.FederateEvent{OwnershipReceipt: &rtiv1.OwnershipCallbackReceipt{FederationGeneration: 4, CallbackEpoch: 1, CallbackId: 1, DeliveryAttempt: 1}, Event: &rtiv1.FederateEvent_OwnershipUnavailable{OwnershipUnavailable: &rtiv1.AttributeOwnershipUnavailable{ObjectHandle: 3, AttributeHandles: []uint64{2}}}}
	event := f.translate(wire)
	if reports != 0 {
		t.Fatal("queue/translation acknowledged callback")
	}
	invocations := 0
	appErr := errors.New(testApplicationFailure)
	callbackErr, reportErr := f.DispatchEvent(context.Background(), event, func(Event) error { invocations++; return appErr })
	if callbackErr != appErr || reportErr == nil || invocations != 1 {
		t.Fatalf("%v %v %d", callbackErr, reportErr, invocations)
	}
	fail = false
	wire.OwnershipReceipt.DeliveryAttempt = 2
	event = f.translate(wire)
	_, reportErr = f.DispatchEvent(context.Background(), event, func(Event) error { invocations++; return nil })
	if reportErr != nil || invocations != 1 {
		t.Fatalf("callback replayed %v %d", reportErr, invocations)
	}
	if len(f.ownershipPending) != 0 || reports != 2 {
		t.Fatalf("pending %d reports %d", len(f.ownershipPending), reports)
	}
}

func TestOwnershipBufferedAdviceEpochAndLocalDeleteFence(t *testing.T) {
	f := remainingServicesFederate(t, func(_ context.Context, _ any, info *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		if strings.HasSuffix(info.FullMethod, "LocalDeleteObjectInstanceFenced") {
			return &rtiv1.LocalDeleteObjectInstanceFence{RetiredKnowledgeEpoch: 10, FederationGeneration: 4, CallbackEpoch: 1}, nil
		}
		return &rtiv1.Empty{}, nil
	})
	f.federationGeneration = 4
	f.ownershipReceiptsEnabled = true
	old := RequestAttributeOwnershipAssumption{ObjectHandle: 9, CallbackInfo: CallbackInfo{knowledgeEpoch: 10}}
	if err := f.LocalDeleteObjectInstance(context.Background(), 9); err != nil {
		t.Fatal(err)
	}
	invocations := 0
	_, err := f.DispatchEvent(context.Background(), old, func(Event) error { invocations++; return nil })
	if err != nil || invocations != 0 {
		t.Fatalf("stale assumption delivered %d %v", invocations, err)
	}
	newer := DiscoverObjectInstance{ObjectHandle: 9, CallbackInfo: CallbackInfo{knowledgeEpoch: 11}}
	_, _ = f.DispatchEvent(context.Background(), newer, func(Event) error { invocations++; return nil })
	if invocations != 1 {
		t.Fatal("new discovery discarded")
	}
	terminal := AttributeOwnershipUnavailable{ObjectHandle: 9, CallbackInfo: CallbackInfo{knowledgeEpoch: 10}}
	_, _ = f.DispatchEvent(context.Background(), terminal, func(Event) error { invocations++; return nil })
	if invocations != 2 {
		t.Fatal("mandatory terminal callback discarded by knowledge epoch")
	}
}
