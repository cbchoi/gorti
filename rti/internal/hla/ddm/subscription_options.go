package ddm

import (
	"context"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// Keep disjoint region groups with common options, including grouped rows
// restored from older snapshots. Only requested triples take the new options.
func (st *federationDDMState) addObjectSubscriptionLocked(key objAttrKey, added subscription) {
	rows := st.objSubs[key]
	kept := make([]subscription, 0, len(rows)+1)
	for _, row := range rows {
		if row.subscriber != added.subscriber {
			kept = append(kept, row)
			continue
		}
		if row.passive == added.passive && row.updateRate == added.updateRate {
			added.regions = append(added.regions, row.regions...)
			continue
		}
		row.regions = remainingRegions(row.regions, added.regions)
		if len(row.regions) > 0 {
			kept = append(kept, row)
		}
	}
	slices.Sort(added.regions)
	added.regions = slices.Compact(added.regions)
	st.objSubs[key] = append(kept, added)
}

// Interaction options belong to each (class, region) pair (9.10), with
// disjoint groups retained in the existing snapshot representation.
func (st *federationDDMState) addInteractionSubscriptionLocked(cls core.InteractionClassHandle, added interactionSubscription) {
	rows := st.intSubs[cls]
	kept := make([]interactionSubscription, 0, len(rows)+1)
	for _, row := range rows {
		if row.subscriber != added.subscriber {
			kept = append(kept, row)
			continue
		}
		if row.passive == added.passive {
			added.regions = append(added.regions, row.regions...)
			continue
		}
		row.regions = remainingRegions(row.regions, added.regions)
		if len(row.regions) > 0 {
			kept = append(kept, row)
		}
	}
	slices.Sort(added.regions)
	added.regions = slices.Compact(added.regions)
	st.intSubs[cls] = append(kept, added)
}

// Snapshot only this operation's subscriber/class rows. subscriptionMu remains
// held through the interest hook and rollback; m.mu is released for the hook.
func (m *Manager) objectSubscriptionRollbackLocked(fed core.FederationName, st *federationDDMState, sub core.FederateHandle, cls core.ObjectClassHandle) func() {
	previous := make(map[objAttrKey][]subscription)
	for key, rows := range st.objSubs {
		if key.cls != cls {
			continue
		}
		for _, row := range rows {
			if row.subscriber == sub {
				row.regions = slices.Clone(row.regions)
				previous[key] = append(previous[key], row)
			}
		}
	}
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.fed[fed] != st {
			return
		}
		for key := range st.objSubs {
			if key.cls == cls {
				st.removeObjSubLocked(key, sub)
			}
		}
		for key, rows := range previous {
			st.objSubs[key] = append(st.objSubs[key], rows...)
		}
	}
}

func (m *Manager) interactionSubscriptionRollbackLocked(fed core.FederationName, st *federationDDMState, sub core.FederateHandle, cls core.InteractionClassHandle) func() {
	var previous []interactionSubscription
	for _, row := range st.intSubs[cls] {
		if row.subscriber == sub {
			row.regions = slices.Clone(row.regions)
			previous = append(previous, row)
		}
	}
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.fed[fed] != st {
			return
		}
		st.removeIntSubLocked(cls, sub)
		if len(previous) > 0 {
			st.intSubs[cls] = append(st.intSubs[cls], previous...)
		}
	}
}

// Advisory interest is separate from ordinary delivery subscriptions. These
// hooks run without DDM locks and do not widen regional delivery recipients.
func (m *Manager) objectInterest(ctx context.Context, fed core.FederationName, sub core.FederateHandle, cls core.ObjectClassHandle) error {
	if m.opts.OnObjectSubscriptionInterest == nil {
		return nil
	}
	m.mu.RLock()
	var attrs []core.AttributeHandle
	if st := m.fed[fed]; st != nil {
		for key, rows := range st.objSubs {
			if key.cls != cls {
				continue
			}
			for _, row := range rows {
				if row.subscriber == sub && !row.passive && len(row.regions) > 0 {
					attrs = append(attrs, key.attr)
					break
				}
			}
		}
	}
	m.mu.RUnlock()
	slices.Sort(attrs)
	return m.opts.OnObjectSubscriptionInterest(ctx, fed, sub, cls, attrs)
}

func (m *Manager) interactionInterest(ctx context.Context, fed core.FederationName, sub core.FederateHandle, cls core.InteractionClassHandle) error {
	if m.opts.OnInteractionSubscriptionInterest == nil {
		return nil
	}
	m.mu.RLock()
	active := false
	if st := m.fed[fed]; st != nil {
		for _, row := range st.intSubs[cls] {
			if row.subscriber == sub && !row.passive && len(row.regions) > 0 {
				active = true
				break
			}
		}
	}
	m.mu.RUnlock()
	return m.opts.OnInteractionSubscriptionInterest(ctx, fed, sub, cls, active)
}
