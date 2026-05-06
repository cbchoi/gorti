package sync

import (
	"fmt"
	"slices"

	"google.golang.org/protobuf/proto"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// Marshal serializes the federation's sync-point state into a
// SyncManagerState proto and returns its wire bytes. M13 thread C
// (docs/srs.md §10.4): the savepoint Manager bundles this byte slice
// into the save manifest under the "sync" key.
//
// Returns nil bytes (and a nil error) for federations the manager has
// never seen — the on-disk manifest's manager_snapshots["sync"] is
// then absent, and Unmarshal of an absent slice on restore is a
// silent no-op (the manager was empty pre-save, the federation is
// empty post-restore).
//
// Iteration order is label-sorted ascending so the encoded bytes are
// deterministic across replays.
func (m *Manager) Marshal(fed core.FederationName) ([]byte, error) {
	m.mu.RLock()
	st, ok := m.fed[fed]
	if !ok {
		m.mu.RUnlock()
		return nil, nil
	}
	labels := make([]string, 0, len(st.points))
	for l := range st.points {
		labels = append(labels, l)
	}
	slices.Sort(labels)
	out := &rtiv1.SyncManagerState{
		Points: make([]*rtiv1.SyncPointStateEntry, 0, len(labels)),
	}
	for _, l := range labels {
		sp := st.points[l]
		entry := &rtiv1.SyncPointStateEntry{
			Label:    l,
			Tag:      append([]byte(nil), sp.tag...),
			State:    syncStateToProto(sp.state),
			Required: sortedHandlesUint64(sp.required),
			Achieved: sortedHandlesUint64(sp.achieved),
			Dynamic:  sp.dynamic,
		}
		out.Points = append(out.Points, entry)
	}
	m.mu.RUnlock()
	return proto.Marshal(out)
}

// Unmarshal reconstructs the federation's sync-point state from a
// SyncManagerState wire-format byte slice. M13 thread C: invoked by
// the savepoint Manager during RequestFederationRestore, before the
// event-log slice replay runs.
//
// Empty input is a no-op (returns nil) — the manifest may carry no
// snapshot for federations that had no sync points active at save
// time. The manager replaces any pre-existing state for fed with the
// bundled set; the production restore-bootstrap path constructs a
// fresh manager so this is just an idempotent rewrite.
func (m *Manager) Unmarshal(fed core.FederationName, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	var pb rtiv1.SyncManagerState
	if err := proto.Unmarshal(data, &pb); err != nil {
		return fmt.Errorf("sync: unmarshal SyncManagerState for %q: %w", fed, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := &federationState{points: map[string]*syncPoint{}}
	for _, e := range pb.GetPoints() {
		if e == nil {
			continue
		}
		required := map[core.FederateHandle]struct{}{}
		for _, h := range e.GetRequired() {
			required[core.FederateHandle(h)] = struct{}{}
		}
		achieved := map[core.FederateHandle]struct{}{}
		for _, h := range e.GetAchieved() {
			achieved[core.FederateHandle(h)] = struct{}{}
		}
		st.points[e.GetLabel()] = &syncPoint{
			tag:      append([]byte(nil), e.GetTag()...),
			state:    protoToSyncState(e.GetState()),
			required: required,
			achieved: achieved,
			dynamic:  e.GetDynamic(),
		}
	}
	m.fed[fed] = st
	return nil
}

func syncStateToProto(s SyncPointState) rtiv1.SyncPointSnapshotState {
	switch s {
	case StateAnnounced:
		return rtiv1.SyncPointSnapshotState_SYNC_POINT_SNAPSHOT_STATE_ANNOUNCED
	case StateAchieved:
		return rtiv1.SyncPointSnapshotState_SYNC_POINT_SNAPSHOT_STATE_ACHIEVED
	default:
		return rtiv1.SyncPointSnapshotState_SYNC_POINT_SNAPSHOT_STATE_UNSPECIFIED
	}
}

func protoToSyncState(s rtiv1.SyncPointSnapshotState) SyncPointState {
	switch s {
	case rtiv1.SyncPointSnapshotState_SYNC_POINT_SNAPSHOT_STATE_ANNOUNCED:
		return StateAnnounced
	case rtiv1.SyncPointSnapshotState_SYNC_POINT_SNAPSHOT_STATE_ACHIEVED:
		return StateAchieved
	default:
		return StateUnknown
	}
}

func sortedHandlesUint64(set map[core.FederateHandle]struct{}) []uint64 {
	out := make([]uint64, 0, len(set))
	for h := range set {
		out = append(out, uint64(h))
	}
	slices.Sort(out)
	return out
}
