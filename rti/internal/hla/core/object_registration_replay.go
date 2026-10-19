package core

import (
	"context"
	"errors"
)

// ErrObjectRegistrationReplayState means a registration record is invalid or
// incompatible with the registry image preceding it.
var ErrObjectRegistrationReplayState = errors.New("object registration replay state mismatch")

// ObjectRegistrationRecord identifies an already admitted registration. Name
// reservations are not represented by this journal record and must not be
// inferred from it. WallNS retains the source journal's informational timestamp.
type ObjectRegistrationRecord struct {
	Object     ObjectHandle
	Class      ObjectClassHandle
	Registrant FederateHandle
	Name       string
	WallNS     uint64
}

// ObjectRegistrationReplay is an internal recovery port, not a federate service.
// The replay coordinator supplies the matching federation, FOM, declarations,
// and prior state, and excludes live service calls during recovery. Implementors
// preserve recorded identity, reject conflicts before mutation, and never create
// name reservations. Public Register remains subject to reservation admission.
type ObjectRegistrationReplay interface {
	ReplayObjectRegistration(context.Context, FederationName, ObjectRegistrationRecord) error
}
