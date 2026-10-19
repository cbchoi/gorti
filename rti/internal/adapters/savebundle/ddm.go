package savebundle

import (
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"google.golang.org/protobuf/proto"
)

type DDM struct{ Manager *ddm.Manager }

func (s DDM) Marshal(fed core.FederationName) ([]byte, error) {
	state := s.Manager.ExportState(fed)
	if state == nil {
		return nil, nil
	}
	if err := validateDDMCopiedState(state); err != nil {
		return nil, fmt.Errorf("ddm snapshot for %q: %w", fed, err)
	}
	wire := &rtiv1.DDMManagerState{NextRegionHandle: uint64(state.NextRegionHandle)}
	for _, entry := range state.Regions {
		region := &rtiv1.DDMRegionEntry{RegionHandle: uint64(entry.Handle), OwnerFederate: uint64(entry.Owner), RoutingSpaceHandle: uint64(entry.RoutingSpace), Template: entry.Template}
		for _, v := range entry.Dimensions {
			region.DimensionHandles = append(region.DimensionHandles, uint64(v))
		}
		region.Committed = encodeRanges(entry.Committed)
		region.Pending = encodeRanges(entry.Pending)
		wire.Regions = append(wire.Regions, region)
	}
	for _, entry := range state.ObjectSubscriptions {
		wire.ObjectSubscriptions = append(wire.ObjectSubscriptions, &rtiv1.DDMSubscriptionEntry{ObjectClassHandle: uint64(entry.ObjectClass), AttributeHandle: uint64(entry.Attribute), SubscriberFederate: uint64(entry.Subscriber), RegionHandles: encodeRegionHandles(entry.Regions), Passive: entry.Passive, UpdateRateDesignator: entry.UpdateRateDesignator})
	}
	for _, entry := range state.InteractionSubscriptions {
		wire.InteractionSubscriptions = append(wire.InteractionSubscriptions, &rtiv1.DDMInteractionSubscriptionEntry{InteractionClassHandle: uint64(entry.InteractionClass), SubscriberFederate: uint64(entry.Subscriber), RegionHandles: encodeRegionHandles(entry.Regions), Passive: entry.Passive})
	}
	for _, entry := range state.ObjectPublications {
		wire.ObjectPublications = append(wire.ObjectPublications, &rtiv1.DDMObjectPublicationEntry{ObjectHandle: uint64(entry.Object), AttributeHandle: uint64(entry.Attribute), RegionHandles: encodeRegionHandles(entry.Regions)})
	}
	for _, entry := range state.InactiveAssociations {
		wire.InactiveAssociations = append(wire.InactiveAssociations, &rtiv1.DDMInactiveAssociationEntry{ObjectHandle: uint64(entry.Object), AttributeHandle: uint64(entry.Attribute), AssociatingFederate: uint64(entry.Federate), RegionHandles: encodeRegionHandles(entry.Regions)})
	}
	wire.ConveyedRegions = encodeDDMCopiedState(state.ConveyedRegions)
	return proto.Marshal(wire)
}

func (s DDM) Unmarshal(fed core.FederationName, data []byte) error {
	// Preserve the legacy direct-call contract; the manifest-aware path uses
	// PrepareRestore, where present empty bytes mean an empty saved state.
	if len(data) == 0 {
		return nil
	}
	commit, err := s.PrepareRestore(fed, data)
	if err != nil {
		return err
	}
	if commit != nil {
		commit()
	}
	return nil
}

// PrepareRestore validates the complete snapshot before any live mutation.
func (s DDM) PrepareRestore(fed core.FederationName, data []byte) (func(), error) {
	if s.Manager == nil {
		return nil, fmt.Errorf("ddm snapshot for %q: manager is nil", fed)
	}
	state, err := decodeDDMState(fed, data)
	if err != nil {
		return nil, err
	}
	return func() { s.Manager.ImportState(fed, state) }, nil
}

// Candidate FOM validation does not consult or modify the live federation.
func (s DDM) PrepareRestoreWithFOM(fed core.FederationName, data []byte, model core.FOMHandle) (func(), error) {
	if s.Manager == nil {
		return nil, fmt.Errorf("ddm snapshot for %q: manager is nil", fed)
	}
	state, err := decodeDDMState(fed, data)
	if err != nil {
		return nil, err
	}
	if err := validateDDMCopiedFOM(state, model); err != nil {
		return nil, fmt.Errorf("ddm snapshot for %q: %w", fed, err)
	}
	return func() { s.Manager.ImportState(fed, state) }, nil
}

func decodeDDMState(fed core.FederationName, data []byte) (*ddm.ManagerState, error) {
	var wire rtiv1.DDMManagerState
	if err := proto.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("ddm snapshot for %q: %w", fed, err)
	}
	if err := validateDDMTemplates(&wire); err != nil {
		return nil, fmt.Errorf("ddm snapshot for %q: %w", fed, err)
	}
	state := &ddm.ManagerState{NextRegionHandle: ddm.RegionHandle(wire.GetNextRegionHandle())}
	decodeDDMRecords(&wire, state)
	for _, entry := range wire.GetInactiveAssociations() {
		if entry == nil || entry.ObjectHandle == 0 || entry.AttributeHandle == 0 || entry.AssociatingFederate == 0 || len(entry.RegionHandles) == 0 {
			return nil, fmt.Errorf("ddm snapshot for %q: invalid inactive association", fed)
		}
		state.InactiveAssociations = append(state.InactiveAssociations, ddm.InactiveAssociationState{Object: core.ObjectHandle(entry.ObjectHandle), Attribute: core.AttributeHandle(entry.AttributeHandle), Federate: core.FederateHandle(entry.AssociatingFederate), Regions: decodeRegionHandles(entry.RegionHandles)})
	}
	var err error
	state.ConveyedRegions, err = decodeDDMCopiedState(wire.GetConveyedRegions())
	if err != nil {
		return nil, fmt.Errorf("ddm snapshot for %q: %w", fed, err)
	}
	if err := validateDDMCopiedState(state); err != nil {
		return nil, fmt.Errorf("ddm snapshot for %q: %w", fed, err)
	}
	return state, nil
}

func encodeRanges(in []ddm.RangeState) []*rtiv1.DDMRangeEntry {
	out := make([]*rtiv1.DDMRangeEntry, 0, len(in))
	for _, e := range in {
		out = append(out, &rtiv1.DDMRangeEntry{DimensionHandle: uint64(e.Dimension), Lower: e.Lower, Upper: e.Upper})
	}
	return out
}
func decodeRanges(in []*rtiv1.DDMRangeEntry) []ddm.RangeState {
	out := make([]ddm.RangeState, 0, len(in))
	for _, e := range in {
		if e != nil {
			out = append(out, ddm.RangeState{Dimension: ddm.DimensionHandle(e.GetDimensionHandle()), Lower: e.GetLower(), Upper: e.GetUpper()})
		}
	}
	return out
}
func encodeRegionHandles(in []ddm.RegionHandle) []uint64 {
	out := make([]uint64, len(in))
	for i, v := range in {
		out[i] = uint64(v)
	}
	return out
}
func decodeRegionHandles(in []uint64) []ddm.RegionHandle {
	out := make([]ddm.RegionHandle, len(in))
	for i, v := range in {
		out[i] = ddm.RegionHandle(v)
	}
	return out
}

func decodeDDMRecords(wire *rtiv1.DDMManagerState, state *ddm.ManagerState) {
	for _, entry := range wire.GetRegions() {
		if entry == nil {
			continue
		}
		region := ddm.RegionState{Handle: ddm.RegionHandle(entry.GetRegionHandle()), Owner: core.FederateHandle(entry.GetOwnerFederate()), RoutingSpace: ddm.RoutingSpaceHandle(entry.GetRoutingSpaceHandle()), Template: entry.GetTemplate(), Committed: decodeRanges(entry.GetCommitted()), Pending: decodeRanges(entry.GetPending())}
		for _, v := range entry.GetDimensionHandles() {
			region.Dimensions = append(region.Dimensions, ddm.DimensionHandle(v))
		}
		state.Regions = append(state.Regions, region)
	}
	for _, entry := range wire.GetObjectSubscriptions() {
		if entry != nil {
			state.ObjectSubscriptions = append(state.ObjectSubscriptions, ddm.ObjectSubscriptionState{ObjectClass: core.ObjectClassHandle(entry.GetObjectClassHandle()), Attribute: core.AttributeHandle(entry.GetAttributeHandle()), Subscriber: core.FederateHandle(entry.GetSubscriberFederate()), Regions: decodeRegionHandles(entry.GetRegionHandles()), Passive: entry.GetPassive(), UpdateRateDesignator: entry.GetUpdateRateDesignator()})
		}
	}
	for _, entry := range wire.GetInteractionSubscriptions() {
		if entry != nil {
			state.InteractionSubscriptions = append(state.InteractionSubscriptions, ddm.InteractionSubscriptionState{InteractionClass: core.InteractionClassHandle(entry.GetInteractionClassHandle()), Subscriber: core.FederateHandle(entry.GetSubscriberFederate()), Regions: decodeRegionHandles(entry.GetRegionHandles()), Passive: entry.GetPassive()})
		}
	}
	for _, entry := range wire.GetObjectPublications() {
		if entry != nil {
			state.ObjectPublications = append(state.ObjectPublications, ddm.ObjectPublicationState{Object: core.ObjectHandle(entry.GetObjectHandle()), Attribute: core.AttributeHandle(entry.GetAttributeHandle()), Regions: decodeRegionHandles(entry.GetRegionHandles())})
		}
	}
}
