package savebundle

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
)

func TestDDMRestoreRefreshesReusedFOMRevision(t *testing.T) {
	ctx := context.Background()
	const fed core.FederationName = "dimension-restore"
	repo := composition.NewFOMRepository()
	base, err := repo.Load(ctx, []core.FOMModule{{Path: "base.xml", XML: []byte(`<objectModel><dimensions><dimension><name>BaseDim</name><upperBound>50</upperBound></dimension></dimensions></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor(fed, base)
	manager, err := ddm.New(ddm.Options{FOMs: repo, Outbox: delivery.NewWithBatch(8, 1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	space, ok := manager.LookupRoutingSpace(fed, "default")
	if !ok {
		t.Fatal("missing routing space")
	}
	baseDim, ok := manager.LookupDimension(fed, space, "BaseDim")
	if !ok {
		t.Fatal("missing base dimension")
	}
	savedRegion, err := manager.CreateRegion(ctx, fed, 1, space, []ddm.DimensionHandle{baseDim})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetRangeBounds(fed, 1, savedRegion, baseDim, core.DDMRange{Lower: 10, Upper: 20}); err != nil {
		t.Fatal(err)
	}
	if err := manager.CommitRegionModifications(ctx, fed, 1, []ddm.RegionHandle{savedRegion}); err != nil {
		t.Fatal(err)
	}
	fomSnapshot, ddmSnapshot := FOM{Repository: repo}, DDM{Manager: manager}
	fomBytes, err := fomSnapshot.Marshal(fed)
	if err != nil {
		t.Fatal(err)
	}
	ddmBytes, err := ddmSnapshot.Marshal(fed)
	if err != nil {
		t.Fatal(err)
	}
	extend := func(path, xml string) uint64 {
		t.Helper()
		prepared, err := repo.PrepareLiveExtension(ctx, fed, []core.FOMModule{{Path: path, XML: []byte(xml)}})
		if err != nil {
			t.Fatal(err)
		}
		defer prepared.Release()
		if err := prepared.Commit(); err != nil {
			t.Fatal(err)
		}
		return prepared.View().Revision
	}
	oldRevision := extend("old.xml", `<objectModel><dimensions><dimension><name>OldDim</name><upperBound>100</upperBound></dimension></dimensions></objectModel>`)
	if _, ok := manager.LookupDimension(fed, space, "OldDim"); !ok {
		t.Fatal("old extension not cached")
	}
	preparedFOM, err := fomSnapshot.PrepareRestoreState(fed, fomBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer preparedFOM.Release()
	commitDDM, err := ddmSnapshot.PrepareRestore(fed, ddmBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := preparedFOM.Commit(); err != nil {
		t.Fatal(err)
	}
	commitDDM()
	// No DDM operation may refresh the rolled-back revision before it is reused.
	newRevision := extend("new.xml", `<objectModel><dimensions><dimension><name>NewDim</name><upperBound>1000</upperBound></dimension></dimensions></objectModel>`)
	if newRevision != oldRevision {
		t.Fatalf("revision not reused: old=%d new=%d", oldRevision, newRevision)
	}
	if _, ok := manager.LookupDimension(fed, space, "OldDim"); ok {
		t.Error("restored cache retained rolled-back OldDim at reused revision")
	}
	newDim, ok := manager.LookupDimension(fed, space, "NewDim")
	if !ok {
		t.Fatal("restored cache missed NewDim at reused revision")
	}
	if got, ok := manager.LookupDimension(fed, space, "BaseDim"); !ok || got != baseDim {
		t.Fatal("base dimension binding changed")
	}
	if bounds, ok := manager.QueryBounds(fed, savedRegion, baseDim); !ok || bounds != (core.DDMRange{Lower: 10, Upper: 20}) {
		t.Fatalf("saved region changed: %+v %v", bounds, ok)
	}
	region, err := manager.CreateRegion(ctx, fed, 1, space, []ddm.DimensionHandle{newDim})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetRangeBounds(fed, 1, region, newDim, ddm.Range{Upper: 1000}); err != nil {
		t.Fatal(err)
	}
	if err := manager.CommitRegionModifications(ctx, fed, 1, []ddm.RegionHandle{region}); err != nil {
		t.Fatal(err)
	}
	if bounds, ok := manager.QueryBounds(fed, region, newDim); !ok || bounds.Upper != 1000 {
		t.Fatalf("stale upper bound: %+v %v", bounds, ok)
	}
}
