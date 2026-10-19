package ownership

import (
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// PrepareLocalDelete pins ownership until the object service forgets the caller.
// The caller holds object state first; this method never calls other services.
func (m *Manager) PrepareLocalDelete(fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle) (func(), error) {
	m.mu.RLock()
	st := m.fed[fed]
	if st != nil {
		for key, rec := range st.owners {
			if key.obj == obj && rec.owner == caller {
				m.mu.RUnlock()
				return nil, core.ErrFederateOwnsAttributes
			}
		}
		if st.hasAcquisitionObligationLocked(caller, obj, nil) {
			m.mu.RUnlock()
			return nil, core.ErrOwnershipAcquisitionPending
		}
	}
	var once sync.Once
	return func() { once.Do(m.mu.RUnlock) }, nil
}
