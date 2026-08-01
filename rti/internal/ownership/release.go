// Package ownership — ReleaseAllOwnedBy + CancelPendingFor (M24 W1+W2).
//
// ReleaseAllOwnedBy is the resign-time hook the federation manager
// chains into OnFederateResigned when the resign action includes
// attribute divestiture. It drops every ownership record where the
// owner equals the resigning federate, returning the released set
// for caller use (e.g., emitting subscriber notifications).
//
// CancelPendingFor (M24 W2) drops every in-flight pending divest /
// acquire keyed by the resigning federate. Used by the
// CANCEL_PENDING_OWNERSHIP and CANCEL_THEN_DELETE_THEN_DIVEST
// resign actions.

package ownership

import (
	"context"
	"sort"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ReleasedAttribute bundles one (object, attribute) record that was
// dropped by ReleaseAllOwnedBy. Callers use this to emit any peer
// notifications they want; the manager itself does not emit on
// release (release-on-resign is silent in cut-1).
type ReleasedAttribute struct {
	Object    core.ObjectHandle
	Attribute core.AttributeHandle
}

// ReleaseAllOwnedBy releases every attribute owned by the federate
// in fed. Returns the released set in (object, attribute) sort order.
//
// Idempotent: if the federate owns nothing (or fed is unknown), the
// result is an empty slice and no error is returned.
func (m *Manager) ReleaseAllOwnedBy(
	_ context.Context,
	fed core.FederationName,
	h core.FederateHandle,
) []ReleasedAttribute {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.fed[fed]
	if !ok {
		return nil
	}
	var released []ReleasedAttribute
	for k, rec := range st.owners {
		if rec.owner != h {
			continue
		}
		released = append(released, ReleasedAttribute{Object: k.obj, Attribute: k.attr})
		delete(st.owners, k)
	}
	sort.Slice(released, func(i, j int) bool {
		if released[i].Object != released[j].Object {
			return released[i].Object < released[j].Object
		}
		return released[i].Attribute < released[j].Attribute
	})
	return released
}

// CancelPendingFor cancels every pending divest / acquire keyed by
// the resigning federate. Used by the CANCEL_PENDING_OWNERSHIP and
// CANCEL_THEN_DELETE_THEN_DIVEST resign actions.
//
// Returns the count of cancellations applied (for logging /
// observability).
func (m *Manager) CancelPendingFor(
	_ context.Context,
	fed core.FederationName,
	h core.FederateHandle,
) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.fed[fed]
	if !ok {
		return 0
	}
	cancelled := 0
	// Drop pending divests where this federate is the owner.
	for k, pd := range st.pendingDivests {
		if pd.owner == h {
			delete(st.pendingDivests, k)
			cancelled++
		}
	}
	// Drop pending acquires where this federate is the acquirer.
	for k := range st.pendingAcquires {
		if k.acquirer == h {
			delete(st.pendingAcquires, k)
			cancelled++
		}
	}
	return cancelled
}
