package declaration

import "github.com/cbchoi/gorti/rti/internal/hla/core"

// Regional rates are already restricted to overlapping object attributes by
// the DDM bridge. Passive subscriptions still constrain received updates.
func (m *Manager) RegionalAttributeSubscriptionOptions(fed core.FederationName, sub core.FederateHandle, obj core.ObjectHandle, attr core.AttributeHandle) []core.SubscriptionOptions {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := m.fed[fed]
	if st == nil {
		return nil
	}
	var result []core.SubscriptionOptions
	for _, entry := range st.regionalRates {
		if entry.Subscriber == sub && entry.Object == obj && entry.Attribute == attr {
			result = append(result, entry.Options)
		}
	}
	return result
}
