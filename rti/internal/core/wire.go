package core

import "context"

// WireApplier is an OPTIONAL registry fast path for the LocalLRC stream
// apply loop (W3). It accepts the wire-shaped uint64-keyed maps exactly
// as the proto decoder produced them, skipping the unary handler's
// typed-map re-box and the registry's defensive copies.
//
// OWNERSHIP CONTRACT (load-bearing — read before adding a caller):
//
//   - The caller transfers ownership of `attrs` / `params` to the
//     registry. The map MUST be a freshly decoded proto map that
//     nothing else retains — no other goroutine, no request object the
//     caller will reuse, no pool. The registry stores the map directly
//     into outbound ReflectAttributeValues / ReceiveInteraction
//     envelopes and eventlog records WITHOUT copying, so the caller
//     MUST NOT read or mutate the map (or its byte slices) after the
//     call returns.
//   - Only the LocalLRC apply path (transport/grpc localLRCService)
//     may call these methods. Every other entrypoint keeps the copying
//     semantics of core.ObjectRegistry.
//
// The interface is deliberately NOT part of core.ObjectRegistry: it is
// asserted ONCE at localLRCService construction, and callers fall back
// to the copying unary path when the composed registry does not
// implement it.
type WireApplier interface {
	// UpdateAttributesWire is UpdateAttributesRetractable(…, rh=0) with
	// wire-shaped attrs and transferred map ownership (see contract).
	UpdateAttributesWire(
		ctx context.Context,
		fed FederationName,
		producer FederateHandle,
		obj ObjectHandle,
		attrs map[uint64][]byte,
		ts *LogicalTime, // nil = RO; non-nil = TSO
	) error

	// SendInteractionWire is SendInteractionRetractable(…, rh=0) with
	// wire-shaped params and transferred map ownership (see contract).
	SendInteractionWire(
		ctx context.Context,
		fed FederationName,
		producer FederateHandle,
		cls InteractionClassHandle,
		params map[uint64][]byte,
		ts *LogicalTime,
	) error
}
