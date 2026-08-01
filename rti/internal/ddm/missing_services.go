// Package ddm — IEEE 1516.1-2010 §9 missing services (M23 W5).
//
// AssociateRegionsForUpdates — wires the existing manager method
//   AssociateRegionsWithObjectInstance (which already implements the
//   §9.6 semantics).
// UnassociateRegionsForUpdates — drops matching attr-region pairs.
// UnsubscribeObjectClassAttributesWithRegions — inverse of the
//   subscribe variant.
// UnsubscribeInteractionClassWithRegions — interaction variant.

package ddm

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// AssociateRegionsForUpdates is the §9.6-named alias for the existing
// AssociateRegionsWithObjectInstance manager method. The wire RPC
// dispatches here so the spec name lives at the API surface.
func (m *Manager) AssociateRegionsForUpdates(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrToRegions map[core.AttributeHandle][]RegionHandle,
) error {
	return m.AssociateRegionsWithObjectInstance(ctx, fed, owner, obj, attrToRegions)
}

// UnassociateRegionsForUpdates drops the supplied attr-region pairs
// from the object's publisher associations. When attrToRegions is
// empty, drops ALL associations for the object (matches §9.7's
// "all associations" semantic).
//
// Errors: ErrObjectHandleInvalid if the object has no associations.
func (m *Manager) UnassociateRegionsForUpdates(
	_ context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrToRegions map[core.AttributeHandle][]RegionHandle,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(context.Background(), fed)
	per, ok := st.objPubs[obj]
	if !ok {
		// Idempotent: nothing to drop.
		return nil
	}

	// Empty pairs → drop all associations for the object.
	if len(attrToRegions) == 0 {
		delete(st.objPubs, obj)
		_ = owner
		return nil
	}

	// Targeted: per attribute, drop matching regions. If a region set
	// becomes empty after the drops, remove the attribute key.
	for attr, dropRegions := range attrToRegions {
		current, exists := per[attr]
		if !exists {
			continue
		}
		if len(dropRegions) == 0 {
			// Empty region list for an attribute → drop ALL regions
			// for that attribute (matches the spec's "unassociate
			// these attribute(s) entirely" reading).
			delete(per, attr)
			continue
		}
		dropSet := make(map[RegionHandle]struct{}, len(dropRegions))
		for _, r := range dropRegions {
			dropSet[r] = struct{}{}
		}
		filtered := current[:0]
		for _, r := range current {
			if _, drop := dropSet[r]; drop {
				continue
			}
			filtered = append(filtered, r)
		}
		if len(filtered) == 0 {
			delete(per, attr)
		} else {
			per[attr] = append([]RegionHandle(nil), filtered...)
		}
	}
	if len(per) == 0 {
		delete(st.objPubs, obj)
	}
	return nil
}

// UnsubscribeObjectClassAttributesWithRegions drops the subscriber's
// region-scoped subscription rows for the (cls, attr) pairs.
//
// M23 simplification: drops the subscription regardless of which
// regions the caller passes — i.e., calling unsubscribe with any
// region set drops the entire subscription. Per-region partial
// unsubscribe is a follow-up.
func (m *Manager) UnsubscribeObjectClassAttributesWithRegions(
	_ context.Context,
	fed core.FederationName,
	subscriber core.FederateHandle,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
	_ []RegionHandle,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(context.Background(), fed)
	for _, attr := range attrs {
		key := objAttrKey{cls: cls, attr: attr}
		st.removeObjSubLocked(key, subscriber)
	}
	return nil
}

// UnsubscribeInteractionClassWithRegions drops the subscriber's
// region-scoped interaction subscription. Same simplification as the
// object-class variant.
func (m *Manager) UnsubscribeInteractionClassWithRegions(
	_ context.Context,
	fed core.FederationName,
	subscriber core.FederateHandle,
	cls core.InteractionClassHandle,
	_ []RegionHandle,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(context.Background(), fed)
	st.removeIntSubLocked(cls, subscriber)
	return nil
}
