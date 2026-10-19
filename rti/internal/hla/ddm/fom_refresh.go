package ddm

import (
	"context"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// Live extensions preserve every existing dimension binding and range. Only
// the declaration tables are refreshed; region state is never reconstructed.
func (m *Manager) refreshFOMDimensions(ctx context.Context, fed core.FederationName, st *federationDDMState) {
	h, err := m.opts.FOMs.Get(ctx, fed)
	if err != nil {
		return
	}
	st.fom = h
	provider, ok := h.(interface {
		FOMRevision() uint64
		View() core.FOMView
	})
	if !ok || provider.FOMRevision() == st.fomRevision {
		return
	}
	view := provider.View()
	if enumerator, ok := h.(DimensionEnumerator); ok {
		byName := make(map[string]model.Dimension)
		for _, dimension := range enumerator.Dimensions() {
			byName[dimension.Name] = dimension
		}
		st.dimDefaults = make(map[DimensionHandle]*Range, len(view.Dimensions))
		for _, dimension := range view.Dimensions {
			st.setDimensionDefault(DimensionHandle(dimension.Handle), byName[dimension.Name])
		}
	}
	const space = RoutingSpaceHandle(1)
	names := make(map[string]DimensionHandle, len(view.Dimensions))
	bounds := make(map[DimensionHandle]uint64, len(view.Dimensions))
	var highest DimensionHandle
	for _, dimension := range view.Dimensions {
		handle := DimensionHandle(dimension.Handle)
		names[dimension.Name] = handle
		upper := dimension.UpperBound
		if upper == 0 {
			upper = ^uint64(0)
		}
		bounds[handle] = upper
		if handle > highest {
			highest = handle
		}
	}
	st.routingSpaces[DefaultRoutingSpace] = space
	st.dimensions[space] = names
	st.dimUpperBound = bounds
	st.nextDimHandle = highest
	st.permissive = false
	st.fomRevision = view.Revision
}
