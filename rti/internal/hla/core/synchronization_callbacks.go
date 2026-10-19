package core

import "context"

// SyncCallbackKind identifies an IEEE federation synchronization callback.
type SyncCallbackKind uint8

const (
	SyncCallbackAnnounced SyncCallbackKind = iota + 1
	SyncCallbackSynchronized
	SyncCallbackRegistrationSucceeded
	SyncCallbackRegistrationFailed
)

// SyncCallbackObserver receives authenticated actual-invocation reports.
// Sequence identifies a particular callback across label reuse and retry.
type SyncCallbackObserver interface {
	ObserveSyncCallbackEntry(context.Context, FederationName, FederateHandle, SyncCallbackKind, string, uint64) error
	ObserveSyncCallback(context.Context, FederationName, FederateHandle, SyncCallbackKind, string, uint64) error
}

// SyncLifecycle follows committed membership changes outside membership locks.
// A delivery error leaves a durable synchronization obligation for RetryPending.
type SyncLifecycle interface {
	FederateJoined(context.Context, FederationName, FederateHandle) error
	FederateResigned(context.Context, FederationName, FederateHandle) error
	RetryPending(context.Context, FederationName) error
}

// SyncPendingQuery lets the retry worker discover restored unsent obligations.
type SyncPendingQuery interface {
	PendingFederations() []FederationName
}
