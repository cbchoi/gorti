package ddm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// ErrNotImplemented is retained as an exported sentinel for callers that
// matched on it during the M10 RED state. Implemented methods never
// return it; spec tests in rti/spec/M10/ use it to skip cleanly during
// pre-dispatch.
var ErrNotImplemented = errors.New("ddm: not implemented ( M10 deliverable)")

// DimensionHandle identifies one dimension of a routing space.
// 1-based; 0 is the invalid sentinel.
//
// Phase 1 of the research-platform refactor (docs/research-platform.md
// §5.4) declared the canonical handle types in core/ddm.go so that the
// concrete *ddm.Manager satisfies core.DataDistributionManagement
// without per-method conversion. The ddm-package aliases below preserve
// the existing public spelling.
type DimensionHandle = core.DDMDimensionHandle

// RegionHandle identifies one region instance.
// 1-based; 0 is the invalid sentinel.
type RegionHandle = core.DDMRegionHandleCore

// RoutingSpaceHandle identifies one routing space.
// 1-based; 0 is the invalid sentinel.
//
// In IEEE 1516-2010 dimensions are flat (no enclosing routing-space
// element); the ddm.Manager exposes a single implicit routing space
// per federation named "default" (handle 1). The RoutingSpaceHandle
// type stays in the API to keep the surface forward-compatible with
// 1.3-style multi-routing-space FOMs (cut-3).
type RoutingSpaceHandle = core.DDMRoutingSpaceHandle

// DefaultRoutingSpace is the implicit routing-space name used by the
// 1516-2010 flat-dimension model. Every dimension in the FOM belongs
// to this single routing space.
const DefaultRoutingSpace = "default"

// Range describes a closed-open interval [Lower, Upper) on one
// dimension. Per IEEE 1516.1-2010 §6.5; bounds are uint64 normalized
// values (the FOM declares the upper bound + normalization).
//
// Aliased to core.DDMRange so the .Overlap method (declared on the core
// type) is reachable as ddm.Range.Overlap, and so *ddm.Manager
// signatures using Range satisfy core.DataDistributionManagement
// without conversion.
type Range = core.DDMRange

// Options bundles Manager dependencies.
type Options struct {
	// Outbox delivers region-scoped attribute updates + interactions.
	// MUST NOT be nil.
	Outbox core.Outbox

	// EventLog records DDM lifecycle (region create/commit/delete,
	// subscribeWithRegions). Optional in cut-2.
	EventLog core.EventLog

	// FOMs is consulted to resolve routing-space declarations + their
	// dimensions. MUST NOT be nil.
	FOMs core.FOMRepository

	// FederationExists fences name-only lookup helpers from recreating state
	// after teardown. Production wires federation.Manager.GenerationFor;
	// embedded fixtures may leave it nil for legacy permissive behavior.
	FederationExists func(core.FederationName) bool

	// Objects validates and pins association targets before the DDM lock.
	// Required in production; nil preserves legacy isolated DDM fixtures.
	Objects                           core.RegionAssociationValidator
	PrepareKnownAssociation           func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (core.ObjectClassHandle, []core.AttributeHandle, map[core.AttributeHandle]core.FederateHandle, func(), error)
	ValidateSubscriptionOptions       func(context.Context, core.FederationName, core.SubscriptionOptions) error
	OnObjectSubscriptionInterest      func(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, []core.AttributeHandle) error
	OnInteractionSubscriptionInterest func(context.Context, core.FederationName, core.FederateHandle, core.InteractionClassHandle, bool) error
	// OnObjectSubscribed observes successful nonempty subscription mutations
	// after the subscription, rate and state locks have all been released.
	OnObjectSubscribed func(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, []core.AttributeHandle)
}

// DimensionEnumerator is an OPTIONAL extra capability on a FOMHandle:
// the production *fomHandle in cmd/rtid implements it; the permissive
// stubs in spec tests do not. When the handle satisfies this contract
// the ddm.Manager populates its routing-space maps eagerly from the
// FOM at first lookup. Otherwise the Manager runs in permissive mode
// — every name resolves to handle 1 (matches the spec-test fixture
// stub semantics).
type DimensionEnumerator interface {
	Dimensions() []model.Dimension
}

// Manager owns per-federation routing-space + region + scoped
// subscription state. Goroutine-safe.
//
// FROZEN-shape per docs/srs.md FR-DDM-1..6.
type Manager struct {
	opts Options

	mu  sync.RWMutex
	fed map[core.FederationName]*federationDDMState
	// Serializes subscription commits and post-unlock interest notifications.
	// Interest hooks must not synchronously mutate DDM subscriptions.
	subscriptionMu       sync.Mutex
	onRateDemandChanged  func(context.Context, core.FederationName, bool) error
	onRegionStateChanged func(context.Context, core.FederationName)
	rateChangeGuard      func() func()
	associationRollback  *associationRollbackProjection
}

// Compile-time assertion: *Manager satisfies core.DataDistributionManagement.
// Phase 1 of the research-platform refactor (docs/research-platform.md
// §5.4) introduced the interface; the typed handle aliases above let
// the existing method signatures match it without any conversion.
var _ core.DataDistributionManagement = (*Manager)(nil)

// New constructs a Manager. Returns an error if any required Options
// field is nil.
func New(opts Options) (*Manager, error) {
	if opts.Outbox == nil {
		return nil, errors.New("ddm.New: Options.Outbox is required")
	}
	if isNilInterface(opts.FOMs) {
		return nil, errors.New("ddm.New: Options.FOMs is required")
	}
	if isNilInterface(opts.EventLog) {
		opts.EventLog = nil
	}
	return &Manager{
		opts: opts,
		fed:  map[core.FederationName]*federationDDMState{},
	}, nil
}

// OnFederationDestroyed drops routing spaces, regions, and scoped
// subscriptions belonging to fed.
func (m *Manager) OnFederationDestroyed(fed core.FederationName) {
	m.mu.Lock()
	delete(m.fed, fed)
	m.mu.Unlock()
}

// isNilInterface reports whether v is a true nil interface or a typed-
// nil (interface holding a nil concrete pointer). Mirrors the helper
// in object/registry.go.
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

// stateForLocked returns (creating + populating from FOM if missing)
// the per-federation state. Caller MUST hold m.mu (write lock).
func (m *Manager) stateForLocked(ctx context.Context, fed core.FederationName) *federationDDMState {
	if st, ok := m.fed[fed]; ok {
		m.refreshFOMDimensions(ctx, fed, st)
		return st
	}
	st := newFederationDDMState()
	// Best-effort FOM enumeration: when the FOM handle exposes
	// Dimensions(), populate the routing-space + dimension tables
	// eagerly. Otherwise the Manager stays in permissive mode and
	// resolves every name to handle 1 at lookup time (see Lookup*).
	if h, err := m.opts.FOMs.Get(ctx, fed); err == nil && h != nil && h.IsValid() {
		if de, ok := h.(DimensionEnumerator); ok {
			st.populateFromFOM(de.Dimensions())
		}
	}
	m.refreshFOMDimensions(ctx, fed, st)
	m.fed[fed] = st
	return st
}

// --- Routing-space queries (FR-DDM-1) -------------------------------------

// LookupRoutingSpace returns the handle for a named routing space
// declared in the federation's FOM. Returns 0 + false if not declared.
//
// In the 1516-2010 flat-dimension model only the implicit routing
// space "default" exists; the API surface stays for forward
// compatibility. In permissive mode (FOMHandle does not implement
// DimensionEnumerator) every non-empty name resolves to handle 1.
func (m *Manager) LookupRoutingSpace(fed core.FederationName, name string) (RoutingSpaceHandle, bool) {
	if name == "" {
		return 0, false
	}
	if m.opts.FederationExists != nil && !m.opts.FederationExists(fed) {
		return 0, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(context.Background(), fed)
	h, ok := st.routingSpaces[name]
	if ok {
		return h, true
	}
	if st.permissive {
		// Permissive stubs: lazily mint a handle on first lookup so
		// repeated lookups are stable. len() can't realistically
		// exceed uint64 capacity (it's bounded by available memory),
		// so the int→uint64 conversion is safe; the explicit cast
		// silences gosec G115.
		h = RoutingSpaceHandle(uint64(len(st.routingSpaces)) + 1)
		st.routingSpaces[name] = h
		return h, true
	}
	return 0, false
}

// LookupDimension returns the handle for a named dimension within a
// routing space. Returns 0 + false if not declared.
func (m *Manager) LookupDimension(fed core.FederationName, space RoutingSpaceHandle, name string) (DimensionHandle, bool) {
	if name == "" || space == 0 {
		return 0, false
	}
	if m.opts.FederationExists != nil && !m.opts.FederationExists(fed) {
		return 0, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(context.Background(), fed)
	dimsByName, ok := st.dimensions[space]
	if !ok {
		if st.permissive {
			dimsByName = map[string]DimensionHandle{}
			st.dimensions[space] = dimsByName
		} else {
			return 0, false
		}
	}
	h, ok := dimsByName[name]
	if ok {
		return h, true
	}
	if st.permissive {
		// len() can't realistically exceed uint64 capacity; the
		// explicit cast silences gosec G115.
		h = DimensionHandle(uint64(len(dimsByName)) + 1)
		dimsByName[name] = h
		// Default upper bound for permissive dimensions: full uint64
		// range so initial regions cover every overlap query unless
		// SetRangeBounds narrows them.
		st.dimUpperBound[h] = ^uint64(0)
		return h, true
	}
	return 0, false
}

// --- Region lifecycle (FR-DDM-2) ------------------------------------------

// CreateRegion creates a new region in the given routing space, with
// the given dimensions. Initial bounds are [0, dimension upper bound).
// Returns the new RegionHandle.
//
// Errors:
//   - ErrRoutingSpaceNotFound if space doesn't exist
//   - ErrDimensionNotFound if any dimension isn't part of space
func (m *Manager) CreateRegion(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	space RoutingSpaceHandle,
	dims []DimensionHandle,
) (RegionHandle, error) {
	if space == 0 {
		return 0, core.ErrRoutingSpaceNotFound
	}
	dims = dimensionSet(dims)
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(ctx, fed)
	if !st.hasRoutingSpace(space) {
		return 0, core.ErrRoutingSpaceNotFound
	}
	for _, d := range dims {
		if !st.hasDimension(space, d) {
			return 0, core.ErrDimensionNotFound
		}
	}
	if st.nextRegionHandle == RegionHandle(^uint64(0)) {
		return 0, fmt.Errorf("region handle namespace exhausted")
	}
	st.nextRegionHandle++
	rh := st.nextRegionHandle
	rs := &regionState{
		template:  true,
		owner:     owner,
		space:     space,
		dims:      append([]DimensionHandle(nil), dims...),
		committed: map[DimensionHandle]Range{},
		pending:   map[DimensionHandle]Range{},
	}
	for _, d := range dims {
		// Full-range template bounds do not constitute a specification.
		// Every explicit dimension must be set before the first commit.
		ub := st.dimUpperBound[d]
		if ub == 0 {
			ub = ^uint64(0)
		}
		rs.committed[d] = Range{Lower: 0, Upper: ub}
	}
	st.regions[rh] = rs
	return rh, nil
}

// CommitRegionModifications applies pending bound changes to a set of
// regions. Per IEEE 1516.1-2010 §6.5: bounds set via SetRangeBounds
// don't take effect until commit (so federates can update multiple
// regions atomically).
func (m *Manager) CommitRegionModifications(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	regions []RegionHandle,
) (err error) {
	defer m.notifyRegionStateChanged(ctx, fed, &err)
	m.subscriptionMu.Lock()
	defer m.subscriptionMu.Unlock()
	releaseRateGuard := m.lockRateChange()
	defer releaseRateGuard()
	var rollback func()
	defer m.finishRateMutation(ctx, fed, &err, &rollback)
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(ctx, fed)
	// Validate everything before applying — commit is atomic across
	// the supplied region set per §6.5.
	for _, rh := range regions {
		rs, ok := st.regions[rh]
		if !ok {
			return fmt.Errorf("ddm: commit region %d in %q: %w", rh, fed, core.ErrRegionNotFound)
		}
		if rs.owner != owner {
			return fmt.Errorf("ddm: commit region %d in %q by federate %d: %w", rh, fed, owner, core.ErrRegionNotOwnedByFederate)
		}
		if rs.template {
			for _, dim := range rs.dims {
				if _, initialized := rs.pending[dim]; !initialized {
					return fmt.Errorf("ddm: incomplete template %d dimension %d: %w", rh, dim, core.ErrRegionNotFound)
				}
			}
		}
	}
	rollback = m.regionRateRollbackLocked(fed, st, regions)
	for _, rh := range regions {
		rs := st.regions[rh]
		for d, r := range rs.pending {
			rs.committed[d] = r
		}
		// Reset the pending map so subsequent SetRangeBounds calls
		// start from a clean slate.
		rs.pending = map[DimensionHandle]Range{}
		rs.template = false
	}
	return nil
}

// SetRangeBounds sets the [Lower, Upper) bounds for a (region,
// dimension) pair. Bounds are pending until CommitRegionModifications.
// IEEE 1516.1-2010 10.30.3(h-i) requires lower < upper <= dimension limit.
func (m *Manager) SetRangeBounds(
	fed core.FederationName,
	owner core.FederateHandle,
	region RegionHandle,
	dim DimensionHandle,
	bounds Range,
) error {
	m.subscriptionMu.Lock()
	defer m.subscriptionMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(context.Background(), fed)
	rs, ok := st.regions[region]
	if !ok {
		return fmt.Errorf("ddm: set bounds region %d in %q: %w", region, fed, core.ErrRegionNotFound)
	}
	if rs.owner != owner {
		return fmt.Errorf("ddm: set bounds region %d in %q by federate %d: %w", region, fed, owner, core.ErrRegionNotOwnedByFederate)
	}
	if !slices.Contains(rs.dims, dim) {
		return fmt.Errorf("ddm: set bounds region %d in %q dim %d: %w", region, fed, dim, core.ErrDimensionNotFound)
	}
	if bounds.Lower >= bounds.Upper || bounds.Upper > st.dimUpperBound[dim] {
		return fmt.Errorf("ddm: set bounds region %d in %q dim %d: %w", region, fed, dim, core.ErrInvalidRangeBounds)
	}
	rs.pending[dim] = bounds
	return nil
}

// DeleteRegion removes a region. The region MUST NOT be in use by any
// active subscription or registered object instance.
func (m *Manager) DeleteRegion(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	region RegionHandle,
) error {
	m.subscriptionMu.Lock()
	defer m.subscriptionMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(ctx, fed)
	rs, ok := st.regions[region]
	if !ok {
		return fmt.Errorf("ddm: delete region %d in %q: %w", region, fed, core.ErrRegionNotFound)
	}
	if rs.owner != owner {
		return fmt.Errorf("ddm: delete region %d in %q by federate %d: %w", region, fed, owner, core.ErrRegionNotOwnedByFederate)
	}
	if st.regionInUse(region) {
		return fmt.Errorf("ddm: delete region %d in %q: %w", region, fed, core.ErrRegionInUse)
	}
	delete(st.regions, region)
	st.removeInactiveRegion(region)
	return nil
}

// QueryBounds returns the committed bounds of (region, dim). Returns
// zero Range + false if not set.
func (m *Manager) QueryBoundsChecked(fed core.FederationName, region RegionHandle, dim DimensionHandle) (Range, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := m.fed[fed]
	if st != nil {
		if snapshot, found := st.conveyed[region]; found {
			if bounds, found := snapshot.RangeBounds(dim); found {
				return bounds, nil
			}
			return Range{}, core.ErrDimensionNotFound
		}
	}
	if st == nil || st.regions[region] == nil || st.regions[region].template {
		return Range{}, core.ErrRegionNotFound
	}
	r, ok := st.regions[region].committed[dim]
	if !ok {
		return Range{}, core.ErrDimensionNotFound
	}
	return r, nil
}

func (m *Manager) QueryBounds(
	fed core.FederationName,
	region RegionHandle,
	dim DimensionHandle,
) (Range, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return Range{}, false
	}
	if snapshot, found := st.conveyed[region]; found {
		return snapshot.RangeBounds(dim)
	}
	rs, ok := st.regions[region]
	if !ok || rs.template {
		return Range{}, false
	}
	r, ok := rs.committed[dim]
	if !ok {
		return Range{}, false
	}
	return r, true
}

// --- Region-scoped subscriptions (FR-DDM-3) -------------------------------

// SubscribeObjectClassAttributesWithRegions extends the cut-1
// SubscribeObjectClassAttributes with a set of regions; the subscriber
// only receives updates whose publisher region(s) overlap one of the
// subscribed regions.
//
// IEEE 1516.1-2010 9.8: regions are additive. Repeating a class/attribute/
// region triple changes only that triple's options, without duplicating it
// or changing subscriptions with other regions.
func (m *Manager) SubscribeObjectClassAttributesWithRegions(
	ctx context.Context,
	fed core.FederationName,
	subscriber core.FederateHandle,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
	regions []RegionHandle,
) error {
	return m.SubscribeObjectClassAttributesWithRegionsAndOptions(ctx, fed, subscriber, cls, attrs, regions, core.SubscriptionOptions{Active: true})
}

func (m *Manager) SubscribeObjectClassAttributesWithRegionsAndOptions(ctx context.Context, fed core.FederationName, subscriber core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle, regions []RegionHandle, options core.SubscriptionOptions) (err error) {
	var subscribed []core.AttributeHandle
	defer m.notifyObjectSubscribed(ctx, fed, subscriber, cls, &subscribed, &err)
	m.subscriptionMu.Lock()
	defer m.subscriptionMu.Unlock()
	releaseRateGuard := m.lockRateChange()
	defer releaseRateGuard()
	if m.opts.ValidateSubscriptionOptions != nil {
		if err := m.opts.ValidateSubscriptionOptions(ctx, fed, options); err != nil {
			return err
		}
	} else if options.UpdateRateDesignator != "" {
		return core.ErrInvalidUpdateRateDesignator
	}
	required, err := m.objectContext(ctx, fed, cls, attrs)
	if err != nil {
		return err
	}
	var rollback func()
	defer func() {
		if err == nil {
			err = m.objectInterest(ctx, fed, subscriber, cls)
			if err != nil && rollback != nil {
				rollback()
			}
		}
	}()
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(ctx, fed)
	if err := validateObjectRegions(st, subscriber, attrs, regions, required); err != nil {
		return err
	}
	// IEEE 1516.1-2010 9.8: an empty region set changes neither the
	// existing subscriptions nor their active/passive settings.
	if len(regions) == 0 {
		return nil
	}
	rollback = m.objectSubscriptionRollbackLocked(fed, st, subscriber, cls)
	for _, attr := range attrs {
		key := objAttrKey{cls: cls, attr: attr}
		st.addObjectSubscriptionLocked(key, subscription{
			subscriber: subscriber,
			regions:    append([]RegionHandle(nil), regions...),
			passive:    !options.Active,
			updateRate: options.UpdateRateDesignator,
		})
	}
	subscribed = slices.Clone(attrs)
	return nil
}

// SubscribeInteractionClassWithRegions: same shape for interactions.
func (m *Manager) SubscribeInteractionClassWithRegions(
	ctx context.Context,
	fed core.FederationName,
	subscriber core.FederateHandle,
	cls core.InteractionClassHandle,
	regions []RegionHandle,
) error {
	return m.SubscribeInteractionClassWithRegionsAndOptions(ctx, fed, subscriber, cls, regions, true)
}

func (m *Manager) SubscribeInteractionClassWithRegionsAndOptions(ctx context.Context, fed core.FederationName, subscriber core.FederateHandle, cls core.InteractionClassHandle, regions []RegionHandle, active bool) (err error) {
	m.subscriptionMu.Lock()
	defer m.subscriptionMu.Unlock()
	dims, strict, err := m.interactionContext(ctx, fed, cls)
	if err != nil {
		return err
	}
	var rollback func()
	defer func() {
		if err == nil {
			err = m.interactionInterest(ctx, fed, subscriber, cls)
			if err != nil && rollback != nil {
				rollback()
			}
		}
	}()
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(ctx, fed)
	if err := validateRegions(st, subscriber, regions, dims, strict); err != nil {
		return err
	}
	// IEEE 1516.1-2010 9.10: an empty set leaves all pairs/options intact.
	if len(regions) == 0 {
		return nil
	}
	rollback = m.interactionSubscriptionRollbackLocked(fed, st, subscriber, cls)
	st.addInteractionSubscriptionLocked(cls, interactionSubscription{
		subscriber: subscriber,
		regions:    append([]RegionHandle(nil), regions...),
		passive:    !active,
	})
	return nil
}

// --- Region-scoped publishing (FR-DDM-4) ----------------------------------

// AssociateRegionsWithObjectInstance records the publisher's per-attribute
// region set for an existing object instance. This is the cut-2
// "register-then-associate" path: the caller still uses
// object.Registry.Register to create the instance (which assigns the
// ObjectHandle in the canonical event log) and then calls this method
// to layer DDM-scoped publishing onto it.
//
// This legacy replacement helper clears only the caller's associations when
// given an empty map. The IEEE grouped service uses empty-as-no-op semantics.
func (m *Manager) AssociateRegionsWithObjectInstance(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrToRegions map[core.AttributeHandle][]RegionHandle,
) (err error) {
	return m.changeAssociationGroupsMode(ctx, fed, owner, obj, bindingGroups(attrToRegions), associationReplace)
}

// RegisterObjectInstanceWithRegions: associates regions with the
// instance's attributes at register time. The caller passes the
// already-assigned ObjectHandle (from object.Registry.Register) and a
// per-attribute region map.
//
// In the 1516.1-2010 §6.7 spec this is a single fused call; the cut-2
// gorti split (Register + AssociateRegionsWithObjectInstance) keeps the
// object handle assignment in object.Registry as the single source of
// truth. The method here is a thin alias kept for spec-name parity;
// the gRPC handler (M10 W2) will dispatch to this method.
func (m *Manager) RegisterObjectInstanceWithRegions(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrToRegions map[core.AttributeHandle][]RegionHandle,
) error {
	return m.AssociateRegionsWithObjectInstance(ctx, fed, owner, obj, attrToRegions)
}

// PublisherRegionsFor returns the publisher's region set for (obj, attr),
// or nil when no DDM association exists. The object.Registry hot path
// uses the nil/empty distinction to bypass overlap testing (FR-DDM-6:
// zero-cost when no regions are in play).
func (m *Manager) PublisherRegionsFor(
	fed core.FederationName,
	obj core.ObjectHandle,
	attr core.AttributeHandle,
) []RegionHandle {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return nil
	}
	per, ok := st.objPubs[obj]
	if !ok {
		return nil
	}
	rs, ok := per[attr]
	if !ok {
		return nil
	}
	out := make([]RegionHandle, len(rs))
	copy(out, rs)
	return out
}

// HasObjectAssociations reports whether any DDM region associations
// exist for the given object. Lets the registry fast-path the
// no-DDM-in-play case without locking the per-attr map.
func (m *Manager) HasObjectAssociations(fed core.FederationName, obj core.ObjectHandle) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return false
	}
	_, ok = st.objPubs[obj]
	return ok
}

// SubscribersForUpdate returns the federates that should receive an
// update of (obj, attr) given the publisher's region(s). Used by the
// object.Registry's fan-out path to filter subscribers.
//
// Returns the set of subscriber handles in handle-sorted order
// (NFR-DET-1). Empty slice = no subscribers in scope (the publisher's
// regions don't overlap any subscriber's regions; the update is
// dropped).
//
// FR-DDM-5: overlap is computed deterministically across all dimensions
// of the routing space.
//
// FR-DDM-6 hot-path contract: when publisherRegions is empty the
// method returns nil immediately. Callers MUST then fall back to the
// declaration.Manager's default subscriber list (cut-1 behavior).
func (m *Manager) SubscribersForUpdate(
	fed core.FederationName,
	cls core.ObjectClassHandle,
	attr core.AttributeHandle,
	publisherRegions []RegionHandle,
) []core.FederateHandle {
	if len(publisherRegions) == 0 {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return []core.FederateHandle{}
	}
	subs := st.objSubs[objAttrKey{cls: cls, attr: attr}]
	if len(subs) == 0 {
		return []core.FederateHandle{}
	}
	pubBounds := st.objectRealizations(cls, attr, publisherRegions)
	hits := map[core.FederateHandle]struct{}{}
	for _, s := range subs {
		subBounds := st.objectRealizations(cls, attr, s.regions)
		if regionsOverlap(pubBounds, subBounds) {
			hits[s.subscriber] = struct{}{}
		}
	}
	return sortedFederateHandles(hits)
}

// RegionSubscribersFor returns regional subscribers whose contextual
// realizations overlap the default publisher region. A realization with
// every available dimension excluded is empty and cannot be in scope.
//
// Zero-cost contract: when no region subscriptions exist for the pair
// the method is one map miss and returns nil.
func (m *Manager) RegionSubscribersFor(
	fed core.FederationName,
	cls core.ObjectClassHandle,
	attr core.AttributeHandle,
) []core.FederateHandle {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return nil
	}
	subs := st.objSubs[objAttrKey{cls: cls, attr: attr}]
	if len(subs) == 0 {
		return nil
	}
	hits := map[core.FederateHandle]struct{}{}
	for _, s := range subs {
		if overlapsDefault(st.objectRealizations(cls, attr, s.regions)) {
			hits[s.subscriber] = struct{}{}
		}
	}
	return sortedFederateHandles(hits)
}

// InteractionSubscribersForSend is the interaction-side analogue of
// SubscribersForUpdate. Returns nil for the zero-cost (no producer
// regions) path; callers fall back to the declaration.Manager.
func (m *Manager) InteractionSubscribersForSend(
	fed core.FederationName,
	cls core.InteractionClassHandle,
	producerRegions []RegionHandle,
) []core.FederateHandle {
	if len(producerRegions) == 0 {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return []core.FederateHandle{}
	}
	subs := st.intSubs[cls]
	if len(subs) == 0 {
		return []core.FederateHandle{}
	}
	pubBounds := st.interactionRealizations(cls, producerRegions)
	hits := map[core.FederateHandle]struct{}{}
	for _, s := range subs {
		subBounds := st.interactionRealizations(cls, s.regions)
		if regionsOverlap(pubBounds, subBounds) {
			hits[s.subscriber] = struct{}{}
		}
	}
	return sortedFederateHandles(hits)
}

// Snapshot returns aggregate DDM state for the AdminService handler.
// Phase 1 of the rtid-TUI plan: region count only — sufficient for the
// drill-down view's "Region count: N (no DDM activity)" line. Read
// under the manager RLock; cheap.
func (m *Manager) Snapshot(fed core.FederationName) core.DDMSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return core.DDMSnapshot{}
	}
	return core.DDMSnapshot{
		RegionCount: uint32(len(st.regions)),
	}
}

// sortedFederateHandles materializes a federate-handle set as a sorted
// slice. Returns an empty (non-nil) slice for nil/empty input
// (NFR-DET-1).
func sortedFederateHandles(set map[core.FederateHandle]struct{}) []core.FederateHandle {
	out := make([]core.FederateHandle, 0, len(set))
	for h := range set {
		out = append(out, h)
	}
	slices.Sort(out)
	return out
}
