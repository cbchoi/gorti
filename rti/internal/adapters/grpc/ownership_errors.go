package grpc

import (
	"context"
	"errors"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Cancellation disambiguates shared ownership sentinels at the service boundary.
func ownershipCancellationStatus(ctx context.Context, err error, acquisition bool) error {
	var name string
	switch {
	case errors.Is(err, core.ErrOwnershipNotInTransfer):
		name = "AttributeDivestitureWasNotRequested"
		if acquisition {
			name = "AttributeAcquisitionWasNotRequested"
		}
	case acquisition && errors.Is(err, core.ErrFederateOwnsAttributes):
		name = "AttributeAlreadyOwned"
	default:
		return errToStatus(ctx, err)
	}
	_ = grpcgo.SetTrailer(ctx, metadata.Pairs(specExceptionTrailerKey, name))
	return status.Error(codes.FailedPrecondition, err.Error())
}
