// Shared error mapping helpers for the gRPC handler layer.
//
// File name is `errs.go` (not `errors.go`) to avoid shadowing the standard
// library's `errors` package inside this package.
//
// Used by: federation.go, declaration.go, object.go, stream.go.
// Shared across W3A/W3B/W3C; this is the canonical error mapping.

package grpc

import (
	"context"
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
// M39: ctx is the RPC context (unary handler ctx, or
// stream.Context() for streaming handlers). When the sentinel has an
// Annex C identity, the `rti-spec-exception` trailer is attached here —
// this is the ONE choke point every handler error already flows
// through (see spec_exception.go for the cross-SDK contract).
//
// Returns nil for nil input so callers can `return errToStatus(ctx, err)`
// uniformly on the happy path.
func errToStatus(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	attachSpecException(ctx, err)
	switch {
	// NotFound — entity (federation / class / attribute / region / etc) does not exist.
	case errors.Is(err, core.ErrFederationNotFound),
		errors.Is(err, core.ErrObjectNotFound),
		errors.Is(err, core.ErrInteractionClassNotFound),
		errors.Is(err, core.ErrInteractionParameterNotFound),
		errors.Is(err, core.ErrObjectClassNotFound),
		errors.Is(err, core.ErrAttributeNotFound),
		errors.Is(err, core.ErrSyncPointNotRegistered),
		errors.Is(err, core.ErrRoutingSpaceNotFound),
		errors.Is(err, core.ErrDimensionNotFound),
		errors.Is(err, core.ErrRegionNotFound),
		errors.Is(err, core.ErrFederateNotInSave),
		errors.Is(err, core.ErrFederateNotInRestore),
		errors.Is(err, core.ErrObjectAlreadyDeleted): // M23
		return status.Error(codes.NotFound, err.Error())

	// AlreadyExists — entity creation conflict.
	case errors.Is(err, core.ErrFederationAlreadyExists),
		errors.Is(err, core.ErrFederateAlreadyJoined),
		errors.Is(err, core.ErrSyncPointAlreadyRegistered):
		return status.Error(codes.AlreadyExists, err.Error())

	// PermissionDenied — federate lacks authority for the operation.
	// Distinct from FailedPrecondition (state-forbids) because the action
	// itself is unauthorized regardless of state.
	case errors.Is(err, core.ErrAttributeNotOwned),
		errors.Is(err, core.ErrRegionNotOwnedByFederate),
		errors.Is(err, core.ErrObjectNotOwned),                    // M23
		errors.Is(err, core.ErrObjectInstanceNameReservedByOther): // M26
		return status.Error(codes.PermissionDenied, err.Error())

	// FailedPrecondition — entity exists but state forbids the action.
	case errors.Is(err, core.ErrFederateNotJoined),
		errors.Is(err, core.ErrFederationGenerationMismatch),
		errors.Is(err, core.ErrFederationHasFederatesJoined),
		errors.Is(err, core.ErrFederationHalted),
		errors.Is(err, core.ErrObjectClassNotPublished),
		errors.Is(err, core.ErrInteractionClassNotPublished),
		errors.Is(err, core.ErrTimeNotRegulating),
		errors.Is(err, core.ErrTimeNotConstrained),
		errors.Is(err, core.ErrTimeAlreadyRegulating),
		errors.Is(err, core.ErrTimeAlreadyConstrained),
		errors.Is(err, core.ErrTimeAdvancingState),                // M21 TASK-202c: re-export of time.ErrDuplicateNER
		errors.Is(err, core.ErrTimeAlreadyAsynchronous),           // M22 TASK-235
		errors.Is(err, core.ErrTimeNotAsynchronous),               // M22 TASK-235
		errors.Is(err, core.ErrAttributeNotPublishedByFederation), // M23
		errors.Is(err, core.ErrWireVersionMismatch),
		errors.Is(err, core.ErrSyncPointAlreadyAchieved),
		errors.Is(err, core.ErrOwnershipDivestPending),
		errors.Is(err, core.ErrOwnershipAcquirePending),
		errors.Is(err, core.ErrOwnershipNotInTransfer),
		errors.Is(err, core.ErrRegionInUse),
		errors.Is(err, core.ErrSaveAlreadyInProgress),
		errors.Is(err, core.ErrRestoreAlreadyInProgress),
		errors.Is(err, core.ErrSaveBundleCorrupt),
		errors.Is(err, core.ErrSaveNotInProgress),             // M24
		errors.Is(err, core.ErrRestoreNotInProgress),          // M24
		errors.Is(err, core.ErrObjectInstanceNameInUse),       // M26
		errors.Is(err, core.ErrObjectInstanceNameNotReserved): // M26
		return status.Error(codes.FailedPrecondition, err.Error())

	// InvalidArgument — caller-supplied value violates the contract.
	// M21 TASK-202c: ErrTimeRequestInPast moved here from FailedPrecondition.
	// Rationale: the manager fires this when t < currentTime + lookahead,
	// which the caller controls — bad input, not bad state.
	case errors.Is(err, core.ErrFederationInvalidName),
		errors.Is(err, core.ErrObjectHandleInvalid),
		errors.Is(err, core.ErrTimeInvalidLookahead),
		errors.Is(err, core.ErrTimeRequestInPast),
		errors.Is(err, core.ErrTimeInvalidLogicalTime), // M37 EB-3: same rationale as ErrTimeRequestInPast — the sender controls the timestamp

		errors.Is(err, core.ErrTransportTypeUnspecified), // M23
		errors.Is(err, core.ErrResignActionUnsupported),  // M24
		errors.Is(err, core.ErrEncInsufficientBytes),
		errors.Is(err, core.ErrEncTypeMismatch),
		errors.Is(err, core.ErrEncPaddingViolation),
		errors.Is(err, core.ErrWireMalformedMessage):
		return status.Error(codes.InvalidArgument, err.Error())

	default:
		return status.Error(codes.Internal, err.Error())
	}
}
