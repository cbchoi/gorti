package server

import (
	"context"
	grpcsvc "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
)

func validateRuntimeCallbackRecoveryOf[T core.LogicalTimeScalar](ctx context.Context, fed core.FederationName, manifest savepoint.ManifestOf[T]) error {
	if err := validateSDKRestoreCallbackRecoveryOf(ctx, fed, manifest); err != nil {
		return err
	}
	data := manifest.ManagerSnapshots[grpcsvc.CallbackCheckpointKey]
	own, err := savebundle.ReadOwnershipCheckpointState(manifest.ManagerSnapshots[savepoint.ManagerSnapshotKeyOwnership])
	if err != nil {
		return err
	}
	if err = (grpcsvc.CallbackCheckpoint{}).ValidateOwnership(data, own); err != nil {
		return err
	}
	captured, err := grpcsvc.CallbackCheckpointSyncCallbacks(data)
	if err != nil {
		return err
	}
	if len(captured) == 0 {
		return nil
	}
	state, err := savebundle.ReadSynchronizationCheckpointState(manifest.ManagerSnapshots[savepoint.ManagerSnapshotKeySync])
	if err != nil {
		return err
	}
	return syncpkg.ValidateRestoredCallbacks(state, captured)
}
