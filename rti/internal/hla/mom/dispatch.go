// HLAmanager dispatch uses a bounded handler catalog. Unsupported classes
// retain the existing empty result; that is not evidence of implemented MOM
// behavior. Per-interaction handlers live in handlers_*.go.

package mom

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// HLAmanagerPrefix is the FOM class-name prefix every dispatchable
// HLAmanager interaction starts with.
const HLAmanagerPrefix = "HLAmanager."

// Handler is the per-class entry point for an HLAmanager interaction.
// “params“ is the federate-supplied (parameter handle → encoded bytes)
// map, identical to what fanoutReceive would pass to subscribers.
//
// Handlers are responsible for ACK / response: most HLArequest*
// interactions emit an HLAreport* response back to the sender via the
// returned “response“ slice. Empty response is OK — fire-and-forget
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
	Counters   CounterSource
	Catalog    CatalogSources
	Executor   MOMServiceExecutor
}

// CounterKind identifies the traffic measured at the target federate. Received
// counts use the sent/registered class, not the subscriber's known class.
type CounterKind string

const (
	InteractionsSent     CounterKind = "interactionsSent"
	InteractionsReceived CounterKind = "interactionsReceived"
	UpdatesSent          CounterKind = "updatesSent"
	ReflectionsReceived  CounterKind = "reflectionsReceived"
)

// TransportationCounts is a detached snapshot for one transportation type.
// Counts are keyed by interaction class for interactions, object class otherwise.
// The source must include each supported transportation, even when its map is
// empty, and must not turn unavailable/uncollected data into empty counts.
type TransportationCounts struct {
	Transportation string
	Counts         map[uint64]uint32
}

// CounterSource supplies actual per-target, per-class traffic history. Aggregate
// MOM attributes cannot reconstruct this information. Production traffic hooks
// must populate a source before non-synthetic counter reports can be supplied.
type CounterSource func(context.Context, core.FederationName, core.FederateHandle, CounterKind) ([]TransportationCounts, error)

// ResponseInteraction is one HLAreport* response the handler wants
// the dispatcher to emit back to the sender. The dispatcher resolves
// the FOM handle of “ClassName“ and produces an outbound interaction
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
// response actually reaches the wire. Requests producing reports require an
// emitter; switch adjustments do not.
//
// “fom“ and “fomNames“ are forwarded so the emitter can resolve
// the response class name + parameter names to handles before
// building the wire proto. Production wires this via cmd/rtid; the
// recording emitter in tests just captures the name-keyed form.
type ResponseEmitter func(
	ctx context.Context,
	fed core.FederationName,
	recipient core.FederateHandle,
	resp ResponseInteraction,
	fom core.FOMHandle,
	fomNames core.FOMHandleNameLookup,
) error

// Dispatcher holds the class-name → Handler table.
type Dispatcher struct {
	reportMu sync.Mutex
	mu       sync.RWMutex
	handlers map[string]Handler
	mom      *Manager
	// Emitter for HLAreport* responses.
	emitter  ResponseEmitter
	counters CounterSource
	catalog  CatalogSources
	executor MOMServiceExecutor
}

// NewDispatcher wires the default M20.3+ handler catalog.
// “mom“ is captured so handlers can mutate MOM state (switch
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
	registerCatalogHandlers(d)
	d.Register(ClassSetTiming, handleSetTiming)
	registerServiceHandlers(d)
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

// SetCounterSource installs the detailed traffic snapshot source. A nil source
// makes counter requests fail explicitly rather than fabricate class counts.
func (d *Dispatcher) SetCounterSource(source CounterSource) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.counters = source
}

// Register associates a handler with an FOM class name. Overwrites
// any prior registration for that name (idempotent).
func (d *Dispatcher) Register(className string, h Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[strings.TrimPrefix(className, "HLAinteractionRoot.")] = h
}

// Lookup returns the handler for “className“ and whether one is
// registered. Used by object.Registry's send path to decide between
// dispatch and fallback fanout.
func (d *Dispatcher) Lookup(className string) (Handler, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, h, ok := d.lookupLocked(className)
	return h, ok
}

func (d *Dispatcher) lookupLocked(className string) (string, Handler, bool) {
	name := strings.TrimPrefix(className, "HLAinteractionRoot.")
	for {
		if h, ok := d.handlers[name]; ok {
			return name, h, true
		}
		if i := strings.LastIndexByte(name, '.'); i >= 0 {
			name = name[:i]
		} else {
			return "", nil, false
		}
	}
}

// IsManagerClass reports whether “className“ is in the HLAmanager
// subtree. Cheap prefix check; callers gate the handler lookup
// behind this to avoid touching the map on every interaction.
func (d *Dispatcher) IsManagerClass(className string) bool {
	_, ok := d.Lookup(className)
	return ok
}

// Dispatch invokes a registered handler and emits its responses to the sender.
// Unknown classes retain the legacy empty-result behavior, not a conformance
// claim. Handler/source/encoding/delivery errors are returned to the caller.
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
	d.mu.RLock()
	leaf, h, ok := d.lookupLocked(className)
	d.mu.RUnlock()
	if !ok {
		return nil
	}
	d.mu.RLock()
	counters := d.counters
	catalog := d.catalog
	executor := d.executor
	d.mu.RUnlock()
	dctx := DispatchContext{
		Federation: fed,
		FOM:        fom,
		FOMNames:   fomNames,
		MOM:        d.mom,
		Counters:   counters,
		Catalog:    catalog,
		Executor:   executor,
	}
	{
		var err error
		projected, projectionErr := projectManagerParameters(dctx, className, leaf, params)
		err = projectionErr
		if err != nil {
			if strings.HasPrefix(leaf, ServiceClassPrefix) {
				target, _ := decodeSwitchTarget(dctx, className, params)
				return errors.Join(err, d.reportServiceMOMFailure(ctx, dctx, leaf, sender, target, err))
			}
			return err
		}
		params = projected
	}
	responses, err := h(ctx, dctx, sender, params)
	if err != nil {
		if !strings.HasPrefix(leaf, ServiceClassPrefix) {
			if reportErr := d.reportMOMFailure(ctx, dctx, leaf, sender, params, err); reportErr != nil {
				err = errors.Join(err, reportErr)
			}
		}
		return fmt.Errorf("mom dispatch %s: %w", className, err)
	}
	// M20.5 — invoke the response emitter (if wired) for each
	// HLAreport* response the handler produced. Send errors are
	// logged via the emitter's caller (M20.6 wires this through the
	// MOM Outbox which logs internally).
	return d.emitResponses(ctx, fed, sender, responses, fom, fomNames, "")
}
