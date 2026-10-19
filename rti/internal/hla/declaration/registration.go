package declaration

import (
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// PublishedObjectAttributes captures one declaration state for registration.
// The returned slice is detached and sorted.
func (m *Manager) PublishedObjectAttributes(fed core.FederationName, publisher core.FederateHandle, cls core.ObjectClassHandle) []core.AttributeHandle {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := m.fed[fed]
	if st == nil {
		return nil
	}
	var attrs []core.AttributeHandle
	for key, publishers := range st.objPubs {
		if key.cls == cls {
			if _, ok := publishers[publisher]; ok {
				attrs = append(attrs, key.attr)
			}
		}
	}
	slices.Sort(attrs)
	return attrs
}
