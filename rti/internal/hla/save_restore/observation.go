package savepoint

import (
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// AcquireObservation admits callback receipt accounting and read-only metrics
// during save/restore protocols while excluding atomic checkpoint transitions.
// It does not authorize service mutations. Composition must expose this only
// to observation producers, and acquire it before any membership/domain lease.
// Never acquire it recursively or while holding checkpoint admission.
func (m *ManagerOf[T]) AcquireObservation(fed core.FederationName) func() {
	entry := m.retainAdmission(fed)
	entry.lock.RLock()
	var once sync.Once
	return func() { once.Do(func() { entry.lock.RUnlock(); m.releaseAdmission(fed, entry) }) }
}
