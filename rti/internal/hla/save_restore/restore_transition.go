package savepoint

import (
	"fmt"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// RestoreParticipant binds a currently joined session to its saved identity.
// Both handles belong to the same live federation execution/generation.
type RestoreParticipant = core.RestoreParticipant

// SnapshotDigestValidator proves the manifest identity against saved FOM bytes,
// rather than against a later live extension. Validation must not publish state.
type SnapshotDigestValidator interface {
	ValidateSnapshotDigest(data []byte, expectedSHA256 string) error
}

// PreparedRestore retains a preparation lease until commit or cancellation.
// Commit errors are observable; Release is idempotent and always called.
type PreparedRestore = core.PreparedRestore

type PreparedRestoreSnapshotter interface {
	PrepareRestoreState(core.FederationName, []byte) (PreparedRestore, error)
}

func (m *ManagerOf[T]) restoreParticipants(fed core.FederationName, manifest ManifestOf[T]) ([]RestoreParticipant, error) {
	if manifest.FederateTypes != nil && (len(manifest.FederateTypes) != len(manifest.Federates) || m.opts.Roster == nil) {
		return nil, fmt.Errorf("%w: participant type census missing or malformed", ErrSaveBundleIncompatible)
	}
	if manifest.FederateModules != nil && len(manifest.FederateModules) != len(manifest.Federates) {
		return nil, fmt.Errorf("%w: participant module baseline count changed", ErrSaveBundleIncompatible)
	}
	current := m.currentMembers(fed)
	if len(current) != len(manifest.Federates) {
		return nil, fmt.Errorf("%w: participant count changed", ErrSaveBundleIncompatible)
	}
	byName, typesByName, err := m.restoreRoster(fed, manifest)
	if err != nil {
		return nil, err
	}
	result, err := m.matchRestoreParticipants(manifest, current, byName, typesByName)
	if err != nil {
		return nil, err
	}
	for key, snapshot := range m.opts.ManagerSnapshots {
		if transition, ok := snapshot.(core.TransitionSnapshotter); ok {
			if !m.opts.hasRestoreTransition() {
				return nil, fmt.Errorf("%w: snapshot %s needs identity transition", ErrSaveBundleIncompatible, key)
			}
			var err error
			result, err = transition.PrepareRestoreParticipants(fed, manifest.ManagerSnapshots[key], result)
			if err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func cloneRestoreModules(modules []core.FOMModule) []core.FOMModule {
	if modules == nil {
		return nil
	}
	result := make([]core.FOMModule, len(modules))
	for i, module := range modules {
		result[i] = module
		result[i].XML = append([]byte(nil), module.XML...)
	}
	return result
}

func releaseRestoreOf[T core.LogicalTimeScalar](ar *activeRestoreOf[T]) {
	if ar != nil && ar.release != nil {
		ar.release()
		ar.release = nil
	}
}

func (m *ManagerOf[T]) restoreRoster(fed core.FederationName, manifest ManifestOf[T]) (map[string]core.FederateHandle, map[string]string, error) {
	byName := map[string]core.FederateHandle{}
	typesByName := map[string]string{}
	if m.opts.Roster != nil {
		if len(manifest.FederateNames) != len(manifest.Federates) {
			return nil, nil, fmt.Errorf("%w: participant names missing", ErrSaveBundleIncompatible)
		}
		for _, member := range m.opts.Roster(fed) {
			if member.Name == "" || byName[member.Name] != 0 {
				return nil, nil, fmt.Errorf("%w: empty or duplicate live name", ErrSaveBundleIncompatible)
			}
			byName[member.Name] = member.Handle
			typesByName[member.Name] = member.FederateType
		}
	}
	return byName, typesByName, nil
}

func (m *ManagerOf[T]) matchRestoreParticipants(manifest ManifestOf[T], current map[core.FederateHandle]struct{}, byName map[string]core.FederateHandle, typesByName map[string]string) ([]RestoreParticipant, error) {

	seenSaved, seenCurrent := map[core.FederateHandle]bool{}, map[core.FederateHandle]bool{}
	result := make([]RestoreParticipant, 0, len(manifest.Federates))
	for i, saved := range manifest.Federates {
		p, err := m.restoreParticipant(manifest, i, byName, typesByName)
		if err != nil {
			return nil, err
		}
		_, exists := current[p.CurrentHandle]
		if saved == 0 || p.CurrentHandle == 0 || seenSaved[saved] || seenCurrent[p.CurrentHandle] || !exists {
			return nil, fmt.Errorf("%w: missing or duplicate participant identity", ErrSaveBundleIncompatible)
		}
		if saved != p.CurrentHandle && !m.opts.hasRestoreTransition() {
			return nil, fmt.Errorf("%w: identity transition is not configured", ErrSaveBundleIncompatible)
		}
		seenSaved[saved], seenCurrent[p.CurrentHandle] = true, true
		result = append(result, p)
	}
	return result, nil
}

func (m *ManagerOf[T]) restoreParticipant(manifest ManifestOf[T], i int, byName map[string]core.FederateHandle, typesByName map[string]string) (RestoreParticipant, error) {
	saved := manifest.Federates[i]
	p := RestoreParticipant{SavedHandle: saved, CurrentHandle: saved}
	if manifest.FederateModules != nil {
		p.ModulesKnown = true
		p.SavedModules = cloneRestoreModules(manifest.FederateModules[i])
	}
	if m.opts.Roster != nil {
		p.Name = manifest.FederateNames[i]
		p.CurrentHandle = byName[p.Name]
		if manifest.FederateTypes != nil && manifest.FederateTypes[i] != typesByName[p.Name] {
			return RestoreParticipant{}, fmt.Errorf("%w: participant type changed for %q", ErrSaveBundleIncompatible, p.Name)
		}
	}
	return p, nil
}
