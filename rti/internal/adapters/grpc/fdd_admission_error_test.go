package grpc

import (
	"context"
	"fmt"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFDDInvalidModuleErrorClassification(t *testing.T) {
	for _, service := range []string{"create", "prepare join FOM"} {
		t.Run(service, func(t *testing.T) {
			err := fmt.Errorf("%s: %w", service, core.ErrFOMInvalidModule)
			if status.Code(errToStatus(context.Background(), err)) != codes.InvalidArgument {
				t.Error("invalid FOM content did not map to InvalidArgument")
			}
			if name, ok := specExceptionName(err); !ok || name != "ErrorReadingFDD" {
				t.Errorf("exception=(%s,%v), want ErrorReadingFDD", name, ok)
			}
		})
	}
}
