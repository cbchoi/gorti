package ddm

import (
	"github.com/cbchoi/gorti/rti/internal/core"
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
	owner     core.FederateHandle
	space     RoutingSpaceHandle
	dims      []DimensionHandle
	committed map[DimensionHandle]Range
	pending   map[DimensionHandle]Range
}

// subscription is one entry in a (cls, attr) subscriber list: the
// federate plus its scoping region set.
type subscription struct {
	subscriber core.FederateHandle
	regions    []RegionHandle
}

type interactionSubscription struct {
	subscriber core.FederateHandle
	regions    []RegionHandle
}

// federationDDMState holds the per-federation DDM tables. All access
// is serialized through Manager.mu.
type federationDDMState struct {
	// Routing-space declarations populated from the FOM at first
	// use (or lazily by permissive lookups when no FOM data is
	// available).
	routingSpaces map[string]RoutingSpaceHandle
	dimensions    map[RoutingSpaceHandle]map[string]DimensionHandle
	dimUpperBound map[DimensionHandle]uint64

	// Region store. nextRegionHandle is monotonic per federation and
	// reproducible across replays; the eventlog wiring is M10 W2.
	nextRegionHandle RegionHandle
	regions          map[RegionHandle]*regionState

	// Subscribed-with-regions: by (class, attr) for object updates,
	// by class for interactions.
	objSubs map[objAttrKey][]subscription
	intSubs map[core.InteractionClassHandle][]interactionSubscription

	// Published-with-regions: per object instance, the per-attribute
	// region set the producer associated at register time.
	objPubs map[core.ObjectHandle]map[core.AttributeHandle][]RegionHandle

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
		permissive:    true, // upgraded to false by populateFromFOM
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
		ub := d.UpperBound
		if ub == 0 {
			ub = ^uint64(0) // unbounded → cover the full uint64 range
		}
		st.dimUpperBound[dh] = ub
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
// matches `fed`. Used to implement REPLACE semantics on
// SubscribeObjectClassAttributesWithRegions.
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
// overlap test. Indexed by dimension; missing dimensions are treated
// as full-range [0, MaxUint64) so a region declared on dim X but not
// dim Y trivially overlaps any other region's Y axis (matches HLA's
// "unspecified dimensions are wildcards" semantics for cut-2).
type regionBounds map[DimensionHandle]Range

// materializeRegions collects committed bounds for each handle. Caller
// must hold the appropriate lock. Unknown / deleted region handles
// produce empty bounds (won't overlap anything).
func (st *federationDDMState) materializeRegions(handles []RegionHandle) []regionBounds {
	out := make([]regionBounds, 0, len(handles))
	for _, rh := range handles {
		rs, ok := st.regions[rh]
		if !ok {
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
