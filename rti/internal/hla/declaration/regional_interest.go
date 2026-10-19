package declaration

import (
	"context"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// These hooks replace the aggregate active interest from DDM without adding
// ordinary data recipients. DDM must serialize commits with their notifications
// and release its state lock before calling. Region-overlap-specific advisory
// semantics are not implemented by this class-level interest projection.
func (m *Manager) SetRegionalObjectSubscriptionInterest(ctx context.Context, fed core.FederationName, sub core.FederateHandle, cls core.ObjectClassHandle, activeAttrs []core.AttributeHandle) error {
	return m.change(ctx, fed, func(st *federationState) {
		for key, members := range st.regionalObjects {
			if key.cls == cls {
				delete(members, sub)
				if len(members) == 0 {
					delete(st.regionalObjects, key)
				}
			}
		}
		for _, attr := range activeAttrs {
			key := objAttrKey{cls, attr}
			if st.regionalObjects[key] == nil {
				st.regionalObjects[key] = map[core.FederateHandle]struct{}{}
			}
			st.regionalObjects[key][sub] = struct{}{}
		}
	})
}

func (m *Manager) SetRegionalInteractionSubscriptionInterest(ctx context.Context, fed core.FederationName, sub core.FederateHandle, cls core.InteractionClassHandle, active bool) error {
	return m.change(ctx, fed, func(st *federationState) {
		delete(st.regionalInteractions[cls], sub)
		if active {
			if st.regionalInteractions[cls] == nil {
				st.regionalInteractions[cls] = map[core.FederateHandle]struct{}{}
			}
			st.regionalInteractions[cls][sub] = struct{}{}
		} else if len(st.regionalInteractions[cls]) == 0 {
			delete(st.regionalInteractions, cls)
		}
	})
}
