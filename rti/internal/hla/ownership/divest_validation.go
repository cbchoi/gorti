package ownership

import (
	"context"
	"errors"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// Preparation precedes the ownership lock, matching object deletion's lock
// order. Release the object pin before calling journals, observers or outboxes.
func (m *Manager) prepareDivestiture(ctx context.Context, fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
	if m.opts.PrepareDivestiture == nil {
		return func() {}, nil
	}
	release, err := m.opts.PrepareDivestiture(ctx, fed, owner, obj, attrs)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return nil, errors.New("ownership: divestiture preparation returned no release")
	}
	return sync.OnceFunc(release), nil
}
