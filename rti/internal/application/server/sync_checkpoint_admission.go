package server

import (
	"context"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// Admission of a queued confirmation is checked at callback entry, not only
// when its registration request was accepted. The receipt remains retryable.
func syncCheckpointEntryObserver(checkpoints *runtimeSaveAdmission, next func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent) error) func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent) error {
	return func(ctx context.Context, fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent) error {
		if checkpoints != nil && event != nil && grpcadapter.CurrentCheckpointControlEntry(ctx) {
			manager := checkpoints.selected(fed)
			if initiated := event.GetSaveInitiate(); initiated != nil {
				manager.ObserveCheckpointCallbackEntry(fed, h, initiated.GetLabel(), false)
			}
			if event.GetRestoreBegun() != nil {
				manager.ObserveCheckpointCallbackEntry(fed, h, "", true)
			}
			if initiated := event.GetRestoreInitiate(); initiated != nil {
				manager.ObserveCheckpointCallbackEntry(fed, h, initiated.GetLabel(), true)
			}
		}
		if checkpoints != nil && event != nil && (event.GetSyncRegistrationSucceeded() != nil || event.GetSyncRegistrationFailed() != nil) {
			if err := checkpoints.selected(fed).CheckFederateCallback(fed, h); err != nil {
				return err
			}
		}
		return next(ctx, fed, h, event)
	}
}
