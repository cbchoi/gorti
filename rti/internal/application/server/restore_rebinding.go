package server

import (
	"context"
	"fmt"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

// newRestoreRebinding is called only after explicit source provenance and the
// current name roster have been checked under exclusive checkpoint admission.
// Saved FOM parsing/semantic checks finish before fresh identities are consumed.
// No prepared FOM lease is retained while acquiring the federation roster lock.
func newRestoreRebinding(mgr *federation.Manager, graph savebundle.RestoreGraph) func(context.Context, core.FederationName, uint64, savepoint.Manifest) (savepoint.Manifest, error) {
	return newRestoreRebindingOf[core.LogicalTime](mgr, graph)
}

// newRestoreRebindingOf uses the selected scalar end to end, including saved
// graph validation, fresh identity rewriting and current retraction history.
func newRestoreRebindingOf[T core.LogicalTimeScalar](mgr *federation.Manager, graph savebundle.RestoreGraphOf[T]) func(context.Context, core.FederationName, uint64, savepoint.ManifestOf[T]) (savepoint.ManifestOf[T], error) {
	return func(ctx context.Context, fed core.FederationName, generation uint64, manifest savepoint.ManifestOf[T]) (savepoint.ManifestOf[T], error) {
		if err := validateRebindingSource(ctx, fed, manifest, graph); err != nil {
			return savepoint.ManifestOf[T]{}, err
		}
		references, err := graph.References(core.CheckpointManifestOf[T](manifest))
		if err != nil {
			return savepoint.ManifestOf[T]{}, err
		}
		references, err = extendSDKRestoreReferencesOf(ctx, manifest, references)
		if err != nil {
			return savepoint.ManifestOf[T]{}, err
		}
		fresh, err := mgr.ReserveRestoreHandles(fed, generation, len(references))
		if err != nil {
			return savepoint.ManifestOf[T]{}, err
		}
		mapping := make(map[core.FederateHandle]core.FederateHandle, len(references))
		for i, source := range references {
			mapping[source] = fresh[i]
		}
		rebound, err := graph.Rebind(core.CheckpointManifestOf[T](manifest), generation, mapping)
		if err != nil {
			return savepoint.ManifestOf[T]{}, err
		}
		rewritten, err := rebindSDKClientStatesOf(ctx, manifest, savepoint.ManifestOf[T](rebound), generation, mapping)
		if err != nil {
			return savepoint.ManifestOf[T]{}, err
		}
		rebound.FederateClientStates = rewritten.FederateClientStates
		byName := map[string]core.FederateHandle{}
		for i, name := range manifest.FederateNames {
			byName[name] = rebound.Federates[i]
		}
		sessions := map[core.FederateHandle]core.FederateHandle{}
		for _, member := range mgr.ListMembers(fed) {
			target, ok := byName[member.Name]
			if !ok {
				return savepoint.ManifestOf[T]{}, fmt.Errorf("current participant not in saved roster")
			}
			sessions[member.Handle] = target
		}
		ledger, ok := graph.Snapshots["retractions"]
		if !ok {
			return savepoint.ManifestOf[T]{}, core.ErrRetractionHistoryUnavailable
		}
		current, err := ledger.Marshal(fed)
		if err != nil {
			return savepoint.ManifestOf[T]{}, err
		}
		rebound.ManagerSnapshots["retractions"], err = savebundle.MergeSessionRetractionsOf[T](rebound.ManagerSnapshots["retractions"], current, sessions)
		if err != nil {
			return savepoint.ManifestOf[T]{}, err
		}
		return savepoint.ManifestOf[T](rebound), nil
	}
}

// Finish saved-model validation and release its lease before reserving identities.
func validateRebindingSource[T core.LogicalTimeScalar](ctx context.Context, fed core.FederationName, manifest savepoint.ManifestOf[T], graph savebundle.RestoreGraphOf[T]) error {
	if err := validateRuntimeCallbackRecoveryOf(ctx, fed, manifest); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	adapter, ok := graph.Snapshots["fom"].(savepoint.PreparedRestoreSnapshotter)
	if !ok {
		return fmt.Errorf("cross-generation restore needs a prepared FOM adapter")
	}
	prepared, err := adapter.PrepareRestoreState(fed, manifest.ManagerSnapshots["fom"])
	if prepared != nil {
		defer prepared.Release()
	}
	if err != nil {
		return err
	}
	candidate, ok := prepared.(interface{ Candidate() core.FOMHandle })
	if !ok {
		return fmt.Errorf("saved FOM candidate unavailable")
	}
	if err = graph.Validate(fed, core.CheckpointManifestOf[T](manifest), candidate.Candidate()); err != nil {
		return err
	}
	if err = (grpcadapter.CallbackCheckpoint{}).ValidateModel(manifest.ManagerSnapshots[grpcadapter.CallbackCheckpointKey], candidate.Candidate(), manifest.LogicalTimeImplementationName); err != nil {
		return err
	}
	if err = (grpcadapter.CallbackCheckpoint{}).ValidateConveyedRegions(manifest.ManagerSnapshots[grpcadapter.CallbackCheckpointKey], manifest.ManagerSnapshots[savepoint.ManagerSnapshotKeyDDM]); err != nil {
		return err
	}
	if err = validateTransportationCheckpointManifestOf(ctx, fed, manifest, candidate.Candidate()); err != nil {
		return err
	}
	prepared.Release()
	return nil
}
