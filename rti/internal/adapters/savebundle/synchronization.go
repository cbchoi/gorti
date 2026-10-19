package savebundle

import (
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
	"google.golang.org/protobuf/proto"
)

type Synchronization struct{ Manager *syncpkg.Manager }

func (s Synchronization) Marshal(fed core.FederationName) ([]byte, error) {
	if s.Manager == nil {
		return nil, fmt.Errorf("sync snapshot: manager required")
	}
	if s.Manager.RequiresInvocationReceipts() {
		return (StrictSynchronization{Manager: s.Manager}).Marshal(fed)
	}
	state := s.Manager.ExportState(fed)
	if state == nil {
		return nil, nil
	}
	wire := &rtiv1.SyncManagerState{Points: make([]*rtiv1.SyncPointStateEntry, 0, len(state.Points))}
	for _, point := range state.Points {
		entry := &rtiv1.SyncPointStateEntry{Label: point.Label, Tag: append([]byte(nil), point.Tag...), State: syncStateToProto(point.State), Dynamic: point.Dynamic}
		entry.FailureHistoryComplete = proto.Bool(true)
		for _, handle := range point.Required {
			entry.Required = append(entry.Required, uint64(handle))
		}
		for _, handle := range point.Achieved {
			entry.Achieved = append(entry.Achieved, uint64(handle))
		}
		for _, handle := range point.Failed {
			entry.Failed = append(entry.Failed, uint64(handle))
		}
		wire.Points = append(wire.Points, entry)
	}
	return proto.Marshal(wire)
}

func (s Synchronization) Unmarshal(fed core.FederationName, data []byte) error {
	// Preserve the legacy direct-call contract; the manifest-aware path uses
	// PrepareRestore, where present empty bytes mean an empty saved state.
	if len(data) == 0 {
		return nil
	}
	commit, err := s.PrepareRestore(fed, data)
	if err != nil {
		return err
	}
	if commit != nil {
		commit()
	}
	return nil
}

// PrepareRestore validates the complete snapshot before any live mutation.
func (s Synchronization) PrepareRestore(fed core.FederationName, data []byte) (func(), error) {
	if isStrictSynchronization(data) || s.Manager != nil && s.Manager.RequiresInvocationReceipts() {
		return (StrictSynchronization{Manager: s.Manager}).PrepareRestore(fed, data)
	}
	var wire rtiv1.SyncManagerState
	if err := proto.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("sync snapshot for %q: %w", fed, err)
	}
	state := &syncpkg.ManagerState{Points: make([]syncpkg.PointState, 0, len(wire.Points))}
	for _, point := range wire.Points {
		if point == nil {
			return nil, fmt.Errorf("sync snapshot: nil point")
		}
		if err := validateSyncFailureHistory(point); err != nil {
			return nil, err
		}
		entry := syncpkg.PointState{Label: point.GetLabel(), Tag: append([]byte(nil), point.GetTag()...), State: protoToSyncState(point.GetState()), Dynamic: point.GetDynamic()}
		for _, handle := range point.GetRequired() {
			entry.Required = append(entry.Required, core.FederateHandle(handle))
		}
		for _, handle := range point.GetAchieved() {
			entry.Achieved = append(entry.Achieved, core.FederateHandle(handle))
		}
		for _, handle := range point.GetFailed() {
			entry.Failed = append(entry.Failed, core.FederateHandle(handle))
		}
		state.Points = append(state.Points, entry)
	}
	if s.Manager == nil {
		return nil, fmt.Errorf("sync snapshot: manager required")
	}
	return s.Manager.PrepareImport(fed, state)
}

func validateSyncFailureHistory(point *rtiv1.SyncPointStateEntry) error {
	// An older image with no achievements proves no failed achievements either.
	// Once any achievement exists, absent completeness cannot establish success.
	if !point.GetFailureHistoryComplete() && (len(point.Achieved) != 0 || len(point.Failed) != 0) {
		return fmt.Errorf("sync snapshot %q: failed-achievement history unavailable", point.Label)
	}
	achieved := make(map[uint64]bool, len(point.Achieved))
	for _, h := range point.Achieved {
		achieved[h] = true
	}
	failed := make(map[uint64]bool, len(point.Failed))
	for _, h := range point.Failed {
		if h == 0 || !achieved[h] || failed[h] {
			return fmt.Errorf("sync snapshot %q: invalid failed participant", point.Label)
		}
		failed[h] = true
	}
	return nil
}

func syncStateToProto(state syncpkg.SyncPointState) rtiv1.SyncPointSnapshotState {
	switch state {
	case syncpkg.StateAnnounced:
		return rtiv1.SyncPointSnapshotState_SYNC_POINT_SNAPSHOT_STATE_ANNOUNCED
	case syncpkg.StateAchieved:
		return rtiv1.SyncPointSnapshotState_SYNC_POINT_SNAPSHOT_STATE_ACHIEVED
	default:
		return rtiv1.SyncPointSnapshotState_SYNC_POINT_SNAPSHOT_STATE_UNSPECIFIED
	}
}

func protoToSyncState(state rtiv1.SyncPointSnapshotState) syncpkg.SyncPointState {
	switch state {
	case rtiv1.SyncPointSnapshotState_SYNC_POINT_SNAPSHOT_STATE_ANNOUNCED:
		return syncpkg.StateAnnounced
	case rtiv1.SyncPointSnapshotState_SYNC_POINT_SNAPSHOT_STATE_ACHIEVED:
		return syncpkg.StateAchieved
	default:
		return syncpkg.StateUnknown
	}
}
