package federation

import (
	"context"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func (m *Manager) AcquireMemberSnapshot(ctx context.Context, fed core.FederationName, h core.FederateHandle, generation uint64) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if h == core.InvalidFederateHandle {
		return nil, core.ErrFederateNotJoined
	}
	m.mu.RLock()
	fs := m.federations[fed]
	m.mu.RUnlock()
	if fs == nil {
		return nil, core.ErrFederationNotFound
	}
	// Never wait on operations while holding the manager or roster locks.
	fs.operations.Lock()
	var once sync.Once
	release := func() { once.Do(fs.operations.Unlock) }
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	m.mu.RLock()
	current, exists := m.federations[fed]
	destroying := fs.destroying
	m.mu.RUnlock()
	if !exists || current != fs || destroying {
		release()
		return nil, core.ErrFederationNotFound
	}
	if fs.generation != generation {
		release()
		return nil, core.ErrFederationGenerationMismatch
	}
	fs.mu.RLock()
	_, joined := fs.handleToName[h]
	fs.mu.RUnlock()
	if !joined {
		release()
		return nil, core.ErrFederateNotJoined
	}
	return release, nil
}

var _ core.MemberSnapshotLeaser = (*Manager)(nil)
