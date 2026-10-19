package core

import (
	"context"
	"errors"
)

// Outbox delivers server-initiated events (Discover, Reflect, Receive, Grant,
// FederationHalted) to a federate's outbound stream.
//
// Backpressure: implementations have a bounded per-federate buffer. On overflow,
// Send rejects the current transfer with ErrFederateOverflow. The owner of an
// already accepted domain message must retain it for retry; this error alone
// does not resign the recipient.
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

// UncommittedWireError certifies that a direct data operation failed while
// reserving its fanout, before its journal append or callback publication.
// Internal sequence ranges may have been consumed and need not be contiguous.
// Transports may retry a known transient cause after releasing service leases.
// Never use this for a management operation or a failed Commit.
type UncommittedWireError struct{ Err error }

func (e *UncommittedWireError) Error() string { return e.Err.Error() }
func (e *UncommittedWireError) Unwrap() error { return e.Err }

// OutboundQueueStat is a transport-neutral snapshot of one federate's
// callback queue. It lives in core so delivery and observability adapters can
// exchange queue state without importing one another.
type OutboundQueueStat struct {
	Federation FederationName
	Handle     FederateHandle
	QueueDepth uint32
	Capacity   uint32
	DropsTotal uint64
}

// OutboundEvent is a domain callback envelope. The protocol adapter converts
// concrete service events to wire messages without exposing generated types here.
type OutboundEvent interface {
	Seq() uint64
}

// ErrFederateOverflow indicates a federate's outbound buffer is full.
var ErrFederateOverflow = errors.New("federate outbox overflow")

// ErrOutboxUnavailable means the intended recipient no longer has a bound
// outbound queue. It is distinct from ErrFederateNotJoined because the
// producer is still a valid federation member.
var ErrOutboxUnavailable = errors.New("federate outbox unavailable")
