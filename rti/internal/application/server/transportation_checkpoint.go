package server

import (
	"context"
	"fmt"
	"slices"

	grpcsvc "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

// Parsing does not acquire the repository write lease or publish a candidate.
// The restore coordinator may already hold a prepared FOM transaction here.
func transportationCheckpointSavedModel(ctx context.Context, fed core.FederationName, data []byte, repository *composition.Repository) (core.FOMHandle, error) {
	state, err := savebundle.ReadTransportationCheckpointFOMState(data)
	if err != nil {
		return nil, err
	}
	prepared, err := repository.PrepareImportState(ctx, fed, state)
	if err != nil {
		return nil, err
	}
	defer prepared.Release()
	candidate, ok := prepared.(interface{ Candidate() core.FOMHandle })
	if !ok {
		return nil, fmt.Errorf("transportation checkpoint: saved FOM candidate unavailable")
	}
	return candidate.Candidate(), nil
}

func validateTransportationCheckpointManifestOf[T core.LogicalTimeScalar](ctx context.Context, fed core.FederationName, manifest savepoint.ManifestOf[T], model core.FOMHandle) error {
	rows, err := grpcsvc.CallbackCheckpointTransportationRows(manifest.ManagerSnapshots[grpcsvc.CallbackCheckpointKey])
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	state, err := savebundle.ReadTransportationCheckpointObjectStateOf[T](manifest.ManagerSnapshots[savepoint.ManagerSnapshotKeyObject])
	if err != nil {
		return err
	}
	owned, err := savebundle.ReadOwnershipCheckpointState(manifest.ManagerSnapshots[savepoint.ManagerSnapshotKeyOwnership])
	if err != nil {
		return err
	}
	for i, row := range rows {
		if row.OutcomeOnly {
			continue
		}
		if row.Binding == nil {
			return fmt.Errorf("transportation checkpoint: pending intent missing")
		}
		var original []core.AttributeHandle
		for _, attr := range row.Event.GetTransportationChangeConfirmed().AttributeHandles {
			original = append(original, core.AttributeHandle(attr))
		}
		if err := object.ValidateTransportationCheckpointState(ctx, fed, row.Recipient, *row.Binding, state, owned, model, original); err != nil {
			return err
		}
		if row.Binding.Activated {
			continue
		}
		if err := validatePendingTransportationOverlap(row, rows[:i]); err != nil {
			return err
		}
	}
	return nil
}

func transportationCheckpointPlannedScopes(transition core.OutboxReservation, identities map[core.FederateHandle]core.MemberIdentity, participants []savepoint.RestoreParticipant) (map[core.FederateHandle]core.TransportationScope, error) {
	owned, ok := transition.(*ownershipScopeRestoreReservation)
	if !ok {
		return nil, fmt.Errorf("transportation checkpoint: ownership restore wrapper unavailable")
	}
	planned, ok := owned.OutboxReservation.(*restoreTransitionReservation)
	if !ok {
		return nil, fmt.Errorf("transportation checkpoint: planned restore epochs unavailable")
	}
	scopes := make(map[core.FederateHandle]core.TransportationScope, len(participants))
	for _, p := range participants {
		id, ok := identities[p.SavedHandle]
		epoch, present := planned.epochs[p.CurrentHandle]
		if !ok || !present {
			return nil, fmt.Errorf("transportation checkpoint: current identity or planned epoch missing")
		}
		scopes[p.SavedHandle] = core.TransportationScope{Member: id, CallbackEpoch: epoch}
	}
	return scopes, nil
}

func validatePendingTransportationOverlap(row grpcsvc.TransportationCheckpointSavedRow, priorRows []grpcsvc.TransportationCheckpointSavedRow) error {
	for _, prior := range priorRows {
		if prior.OutcomeOnly || prior.Binding == nil || prior.Binding.Activated {
			continue
		}
		if row.Binding.Object != 0 && row.Binding.Object == prior.Binding.Object {
			for _, attr := range row.Binding.ActualAttributes {
				if slices.Contains(prior.Binding.ActualAttributes, attr) {
					return core.ErrAttributeAlreadyBeingChanged
				}
			}
		} else if row.Binding.Interaction != 0 && row.Recipient == prior.Recipient && row.Binding.Interaction == prior.Binding.Interaction {
			return core.ErrInteractionClassAlreadyBeingChanged
		}
	}
	return nil
}
