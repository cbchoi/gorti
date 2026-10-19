package ownership

import (
	"context"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// SetOnOwnershipChanged is composition-only. Notifications run after ownership
// and acquisition object leases are released. Initial registration is handled
// by the object's post-registration hook instead.
func (m *Manager) SetOnOwnershipChanged(hook func(context.Context, core.FederationName, core.ObjectHandle)) {
	m.onOwnershipChanged = hook
}
func (m *Manager) notifyOwnershipChanged(ctx context.Context, fed core.FederationName, obj core.ObjectHandle) {
	if m.onOwnershipChanged != nil {
		m.onOwnershipChanged(ctx, fed, obj)
	}
}

// NotifyOwnershipChanged lets companion mutation paths request the same
// post-commit control refresh. Call only after releasing manager/object locks.
func (m *Manager) NotifyOwnershipChanged(ctx context.Context, fed core.FederationName, obj core.ObjectHandle) {
	m.notifyOwnershipChanged(ctx, fed, obj)
}
