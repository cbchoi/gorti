package grpc

import (
	"context"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type rejectingDDMPairs struct {
	core.DataDistributionManagement
	calls int
}

func (m *rejectingDDMPairs) AssociateRegionsForUpdates(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, map[core.AttributeHandle][]core.DDMRegionHandleCore) error {
	m.calls++
	return nil
}
func (m *rejectingDDMPairs) UnassociateRegionsForUpdates(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, map[core.AttributeHandle][]core.DDMRegionHandleCore) error {
	m.calls++
	return nil
}

func TestDDMMalformedPairsDoNotBecomeClearAll(t *testing.T) {
	for _, pair := range []*rtiv1.AttributeRegions{nil, {}, {AttributeHandle: 1, RegionHandles: []uint64{0}}} {
		mgr := &rejectingDDMPairs{}
		svc := newDDMService(mgr, nil)
		_, associateErr := svc.AssociateRegionsForUpdates(context.Background(), &rtiv1.AssociateRegionsForUpdatesRequest{WireVersion: 1, AttributeRegions: []*rtiv1.AttributeRegions{pair}})
		_, unassociateErr := svc.UnassociateRegionsForUpdates(context.Background(), &rtiv1.UnassociateRegionsForUpdatesRequest{WireVersion: 1, AttributeRegions: []*rtiv1.AttributeRegions{pair}})
		if associateErr == nil || unassociateErr == nil || mgr.calls != 0 {
			t.Errorf("malformed pair reached mutation: pair=%v associate=%v unassociate=%v calls=%d", pair, associateErr, unassociateErr, mgr.calls)
		}
	}
}
