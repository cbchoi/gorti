package core

import "context"

// MemberSnapshotLeaser excludes member operations and membership transitions.
// Callers acquire checkpoint admission first and must not hold a shared member
// lease. Release is idempotent. The generation comparison is exact, including 0.
type MemberSnapshotLeaser interface {
	AcquireMemberSnapshot(context.Context, FederationName, FederateHandle, uint64) (func(), error)
}

type LocalValidationState struct {
	FederationGeneration  uint64
	Federate              FederateHandle
	CallbackEpoch         uint64
	SnapshotID            uint64
	FOM                   FOMView
	Publications          []LocalObjectPublication
	PublishedInteractions []InteractionClassHandle
	Objects               []LocalKnownObject
}

type LocalObjectPublication struct {
	Class      ObjectClassHandle
	Attributes []AttributeHandle
}

type LocalKnownObject struct {
	Object ObjectHandle
	Class  ObjectClassHandle
	Owned  []AttributeHandle
}

// LocalValidationStateSource captures a complete mirror and commits its marker
// to the same ordered callback outbox before releasing exclusive admission.
// Returning state without a committed marker is forbidden.
type LocalValidationStateSource interface {
	LocalValidationState(context.Context, FederationName, FederateHandle, uint64, uint64, uint64) (LocalValidationState, error)
}

// LocalValidationBarrierEvent is internal control traffic, not a user callback.
type LocalValidationBarrierEvent struct {
	SnapshotID    uint64
	Federate      FederateHandle
	CallbackEpoch uint64
}

func (LocalValidationBarrierEvent) Seq() uint64 { return 0 }
