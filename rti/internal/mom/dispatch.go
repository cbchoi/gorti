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

// ResponseEmitter delivers an HLAreport* response back to the
// requesting federate. M20.5 lets handlers produce responses;
// M20.6 wires the production emitter to the MOM Outbox so the
// response actually reaches the wire. Nil emitter discards
// responses (M20.3 default behavior).
type ResponseEmitter func(
	ctx context.Context,
	fed core.FederationName,
	recipient core.FederateHandle,
	resp ResponseInteraction,
) error

// Dispatcher holds the class-name → Handler table.
type Dispatcher struct {
	mu       sync.RWMutex
	handlers map[string]Handler
	mom      *Manager
	// M20.5 — emitter for HLAreport* responses. nil = discard.
	emitter ResponseEmitter
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
	registerSwitchHandlers(d)
	// M20.5 — HLArequest* counter handlers.
	registerRequestHandlers(d)
	return d
}

// SetEmitter wires the ResponseEmitter the dispatcher invokes for
// each handler-produced HLAreport* response. M20.6 calls this from
// cmd/rtid composition to forward responses through the MOM's
// Outbox.
func (d *Dispatcher) SetEmitter(e ResponseEmitter) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.emitter = e
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
	// M20.5 — invoke the response emitter (if wired) for each
	// HLAreport* response the handler produced. Send errors are
	// logged via the emitter's caller (M20.6 wires this through the
	// MOM Outbox which logs internally).
	d.mu.RLock()
	emit := d.emitter
	d.mu.RUnlock()
	if emit != nil {
		for _, r := range responses {
			if err := emit(ctx, fed, sender, r); err != nil {
				return fmt.Errorf("mom emit %s: %w", r.ClassName, err)
			}
		}
	}
	return nil
}
