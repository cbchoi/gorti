package federate

import (
	"context"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGenericCallbackEntryPrecedesBodyAndDeduplicates(t *testing.T) {
	var phases []bool
	f := remainingServicesFederate(t, func(_ context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		r := req.(*rtiv1.ReportCallbackInvocationRequest)
		phases = append(phases, r.InvocationEntry)
		return &rtiv1.Empty{}, nil
	})
	event := TimeAdvanceGrant{CallbackInfo: CallbackInfo{receipt: make([]byte, 32), federate: 7}}
	f.callbackEntrySupported = true
	invoked := 0
	callback := func(Event) error {
		if len(phases) != 1 || !phases[0] {
			t.Fatal("body entered before ENTRY accepted")
		}
		invoked++
		return nil
	}
	for i := 0; i < 2; i++ {
		if a, r := f.DispatchEvent(context.Background(), event, callback); a != nil || r != nil {
			t.Fatalf("%v / %v", a, r)
		}
	}
	if invoked != 1 || len(phases) != 2 || phases[1] {
		t.Fatalf("invoked=%d phases=%v", invoked, phases)
	}
}

func TestGenericCallbackFailedEntryCanRetryButCompletionNeverReinvokes(t *testing.T) {
	entryFails, completionFails := true, false
	f := remainingServicesFederate(t, func(_ context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		r := req.(*rtiv1.ReportCallbackInvocationRequest)
		if r.InvocationEntry && entryFails || !r.InvocationEntry && completionFails {
			return nil, status.Error(codes.Unavailable, "retry")
		}
		return &rtiv1.Empty{}, nil
	})
	event := TimeAdvanceGrant{CallbackInfo: CallbackInfo{receipt: make([]byte, 32), federate: 7}}
	f.callbackEntrySupported = true
	invoked := 0
	callback := func(Event) error { invoked++; return nil }
	if a, r := f.DispatchEvent(context.Background(), event, callback); a != nil || r == nil || invoked != 0 {
		t.Fatalf("entry failure invoked body: %v %v %d", a, r, invoked)
	}
	entryFails, completionFails = false, true
	if _, r := f.DispatchEvent(context.Background(), event, callback); r == nil || invoked != 1 {
		t.Fatal("completion failure not retained")
	}
	completionFails = false
	if a, r := f.DispatchEvent(context.Background(), event, callback); a != nil || r != nil || invoked != 1 {
		t.Fatalf("duplicate reinvoked: %v %v %d", a, r, invoked)
	}
}

func TestGenericCallbackLegacyServerNeverReceivesPrematureCompletion(t *testing.T) {
	invoked, reports := false, 0
	f := remainingServicesFederate(t, func(_ context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		r := req.(*rtiv1.ReportCallbackInvocationRequest)
		if r.InvocationEntry || !invoked {
			t.Error("legacy server received premature completion")
		}
		reports++
		return &rtiv1.Empty{}, nil
	})
	event := TimeAdvanceGrant{CallbackInfo: CallbackInfo{receipt: make([]byte, 32), federate: 7}}
	if a, r := f.DispatchEvent(context.Background(), event, func(Event) error { invoked = true; return nil }); a != nil || r != nil {
		t.Fatalf("%v / %v", a, r)
	}
	if reports != 1 {
		t.Fatalf("reports=%d", reports)
	}
	if err := f.ReportCallbackEntry(context.Background(), event); err == nil {
		t.Fatal("explicit entry was dispatched without capability")
	}
}
