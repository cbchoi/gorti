package grpc

import (
	"errors"

	"github.com/cbchoi/gorti/rti/internal/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// sentinelToCode maps every core sentinel error to a gRPC status code.
// The table mirrors proto/rti/v1/errors.proto and docs/idd.md §1.1.4.
// Errors not in the table collapse to codes.Internal.
var sentinelToCode = []struct {
	err  error
	code codes.Code
}{
	// Federation lifecycle.
	{core.ErrFederationNotFound, codes.NotFound},
	{core.ErrFederationAlreadyExists, codes.AlreadyExists},
	{core.ErrFederationHasFederatesJoined, codes.FailedPrecondition},
	{core.ErrFederationHalted, codes.FailedPrecondition},
	{core.ErrFederationInvalidName, codes.InvalidArgument},

	// Federate lifecycle.
	{core.ErrFederateNotJoined, codes.FailedPrecondition},
	{core.ErrFederateAlreadyJoined, codes.AlreadyExists},

	// Object / interaction.
	{core.ErrObjectNotFound, codes.NotFound},
	{core.ErrObjectClassNotPublished, codes.FailedPrecondition},
	{core.ErrObjectClassNotFound, codes.NotFound},
	{core.ErrAttributeNotOwned, codes.PermissionDenied},
	{core.ErrAttributeNotFound, codes.NotFound},
	{core.ErrInteractionClassNotPublished, codes.FailedPrecondition},
	{core.ErrObjectHandleInvalid, codes.InvalidArgument},

	// Wire-level.
	{core.ErrWireVersionMismatch, codes.FailedPrecondition},
	{core.ErrWireMalformedMessage, codes.InvalidArgument},
}

// errToStatus maps a core sentinel error to a gRPC status. nil input
// returns nil so handlers can write `return nil, errToStatus(err)`
// without an extra branch.
func errToStatus(err error) error {
	if err == nil {
		return nil
	}
	for _, m := range sentinelToCode {
		if errors.Is(err, m.err) {
			return status.Error(m.code, err.Error())
		}
	}
	return status.Error(codes.Internal, err.Error())
}
