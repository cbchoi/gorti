package mom

import (
	"fmt"
	"slices"
	"sync/atomic"

	"google.golang.org/protobuf/proto"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// Marshal serializes the federation's MOM state into an
// MOMManagerState proto and returns its wire bytes. M13 thread C
// (docs/srs.md §10.4): consumed by the savepoint Manager to bundle
// state under the manifest's "mom" key.
//
// Returns nil for federations the manager has never seen — Unmarshal
// treats nil/empty as no-op so round-trip is the identity. Federates
// are emitted in handle-sorted order so the encoded bytes are
// deterministic.
//
// Counters are read with atomic.LoadUint32 to ensure tear-free reads
// even under concurrent IncrementX traffic.
func (m *Manager) Marshal(fed core.FederationName) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return nil, nil
	}
	out := &rtiv1.MOMManagerState{
		Federation: &rtiv1.MOMFederationRecord{
			Name:            string(st.federation.name),
			FederateHandles: append([]uint64(nil), federateHandlesUint64(st.federation.federateHandles)...),
			FomModuleNames:  append([]string(nil), st.federation.fomModuleNames...),
		},
	}
	handles := make([]core.FederateHandle, 0, len(st.federates))
	for h := range st.federates {
		handles = append(handles, h)
	}
	slices.Sort(handles)
	out.Federates = make([]*rtiv1.MOMFederateRecord, 0, len(handles))
	for _, h := range handles {
		fs := st.federates[h]
		out.Federates = append(out.Federates, &rtiv1.MOMFederateRecord{
			Handle:               uint64(fs.handle),
			Name:                 fs.name,
			FederateType:         fs.federateType,
			TimeRegulating:       fs.timeRegulating,
			TimeConstrained:      fs.timeConstrained,
			Lookahead:            float64(fs.lookahead),
			LogicalTime:          float64(fs.logicalTime),
			InteractionsSent:     atomic.LoadUint32(&fs.interactionsSent),
			InteractionsReceived: atomic.LoadUint32(&fs.interactionsReceived),
			UpdatesSent:          atomic.LoadUint32(&fs.updatesSent),
			ReflectionsReceived:  atomic.LoadUint32(&fs.reflectionsReceived),
		})
	}
	return proto.Marshal(out)
}

// Unmarshal restores the federation's MOM state from wire-format
// bytes. M13 thread C: invoked by the savepoint Manager during
// RequestFederationRestore before the event-log replay runs.
//
// Empty/nil input is a no-op. The manager replaces any pre-existing
// state for fed with the bundled set; the production restore-bootstrap
// path constructs a fresh manager so this is just an idempotent
// rewrite.
func (m *Manager) Unmarshal(fed core.FederationName, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	var pb rtiv1.MOMManagerState
	if err := proto.Unmarshal(data, &pb); err != nil {
		return fmt.Errorf("mom: unmarshal MOMManagerState for %q: %w", fed, err)
	}
	st := newMOMState()
	if pbFed := pb.GetFederation(); pbFed != nil {
		st.federation.name = core.FederationName(pbFed.GetName())
		handles := pbFed.GetFederateHandles()
		st.federation.federateHandles = make([]core.FederateHandle, len(handles))
		for i, h := range handles {
			st.federation.federateHandles[i] = core.FederateHandle(h)
		}
		// Sort defensively in case the bundle was hand-edited.
		slices.SortFunc(st.federation.federateHandles, func(a, b core.FederateHandle) int {
			if a < b {
				return -1
			}
			if a > b {
				return 1
			}
			return 0
		})
		st.federation.fomModuleNames = append([]string(nil), pbFed.GetFomModuleNames()...)
	}
	for _, e := range pb.GetFederates() {
		if e == nil {
			continue
		}
		fs := &federateSnapshot{
			handle:          core.FederateHandle(e.GetHandle()),
			name:            e.GetName(),
			federateType:    e.GetFederateType(),
			timeRegulating:  e.GetTimeRegulating(),
			timeConstrained: e.GetTimeConstrained(),
			lookahead:       core.LogicalTime(e.GetLookahead()),
			logicalTime:     core.LogicalTime(e.GetLogicalTime()),
		}
		atomic.StoreUint32(&fs.interactionsSent, e.GetInteractionsSent())
		atomic.StoreUint32(&fs.interactionsReceived, e.GetInteractionsReceived())
		atomic.StoreUint32(&fs.updatesSent, e.GetUpdatesSent())
		atomic.StoreUint32(&fs.reflectionsReceived, e.GetReflectionsReceived())
		st.federates[fs.handle] = fs
	}
	m.mu.Lock()
	m.fed[fed] = st
	m.mu.Unlock()
	return nil
}

func federateHandlesUint64(in []core.FederateHandle) []uint64 {
	out := make([]uint64, len(in))
	for i, h := range in {
		out[i] = uint64(h)
	}
	return out
}
