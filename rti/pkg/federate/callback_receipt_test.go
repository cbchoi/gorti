package federate

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGoDispatchEventEmptyErrorPreservesFailureAndIdentity(t *testing.T) {
	for _, reportFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("reportFails=%t", reportFails), func(t *testing.T) {
			var reports atomic.Int32
			var entries atomic.Int32
			observed := make(chan *rtiv1.ReportCallbackInvocationRequest, 1)
			f := remainingServicesFederate(t, func(_ context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
				r := req.(*rtiv1.ReportCallbackInvocationRequest)
				if r.InvocationEntry {
					entries.Add(1)
					if !r.Success || r.Exception != "" {
						t.Error("entry carried callback outcome")
					}
					return &rtiv1.Empty{}, nil
				}
				reports.Add(1)
				observed <- r
				if r.Success || r.Exception == "" {
					return nil, status.Error(codes.InvalidArgument, "failure exception required")
				}
				if reportFails {
					return nil, status.Error(codes.Unavailable, "report unavailable")
				}
				return &rtiv1.Empty{}, nil
			})
			event := TimeAdvanceGrant{CallbackInfo: CallbackInfo{receipt: []byte(testCallbackReceipt), federate: 7}}
			f.callbackEntrySupported = true
			applicationErr := errors.New("")
			invocations := 0
			callbackErr, reportErr := f.DispatchEvent(context.Background(), event, func(Event) error {
				invocations++
				return applicationErr
			})
			if callbackErr != applicationErr || invocations != 1 || reports.Load() != 1 || entries.Load() != 1 {
				t.Fatalf("callback identity=%t invocations=%d reports=%d", callbackErr == applicationErr, invocations, reports.Load())
			}
			if (reportErr != nil) != reportFails {
				t.Errorf("report error=%v, expected failure=%t", reportErr, reportFails)
			}
			r := <-observed
			if r.Success || r.Exception != fmt.Sprintf("%T", applicationErr) {
				t.Errorf("empty-message error lost failure/type detail: %v", r)
			}
		})
	}
}

func TestGoCallbackFailureReportRequiresDetailBeforeRPC(t *testing.T) {
	var reports atomic.Int32
	f := remainingServicesFederate(t, func(_ context.Context, _ any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		reports.Add(1)
		return &rtiv1.Empty{}, nil
	})
	event := TimeAdvanceGrant{CallbackInfo: CallbackInfo{receipt: []byte(testCallbackReceipt), federate: 7}}
	if err := f.ReportCallbackInvocation(context.Background(), event, false, ""); err == nil {
		t.Error("accepted failure report without exception detail")
	}
	if reports.Load() != 0 {
		t.Fatalf("invalid failure report made %d RPCs", reports.Load())
	}
}
