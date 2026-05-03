package sync

import (
	"context"
	"errors"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ErrNotImplemented is returned by stub methods until Agent A implements
// them in M8. Spec tests in rti/spec/M8/ fail RED with this initially.
var ErrNotImplemented = errors.New("sync: not implemented (Agent A M8 deliverable)")

// SyncPointState reports the current state of a sync point in a
// federation. Returned by Manager.QueryState; mostly for tests + MOM
// (M11) to introspect state without reaching into internal storage.
type SyncPointState int

const (
	StateUnknown SyncPointState = iota
	// StateAnnounced — point registered; announce sent to all federates;
	// awaiting their achieved confirmations.
	StateAnnounced
	// StateAchieved — every required federate has called
	// synchronizationPointAchieved(label).
	StateAchieved
)

// Options bundles Manager dependencies. Required: Outbox, EventLog (for
// determinism — sync-point transitions are recorded so replay reproduces
// announce/achieve order byte-identically).
type Options struct {
	Outbox   core.Outbox
	EventLog core.EventLog
}

// Manager owns per-federation sync-point state. Goroutine-safe.
//
// FROZEN-shape per docs/srs.md FR-SYN-1..4. Agent A implements bodies
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

// Register implements IEEE 1516.1-2010 §4.6 — registerFederationSynchronizationPoint.
//
// label is the sync-point identifier. tag is opaque user data echoed in
// the announce callback. requiredFederates is the optional explicit set
// (nil = all currently joined federates).
//
// Errors:
//   - core.ErrSyncPointAlreadyRegistered if label already exists in fed
//   - core.ErrFederationHalted if federation is halted
func (m *Manager) Register(
	ctx context.Context,
	fed core.FederationName,
	label string,
	tag []byte,
	requiredFederates []core.FederateHandle,
) error {
	_ = ctx
	_ = fed
	_ = label
	_ = tag
	_ = requiredFederates
	return ErrNotImplemented
}

// Achieve implements IEEE 1516.1-2010 §4.7 —
// synchronizationPointAchieved. Called by a federate that has reached
// the sync point. When all required federates have called Achieve, the
// RTI emits federationSynchronized to all of them.
//
// Errors:
//   - core.ErrSyncPointNotRegistered if label doesn't exist
//   - core.ErrSyncPointAlreadyAchieved if this federate has already achieved
//   - core.ErrFederationHalted if federation is halted
func (m *Manager) Achieve(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
	label string,
) error {
	_ = ctx
	_ = fed
	_ = h
	_ = label
	return ErrNotImplemented
}

// QueryState returns the state of (fed, label). StateUnknown if no such
// sync point exists. Used by tests + MOM (M11).
func (m *Manager) QueryState(fed core.FederationName, label string) SyncPointState {
	_ = fed
	_ = label
	return StateUnknown
}
