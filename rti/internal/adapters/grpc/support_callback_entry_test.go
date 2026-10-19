package grpc

import (
	"context"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSupportCallbackEntryRoutesWithoutRecordingCompletion(t *testing.T) {
	entered, completed := 0, 0
	receipts, err := NewCallbackReceipts(CallbackReceiptOptions{
		Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
		ObserveEntry: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent) error {
			entered++
			return nil
		},
		Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
			completed++
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_SyncAnnounced{SyncAnnounced: &rtiv1.SynchronizationPointAnnounced{Label: "ready"}}}
	ticket, commit, err := receipts.PrepareRequired(context.Background(), "f", 1, event)
	if err != nil {
		t.Fatal(err)
	}
	commit(true)
	service := &supportService{callbackReceipts: receipts}
	req := &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1, CallbackReceipt: ticket, Success: true, InvocationEntry: true}
	for i := 0; i < 2; i++ {
		if _, err := service.ReportCallbackInvocation(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	if entered != 1 || completed != 0 {
		t.Fatalf("entry=%d completion=%d", entered, completed)
	}
	req.InvocationEntry = false
	if _, err := service.ReportCallbackInvocation(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if completed != 1 {
		t.Fatalf("completion=%d", completed)
	}
}

func TestSupportCallbackEntryRejectsOutcomeContradictions(t *testing.T) {
	for _, req := range []*rtiv1.ReportCallbackInvocationRequest{
		{WireVersion: 1, InvocationEntry: true},
		{WireVersion: 1, InvocationEntry: true, Success: true, Exception: "failure"},
	} {
		_, err := (&supportService{}).ReportCallbackInvocation(context.Background(), req)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("contradictory entry: %v", err)
		}
	}
}
