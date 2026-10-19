package core

import "context"

// OwnershipOperation identifies a federate's ownership service request.
type OwnershipOperation uint8

const (
	OwnershipUnconditionalDivest OwnershipOperation = iota + 1
	OwnershipNegotiatedDivest
	OwnershipNegotiatedDivestTwoPhase
	OwnershipConfirmDivestiture
	OwnershipAcquire
	OwnershipAcquireIfAvailable
	OwnershipCancelDivest
	OwnershipCancelAcquire
	OwnershipDivestIfWanted
	OwnershipReleaseDenied
)

// OwnershipCommand uses attribute handles from the caller's known object class.
type OwnershipCommand struct {
	Operation  OwnershipOperation
	Federation FederationName
	Federate   FederateHandle
	Object     ObjectHandle
	Attributes []AttributeHandle
	Tag        []byte
}

// KnownOwnershipCoordinator maps and validates attributes while pinning object
// knowledge through the ownership mutation. Returned attributes use the same
// caller coordinates. Legacy algorithm compositions may omit this capability.
type KnownOwnershipCoordinator interface {
	KnownOwnershipAvailable() bool
	ExecuteKnownOwnership(context.Context, OwnershipCommand) ([]AttributeHandle, error)
}
