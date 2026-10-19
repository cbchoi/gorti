package ddm

import (
	"context"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type ManagerState struct {
	NextRegionHandle         RegionHandle
	Regions                  []RegionState
	ObjectSubscriptions      []ObjectSubscriptionState
	InteractionSubscriptions []InteractionSubscriptionState
	ObjectPublications       []ObjectPublicationState
	InactiveAssociations     []InactiveAssociationState
	ConveyedRegions          []ConveyedRegionState
}

type ConveyedRegionState struct {
	Designator  RegionHandle
	Realization core.RegionRealizationSnapshot
	Recipients  []ConveyedRegionRecipientState
}

type ConveyedRegionRecipientState struct {
	Federate core.FederateHandle
	Active   bool
}

type RegionState struct {
	// Missing in legacy snapshots means an already committed specification.
	Template     bool
	Handle       RegionHandle
	Owner        core.FederateHandle
	RoutingSpace RoutingSpaceHandle
	Dimensions   []DimensionHandle
	Committed    []RangeState
	Pending      []RangeState
}

type RangeState struct {
	Dimension    DimensionHandle
	Lower, Upper uint64
}
type ObjectSubscriptionState struct {
	ObjectClass          core.ObjectClassHandle
	Attribute            core.AttributeHandle
	Subscriber           core.FederateHandle
	Regions              []RegionHandle
	Passive              bool
	UpdateRateDesignator string
}
type InteractionSubscriptionState struct {
	InteractionClass core.InteractionClassHandle
	Subscriber       core.FederateHandle
	Regions          []RegionHandle
	Passive          bool
}
type ObjectPublicationState struct {
	Object    core.ObjectHandle
	Attribute core.AttributeHandle
	Regions   []RegionHandle
}

type InactiveAssociationState struct {
	Object    core.ObjectHandle
	Attribute core.AttributeHandle
	Federate  core.FederateHandle
	Regions   []RegionHandle
}

func (m *Manager) ExportState(fed core.FederationName) *ManagerState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return nil
	}
	out := &ManagerState{NextRegionHandle: st.nextRegionHandle}
	st.exportRegions(out)
	st.exportSubscriptions(out)
	st.exportAssociations(out)
	return out
}

func (st *federationDDMState) exportRegions(out *ManagerState) {
	conveyedHandles := make([]RegionHandle, 0, len(st.conveyed))
	for handle := range st.conveyed {
		conveyedHandles = append(conveyedHandles, handle)
	}
	slices.Sort(conveyedHandles)
	for _, handle := range conveyedHandles {
		entry := ConveyedRegionState{Designator: handle, Realization: st.conveyed[handle]}
		for receiver, active := range st.conveyedRecipients[handle] {
			entry.Recipients = append(entry.Recipients, ConveyedRegionRecipientState{Federate: receiver, Active: active})
		}
		slices.SortFunc(entry.Recipients, func(a, b ConveyedRegionRecipientState) int { return compare64(uint64(a.Federate), uint64(b.Federate)) })
		out.ConveyedRegions = append(out.ConveyedRegions, entry)
	}
	regionHandles := make([]RegionHandle, 0, len(st.regions))
	for handle := range st.regions {
		regionHandles = append(regionHandles, handle)
	}
	slices.Sort(regionHandles)
	for _, handle := range regionHandles {
		region := st.regions[handle]
		out.Regions = append(out.Regions, RegionState{Template: region.template, Handle: handle, Owner: region.owner, RoutingSpace: region.space, Dimensions: append([]DimensionHandle(nil), region.dims...), Committed: exportRanges(region.committed), Pending: exportRanges(region.pending)})
	}
}

func (st *federationDDMState) exportSubscriptions(out *ManagerState) {
	objKeys := make([]objAttrKey, 0, len(st.objSubs))
	for key := range st.objSubs {
		objKeys = append(objKeys, key)
	}
	slices.SortFunc(objKeys, func(a, b objAttrKey) int {
		if a.cls != b.cls {
			return compare64(uint64(a.cls), uint64(b.cls))
		}
		return compare64(uint64(a.attr), uint64(b.attr))
	})
	for _, key := range objKeys {
		subs := append([]subscription(nil), st.objSubs[key]...)
		for i := range subs {
			subs[i].regions = slices.Clone(subs[i].regions)
			slices.Sort(subs[i].regions)
		}
		slices.SortFunc(subs, func(a, b subscription) int {
			if a.subscriber != b.subscriber {
				return compare64(uint64(a.subscriber), uint64(b.subscriber))
			}
			return slices.Compare(a.regions, b.regions)
		})
		for _, sub := range subs {
			out.ObjectSubscriptions = append(out.ObjectSubscriptions, ObjectSubscriptionState{ObjectClass: key.cls, Attribute: key.attr, Subscriber: sub.subscriber, Regions: append([]RegionHandle(nil), sub.regions...), Passive: sub.passive, UpdateRateDesignator: sub.updateRate})
		}
	}
	classes := make([]core.InteractionClassHandle, 0, len(st.intSubs))
	for class := range st.intSubs {
		classes = append(classes, class)
	}
	slices.Sort(classes)
	for _, class := range classes {
		subs := append([]interactionSubscription(nil), st.intSubs[class]...)
		for i := range subs {
			subs[i].regions = slices.Clone(subs[i].regions)
			slices.Sort(subs[i].regions)
		}
		slices.SortFunc(subs, func(a, b interactionSubscription) int {
			if a.subscriber != b.subscriber {
				return compare64(uint64(a.subscriber), uint64(b.subscriber))
			}
			return slices.Compare(a.regions, b.regions)
		})
		for _, sub := range subs {
			out.InteractionSubscriptions = append(out.InteractionSubscriptions, InteractionSubscriptionState{InteractionClass: class, Subscriber: sub.subscriber, Regions: append([]RegionHandle(nil), sub.regions...), Passive: sub.passive})
		}
	}
}

func (st *federationDDMState) exportAssociations(out *ManagerState) {
	objects := make([]core.ObjectHandle, 0, len(st.objPubs))
	for object := range st.objPubs {
		objects = append(objects, object)
	}
	slices.Sort(objects)
	for _, object := range objects {
		attrs := make([]core.AttributeHandle, 0, len(st.objPubs[object]))
		for attr := range st.objPubs[object] {
			attrs = append(attrs, attr)
		}
		slices.Sort(attrs)
		for _, attr := range attrs {
			out.ObjectPublications = append(out.ObjectPublications, ObjectPublicationState{Object: object, Attribute: attr, Regions: append([]RegionHandle(nil), st.objPubs[object][attr]...)})
		}
	}
	for obj, callers := range st.inactivePubs {
		for caller, attrs := range callers {
			for attr, regions := range attrs {
				out.InactiveAssociations = append(out.InactiveAssociations, InactiveAssociationState{Object: obj, Attribute: attr, Federate: caller, Regions: slices.Clone(regions)})
			}
		}
	}
	slices.SortFunc(out.InactiveAssociations, func(a, b InactiveAssociationState) int {
		if a.Object != b.Object {
			return compare64(uint64(a.Object), uint64(b.Object))
		}
		if a.Attribute != b.Attribute {
			return compare64(uint64(a.Attribute), uint64(b.Attribute))
		}
		return compare64(uint64(a.Federate), uint64(b.Federate))
	})
}

func (m *Manager) ImportState(fed core.FederationName, state *ManagerState) {
	if state == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(context.Background(), fed)
	// Restore can roll the FOM revision back and later reuse its number for a
	// different extension. Force lazy refresh from the restored repository.
	st.fomRevision = 0
	st.nextRegionHandle = state.NextRegionHandle
	st.conveyed = make(map[RegionHandle]core.RegionRealizationSnapshot, len(state.ConveyedRegions))
	st.conveyedRecipients = make(map[RegionHandle]map[core.FederateHandle]bool, len(state.ConveyedRegions))
	for _, entry := range state.ConveyedRegions {
		if entry.Designator > st.nextRegionHandle {
			st.nextRegionHandle = entry.Designator
		}
		if len(entry.Recipients) == 0 {
			continue
		}
		st.conveyed[entry.Designator] = entry.Realization
		st.conveyedRecipients[entry.Designator] = make(map[core.FederateHandle]bool, len(entry.Recipients))
		for _, receiver := range entry.Recipients {
			st.conveyedRecipients[entry.Designator][receiver.Federate] = receiver.Active
		}
	}
	st.regions = map[RegionHandle]*regionState{}
	for _, entry := range state.Regions {
		st.regions[entry.Handle] = &regionState{template: entry.Template, owner: entry.Owner, space: entry.RoutingSpace, dims: append([]DimensionHandle(nil), entry.Dimensions...), committed: importRanges(entry.Committed), pending: importRanges(entry.Pending)}
	}
	st.objSubs = map[objAttrKey][]subscription{}
	for _, entry := range state.ObjectSubscriptions {
		key := objAttrKey{cls: entry.ObjectClass, attr: entry.Attribute}
		st.objSubs[key] = append(st.objSubs[key], subscription{subscriber: entry.Subscriber, regions: append([]RegionHandle(nil), entry.Regions...), passive: entry.Passive, updateRate: entry.UpdateRateDesignator})
	}
	st.intSubs = map[core.InteractionClassHandle][]interactionSubscription{}
	for _, entry := range state.InteractionSubscriptions {
		st.intSubs[entry.InteractionClass] = append(st.intSubs[entry.InteractionClass], interactionSubscription{subscriber: entry.Subscriber, regions: append([]RegionHandle(nil), entry.Regions...), passive: entry.Passive})
	}
	st.objPubs = map[core.ObjectHandle]map[core.AttributeHandle][]RegionHandle{}
	for _, entry := range state.ObjectPublications {
		if st.objPubs[entry.Object] == nil {
			st.objPubs[entry.Object] = map[core.AttributeHandle][]RegionHandle{}
		}
		st.objPubs[entry.Object][entry.Attribute] = append([]RegionHandle(nil), entry.Regions...)
	}
	st.inactivePubs = map[core.ObjectHandle]map[core.FederateHandle]map[core.AttributeHandle][]RegionHandle{}
	for _, entry := range state.InactiveAssociations {
		st.inactiveFor(entry.Object, entry.Federate, true)[entry.Attribute] = slices.Clone(entry.Regions)
	}
}

func exportRanges(in map[DimensionHandle]Range) []RangeState {
	keys := make([]DimensionHandle, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	out := make([]RangeState, 0, len(keys))
	for _, key := range keys {
		value := in[key]
		out = append(out, RangeState{Dimension: key, Lower: value.Lower, Upper: value.Upper})
	}
	return out
}
func importRanges(in []RangeState) map[DimensionHandle]Range {
	out := map[DimensionHandle]Range{}
	for _, entry := range in {
		out[entry.Dimension] = Range{Lower: entry.Lower, Upper: entry.Upper}
	}
	return out
}
func compare64(a, b uint64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
