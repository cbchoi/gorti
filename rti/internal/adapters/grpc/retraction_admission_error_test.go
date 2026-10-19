package grpc

import (
	"context"
	"fmt"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRetractionAdmissionErrorsAreExplicitPreconditions(t *testing.T) {
	for _, cause := range []error{
		core.ErrRetractionIDAlreadyUsed,
		core.ErrRetractionHistoryUnavailable,
		core.ErrRetractionIDExhausted,
	} {
		t.Run(cause.Error(), func(t *testing.T) {
			err := fmt.Errorf("outbound message: %w", cause)
			if got := status.Code(errToStatus(context.Background(), err)); got != codes.FailedPrecondition {
				t.Fatalf("code = %s, want FailedPrecondition", got)
			}
			if name, mapped := specExceptionName(err); mapped {
				t.Fatalf("product admission condition fabricated IEEE exception %q", name)
			}
		})
	}
}
