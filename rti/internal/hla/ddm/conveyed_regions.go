package ddm

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func validateRealizationLocked(st *federationDDMState, snapshot core.ConveyedRegionCopy) error {
	if snapshot.Designator == 0 || snapshot.Designator == snapshot.Realization.SourceRegion() || snapshot.Realization.SourceRegion() == 0 {
		return core.ErrRegionNotFound
	}
	if st.regions[snapshot.Designator] != nil {
		return fmt.Errorf("conveyed region: designator collides with specification")
	}
	for _, dimension := range snapshot.Realization.DimensionRanges() {
		upper, found := st.dimUpperBound[dimension.Dimension]
		if !st.permissive && (!found || dimension.Bounds.Upper > upper) {
			return core.ErrInvalidRangeBounds
		}
	}
	if prior, found := st.conveyed[snapshot.Designator]; found && (prior.SourceRegion() != snapshot.Realization.SourceRegion() || !slices.Equal(prior.DimensionRanges(), snapshot.Realization.DimensionRanges())) {
		return fmt.Errorf("conveyed region: realization identity collision")
	}
	return nil
}

func validateRealizationsLocked(st *federationDDMState, copies []core.ConveyedRegionCopy) error {
	seen := make(map[RegionHandle]core.RegionRealizationSnapshot, len(copies))
	for _, snapshot := range copies {
		if err := validateRealizationLocked(st, snapshot); err != nil {
			return err
		}
		if prior, found := seen[snapshot.Designator]; found && (prior.SourceRegion() != snapshot.Realization.SourceRegion() || !slices.Equal(prior.DimensionRanges(), snapshot.Realization.DimensionRanges())) {
			return fmt.Errorf("conveyed region: conflicting batch designator")
		}
		seen[snapshot.Designator] = snapshot.Realization
	}
	return nil
}

func cacheRealizationsLocked(st *federationDDMState, copies []core.ConveyedRegionCopy) {
	if st.conveyed == nil {
		st.conveyed = make(map[RegionHandle]core.RegionRealizationSnapshot)
	}
	for _, snapshot := range copies {
		st.conveyed[snapshot.Designator] = snapshot.Realization
		if snapshot.Designator > st.nextRegionHandle {
			st.nextRegionHandle = snapshot.Designator
		}
	}
}

func captureRealizationsLocked(st *federationDDMState, handles []RegionHandle, realize func([]RegionHandle) []regionBounds) ([]core.ConveyedRegionCopy, error) {
	if len(handles) == 0 {
		return nil, nil
	}
	handles = slices.Clone(handles)
	slices.Sort(handles)
	handles = slices.Compact(handles)
	if uint64(len(handles)) > ^uint64(0)-uint64(st.nextRegionHandle) {
		return nil, fmt.Errorf("conveyed region: handle namespace exhausted")
	}
	copies := make([]core.ConveyedRegionCopy, 0, len(handles))
	for _, handle := range handles {
		bounds := realize([]RegionHandle{handle})
		if len(bounds) != 1 {
			return nil, core.ErrRegionNotFound
		}
		var dimensions []core.RegionDimensionRange
		for dimension, value := range bounds[0] {
			dimensions = append(dimensions, core.RegionDimensionRange{Dimension: dimension, Bounds: value})
		}
		snapshot, err := core.NewRegionRealizationSnapshot(handle, dimensions)
		if err != nil {
			return nil, err
		}
		conveyed := core.ConveyedRegionCopy{Designator: st.nextRegionHandle + RegionHandle(len(copies)) + 1, Realization: snapshot}
		copies = append(copies, conveyed)
	}
	st.nextRegionHandle += RegionHandle(len(copies))
	return copies, nil
}

// Routing and snapshot creation share a single manager boundary; no later region
// query can substitute a different committed realization into the callback.
func (m *Manager) InteractionDeliverySnapshot(ctx context.Context, fed core.FederationName, sender core.FederateHandle, cls core.InteractionClassHandle, regions []RegionHandle) ([]core.FederateHandle, []core.ConveyedRegionCopy, bool, error) {
	if len(regions) == 0 {
		return nil, nil, false, core.ErrInvalidRegionContext
	}
	_, err := m.opts.FOMs.Get(ctx, fed)
	if err != nil {
		return nil, nil, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.fed[fed]
	if st == nil {
		return nil, nil, false, core.ErrRegionNotFound
	}
	m.stateForLocked(ctx, fed)
	fom := st.fom
	if names, ok := fom.(interface {
		InteractionClassName(core.InteractionClassHandle) (string, bool)
	}); ok {
		if _, found := names.InteractionClassName(cls); !found {
			return nil, nil, false, core.ErrInteractionClassNotFound
		}
	}
	var dims []DimensionHandle
	dimensions, strict := fom.(core.FOMInteractionDimensions)
	if strict {
		values, found := dimensions.InteractionDimensions(cls)
		if !found {
			return nil, nil, false, core.ErrInteractionClassNotFound
		}
		dims = dimensionSet(values)
	}
	if err := validateRegions(st, sender, regions, dims, strict); err != nil {
		return nil, nil, false, err
	}
	copies, err := captureRealizationsLocked(st, regions, func(handles []RegionHandle) []regionBounds { return st.realize(handles, dims, strict) })
	if err != nil {
		return nil, nil, false, err
	}
	pub := portableRealizations(copies)
	return interactionSnapshotSubscribersLocked(st, fom, cls, pub), copies, overlapsDefault(pub), nil
}

func interactionSnapshotSubscribersLocked(st *federationDDMState, fom core.FOMHandle, cls core.InteractionClassHandle, pub []regionBounds) []core.FederateHandle {
	names, named := fom.(interface {
		InteractionClassName(core.InteractionClassHandle) (string, bool)
	})
	var sentName string
	if named {
		sentName, _ = names.InteractionClassName(cls)
	}
	var result []core.FederateHandle
	for subscribed, rows := range st.intSubs {
		matches := subscribed == cls
		if !matches && named {
			name, found := names.InteractionClassName(subscribed)
			matches = found && strings.HasPrefix(sentName, name+".")
		}
		if !matches {
			continue
		}
		for _, row := range rows {
			if regionsOverlap(pub, st.interactionRealizations(subscribed, row.regions)) {
				result = append(result, row.subscriber)
			}
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}

func (m *Manager) ObjectDeliverySnapshots(ctx context.Context, fed core.FederationName, obj core.ObjectHandle, registered core.ObjectClassHandle, attrs []core.AttributeHandle, known map[core.FederateHandle]core.ObjectClassHandle) (map[core.AttributeHandle]core.AttributeRegionDeliverySnapshot, error) {
	_, err := m.opts.FOMs.Get(ctx, fed)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(ctx, fed)
	fom := st.fom
	names, _ := fom.(interface {
		AttributeName(core.ObjectClassHandle, core.AttributeHandle) (string, bool)
	})
	result := make(map[core.AttributeHandle]core.AttributeRegionDeliverySnapshot, len(attrs))
	for _, attr := range attrs {
		regions := slices.Clone(st.objPubs[obj][attr])
		copies, err := captureRealizationsLocked(st, regions, func(handles []RegionHandle) []regionBounds { return st.objectRealizations(registered, attr, handles) })
		if err != nil {
			return nil, err
		}
		pub := portableRealizations(copies)
		row := core.AttributeRegionDeliverySnapshot{Regions: regions, Copies: copies, OverlapsDefault: len(regions) == 0 || overlapsDefault(pub)}
		for recipient, cls := range known {
			mapped := attr
			if cls != registered {
				if names == nil {
					continue
				}
				name, found := names.AttributeName(registered, attr)
				if !found {
					continue
				}
				var foundMapping bool
				mapped, foundMapping = fom.LookupAttribute(cls, name)
				if !foundMapping {
					continue
				}
			}
			for _, subscription := range st.objSubs[objAttrKey{cls: cls, attr: mapped}] {
				sub := st.objectRealizations(cls, mapped, subscription.regions)
				overlap := regionsOverlap(pub, sub)
				if len(regions) == 0 {
					overlap = overlapsDefault(sub)
				}
				if subscription.subscriber == recipient && overlap {
					row.Subscribers = append(row.Subscribers, recipient)
					break
				}
			}
		}
		slices.Sort(row.Subscribers)
		result[attr] = row
	}
	return result, nil
}

// Restored portable callback state reserves designators without caching values.
// Query state is rehydrated only when a receiver lease is prepared.
func (m *Manager) RestoreConveyedRegions(ctx context.Context, fed core.FederationName, copies []core.ConveyedRegionCopy) error {
	if len(copies) == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(ctx, fed)
	if err := validateRealizationsLocked(st, copies); err != nil {
		return err
	}
	for _, snapshot := range copies {
		if snapshot.Designator > st.nextRegionHandle {
			st.nextRegionHandle = snapshot.Designator
		}
	}
	return nil
}

func (m *Manager) InteractionSubscribersForConveyedRegions(ctx context.Context, fed core.FederationName, cls core.InteractionClassHandle, copies []core.ConveyedRegionCopy) ([]core.FederateHandle, error) {
	_, err := m.opts.FOMs.Get(ctx, fed)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.fed[fed]
	if st == nil {
		return nil, core.ErrRegionNotFound
	}
	m.stateForLocked(ctx, fed)
	return interactionSnapshotSubscribersLocked(st, st.fom, cls, portableRealizations(copies)), nil
}

func portableRealizations(copies []core.ConveyedRegionCopy) []regionBounds {
	result := make([]regionBounds, 0, len(copies))
	for _, snapshot := range copies {
		bounds := make(regionBounds)
		for _, dimension := range snapshot.Realization.DimensionRanges() {
			bounds[dimension.Dimension] = dimension.Bounds
		}
		result = append(result, bounds)
	}
	return result
}

func (m *Manager) SubscribersForConveyedUpdate(ctx context.Context, fed core.FederationName, cls core.ObjectClassHandle, attr core.AttributeHandle, copies []core.ConveyedRegionCopy) ([]core.FederateHandle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(ctx, fed)
	pub := portableRealizations(copies)
	var result []core.FederateHandle
	for _, row := range st.objSubs[objAttrKey{cls: cls, attr: attr}] {
		if regionsOverlap(pub, st.objectRealizations(cls, attr, row.regions)) {
			result = append(result, row.subscriber)
		}
	}
	slices.Sort(result)
	return slices.Compact(result), nil
}

func (m *Manager) ConveyedRegionDimensions(fed core.FederationName, handle RegionHandle) ([]DimensionHandle, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := m.fed[fed]
	if st == nil {
		return nil, false
	}
	snapshot, found := st.conveyed[handle]
	if !found {
		return nil, false
	}
	var result []DimensionHandle
	for _, dimension := range snapshot.DimensionRanges() {
		result = append(result, dimension.Dimension)
	}
	return result, true
}
