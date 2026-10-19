package object

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ErrNotImplemented is retained as an exported sentinel for callers that
// matched on it during the M2 transition; implemented methods never return
// it. Removed when no caller still references it post-M2.
var ErrNotImplemented = errors.New("object: not implemented ( M2 deliverable)")

// fanoutAttrProbe preserves legacy embedded FOM profiles without attribute
// enumeration. Production uses the FOM's complete attribute list.
var fanoutAttrProbe = []core.AttributeHandle{1, 2, 3, 4, 5, 6, 7, 8}

// Registry implements core.ObjectRegistry. It owns per-federation object
// state (handle counters, instance map, name index) and routes attribute
// updates and interactions to subscribers via Outbox.
//
// Concurrency: Registry.mu guards the federation table; per-federation
// state has its own RWMutex for independent fanout from concurrent
// federations.
type RegistryOf[T core.LogicalTimeScalar] struct {
	opts OptionsOf[T]

	mu          sync.RWMutex
	federations map[core.FederationName]*federationStateOf[T]

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
type federationStateOf[T core.LogicalTimeScalar] struct {
	deletions         map[core.ObjectHandle]DeletionStateOf[T]
	deletionHooks     []deletionHook
	attributeOrders   map[core.ObjectHandle]map[core.AttributeHandle]core.OrderType
	interactionOrders map[core.FederateHandle]map[core.InteractionClassHandle]core.OrderType
	mu                sync.Mutex

	managementClassMu     sync.RWMutex
	managementClasses     map[core.InteractionClassHandle]managementClassClassification
	subscriptionReference core.FOMHandleReference

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

	// discovered retains the class conveyed to each recipient, or the actual
	// registered class for its registrant. Subscription changes never replace
	// this historical class; local deletion allows a later rediscovery.
	discovered       map[discoverKey]core.ObjectClassHandle
	knowledgeEpochs  map[discoverKey]*atomic.Bool
	knowledgeRetired atomic.Bool

	// scope is the §6.17/§6.18 per-(object, subscriber) in-scope
	// attribute cache backing the DDM scope advisories (M37).
	// Lazily allocated on the first DDM-aware update; nil for non-DDM
	// federations (FR-DDM-6 zero-cost contract).
	scope map[core.ObjectHandle]map[core.FederateHandle]map[core.AttributeHandle]struct{}
}

// discoverKey identifies one delivered DiscoverObjectInstance.
type discoverKey struct {
	sub core.FederateHandle
	obj core.ObjectHandle
}

func newFederationStateOf[T core.LogicalTimeScalar]() *federationStateOf[T] {
	return &federationStateOf[T]{
		instances:         map[core.ObjectHandle]*objectInstance{},
		nameToHandle:      map[string]core.ObjectHandle{},
		discovered:        map[discoverKey]core.ObjectClassHandle{},
		managementClasses: map[core.InteractionClassHandle]managementClassClassification{},
	}
}

// objectInstance tracks the bare minimum the routing path needs: handle,
// canonical name, class, owner federate.
type objectInstance struct {
	// Serializes discovery, local deletion, removal, and attribute delivery for
	// this object without holding the state mutex across callback delivery.
	deliveryMu     sync.Mutex
	handle         core.ObjectHandle
	name           string
	cls            core.ObjectClassHandle
	owner          core.FederateHandle
	lastReflection map[reflectionRateKey]reflectionDeliveryTime
}

// Options bundles Registry dependencies. EventLog is optional in cut 1 to
// match the federation manager's relaxation (a nil log silently drops
// events; W4 wires a real log in production). All other fields are
// required.
type OptionsOf[T core.LogicalTimeScalar] struct {
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

	// M20.3 — MOM-driven interaction dispatch hook. Optional: when
	// nil, every interaction takes the normal fanout path. When set,
	// SendInteraction checks if the class name is in the HLAmanager
	// subtree AND has a registered handler; if so, the dispatcher
	// handles it INSTEAD of fanout. See rti/internal/mom/dispatch.go.
	ManagementDispatch core.ManagementInteractionDispatcher

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

	// TSOGate admits reflection, interaction, and removal messages. A
	// reservation-capable gate classifies every RO/TSO recipient under one
	// evaluation lease. Legacy gates buffer only TSO; discovery and control
	// callbacks never pass through this message gate.
	//
	// OPTIONAL: when nil, the registry falls back to direct Send for
	// every event, preserving the pre-M22 always-async behavior. This
	// keeps test fixtures + in-process drivers that do not wire a
	// time manager working unchanged.
	TSOGate core.TSODeliveryGateOf[T]

	// Retractions admits one ID per originating timestamped operation. When
	// absent, a capable TSOGate supplies admission; otherwise nonzero IDs fail.
	Retractions core.RetractionIDAdmission

	// TSOValidator validates outgoing TSO timestamps at the
	// send/update/delete ingestion points (M37 EB-3 / IEEE 1516.1-2010
	// §8.1.2): a time-regulating sender may not stamp a TSO message
	// below currentTime + lookahead. The time.Manager satisfies this
	// interface directly (wired through cmd/rtid alongside TSOGate).
	//
	// OPTIONAL: when nil, no server-side timestamp validation runs
	// (pre-M37 behavior; in-process fixtures without a time manager
	// keep working). RO sends (nil timestamp) never consult it.
	TSOValidator OutgoingTSOValidatorOf[T]

	// OnRegister initializes ownership before the new object is exposed or
	// discovered. The attribute set is one publication snapshot plus the
	// implicit deletion privilege. Called under the object-state lock; it
	// must not re-enter the registry or DDM, or deliver callbacks.
	OnRegister func(fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle)
	// Runs after registration and discovery, without object locks. Must not block.
	OnObjectRegistered func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, core.ObjectClassHandle, []core.AttributeHandle)

	// OnDelete removes dependent ownership state after an instance is deleted.
	OnDelete func(core.FederationName, core.ObjectHandle)

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

	// Detailed traffic hooks use the same acceptance boundary as the aggregate
	// hooks above. incoming distinguishes recipient acceptance from sending.
	OnObjectTraffic         func(core.FederationName, core.FederateHandle, core.ObjectClassHandle, bool)
	OnObjectInstanceTraffic func(core.FederationName, core.FederateHandle, core.ObjectHandle, core.ObjectClassHandle, bool)
	OnInteractionTraffic    func(core.FederationName, core.FederateHandle, core.InteractionClassHandle, bool)
	// Sent transport partitions are captured by fanout classification, before
	// admission. These hooks run only after the complete send commits.
	OnObjectSentTransport      func(core.FederationName, core.FederateHandle, core.ObjectClassHandle, core.TransportType)
	OnInteractionSentTransport func(core.FederationName, core.FederateHandle, core.InteractionClassHandle, core.TransportType)

	// Ownership is the OPTIONAL per-instance attribute-ownership
	// lookup (M38 GB / IEEE 1516.1-2010 §6.6 precondition). When
	// non-nil, updateAttributes requires the producer to be the
	// CURRENT §7 owner of EVERY attribute in the update — the
	// class-level publication check (producerOwnsAllAttrs, the §5
	// precondition) remains necessary but is no longer sufficient.
	// The RTI-internal MOM producer (max-uint64, mom.momProducer) is
	// exempt — see internalMOMProducer in update.go.
	//
	// OPTIONAL: when nil, the pre-M38 publication-only gate applies
	// (in-process fixtures composed without an ownership manager keep
	// working — same relaxation pattern as TSOGate / TSOValidator).
	// cmd/rtid wires the production ownership.Manager here, whose
	// state is seeded by the OnRegister hook below.
	Ownership InstanceAttributeOwnership

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

	// RegistrationRegions pins validated region bindings through registration.
	// Required only for RegisterWithRegions; ordinary registration is unchanged.
	RegistrationRegions core.ObjectRegionRegistrar
}

// InstanceAttributeOwnership is the contract the update gate consumes
// from the ownership manager (M38 GB): "is federate h the CURRENT §7
// owner of instance-attribute (obj, attr)?". ownership.Manager (and
// the core.OwnershipCoordinator interface) satisfy it directly via
// IsOwnedBy — a per-attribute map hit under the manager's RLock, safe
// on the update hot path. Consumer-owned interface, same direction as
// Declarations / DDMFilter.
type InstanceAttributeOwnership interface {
	IsOwnedBy(fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attr core.AttributeHandle) bool
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

	// RegionSubscribersFor returns every federate holding a
	// region-scoped subscription to (cls, attr), in sorted handle
	// order, WITHOUT an overlap test. M36 DC-1: the Discover
	// fan-out consults this for the register-time default-region
	// case — the register/associate split means a freshly
	// registered object has no publisher regions yet, and an
	// unassociated update maps to the default region, which
	// overlaps every subscriber region. nil when no region
	// subscriptions exist (zero-cost path).
	RegionSubscribersFor(
		fed core.FederationName,
		cls core.ObjectClassHandle,
		attr core.AttributeHandle,
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
func NewOf[T core.LogicalTimeScalar](opts OptionsOf[T]) (*RegistryOf[T], error) {
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
	if isNilInterface(opts.Ownership) {
		opts.Ownership = nil
	}
	return &RegistryOf[T]{
		opts:         opts,
		federations:  map[core.FederationName]*federationStateOf[T]{},
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

// ClassOf returns the object class handle of a registered object
// instance, or zero if the (fed, obj) pair is unknown. M17.27 —
// the ownership manager's SubscribersResolver uses this to project
// an ObjectHandle into the ObjectClassHandle that
// declaration.Manager.SubscribersFor expects.
func (r *RegistryOf[T]) ClassOf(fed core.FederationName, obj core.ObjectHandle) core.ObjectClassHandle {
	r.mu.RLock()
	st, ok := r.federations[fed]
	r.mu.RUnlock()
	if !ok {
		return 0
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	inst, ok := st.instances[obj]
	if !ok {
		return 0
	}
	return inst.cls
}

// stateFor returns (and lazily creates) the per-federation state.
func (r *RegistryOf[T]) stateFor(fed core.FederationName) *federationStateOf[T] {
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
		st = newFederationStateOf[T]()
		r.federations[fed] = st
	}
	return st
}

// Register implements core.ObjectRegistry. Assigns a monotonic
// ObjectHandle, persists ObjectRegistered to EventLog (write-ahead), then
// fans out DiscoverObjectInstance to subscribers in deterministic
// (sorted) handle order, excluding the producer.
func (r *RegistryOf[T]) Register(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	cls core.ObjectClassHandle,
	name string,
) (core.ObjectHandle, string, error) {
	return r.register(ctx, fed, producer, cls, name, nil, false, nil, false)
}

// RegisterWithRegions validates and installs every region association before
// making the instance discoverable. A rejected request consumes no object ID,
// name reservation, or region association.
func (r *RegistryOf[T]) RegisterWithRegions(ctx context.Context, fed core.FederationName, producer core.FederateHandle, cls core.ObjectClassHandle, name string, bindings map[core.AttributeHandle][]core.DDMRegionHandleCore) (core.ObjectHandle, string, error) {
	return r.register(ctx, fed, producer, cls, name, bindings, true, nil, false)
}

func (r *RegistryOf[T]) register(ctx context.Context, fed core.FederationName, producer core.FederateHandle, cls core.ObjectClassHandle, name string, bindings map[core.AttributeHandle][]core.DDMRegionHandleCore, withRegions bool, groups []core.AttributeRegionGroup, grouped bool) (core.ObjectHandle, string, error) {
	if producer == core.InvalidFederateHandle {
		return core.InvalidObjectHandle, "", core.ErrFederateNotJoined
	}
	published := r.registrationAttributes(ctx, fed, producer, cls)
	if len(published) == 0 {
		return core.InvalidObjectHandle, "", core.ErrObjectClassNotPublished
	}
	groupedRegistrar, err := r.validateRegistrationRegions(ctx, fed, cls, published, bindings, withRegions, grouped)
	if err != nil {
		return 0, "", err
	}
	owned := append([]core.AttributeHandle(nil), published...)

	st := r.stateFor(fed)
	st.mu.Lock()
	if name != "" {
		if _, dup := st.nameToHandle[name]; dup {
			st.mu.Unlock()
			return core.InvalidObjectHandle, "", fmt.Errorf("object: name %q already registered in federation %q: %w", name, fed, core.ErrObjectInstanceNameInUse)
		}
	}
	assigned := core.ObjectHandle(st.nextObjectHandle + 1)
	if assigned == 0 {
		st.mu.Unlock()
		return 0, "", core.ErrObjectHandleInvalid
	}
	canonical := name
	if canonical == "" {
		canonical = fmt.Sprintf("HLAobj_%d_%d", cls, assigned)
	}
	regions, err := r.prepareRegistrationRegions(ctx, fed, producer, cls, bindings, groups, groupedRegistrar, withRegions, grouped)
	if err != nil {
		st.mu.Unlock()
		return 0, "", err
	}

	assigned, canonical, err = r.registerName(ctx, fed, producer, cls, name, assigned, canonical, bindings, withRegions)
	if err != nil {
		if regions != nil {
			regions.Release()
		}
		st.mu.Unlock()
		return core.InvalidObjectHandle, "", err
	}
	st.nextObjectHandle = uint64(assigned)

	inst := &objectInstance{
		handle: assigned,
		name:   canonical,
		cls:    cls,
		owner:  producer,
	}
	st.instances[assigned] = inst
	st.nameToHandle[canonical] = assigned
	if producer != internalProducer {
		st.discovered[discoverKey{sub: producer, obj: assigned}] = cls
	}
	if regions != nil {
		regions.Commit(assigned)
		regions.Release()
	}
	if r.opts.OnRegister != nil {
		// M36 DC-3: seed initial ownership ONLY for attributes the
		// registrant actually publishes (plus the implicit privilege
		// attribute, DC-4) — the pre-M36 blanket fanoutAttrProbe
		// seeding made the registrant "owner" of attributes it never
		// published, so §7.17 queries on unpublished attributes
		// wrongly answered informAttributeOwnership instead of
		// attributeIsNotOwned.
		r.opts.OnRegister(fed, producer, assigned, cls, owned)
	}
	st.mu.Unlock()
	r.fanoutDiscover(ctx, fed, st, producer, inst, published)
	if r.opts.OnObjectRegistered != nil {
		r.opts.OnObjectRegistered(ctx, fed, producer, assigned, cls, slices.Clone(owned))
	}
	return assigned, canonical, nil
}

func (r *RegistryOf[T]) classAttributes(ctx context.Context, fed core.FederationName, cls core.ObjectClassHandle) []core.AttributeHandle {
	if fom, err := r.opts.FOMs.Get(ctx, fed); err == nil && fom != nil {
		if enumeration, ok := fom.(interface {
			AttributeHandles(core.ObjectClassHandle) []core.AttributeHandle
		}); ok {
			return enumeration.AttributeHandles(cls)
		}
	}
	return fanoutAttrProbe
}

func (r *RegistryOf[T]) registrationAttributes(ctx context.Context, fed core.FederationName, producer core.FederateHandle, cls core.ObjectClassHandle) []core.AttributeHandle {
	if snapshot, ok := r.opts.Declarations.(interface {
		PublishedObjectAttributes(core.FederationName, core.FederateHandle, core.ObjectClassHandle) []core.AttributeHandle
	}); ok {
		return snapshot.PublishedObjectAttributes(fed, producer, cls)
	}
	var attrs []core.AttributeHandle
	for _, attr := range r.classAttributes(ctx, fed, cls) {
		if slices.Contains(r.opts.Declarations.PublishersFor(ctx, fed, cls, attr), producer) {
			attrs = append(attrs, attr)
		}
	}
	return attrs
}

// UpdateAttributes implements core.ObjectRegistry. Implemented in update.go.
func (r *RegistryOf[T]) UpdateAttributes(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	obj core.ObjectHandle,
	attrs map[core.AttributeHandle][]byte,
	ts *T,
) error {
	return r.updateAttributes(ctx, fed, producer, obj, attrs, ts, 0)
}

// UpdateAttributesRetractable — M20.2. See core.ObjectRegistry doc.
func (r *RegistryOf[T]) UpdateAttributesRetractable(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	obj core.ObjectHandle,
	attrs map[core.AttributeHandle][]byte,
	ts *T,
	retractionHandle uint64,
) error {
	return r.updateAttributes(ctx, fed, producer, obj, attrs, ts, retractionHandle)
}

// SendInteraction implements core.ObjectRegistry. Implemented in interaction.go.
func (r *RegistryOf[T]) SendInteraction(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	cls core.InteractionClassHandle,
	params map[core.ParameterHandle][]byte,
	ts *T,
) error {
	return r.sendInteraction(ctx, fed, producer, cls, params, ts, 0)
}

// SendInteractionRetractable — M20.2. See core.ObjectRegistry doc.
func (r *RegistryOf[T]) SendInteractionRetractable(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	cls core.InteractionClassHandle,
	params map[core.ParameterHandle][]byte,
	ts *T,
	retractionHandle uint64,
) error {
	return r.sendInteraction(ctx, fed, producer, cls, params, ts, retractionHandle)
}

// RetractMessage is the legacy count-only service wrapper. Zero also denotes
// rejection; callers needing the IEEE exception use RetractWithError.
func (r *RegistryOf[T]) RetractMessage(
	fed core.FederationName,
	sender core.FederateHandle,
	retractionHandle uint64,
) int {
	if err := r.RetractWithError(context.Background(), fed, sender, retractionHandle); err != nil {
		return 0
	}
	return 1
}

// RetractMessageNotify retains the old service name without bypassing admission.
func (r *RegistryOf[T]) RetractMessageNotify(
	ctx context.Context,
	fed core.FederationName,
	sender core.FederateHandle,
	retractionHandle uint64,
) int {
	if err := r.RetractWithError(ctx, fed, sender, retractionHandle); err != nil {
		return 0
	}
	return 1
}

// Snapshot returns aggregate object-instance counts for the
// AdminService handler. Phase 1 of the rtid-TUI plan
// (docs/rtid-tui.md): consumed by the drill-down view's
// "OBJECTS" column.
//
// Read order: Registry RLock for the federation map, then per-
// federation mu.Lock for the instance count. Cheap.
func (r *RegistryOf[T]) Snapshot(fed core.FederationName) core.ObjectSnapshot {
	r.mu.RLock()
	st, ok := r.federations[fed]
	r.mu.RUnlock()
	if !ok {
		return core.ObjectSnapshot{}
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	count := uint64(len(st.instances))
	if count > math.MaxUint32 {
		// The summary schema cannot represent larger counts; never wrap to zero.
		return core.ObjectSnapshot{InstanceCount: math.MaxUint32}
	}
	return core.ObjectSnapshot{InstanceCount: uint32(count)}
}

// nextOutboundSeqLocked returns the next per-federation outbound seq.
// Caller must hold st.mu.
func (st *federationStateOf[T]) nextOutboundSeqLocked() uint64 {
	st.nextOutboundSeq++
	return st.nextOutboundSeq
}

// nextOutboundSeqRangeLocked reserves n consecutive seq numbers and
// returns the first. Used by fanout paths to amortize the lock acquire
// across an N-subscriber delivery batch. Caller must hold st.mu.
func (st *federationStateOf[T]) nextOutboundSeqRangeLocked(n int) uint64 {
	start := st.nextOutboundSeq + 1
	if n > 0 {
		st.nextOutboundSeq += uint64(n)
	}
	return start
}

// Compile-time assertion that Registry implements core.ObjectRegistry.
var _ core.ObjectRegistry = (*Registry)(nil)

// newObjectRegisteredEvent builds the eventlog adapter for an
// ObjectRegistered event. Defined here so Register-only changes do not
// drag in the discover/update/interaction files.
func newObjectRegisteredEventOf[T core.LogicalTimeScalar](handle core.ObjectHandle, cls core.ObjectClassHandle, owner core.FederateHandle, name string, wallNs int64) *core.ObjectJournalRecordOf[T] {
	return &core.ObjectJournalRecordOf[T]{
		Kind:        JournalObjectRegistered,
		WallNS:      journalWallBits(wallNs),
		Object:      handle,
		ObjectClass: cls,
		Federate:    owner,
		ObjectName:  name,
	}
}

func (r *RegistryOf[T]) validateRegistrationRegions(ctx context.Context, fed core.FederationName, cls core.ObjectClassHandle, published []core.AttributeHandle, bindings map[core.AttributeHandle][]core.DDMRegionHandleCore, withRegions, grouped bool) (core.GroupedObjectRegionRegistrar, error) {
	var groupedRegistrar core.GroupedObjectRegionRegistrar
	if grouped {
		var ok bool
		groupedRegistrar, ok = r.opts.RegistrationRegions.(core.GroupedObjectRegionRegistrar)
		if !ok {
			return nil, fmt.Errorf("object: grouped region registration coordinator unavailable")
		}
	}
	if withRegions {
		if isNilInterface(r.opts.RegistrationRegions) {
			return nil, fmt.Errorf("object: region registration coordinator unavailable")
		}
		if fom, err := r.opts.FOMs.Get(ctx, fed); err == nil && fom != nil {
			if names, ok := fom.(interface {
				AttributeName(core.ObjectClassHandle, core.AttributeHandle) (string, bool)
			}); ok {
				for attr := range bindings {
					if _, defined := names.AttributeName(cls, attr); !defined {
						return nil, core.ErrAttributeNotFound
					}
				}
			}
		}
		for attr := range bindings {
			if !slices.Contains(published, attr) {
				return nil, core.ErrAttributeNotPublished
			}
		}
	}
	return groupedRegistrar, nil
}

func (r *RegistryOf[T]) registerName(ctx context.Context, fed core.FederationName, producer core.FederateHandle, cls core.ObjectClassHandle, name string, assigned core.ObjectHandle, canonical string, bindings map[core.AttributeHandle][]core.DDMRegionHandleCore, withRegions bool) (core.ObjectHandle, string, error) {
	// Keep name validation and registration in one reservation transaction.
	// Failed persistence leaves both the caller's reservation and the object
	// counter unchanged; concurrent name release cannot race with rollback.
	var err error
	for {
		journalAttempted := false
		err = r.reservations.register(fed, producer, canonical, name != "" && producer != internalProducer, func() error {
			if r.opts.EventLog != nil {
				journalAttempted = true
				ev := newObjectRegisteredEventOf[T](assigned, cls, producer, canonical, r.opts.Clock.Now().UnixNano())
				for _, associated := range bindings {
					if withRegions && len(associated) > 0 {
						ev.RegionScoped = true
						break
					}
				}
				if err := r.opts.EventLog.Append(ctx, fed, ev); err != nil {
					return fmt.Errorf("object: register %q in %q: eventlog append: %w", canonical, fed, err)
				}
			}
			return nil
		})
		if name != "" || journalAttempted || (!errors.Is(err, core.ErrObjectInstanceNameInUse) && !errors.Is(err, core.ErrObjectInstanceNameReservedByOther)) {
			break
		}
		// A generated name must not repeatedly select another federate's
		// reservation. Only the successful candidate advances the live counter.
		assigned++
		if assigned == 0 {
			err = core.ErrObjectHandleInvalid
			break
		}
		canonical = fmt.Sprintf("HLAobj_%d_%d", cls, assigned)
	}
	return assigned, canonical, err
}

func (r *RegistryOf[T]) prepareRegistrationRegions(ctx context.Context, fed core.FederationName, producer core.FederateHandle, cls core.ObjectClassHandle, bindings map[core.AttributeHandle][]core.DDMRegionHandleCore, groups []core.AttributeRegionGroup, groupedRegistrar core.GroupedObjectRegionRegistrar, withRegions, grouped bool) (core.ObjectRegionRegistration, error) {
	var regions core.ObjectRegionRegistration
	if withRegions {
		var err error
		if grouped {
			regions, err = groupedRegistrar.PrepareObjectRegistrationGroups(ctx, fed, producer, cls, groups)
		} else {
			regions, err = r.opts.RegistrationRegions.PrepareObjectRegistration(ctx, fed, producer, cls, bindings)
		}
		if err != nil {
			return nil, err
		}
	}

	return regions, nil
}
