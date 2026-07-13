package federate

import (
	"context"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// RegisterFederationSynchronizationPoint registers an HLA synchronization
// point. An empty requiredFederates slice targets all currently joined
// federates.
func (f *Federate) RegisterFederationSynchronizationPoint(
	ctx context.Context, label string, tag []byte, requiredFederates []uint64,
) error {
	_, err := f.conn.sync.RegisterFederationSynchronizationPoint(ctx, &rtiv1.RegisterSyncPointRequest{
		WireVersion:       rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:    f.federationName,
		FederateHandle:    f.federateHandle,
		Label:             label,
		Tag:               append([]byte(nil), tag...),
		RequiredFederates: append([]uint64(nil), requiredFederates...),
	})
	return wrapStatusErr(err)
}

// SynchronizationPointAchieved marks an announced HLA synchronization point
// as achieved by this federate.
func (f *Federate) SynchronizationPointAchieved(
	ctx context.Context, label string, successfully bool,
) error {
	value := successfully
	_, err := f.conn.sync.SynchronizationPointAchieved(ctx, &rtiv1.AchieveSyncPointRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
		Label:          label,
		Successfully:   &value,
	})
	return wrapStatusErr(err)
}
