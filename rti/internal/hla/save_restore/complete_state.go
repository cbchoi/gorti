package savepoint

import (
	"fmt"
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func validateSnapshotOptionsOf[T core.LogicalTimeScalar](opts OptionsOf[T]) error {
	if len(opts.RequiredSnapshots) == 0 {
		return nil
	}
	if opts.Members == nil {
		return fmt.Errorf("savepoint.New: required snapshots need a Members resolver")
	}
	if _, ok := opts.Outbox.(core.ReservableOutbox); !ok {
		return fmt.Errorf("savepoint.New: required snapshots need a reservable outbox")
	}
	seen := map[string]bool{}
	for _, key := range opts.RequiredSnapshots {
		if key == "" || seen[key] {
			return fmt.Errorf("savepoint.New: invalid required snapshot key %q", key)
		}
		seen[key] = true
		if !canPrepareSnapshot(opts.ManagerSnapshots[key]) {
			return fmt.Errorf("savepoint.New: required snapshot %q needs PrepareRestore", key)
		}
	}
	for key, snapshot := range opts.ManagerSnapshots {
		if !canPrepareSnapshot(snapshot) {
			return fmt.Errorf("savepoint.New: complete-state snapshot %q needs PrepareRestore", key)
		}
	}
	return nil
}

func (m *ManagerOf[T]) validateCompleteRoster(fed core.FederationName, manifest ManifestOf[T]) error {
	_, err := m.restoreParticipants(fed, manifest)
	return err
}

func canPrepareSnapshot(snapshot ManagerSnapshotter) bool {
	_, legacy := snapshot.(PreparedSnapshotter)
	_, leased := snapshot.(PreparedRestoreSnapshotter)
	_, transition := snapshot.(core.TransitionSnapshotter)
	return legacy || leased || transition
}

// FOMAwareSnapshotter validates dependent state against the saved, unpublished
// model instead of the live model, which may belong to a different checkpoint.
type FOMAwareSnapshotter interface {
	PrepareRestoreWithFOM(core.FederationName, []byte, core.FOMHandle) (func(), error)
}

func (m *ManagerOf[T]) prepareCompleteSnapshots(fed core.FederationName, snapshots map[string][]byte) (func() error, func(), error) {
	return m.prepareCompleteManifest(fed, ManifestOf[T]{Federation: fed, ManagerSnapshots: snapshots})
}

func (m *ManagerOf[T]) prepareCompleteManifest(fed core.FederationName, manifest ManifestOf[T]) (func() error, func(), error) {
	snapshots := manifest.ManagerSnapshots
	for _, key := range m.opts.RequiredSnapshots {
		if _, ok := snapshots[key]; !ok {
			return nil, nil, fmt.Errorf("%w: required snapshot %q is missing", ErrSaveBundleIncompatible, key)
		}
	}
	keys := make([]string, 0, len(snapshots))
	for key := range snapshots {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if index := slices.Index(keys, "fom"); index >= 0 {
		keys = append([]string{"fom"}, append(keys[:index], keys[index+1:]...)...)
	}
	plan := &snapshotPreparation{commits: make([]func() error, 0, len(keys))}
	_, hasSavedFOM := snapshots["fom"]
	var once sync.Once
	release := func() {
		once.Do(func() {
			for i := len(plan.releases) - 1; i >= 0; i-- {
				plan.releases[i]()
			}
		})
	}
	for _, key := range keys {
		if err := m.prepareSnapshot(fed, key, snapshots[key], hasSavedFOM, plan); err != nil {
			release()
			return nil, nil, err
		}
	}
	if m.opts.ValidateRestoreSnapshots != nil {
		if err := m.opts.ValidateRestoreSnapshots(fed, manifest, plan.savedFOM); err != nil {
			release()
			return nil, nil, fmt.Errorf("%w: saved state graph: %v", ErrSaveBundleIncompatible, err)
		}
	}
	return func() error {
		for _, commit := range plan.commits {
			if err := commit(); err != nil {
				return err
			}
		}
		return nil
	}, release, nil
}

func errorsOrNilPreparation(err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("nil prepared snapshot")
}

func (m *ManagerOf[T]) validateCompleteCommit(fed core.FederationName, manifest ManifestOf[T]) error {
	generation, digest, err := m.resolveProvenance(fed, true)
	if err != nil {
		return err
	}
	if err := m.validateRestoreProvenance(fed, manifest.Label, generation, digest, manifest); err != nil {
		return err
	}
	return m.validateCompleteRoster(fed, manifest)
}

type snapshotPreparation struct {
	commits  []func() error
	releases []func()
	savedFOM core.FOMHandle
}

func (m *ManagerOf[T]) prepareSnapshot(fed core.FederationName, key string, data []byte, hasSavedFOM bool, plan *snapshotPreparation) error {
	if _, ok := m.opts.ManagerSnapshots[key].(core.TransitionSnapshotter); ok {
		// restoreParticipants has prepared this state explicitly; the identity
		// reservation owns publication under its already-held roster lock.
		return nil
	}
	if snapshot, ok := m.opts.ManagerSnapshots[key].(FOMAwareSnapshotter); ok && hasSavedFOM {
		if plan.savedFOM == nil || !plan.savedFOM.IsValid() {
			return fmt.Errorf("%w: %s needs a valid saved FOM candidate", ErrSaveBundleIncompatible, key)
		}
		commit, err := snapshot.PrepareRestoreWithFOM(fed, append([]byte(nil), data...), plan.savedFOM)
		if err != nil || commit == nil {
			return fmt.Errorf("savepoint: prepare %s: %w", key, errorsOrNilPreparation(err))
		}
		plan.commits = append(plan.commits, func() error { commit(); return nil })
		return nil
	}
	if snapshot, ok := m.opts.ManagerSnapshots[key].(PreparedRestoreSnapshotter); ok {
		return plan.prepareLeased(fed, key, data, snapshot)
	}
	snapshot, ok := m.opts.ManagerSnapshots[key].(PreparedSnapshotter)
	if !ok {
		return fmt.Errorf("%w: unsupported snapshot %q", ErrSaveBundleIncompatible, key)
	}
	commit, err := snapshot.PrepareRestore(fed, append([]byte(nil), data...))
	if err != nil {
		return fmt.Errorf("savepoint: prepare %s: %w", key, err)
	}
	if commit != nil {
		plan.commits = append(plan.commits, func() error { commit(); return nil })
	}
	return nil
}

func (plan *snapshotPreparation) prepareLeased(fed core.FederationName, key string, data []byte, snapshot PreparedRestoreSnapshotter) error {
	prepared, err := snapshot.PrepareRestoreState(fed, append([]byte(nil), data...))
	if prepared != nil {
		plan.releases = append(plan.releases, prepared.Release)
	}
	if err != nil || prepared == nil {
		return fmt.Errorf("savepoint: prepare %s: %w", key, errorsOrNilPreparation(err))
	}
	if key == "fom" {
		if candidate, ok := prepared.(interface{ Candidate() core.FOMHandle }); ok {
			plan.savedFOM = candidate.Candidate()
		}
	}
	plan.commits = append(plan.commits, prepared.Commit)
	return nil
}
