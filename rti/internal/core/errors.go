package core

import "errors"

// Sentinel errors returned by core implementations. Wire codes are mapped
// from these in the gRPC layer (see docs/idd.md §1.1.4).

var (
	// Federation
	ErrFederationNotFound          = errors.New("federation not found")
	ErrFederationAlreadyExists     = errors.New("federation already exists")
	ErrFederationHasFederatesJoined = errors.New("federation has federates joined")
	ErrFederateNotJoined           = errors.New("federate not joined")
	ErrFederateAlreadyJoined       = errors.New("federate already joined")
	ErrFederationHalted            = errors.New("federation halted")
	ErrFederationInvalidName       = errors.New("federation name is invalid")

	// Object
	ErrObjectNotFound             = errors.New("object not found")
	ErrObjectClassNotPublished    = errors.New("object class not published by federate")
	ErrAttributeNotOwned          = errors.New("attribute not owned by federate")
	ErrObjectHandleInvalid        = errors.New("object handle is invalid")
	ErrInteractionClassNotPublished = errors.New("interaction class not published")
	ErrObjectClassNotFound        = errors.New("object class not found in FOM")
	ErrAttributeNotFound          = errors.New("attribute not found in FOM")

	// Time
	ErrTimeNotRegulating       = errors.New("federate is not time-regulating")
	ErrTimeNotConstrained      = errors.New("federate is not time-constrained")
	ErrTimeInvalidLookahead    = errors.New("lookahead must be non-negative and finite")
	ErrTimeRequestInPast       = errors.New("requested time is not greater than current logical time")
	ErrTimeAlreadyRegulating   = errors.New("federate is already time-regulating")
	ErrTimeAlreadyConstrained  = errors.New("federate is already time-constrained")

	// Encoding
	ErrEncInsufficientBytes = errors.New("insufficient bytes for type")
	ErrEncTypeMismatch      = errors.New("value does not match codec type")
	ErrEncPaddingViolation  = errors.New("padding violates HLA Evolved alignment rule")

	// Wire
	ErrWireVersionMismatch  = errors.New("wire protocol version mismatch")
	ErrWireMalformedMessage = errors.New("malformed wire message")
)
