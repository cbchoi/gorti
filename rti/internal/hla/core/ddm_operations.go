package core

import "context"

// RegionAssociationValidator pins object identity while DDM validates and
// changes its associations. DDM must invoke this before acquiring its own lock
// and release it after releasing that lock. No callbacks run inside the lease.
type RegionAssociationValidator interface {
	PrepareRegionAssociation(context.Context, FederationName, FederateHandle, ObjectHandle, []AttributeHandle) (ObjectClassHandle, func(), error)
}

type FOMInteractionDimensions interface {
	InteractionDimensions(InteractionClassHandle) ([]DDMDimensionHandle, bool)
}

// RegionRouting is an additive capability on ObjectRegionRegistrar. Send
// selection is a detached snapshot; update selection pins DDM until Release.
type RegionRouting interface {
	InteractionSubscribersWithRegions(context.Context, FederationName, FederateHandle, InteractionClassHandle, []DDMRegionHandleCore) ([]FederateHandle, error)
	PrepareRegionUpdateRequest(context.Context, FederationName, FederateHandle, ObjectClassHandle, []AttributeHandle, []DDMRegionHandleCore) (RegionUpdateRequest, error)
}

// RegionUpdateRequest is used while holding the object-state lock. Release it
// before querying ownership or delivering callbacks; this preserves object ->
// DDM and object -> ownership ordering, without DDM -> ownership calls.
type RegionUpdateRequest interface {
	Matches(ObjectHandle, AttributeHandle) bool
	Release()
}
