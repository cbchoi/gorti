package savebundle

import (
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
)

func encodeDDMCopiedState(state []ddm.ConveyedRegionState) []*rtiv1.DDMConveyedRegionEntry {
	var result []*rtiv1.DDMConveyedRegionEntry
	for _, copiedRegion := range state {
		entry := &rtiv1.DDMConveyedRegionEntry{Designator: uint64(copiedRegion.Designator), SourceRegionHandle: uint64(copiedRegion.Realization.SourceRegion())}
		for _, dimension := range copiedRegion.Realization.DimensionRanges() {
			entry.Realization = append(entry.Realization, &rtiv1.DDMRangeEntry{DimensionHandle: uint64(dimension.Dimension), Lower: dimension.Bounds.Lower, Upper: dimension.Bounds.Upper})
		}
		for _, receiver := range copiedRegion.Recipients {
			entry.Recipients = append(entry.Recipients, &rtiv1.DDMConveyedRegionRecipientEntry{FederateHandle: uint64(receiver.Federate), Active: receiver.Active})
		}
		result = append(result, entry)
	}
	return result
}

func decodeDDMCopiedState(entries []*rtiv1.DDMConveyedRegionEntry) ([]ddm.ConveyedRegionState, error) {
	var result []ddm.ConveyedRegionState
	for _, entry := range entries {
		if entry == nil {
			return nil, fmt.Errorf("nil copied region entry")
		}
		var dimensions []core.RegionDimensionRange
		for _, dimension := range entry.Realization {
			if dimension == nil {
				return nil, fmt.Errorf("nil copied region dimension range")
			}
			dimensions = append(dimensions, core.RegionDimensionRange{Dimension: core.DDMDimensionHandle(dimension.DimensionHandle), Bounds: core.DDMRange{Lower: dimension.Lower, Upper: dimension.Upper}})
		}
		realization, err := core.NewRegionRealizationSnapshot(core.DDMRegionHandleCore(entry.SourceRegionHandle), dimensions)
		if err != nil {
			return nil, err
		}
		copiedRegion := ddm.ConveyedRegionState{Designator: ddm.RegionHandle(entry.Designator), Realization: realization}
		for _, receiver := range entry.Recipients {
			if receiver == nil {
				return nil, fmt.Errorf("nil copied region receiver lease")
			}
			copiedRegion.Recipients = append(copiedRegion.Recipients, ddm.ConveyedRegionRecipientState{Federate: core.FederateHandle(receiver.FederateHandle), Active: receiver.Active})
		}
		result = append(result, copiedRegion)
	}
	return result, nil
}

// Copied designators share the normal region namespace; their historical source
// is provenance and need not still exist in the saved federation.
func validateDDMCopiedState(state *ddm.ManagerState) error {
	occupied := make(map[ddm.RegionHandle]bool, len(state.Regions)+len(state.ConveyedRegions))
	for _, region := range state.Regions {
		occupied[region.Handle] = true
	}
	for _, copiedRegion := range state.ConveyedRegions {
		if copiedRegion.Designator == 0 || copiedRegion.Designator > state.NextRegionHandle || occupied[copiedRegion.Designator] || copiedRegion.Designator == copiedRegion.Realization.SourceRegion() || copiedRegion.Realization.SourceRegion() == 0 {
			return fmt.Errorf("invalid copied region identity or namespace")
		}
		occupied[copiedRegion.Designator] = true
		if len(copiedRegion.Recipients) == 0 {
			return fmt.Errorf("copied region has no receiver lease")
		}
		receivers := make(map[core.FederateHandle]bool, len(copiedRegion.Recipients))
		for _, receiver := range copiedRegion.Recipients {
			if receiver.Federate == 0 || receiver.Federate == ^core.FederateHandle(0) || receivers[receiver.Federate] {
				return fmt.Errorf("invalid or duplicate copied region receiver")
			}
			receivers[receiver.Federate] = true
		}
	}
	return nil
}

func validateDDMCopiedFOM(state *ddm.ManagerState, model core.FOMHandle) error {
	names, ok := model.(core.FOMHandleNameLookup)
	if model == nil || !model.IsValid() || !ok {
		return fmt.Errorf("copied region restore requires a valid candidate FOM")
	}
	for _, copiedRegion := range state.ConveyedRegions {
		for _, dimension := range copiedRegion.Realization.DimensionRanges() {
			upper, found := names.DimensionUpperBound(core.DimensionHandle(dimension.Dimension))
			if !found || dimension.Bounds.Upper > upper {
				return fmt.Errorf("copied region realization exceeds candidate dimension")
			}
		}
	}
	return nil
}
