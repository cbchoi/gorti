package core

import "context"

// OwnershipCoordinator owns per-federation, per-(object, attribute)
// attribute-ownership state and the divest/acquire two-phase transfer
// protocol (IEEE 1516.1-2010 §7). Phase 1 of the research-platform
// refactor (docs/research-platform.md §5.2) carves this out as the
// service-level interface so alternative implementations
// (market-based, optimistic, authority-based) can plug in without
// forking the gRPC handler or composition root.
//
// Production impl: rti/internal/ownership.Manager.
//
// Concurrency: implementations must be goroutine-safe.
//
// The eight §7 RPC methods plus RegisterInitialOwnership are exposed
// because they have current consumers (gRPC handler + composition
// root's object.Registry OnRegister hook). The private
// `fanoutAssumption` helper used internally by NegotiatedDivest is
// deliberately NOT in the interface — it is an implementation detail
// of the production manager.
type OwnershipCoordinator interface {
	// RegisterInitialOwnership records the initial owner of every
	// attribute in attrs for object obj in federation fed. Called by
	// object.Registry's OnRegister hook so subsequent §7 calls have
	// ground-truth ownership state to consult.
	RegisterInitialOwnership(
		fed FederationName,
		owner FederateHandle,
		obj ObjectHandle,
		attrs []AttributeHandle,
	)

	// UnconditionalDivest implements §7.2.
	UnconditionalDivest(
		ctx context.Context,
		fed FederationName,
		owner FederateHandle,
		obj ObjectHandle,
		attrs []AttributeHandle,
	) error

	// NegotiatedDivest implements §7.3.
	NegotiatedDivest(
		ctx context.Context,
		fed FederationName,
		owner FederateHandle,
		obj ObjectHandle,
		attrs []AttributeHandle,
		tag []byte,
	) error

	// Acquire implements §7.4.
	Acquire(
		ctx context.Context,
		fed FederationName,
		acquirer FederateHandle,
		obj ObjectHandle,
		attrs []AttributeHandle,
		tag []byte,
	) error

	// CancelDivest implements §7.5.
	CancelDivest(
		ctx context.Context,
		fed FederationName,
		owner FederateHandle,
		obj ObjectHandle,
		attrs []AttributeHandle,
	) error

	// CancelAcquire implements §7.6.
	CancelAcquire(
		ctx context.Context,
		fed FederationName,
		acquirer FederateHandle,
		obj ObjectHandle,
		attrs []AttributeHandle,
	) error

	// DivestIfWanted implements §7.7.
	DivestIfWanted(
		ctx context.Context,
		fed FederationName,
		owner FederateHandle,
		obj ObjectHandle,
		attrs []AttributeHandle,
	) error

	// QueryOwnership implements §7.8. Returns (owner, true) on a known
	// (obj, attr) ownership; (0, false) when the attribute is unowned
	// (mid-transfer, never registered, etc.).
	QueryOwnership(
		fed FederationName,
		obj ObjectHandle,
		attr AttributeHandle,
	) (FederateHandle, bool)

	// IsOwnedBy implements §7.9. Convenience wrapper over QueryOwnership.
	IsOwnedBy(
		fed FederationName,
		h FederateHandle,
		obj ObjectHandle,
		attr AttributeHandle,
	) bool

	// --- Read-only introspection (rtid-TUI Phase 1) ----------------------

	// Snapshot returns aggregate ownership counters for the
	// AdminService handler. Read-only; cheap.
	Snapshot(fed FederationName) OwnershipSnapshot
}

// OwnershipSnapshot is the federation-wide ownership rollup for the
// AdminService Snapshot RPC.
//
// Phase 1 keeps this minimal — counts only — so the handler can show
// "ownership transfers in flight" in the TUI without exposing
// per-attribute history (the design doc §3.2 explicitly excludes
// per-attribute ownership history from the snapshot).
type OwnershipSnapshot struct {
	OwnedAttributesCount uint32
	PendingDivestsCount  uint32
	PendingAcquiresCount uint32
}
