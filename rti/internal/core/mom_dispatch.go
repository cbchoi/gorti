// Package core — ManagementInteractionDispatcher interface (M20.3).
//
// The interface is satisfied by *mom.Dispatcher; object.Registry's
// SendInteraction calls it via Options.ManagementDispatch. Keeping
// the interface here (rather than importing mom from object) breaks
// the would-be import cycle: object → mom, mom → object can't both
// hold.

package core

import "context"

// ManagementInteractionDispatcher consumes incoming HLAmanager.*
// interactions and routes them to the corresponding handler.
// Implementations live in rti/internal/mom; tests in
// rti/internal/object pass a small stub.
type ManagementInteractionDispatcher interface {
	// IsManagerClass reports whether the class name falls in the
	// HLAmanager subtree. Callers gate the Dispatch call behind
	// this to avoid touching the handler map on every interaction.
	IsManagerClass(className string) bool

	// Dispatch looks up a handler for the class and runs it.
	// Returns nil when no handler is registered (the interaction
	// is silently dropped per IEEE 1516.1 §10 — unhandled
	// management interactions are no-ops).
	Dispatch(
		ctx context.Context,
		fed FederationName,
		className string,
		sender FederateHandle,
		params map[ParameterHandle][]byte,
		fom FOMHandle,
		fomNames FOMHandleNameLookup,
	) error
}
