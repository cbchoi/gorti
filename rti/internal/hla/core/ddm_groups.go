package core

import "context"

// AttributeRegionGroup preserves both sets, including handles in empty products.
type AttributeRegionGroup struct {
	Attributes []AttributeHandle
	Regions    []DDMRegionHandleCore
}

// GroupedObjectRegionRegistrar preserves handles in empty Cartesian products.
// It shares ObjectRegionRegistrar's object-before-DDM lease ordering.
type GroupedObjectRegionRegistrar interface {
	PrepareObjectRegistrationGroups(context.Context, FederationName, FederateHandle, ObjectClassHandle, []AttributeRegionGroup) (ObjectRegionRegistration, error)
}

// GroupedRegionRouting validates the entire request and pins one DDM snapshot.
type GroupedRegionRouting interface {
	PrepareRegionUpdateGroups(context.Context, FederationName, FederateHandle, ObjectClassHandle, []AttributeRegionGroup) (GroupedRegionUpdateRequest, error)
}

type GroupedRegionUpdateRequest interface {
	// The request's attribute and the instance's attribute can differ for subclasses.
	Matches(ObjectHandle, AttributeHandle, AttributeHandle) bool
	Release()
}
