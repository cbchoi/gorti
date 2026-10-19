package core

import "context"

// OwnershipReleaseDenier is an additive ownership service port. Denial leaves
// ownership unchanged and terminates the matching pending acquisitions.
type OwnershipReleaseDenier interface {
	ReleaseDenied(context.Context, FederationName, FederateHandle, ObjectHandle, []AttributeHandle) error
}
