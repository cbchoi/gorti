package core

import (
	"context"
	"errors"
)

var ErrSaveTimeNotAllowed = errors.New("federate may not use the save timestamp: not regulating and GALT undefined")

// RequesterSavepointCoordinatorOf retains the authenticated requesting member
// without changing the existing embedded save coordinator contract.
type RequesterSavepointCoordinatorOf[T LogicalTimeScalar] interface {
	RequestFederationSaveFrom(context.Context, FederationName, FederateHandle, string, *T) error
}

type FederateServiceAdmission interface {
	AcquireFederateService(FederationName, FederateHandle) (func(), error)
}

// ScheduledSaveReservation prepares a replacement without cancelling its prior
// barrier. Commit installs it; Release discards preparation or ends the active
// barrier. Ready drains prerequisite callbacks before returning eligible members.
type ScheduledSaveReservation interface {
	Commit()
	Release()
	Ready(context.Context) ([]FederateHandle, error)
	Pause([]FederateHandle) error
}
