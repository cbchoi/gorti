package grpc

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timedomain "github.com/cbchoi/gorti/rti/internal/hla/time"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTimeEnableCallbacksEncodeActualTime(t *testing.T) {
	for _, at := range []core.LogicalTime{0, 7.25} {
		for _, event := range []core.OutboundEvent{&timedomain.TimeRegulationEnabled{Time: at}, &timedomain.TimeConstrainedEnabled{Time: at}} {
			encoded, err := EncodeOutboundEvent(event)
			if err != nil || encoded == nil {
				t.Fatalf("encode %T: %v", event, err)
			}
			if encoded.GetSeq() != 0 {
				t.Fatalf("invented journal sequence: %v", encoded)
			}
			if _, regulation := event.(*timedomain.TimeRegulationEnabled); regulation {
				if callbackServiceName(encoded) != "timeRegulationEnabled" {
					t.Fatal("regulation receipt has no service identity")
				}
				if encoded.GetTimeRegulationEnabled() == nil || encoded.GetTimeRegulationEnabled().GetLogicalTime() != float64(at) {
					t.Fatalf("regulation callback lost: %v", encoded)
				}
			} else {
				if callbackServiceName(encoded) != "timeConstrainedEnabled" {
					t.Fatal("constraint receipt has no service identity")
				}
				if encoded.GetTimeConstrainedEnabled() == nil || encoded.GetTimeConstrainedEnabled().GetLogicalTime() != float64(at) {
					t.Fatalf("constraint callback lost: %v", encoded)
				}
			}
		}
	}
}

func TestTimeEnablePendingAndRetractionStatus(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code codes.Code
		spec string
	}{
		{timedomain.ErrTimeRegulationPending, codes.FailedPrecondition, "RequestForTimeRegulationPending"},
		{timedomain.ErrTimeConstrainedPending, codes.FailedPrecondition, "RequestForTimeConstrainedPending"},
		{core.ErrRetractionHandleInvalid, codes.InvalidArgument, "InvalidMessageRetractionHandle"},
		{core.ErrRetractionTooLate, codes.FailedPrecondition, "MessageCanNoLongerBeRetracted"},
	} {
		if got := status.Code(errToStatus(context.Background(), tc.err)); got != tc.code {
			t.Errorf("%v mapped to %v, want %v", tc.err, got, tc.code)
		}
		found := false
		for _, entry := range specExceptionTable {
			if entry.sentinel == tc.err && entry.spec == tc.spec {
				found = true
			}
		}
		if !found {
			t.Errorf("%v has no %s exception identity", tc.err, tc.spec)
		}
	}
}
