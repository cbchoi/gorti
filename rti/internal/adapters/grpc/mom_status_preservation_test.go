package grpc

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestMOMWrappedAdmissionStatusPreserved(t *testing.T) {
	for _, code := range []codes.Code{codes.Unauthenticated, codes.PermissionDenied, codes.Unimplemented, codes.ResourceExhausted} {
		t.Run(code.String(), func(t *testing.T) {
			original, err := status.New(code, "MOM admission rejected").WithDetails(&errdetails.ErrorInfo{Reason: "MOM_ADMISSION", Domain: "gorti"})
			if err != nil {
				t.Fatal(err)
			}
			wrapped := fmt.Errorf("mom dispatch HLAservice: %w", original.Err())
			for _, input := range []error{wrapped, errors.Join(wrapped, errors.New("rollback context"))} {
				mapped := status.Convert(errToStatus(context.Background(), input))
				if mapped.Code() != code {
					t.Fatalf("status lost through MOM dispatch: %v", mapped)
				}
				if len(mapped.Details()) != 1 || !proto.Equal(mapped.Details()[0].(*errdetails.ErrorInfo), original.Details()[0].(*errdetails.ErrorInfo)) {
					t.Fatalf("status details lost: %v", mapped)
				}
			}
		})
	}
	if code := status.Code(errToStatus(context.Background(), fmt.Errorf("domain: %w", core.ErrAttributeNotOwned))); code != codes.PermissionDenied {
		t.Fatalf("domain mapping changed: %v", code)
	}
	if code := status.Code(errToStatus(context.Background(), errors.New("unclassified failure"))); code != codes.Internal {
		t.Fatalf("unknown failure mapping changed: %v", code)
	}
}
