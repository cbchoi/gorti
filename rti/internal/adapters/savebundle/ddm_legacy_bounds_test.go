package savebundle

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"google.golang.org/protobuf/proto"
)

// Legacy snapshots accepted equal bounds. Restoring them does not invoke
// Set Range Bounds; retain those bytes and require strict bounds on new calls.
func TestDDMLegacyEqualBoundsRestoreCompatibility(t *testing.T) {
	ctx := context.Background()
	const fed core.FederationName = "legacy-bounds"
	repo := composition.NewFOMRepository()
	model, err := repo.Load(ctx, []core.FOMModule{{Path: "bounds.xml", XML: []byte(`<objectModel><dimensions><dimension><name>X</name><upperBound>100</upperBound></dimension></dimensions></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor(fed, model)
	m, err := ddm.New(ddm.Options{FOMs: repo, Outbox: delivery.NewWithBatch(8, 1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	space, _ := m.LookupRoutingSpace(fed, "default")
	dim, _ := m.LookupDimension(fed, space, "X")
	legacy := &rtiv1.DDMManagerState{NextRegionHandle: 1, Regions: []*rtiv1.DDMRegionEntry{{
		RegionHandle: 1, OwnerFederate: 1, RoutingSpaceHandle: uint64(space), DimensionHandles: []uint64{uint64(dim)},
		Committed: []*rtiv1.DDMRangeEntry{{DimensionHandle: uint64(dim), Lower: 0, Upper: 0}},
		Pending:   []*rtiv1.DDMRangeEntry{{DimensionHandle: uint64(dim), Lower: 100, Upper: 100}},
	}}}
	data, err := proto.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	v := graphValidation{model: model, names: model.(core.FOMHandleNameLookup), members: map[core.FederateHandle]string{1: "owner"}, regions: map[uint64]*rtiv1.DDMRegionEntry{}}
	if err := v.validateDDM(data); err != nil {
		t.Fatalf("legacy equal bounds rejected by restore graph: %v", err)
	}
	adapter := DDM{Manager: m}
	before := m.ExportState(fed)
	commit, err := adapter.PrepareRestore(fed, data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.ExportState(fed)) {
		t.Fatal("prepare changed live state")
	}
	commit()
	roundtrip, err := adapter.Marshal(fed)
	if err != nil {
		t.Fatal(err)
	}
	var restored rtiv1.DDMManagerState
	if err := proto.Unmarshal(roundtrip, &restored); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(legacy, &restored) {
		t.Fatal("legacy pending or committed bounds changed on round trip")
	}
	before = m.ExportState(fed)
	if err := m.SetRangeBounds(fed, 1, 1, dim, core.DDMRange{Lower: 50, Upper: 50}); !errors.Is(err, core.ErrInvalidRangeBounds) {
		t.Errorf("new equal bounds accepted after restore: %v", err)
	}
	if !reflect.DeepEqual(before, m.ExportState(fed)) {
		t.Error("rejected call replaced restored bounds")
	}
	if err := m.CommitRegionModifications(ctx, fed, 1, []ddm.RegionHandle{1}); err != nil {
		t.Fatal(err)
	}
	if got, ok := m.QueryBounds(fed, 1, dim); !ok || got != (core.DDMRange{Lower: 100, Upper: 100}) {
		t.Fatalf("legacy pending range was not preserved: %+v, %t", got, ok)
	}
	if err := m.SetRangeBounds(fed, 1, 1, dim, core.DDMRange{Lower: 99, Upper: 100}); err != nil {
		t.Fatal(err)
	}
	if err := m.CommitRegionModifications(ctx, fed, 1, []ddm.RegionHandle{1}); err != nil {
		t.Fatal(err)
	}
	if got, ok := m.QueryBounds(fed, 1, dim); !ok || got != (core.DDMRange{Lower: 99, Upper: 100}) {
		t.Fatalf("valid correction did not commit: %+v, %t", got, ok)
	}
}
