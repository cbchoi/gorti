package ddm

import (
	"context"
	"strings"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// InteractionSubscribersForDefaultRegion supplies the regional half of an
// ordinary send's recipients. Ordinary declaration subscriptions remain a
// separate union at the object-service boundary.
func (m *Manager) InteractionSubscribersForDefaultRegion(ctx context.Context, fed core.FederationName, cls core.InteractionClassHandle) []core.FederateHandle {
	m.mu.RLock()
	st := m.fed[fed]
	unused := st == nil || len(st.intSubs) == 0
	m.mu.RUnlock()
	if unused {
		return nil
	}
	h, err := m.opts.FOMs.Get(ctx, fed)
	if err != nil {
		return nil
	}
	names, named := h.(interface {
		InteractionClassName(core.InteractionClassHandle) (string, bool)
	})
	var sentName string
	if named {
		sentName, _ = names.InteractionClassName(cls)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	st = m.fed[fed]
	if st == nil {
		return nil
	}
	hits := map[core.FederateHandle]struct{}{}
	for subscribed, rows := range st.intSubs {
		matches := subscribed == cls
		if !matches && named {
			name, found := names.InteractionClassName(subscribed)
			matches = found && strings.HasPrefix(sentName, name+".")
		}
		if !matches {
			continue
		}
		for _, row := range rows {
			if overlapsDefault(st.interactionRealizations(subscribed, row.regions)) {
				hits[row.subscriber] = struct{}{}
			}
		}
	}
	return sortedFederateHandles(hits)
}

// InteractionRegionsOverlapDefaultRegion checks the producer realization,
// rather than assuming every valid specification overlaps the default region.
func (m *Manager) InteractionRegionsOverlapDefaultRegion(ctx context.Context, fed core.FederationName, cls core.InteractionClassHandle, regions []core.DDMRegionHandleCore) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.fed[fed]
	if st == nil {
		return false
	}
	m.refreshFOMDimensions(ctx, fed, st)
	return overlapsDefault(st.interactionRealizations(cls, regions))
}

// PublisherRegionsOverlapDefaultRegion is the attribute-side default receiver
// predicate. An absent association denotes the default region, not an empty
// contextual realization.
func (m *Manager) PublisherRegionsOverlapDefaultRegion(fed core.FederationName, cls core.ObjectClassHandle, attr core.AttributeHandle, regions []core.DDMRegionHandleCore) bool {
	if len(regions) == 0 {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.fed[fed]
	if st == nil {
		return false
	}
	m.refreshFOMDimensions(context.Background(), fed, st)
	return overlapsDefault(st.objectRealizations(cls, attr, regions))
}
