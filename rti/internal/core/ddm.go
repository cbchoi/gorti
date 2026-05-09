package core

import "context"

// DDMRoutingSpaceHandle identifies one routing space declared in a
// federation's FOM. 1-based; 0 is the invalid sentinel. The
// rti/internal/ddm package aliases its own RoutingSpaceHandle to this
// type so a single concrete *ddm.Manager satisfies the
// DataDistributionManagement interface declared in this file.
type DDMRoutingSpaceHandle uint64

// DDMDimensionHandle identifies one dimension within a routing space.
// 1-based; 0 is the invalid sentinel. (See note above re: aliasing.)
type DDMDimensionHandle uint64

// DDMRegionHandleCore identifies one region instance. The "Core"
// suffix disambiguates from object.DDMRegionHandle (the consumer-
// defined narrow alias used by object.Registry's three-method
// DDMFilter view). The two have identical uint64 layout; the
// production composition root (cmd/rtid) bridges between them via a
// small adapter (ddmFilterAdapter).
type DDMRegionHandleCore uint64

// DDMRange describes a closed-open interval [Lower, Upper) on one
// dimension (IEEE 1516.1-2010 §6.5). Bounds are uint64 normalized
// values declared by the FOM.
type DDMRange struct {
	Lower uint64
	Upper uint64
}

// Overlap reports whether this range overlaps other under closed-open
// semantics: [0,5) and [5,10) do NOT overlap; [0,5) and [4,10) do.
func (r DDMRange) Overlap(other DDMRange) bool {
	return r.Lower < other.Upper && other.Lower < r.Upper
}

// DataDistributionManagement owns per-federation routing spaces,
// regions, and region-scoped pub/sub state per IEEE 1516.1-2010 §6.
// Phase 1 of the research-platform refactor (docs/research-platform.md
// §5.4) carves this out as the service-level interface so alternative
// implementations (interval-tree overlap, dimension-hash prefilter,
// etc. — see Phase 2 §6.2) can plug in without forking the gRPC
// handler.
//
// Production impl: rti/internal/ddm.Manager.
//
// Relationship to object.DDMFilter: object.Registry consumes a
// narrow three-method surface (HasObjectAssociations,
// PublisherRegionsFor, SubscribersForUpdate) defined in the object
// package — that consumer-defined narrow interface stays where its
// only consumer lives, untouched by this Phase 1 refactor.
// DataDistributionManagement is the SUPERSET that the gRPC handler
// (and any future composition root logic) needs; researchers swapping
// the DDM service satisfy this larger surface and supply an adapter
// for object.DDMFilter (cmd/rtid wires that adapter today via
// ddmFilterAdapter).
//
// Concurrency: implementations must be goroutine-safe.
type DataDistributionManagement interface {
	// --- Routing-space queries (FR-DDM-1) ---------------------------------

	// LookupRoutingSpace returns the handle for a named routing space.
	LookupRoutingSpace(fed FederationName, name string) (DDMRoutingSpaceHandle, bool)

	// LookupDimension returns the handle for a named dimension within
	// a routing space.
	LookupDimension(fed FederationName, space DDMRoutingSpaceHandle, name string) (DDMDimensionHandle, bool)

	// --- Region lifecycle (FR-DDM-2) --------------------------------------

	// CreateRegion creates a new region with [0, dim-upper) initial
	// bounds for each dim in dims.
	CreateRegion(
		ctx context.Context,
		fed FederationName,
		owner FederateHandle,
		space DDMRoutingSpaceHandle,
		dims []DDMDimensionHandle,
	) (DDMRegionHandleCore, error)

	// SetRangeBounds queues a bounds change for (region, dim). Bounds
	// stay pending until CommitRegionModifications.
	SetRangeBounds(
		fed FederationName,
		owner FederateHandle,
		region DDMRegionHandleCore,
		dim DDMDimensionHandle,
		bounds DDMRange,
	) error

	// CommitRegionModifications applies pending bound changes
	// atomically across the supplied region set.
	CommitRegionModifications(
		ctx context.Context,
		fed FederationName,
		owner FederateHandle,
		regions []DDMRegionHandleCore,
	) error

	// DeleteRegion removes an unused region.
	DeleteRegion(
		ctx context.Context,
		fed FederationName,
		owner FederateHandle,
		region DDMRegionHandleCore,
	) error

	// QueryBounds returns the committed bounds of (region, dim).
	QueryBounds(
		fed FederationName,
		region DDMRegionHandleCore,
		dim DDMDimensionHandle,
	) (DDMRange, bool)

	// --- Region-scoped subscriptions (FR-DDM-3) ---------------------------

	// SubscribeObjectClassAttributesWithRegions extends the cut-1
	// subscribe with a region set; only updates whose publisher
	// regions overlap one of these are delivered.
	SubscribeObjectClassAttributesWithRegions(
		ctx context.Context,
		fed FederationName,
		subscriber FederateHandle,
		cls ObjectClassHandle,
		attrs []AttributeHandle,
		regions []DDMRegionHandleCore,
	) error

	// SubscribeInteractionClassWithRegions: same shape for
	// interactions.
	SubscribeInteractionClassWithRegions(
		ctx context.Context,
		fed FederationName,
		subscriber FederateHandle,
		cls InteractionClassHandle,
		regions []DDMRegionHandleCore,
	) error

	// AssociateRegionsForUpdates — IEEE 1516.1-2010 §9.6 (M23 W5).
	// Records the publisher's per-attribute region associations for an
	// existing object instance.
	AssociateRegionsForUpdates(
		ctx context.Context,
		fed FederationName,
		owner FederateHandle,
		obj ObjectHandle,
		attrToRegions map[AttributeHandle][]DDMRegionHandleCore,
	) error

	// UnassociateRegionsForUpdates — IEEE 1516.1-2010 §9.7 (M23 W5).
	// Drops associations matching the supplied pairs. Empty map drops
	// ALL associations for the object.
	UnassociateRegionsForUpdates(
		ctx context.Context,
		fed FederationName,
		owner FederateHandle,
		obj ObjectHandle,
		attrToRegions map[AttributeHandle][]DDMRegionHandleCore,
	) error

	// UnsubscribeObjectClassAttributesWithRegions — §9.9 (M23 W5).
	// Drops the subscriber's region-scoped subscription.
	UnsubscribeObjectClassAttributesWithRegions(
		ctx context.Context,
		fed FederationName,
		subscriber FederateHandle,
		cls ObjectClassHandle,
		attrs []AttributeHandle,
		regions []DDMRegionHandleCore,
	) error

	// UnsubscribeInteractionClassWithRegions — §9.11 (M23 W5).
	UnsubscribeInteractionClassWithRegions(
		ctx context.Context,
		fed FederationName,
		subscriber FederateHandle,
		cls InteractionClassHandle,
		regions []DDMRegionHandleCore,
	) error

	// --- Region-scoped publishing / fan-out (FR-DDM-4..6) -----------------

	// HasObjectAssociations reports whether any DDM region
	// associations exist for the given object. Hot-path fast-check
	// for the object.Registry's no-DDM-in-play case.
	HasObjectAssociations(fed FederationName, obj ObjectHandle) bool

	// PublisherRegionsFor returns the publisher's region set for
	// (obj, attr) (nil when no association exists).
	PublisherRegionsFor(
		fed FederationName,
		obj ObjectHandle,
		attr AttributeHandle,
	) []DDMRegionHandleCore

	// SubscribersForUpdate returns the federates whose region-scoped
	// subscriptions overlap publisherRegions for (cls, attr), in
	// sorted handle order. nil publisherRegions returns nil
	// (zero-cost path; callers fall back to the declaration default).
	SubscribersForUpdate(
		fed FederationName,
		cls ObjectClassHandle,
		attr AttributeHandle,
		publisherRegions []DDMRegionHandleCore,
	) []FederateHandle

	// --- Read-only introspection (rtid-TUI Phase 1) ----------------------

	// Snapshot returns aggregate DDM counters for the AdminService
	// handler. Read-only; cheap.
	Snapshot(fed FederationName) DDMSnapshot
}

// DDMSnapshot is the federation-wide DDM rollup for the AdminService
// Snapshot RPC. Phase 1 keeps this minimal — region count only — so
// the TUI's drill-down view (docs/rtid-tui.md §3.2) shows
// "Region count: N" without per-region detail.
type DDMSnapshot struct {
	RegionCount uint32
}
