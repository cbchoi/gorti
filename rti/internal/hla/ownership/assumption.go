package ownership

import (
	"bytes"
	"context"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// Each candidate is checked against one attribute and the current offer. The
// object lease and ownership lock are released before the outbox is invoked.
func (m *Manager) fanoutAssumption(ctx context.Context, fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, tag []byte, unowned bool) {
	m.fanoutAssumptionTo(ctx, fed, owner, obj, attrs, tag, unowned, 0)
}

func (m *Manager) fanoutAssumptionTo(ctx context.Context, fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, tag []byte, unowned bool, only core.FederateHandle) {
	if m.opts.Subscribers == nil {
		return
	}
	attrs = slices.Clone(attrs)
	slices.Sort(attrs)
	attrs = slices.Compact(attrs)
	for _, attr := range attrs {
		one := []core.AttributeHandle{attr}
		candidates := slices.Clone(m.opts.Subscribers(ctx, fed, obj, one))
		slices.Sort(candidates)
		for _, candidate := range slices.Compact(candidates) {
			if only != 0 && candidate != only {
				continue
			}
			if candidate == owner || candidate == core.InvalidFederateHandle || candidate == ^core.FederateHandle(0) {
				continue
			}
			release := func() {}
			if m.opts.PrepareAcquisition != nil {
				var err error
				release, err = m.opts.PrepareAcquisition(ctx, fed, candidate, obj, one)
				if err != nil || release == nil {
					continue
				}
			}
			m.mu.RLock()
			st := m.fed[fed]
			valid := validAssumption(st, obj, attr, candidate, owner, tag, unowned)
			m.mu.RUnlock()
			release()
			if valid {
				_ = m.opts.Outbox.Send(ctx, fed, candidate, assumptionEvent(obj, one, tag, owner))
			}
		}
	}
}

// ObjectClassPublished resumes existing offers for newly published attributes.
// Candidate enumeration and the pinned acquisition validator are authoritative;
// the changed class alone cannot establish an object's known class.
func (m *Manager) ObjectClassPublished(ctx context.Context, fed core.FederationName, publisher core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle) {
	wanted := make(map[core.AttributeHandle]bool, len(attrs))
	for _, attr := range attrs {
		wanted[attr] = true
	}
	type offer struct {
		key     ownershipKey
		owner   core.FederateHandle
		tag     []byte
		unowned bool
	}
	var offers []offer
	m.mu.RLock()
	if st := m.fed[fed]; st != nil {
		for key, pending := range st.pendingDivests {
			if m.opts.PublicationAttributes != nil || wanted[key.attr] {
				offers = append(offers, offer{key: key, owner: pending.owner, tag: cloneTag(pending.tag)})
			}
		}
		for key, divester := range st.unownedOffers {
			if m.opts.PublicationAttributes != nil || wanted[key.attr] {
				offers = append(offers, offer{key: key, owner: divester, unowned: true})
			}
		}
	}
	m.mu.RUnlock()
	slices.SortFunc(offers, func(a, b offer) int {
		return compareObjectAttribute(a.key.obj, a.key.attr, b.key.obj, b.key.attr)
	})
	for _, offer := range offers {
		if m.opts.PublicationAttributes != nil && !slices.Contains(m.opts.PublicationAttributes(ctx, fed, offer.key.obj, cls, attrs), offer.key.attr) {
			continue
		}
		m.fanoutAssumptionTo(ctx, fed, offer.owner, offer.key.obj, []core.AttributeHandle{offer.key.attr}, offer.tag, offer.unowned, publisher)
	}
}
func validAssumption(st *federationState, obj core.ObjectHandle, attr core.AttributeHandle, candidate, owner core.FederateHandle, tag []byte, unowned bool) bool {
	valid := false
	if st != nil {
		key := ownershipKey{obj: obj, attr: attr}
		rec, owned := st.owners[key]
		_, acquiring := st.pendingAcquires[acquireKey{obj: obj, attr: attr, acquirer: candidate}]
		if unowned {
			divester, offered := st.unownedOffers[key]
			valid = !owned && !acquiring && offered && divester == owner
		} else {
			offer, pending := st.pendingDivests[key]
			valid = owned && rec.owner == owner && pending && offer.owner == owner && !acquiring && bytes.Equal(offer.tag, tag)
		}
	}
	return valid
}
