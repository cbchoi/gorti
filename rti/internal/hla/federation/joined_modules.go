package federation

import (
	"context"
	"fmt"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// JoinedModules returns only the modules actually supplied by this session.
// An empty successful result is different from an unknown historical baseline.
func (m *Manager) JoinedModules(ctx context.Context, fed core.FederationName, h core.FederateHandle) ([]core.FOMModule, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	fs := m.federations[fed]
	m.mu.RUnlock()
	if fs == nil {
		return nil, core.ErrFederationNotFound
	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	if _, ok := fs.handleToName[h]; !ok {
		return nil, core.ErrFederateNotJoined
	}
	modules, ok := fs.joinedModules[h]
	if !ok {
		return nil, fmt.Errorf("federation: join module history unavailable")
	}
	return cloneFOMModules(modules), nil
}
