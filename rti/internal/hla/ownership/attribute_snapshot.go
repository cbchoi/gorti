package ownership

import (
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// PrepareAttributeOwners retains a consistent ownership view until release.
// Service composition acquires object state first and DDM state afterward.
func (m *Manager) PrepareAttributeOwners(fed core.FederationName, obj core.ObjectHandle, attrs []core.AttributeHandle) (map[core.AttributeHandle]core.FederateHandle, func()) {
	m.mu.RLock()
	owners := make(map[core.AttributeHandle]core.FederateHandle, len(attrs))
	if st := m.fed[fed]; st != nil {
		for _, attr := range attrs {
			owners[attr] = st.owners[ownershipKey{obj: obj, attr: attr}].owner
		}
	}
	var once sync.Once
	return owners, func() { once.Do(m.mu.RUnlock) }
}
