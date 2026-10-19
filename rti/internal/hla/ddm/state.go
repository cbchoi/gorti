package ddm

import (
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// objAttrKey identifies a (class, attribute) pair within a federation.
// Same shape as declaration.objAttrKey; duplicated here to keep the
// ddm package free of declaration internals (CODING_CONVENTIONS.md
// "three lines beats premature abstraction").
type objAttrKey struct {
	cls  core.ObjectClassHandle
	attr core.AttributeHandle
}

// regionState is the per-region record. Bounds are split into
// "committed" (visible to overlap tests) and "pending" (set via
// SetRangeBounds, not yet visible) per IEEE 1516.1-2010 §6.5 atomic-
// commit semantics.
type regionState struct {
	template  bool
	owner     core.FederateHandle
	space     RoutingSpaceHandle
	dims      []DimensionHandle
	committed map[DimensionHandle]Range
	pending   map[DimensionHandle]Range
}

// subscription groups one federate's regions sharing the same options for a
// (class, attribute). A federate may have multiple disjoint groups.
type subscription struct {
	subscriber core.FederateHandle
	regions    []RegionHandle
	passive    bool
	updateRate string
}

// Each subscriber may have disjoint active and passive region groups.
type interactionSubscription struct {
	subscriber core.FederateHandle
	regions    []RegionHandle
	passive    bool
}

// federationDDMState holds the per-federation DDM tables. All access
// is serialized through Manager.mu.
type federationDDMState struct {
	fom         core.FOMHandle
	dimDefaults map[DimensionHandle]*Range
	// Routing-space declarations populated from the FOM at first
	// use (or lazily by permissive lookups when no FOM data is
	// available).
	routingSpaces map[string]RoutingSpaceHandle
	dimensions    map[RoutingSpaceHandle]map[string]DimensionHandle
	dimUpperBound map[DimensionHandle]uint64

	// Region store. nextRegionHandle is monotonic per federation and
	// reproducible across replays; the eventlog wiring is M10 W2.
	nextRegionHandle   RegionHandle
	regions            map[RegionHandle]*regionState
	conveyed           map[RegionHandle]core.RegionRealizationSnapshot
	conveyedRecipients map[RegionHandle]map[core.FederateHandle]bool

	// Subscribed-with-regions: by (class, attr) for object updates,
	// by class for interactions.
	objSubs map[objAttrKey][]subscription
	intSubs map[core.InteractionClassHandle][]interactionSubscription

	// Published-with-regions: per object instance, the per-attribute
	// region set the producer associated at register time.
	objPubs map[core.ObjectHandle]map[core.AttributeHandle][]RegionHandle
	// Nonowners retain their own associations without affecting update routing.
	inactivePubs map[core.ObjectHandle]map[core.FederateHandle]map[core.AttributeHandle][]RegionHandle

	// permissive is true when the FOMHandle for this federation does
	// not implement DimensionEnumerator. In that mode every Lookup
	// resolves to a freshly-minted handle (matching the spec-test
	// fixture stub semantics) and every dimension upper-bound
	// defaults to MaxUint64 so initial regions cover the full space.
	permissive bool

	// nextDimHandle tracks the per-federation dimension counter so
	// permissive-mode dimensions and FOM-populated dimensions share
	// a single namespace.
	nextDimHandle DimensionHandle
	fomRevision   uint64
}

func newFederationDDMState() *federationDDMState {
	return &federationDDMState{
		routingSpaces: map[string]RoutingSpaceHandle{},
		dimensions:    map[RoutingSpaceHandle]map[string]DimensionHandle{},
		dimUpperBound: map[DimensionHandle]uint64{},
		regions:       map[RegionHandle]*regionState{},
		objSubs:       map[objAttrKey][]subscription{},
		intSubs:       map[core.InteractionClassHandle][]interactionSubscription{},
		objPubs:       map[core.ObjectHandle]map[core.AttributeHandle][]RegionHandle{},
		inactivePubs:  map[core.ObjectHandle]map[core.FederateHandle]map[core.AttributeHandle][]RegionHandle{},
		permissive:    true, // upgraded to false by populateFromFOM
		dimDefaults:   map[DimensionHandle]*Range{},
	}
}

// populateFromFOM seeds the routing-space + dimension tables from a
// FOM Dimensions slice. Per the 1516-2010 flat-dimension model every
// dimension belongs to the implicit "default" routing space.
//
// Calling this method switches the federation out of permissive mode:
// subsequent lookups on names not present in the FOM return (0,
// false) instead of silently minting handles.
func (st *federationDDMState) populateFromFOM(dims []model.Dimension) {
	st.permissive = false
	if len(dims) == 0 {
		// Even with no dimensions declared, eagerly create the
		// implicit "default" routing space with an empty dim map so
		// subsequent CreateRegion calls fail with
		// ErrDimensionNotFound rather than ErrRoutingSpaceNotFound
		// (matches the more-specific error per FR-DDM-2).
		const space = RoutingSpaceHandle(1)
		st.routingSpaces[DefaultRoutingSpace] = space
		st.dimensions[space] = map[string]DimensionHandle{}
		return
	}
	const space = RoutingSpaceHandle(1)
	st.routingSpaces[DefaultRoutingSpace] = space
	dimsByName := make(map[string]DimensionHandle, len(dims))
	st.dimensions[space] = dimsByName
	for _, d := range dims {
		st.nextDimHandle++
		dh := st.nextDimHandle
		dimsByName[d.Name] = dh
		upper := d.UpperBound
		if upper == 0 {
			upper = ^uint64(0) // Legacy model sentinel for an unspecified upper bound.
		}
		st.dimUpperBound[dh] = upper
		st.setDimensionDefault(dh, d)
	}
}

func (st *federationDDMState) hasRoutingSpace(h RoutingSpaceHandle) bool {
	for _, v := range st.routingSpaces {
		if v == h {
			return true
		}
	}
	return false
}

func (st *federationDDMState) hasDimension(space RoutingSpaceHandle, dim DimensionHandle) bool {
	dims, ok := st.dimensions[space]
	if !ok {
		return false
	}
	for _, v := range dims {
		if v == dim {
			return true
		}
	}
	return false
}

// regionInUse reports whether the given region is referenced by any
// active subscription or any object-publisher association. Used by
// DeleteRegion to enforce the §6.5 "no in-use deletion" rule.
func (st *federationDDMState) regionInUse(rh RegionHandle) bool {
	for _, subs := range st.objSubs {
		for _, s := range subs {
			for _, r := range s.regions {
				if r == rh {
					return true
				}
			}
		}
	}
	for _, subs := range st.intSubs {
		for _, s := range subs {
			for _, r := range s.regions {
				if r == rh {
					return true
				}
			}
		}
	}
	for _, per := range st.objPubs {
		for _, regions := range per {
			for _, r := range regions {
				if r == rh {
					return true
				}
			}
		}
	}
	return false
}

// removeObjSubLocked drops every entry of objSubs[key] whose subscriber
// matches fed, including all option groups during rollback and cleanup.
func (st *federationDDMState) removeObjSubLocked(key objAttrKey, fed core.FederateHandle) {
	subs := st.objSubs[key]
	if len(subs) == 0 {
		return
	}
	out := subs[:0]
	for _, s := range subs {
		if s.subscriber == fed {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		delete(st.objSubs, key)
		return
	}
	st.objSubs[key] = out
}

func (st *federationDDMState) removeIntSubLocked(cls core.InteractionClassHandle, fed core.FederateHandle) {
	subs := st.intSubs[cls]
	if len(subs) == 0 {
		return
	}
	out := subs[:0]
	for _, s := range subs {
		if s.subscriber == fed {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		delete(st.intSubs, cls)
		return
	}
	st.intSubs[cls] = out
}

// regionBounds is the per-region committed bounds snapshot used by the
// overlap test. Only shared dimensions constrain overlap; explicit regions
// with no common dimension do not overlap (IEEE 1516.1-2010 9.1.4).
type regionBounds map[DimensionHandle]Range

// materializeRegions collects committed bounds for each handle. Caller
// must hold the appropriate lock. Unknown / deleted region handles
// produce empty bounds (won't overlap anything).
func (st *federationDDMState) materializeRegions(handles []RegionHandle) []regionBounds {
	out := make([]regionBounds, 0, len(handles))
	for _, rh := range handles {
		if snapshot, ok := st.conveyed[rh]; ok {
			b := make(regionBounds)
			for _, dimension := range snapshot.DimensionRanges() {
				b[dimension.Dimension] = dimension.Bounds
			}
			out = append(out, b)
			continue
		}
		rs, ok := st.regions[rh]
		if !ok || rs.template {
			continue
		}
		// Defensive shallow copy of the committed map so the
		// overlap test sees a stable snapshot even if a concurrent
		// commit lands.
		b := make(regionBounds, len(rs.committed))
		for d, r := range rs.committed {
			b[d] = r
		}
		out = append(out, b)
	}
	return out
}

func (st *federationDDMState) setDimensionDefault(handle DimensionHandle, d model.Dimension) {
	lower, upper, excluded, err := model.DimensionDefaultRange(d)
	st.dimDefaults[handle] = nil
	if err == nil && !excluded {
		st.dimDefaults[handle] = &Range{Lower: lower, Upper: upper}
	}
}

// A realization is contextual: only omitted available dimensions acquire FDD
// defaults. Excluded dimensions remain absent unless explicitly specified.
func (st *federationDDMState) realize(handles []RegionHandle, available []DimensionHandle, strict bool) []regionBounds {
	var out []regionBounds
	for _, handle := range handles {
		materialized := st.materializeRegions([]RegionHandle{handle})
		if len(materialized) == 0 {
			continue
		}
		bounds := materialized[0]
		out = append(out, bounds)
		if _, copied := st.conveyed[handle]; copied || !strict {
			continue
		}
		for _, dimension := range available {
			if _, explicit := bounds[dimension]; !explicit {
				if value := st.dimDefaults[dimension]; value != nil {
					bounds[dimension] = *value
				}
			}
		}
	}
	return out
}

func (st *federationDDMState) objectRealizations(cls core.ObjectClassHandle, attr core.AttributeHandle, handles []RegionHandle) []regionBounds {
	if dimensions, ok := st.fom.(core.FOMAttributeDimensions); ok {
		available, found := dimensions.AttributeDimensions(cls, attr)
		return st.realize(handles, dimensionSet(available), found)
	}
	return st.materializeRegions(handles)
}

func (st *federationDDMState) interactionRealizations(cls core.InteractionClassHandle, handles []RegionHandle) []regionBounds {
	if dimensions, ok := st.fom.(core.FOMInteractionDimensions); ok {
		available, found := dimensions.InteractionDimensions(cls)
		return st.realize(handles, dimensionSet(available), found)
	}
	return st.materializeRegions(handles)
}

func overlapsDefault(realizations []regionBounds) bool {
	for _, bounds := range realizations {
		nonempty := len(bounds) != 0
		for _, value := range bounds {
			if value.Lower >= value.Upper {
				nonempty = false
				break
			}
		}
		if nonempty {
			return true
		}
	}
	return false
}
