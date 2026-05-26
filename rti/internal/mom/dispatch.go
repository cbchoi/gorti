// HLAmanager interaction dispatch (M20.3, IEEE 1516.1-2010 §10).
//
// Federates can drive RTI control services by SENDING interactions
// from the HLAmanager class tree, instead of calling the matching
// service RPCs directly. The dispatch layer intercepts these
// interactions before the normal fanout — they are NOT broadcast to
// subscribers like ordinary interactions.
//
// Wiring (M20.3 scaffold):
//   - object.Registry.SendInteraction looks up the class name via
//     the FOM handle. If it starts with "HLAmanager." AND a
//     Dispatcher is wired AND a handler exists for that class, the
//     dispatcher runs and fanout is suppressed.
//   - Otherwise the existing fanout path runs (HLAmanager classes
//     without registered handlers are broadcast as regular
//     interactions — matches the spec's "unhandled classes are
//     ordinary" fallback).
//
// Per-interaction handlers live in handlers_*.go; the dispatch
// table is populated by NewDispatcher.

package mom

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// HLAmanagerPrefix is the FOM class-name prefix every dispatchable
// HLAmanager interaction starts with.
const HLAmanagerPrefix = "HLAmanager."

// Handler is the per-class entry point for an HLAmanager interaction.
// ``params`` is the federate-supplied (parameter handle → encoded bytes)
// map, identical to what fanoutReceive would pass to subscribers.
//
// Handlers are responsible for ACK / response: most HLArequest*
// interactions emit an HLAreport* response back to the sender via the
// returned ``response`` slice. Empty response is OK — fire-and-forget
// HLAadjust.* interactions (HLAsetSwitches) return nil.
type Handler func(
	ctx context.Context,
	dctx DispatchContext,
	sender core.FederateHandle,
	params map[core.ParameterHandle][]byte,
) ([]ResponseInteraction, error)

// DispatchContext carries the per-call references the handler needs
// to interpret parameters (FOM lookup) and invoke other services
// (the actual MOM manager for HLAsetSwitches, the savepoint manager
// for HLArequestFederationSave, etc.). Populated by NewDispatcher.
type DispatchContext struct {
	Federation core.FederationName
	FOM        core.FOMHandle
	FOMNames   core.FOMHandleNameLookup
	MOM        *Manager
}

// ResponseInteraction is one HLAreport* response the handler wants
// the dispatcher to emit back to the sender. The dispatcher resolves
// the FOM handle of ``ClassName`` and produces an outbound interaction
// that fanoutReceive would have delivered.
type ResponseInteraction struct {
	ClassName string
	// Params is name-keyed (the dispatcher resolves to handles via
	// FOM lookup). Empty bytes for parameters the federate doesn't
	// care about; absent keys are omitted on the wire.
	Params map[string][]byte
}

// Dispatcher holds the class-name → Handler table.
type Dispatcher struct {
	mu       sync.RWMutex
	handlers map[string]Handler
	mom      *Manager
}

// NewDispatcher wires the default M20.3+ handler catalog.
// ``mom`` is captured so handlers can mutate MOM state (switch
// updates, etc.). Wave-by-wave handlers are registered in the M20
// sub-milestones.
func NewDispatcher(mom *Manager) *Dispatcher {
	d := &Dispatcher{
		handlers: make(map[string]Handler),
		mom:      mom,
	}
	// M20.4+ registers concrete handlers; M20.3 scaffold ships only
	// the empty dispatcher + the registration entry point.
	registerSwitchHandlers(d)
	return d
}

// Register associates a handler with an FOM class name. Overwrites
// any prior registration for that name (idempotent).
func (d *Dispatcher) Register(className string, h Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[className] = h
}

// Lookup returns the handler for ``className`` and whether one is
// registered. Used by object.Registry's send path to decide between
// dispatch and fallback fanout.
func (d *Dispatcher) Lookup(className string) (Handler, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	h, ok := d.handlers[className]
	return h, ok
}

// IsManagerClass reports whether ``className`` is in the HLAmanager
// subtree. Cheap prefix check; callers gate the handler lookup
// behind this to avoid touching the map on every interaction.
func (d *Dispatcher) IsManagerClass(className string) bool {
	return strings.HasPrefix(className, HLAmanagerPrefix)
}

// Dispatch resolves the handler for the class, invokes it, then
// (M20.6) emits any response interactions back to the sender. For
// M20.3 the response path is recorded but not yet wired into the
// outbox; M20.6 adds the actual emit step.
//
// Returns nil when:
//   - the class name is not in the HLAmanager subtree (caller takes
//     the regular fanout path)
//   - no handler is registered for an HLAmanager class (the
//     interaction is silently dropped per IEEE 1516.1 §10.4 —
//     "unhandled management interactions are no-ops")
//
// Returns the handler's error otherwise.
func (d *Dispatcher) Dispatch(
	ctx context.Context,
	fed core.FederationName,
	className string,
	sender core.FederateHandle,
	params map[core.ParameterHandle][]byte,
	fom core.FOMHandle,
	fomNames core.FOMHandleNameLookup,
) error {
	if !d.IsManagerClass(className) {
		return nil
	}
	h, ok := d.Lookup(className)
	if !ok {
		return nil
	}
	dctx := DispatchContext{
		Federation: fed,
		FOM:        fom,
		FOMNames:   fomNames,
		MOM:        d.mom,
	}
	responses, err := h(ctx, dctx, sender, params)
	if err != nil {
		return fmt.Errorf("mom dispatch %s: %w", className, err)
	}
	// M20.6 will emit responses[] back to the sender via the outbox.
	// For M20.3 we discard so the dispatch path is observable but
	// the wire side stays untouched.
	_ = responses
	return nil
}
