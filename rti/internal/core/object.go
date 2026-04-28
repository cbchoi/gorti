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
}
