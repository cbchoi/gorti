package ownership

import (
	"maps"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// PrepareObjectReconstitution restores ownership captured by Delete Object
// Instance (8.21). Object management pins the instance first; this reservation
// holds ownership until the enclosing retraction transaction commits or aborts.
func (m *Manager) PrepareObjectReconstitution(fed core.FederationName, obj core.ObjectHandle, owners map[core.AttributeHandle]core.FederateHandle) core.OutboxReservation {
	m.mu.Lock()
	return &objectReconstitution{manager: m, fed: fed, object: obj, owners: maps.Clone(owners)}
}

type objectReconstitution struct {
	manager *Manager
	fed     core.FederationName
	object  core.ObjectHandle
	owners  map[core.AttributeHandle]core.FederateHandle
	once    sync.Once
}

func (r *objectReconstitution) Commit() error {
	r.once.Do(func() {
		defer r.manager.mu.Unlock()
		st := r.manager.stateForLocked(r.fed)
		for key := range st.owners {
			if key.obj == r.object {
				delete(st.owners, key)
			}
		}
		for key := range st.pendingAcquires {
			if key.obj == r.object {
				delete(st.pendingAcquires, key)
			}
		}
		for key := range st.pendingDivests {
			if key.obj == r.object {
				delete(st.pendingDivests, key)
			}
		}
		for key := range st.unownedOffers {
			if key.obj == r.object {
				delete(st.unownedOffers, key)
			}
		}
		for attr, owner := range r.owners {
			if owner != core.InvalidFederateHandle {
				st.owners[ownershipKey{obj: r.object, attr: attr}] = ownershipRecord{owner: owner}
			}
		}
	})
	return nil
}

func (r *objectReconstitution) Release() { r.once.Do(r.manager.mu.Unlock) }
