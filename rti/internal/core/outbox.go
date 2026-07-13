package core

import (
	"context"
	"errors"
)

// Outbox delivers server-initiated events (Discover, Reflect, Receive, Grant,
// FederationHalted) to a federate's outbound stream.
//
// Backpressure: implementations have a bounded per-federate buffer. On overflow,
// Send returns ErrFederateOverflow and the federate is treated as crashed
// (see SRS NFR-CRASH-1).
type Outbox interface {
	Send(ctx context.Context, fed FederationName, h FederateHandle, evt OutboundEvent) error
}

// OutboxReservation owns bounded capacity for an ordered set of recipient
// events. Reserve happens before a write-ahead append; Commit cannot fail for
// capacity or lifecycle reasons and transfers every event in order. Release
// abandons an uncommitted reservation. Implementations must make both methods
// idempotent.
type OutboxReservation interface {
	Commit() error
	Release()
}

// OutboxDelivery is one positional event transfer owned by a reservation.
type OutboxDelivery struct {
	Recipient FederateHandle
	Event     OutboundEvent
}

// ReservableOutbox extends Outbox with atomic all-recipient admission. The
// deliveries is positional and may repeat recipients. Supplying the events at
// admission time lets bounded batching implementations account for events such
// as TimeAdvanceGrant that force a short batch flush.
type ReservableOutbox interface {
	Outbox
	Reserve(ctx context.Context, fed FederationName, deliveries []OutboxDelivery) (OutboxReservation, error)
}

// OutboundEvent is the abstract callback envelope. Concrete type is the
// generated rtiv1.FederateEvent Protobuf message; this keeps core free of
// generated-package imports.
type OutboundEvent interface {
	Seq() uint64
}

// ErrFederateOverflow indicates a federate's outbound buffer is full;
// the federation manager should treat the federate as crashed.
var ErrFederateOverflow = errors.New("federate outbox overflow")

// ErrOutboxUnavailable means the intended recipient no longer has a bound
// outbound queue. It is distinct from ErrFederateNotJoined because the
// producer is still a valid federation member.
var ErrOutboxUnavailable = errors.New("federate outbox unavailable")
