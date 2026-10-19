package declaration

import (
	"context"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"maps"
	"slices"
)

func setRegionalObjectInterest(st *federationState, sub core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle) {
	for key, members := range st.regionalObjects {
		if key.cls == cls {
			delete(members, sub)
			if len(members) == 0 {
				delete(st.regionalObjects, key)
			}
		}
	}
	for _, attr := range attrs {
		key := objAttrKey{cls, attr}
		if st.regionalObjects[key] == nil {
			st.regionalObjects[key] = map[core.FederateHandle]struct{}{}
		}
		st.regionalObjects[key][sub] = struct{}{}
	}
}

func (m *Manager) SetRegionalObjectInterestAndUpdateRates(ctx context.Context, fed core.FederationName, sub core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle, rates []core.RegionalUpdateRateSubscription) error {
	for _, e := range rates {
		if err := m.ValidateSubscriptionOptions(ctx, fed, e.Options); err != nil {
			return err
		}
	}
	return m.change(ctx, fed, func(st *federationState) {
		setRegionalObjectInterest(st, sub, cls, attrs)
		st.regionalRates = slices.Clone(rates)
	})
}

func (m *Manager) SyncRegionalUpdateRateSubscriptions(ctx context.Context, fed core.FederationName, rates []core.RegionalUpdateRateSubscription) error {
	return m.changeMandatory(ctx, fed, func(st *federationState) { st.regionalRates = slices.Clone(rates) })
}

func (m *Manager) SyncObjectUpdateRateState(ctx context.Context, fed core.FederationName, obj core.ObjectHandle, cls core.ObjectClassHandle, owners map[core.AttributeHandle]core.FederateHandle, rates []core.RegionalUpdateRateSubscription) error {
	return m.changeMandatory(ctx, fed, func(st *federationState) {
		st.rateObjects[obj] = rateObject{class: cls, owners: maps.Clone(owners)}
		st.regionalRates = slices.Clone(rates)
	})
}
