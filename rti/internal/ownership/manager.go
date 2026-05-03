package ownership

import (
	"context"
	"errors"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ErrNotImplemented is returned by stub methods until Agent A implements
// them in M8. Spec tests in rti/spec/M8/ fail RED with this initially.
var ErrNotImplemented = errors.New("ownership: not implemented (Agent A M8 deliverable)")

// Options bundles Manager dependencies.
type Options struct {
	Outbox   core.Outbox
	EventLog core.EventLog
}

// Manager owns per-federation, per-(object, attribute) ownership state.
// Goroutine-safe.
//
// FROZEN-shape per docs/srs.md FR-OWN-1..6. Agent A implements bodies
// in M8 W1.
type Manager struct {
	opts Options
}

// New constructs a Manager. Returns an error if any required Options
// field is nil.
func New(opts Options) (*Manager, error) {
	_ = opts
	return &Manager{opts: opts}, ErrNotImplemented
}

// UnconditionalDivest implements §7.2 — unconditionalAttributeOwnershipDivestiture.
// FR-OWN-1. Cut 1 already has this via federation resign; M8 promotes it
// to a first-class API call.
func (m *Manager) UnconditionalDivest(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
) error {
	_ = ctx
	_ = fed
	_ = owner
	_ = obj
	_ = attrs
	return ErrNotImplemented
}

// NegotiatedDivest implements §7.3 — negotiatedAttributeOwnershipDivestiture.
// Phase 1 of the two-phase ownership transfer protocol (FR-OWN-2).
//
// The owner announces its desire to divest; the RTI broadcasts
// requestAttributeOwnershipAssumption to subscribers. A subscriber
// then calls Acquire to take ownership.
//
// Errors:
//   - core.ErrObjectNotFound
//   - core.ErrAttributeNotOwned (caller is not the current owner)
//   - core.ErrFederationHalted
func (m *Manager) NegotiatedDivest(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
	tag []byte,
) error {
	_ = ctx
	_ = fed
	_ = owner
	_ = obj
	_ = attrs
	_ = tag
	return ErrNotImplemented
}

// Acquire implements §7.4 — attributeOwnershipAcquisition. Phase 2 of
// the two-phase protocol. The acquirer requests ownership; if the
// current owner has already called NegotiatedDivest, the transfer
// completes and both parties get callbacks.
func (m *Manager) Acquire(
	ctx context.Context,
	fed core.FederationName,
	acquirer core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
	tag []byte,
) error {
	_ = ctx
	_ = fed
	_ = acquirer
	_ = obj
	_ = attrs
	_ = tag
	return ErrNotImplemented
}

// CancelDivest implements §7.5 — cancelNegotiatedAttributeOwnershipDivestiture.
// Owner withdraws a pending NegotiatedDivest before any acquirer claims it.
func (m *Manager) CancelDivest(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
) error {
	_ = ctx
	_ = fed
	_ = owner
	_ = obj
	_ = attrs
	return ErrNotImplemented
}

// CancelAcquire implements §7.6 — cancelAttributeOwnershipAcquisition.
// Acquirer withdraws a pending Acquire request.
func (m *Manager) CancelAcquire(
	ctx context.Context,
	fed core.FederationName,
	acquirer core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
) error {
	_ = ctx
	_ = fed
	_ = acquirer
	_ = obj
	_ = attrs
	return ErrNotImplemented
}

// DivestIfWanted implements §7.7 — attributeOwnershipDivestitureIfWanted.
// Owner divests opportunistically — if no subscriber wants it, ownership
// stays with the owner.
func (m *Manager) DivestIfWanted(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
) error {
	_ = ctx
	_ = fed
	_ = owner
	_ = obj
	_ = attrs
	return ErrNotImplemented
}

// QueryOwnership implements §7.8 — queryAttributeOwnership. Returns the
// current owner of (obj, attr). Returns 0 + false if the attribute is
// unowned (e.g. mid-transfer).
func (m *Manager) QueryOwnership(
	fed core.FederationName,
	obj core.ObjectHandle,
	attr core.AttributeHandle,
) (core.FederateHandle, bool) {
	_ = fed
	_ = obj
	_ = attr
	return 0, false
}

// IsOwnedBy implements §7.9 — isAttributeOwnedByFederate. Convenience
// wrapper over QueryOwnership.
func (m *Manager) IsOwnedBy(
	fed core.FederationName,
	h core.FederateHandle,
	obj core.ObjectHandle,
	attr core.AttributeHandle,
) bool {
	owner, ok := m.QueryOwnership(fed, obj, attr)
	return ok && owner == h
}
