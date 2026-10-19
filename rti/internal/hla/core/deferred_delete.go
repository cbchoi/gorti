package core

import "errors"

var (
	ErrObjectDeletionPending = errors.New("object deletion is already pending")
	ErrRetractionTooLate     = errors.New("object deletion has already been delivered")
)

// DeletionLifecycle pins object state while a time delivery is admitted. The
// time evaluator calls Prepare without holding its state-store mutexes. Commit
// follows successful outbox acceptance; Release leaves object state unchanged.
// Finish must run after releasing the evaluator and all time-store locks.
type DeletionLifecycle interface {
	PrepareDeletionDelivery(FederationName, []OutboundEvent) OutboxReservation
	FinishDeletionDelivery(FederationName)
}

// InternalTSOEvent participates in logical-time ordering without a wire callback.
type InternalTSOEvent interface {
	InternalTSO() bool
}

// ObjectDeletionEvent identifies a queued removal for lifecycle cancellation.
type ObjectDeletionEvent interface {
	DeletionObject() ObjectHandle
}
