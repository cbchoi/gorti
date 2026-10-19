package savebundle

import (
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

// Template drafts keep full initial ranges and use pending keys as SetBounds
// coverage. Legacy snapshots without the template field remain specifications.
func validateDDMTemplates(state *rtiv1.DDMManagerState) error {
	templates := map[uint64]bool{}
	for _, region := range state.GetRegions() {
		if !region.GetTemplate() {
			continue
		}
		if region.GetRegionHandle() == 0 || region.GetOwnerFederate() == 0 || region.GetRoutingSpaceHandle() != 1 || templates[region.GetRegionHandle()] {
			return fmt.Errorf("invalid region template identity")
		}
		templates[region.GetRegionHandle()] = true
		if err := validateDDMTemplateRanges(region); err != nil {
			return err
		}
	}
	return validateDDMTemplateReferences(state, templates)
}

func validateDDMTemplateRanges(region *rtiv1.DDMRegionEntry) error {
	dimensions := map[uint64]bool{}
	for _, dim := range region.GetDimensionHandles() {
		if dim == 0 || dimensions[dim] {
			return fmt.Errorf("invalid template dimension")
		}
		dimensions[dim] = true
	}
	limits := map[uint64]uint64{}
	for _, bounds := range region.GetCommitted() {
		if bounds == nil || !dimensions[bounds.DimensionHandle] || limits[bounds.DimensionHandle] != 0 || bounds.Lower != 0 || bounds.Upper == 0 {
			return fmt.Errorf("invalid template initial range")
		}
		limits[bounds.DimensionHandle] = bounds.Upper
	}
	if len(limits) != len(dimensions) {
		return fmt.Errorf("template initial ranges do not cover dimensions")
	}
	return validateDDMTemplatePending(region, limits)
}

func validateDDMTemplatePending(region *rtiv1.DDMRegionEntry, limits map[uint64]uint64) error {
	seen := map[uint64]bool{}
	for _, bounds := range region.GetPending() {
		if bounds == nil || limits[bounds.DimensionHandle] == 0 || seen[bounds.DimensionHandle] || bounds.Lower >= bounds.Upper || bounds.Upper > limits[bounds.DimensionHandle] {
			return fmt.Errorf("invalid template pending range")
		}
		seen[bounds.DimensionHandle] = true
	}
	return nil
}

func validateDDMTemplateReferences(state *rtiv1.DDMManagerState, templates map[uint64]bool) error {
	check := func(handles []uint64) error {
		for _, handle := range handles {
			if templates[handle] {
				return fmt.Errorf("region binding references a template")
			}
		}
		return nil
	}
	for _, subscription := range state.GetObjectSubscriptions() {
		if err := check(subscription.GetRegionHandles()); err != nil {
			return err
		}
	}
	for _, subscription := range state.GetInteractionSubscriptions() {
		if err := check(subscription.GetRegionHandles()); err != nil {
			return err
		}
	}
	for _, publication := range state.GetObjectPublications() {
		if err := check(publication.GetRegionHandles()); err != nil {
			return err
		}
	}
	for _, association := range state.GetInactiveAssociations() {
		if err := check(association.GetRegionHandles()); err != nil {
			return err
		}
	}
	return nil
}
