package grpc

import (
	"errors"

	"github.com/cbchoi/gorti/rti/internal/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// errToStatus maps a core sentinel error to a gRPC status. Mapping mirrors
// the table in proto/rti/v1/errors.proto and docs/idd.md §1.1.4. Unknown
// errors collapse to Internal so callers always observe a valid status.
//
// nil input returns nil so handlers can write `return nil, errToStatus(err)`
// without an extra branch.
func errToStatus(err error) error {
	if err == nil {
		return nil
	}
	switch {
	// Federation lifecycle.
	case errors.Is(err, core.ErrFederationNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, core.ErrFederationAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, core.ErrFederationHasFederatesJoined):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, core.ErrFederationHalted):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, core.ErrFederationInvalidName):
		return status.Error(codes.InvalidArgument, err.Error())

	// Federate lifecycle.
	case errors.Is(err, core.ErrFederateNotJoined):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, core.ErrFederateAlreadyJoined):
		return status.Error(codes.AlreadyExists, err.Error())

	// Object / interaction.
	case errors.Is(err, core.ErrObjectNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, core.ErrObjectClassNotPublished):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, core.ErrObjectClassNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, core.ErrAttributeNotOwned):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, core.ErrAttributeNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, core.ErrInteractionClassNotPublished):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, core.ErrObjectHandleInvalid):
		return status.Error(codes.InvalidArgument, err.Error())

	// Wire-level.
	case errors.Is(err, core.ErrWireVersionMismatch):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, core.ErrWireMalformedMessage):
		return status.Error(codes.InvalidArgument, err.Error())

	default:
		return status.Error(codes.Internal, err.Error())
	}
}
