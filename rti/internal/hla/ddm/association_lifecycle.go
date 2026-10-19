package ddm

import (
	"context"
	"fmt"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type knownRegionAssociationPreparer interface {
	PrepareKnownRegionAssociation(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (core.ObjectClassHandle, []core.AttributeHandle, map[core.AttributeHandle]core.FederateHandle, func(), error)
}

// Ownership may commit while rate admission runs. Project those commits into
// the saved state so rollback cannot reverse ownership effects. subscriptionMu
// permits one admission at a time; Manager.mu protects this shadow state.
type associationRollbackProjection struct {
	fed    core.FederationName
	object core.ObjectHandle
	origin *federationDDMState
	shadow *federationDDMState
}

func (m *Manager) rollbackStateLocked(fed core.FederationName, obj core.ObjectHandle, st *federationDDMState) *federationDDMState {
	if p := m.associationRollback; p != nil && p.fed == fed && p.object == obj && p.origin == st {
		return p.shadow
	}
	return nil
}

func (m *Manager) prepareAssociationGroups(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, groups []core.AttributeRegionGroup, remove bool) ([]core.AttributeRegionGroup, map[core.AttributeHandle]core.FederateHandle, map[core.AttributeHandle][]DimensionHandle, func(), error) {
	attrs := groupAttributes(groups)
	prepare := m.opts.PrepareKnownAssociation
	if prepare == nil {
		if objects, ok := m.opts.Objects.(knownRegionAssociationPreparer); ok {
			prepare = objects.PrepareKnownRegionAssociation
		}
	}
	if prepare != nil {
		cls, actual, owners, release, err := prepare(ctx, fed, caller, obj, attrs)
		if err != nil {
			if release != nil {
				release()
			}
			return nil, nil, nil, nil, err
		}
		if release == nil {
			return nil, nil, nil, nil, fmt.Errorf("ddm: known association preparation returned no release")
		}
		fail := func(err error) ([]core.AttributeRegionGroup, map[core.AttributeHandle]core.FederateHandle, map[core.AttributeHandle][]DimensionHandle, func(), error) {
			release()
			return nil, nil, nil, nil, err
		}
		result, err := mapAssociationGroups(groups, attrs, actual)
		if err != nil {
			return fail(err)
		}
		required, err := m.objectContext(ctx, fed, cls, actual)
		if err != nil {
			return fail(err)
		}
		return result, owners, required, release, nil
	}
	if m.opts.Objects == nil {
		return groups, nil, nil, func() {}, nil
	}
	var owned []core.AttributeHandle
	if !remove {
		owned = associatedAttributes(groups)
	}
	cls, release, err := m.opts.Objects.PrepareRegionAssociation(ctx, fed, caller, obj, owned)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if release == nil {
		return nil, nil, nil, nil, fmt.Errorf("ddm: association validation returned no release")
	}
	required, err := m.objectContext(ctx, fed, cls, attrs)
	if err != nil {
		release()
		return nil, nil, nil, nil, err
	}
	return groups, nil, required, release, nil
}

func associatedAttributes(groups []core.AttributeRegionGroup) []core.AttributeHandle {
	var attrs []core.AttributeHandle
	for _, group := range groups {
		if len(group.Regions) != 0 {
			attrs = append(attrs, group.Attributes...)
		}
	}
	slices.Sort(attrs)
	return slices.Compact(attrs)
}

func mapAssociationGroups(groups []core.AttributeRegionGroup, attrs, actual []core.AttributeHandle) ([]core.AttributeRegionGroup, error) {
	if len(attrs) != len(actual) {
		return nil, fmt.Errorf("ddm: known association mapping length mismatch")
	}
	mapped, inverse := map[core.AttributeHandle]core.AttributeHandle{}, map[core.AttributeHandle]core.AttributeHandle{}
	for i, attr := range attrs {
		if actual[i] == 0 {
			return nil, fmt.Errorf("ddm: invalid mapped attribute")
		}
		if previous, found := inverse[actual[i]]; found && previous != attr {
			return nil, fmt.Errorf("ddm: ambiguous known association mapping")
		}
		mapped[attr], inverse[actual[i]] = actual[i], attr
	}
	result := make([]core.AttributeRegionGroup, len(groups))
	for i, group := range groups {
		result[i].Regions = slices.Clone(group.Regions)
		for _, attr := range group.Attributes {
			result[i].Attributes = append(result[i].Attributes, mapped[attr])
		}
	}
	return result, nil
}

func (st *federationDDMState) inactiveFor(obj core.ObjectHandle, caller core.FederateHandle, create bool) map[core.AttributeHandle][]RegionHandle {
	rows := st.inactivePubs[obj]
	if rows == nil && create {
		rows = map[core.FederateHandle]map[core.AttributeHandle][]RegionHandle{}
		st.inactivePubs[obj] = rows
	}
	per := rows[caller]
	if per == nil && create {
		per = map[core.AttributeHandle][]RegionHandle{}
		rows[caller] = per
	}
	return per
}

func (st *federationDDMState) pruneInactive(obj core.ObjectHandle) {
	for caller, per := range st.inactivePubs[obj] {
		if len(per) == 0 {
			delete(st.inactivePubs[obj], caller)
		}
	}
	if len(st.inactivePubs[obj]) == 0 {
		delete(st.inactivePubs, obj)
	}
}

func cloneInactiveAssociations(rows map[core.FederateHandle]map[core.AttributeHandle][]RegionHandle) map[core.FederateHandle]map[core.AttributeHandle][]RegionHandle {
	result := make(map[core.FederateHandle]map[core.AttributeHandle][]RegionHandle, len(rows))
	for caller, attrs := range rows {
		result[caller] = map[core.AttributeHandle][]RegionHandle{}
		for attr, regions := range attrs {
			result[caller][attr] = slices.Clone(regions)
		}
	}
	return result
}

func (st *federationDDMState) removeInactiveRegion(region RegionHandle) {
	for obj, callers := range st.inactivePubs {
		for _, attrs := range callers {
			for attr, regions := range attrs {
				remaining := remainingRegions(regions, []RegionHandle{region})
				if len(remaining) == 0 {
					delete(attrs, attr)
				} else {
					attrs[attr] = remaining
				}
			}
		}
		st.pruneInactive(obj)
	}
}

// OwnershipChanged is called while object knowledge remains pinned, after the
// ownership lock is released. It never calls back into object or ownership.
func (m *Manager) OwnershipChanged(fed core.FederationName, obj core.ObjectHandle, before, after map[core.AttributeHandle]core.FederateHandle) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.fed[fed]
	if st == nil {
		return
	}
	st.applyOwnershipChange(obj, before, after)
	if shadow := m.rollbackStateLocked(fed, obj, st); shadow != nil {
		shadow.applyOwnershipChange(obj, before, after)
	}
}

func (st *federationDDMState) applyOwnershipChange(obj core.ObjectHandle, before, after map[core.AttributeHandle]core.FederateHandle) {
	for attr, previous := range before {
		next := after[attr]
		if previous == next {
			continue
		}
		delete(st.objPubs[obj], attr)
		delete(st.inactiveFor(obj, previous, false), attr)
		if next != 0 {
			pending := st.inactiveFor(obj, next, false)
			if regions := pending[attr]; len(regions) > 0 {
				if st.objPubs[obj] == nil {
					st.objPubs[obj] = map[core.AttributeHandle][]RegionHandle{}
				}
				st.objPubs[obj][attr] = regions
				delete(pending, attr)
			}
		}
	}
	if len(st.objPubs[obj]) == 0 {
		delete(st.objPubs, obj)
	}
	st.pruneInactive(obj)
}

// AssociationAttributes snapshots only keys. Ownership queries must run after
// releasing the DDM lock; the caller holds the object pin throughout resignation.
func (m *Manager) AssociationAttributes(fed core.FederationName) map[core.ObjectHandle][]core.AttributeHandle {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := map[core.ObjectHandle][]core.AttributeHandle{}
	if st := m.fed[fed]; st != nil {
		for obj, attrs := range st.objPubs {
			for attr := range attrs {
				result[obj] = append(result[obj], attr)
			}
		}
		for obj, callers := range st.inactivePubs {
			for _, attrs := range callers {
				for attr := range attrs {
					result[obj] = append(result[obj], attr)
				}
			}
		}
	}
	for obj, attrs := range result {
		slices.Sort(attrs)
		result[obj] = slices.Compact(attrs)
	}
	return result
}

// ForgetFederateAssociations is the object-pinned, callback-free portion of
// resignation cleanup. Full region/subscription teardown follows after unlock.
func (m *Manager) ForgetFederateAssociations(fed core.FederationName, caller core.FederateHandle) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.fed[fed]
	if st == nil {
		return
	}
	st.forgetFederateAssociations(caller)
	if p := m.associationRollback; p != nil && p.fed == fed && p.origin == st {
		p.shadow.forgetFederateAssociations(caller)
	}
}

func (st *federationDDMState) forgetFederateAssociations(caller core.FederateHandle) {
	st.releaseConveyedReceiver(caller)
	for obj, attrs := range st.objPubs {
		for attr, regions := range attrs {
			if len(regions) != 0 && st.regions[regions[0]] != nil && st.regions[regions[0]].owner == caller {
				delete(attrs, attr)
			}
		}
		if len(attrs) == 0 {
			delete(st.objPubs, obj)
		}
	}
	for obj := range st.inactivePubs {
		delete(st.inactivePubs[obj], caller)
		st.pruneInactive(obj)
	}
}

// Local deletion is only permitted for a nonowner, so only that caller's
// inactive associations can exist. Other federates' state remains untouched.
func (m *Manager) ForgetObjectFederateAssociations(fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st := m.fed[fed]; st != nil {
		delete(st.inactivePubs[obj], caller)
		st.pruneInactive(obj)
		if shadow := m.rollbackStateLocked(fed, obj, st); shadow != nil {
			delete(shadow.inactivePubs[obj], caller)
			shadow.pruneInactive(obj)
		}
	}
}
