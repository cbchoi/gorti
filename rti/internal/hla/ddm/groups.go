package ddm

import (
	"context"
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func groupAttributes(groups []core.AttributeRegionGroup) []core.AttributeHandle {
	var attrs []core.AttributeHandle
	for _, group := range groups {
		attrs = append(attrs, group.Attributes...)
	}
	slices.Sort(attrs)
	return slices.Compact(attrs)
}

func bindingGroups(bindings map[core.AttributeHandle][]RegionHandle) []core.AttributeRegionGroup {
	groups := make([]core.AttributeRegionGroup, 0, len(bindings))
	for attr, regions := range bindings {
		groups = append(groups, core.AttributeRegionGroup{Attributes: []core.AttributeHandle{attr}, Regions: regions})
	}
	slices.SortFunc(groups, func(a, b core.AttributeRegionGroup) int {
		if a.Attributes[0] < b.Attributes[0] {
			return -1
		}
		if a.Attributes[0] > b.Attributes[0] {
			return 1
		}
		return 0
	})
	return groups
}

func validateGroups(st *federationDDMState, caller core.FederateHandle, groups []core.AttributeRegionGroup, required map[core.AttributeHandle][]DimensionHandle) error {
	for _, group := range groups {
		if err := validateObjectRegions(st, caller, group.Attributes, group.Regions, required); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) SubscribeObjectClassAttributeRegionGroups(ctx context.Context, fed core.FederationName, sub core.FederateHandle, cls core.ObjectClassHandle, groups []core.AttributeRegionGroup, options core.SubscriptionOptions) error {
	return m.changeObjectRegionGroups(ctx, fed, sub, cls, groups, &options)
}

func (m *Manager) UnsubscribeObjectClassAttributeRegionGroups(ctx context.Context, fed core.FederationName, sub core.FederateHandle, cls core.ObjectClassHandle, groups []core.AttributeRegionGroup) error {
	return m.changeObjectRegionGroups(ctx, fed, sub, cls, groups, nil)
}

func (m *Manager) changeObjectRegionGroups(ctx context.Context, fed core.FederationName, sub core.FederateHandle, cls core.ObjectClassHandle, groups []core.AttributeRegionGroup, options *core.SubscriptionOptions) (err error) {
	var subscribed []core.AttributeHandle
	defer m.notifyObjectSubscribed(ctx, fed, sub, cls, &subscribed, &err)
	m.subscriptionMu.Lock()
	defer m.subscriptionMu.Unlock()
	releaseRateGuard := m.lockRateChange()
	defer releaseRateGuard()
	if err := m.validateGroupSubscriptionOptions(ctx, fed, options); err != nil {
		return err
	}
	required, err := m.objectContext(ctx, fed, cls, groupAttributes(groups))
	if err != nil {
		return err
	}
	if options == nil {
		// Unsubscription validates handles, but has no dimension-subset precondition.
		required = nil
	}
	var rollback func()
	defer func() {
		if err == nil && rollback != nil {
			err = m.objectInterest(ctx, fed, sub, cls)
			if err != nil {
				rollback()
			}
		}
	}()
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(ctx, fed)
	if err := validateGroups(st, sub, groups, required); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, group := range groups {
		if len(group.Attributes) == 0 || len(group.Regions) == 0 {
			continue
		}
		if rollback == nil {
			rollback = m.objectSubscriptionRollbackLocked(fed, st, sub, cls)
		}
		for _, attr := range group.Attributes {
			key := objAttrKey{cls: cls, attr: attr}
			if options != nil {
				st.addObjectSubscriptionLocked(key, subscription{subscriber: sub, regions: slices.Clone(group.Regions), passive: !options.Active, updateRate: options.UpdateRateDesignator})
				subscribed = append(subscribed, attr)
				continue
			}
			st.removeObjectSubscriptionRegions(key, sub, group.Regions)
		}
	}
	return nil
}

func (st *federationDDMState) removeObjectSubscriptionRegions(key objAttrKey, sub core.FederateHandle, regions []RegionHandle) {
	kept := removeSubscriptionRegions(st.objSubs[key], sub, regions)
	if len(kept) == 0 {
		delete(st.objSubs, key)
	} else {
		st.objSubs[key] = kept
	}
}

func (m *Manager) validateGroupSubscriptionOptions(ctx context.Context, fed core.FederationName, options *core.SubscriptionOptions) error {
	if options == nil {
		return nil
	}
	if m.opts.ValidateSubscriptionOptions != nil {
		return m.opts.ValidateSubscriptionOptions(ctx, fed, *options)
	}
	if options.UpdateRateDesignator != "" {
		return core.ErrInvalidUpdateRateDesignator
	}
	return nil
}

func removeSubscriptionRegions(rows []subscription, sub core.FederateHandle, regions []RegionHandle) []subscription {
	var kept []subscription
	for _, row := range rows {
		if row.subscriber == sub {
			row.regions = remainingRegions(row.regions, regions)
		}
		if len(row.regions) > 0 {
			kept = append(kept, row)
		}
	}
	return kept
}

func (m *Manager) AssociateRegionGroupsForUpdates(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, groups []core.AttributeRegionGroup) error {
	return m.changeAssociationGroups(ctx, fed, caller, obj, groups, false)
}

func (m *Manager) UnassociateRegionGroupsForUpdates(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, groups []core.AttributeRegionGroup) error {
	return m.changeAssociationGroups(ctx, fed, caller, obj, groups, true)
}

func (m *Manager) changeAssociationGroups(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, groups []core.AttributeRegionGroup, remove bool) (err error) {
	mode := associationAdd
	if remove {
		mode = associationRemove
	}
	return m.changeAssociationGroupsMode(ctx, fed, caller, obj, groups, mode)
}

type associationMode uint8

const (
	associationAdd associationMode = iota
	associationRemove
	associationReplace
	associationLegacyRemove
)

func (m *Manager) changeAssociationGroupsMode(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, groups []core.AttributeRegionGroup, mode associationMode) (err error) {
	defer m.notifyRegionStateChanged(ctx, fed, &err)
	m.subscriptionMu.Lock()
	defer m.subscriptionMu.Unlock()
	releaseRateGuard := m.lockRateChange()
	defer releaseRateGuard()
	var rollback func()
	defer m.finishRateMutation(ctx, fed, &err, &rollback)
	remove := mode == associationRemove || mode == associationLegacyRemove
	groups, owners, required, release, err := m.prepareAssociationGroups(ctx, fed, caller, obj, groups, remove)
	if err != nil {
		return err
	}
	defer release()
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(ctx, fed)
	if remove {
		// Removal checks handle validity and creator, not dimensional compatibility.
		required = nil
	}
	if err := validateGroups(st, caller, groups, required); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(groups) == 0 && (mode == associationReplace || mode == associationLegacyRemove) {
		rollback = m.associationRateRollbackLocked(fed, st, obj)
		st.clearObjectAssociations(obj, caller)
		return nil
	}
	for _, group := range groups {
		if len(group.Attributes) == 0 || (len(group.Regions) == 0 && mode != associationReplace && mode != associationLegacyRemove) {
			continue
		}
		if rollback == nil {
			rollback = m.associationRateRollbackLocked(fed, st, obj)
		}
		st.changeAssociationGroup(obj, caller, group, owners, mode)
	}
	return nil
}

func (st *federationDDMState) clearObjectAssociations(obj core.ObjectHandle, caller core.FederateHandle) {
	for attr, regions := range st.objPubs[obj] {
		if len(regions) != 0 && st.regions[regions[0]] != nil && st.regions[regions[0]].owner == caller {
			delete(st.objPubs[obj], attr)
		}
	}
	delete(st.inactivePubs[obj], caller)
	if len(st.objPubs[obj]) == 0 {
		delete(st.objPubs, obj)
	}
	st.pruneInactive(obj)
}

func (st *federationDDMState) changeAssociationGroup(obj core.ObjectHandle, caller core.FederateHandle, group core.AttributeRegionGroup, owners map[core.AttributeHandle]core.FederateHandle, mode associationMode) {
	remove := mode == associationRemove || mode == associationLegacyRemove
	for _, attr := range group.Attributes {
		per := st.objPubs[obj]
		if owners != nil && owners[attr] != caller {
			per = st.inactiveFor(obj, caller, !remove)
		} else if per == nil && !remove {
			per = make(map[core.AttributeHandle][]RegionHandle)
			st.objPubs[obj] = per
		}
		if per == nil {
			continue
		}
		changeAttributeRegions(per, attr, group.Regions, mode)
	}
	if len(st.objPubs[obj]) == 0 {
		delete(st.objPubs, obj)
	}
	st.pruneInactive(obj)
}

func changeAttributeRegions(per map[core.AttributeHandle][]RegionHandle, attr core.AttributeHandle, regions []RegionHandle, mode associationMode) {
	switch mode {
	case associationRemove, associationLegacyRemove:
		remaining := remainingRegions(per[attr], regions)
		if len(remaining) == 0 {
			delete(per, attr)
		} else {
			per[attr] = remaining
		}
	case associationReplace:
		if len(regions) == 0 {
			delete(per, attr)
		} else {
			per[attr] = slices.Clone(regions)
		}
	default:
		added := append(slices.Clone(per[attr]), regions...)
		slices.Sort(added)
		per[attr] = slices.Compact(added)
	}
}

func (m *Manager) PrepareRegionUpdateGroups(ctx context.Context, fed core.FederationName, caller core.FederateHandle, cls core.ObjectClassHandle, groups []core.AttributeRegionGroup) (core.GroupedRegionUpdateRequest, error) {
	required, err := m.objectContext(ctx, fed, cls, groupAttributes(groups))
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	st := m.fed[fed]
	if st == nil {
		st = newFederationDDMState()
	}
	if err := validateGroups(st, caller, groups, required); err != nil {
		m.mu.RUnlock()
		return nil, err
	}
	queries := make(map[core.AttributeHandle][]regionBounds)
	for _, group := range groups {
		for _, attr := range group.Attributes {
			dims, strict := required[attr]
			bounds := st.realize(group.Regions, dims, strict)
			queries[attr] = append(queries[attr], bounds...)
		}
	}
	return &groupedUpdateRequest{state: st, cls: cls, queries: queries, release: sync.OnceFunc(m.mu.RUnlock)}, nil
}

type groupedUpdateRequest struct {
	cls     core.ObjectClassHandle
	state   *federationDDMState
	queries map[core.AttributeHandle][]regionBounds
	release func()
}

func (r *groupedUpdateRequest) Release() { r.release() }
func (r *groupedUpdateRequest) Matches(obj core.ObjectHandle, requested, actual core.AttributeHandle) bool {
	query := r.queries[requested]
	if len(query) == 0 {
		return false
	}
	regions := r.state.objPubs[obj][actual]
	if len(regions) == 0 {
		return overlapsDefault(query)
	}
	return regionsOverlap(query, r.state.objectRealizations(r.cls, actual, regions))
}
