package savepoint

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// FederateSnapshotRebinder operates on a fully typed snapshot. Unknown formats
// must fail rather than preserve possibly execution-scoped references.
type FederateSnapshotRebinder = core.FederateSnapshotRebinder

// RequestFederationRestoreFrom selects an exact persisted source execution.
func (m *ManagerOf[T]) RequestFederationRestoreFrom(ctx context.Context, fed core.FederationName, requester core.FederateHandle, label string, savedGeneration uint64) error {
	return m.requestFederationRestore(ctx, fed, label, &requester, &savedGeneration)
}

func (m *ManagerOf[T]) prepareCrossGeneration(ctx context.Context, fed core.FederationName, generation uint64, digest string, manifest *ManifestOf[T], ar *activeRestoreOf[T]) error {
	if err := m.validateCrossGenerationSource(ctx, fed, manifest); err != nil {
		return err
	}
	source := cloneCrossGenerationManifestOf(*manifest)
	rebound, err := m.opts.RebindRestore(ctx, fed, generation, cloneCrossGenerationManifestOf(source))
	if err != nil {
		return fmt.Errorf("%w: source execution translation: %w", ErrSaveBundleIncompatible, err)
	}
	if err := validateCrossGenerationResult(fed, generation, source, rebound); err != nil {
		return err
	}
	handles, err := crossGenerationHandles(generation, source, rebound)
	if err != nil {
		return err
	}
	if err := m.rebindCrossGenerationClients(ctx, fed, generation, source, &rebound, handles); err != nil {
		return err
	}
	if err := validateClientStatesOf[T](rebound); err != nil {
		return err
	}
	ar.targetGeneration, ar.targetFOMSHA256 = cloneGeneration(&generation), digest
	*manifest = cloneCrossGenerationManifestOf(rebound)
	return nil
}

func cloneClientStates(states [][]byte) [][]byte {
	if states == nil {
		return nil
	}
	cloned := make([][]byte, len(states))
	for i, state := range states {
		cloned[i] = slices.Clone(state)
	}
	return cloned
}

func cloneCrossGenerationManifestOf[T core.LogicalTimeScalar](source ManifestOf[T]) ManifestOf[T] {
	cloned := source
	cloned.FederationGeneration = cloneGeneration(source.FederationGeneration)
	if source.SaveTime != nil {
		time := *source.SaveTime
		cloned.SaveTime = &time
	}
	cloned.Federates = slices.Clone(source.Federates)
	cloned.FederateNames = slices.Clone(source.FederateNames)
	cloned.FederateTypes = slices.Clone(source.FederateTypes)
	if source.FederateModules != nil {
		cloned.FederateModules = make([][]core.FOMModule, len(source.FederateModules))
		for i, modules := range source.FederateModules {
			cloned.FederateModules[i] = cloneRestoreModules(modules)
		}
	}
	cloned.FederateClientStates = cloneClientStates(source.FederateClientStates)
	if source.ManagerSnapshots != nil {
		cloned.ManagerSnapshots = make(map[string][]byte, len(source.ManagerSnapshots))
		for key, state := range source.ManagerSnapshots {
			cloned.ManagerSnapshots[key] = slices.Clone(state)
		}
	}
	return cloned
}

func (m *ManagerOf[T]) validateRestoreTarget(fed core.FederationName, ar *activeRestoreOf[T]) error {
	if err := m.validateRestoreTimeImplementation(fed, ar.manifest); err != nil {
		return err
	}
	if ar.targetGeneration != nil {
		generation, digest, err := m.resolveProvenance(fed, true)
		if err != nil {
			return err
		}
		if generation == nil || *generation != *ar.targetGeneration || digest != ar.targetFOMSHA256 {
			return fmt.Errorf("%w: target execution or FOM changed during restore", ErrSaveBundleIncompatible)
		}
	}
	return m.validateCompleteCommit(fed, ar.manifest)
}

func (m *ManagerOf[T]) validateCrossGenerationSource(ctx context.Context, fed core.FederationName, manifest *ManifestOf[T]) error {
	if err := validateClientStatesOf[T](*manifest); err != nil {
		return err
	}
	if m.opts.RebindRestore == nil || m.opts.ValidateRestoreSnapshots == nil || m.opts.Roster == nil || len(m.opts.RequiredSnapshots) == 0 || !m.opts.hasRestoreTransition() {
		return fmt.Errorf("%w: complete cross-generation restore is not configured", ErrSaveBundleIncompatible)
	}
	if _, present := manifest.ManagerSnapshots["fom"]; !present {
		return fmt.Errorf("%w: cross-generation restore requires the saved FOM image", ErrSaveBundleIncompatible)
	}
	if manifest.FederationGeneration == nil {
		return fmt.Errorf("%w: source execution generation missing", ErrSaveBundleIncompatible)
	}
	for _, h := range manifest.Federates {
		if uint64(h)>>32 != *manifest.FederationGeneration || uint64(h)&0xffffffff == 0 {
			return fmt.Errorf("%w: saved participant belongs to another execution", ErrSaveBundleIncompatible)
		}
	}
	if _, err := m.restoreParticipants(fed, *manifest); err != nil {
		return err
	}
	return m.validateCrossGenerationClients(ctx, fed, manifest)
}

func (m *ManagerOf[T]) validateCrossGenerationClients(ctx context.Context, fed core.FederationName, manifest *ManifestOf[T]) error {
	// Validate the entire source before the graph adapter can allocate identities.
	for i, state := range manifest.FederateClientStates {
		if len(state) == 0 {
			continue
		}
		if m.opts.ValidateClientState == nil || m.opts.RebindClientState == nil {
			return fmt.Errorf("%w: SDK runtime translation is not configured", ErrSaveBundleIncompatible)
		}
		if err := m.opts.ValidateClientState(ctx, ClientStateValidationRequest{
			Federation: fed, Label: manifest.Label, SourceGeneration: *manifest.FederationGeneration,
			SourceHandle: manifest.Federates[i], LogicalTimeImplementationName: manifest.LogicalTimeImplementationName,
			State: slices.Clone(state),
		}); err != nil {
			return fmt.Errorf("%w: SDK runtime preflight for participant %d: %w", ErrSaveBundleIncompatible, manifest.Federates[i], err)
		}
	}
	return nil
}

func validateCrossGenerationResult[T core.LogicalTimeScalar](fed core.FederationName, generation uint64, source, rebound ManifestOf[T]) error {
	if rebound.FederationGeneration == nil || *rebound.FederationGeneration != generation || rebound.Federation != fed || rebound.Label != source.Label ||
		rebound.Version != source.Version || rebound.FOMSHA256 != source.FOMSHA256 || rebound.LogicalTimeImplementationName != source.LogicalTimeImplementationName || !reflect.DeepEqual(rebound.FederateNames, source.FederateNames) || !reflect.DeepEqual(rebound.FederateTypes, source.FederateTypes) ||
		!reflect.DeepEqual(rebound.SaveTime, source.SaveTime) || !reflect.DeepEqual(rebound.FederateModules, source.FederateModules) || rebound.EventLogBytes != source.EventLogBytes ||
		len(rebound.Federates) != len(source.Federates) || !bytes.Equal(rebound.ManagerSnapshots["fom"], source.ManagerSnapshots["fom"]) {
		return fmt.Errorf("%w: invalid cross-generation translation", ErrSaveBundleIncompatible)
	}
	return nil
}

func crossGenerationHandles[T core.LogicalTimeScalar](generation uint64, source, rebound ManifestOf[T]) (map[core.FederateHandle]core.FederateHandle, error) {
	handles := make(map[core.FederateHandle]core.FederateHandle, len(source.Federates))
	seen := make(map[core.FederateHandle]bool, len(rebound.Federates))
	for i, h := range rebound.Federates {
		if uint64(h)>>32 != generation || uint64(h)&0xffffffff == 0 || seen[h] {
			return nil, fmt.Errorf("%w: translated participant belongs to another execution", ErrSaveBundleIncompatible)
		}
		seen[h] = true
		handles[source.Federates[i]] = h
	}
	return handles, nil
}

func (m *ManagerOf[T]) rebindCrossGenerationClients(ctx context.Context, fed core.FederationName, generation uint64, source ManifestOf[T], rebound *ManifestOf[T], handles map[core.FederateHandle]core.FederateHandle) error {
	// Server graph translation never substitutes for private runtime validation.
	// Retain results only after every participant's rewrite has succeeded.
	graphClientStates := cloneClientStates(rebound.FederateClientStates)
	rebound.FederateClientStates = cloneClientStates(source.FederateClientStates)
	for i, state := range source.FederateClientStates {
		if len(state) == 0 {
			continue
		}
		mapping := make(map[core.FederateHandle]core.FederateHandle, len(handles))
		for saved, target := range handles {
			mapping[saved] = target
		}
		var graphState []byte
		if len(graphClientStates) == len(source.FederateClientStates) {
			graphState = graphClientStates[i]
		}
		translated, err := m.opts.RebindClientState(ctx, ClientStateRebindRequest{
			Federation: fed, Label: source.Label, SourceGeneration: *source.FederationGeneration,
			TargetGeneration: generation, SourceHandle: source.Federates[i], TargetHandle: rebound.Federates[i],
			LogicalTimeImplementationName: source.LogicalTimeImplementationName,
			FederateHandles:               mapping, State: slices.Clone(state), ReboundState: graphState,
		})
		if err != nil {
			return fmt.Errorf("%w: SDK runtime translation for participant %d: %w", ErrSaveBundleIncompatible, source.Federates[i], err)
		}
		if len(translated) == 0 || len(translated) > MaxClientStateBytes {
			return fmt.Errorf("%w: SDK runtime translation returned empty or oversized state", ErrSaveBundleIncompatible)
		}
		rebound.FederateClientStates[i] = slices.Clone(translated)
	}
	return nil
}
