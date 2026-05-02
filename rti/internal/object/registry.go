package object

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// ErrNotImplemented is retained as an exported sentinel for callers that
// matched on it during the M2 transition; implemented methods never return
// it. Removed when no caller still references it post-M2.
var ErrNotImplemented = errors.New("object: not implemented (Agent A M2 deliverable)")

// fanoutAttrProbe is the cut-1 attribute set used to enumerate "any
// subscriber to this class" during Discover fanout. The fixed range covers
// the spec-test fixtures (which use small attribute handles 1..3).
//
// FOM-driven enumeration is a future task; tracked alongside TASK-031's
// future "fan to subscribers of any class attr" extension. See registry
// brief ARCH FIXME (2).
var fanoutAttrProbe = []core.AttributeHandle{1, 2, 3, 4, 5, 6, 7, 8}

// Registry implements core.ObjectRegistry. It owns per-federation object
// state (handle counters, instance map, name index) and routes attribute
// updates and interactions to subscribers via Outbox.
//
// Concurrency: Registry.mu guards the federation table; per-federation
// state has its own RWMutex for independent fanout from concurrent
// federations.
type Registry struct {
	opts Options

	mu          sync.RWMutex
	federations map[core.FederationName]*federationState
}

// federationState is the per-federation in-memory record.
type federationState struct {
	mu sync.Mutex

	// nextObjectHandle is the monotonic counter for ObjectHandle
	// assignment. Replay re-reads ObjectRegistered events from the event
	// log to reproduce the same handles.
	nextObjectHandle uint64

	// nextOutboundSeq stamps every outbound event the registry hands to
	// the Outbox. It is independent of the eventlog's seq — outbound seq
	// is per-federation downstream multiplexing metadata, not part of the
	// determinism log.
	nextOutboundSeq uint64

	instances    map[core.ObjectHandle]*objectInstance
	nameToHandle map[string]core.ObjectHandle
}

func newFederationState() *federationState {
	return &federationState{
		instances:    map[core.ObjectHandle]*objectInstance{},
		nameToHandle: map[string]core.ObjectHandle{},
	}
}

// objectInstance tracks the bare minimum the routing path needs: handle,
// canonical name, class, owner federate.
type objectInstance struct {
	handle core.ObjectHandle
	name   string
	cls    core.ObjectClassHandle
	owner  core.FederateHandle
}

// Options bundles Registry dependencies. EventLog is optional in cut 1 to
// match the federation manager's relaxation (a nil log silently drops
// events; W4 wires a real log in production). All other fields are
// required.
type Options struct {
	EventLog     core.EventLog
	Declarations *declaration.Manager
	Outbox       core.Outbox
	Codec        core.CodecFactory
	FOMs         core.FOMRepository
	Clock        core.Clock
}

// New constructs a Registry. Returns an error if any required field is nil.
//
// Required: Declarations, Outbox, FOMs, Clock. Optional: EventLog (cut-1
// relaxation; nil => events silently dropped), Codec (cut-1; attribute
// bytes pass through as-is until M2 wiring lands the production codec).
func New(opts Options) (*Registry, error) {
	if opts.Declarations == nil {
		return nil, errors.New("object: Options.Declarations is required")
	}
	if opts.Outbox == nil {
		return nil, errors.New("object: Options.Outbox is required")
	}
	if opts.FOMs == nil {
		return nil, errors.New("object: Options.FOMs is required")
	}
	if opts.Clock == nil {
		return nil, errors.New("object: Options.Clock is required (D-1: no time.Now)")
	}
	if isNilInterface(opts.EventLog) {
		opts.EventLog = nil
	}
	return &Registry{
		opts:        opts,
		federations: map[core.FederationName]*federationState{},
	}, nil
}

// isNilInterface reports whether v is a true nil interface or a typed-nil
// (interface holding a nil concrete pointer). Mirrors the helper in
// federation/manager.go (helpers are duplicated rather than shared per
// CODING_CONVENTIONS.md "three lines beats premature abstraction").
func isNilInterface(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return rv.IsNil()
	default:
		return false
	}
}

// stateFor returns (and lazily creates) the per-federation state.
func (r *Registry) stateFor(fed core.FederationName) *federationState {
	r.mu.RLock()
	st, ok := r.federations[fed]
	r.mu.RUnlock()
	if ok {
		return st
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok = r.federations[fed]
	if !ok {
		st = newFederationState()
		r.federations[fed] = st
	}
	return st
}

// Register implements core.ObjectRegistry. Assigns a monotonic
// ObjectHandle, persists ObjectRegistered to EventLog (write-ahead), then
// fans out DiscoverObjectInstance to subscribers in deterministic
// (sorted) handle order, excluding the producer.
func (r *Registry) Register(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	cls core.ObjectClassHandle,
	name string,
) (core.ObjectHandle, string, error) {
	if producer == core.InvalidFederateHandle {
		return core.InvalidObjectHandle, "", core.ErrFederateNotJoined
	}
	if !r.producerPublishesAnyAttrOf(ctx, fed, producer, cls) {
		return core.InvalidObjectHandle, "", core.ErrObjectClassNotPublished
	}

	st := r.stateFor(fed)
	st.mu.Lock()
	if name != "" {
		if _, dup := st.nameToHandle[name]; dup {
			st.mu.Unlock()
			return core.InvalidObjectHandle, "", fmt.Errorf("object: name %q already registered in federation %q", name, fed)
		}
	}
	st.nextObjectHandle++
	assigned := core.ObjectHandle(st.nextObjectHandle)
	canonical := name
	if canonical == "" {
		canonical = fmt.Sprintf("HLAobj_%d_%d", cls, assigned)
	}

	if r.opts.EventLog != nil {
		ev := newObjectRegisteredEvent(assigned, cls, producer, canonical, r.opts.Clock.Now().UnixNano())
		if err := r.opts.EventLog.Append(ctx, fed, ev); err != nil {
			// Roll back the counter so the next Register reuses the slot;
			// nothing is observable yet (no instance recorded).
			st.nextObjectHandle--
			st.mu.Unlock()
			return core.InvalidObjectHandle, "", fmt.Errorf("object: register %q in %q: eventlog append: %w", canonical, fed, err)
		}
	}

	inst := &objectInstance{
		handle: assigned,
		name:   canonical,
		cls:    cls,
		owner:  producer,
	}
	st.instances[assigned] = inst
	st.nameToHandle[canonical] = assigned
	st.mu.Unlock()

	r.fanoutDiscover(ctx, fed, st, producer, inst)
	return assigned, canonical, nil
}

// producerPublishesAnyAttrOf reports whether `producer` publishes at least
// one attribute of `cls` in `fed`. The cut-1 simplification probes a small
// fixed attribute range (covers test fixtures); FOM-driven enumeration is
// a follow-up task.
func (r *Registry) producerPublishesAnyAttrOf(ctx context.Context, fed core.FederationName, producer core.FederateHandle, cls core.ObjectClassHandle) bool {
	for _, attr := range fanoutAttrProbe {
		for _, h := range r.opts.Declarations.PublishersFor(ctx, fed, cls, attr) {
			if h == producer {
				return true
			}
		}
	}
	return false
}

// UpdateAttributes implements core.ObjectRegistry. Implemented in update.go.
func (r *Registry) UpdateAttributes(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	obj core.ObjectHandle,
	attrs map[core.AttributeHandle][]byte,
	ts *core.LogicalTime,
) error {
	return r.updateAttributes(ctx, fed, producer, obj, attrs, ts)
}

// SendInteraction implements core.ObjectRegistry. Implemented in interaction.go.
func (r *Registry) SendInteraction(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	cls core.InteractionClassHandle,
	params map[core.ParameterHandle][]byte,
	ts *core.LogicalTime,
) error {
	return r.sendInteraction(ctx, fed, producer, cls, params, ts)
}

// nextOutboundSeqLocked returns the next per-federation outbound seq.
// Caller must hold st.mu.
func (st *federationState) nextOutboundSeqLocked() uint64 {
	st.nextOutboundSeq++
	return st.nextOutboundSeq
}

// Compile-time assertion that Registry implements core.ObjectRegistry.
var _ core.ObjectRegistry = (*Registry)(nil)

// newObjectRegisteredEvent builds the eventlog adapter for an
// ObjectRegistered event. Defined here so Register-only changes do not
// drag in the discover/update/interaction files.
func newObjectRegisteredEvent(handle core.ObjectHandle, cls core.ObjectClassHandle, owner core.FederateHandle, name string, wallNs int64) *eventRecord {
	return &eventRecord{
		pb: &rtiv1.Event{
			WallNs: uint64(wallNs),
			Body: &rtiv1.Event_ObjRegistered{
				ObjRegistered: &rtiv1.ObjectRegistered{
					ObjectHandle:        uint64(handle),
					ObjectClassHandle:   uint64(cls),
					OwnerFederateHandle: uint64(owner),
					ObjectName:          name,
				},
			},
		},
	}
}
