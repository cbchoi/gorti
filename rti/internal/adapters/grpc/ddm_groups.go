package grpc

import (
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func attributeRegionGroups(in *rtiv1.AttributeRegionGroups, legacyPresent bool) ([]core.AttributeRegionGroup, error) {
	if legacyPresent {
		return nil, core.ErrWireMalformedMessage
	}
	groups := make([]core.AttributeRegionGroup, 0, len(in.GetGroups()))
	for _, group := range in.GetGroups() {
		if group == nil {
			return nil, core.ErrWireMalformedMessage
		}
		converted := core.AttributeRegionGroup{}
		// Preserve invalid handles for the domain's service-specific exceptions.
		for _, attr := range group.GetAttributeHandles() {
			converted.Attributes = append(converted.Attributes, core.AttributeHandle(attr))
		}
		for _, region := range group.GetRegionHandles() {
			converted.Regions = append(converted.Regions, core.DDMRegionHandleCore(region))
		}
		groups = append(groups, converted)
	}
	return groups, nil
}
