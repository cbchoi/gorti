package core

import (
	"context"
	"errors"
)

type OwnershipCallbackScope struct {
	Federation           FederationName
	Recipient            FederateHandle
	FederationGeneration uint64
	CallbackEpoch        uint64
}

type OwnershipCallbackReceipt struct {
	FederationGeneration uint64
	CallbackEpoch        uint64
	CallbackID           uint64
	DeliveryAttempt      uint64
}

var (
	ErrOwnershipCallbackScope    = errors.New("ownership callback scope is stale or unavailable")
	ErrOwnershipCallbackReceipt  = errors.New("ownership callback receipt is invalid or unavailable")
	ErrOwnershipCallbackCapacity = errors.New("ownership callback obligation window exhausted")
	ErrOwnershipCallbackBusy     = errors.New("ownership callback delivery is already in progress")
	ErrOwnershipCallbackComplete = errors.New("ownership callback was already invoked and acknowledged")
)

// The adapter authenticates and pins membership before invoking this port.
// A successful invocation report is an SDK assertion of actual callback entry,
// including an invocation whose handler throws. It is independent of MOM metrics.
type OwnershipCallbackCoordinator interface {
	EnableOwnershipCallbackReceipts(context.Context, OwnershipCallbackScope) error
	BeginOwnershipCallbackDelivery(context.Context, OwnershipCallbackScope, uint64) (OwnershipCallbackReceipt, func(bool), error)
	ReportOwnershipCallbackInvocation(context.Context, OwnershipCallbackScope, OwnershipCallbackReceipt) error
	RetryOwnershipCallbacks(context.Context, OwnershipCallbackScope) error
}

// Terminal callbacks retain their identity through attribute projection.
// Strict=false preserves legacy queue-admission semantics without a mandatory
// invocation receipt. Failed admission is still retained by the domain ledger.
type OwnershipCallbackIdentity interface {
	OwnershipCallbackIdentity() (OwnershipCallbackScope, uint64, bool)
}

// Called under the exclusive checkpoint gate after imported ownership state is
// visible, before publishing the prevalidated membership/queue transition.
type OwnershipCallbackRestorePreparer interface {
	PrepareOwnershipCallbackRestoreScopes(FederationName, []OwnershipCallbackScope) (OutboxReservation, error)
	PrepareOwnershipCallbackScopeRestore(FederationName, map[FederateHandle]OwnershipCallbackScope) (OutboxReservation, error)
}
