package savepoint

import (
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type admissionEntry struct {
	lock       sync.RWMutex
	references int
}

func (m *ManagerOf[T]) retainAdmission(fed core.FederationName) *admissionEntry {
	m.admissionMu.Lock()
	defer m.admissionMu.Unlock()
	value, _ := m.admission.LoadOrStore(fed, &admissionEntry{})
	entry := value.(*admissionEntry)
	entry.references++
	return entry
}

func (m *ManagerOf[T]) releaseAdmission(fed core.FederationName, entry *admissionEntry) {
	m.admissionMu.Lock()
	defer m.admissionMu.Unlock()
	entry.references--
	if entry.references == 0 {
		m.admission.Delete(fed)
	}
}

// AcquireService admits an ordinary federation operation. The caller keeps
// the lease through all synchronous service effects, including callback queue
// admission. Acquire it before the membership lease to avoid lock inversion.
func (m *ManagerOf[T]) AcquireService(fed core.FederationName) (func(), error) {
	return m.acquireService(fed, core.InvalidFederateHandle)
}

func (m *ManagerOf[T]) AcquireFederateService(fed core.FederationName, h core.FederateHandle) (func(), error) {
	return m.acquireService(fed, h)
}

func (m *ManagerOf[T]) acquireService(fed core.FederationName, h core.FederateHandle) (func(), error) {
	entry := m.retainAdmission(fed)
	entry.lock.RLock()
	m.mu.Lock()
	as, saving := m.saves[fed]
	if saving {
		if h == core.InvalidFederateHandle {
			saving = !as.waiting || len(as.initiated) != 0
		} else {
			saving = saveParticipantStarted(as, h)
		}
	}
	_, restoring := m.restore[fed]
	m.mu.Unlock()
	if saving || restoring {
		entry.lock.RUnlock()
		m.releaseAdmission(fed, entry)
		if saving {
			return nil, core.ErrSaveAlreadyInProgress
		}
		return nil, core.ErrRestoreAlreadyInProgress
	}
	return func() { entry.lock.RUnlock(); m.releaseAdmission(fed, entry) }, nil
}

// AcquireCheckpoint drains admitted service work before a checkpoint control
// call. Transports must acquire this before membership validation and release
// it after the control call, not hold it while waiting for participant reports.
// Callback consumption remains available throughout the protocol.
func (m *ManagerOf[T]) AcquireCheckpoint(fed core.FederationName) func() {
	entry := m.retainAdmission(fed)
	entry.lock.Lock()
	return func() { entry.lock.Unlock(); m.releaseAdmission(fed, entry) }
}
