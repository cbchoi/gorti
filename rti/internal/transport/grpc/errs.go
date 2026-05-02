// Shared error mapping helpers for the gRPC handler layer.
//
// File name is `errs.go` (not `errors.go`) to avoid shadowing the standard
// library's `errors` package inside this package.
//
// Used by: federation.go, declaration.go, object.go, stream.go.
// Owner: shared across W3A/W3B/W3C; orchestrator picks one canonical
// version at merge time if multiple wave branches define it.

package grpc

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// errToStatus maps a core sentinel (or wrapped sentinel) onto a gRPC
// status error, per docs/idd.md §1.1.4. Unknown / non-sentinel errors
// fall through to codes.Internal — never leak a raw Go error message
// to the wire if it's not a documented sentinel, but DO retain the
// text via status.Error so logs remain useful.
//
// Returns nil for nil input so callers can `return errToStatus(err)`
// uniformly on the happy path.
func errToStatus(err error) error {
	if err == nil {
		return nil
	}
	switch {
	// NotFound — entity (federation / class / attribute) does not exist.
	case errors.Is(err, core.ErrFederationNotFound),
		errors.Is(err, core.ErrObjectNotFound),
		errors.Is(err, core.ErrObjectClassNotFound),
		errors.Is(err, core.ErrAttributeNotFound):
		return status.Error(codes.NotFound, err.Error())

	// AlreadyExists — entity creation conflict.
	case errors.Is(err, core.ErrFederationAlreadyExists),
		errors.Is(err, core.ErrFederateAlreadyJoined):
		return status.Error(codes.AlreadyExists, err.Error())

	// FailedPrecondition — entity exists but state forbids the action.
	case errors.Is(err, core.ErrFederateNotJoined),
		errors.Is(err, core.ErrFederationHasFederatesJoined),
		errors.Is(err, core.ErrFederationHalted),
		errors.Is(err, core.ErrObjectClassNotPublished),
		errors.Is(err, core.ErrAttributeNotOwned),
		errors.Is(err, core.ErrInteractionClassNotPublished),
		errors.Is(err, core.ErrTimeNotRegulating),
		errors.Is(err, core.ErrTimeNotConstrained),
		errors.Is(err, core.ErrTimeAlreadyRegulating),
		errors.Is(err, core.ErrTimeAlreadyConstrained),
		errors.Is(err, core.ErrTimeRequestInPast):
		return status.Error(codes.FailedPrecondition, err.Error())

	// InvalidArgument — caller-supplied value violates the contract.
	case errors.Is(err, core.ErrFederationInvalidName),
		errors.Is(err, core.ErrObjectHandleInvalid),
		errors.Is(err, core.ErrTimeInvalidLookahead),
		errors.Is(err, core.ErrEncInsufficientBytes),
		errors.Is(err, core.ErrEncTypeMismatch),
		errors.Is(err, core.ErrEncPaddingViolation),
		errors.Is(err, core.ErrWireMalformedMessage):
		return status.Error(codes.InvalidArgument, err.Error())

	default:
		return status.Error(codes.Internal, err.Error())
	}
}
