package ddm

import (
	"context"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ConveyCopies prepares receiver-specific query leases before queue admission.
// Queued callbacks retain portable values independently of this query cache.
func (m *Manager) ConveyCopies(ctx context.Context, fed core.FederationName, receiver core.FederateHandle, copies []core.ConveyedRegionCopy) error {
	if receiver == 0 {
		return core.ErrRegionNotFound
	}
	if len(copies) == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(ctx, fed)
	if err := validateRealizationsLocked(st, copies); err != nil {
		return err
	}
	cacheRealizationsLocked(st, copies)
	if st.conveyedRecipients == nil {
		st.conveyedRecipients = make(map[RegionHandle]map[core.FederateHandle]bool)
	}
	for _, snapshot := range copies {
		if st.conveyedRecipients[snapshot.Designator] == nil {
			st.conveyedRecipients[snapshot.Designator] = make(map[core.FederateHandle]bool)
		}
		if _, prepared := st.conveyedRecipients[snapshot.Designator][receiver]; !prepared {
			st.conveyedRecipients[snapshot.Designator][receiver] = false
		}
	}
	return nil
}

// EnterConveyedRegions makes an already prepared batch queryable atomically.
func (m *Manager) EnterConveyedRegions(_ context.Context, fed core.FederationName, receiver core.FederateHandle, handles []RegionHandle) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.fed[fed]
	if st == nil {
		return core.ErrRegionNotFound
	}
	for _, handle := range handles {
		if _, prepared := st.conveyedRecipients[handle][receiver]; !prepared {
			return core.ErrRegionNotFound
		}
	}
	for _, handle := range handles {
		st.conveyedRecipients[handle][receiver] = true
	}
	return nil
}

// ReleaseConveyedRegions cannot release another receiver's leases. Duplicate
// completion receipts and cancellation after completion are harmless.
func (m *Manager) ReleaseConveyedRegions(_ context.Context, fed core.FederationName, receiver core.FederateHandle, handles []RegionHandle) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st := m.fed[fed]; st != nil {
		for _, handle := range handles {
			delete(st.conveyedRecipients[handle], receiver)
			st.pruneConveyedRegion(handle)
		}
	}
	return nil
}

func (st *federationDDMState) pruneConveyedRegion(handle RegionHandle) {
	if len(st.conveyedRecipients[handle]) == 0 {
		delete(st.conveyedRecipients, handle)
		delete(st.conveyed, handle)
	}
}

func (st *federationDDMState) releaseConveyedReceiver(receiver core.FederateHandle) {
	for handle, recipients := range st.conveyedRecipients {
		delete(recipients, receiver)
		st.pruneConveyedRegion(handle)
	}
}

func (m *Manager) QueryBoundsFor(fed core.FederationName, receiver core.FederateHandle, handle RegionHandle, dimension DimensionHandle) (Range, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := m.fed[fed]
	if st == nil {
		return Range{}, core.ErrRegionNotFound
	}
	if snapshot, found := st.conveyed[handle]; found {
		if !st.conveyedRecipients[handle][receiver] {
			return Range{}, core.ErrRegionNotFound
		}
		if bounds, found := snapshot.RangeBounds(dimension); found {
			return bounds, nil
		}
		return Range{}, core.ErrDimensionNotFound
	}
	region := st.regions[handle]
	if region == nil || region.template {
		return Range{}, core.ErrRegionNotFound
	}
	if region.owner != receiver {
		return Range{}, core.ErrRegionNotOwnedByFederate
	}
	if bounds, found := region.committed[dimension]; found {
		return bounds, nil
	}
	return Range{}, core.ErrDimensionNotFound
}

func (m *Manager) RegionDimensionsFor(fed core.FederationName, receiver core.FederateHandle, handle RegionHandle) ([]DimensionHandle, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := m.fed[fed]
	if st == nil {
		return nil, core.ErrRegionNotFound
	}
	if snapshot, found := st.conveyed[handle]; found {
		if !st.conveyedRecipients[handle][receiver] {
			return nil, core.ErrRegionNotFound
		}
		result := make([]DimensionHandle, 0, len(snapshot.DimensionRanges()))
		for _, dimension := range snapshot.DimensionRanges() {
			result = append(result, dimension.Dimension)
		}
		return result, nil
	}
	if region := st.regions[handle]; region != nil {
		if region.owner != receiver {
			return nil, core.ErrRegionNotOwnedByFederate
		}
		return slices.Clone(region.dims), nil
	}
	return nil, core.ErrRegionNotFound
}
