package savebundle

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

func TestDDMConveyedSnapshotPrecisionAndLeases(t *testing.T) {
	ctx := context.Background()
	source := templateCodec(t)
	snapshot, err := core.NewRegionRealizationSnapshot(90, []core.RegionDimensionRange{{Dimension: 1, Bounds: core.DDMRange{Lower: 1<<53 + 1, Upper: ^uint64(0)}}})
	if err != nil {
		t.Fatal(err)
	}
	copies := []core.ConveyedRegionCopy{{Designator: 91, Realization: snapshot}}
	if err := source.Manager.ConveyCopies(ctx, "fed", 2, copies); err != nil {
		t.Fatal(err)
	}
	if err := source.Manager.EnterConveyedRegions(ctx, "fed", 2, []core.DDMRegionHandleCore{91}); err != nil {
		t.Fatal(err)
	}
	if err := source.Manager.ConveyCopies(ctx, "fed", 3, copies); err != nil {
		t.Fatal(err)
	}
	data, err := source.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	dest := templateCodec(t)
	if err := dest.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(source.Manager.ExportState("fed"), dest.Manager.ExportState("fed")) {
		t.Fatal("codec lost copied realization or receiver lease")
	}
	if got, err := dest.Manager.QueryBoundsFor("fed", 2, 91, 1); err != nil || got.Lower != 1<<53+1 || got.Upper != ^uint64(0) {
		t.Fatalf("exact restored query: %v %v", got, err)
	}
	if _, err := dest.Manager.QueryBoundsFor("fed", 3, 91, 1); !errors.Is(err, core.ErrRegionNotFound) {
		t.Fatalf("pending lease became queryable: %v", err)
	}
	if err := dest.Manager.ReleaseConveyedRegions(ctx, "fed", 2, []core.DDMRegionHandleCore{91}); err != nil {
		t.Fatal(err)
	}
	if err := dest.Manager.EnterConveyedRegions(ctx, "fed", 3, []core.DDMRegionHandleCore{91}); err != nil {
		t.Fatal(err)
	}
	if _, err := dest.Manager.QueryBoundsFor("fed", 3, 91, 1); err != nil {
		t.Fatal(err)
	}
}

func testDDMConveyedTypedGraph[T core.LogicalTimeScalar](t *testing.T, stamp T) {
	t.Helper()
	source := templateCodec(t)
	state := copiedStateFixture(t)
	source.Manager.ImportState("fed", state)
	data, err := source.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	fom := composition.NewModelHandle(model.NewFOMWithDimensions(nil, nil, nil, []model.Dimension{{Name: "X", UpperBound: ^uint64(0)}, {Name: "Y", UpperBound: 100}}))
	family := "HLAfloat64Time"
	objectVersion := uint32(4)
	if integerScalar[T]() {
		family = "HLAinteger64Time"
		objectVersion = 5
	}
	generation := uint64(0)
	history, err := writeRetractionsOf(&timepkg.RetractionHistoryOf[T]{Complete: true})
	if err != nil {
		t.Fatal(err)
	}
	manifest := core.CheckpointManifestOf[T]{Federation: "fed", FederationGeneration: &generation, LogicalTimeImplementationName: family, SaveTime: &stamp, Federates: []core.FederateHandle{2, 3}, FederateNames: []string{"active", "pending"}, ManagerSnapshots: map[string][]byte{
		"object":    graphJSON(t, objectSnapshotV1Of[T]{Version: objectVersion, LogicalTimeImplementationName: family, State: &object.RegistryStateOf[T]{}}),
		"ownership": nil, "declaration": graphJSON(t, declarationSnapshot{Version: 3}),
		"ddm": data, "time": nil, "sync": nil, "mom": nil, "retractions": history,
	}}
	graph := RestoreGraphOf[T]{}
	if err := graph.Validate("fed", manifest, fom); err != nil {
		t.Fatal(err)
	}
	const active core.FederateHandle = 9<<32 | 2
	const pending core.FederateHandle = 9<<32 | 3
	rebound, err := graph.Rebind(manifest, 9, map[core.FederateHandle]core.FederateHandle{2: active, 3: pending})
	if err != nil {
		t.Fatal(err)
	}
	if *rebound.SaveTime != stamp {
		t.Fatal("typed save boundary rounded")
	}
	if err := graph.Validate("fed", rebound, fom); err != nil {
		t.Fatal(err)
	}
	dest := templateCodec(t)
	commit, err := dest.PrepareRestoreWithFOM("fed", rebound.ManagerSnapshots["ddm"], fom)
	if err != nil {
		t.Fatal(err)
	}
	if dest.Manager.ExportState("fed") != nil {
		t.Fatal("prepare published copied query state")
	}
	commit()
	got := dest.Manager.ExportState("fed")
	if len(got.ConveyedRegions) != 2 || got.ConveyedRegions[0].Designator != 91 || got.ConveyedRegions[0].Realization.SourceRegion() != 90 || got.NextRegionHandle != 92 {
		t.Fatalf("copied state lost or source/designator rebound: %+v", got)
	}
	if bounds, err := dest.Manager.QueryBoundsFor("fed", active, 91, 1); err != nil || bounds.Lower != 1<<53+1 || bounds.Upper != ^uint64(0) {
		t.Fatalf("exact copied query after generation rebind: %+v %v", bounds, err)
	}
	if _, err := dest.Manager.QueryBoundsFor("fed", pending, 91, 1); !errors.Is(err, core.ErrRegionNotFound) {
		t.Fatalf("pending receiver became active: %v", err)
	}
	if err := dest.Manager.ReleaseConveyedRegions(context.Background(), "fed", active, []core.DDMRegionHandleCore{91}); err != nil {
		t.Fatal(err)
	}
	if err := dest.Manager.EnterConveyedRegions(context.Background(), "fed", pending, []core.DDMRegionHandleCore{91, 92}); err != nil {
		t.Fatal(err)
	}
	if _, err := dest.Manager.QueryBoundsFor("fed", pending, 91, 1); err != nil {
		t.Fatal(err)
	}
}

func TestDDMConveyedTypedGraphBothFamilies(t *testing.T) {
	t.Run("Float64", func(t *testing.T) { testDDMConveyedTypedGraph[core.LogicalTime](t, 3.5) })
	t.Run("Integer64", func(t *testing.T) { testDDMConveyedTypedGraph[int64](t, 1<<53+1) })
}
