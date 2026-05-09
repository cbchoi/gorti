package core

import "context"

// ObjectRegistry is the entry point for object/interaction operations.
// Discover/Reflect/Receive callbacks are emitted via Outbox, not returned here.
type ObjectRegistry interface {
	Register(
		ctx context.Context,
		fed FederationName,
		producer FederateHandle,
		cls ObjectClassHandle,
		name string, // empty = RTI generates
	) (ObjectHandle, string, error)

	UpdateAttributes(
		ctx context.Context,
		fed FederationName,
		producer FederateHandle,
		obj ObjectHandle,
		attrs map[AttributeHandle][]byte,
		ts *LogicalTime, // nil = RO; non-nil = TSO
	) error

	SendInteraction(
		ctx context.Context,
		fed FederationName,
		producer FederateHandle,
		cls InteractionClassHandle,
		params map[ParameterHandle][]byte,
		ts *LogicalTime,
	) error

	// Delete — IEEE 1516.1-2010 §6.16. M23.
	// Owner-only; subscribers receive RemoveObjectInstance via Outbox.
	// Errors: ErrObjectHandleInvalid, ErrObjectNotOwned.
	Delete(
		ctx context.Context,
		fed FederationName,
		deleter FederateHandle,
		obj ObjectHandle,
		ts *LogicalTime,
		tag []byte,
	) error

	// LocalDelete — IEEE 1516.1-2010 §6.18. M23.
	// Federate-local cleanup; no peer notification. Cut-1 simplification:
	// records the event for replay but does NOT mutate global instance
	// state — other subscribers continue to see the instance.
	LocalDelete(
		ctx context.Context,
		fed FederationName,
		federate FederateHandle,
		obj ObjectHandle,
	) error

	// RequestAttributeValueUpdate — IEEE 1516.1-2010 §6.24. M23.
	// Resolves the owner of obj and emits a ProvideAttributeValueUpdate
	// event with the requested attributes + tag.
	// Errors: ErrObjectHandleInvalid.
	RequestAttributeValueUpdate(
		ctx context.Context,
		fed FederationName,
		requester FederateHandle,
		obj ObjectHandle,
		attrs []AttributeHandle,
		tag []byte,
	) error

	// RequestClassAttributeValueUpdate — IEEE 1516.1-2010 §6.25. M23.
	// Class-scoped variant: every unique owner of any instance of the
	// class receives a ProvideAttributeValueUpdate event.
	RequestClassAttributeValueUpdate(
		ctx context.Context,
		fed FederationName,
		requester FederateHandle,
		cls ObjectClassHandle,
		attrs []AttributeHandle,
		tag []byte,
	) error

	// --- Read-only introspection (rtid-TUI Phase 1) ----------------------

	// Snapshot returns aggregate object-instance counts for the
	// AdminService handler. Read-only; cheap.
	Snapshot(fed FederationName) ObjectSnapshot
}

// ObjectSnapshot is the federation-wide object rollup for the
// AdminService Snapshot RPC.
type ObjectSnapshot struct {
	InstanceCount uint32
}
