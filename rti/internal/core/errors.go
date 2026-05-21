package core

import "errors"

// Sentinel errors returned by core implementations. Wire codes are mapped
// from these in the gRPC layer (see docs/idd.md §1.1.4).

var (
	// Federation
	ErrFederationNotFound           = errors.New("federation not found")
	ErrFederationAlreadyExists      = errors.New("federation already exists")
	ErrFederationHasFederatesJoined = errors.New("federation has federates joined")
	ErrFederateNotJoined            = errors.New("federate not joined")
	ErrFederateAlreadyJoined        = errors.New("federate already joined")
	ErrFederationHalted             = errors.New("federation halted")
	ErrFederationInvalidName        = errors.New("federation name is invalid")

	// Object
	ErrObjectNotFound               = errors.New("object not found")
	ErrObjectClassNotPublished      = errors.New("object class not published by federate")
	ErrAttributeNotOwned            = errors.New("attribute not owned by federate")
	ErrObjectHandleInvalid          = errors.New("object handle is invalid")
	ErrInteractionClassNotPublished = errors.New("interaction class not published")
	ErrObjectClassNotFound          = errors.New("object class not found in FOM")
	ErrAttributeNotFound            = errors.New("attribute not found in FOM")

	// Time
	ErrTimeNotRegulating      = errors.New("federate is not time-regulating")
	ErrTimeNotConstrained     = errors.New("federate is not time-constrained")
	ErrTimeInvalidLookahead   = errors.New("lookahead must be non-negative and finite")
	ErrTimeRequestInPast      = errors.New("requested time is not greater than current logical time")
	ErrTimeAlreadyRegulating  = errors.New("federate is already time-regulating")
	ErrTimeAlreadyConstrained = errors.New("federate is already time-constrained")
	// ErrTimeAdvancingState — federate has an outstanding advance request
	// (NER, NMRA, TAR, TARA, or FQR). Re-export for the wire layer
	// (errs.go matches via errors.Is). M21 TASK-202b.
	// time.ErrDuplicateNER aliases this so existing call sites keep working.
	ErrTimeAdvancingState = errors.New("federate has an outstanding advance request")
	// M22 — async-delivery toggle errors. Per IEEE 1516.1 §8.16-8.17.
	ErrTimeAlreadyAsynchronous = errors.New("federate has already enabled asynchronous delivery")
	ErrTimeNotAsynchronous     = errors.New("federate has not enabled asynchronous delivery")

	// M23 — object-management additions per IEEE 1516.1 §6.
	ErrObjectNotOwned                    = errors.New("object not owned by federate (cannot delete or change transport)")
	ErrAttributeNotPublishedByFederation = errors.New("no federate publishes any of the requested attributes")
	ErrObjectAlreadyDeleted              = errors.New("object instance already deleted")
	ErrTransportTypeUnspecified          = errors.New("transport type must be Reliable or BestEffort")

	// M24 — federation-management additions per IEEE 1516.1 §4.
	ErrResignActionUnsupported = errors.New("resign action not supported")
	ErrSaveNotInProgress       = errors.New("no save in progress to abort")
	ErrRestoreNotInProgress    = errors.New("no restore in progress to abort")

	// M26 Phase F — object instance name reservation per IEEE 1516.1 §6.1-6.5.
	ErrObjectInstanceNameInUse       = errors.New("object instance name already reserved or in use")
	ErrObjectInstanceNameNotReserved = errors.New("object instance name has not been reserved")
	ErrObjectInstanceNameReservedByOther = errors.New("object instance name is reserved by another federate")

	// Encoding
	ErrEncInsufficientBytes = errors.New("insufficient bytes for type")
	ErrEncTypeMismatch      = errors.New("value does not match codec type")
	ErrEncPaddingViolation  = errors.New("padding violates HLA Evolved alignment rule")

	// Wire
	ErrWireVersionMismatch  = errors.New("wire protocol version mismatch")
	ErrWireMalformedMessage = errors.New("malformed wire message")

	// Synchronization (M8 — FR-SYN-*)
	ErrSyncPointAlreadyRegistered = errors.New("synchronization point already registered in federation")
	ErrSyncPointNotRegistered     = errors.New("synchronization point not registered in federation")
	ErrSyncPointAlreadyAchieved   = errors.New("federate has already achieved this synchronization point")

	// Ownership (M8 — FR-OWN-*)
	ErrOwnershipDivestPending  = errors.New("attribute already has a pending negotiated divest")
	ErrOwnershipAcquirePending = errors.New("attribute already has a pending acquire")
	ErrOwnershipNotInTransfer  = errors.New("ownership transfer is not in progress for this attribute")

	// DDM (M10 — FR-DDM-*)
	ErrRoutingSpaceNotFound     = errors.New("routing space not declared in FOM")
	ErrDimensionNotFound        = errors.New("dimension not part of routing space")
	ErrRegionNotFound           = errors.New("region not found in federation")
	ErrRegionNotOwnedByFederate = errors.New("region is not owned by this federate")
	ErrRegionInUse              = errors.New("region is in use by an active subscription or instance")

	// Save/Restore (M9 — FR-SR-*)
	ErrSaveAlreadyInProgress    = errors.New("federation save is already in progress")
	ErrRestoreAlreadyInProgress = errors.New("federation restore is already in progress")
	ErrSaveBundleCorrupt        = errors.New("save bundle is corrupt or unreadable")
	ErrFederateNotInSave        = errors.New("federate is not part of the active save protocol")
	ErrFederateNotInRestore     = errors.New("federate is not part of the active restore protocol")
)
