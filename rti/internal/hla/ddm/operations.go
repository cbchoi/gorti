package ddm

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var _ core.RegionRouting = (*Manager)(nil)

func (m *Manager) objectContext(ctx context.Context, fed core.FederationName, cls core.ObjectClassHandle, attrs []core.AttributeHandle) (map[core.AttributeHandle][]DimensionHandle, error) {
	h, err := m.opts.FOMs.Get(ctx, fed)
	if err != nil {
		return nil, err
	}
	if names, ok := h.(interface {
		ObjectClassName(core.ObjectClassHandle) (string, bool)
	}); ok {
		if _, found := names.ObjectClassName(cls); !found {
			return nil, core.ErrObjectClassNotFound
		}
	}
	required := make(map[core.AttributeHandle][]DimensionHandle, len(attrs))
	dims, strict := h.(core.FOMAttributeDimensions)
	names, named := h.(interface {
		AttributeName(core.ObjectClassHandle, core.AttributeHandle) (string, bool)
	})
	for _, attr := range attrs {
		if attr == 0 {
			return nil, core.ErrAttributeNotFound
		}
		if named {
			if _, found := names.AttributeName(cls, attr); !found {
				return nil, core.ErrAttributeNotFound
			}
		}
		if strict {
			values, found := dims.AttributeDimensions(cls, attr)
			if !found {
				return nil, core.ErrAttributeNotFound
			}
			required[attr] = dimensionSet(values)
		}
	}
	return required, nil
}

func (m *Manager) interactionContext(ctx context.Context, fed core.FederationName, cls core.InteractionClassHandle) ([]DimensionHandle, bool, error) {
	h, err := m.opts.FOMs.Get(ctx, fed)
	if err != nil {
		return nil, false, err
	}
	if names, ok := h.(interface {
		InteractionClassName(core.InteractionClassHandle) (string, bool)
	}); ok {
		if _, found := names.InteractionClassName(cls); !found {
			return nil, false, core.ErrInteractionClassNotFound
		}
	}
	if dims, ok := h.(core.FOMInteractionDimensions); ok {
		values, found := dims.InteractionDimensions(cls)
		if !found {
			return nil, true, core.ErrInteractionClassNotFound
		}
		return dimensionSet(values), true, nil
	}
	return nil, false, nil
}

func validateRegions(st *federationDDMState, caller core.FederateHandle, regions []RegionHandle, dimensions []DimensionHandle, strict bool) error {
	for _, region := range regions {
		rs, found := st.regions[region]
		if !found {
			return core.ErrRegionNotFound
		}
		if rs.owner != caller {
			return core.ErrRegionNotOwnedByFederate
		}
		if rs.template {
			return core.ErrRegionNotFound
		}
		if strict && (len(dimensions) == 0 || !dimensionsSubset(rs.dims, dimensions)) {
			return core.ErrInvalidRegionContext
		}
	}
	return nil
}

// IEEE 1516.1-2010 9.1.3: a region may constrain only some of the
// dimensions available to an attribute or interaction, but no other ones.
func dimensionsSubset(region, available []DimensionHandle) bool {
	for _, dim := range region {
		if !slices.Contains(available, dim) {
			return false
		}
	}
	return true
}

func validateObjectRegions(st *federationDDMState, caller core.FederateHandle, attrs []core.AttributeHandle, regions []RegionHandle, required map[core.AttributeHandle][]DimensionHandle) error {
	if err := validateRegions(st, caller, regions, nil, false); err != nil {
		return err
	}
	for _, attr := range attrs {
		dims, strict := required[attr]
		if err := validateRegions(st, caller, regions, dims, strict); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) InteractionSubscribersWithRegions(ctx context.Context, fed core.FederationName, caller core.FederateHandle, cls core.InteractionClassHandle, regions []RegionHandle) ([]core.FederateHandle, error) {
	dims, strict, err := m.interactionContext(ctx, fed, cls)
	if err != nil {
		return nil, err
	}
	if len(regions) == 0 {
		return nil, core.ErrInvalidRegionContext
	}
	h, err := m.opts.FOMs.Get(ctx, fed)
	if err != nil {
		return nil, err
	}
	names, named := h.(interface {
		InteractionClassName(core.InteractionClassHandle) (string, bool)
	})
	var sentName string
	if named {
		sentName, _ = names.InteractionClassName(cls)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := m.fed[fed]
	if st == nil {
		return nil, core.ErrRegionNotFound
	}
	if err := validateRegions(st, caller, regions, dims, strict); err != nil {
		return nil, err
	}
	pub := st.realize(regions, dims, strict)
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
	return slices.Compact(result), nil
}

func (m *Manager) PrepareRegionUpdateRequest(ctx context.Context, fed core.FederationName, caller core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle, regions []RegionHandle) (core.RegionUpdateRequest, error) {
	required, err := m.objectContext(ctx, fed, cls, attrs)
	if err != nil {
		return nil, err
	}
	if len(regions) == 0 {
		return nil, core.ErrInvalidRegionContext
	}
	m.mu.RLock()
	st := m.fed[fed]
	if st == nil {
		m.mu.RUnlock()
		return nil, core.ErrRegionNotFound
	}
	if err := validateObjectRegions(st, caller, attrs, regions, required); err != nil {
		m.mu.RUnlock()
		return nil, err
	}
	queries := make(map[core.AttributeHandle][]regionBounds, len(attrs))
	for _, attr := range attrs {
		dims, strict := required[attr]
		queries[attr] = st.realize(regions, dims, strict)
	}
	return &regionUpdateRequest{state: st, cls: cls, queries: queries, release: sync.OnceFunc(m.mu.RUnlock)}, nil
}

type regionUpdateRequest struct {
	state   *federationDDMState
	cls     core.ObjectClassHandle
	queries map[core.AttributeHandle][]regionBounds
	release func()
}

func (r *regionUpdateRequest) Release() { r.release() }
func (r *regionUpdateRequest) Matches(obj core.ObjectHandle, attr core.AttributeHandle) bool {
	regions := r.state.objPubs[obj][attr]
	// No explicit association uses the existing whole-space default policy.
	query := r.queries[attr]
	if len(regions) == 0 {
		return overlapsDefault(query)
	}
	return regionsOverlap(query, r.state.objectRealizations(r.cls, attr, regions))
}
