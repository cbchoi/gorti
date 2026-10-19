package object

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// RegisterWithRegionGroups retains empty products for validation in the same
// registration transaction that assigns the object ID while retaining the name reservation.
func (r *RegistryOf[T]) RegisterWithRegionGroups(ctx context.Context, fed core.FederationName, producer core.FederateHandle, cls core.ObjectClassHandle, name string, groups []core.AttributeRegionGroup) (core.ObjectHandle, string, error) {
	bindings := make(map[core.AttributeHandle][]core.DDMRegionHandleCore)
	for _, group := range groups {
		for _, attr := range group.Attributes {
			bindings[attr] = append(bindings[attr], group.Regions...)
		}
	}
	return r.register(ctx, fed, producer, cls, name, bindings, true, groups, true)
}
