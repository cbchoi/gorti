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

// OutboundEvent is the abstract callback envelope. Concrete type is the
// generated rtiv1.FederateEvent Protobuf message; this keeps core free of
// generated-package imports.
type OutboundEvent interface {
	Seq() uint64
}

// ErrFederateOverflow indicates a federate's outbound buffer is full;
// the federation manager should treat the federate as crashed.
var ErrFederateOverflow = errors.New("federate outbox overflow")
