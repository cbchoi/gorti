package server

import (
	"context"
	"fmt"
	"log/slog"

	grpcsvc "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	storage "github.com/cbchoi/gorti/rti/internal/adapters/savebundle/storage"
	"github.com/cbchoi/gorti/rti/internal/adapters/sdkstate"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

type runtimeSaveDependencies struct {
	directory       string
	logger          *slog.Logger
	outbox          *multiOutbox
	journal         core.EventLog
	foms            *composition.Repository
	federations     *federation.Manager
	synchronization *syncpkg.Manager
	ownership       *ownership.Manager
	management      *mom.Manager
	regions         *ddm.Manager
	declarations    *declaration.Manager
	receipts        func() *grpcsvc.CallbackReceipts
}

func newTypedSaveManager[T core.LogicalTimeScalar](deps runtimeSaveDependencies, objects *object.RegistryOf[T], times *timepkg.ManagerOf[T]) (*savepoint.ManagerOf[T], error) {
	store, err := storage.NewFSStorageOf[T](deps.directory)
	if err != nil {
		return nil, fmt.Errorf("rtid: typed savepoint storage init: %w", err)
	}
	snapshots := map[string]savepoint.ManagerSnapshotter{
		grpcsvc.CallbackCheckpointKey:           grpcsvc.CallbackCheckpoint{Receipts: deps.receipts, Ownership: deps.ownership.ExportState, Transportation: objects.ExportTransportationCheckpoint},
		"retractions":                           savebundle.RetractionsOf[T]{Manager: times},
		"fom":                                   savebundle.FOM{Repository: deps.foms},
		"automatic_resign":                      savebundle.AutomaticResign{Manager: deps.federations},
		savepoint.ManagerSnapshotKeySync:        synchronizationCheckpoint{Manager: deps.synchronization},
		savepoint.ManagerSnapshotKeyOwnership:   savebundle.Ownership{Manager: deps.ownership},
		savepoint.ManagerSnapshotKeyMOM:         savebundle.MOM{Manager: deps.management},
		savepoint.ManagerSnapshotKeyDDM:         savebundle.DDM{Manager: deps.regions},
		savepoint.ManagerSnapshotKeyObject:      savebundle.ObjectOf[T]{Manager: objects},
		savepoint.ManagerSnapshotKeyDeclaration: savebundle.Declaration{Manager: deps.declarations},
		savepoint.ManagerSnapshotKeyTime:        savebundle.TimeOf[T]{Manager: times},
	}
	graph := savebundle.RestoreGraphOf[T]{Snapshots: snapshots, FOMs: deps.foms}
	return savepoint.NewOf[T](savepoint.OptionsOf[T]{
		Outbox: grpcsvc.NewMandatoryCallbackOutbox(deps.outbox, deps.receipts), EventLog: deps.journal, BundleStore: store, HistoryStore: store,
		OnSaveCompleted: func(ctx context.Context, fed core.FederationName) {
			if err := deps.management.RefreshMetrics(context.WithoutCancel(ctx), fed, 0); err != nil {
				deps.logger.Warn("MOM completed-save update unavailable", "federation", fed, "error", err)
			}
		},
		Members: deps.federations.MembersOf, Roster: deps.federations.ListMembers,
		Generation: deps.federations.GenerationFor, FOMSHA256: deps.foms.DigestFor,
		LogicalTimeImplementation: deps.federations.LogicalTimeImplementationFor,
		ScheduledTime:             times.ReserveScheduledSave, ResumeTime: times.ResumeScheduledSave,
		ValidateSaveRequest:                  times.ValidateScheduledSaveRequest,
		PrepareScheduledTime:                 times.PrepareScheduledSave,
		PrepareImmediateTime:                 times.PrepareImmediateSave,
		CheckpointLabels:                     store,
		PrepareRestoreTransitionWithManifest: newRuntimeRestoreTransition[T](deps, objects),
		ValidateRestoreSnapshots:             newRuntimeRestoreValidator[T](deps, graph),
		RebindRestore:                        newRestoreRebindingOf[T](deps.federations, graph),
		ValidateClientState:                  sdkstate.Validate,
		RebindClientState:                    sdkstate.Rebind,
		ValidateRestoreCallbackRecovery:      validateRuntimeCallbackRecoveryOf[T],
		JoinedModules:                        deps.federations.JoinedModules, ManagerSnapshots: snapshots,
		RequiredSnapshots: []string{"retractions", savepoint.ManagerSnapshotKeySync, savepoint.ManagerSnapshotKeyOwnership, savepoint.ManagerSnapshotKeyMOM, savepoint.ManagerSnapshotKeyDDM, savepoint.ManagerSnapshotKeyObject, savepoint.ManagerSnapshotKeyDeclaration, savepoint.ManagerSnapshotKeyTime},
	})
}

type runtimeSaveAdmission struct {
	implementations core.FederationTimeImplementation
	floating        *savepoint.Manager
	integer         *savepoint.ManagerOf[int64]
}
type runtimeSaveAdmissionManager interface {
	scheduledSaveManager
	core.FederateServiceAdmission
	AcquireObservation(core.FederationName) func()
	InitializeSaveHistory(core.FederationName) error
	CheckFederateCallback(core.FederationName, core.FederateHandle) error
	ObserveCheckpointCallbackEntry(core.FederationName, core.FederateHandle, string, bool)
}

func (s *runtimeSaveAdmission) selected(fed core.FederationName) runtimeSaveAdmissionManager {
	name, ok := s.implementations.LogicalTimeImplementationFor(fed)
	if ok && name == core.ScalarRepresentation[int64]().String() {
		return s.integer
	}
	return s.floating
}
func (s *runtimeSaveAdmission) AcquireService(fed core.FederationName) (func(), error) {
	return s.selected(fed).AcquireService(fed)
}
func (s *runtimeSaveAdmission) AcquireFederateService(fed core.FederationName, h core.FederateHandle) (func(), error) {
	return s.selected(fed).AcquireFederateService(fed, h)
}
func (s *runtimeSaveAdmission) AcquireCheckpoint(fed core.FederationName) func() {
	return s.selected(fed).AcquireCheckpoint(fed)
}
func (s *runtimeSaveAdmission) AcquireObservation(fed core.FederationName) func() {
	return s.selected(fed).AcquireObservation(fed)
}
func (s *runtimeSaveAdmission) HasPendingScheduledSave(fed core.FederationName) bool {
	return s.selected(fed).HasPendingScheduledSave(fed)
}
func (s *runtimeSaveAdmission) ProgressScheduledSave(ctx context.Context, fed core.FederationName) error {
	return s.selected(fed).ProgressScheduledSave(ctx, fed)
}
func (s *runtimeSaveAdmission) InitializeSaveHistory(fed core.FederationName) error {
	return s.selected(fed).InitializeSaveHistory(fed)
}

func newRuntimeRestoreTransition[T core.LogicalTimeScalar](deps runtimeSaveDependencies, objects *object.RegistryOf[T]) func(ctx context.Context, fed core.FederationName, manifest savepoint.ManifestOf[T], participants []savepoint.RestoreParticipant, terminal []core.OutboxDelivery) (core.OutboxReservation, error) {
	return func(ctx context.Context, fed core.FederationName, manifest savepoint.ManifestOf[T], participants []savepoint.RestoreParticipant, terminal []core.OutboxDelivery) (core.OutboxReservation, error) {
		receipts := deps.receipts()
		if receipts == nil {
			return nil, fmt.Errorf("restore callback ledger unavailable")
		}
		owned, err := savebundle.ReadOwnershipCheckpointState(manifest.ManagerSnapshots[savepoint.ManagerSnapshotKeyOwnership])
		if err != nil {
			return nil, err
		}
		captured, err := grpcsvc.CallbackCheckpointSyncCallbacks(manifest.ManagerSnapshots[grpcsvc.CallbackCheckpointKey])
		if err != nil {
			return nil, err
		}
		model, err := transportationCheckpointSavedModel(ctx, fed, manifest.ManagerSnapshots["fom"], deps.foms)
		if err != nil {
			return nil, err
		}
		if err := validateTransportationCheckpointManifestOf(ctx, fed, manifest, model); err != nil {
			return nil, err
		}
		transportRows, err := grpcsvc.CallbackCheckpointTransportationRows(manifest.ManagerSnapshots[grpcsvc.CallbackCheckpointKey])
		if err != nil {
			return nil, err
		}
		objectState, err := savebundle.ReadTransportationCheckpointObjectStateOf[T](manifest.ManagerSnapshots[savepoint.ManagerSnapshotKeyObject])
		if err != nil {
			return nil, err
		}
		// Membership is pinned by the transition below. Capture live identity
		// first; retain its generation/incarnation while rebinding its handle.
		identities, err := restoreParticipantIdentities(deps.federations, fed, participants)
		if err != nil {
			return nil, err
		}
		pending, replay, err := receipts.PrepareCheckpointReplayWithOwnership(ctx, fed, manifest.ManagerSnapshots[grpcsvc.CallbackCheckpointKey], participants, owned)
		if err != nil {
			return nil, err
		}
		transition, err := newOwnershipRestoreTransitionWithPendingOf[T](deps.ownership, deps.federations, deps.outbox, pending)(ctx, fed, manifest, participants, terminal)
		if err != nil || len(terminal) == 0 {
			replay.Release()
			return transition, err
		}
		scopes, err := transportationCheckpointPlannedScopes(transition, identities, participants)
		if err != nil {
			transition.Release()
			replay.Release()
			return nil, err
		}
		requests := transportationCheckpointRequests(transportRows, scopes)
		prepared, err := objects.PrepareTransportationCheckpoint(ctx, requests, objectState, owned, model)
		if err != nil {
			transition.Release()
			replay.Release()
			return nil, err
		}
		transportation, err := replay.PrepareTransportation(ctx, nil, scopes, prepared)
		if err != nil {
			prepared.Release()
			transition.Release()
			replay.Release()
			return nil, err
		}
		label := terminal[0].Event.(*savepoint.RestoredEvent).Label()
		return &callbackReplayTransition{transition: receipts.PreserveRestoreTransition(fed, label, participants, transition), replay: replay, transportation: transportation, beforePublication: func() error { return deps.synchronization.ReconcileRestoredCallbacks(fed, captured) }}, nil
	}
}

func newRuntimeRestoreValidator[T core.LogicalTimeScalar](deps runtimeSaveDependencies, graph savebundle.RestoreGraphOf[T]) func(fed core.FederationName, manifest savepoint.ManifestOf[T], model core.FOMHandle) error {
	return func(fed core.FederationName, manifest savepoint.ManifestOf[T], model core.FOMHandle) error {
		if err := graph.Validate(fed, core.CheckpointManifestOf[T](manifest), model); err != nil {
			return err
		}
		if err := (grpcsvc.CallbackCheckpoint{}).ValidateConveyedRegions(manifest.ManagerSnapshots[grpcsvc.CallbackCheckpointKey], manifest.ManagerSnapshots[savepoint.ManagerSnapshotKeyDDM]); err != nil {
			return err
		}
		if model == nil {
			var err error
			model, err = deps.foms.Get(context.Background(), fed)
			if err != nil {
				return err
			}
		}
		if err := (grpcsvc.CallbackCheckpoint{}).ValidateModel(manifest.ManagerSnapshots[grpcsvc.CallbackCheckpointKey], model, manifest.LogicalTimeImplementationName); err != nil {
			return err
		}
		return validateTransportationCheckpointManifestOf(context.Background(), fed, manifest, model)
	}
}

func restoreParticipantIdentities(federations *federation.Manager, fed core.FederationName, participants []savepoint.RestoreParticipant) (map[core.FederateHandle]core.MemberIdentity, error) {
	identities := make(map[core.FederateHandle]core.MemberIdentity, len(participants))
	for _, p := range participants {
		id, err := federations.MemberIdentity(fed, p.CurrentHandle)
		if err != nil {
			return nil, err
		}
		id.Handle = p.SavedHandle
		identities[p.SavedHandle] = id
	}
	return identities, nil
}

func transportationCheckpointRequests(transportRows []grpcsvc.TransportationCheckpointSavedRow, scopes map[core.FederateHandle]core.TransportationScope) []object.TransportationCheckpointRestore {
	var requests []object.TransportationCheckpointRestore
	for _, row := range transportRows {
		if !row.OutcomeOnly {
			requests = append(requests, object.TransportationCheckpointRestore{Scope: scopes[row.Recipient], Binding: *row.Binding})
		}
	}
	return requests
}
