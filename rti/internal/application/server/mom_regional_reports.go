package server

import (
	"fmt"
	"math"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

func momRegionalReportRecipients(state *ddm.ManagerState, fom core.FOMHandle, classes []core.InteractionClassHandle, subject core.FederateHandle, service string) ([]core.FederateHandle, error) {
	if state == nil {
		return nil, nil
	}
	names, ok := fom.(core.FOMHandleNameLookup)
	if !ok {
		return nil, fmt.Errorf("MOM: report dimension names unavailable")
	}
	dimensions, ok := fom.(core.FOMInteractionDimensions)
	if !ok {
		return nil, fmt.Errorf("MOM: report dimensions unavailable")
	}
	regions := make(map[ddm.RegionHandle]ddm.RegionState, len(state.Regions))
	for _, region := range state.Regions {
		regions[region.Handle] = region
	}
	var recipients []core.FederateHandle
	for _, sub := range state.InteractionSubscriptions {
		if !slices.Contains(classes, sub.InteractionClass) {
			continue
		}
		coordinates, err := momReportCoordinates(names, dimensions, classes[0], subject, service)
		if err != nil {
			return nil, err
		}
		for _, handle := range sub.Regions {
			region, found := regions[handle]
			if !found || region.Owner != sub.Subscriber {
				return nil, fmt.Errorf("MOM: invalid report subscription region %d", handle)
			}
			matches, err := momReportRegionMatches(region, coordinates)
			if err != nil {
				return nil, err
			}
			if matches {
				recipients = append(recipients, sub.Subscriber)
				break
			}
		}
	}
	return recipients, nil
}

func momReportCoordinates(names core.FOMHandleNameLookup, dimensions core.FOMInteractionDimensions, class core.InteractionClassHandle, subject core.FederateHandle, service string) (map[ddm.DimensionHandle]uint64, error) {
	dims, found := dimensions.InteractionDimensions(class)
	if !found {
		return nil, fmt.Errorf("MOM: report dimensions undefined")
	}
	coordinates := map[ddm.DimensionHandle]uint64{}
	for _, dim := range dims {
		name, found := names.DimensionName(core.DimensionHandle(dim))
		if !found {
			return nil, fmt.Errorf("MOM: report dimension %d unnamed", dim)
		}
		var value uint64
		switch name {
		case "HLAfederate":
			// Match the low-32-bit coordinate used by MOM attribute points.
			value = uint64(subject) & math.MaxUint32
		case "HLAserviceGroup":
			var known bool
			value, known = mom.ServiceGroup(service)
			if !known {
				return nil, fmt.Errorf("MOM: service group unavailable for %q", service)
			}
		default:
			return nil, fmt.Errorf("MOM: report dimension %q has no runtime coordinate", name)
		}
		coordinates[ddm.DimensionHandle(dim)] = value
	}
	return coordinates, nil
}

func momReportRegionMatches(region ddm.RegionState, coordinates map[ddm.DimensionHandle]uint64) (bool, error) {
	matches := true
	for _, dim := range region.Dimensions {
		value, shared := coordinates[dim]
		if !shared {
			continue
		}
		found := false
		for _, bounds := range region.Committed {
			if bounds.Dimension != dim {
				continue
			}
			found = true
			// Match the runtime DDM half-open range predicate, not pending bounds.
			if value < bounds.Lower || value >= bounds.Upper {
				matches = false
			}
			break
		}
		if !found {
			return false, fmt.Errorf("MOM: committed region dimension %d unavailable", dim)
		}
	}
	return matches, nil
}
