package ddm

import (
	"fmt"
	"slices"

	"google.golang.org/protobuf/proto"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// Marshal serializes the federation's runtime DDM state into a
// DDMManagerState proto and returns its wire bytes. M13 thread C
// (docs/srs.md §10.4): consumed by the savepoint Manager to bundle
// state under the manifest's "ddm" key.
//
// Setup-time configuration (routing-space + dimension declarations
// + per-dimension upper bounds + the permissive flag) is intentionally
// NOT captured — those are FOM-derived and reconstructed at
// restore-time when the federation is re-created against the same FOM.
//
// Returns nil for federations the manager has never seen — Unmarshal
// treats nil/empty as no-op so round-trip is the identity.
//
// Iteration is deterministic: regions sorted by handle, subscriptions
// by (class, attribute, subscriber), publications by (object,
// attribute).
func (m *Manager) Marshal(fed core.FederationName) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return nil, nil
	}
	out := &rtiv1.DDMManagerState{
		NextRegionHandle:         uint64(st.nextRegionHandle),
		Regions:                  ddmRegionsToProto(st.regions),
		ObjectSubscriptions:      ddmObjSubsToProto(st.objSubs),
		InteractionSubscriptions: ddmIntSubsToProto(st.intSubs),
		ObjectPublications:       ddmObjPubsToProto(st.objPubs),
	}
	return proto.Marshal(out)
}

// Unmarshal restores the federation's runtime DDM state from
// wire-format bytes. M13 thread C: invoked by the savepoint Manager
// during RequestFederationRestore before the event-log replay runs.
//
// Empty/nil input is a no-op. The manager merges the bundled state
// onto the existing federation record (which carries the
// FOM-derived setup-time configuration). The production
// restore-bootstrap path constructs a fresh manager that has already
// observed CreateFederation, so the routing-space/dimension tables
// are populated and only the runtime state needs to land.
func (m *Manager) Unmarshal(fed core.FederationName, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	var pb rtiv1.DDMManagerState
	if err := proto.Unmarshal(data, &pb); err != nil {
		return fmt.Errorf("ddm: unmarshal DDMManagerState for %q: %w", fed, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.fed[fed]
	if !ok {
		st = newFederationDDMState()
		m.fed[fed] = st
	}
	st.nextRegionHandle = RegionHandle(pb.GetNextRegionHandle())
	st.regions = map[RegionHandle]*regionState{}
	for _, e := range pb.GetRegions() {
		if e == nil {
			continue
		}
		rs := &regionState{
			owner:     core.FederateHandle(e.GetOwnerFederate()),
			space:     RoutingSpaceHandle(e.GetRoutingSpaceHandle()),
			dims:      handlesToDimSlice(e.GetDimensionHandles()),
			committed: rangesToMap(e.GetCommitted()),
			pending:   rangesToMap(e.GetPending()),
		}
		st.regions[RegionHandle(e.GetRegionHandle())] = rs
	}
	st.objSubs = map[objAttrKey][]subscription{}
	for _, e := range pb.GetObjectSubscriptions() {
		if e == nil {
			continue
		}
		key := objAttrKey{
			cls:  core.ObjectClassHandle(e.GetObjectClassHandle()),
			attr: core.AttributeHandle(e.GetAttributeHandle()),
		}
		st.objSubs[key] = append(st.objSubs[key], subscription{
			subscriber: core.FederateHandle(e.GetSubscriberFederate()),
			regions:    handlesToRegionSlice(e.GetRegionHandles()),
		})
	}
	st.intSubs = map[core.InteractionClassHandle][]interactionSubscription{}
	for _, e := range pb.GetInteractionSubscriptions() {
		if e == nil {
			continue
		}
		cls := core.InteractionClassHandle(e.GetInteractionClassHandle())
		st.intSubs[cls] = append(st.intSubs[cls], interactionSubscription{
			subscriber: core.FederateHandle(e.GetSubscriberFederate()),
			regions:    handlesToRegionSlice(e.GetRegionHandles()),
		})
	}
	st.objPubs = map[core.ObjectHandle]map[core.AttributeHandle][]RegionHandle{}
	for _, e := range pb.GetObjectPublications() {
		if e == nil {
			continue
		}
		obj := core.ObjectHandle(e.GetObjectHandle())
		per, exists := st.objPubs[obj]
		if !exists {
			per = map[core.AttributeHandle][]RegionHandle{}
			st.objPubs[obj] = per
		}
		per[core.AttributeHandle(e.GetAttributeHandle())] = handlesToRegionSlice(e.GetRegionHandles())
	}
	return nil
}

// --- helpers ---------------------------------------------------------------

func ddmRegionsToProto(in map[RegionHandle]*regionState) []*rtiv1.DDMRegionEntry {
	keys := make([]RegionHandle, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]*rtiv1.DDMRegionEntry, 0, len(keys))
	for _, k := range keys {
		rs := in[k]
		out = append(out, &rtiv1.DDMRegionEntry{
			RegionHandle:       uint64(k),
			OwnerFederate:      uint64(rs.owner),
			RoutingSpaceHandle: uint64(rs.space),
			DimensionHandles:   dimsToUint64(rs.dims),
			Committed:          rangesToProto(rs.committed),
			Pending:            rangesToProto(rs.pending),
		})
	}
	return out
}

func rangesToProto(in map[DimensionHandle]Range) []*rtiv1.DDMRangeEntry {
	keys := make([]DimensionHandle, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]*rtiv1.DDMRangeEntry, 0, len(keys))
	for _, k := range keys {
		r := in[k]
		out = append(out, &rtiv1.DDMRangeEntry{
			DimensionHandle: uint64(k),
			Lower:           r.Lower,
			Upper:           r.Upper,
		})
	}
	return out
}

func rangesToMap(in []*rtiv1.DDMRangeEntry) map[DimensionHandle]Range {
	out := map[DimensionHandle]Range{}
	for _, e := range in {
		if e == nil {
			continue
		}
		out[DimensionHandle(e.GetDimensionHandle())] = Range{
			Lower: e.GetLower(),
			Upper: e.GetUpper(),
		}
	}
	return out
}

func ddmObjSubsToProto(in map[objAttrKey][]subscription) []*rtiv1.DDMSubscriptionEntry {
	out := make([]*rtiv1.DDMSubscriptionEntry, 0)
	keys := make([]objAttrKey, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b objAttrKey) int {
		if a.cls != b.cls {
			return cmpUint64(uint64(a.cls), uint64(b.cls))
		}
		return cmpUint64(uint64(a.attr), uint64(b.attr))
	})
	for _, k := range keys {
		subs := in[k]
		// Subscribers sorted ascending for determinism.
		sortedSubs := append([]subscription(nil), subs...)
		slices.SortFunc(sortedSubs, func(a, b subscription) int {
			return cmpUint64(uint64(a.subscriber), uint64(b.subscriber))
		})
		for _, s := range sortedSubs {
			out = append(out, &rtiv1.DDMSubscriptionEntry{
				ObjectClassHandle:  uint64(k.cls),
				AttributeHandle:    uint64(k.attr),
				SubscriberFederate: uint64(s.subscriber),
				RegionHandles:      regionsToUint64(s.regions),
			})
		}
	}
	return out
}

func ddmIntSubsToProto(in map[core.InteractionClassHandle][]interactionSubscription) []*rtiv1.DDMInteractionSubscriptionEntry {
	out := make([]*rtiv1.DDMInteractionSubscriptionEntry, 0)
	keys := make([]core.InteractionClassHandle, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b core.InteractionClassHandle) int {
		return cmpUint64(uint64(a), uint64(b))
	})
	for _, cls := range keys {
		subs := in[cls]
		sortedSubs := append([]interactionSubscription(nil), subs...)
		slices.SortFunc(sortedSubs, func(a, b interactionSubscription) int {
			return cmpUint64(uint64(a.subscriber), uint64(b.subscriber))
		})
		for _, s := range sortedSubs {
			out = append(out, &rtiv1.DDMInteractionSubscriptionEntry{
				InteractionClassHandle: uint64(cls),
				SubscriberFederate:     uint64(s.subscriber),
				RegionHandles:          regionsToUint64(s.regions),
			})
		}
	}
	return out
}

func ddmObjPubsToProto(in map[core.ObjectHandle]map[core.AttributeHandle][]RegionHandle) []*rtiv1.DDMObjectPublicationEntry {
	out := make([]*rtiv1.DDMObjectPublicationEntry, 0)
	objs := make([]core.ObjectHandle, 0, len(in))
	for k := range in {
		objs = append(objs, k)
	}
	slices.SortFunc(objs, func(a, b core.ObjectHandle) int {
		return cmpUint64(uint64(a), uint64(b))
	})
	for _, obj := range objs {
		per := in[obj]
		attrs := make([]core.AttributeHandle, 0, len(per))
		for k := range per {
			attrs = append(attrs, k)
		}
		slices.SortFunc(attrs, func(a, b core.AttributeHandle) int {
			return cmpUint64(uint64(a), uint64(b))
		})
		for _, attr := range attrs {
			out = append(out, &rtiv1.DDMObjectPublicationEntry{
				ObjectHandle:    uint64(obj),
				AttributeHandle: uint64(attr),
				RegionHandles:   regionsToUint64(per[attr]),
			})
		}
	}
	return out
}

func dimsToUint64(in []DimensionHandle) []uint64 {
	out := make([]uint64, len(in))
	for i, d := range in {
		out[i] = uint64(d)
	}
	return out
}

func regionsToUint64(in []RegionHandle) []uint64 {
	out := make([]uint64, len(in))
	for i, r := range in {
		out[i] = uint64(r)
	}
	return out
}

func handlesToDimSlice(in []uint64) []DimensionHandle {
	out := make([]DimensionHandle, len(in))
	for i, v := range in {
		out[i] = DimensionHandle(v)
	}
	return out
}

func handlesToRegionSlice(in []uint64) []RegionHandle {
	out := make([]RegionHandle, len(in))
	for i, v := range in {
		out[i] = RegionHandle(v)
	}
	return out
}

func cmpUint64(a, b uint64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
