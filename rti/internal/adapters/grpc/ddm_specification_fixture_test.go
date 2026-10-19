package grpc

import (
	"context"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"testing"
)

func initializeDDMSpecification(t *testing.T, m *ddm.Manager, fed core.FederationName, owner core.FederateHandle, handle ddm.RegionHandle) {
	t.Helper()
	for _, region := range m.ExportState(fed).Regions {
		if region.Handle != handle {
			continue
		}
		for _, bounds := range region.Committed {
			if err := m.SetRangeBounds(fed, owner, handle, bounds.Dimension, ddm.Range{Lower: bounds.Lower, Upper: bounds.Upper}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := m.CommitRegionModifications(context.Background(), fed, owner, []ddm.RegionHandle{handle}); err != nil {
		t.Fatal(err)
	}
}
