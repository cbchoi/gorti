package core

import (
	"context"
	"errors"
)

var (
	ErrAttributeAlreadyBeingChanged        = errors.New("attribute transportation is already being changed")
	ErrInteractionClassAlreadyBeingChanged = errors.New("interaction transportation is already being changed")
	ErrTransportationTicketInvalid         = errors.New("transportation activation ticket is invalid or retired")
	ErrTransportationPendingLimit          = errors.New("transportation activation capacity exhausted")
)

// TransportationScope is supplied only by an authenticated adapter retaining
// membership and checkpoint admission. Tickets are never snapshot state.
type TransportationScope struct {
	Member        MemberIdentity
	CallbackEpoch uint64
}

type TransportationChange struct {
	Ticket         []byte
	Scope          TransportationScope
	Object         ObjectHandle
	Attributes     []AttributeHandle
	Interaction    InteractionClassHandle
	Transportation TransportType
}

type TransportationActivation interface {
	PrepareAttributeTransportationChange(context.Context, TransportationScope, ObjectHandle, []AttributeHandle, TransportType) (TransportationChange, error)
	PrepareInteractionTransportationChange(context.Context, TransportationScope, InteractionClassHandle, TransportType) (TransportationChange, error)
	ActivateTransportationChange(context.Context, TransportationScope, []byte) (TransportationChange, error)
}
