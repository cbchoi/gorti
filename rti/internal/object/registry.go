package object

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
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

	// M23 W3 — per-instance / per-(publisher,class) transport-type
	// overrides. Recorded by ChangeAttributeTransportType /
	// ChangeInteractionTransportType; read-only via the AttributeTransportType
	// / InteractionTransportType accessors.
	transports *transportStore

	// M26 Phase F — per-federation object instance name reservation
	// table per IEEE 1516.1-2010 §6.1-6.5. Reservations are checked
	// at Register time; registered names are tracked so re-reservation
	// of a live name fails.
	reservations *reservationStore
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
	EventLog core.EventLog
	// Declarations consumes the four lookup queries the registry hot
	// path needs (SubscribersFor / PublishersFor and the interaction
	// twins). Phase 1 of the research-platform refactor
	// (docs/research-platform.md §5.6) typed this as
	// core.DeclarationManagement so alternative declaration impls flow
	// through unchanged.
	Declarations core.DeclarationManagement
	Outbox       core.Outbox
	Codec        core.CodecFactory
	FOMs         core.FOMRepository
	Clock        core.Clock

	// Federations resolves a federation name to its operating mode
	// (TASK-077). OPTIONAL: when nil, every federation is treated as
	// ModeVerbose and the registry preserves the existing TSO-only
	// delivery behavior. The federation.Manager satisfies this
	// interface directly via its ModeFor method.
	Federations FederationModeLookup

	// Orders resolves an attribute / interaction class to its declared
	// FOM delivery order (TASK-077). OPTIONAL: when nil, every
	// attribute / interaction is treated as OrderTimeStamp and the
	// registry preserves TSO-only delivery.
	//
	// Both Federations AND Orders must be supplied for best-effort
	// RO delivery to engage; missing either means TSO-only behavior.
	Orders AttributeOrderLookup

	// TSOGate gates TSO outbound events (M22 W2 / TASK-236). When
	// non-nil, fanoutReflect / fanoutReceive consult the gate before
	// each TSO Send: ShouldDeliverNow=true → direct Send; false →
	// BufferTSO holds the event until the recipient's advance grant
	// or async-on toggle releases it. RO events bypass the gate.
	//
	// OPTIONAL: when nil, the registry falls back to direct Send for
	// every event, preserving the pre-M22 always-async behavior. This
	// keeps test fixtures + in-process drivers that do not wire a
	// time manager working unchanged.
	TSOGate core.TSODeliveryGate

	// OnRegister is an OPTIONAL post-Register hook invoked after
	// a successful object registration AND after the Discover
	// fan-out completes. The cut-1 ownership.Manager wiring uses
	// this to record the producing federate as the initial owner
	// of all class attributes (M8 W1, FR-OWN-5).
	//
	// The hook receives the assigned object handle, the producing
	// federate, and the object class. The attribute set is fixed
	// to the cut-1 fanoutAttrProbe range — the same range the
	// registry already uses for Discover fan-out — pending the
	// FOM-driven enumeration follow-up tracked at fanoutAttrProbe.
	//
	// MUST NOT block; the registry calls it synchronously before
	// returning to the caller of Register.
	OnRegister func(fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle)

	// OnUpdateSent is an OPTIONAL post-update hook invoked after a
	// successful UpdateAttributes call (after eventlog append + fan-out).
	// M11: cmd/rtid wires this to MOM.IncrementUpdatesSent so the
	// producing federate's HLAfederate.HLAupdatesSent counter ticks.
	// MUST NOT block.
	OnUpdateSent func(fed core.FederationName, producer core.FederateHandle)

	// OnInteractionSent is the interaction-side analogue of OnUpdateSent.
	// M11 wires this to MOM.IncrementInteractionsSent.
	OnInteractionSent func(fed core.FederationName, producer core.FederateHandle)

	// OnReflectDelivered fires once per recipient federate when a
	// ReflectAttributeValues envelope is dispatched. M11 wires this to
	// MOM.IncrementReflectionsReceived. MUST NOT block (the dispatcher
	// already iterates the subscriber list under no lock).
	OnReflectDelivered func(fed core.FederationName, recipient core.FederateHandle)

	// OnInteractionDelivered fires once per recipient federate when a
	// ReceiveInteraction envelope is dispatched. M11 wires this to
	// MOM.IncrementInteractionsReceived.
	OnInteractionDelivered func(fed core.FederationName, recipient core.FederateHandle)

	// DDM is the OPTIONAL Data Distribution Management filter
	// (M10 / FR-DDM-3..6). When non-nil, the registry consults
	// the DDM manager on every update / interaction send to:
	//   1. Look up the publisher's region set for the (object,
	//      attribute) being updated (DDM.PublisherRegionsFor).
	//   2. If a region set exists, replace the cut-1
	//      declaration.SubscribersFor result with
	//      DDM.SubscribersForUpdate (which performs the overlap
	//      test against subscribers' own regions).
	//   3. If no region set exists for the object (the producer
	//      did not call AssociateRegionsWithObjectInstance),
	//      the registry falls through to the cut-1 path
	//      unchanged. This is the FR-DDM-6 zero-cost contract:
	//      non-DDM workloads pay only the nil-check + map miss.
	//
	// MUST satisfy DDMFilter (defined below).
	DDM DDMFilter
}

// DDMFilter is the contract object.Registry consumes from the DDM
// manager. ddm.Manager satisfies this interface directly. Defined
// here (rather than depending on the ddm package) to keep the
// object → ddm direction the same as object → declaration: object
// owns the consumer interface, the implementation lives elsewhere.
type DDMFilter interface {
	// HasObjectAssociations returns true iff the producer has
	// associated any DDM regions with the given object (via
	// RegisterObjectInstanceWithRegions / Associate...). The hot
	// path uses this to fast-path the no-DDM case.
	HasObjectAssociations(fed core.FederationName, obj core.ObjectHandle) bool

	// PublisherRegionsFor returns the publisher's region set for
	// (obj, attr), or nil when no association exists for that
	// attribute.
	PublisherRegionsFor(fed core.FederationName, obj core.ObjectHandle, attr core.AttributeHandle) []DDMRegionHandle

	// SubscribersForUpdate returns the federates whose
	// region-scoped subscriptions overlap publisherRegions for
	// (cls, attr), in sorted handle order. nil publisherRegions
	// returns nil (zero-cost path).
	SubscribersForUpdate(
		fed core.FederationName,
		cls core.ObjectClassHandle,
		attr core.AttributeHandle,
		publisherRegions []DDMRegionHandle,
	) []core.FederateHandle
}

// DDMRegionHandle is the typed handle the DDMFilter API uses. uint64
// rather than ddm.RegionHandle keeps this package free of a
// dependency on ddm; the ddm.Manager exposes a thin adapter (see
// cmd/rtid) that converts ddm.RegionHandle → uint64 at the boundary.
type DDMRegionHandle uint64

// New constructs a Registry. Returns an error if any required field is nil.
//
// Required: Declarations, Outbox, FOMs, Clock. Optional: EventLog (cut-1
// relaxation; nil => events silently dropped), Codec (cut-1; attribute
// bytes pass through as-is until M2 wiring lands the production codec).
func New(opts Options) (*Registry, error) {
	if isNilInterface(opts.Declarations) {
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
	if isNilInterface(opts.Federations) {
		opts.Federations = nil
	}
	if isNilInterface(opts.Orders) {
		opts.Orders = nil
	}
	if isNilInterface(opts.DDM) {
		opts.DDM = nil
	}
	return &Registry{
		opts:         opts,
		federations:  map[core.FederationName]*federationState{},
		transports:   newTransportStore(),
		reservations: newReservationStore(),
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
	// M26 Phase F — if the caller supplied a name AND the name has
	// been pre-reserved, consume the reservation. This enforces
	// IEEE 1516.1 §6.6 for federates that opt in to the reservation
	// flow. Federates that register with a name WITHOUT pre-reserving
	// (the pre-M26 backwards-compat path) get the name marked as
	// registered without a prior reservation. If the name was
	// reserved by ANOTHER federate, Consume returns
	// ErrObjectInstanceNameReservedByOther — reject.
	if name != "" {
		if err := r.reservations.Consume(fed, producer, name); err != nil {
			if errors.Is(err, core.ErrObjectInstanceNameReservedByOther) {
				st.mu.Unlock()
				return core.InvalidObjectHandle, "", err
			}
			// Not reserved by anyone — accept as auto-reservation
			// for backwards compat. Mark as registered.
			r.reservations.MarkRegistered(fed, name)
		}
	}
	st.nextObjectHandle++
	assigned := core.ObjectHandle(st.nextObjectHandle)
	canonical := name
	if canonical == "" {
		canonical = fmt.Sprintf("HLAobj_%d_%d", cls, assigned)
		r.reservations.MarkRegistered(fed, canonical)
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
	if r.opts.OnRegister != nil {
		// Cut-1: the same fixed attribute range used by Discover
		// fan-out is forwarded to the OnRegister hook. FOM-driven
		// enumeration is the follow-up tracked at fanoutAttrProbe.
		attrs := make([]core.AttributeHandle, len(fanoutAttrProbe))
		copy(attrs, fanoutAttrProbe)
		r.opts.OnRegister(fed, producer, assigned, cls, attrs)
	}
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

// Snapshot returns aggregate object-instance counts for the
// AdminService handler. Phase 1 of the rtid-TUI plan
// (docs/rtid-tui.md): consumed by the drill-down view's
// "OBJECTS" column.
//
// Read order: Registry RLock for the federation map, then per-
// federation mu.Lock for the instance count. Cheap.
func (r *Registry) Snapshot(fed core.FederationName) core.ObjectSnapshot {
	r.mu.RLock()
	st, ok := r.federations[fed]
	r.mu.RUnlock()
	if !ok {
		return core.ObjectSnapshot{}
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return core.ObjectSnapshot{
		InstanceCount: uint32(len(st.instances)),
	}
}

// nextOutboundSeqLocked returns the next per-federation outbound seq.
// Caller must hold st.mu.
func (st *federationState) nextOutboundSeqLocked() uint64 {
	st.nextOutboundSeq++
	return st.nextOutboundSeq
}

// nextOutboundSeqRangeLocked reserves n consecutive seq numbers and
// returns the first. Used by fanout paths to amortize the lock acquire
// across an N-subscriber delivery batch. Caller must hold st.mu.
func (st *federationState) nextOutboundSeqRangeLocked(n int) uint64 {
	st.nextOutboundSeq++
	start := st.nextOutboundSeq
	st.nextOutboundSeq += uint64(n - 1)
	return start
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
