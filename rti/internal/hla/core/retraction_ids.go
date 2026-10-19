package core

import (
	"context"
	"errors"
)

var (
	ErrRetractionIDAlreadyUsed      = errors.New("retraction ID is already accepted or reserved")
	ErrRetractionHistoryUnavailable = errors.New("complete retraction ID history is unavailable")
	ErrRetractionIDExhausted        = errors.New("retraction ID allocator is exhausted")
	ErrRetractionHandleInvalid      = errors.New("message retraction designator is invalid for this sender")
)

// RetractionIDAdmission reserves an originating operation, not each recipient.
// Commit records an accepted ID; Release cancels an uncommitted reservation.
// Distinct nonmonotonic IDs are valid. Membership and checkpoint admission must
// remain held until the reservation is committed or released.
type RetractionIDAdmission interface {
	ReserveRetractionID(context.Context, FederationName, FederateHandle, uint64) (OutboxReservation, error)
}
