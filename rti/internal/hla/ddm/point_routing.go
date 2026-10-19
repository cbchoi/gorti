package ddm

import (
	"context"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// SubscribersForObjectAttributePoint projects an RTI-supplied point onto
// committed receiver realizations. It selects only regional subscriptions;
// callers retain the separate universal ordinary-subscription union.
func (m *Manager) SubscribersForObjectAttributePoint(ctx context.Context, fed core.FederationName, cls core.ObjectClassHandle, attr core.AttributeHandle, dimension DimensionHandle, point uint64) ([]core.FederateHandle, error) {
	if _, err := m.opts.FOMs.Get(ctx, fed); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(ctx, fed)
	if dimensions, strict := st.fom.(core.FOMAttributeDimensions); strict {
		available, found := dimensions.AttributeDimensions(cls, attr)
		if !found {
			return nil, core.ErrAttributeNotFound
		}
		if !slices.Contains(available, dimension) {
			return nil, core.ErrInvalidRegionContext
		}
	}
	if upper, found := st.dimUpperBound[dimension]; !found {
		return nil, core.ErrDimensionNotFound
	} else if point >= upper {
		return nil, nil
	}
	var result []core.FederateHandle
	for _, row := range st.objSubs[objAttrKey{cls: cls, attr: attr}] {
		if st.subscriptionContainsPoint(row, cls, attr, dimension, point) {
			result = append(result, row.subscriber)
		}
	}
	slices.Sort(result)
	return slices.Compact(result), nil
}

func (st *federationDDMState) subscriptionContainsPoint(row subscription, cls core.ObjectClassHandle, attr core.AttributeHandle, dimension DimensionHandle, point uint64) bool {
	for _, handle := range row.regions {
		region := st.regions[handle]
		if region == nil || region.template || region.owner != row.subscriber {
			continue
		}
		realizations := st.objectRealizations(cls, attr, []RegionHandle{handle})
		if !overlapsDefault(realizations) || len(realizations) != 1 {
			continue
		}
		bounds, present := realizations[0][dimension]
		if present && bounds.Lower <= point && point < bounds.Upper {
			return true
		}
	}
	return false
}
