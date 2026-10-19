package federation

import (
	"fmt"
	"math"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ReserveRestoreHandles allocates fresh target-execution identities, including
// historical producers. Caller holds checkpoint admission. Lock order matches
// Join: manager table -> roster; no callbacks or dependent managers are entered.
// Failed restore preparations consume gaps rather than reusing issued handles.
func (m *Manager) ReserveRestoreHandles(fed core.FederationName, generation uint64, count int) ([]core.FederateHandle, error) {
	if count <= 0 {
		return nil, fmt.Errorf("restore handle count must be positive")
	}
	m.mu.RLock()
	fs := m.federations[fed]
	if fs == nil || fs.destroying {
		m.mu.RUnlock()
		return nil, core.ErrFederationNotFound
	}
	if fs.generation != generation {
		m.mu.RUnlock()
		return nil, core.ErrFederationGenerationMismatch
	}
	fs.mu.Lock()
	m.mu.RUnlock()
	defer fs.mu.Unlock()
	limit := uint64(math.MaxUint32)
	if generation == math.MaxUint32 {
		limit--
	} // max uint64 is the internal MOM producer.
	if fs.nextFederateHandle > limit || uint64(count) > limit-fs.nextFederateHandle {
		return nil, fmt.Errorf("restore federate handles exhausted")
	}
	result := make([]core.FederateHandle, count)
	for i := range result {
		fs.nextFederateHandle++
		result[i] = core.FederateHandle(generation<<32 | fs.nextFederateHandle)
	}
	return result, nil
}
