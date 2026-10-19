// Package ddm — IEEE 1516.1-2010 §9 missing services (M23 W5).
//
// AssociateRegionsForUpdates adds region associations atomically.
// UnassociateRegionsForUpdates — drops matching attr-region pairs.
// UnsubscribeObjectClassAttributesWithRegions — inverse of the
//
//	subscribe variant.
//
// UnsubscribeInteractionClassWithRegions — interaction variant.
package ddm

import (
	"context"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// AssociateRegionsForUpdates adds pairs without replacing existing regions.
// The legacy replacement helper remains available for internal callers.
func (m *Manager) AssociateRegionsForUpdates(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrToRegions map[core.AttributeHandle][]RegionHandle,
) error {
	return m.AssociateRegionGroupsForUpdates(ctx, fed, owner, obj, bindingGroups(attrToRegions))
}

// UnassociateRegionsForUpdates drops the supplied attr-region pairs
// from the object's publisher associations. When attrToRegions is
// empty, drops only the caller's associations (legacy wire convention).
// The grouped operation implements IEEE empty-as-no-op semantics.
//
// Removing absent associations is idempotent after validating the request.
func (m *Manager) UnassociateRegionsForUpdates(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrToRegions map[core.AttributeHandle][]RegionHandle,
) (err error) {
	return m.changeAssociationGroupsMode(ctx, fed, owner, obj, bindingGroups(attrToRegions), associationLegacyRemove)
}

// UnsubscribeObjectClassAttributesWithRegions drops the subscriber's
// region-scoped subscription rows for the (cls, attr) pairs.
//
// A nonempty region set removes only those regions. An empty set leaves
// subscriptions unchanged after validation (9.1.3.2(b), 9.9).
func (m *Manager) UnsubscribeObjectClassAttributesWithRegions(
	ctx context.Context,
	fed core.FederationName,
	subscriber core.FederateHandle,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
	regions []RegionHandle,
) (err error) {
	m.subscriptionMu.Lock()
	defer m.subscriptionMu.Unlock()
	releaseRateGuard := m.lockRateChange()
	defer releaseRateGuard()
	required, err := m.objectContext(ctx, fed, cls, attrs)
	if err != nil {
		return err
	}
	var rollback func()
	defer func() {
		if err == nil {
			err = m.objectInterest(ctx, fed, subscriber, cls)
			if err != nil && rollback != nil {
				rollback()
			}
		}
	}()
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(ctx, fed)
	if err := validateObjectRegions(st, subscriber, attrs, regions, required); err != nil {
		return err
	}
	if len(regions) == 0 {
		return nil
	}
	rollback = m.objectSubscriptionRollbackLocked(fed, st, subscriber, cls)
	for _, attr := range attrs {
		key := objAttrKey{cls: cls, attr: attr}
		var kept []subscription
		for _, row := range st.objSubs[key] {
			if row.subscriber == subscriber {
				row.regions = remainingRegions(row.regions, regions)
			}
			if len(row.regions) > 0 {
				kept = append(kept, row)
			}
		}
		if len(kept) == 0 {
			delete(st.objSubs, key)
		} else {
			st.objSubs[key] = kept
		}
	}
	return nil
}

// UnsubscribeInteractionClassWithRegions drops the subscriber's
// region-scoped interaction subscription, retaining unrequested regions.
func (m *Manager) UnsubscribeInteractionClassWithRegions(
	ctx context.Context,
	fed core.FederationName,
	subscriber core.FederateHandle,
	cls core.InteractionClassHandle,
	regions []RegionHandle,
) (err error) {
	m.subscriptionMu.Lock()
	defer m.subscriptionMu.Unlock()
	dims, strict, err := m.interactionContext(ctx, fed, cls)
	if err != nil {
		return err
	}
	var rollback func()
	defer func() {
		if err == nil {
			err = m.interactionInterest(ctx, fed, subscriber, cls)
			if err != nil && rollback != nil {
				rollback()
			}
		}
	}()
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(ctx, fed)
	if err := validateRegions(st, subscriber, regions, dims, strict); err != nil {
		return err
	}
	// An empty regional unsubscribe is not an ordinary class unsubscribe (9.11).
	if len(regions) == 0 {
		return nil
	}
	rollback = m.interactionSubscriptionRollbackLocked(fed, st, subscriber, cls)
	var kept []interactionSubscription
	for _, row := range st.intSubs[cls] {
		if row.subscriber == subscriber {
			row.regions = remainingRegions(row.regions, regions)
		}
		if len(row.regions) > 0 {
			kept = append(kept, row)
		}
	}
	if len(kept) == 0 {
		delete(st.intSubs, cls)
	} else {
		st.intSubs[cls] = kept
	}
	return nil
}

func remainingRegions(current, removed []RegionHandle) []RegionHandle {
	if len(removed) == 0 {
		return nil
	}
	result := make([]RegionHandle, 0, len(current))
	for _, region := range current {
		if !slices.Contains(removed, region) {
			result = append(result, region)
		}
	}
	return result
}
