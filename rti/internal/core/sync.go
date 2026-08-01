package core

import "context"

// SyncCoordinator owns synchronization-point state per federation
// (IEEE 1516.1-2010 §4.6 / §4.7). Phase 1 of the research-platform
// refactor (docs/research-platform.md §5.1) carves this out as the
// service-level interface so alternative implementations can plug in
// without forking the gRPC handler or composition root.
//
// Only methods with an actual current consumer are exposed; per
// Phase 1's "minimal surface" rule, additions are reactive (when a
// consumer needs them).
//
// Production impl: rti/internal/sync.Manager.
//
// Concurrency: implementations must be goroutine-safe — gRPC handlers
// invoke RPCs concurrently and the per-federation serialization is the
// implementation's responsibility (see docs/sdd.md §1.4).
type SyncCoordinator interface {
	// Register implements registerFederationSynchronizationPoint. When
	// requiredFederates is nil, the implementation derives the implicit
	// "all currently joined federates" set per its members-resolver
	// semantics.
	Register(
		ctx context.Context,
		fed FederationName,
		label string,
		tag []byte,
		requiredFederates []FederateHandle,
	) error

	// Achieve implements synchronizationPointAchieved. When the last
	// required federate calls Achieve, the implementation emits
	// federationSynchronized to all of them.
	Achieve(
		ctx context.Context,
		fed FederationName,
		h FederateHandle,
		label string,
	) error

	// --- Read-only introspection (rtid-TUI Phase 1) ----------------------

	// Snapshot returns the current sync-point state for a federation
	// for the AdminService handler. The slice is sorted by label
	// (deterministic).
	Snapshot(fed FederationName) []SyncPointInfo
}

// SyncPointSnapshotState mirrors sync.SyncPointState in the core
// package so the AdminService handler can consume it via the
// SyncCoordinator interface.
type SyncPointSnapshotState int

const (
	// SyncPointStateUnknown — no such sync point.
	SyncPointStateUnknown SyncPointSnapshotState = iota
	// SyncPointStateAnnounced — registered, awaiting Achieve calls.
	SyncPointStateAnnounced
	// SyncPointStateAchieved — every required federate has called
	// Achieve.
	SyncPointStateAchieved
)

// SyncPointInfo is one sync point's per-federation state.
type SyncPointInfo struct {
	Label           string
	State           SyncPointSnapshotState
	RequiredHandles []FederateHandle
	AchievedHandles []FederateHandle
}
