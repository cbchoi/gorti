package ddm

import (
	"context"
	"errors"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ErrNotImplemented is returned by stub methods until Agent A
// implements them in M10.
var ErrNotImplemented = errors.New("ddm: not implemented (Agent A M10 deliverable)")

// DimensionHandle identifies one dimension of a routing space.
// 1-based; 0 is the invalid sentinel.
type DimensionHandle uint64

// RegionHandle identifies one region instance.
// 1-based; 0 is the invalid sentinel.
type RegionHandle uint64

// RoutingSpaceHandle identifies one routing space.
// 1-based; 0 is the invalid sentinel.
type RoutingSpaceHandle uint64

// Range describes a closed-open interval [Lower, Upper) on one
// dimension. Per IEEE 1516.1-2010 §6.5; bounds are uint64 normalized
// values (the FOM declares the upper bound + normalization).
type Range struct {
	Lower uint64
	Upper uint64
}

// Overlap returns true iff this range overlaps r (closed-open
// semantics, so [0,5) and [5,10) do NOT overlap; [0,5) and [4,10) do).
func (r Range) Overlap(other Range) bool {
	return r.Lower < other.Upper && other.Lower < r.Upper
}

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
}

// Manager owns per-federation routing-space + region + scoped
// subscription state. Goroutine-safe.
//
// FROZEN-shape per docs/srs.md FR-DDM-1..6.
type Manager struct {
	opts Options
}

// New constructs a Manager. Returns an error if any required Options
// field is nil.
func New(opts Options) (*Manager, error) {
	_ = opts
	return &Manager{opts: opts}, ErrNotImplemented
}

// --- Routing-space queries (FR-DDM-1) -------------------------------------

// LookupRoutingSpace returns the handle for a named routing space
// declared in the federation's FOM. Returns 0 + false if not declared.
func (m *Manager) LookupRoutingSpace(fed core.FederationName, name string) (RoutingSpaceHandle, bool) {
	_ = fed
	_ = name
	return 0, false
}

// LookupDimension returns the handle for a named dimension within a
// routing space. Returns 0 + false if not declared.
func (m *Manager) LookupDimension(fed core.FederationName, space RoutingSpaceHandle, name string) (DimensionHandle, bool) {
	_ = fed
	_ = space
	_ = name
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
	_ = ctx
	_ = fed
	_ = owner
	_ = space
	_ = dims
	return 0, ErrNotImplemented
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
) error {
	_ = ctx
	_ = fed
	_ = owner
	_ = regions
	return ErrNotImplemented
}

// SetRangeBounds sets the [Lower, Upper) bounds for a (region,
// dimension) pair. Bounds are pending until CommitRegionModifications.
func (m *Manager) SetRangeBounds(
	fed core.FederationName,
	owner core.FederateHandle,
	region RegionHandle,
	dim DimensionHandle,
	bounds Range,
) error {
	_ = fed
	_ = owner
	_ = region
	_ = dim
	_ = bounds
	return ErrNotImplemented
}

// DeleteRegion removes a region. The region MUST NOT be in use by any
// active subscription or registered object instance.
func (m *Manager) DeleteRegion(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	region RegionHandle,
) error {
	_ = ctx
	_ = fed
	_ = owner
	_ = region
	return ErrNotImplemented
}

// QueryBounds returns the committed bounds of (region, dim). Returns
// zero Range + false if not set.
func (m *Manager) QueryBounds(
	fed core.FederationName,
	region RegionHandle,
	dim DimensionHandle,
) (Range, bool) {
	_ = fed
	_ = region
	_ = dim
	return Range{}, false
}

// --- Region-scoped subscriptions (FR-DDM-3) -------------------------------

// SubscribeObjectClassAttributesWithRegions extends the cut-1
// SubscribeObjectClassAttributes with a set of regions; the subscriber
// only receives updates whose publisher region(s) overlap one of the
// subscribed regions.
func (m *Manager) SubscribeObjectClassAttributesWithRegions(
	ctx context.Context,
	fed core.FederationName,
	subscriber core.FederateHandle,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
	regions []RegionHandle,
) error {
	_ = ctx
	_ = fed
	_ = subscriber
	_ = cls
	_ = attrs
	_ = regions
	return ErrNotImplemented
}

// SubscribeInteractionClassWithRegions: same shape for interactions.
func (m *Manager) SubscribeInteractionClassWithRegions(
	ctx context.Context,
	fed core.FederationName,
	subscriber core.FederateHandle,
	cls core.InteractionClassHandle,
	regions []RegionHandle,
) error {
	_ = ctx
	_ = fed
	_ = subscriber
	_ = cls
	_ = regions
	return ErrNotImplemented
}

// --- Region-scoped publishing (FR-DDM-4) ----------------------------------

// RegisterObjectInstanceWithRegions: associates regions with the
// instance's attributes at register time.
func (m *Manager) RegisterObjectInstanceWithRegions(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	cls core.ObjectClassHandle,
	attrToRegions map[core.AttributeHandle][]RegionHandle,
) (core.ObjectHandle, error) {
	_ = ctx
	_ = fed
	_ = owner
	_ = cls
	_ = attrToRegions
	return 0, ErrNotImplemented
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
func (m *Manager) SubscribersForUpdate(
	fed core.FederationName,
	cls core.ObjectClassHandle,
	attr core.AttributeHandle,
	publisherRegions []RegionHandle,
) []core.FederateHandle {
	_ = fed
	_ = cls
	_ = attr
	_ = publisherRegions
	return nil
}
