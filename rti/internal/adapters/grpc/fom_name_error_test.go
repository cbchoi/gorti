package grpc

import (
	"context"
	"fmt"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestInvalidFOMNameErrorClassification(t *testing.T) {
	for _, service := range []string{"create", "prepare join FOM"} {
		t.Run(service, func(t *testing.T) {
			err := fmt.Errorf("%s: %w", service, core.ErrFOMInvalidName)
			if status.Code(errToStatus(context.Background(), err)) != codes.InvalidArgument {
				t.Fatal("invalid FOM name did not map to InvalidArgument")
			}
			if name, ok := specExceptionName(err); !ok || name != "ErrorReadingFDD" {
				t.Fatalf("exception=(%s,%v), want ErrorReadingFDD", name, ok)
			}
		})
	}
}
