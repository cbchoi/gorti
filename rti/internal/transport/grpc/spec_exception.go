// M39 Agent HB — structured spec-exception channel.
//
// Every RPC failure whose cause is a documented core sentinel carries the
// IEEE 1516.1-2010 Annex C exception class name in the gRPC TRAILING
// metadata under the key `rti-spec-exception`. SDKs read the trailer
// FIRST and only fall back to detail-string sniffing (the pre-M39
// mechanism) when the key is absent — so third-party clients get a
// machine-readable contract and the RTI is free to reword error text.
//
// Contract (shared with pysdk/cppsdk — see cppsdk/src/dlc/README.md):
//
//   - key:   "rti-spec-exception" (trailing metadata, unary + stream)
//   - value: Annex C exception class name, UpperCamelCase exactly as in
//     cppsdk/include/RTI/Exception.h (e.g. "ObjectClassNotPublished",
//     "InvalidLogicalTime", "FederateNotExecutionMember").
//   - absent: the failure has no unambiguous Annex C identity (either a
//     non-sentinel internal error, or a sentinel whose spec exception
//     depends on which service was called — e.g. ErrOwnershipNotInTransfer
//     is AttributeDivestitureWasNotRequested from a cancel-divest but
//     AttributeAcquisitionWasNotRequested from a cancel-acquire). Clients
//     fall back to their legacy sniffs.
//
// The table lives HERE (next to errToStatus) because errs.go is the one
// place that already knows both sides: every handler funnels its error
// return through errToStatus, and the switch below is keyed on the same
// core sentinels. Attachment happens inside errToStatus via grpc.SetTrailer
// so no per-handler code and no interceptor registration (outside this
// package's ownership) is needed. grpc.SetTrailer works for both unary
// handlers (ctx is the RPC context) and streaming handlers (pass
// stream.Context()); on a non-gRPC context (direct handler-invocation
// unit tests) it returns an error which we deliberately ignore.
package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// specExceptionTrailerKey is the trailing-metadata key carrying the
// Annex C exception class name. Fixed by the cross-SDK contract — do
// not rename without migrating pysdk + cppsdk in the same milestone.
const specExceptionTrailerKey = "rti-spec-exception"

// specExceptionTable maps core sentinels onto Annex C exception class
// names. Ordered slice (not a map) because lookup must use errors.Is —
// handlers wrap sentinels with context via fmt.Errorf("...: %w", ...).
//
// Deliberately unmapped sentinels (clients keep legacy behavior):
//   - ErrFederationHalted        — gorti-specific terminal state, no Annex C identity.
//   - ErrOwnershipNotInTransfer  — divest-vs-acquire ambiguity (see file comment).
//   - ErrSyncPointAlreadyRegistered — §4.11 reports this via the
//     synchronizationPointRegistrationFailed callback, not an exception.
//   - ErrRoutingSpaceNotFound    — HLA 1.3 routing-space surface; no 1516 name.
//   - ErrSaveBundleCorrupt, ErrWire* — internal errors (RTIinternalError implied).
var specExceptionTable = []struct {
	sentinel error
	spec     string
}{
	// Federation / federate lifecycle (§4).
	{core.ErrFederationNotFound, "FederationExecutionDoesNotExist"},
	{core.ErrFederationAlreadyExists, "FederationExecutionAlreadyExists"},
	{core.ErrFederationHasFederatesJoined, "FederatesCurrentlyJoined"},
	{core.ErrFederateNotJoined, "FederateNotExecutionMember"},
	{core.ErrFederateAlreadyJoined, "FederateAlreadyExecutionMember"},
	{core.ErrFederationInvalidName, "IllegalName"},
	{core.ErrResignActionUnsupported, "InvalidResignAction"},

	// Object / declaration (§5, §6).
	{core.ErrObjectNotFound, "ObjectInstanceNotKnown"},
	{core.ErrObjectAlreadyDeleted, "ObjectInstanceNotKnown"},
	{core.ErrObjectHandleInvalid, "ObjectInstanceNotKnown"},
	{core.ErrObjectClassNotFound, "ObjectClassNotDefined"},
	{core.ErrAttributeNotFound, "AttributeNotDefined"},
	{core.ErrInteractionClassNotFound, "InteractionClassNotDefined"},
	{core.ErrInteractionParameterNotFound, "InteractionParameterNotDefined"},
	{core.ErrObjectClassNotPublished, "ObjectClassNotPublished"},
	{core.ErrInteractionClassNotPublished, "InteractionClassNotPublished"},
	{core.ErrAttributeNotOwned, "AttributeNotOwned"},
	{core.ErrObjectNotOwned, "DeletePrivilegeNotHeld"},
	{core.ErrAttributeNotPublishedByFederation, "AttributeNotPublished"},
	{core.ErrTransportTypeUnspecified, "InvalidTransportationType"},
	{core.ErrObjectInstanceNameInUse, "ObjectInstanceNameInUse"},
	{core.ErrObjectInstanceNameReservedByOther, "ObjectInstanceNameInUse"},
	{core.ErrObjectInstanceNameNotReserved, "ObjectInstanceNameNotReserved"},

	// Time management (§8).
	{core.ErrTimeNotRegulating, "TimeRegulationIsNotEnabled"},
	{core.ErrTimeNotConstrained, "TimeConstrainedIsNotEnabled"},
	{core.ErrTimeAlreadyRegulating, "TimeRegulationAlreadyEnabled"},
	{core.ErrTimeAlreadyConstrained, "TimeConstrainedAlreadyEnabled"},
	{core.ErrTimeInvalidLookahead, "InvalidLookahead"},
	{core.ErrTimeRequestInPast, "LogicalTimeAlreadyPassed"},
	{core.ErrTimeInvalidLogicalTime, "InvalidLogicalTime"},
	{core.ErrTimeAdvancingState, "InTimeAdvancingState"},
	{core.ErrTimeAlreadyAsynchronous, "AsynchronousDeliveryAlreadyEnabled"},
	{core.ErrTimeNotAsynchronous, "AsynchronousDeliveryAlreadyDisabled"},

	// Synchronization (§4.11-4.14).
	{core.ErrSyncPointNotRegistered, "SynchronizationPointLabelNotAnnounced"},
	{core.ErrSyncPointAlreadyAchieved, "SynchronizationPointLabelNotAnnounced"},

	// Ownership (§7).
	{core.ErrOwnershipDivestPending, "AttributeAlreadyBeingDivested"},
	{core.ErrOwnershipAcquirePending, "AttributeAlreadyBeingAcquired"},

	// DDM (§9).
	{core.ErrRegionNotFound, "InvalidRegion"},
	{core.ErrRegionNotOwnedByFederate, "RegionNotCreatedByThisFederate"},
	{core.ErrRegionInUse, "RegionInUseForUpdateOrSubscription"},
	{core.ErrDimensionNotFound, "RegionDoesNotContainSpecifiedDimension"},

	// Save / restore (§4.16-4.24).
	{core.ErrSaveAlreadyInProgress, "SaveInProgress"},
	{core.ErrRestoreAlreadyInProgress, "RestoreInProgress"},
	{core.ErrSaveNotInProgress, "SaveNotInProgress"},
	{core.ErrRestoreNotInProgress, "RestoreNotInProgress"},
	{core.ErrFederateNotInSave, "FederateHasNotBegunSave"},
	{core.ErrFederateNotInRestore, "RestoreNotRequested"},

	// Encoding — a server-side decode failure of caller-supplied bytes.
	{core.ErrEncInsufficientBytes, "CouldNotDecode"},
	{core.ErrEncTypeMismatch, "CouldNotDecode"},
	{core.ErrEncPaddingViolation, "CouldNotDecode"},
}

// specExceptionName resolves err (or any wrapped sentinel inside it) to
// its Annex C exception class name. ok=false → no unambiguous mapping.
func specExceptionName(err error) (name string, ok bool) {
	for _, row := range specExceptionTable {
		if errors.Is(err, row.sentinel) {
			return row.spec, true
		}
	}
	return "", false
}

// attachSpecException sets the rti-spec-exception trailer on the RPC
// identified by ctx when err maps to an Annex C exception. Safe to call
// with any context: grpc.SetTrailer fails (and is ignored) when ctx is
// not a live gRPC server context, e.g. in direct handler-invocation
// unit tests.
func attachSpecException(ctx context.Context, err error) {
	name, ok := specExceptionName(err)
	if !ok {
		return
	}
	// Error deliberately ignored — see function comment.
	_ = grpc.SetTrailer(ctx, metadata.Pairs(specExceptionTrailerKey, name))
}
