package ddm

import (
	"context"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"maps"
	"slices"
)

// SetOnRegionStateChanged installs a composition-time post-commit observer.
// It runs after service locks and admission/rollback guards are released.
func (m *Manager) SetOnRegionStateChanged(changed func(context.Context, core.FederationName)) {
	m.onRegionStateChanged = changed
}

func (m *Manager) notifyRegionStateChanged(ctx context.Context, fed core.FederationName, err *error) {
	if *err == nil && m.onRegionStateChanged != nil {
		m.onRegionStateChanged(ctx, fed)
	}
}

// SetUpdateRateHooks is composition-only. Mutations take subscriptionMu then
// guard before object/ownership leases and m.mu. Hooks run after releasing
// service locks, with guard still held through admission or rollback.
func (m *Manager) SetUpdateRateHooks(changed func(context.Context, core.FederationName, bool) error, interest func(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, []core.AttributeHandle) error, guard func() func()) {
	m.onRateDemandChanged = changed
	m.rateChangeGuard = guard
	if interest != nil {
		m.opts.OnObjectSubscriptionInterest = interest
	}
}

func (m *Manager) finishRateMutation(ctx context.Context, fed core.FederationName, err *error, rollback *func()) {
	defer func() { m.mu.Lock(); m.associationRollback = nil; m.mu.Unlock() }()
	if *err != nil || *rollback == nil || m.onRateDemandChanged == nil {
		return
	}
	if failure := m.onRateDemandChanged(ctx, fed, false); failure != nil {
		(*rollback)()
		*err = failure
	}
}

func (m *Manager) lockRateChange() func() {
	if m.rateChangeGuard != nil {
		return m.rateChangeGuard()
	}
	return func() {}
}

func (m *Manager) regionRateRollbackLocked(fed core.FederationName, st *federationDDMState, regions []RegionHandle) func() {
	committed, pending := map[RegionHandle]map[DimensionHandle]Range{}, map[RegionHandle]map[DimensionHandle]Range{}
	templates := map[RegionHandle]bool{}
	for _, h := range regions {
		templates[h] = st.regions[h].template
		committed[h] = maps.Clone(st.regions[h].committed)
		pending[h] = maps.Clone(st.regions[h].pending)
	}
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.fed[fed] != st {
			return
		}
		for h, values := range committed {
			if rs := st.regions[h]; rs != nil {
				rs.committed = values
				rs.pending = pending[h]
				rs.template = templates[h]
			}
		}
	}
}

func (m *Manager) associationRateRollbackLocked(fed core.FederationName, st *federationDDMState, obj core.ObjectHandle) func() {
	inactive := cloneInactiveAssociations(st.inactivePubs[obj])
	var previous map[core.AttributeHandle][]RegionHandle
	if entries, ok := st.objPubs[obj]; ok {
		previous = map[core.AttributeHandle][]RegionHandle{}
		for attr, regions := range entries {
			previous[attr] = slices.Clone(regions)
		}
	}
	shadow := &federationDDMState{
		regions:      st.regions,
		objPubs:      map[core.ObjectHandle]map[core.AttributeHandle][]RegionHandle{},
		inactivePubs: map[core.ObjectHandle]map[core.FederateHandle]map[core.AttributeHandle][]RegionHandle{},
	}
	if previous != nil {
		shadow.objPubs[obj] = previous
	}
	if len(inactive) != 0 {
		shadow.inactivePubs[obj] = inactive
	}
	projection := &associationRollbackProjection{fed: fed, object: obj, origin: st, shadow: shadow}
	m.associationRollback = projection
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		// Restoring shares the shadow maps with live state; retire their projection atomically.
		if m.associationRollback == projection {
			m.associationRollback = nil
		}
		if m.fed[fed] != st {
			return
		}
		if shadow.objPubs[obj] == nil {
			delete(st.objPubs, obj)
		} else {
			st.objPubs[obj] = shadow.objPubs[obj]
		}
		if len(shadow.inactivePubs[obj]) == 0 {
			delete(st.inactivePubs, obj)
		} else {
			st.inactivePubs[obj] = shadow.inactivePubs[obj]
		}
	}
}

// OnFederateResign always removes membership-owned DDM state. Its notification
// is mandatory: a full publisher queue cannot restore a resigned participant.
func (m *Manager) OnFederateResign(fed core.FederationName, h core.FederateHandle) {
	defer func() {
		if m.onRegionStateChanged != nil {
			m.onRegionStateChanged(context.Background(), fed)
		}
	}()
	m.subscriptionMu.Lock()
	defer m.subscriptionMu.Unlock()
	releaseRateGuard := m.lockRateChange()
	defer releaseRateGuard()
	m.mu.Lock()
	st := m.fed[fed]
	if st != nil {
		st.removeFederateState(h)
	}
	m.mu.Unlock()
	if m.onRateDemandChanged != nil {
		_ = m.onRateDemandChanged(context.Background(), fed, true)
	}
}

func (st *federationDDMState) removeFederateState(h core.FederateHandle) {
	st.releaseConveyedReceiver(h)
	for region, state := range st.regions {
		if state.owner == h {
			delete(st.regions, region)
			st.removeInactiveRegion(region)
		}
	}
	for obj := range st.inactivePubs {
		delete(st.inactivePubs[obj], h)
		st.pruneInactive(obj)
	}
	keep := func(regions []RegionHandle) []RegionHandle {
		return slices.DeleteFunc(regions, func(region RegionHandle) bool { _, ok := st.regions[region]; return !ok })
	}
	st.removeFederateSubscriptions(h, keep)
	for obj, attrs := range st.objPubs {
		for attr, regions := range attrs {
			regions = keep(regions)
			if len(regions) == 0 {
				delete(attrs, attr)
			} else {
				attrs[attr] = regions
			}
		}
		if len(attrs) == 0 {
			delete(st.objPubs, obj)
		}
	}
}

func (st *federationDDMState) removeFederateSubscriptions(h core.FederateHandle, keep func([]RegionHandle) []RegionHandle) {
	for key, rows := range st.objSubs {
		var next []subscription
		for _, row := range rows {
			if row.subscriber != h {
				row.regions = keep(row.regions)
				if len(row.regions) > 0 {
					next = append(next, row)
				}
			}
		}
		if len(next) == 0 {
			delete(st.objSubs, key)
		} else {
			st.objSubs[key] = next
		}
	}
	for cls, rows := range st.intSubs {
		var next []interactionSubscription
		for _, row := range rows {
			if row.subscriber != h {
				row.regions = keep(row.regions)
				if len(row.regions) > 0 {
					next = append(next, row)
				}
			}
		}
		if len(next) == 0 {
			delete(st.intSubs, cls)
		} else {
			st.intSubs[cls] = next
		}
	}
}
