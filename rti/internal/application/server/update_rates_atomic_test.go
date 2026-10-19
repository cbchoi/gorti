package server

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

func TestUpdateRateBridgeRegionFailureRetryAndResign(t *testing.T) {
	ctx := context.Background()
	repo := composition.NewFOMRepository()
	model, err := repo.Load(ctx, []core.FOMModule{{Path: "bridge.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>V</name><attribute><name>x</name><dimensions><dimension>X</dimension></dimensions></attribute></objectClass></objectClass></objects><dimensions><dimension><name>X</name><upperBound>100</upperBound></dimension></dimensions><updateRates><updateRate><name>Slow</name><rate>2</rate></updateRate><updateRate><name>Fast</name><rate>10</rate></updateRate></updateRates></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("f", model)
	cls, _ := model.LookupObjectClass("HLAobjectRoot.V")
	attr, _ := model.LookupAttribute(cls, "x")
	decl := declaration.New()
	decl.SetFOMRepository(repo)
	out := delivery.NewWithBatch(1, 1, 0)
	decl.SetAdvisoryOutbox(out)
	owners, err := ownership.New(ownership.Options{Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	regions, err := ddm.New(ddm.Options{Outbox: out, FOMs: repo, ValidateSubscriptionOptions: decl.ValidateSubscriptionOptions})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := object.New(object.Options{Declarations: decl, Outbox: out, FOMs: repo, Clock: core.NewRealClock(), Ownership: owners})
	if err != nil {
		t.Fatal(err)
	}
	restore, err := objects.PrepareImportState("f", &object.RegistryState{LastObjectHandle: 100, Objects: []object.InstanceState{{Handle: 100, Name: "v", Class: cls, Registrant: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	restore()
	owners.RegisterInitialOwnership("f", 1, 100, []core.AttributeHandle{attr})
	bridge := newUpdateRateBridge(repo, decl, objects, regions, owners)
	bridge.ObjectChanged(ctx, "f", 100)
	queue, stop, err := out.Subscribe(ctx, "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stop() }()
	if err := decl.SubscribeObjectClassAttributesWithOptions(ctx, "f", 2, cls, []core.AttributeHandle{attr}, core.SubscriptionOptions{Active: true, UpdateRateDesignator: "Slow"}); err != nil {
		t.Fatal(err)
	}
	<-queue
	space, _ := regions.LookupRoutingSpace("f", "default")
	dim, _ := regions.LookupDimension("f", space, "X")
	pub, err := regions.CreateRegion(ctx, "f", 1, space, []core.DDMDimensionHandle{dim})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := regions.CreateRegion(ctx, "f", 3, space, []core.DDMDimensionHandle{dim})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct {
		h      core.FederateHandle
		r      ddm.RegionHandle
		lo, hi uint64
	}{{1, pub, 0, 10}, {3, sub, 20, 30}} {
		if err := regions.SetRangeBounds("f", r.h, r.r, dim, core.DDMRange{Lower: r.lo, Upper: r.hi}); err != nil {
			t.Fatal(err)
		}
		if err := regions.CommitRegionModifications(ctx, "f", r.h, []ddm.RegionHandle{r.r}); err != nil {
			t.Fatal(err)
		}
	}
	if err := regions.AssociateRegionsForUpdates(ctx, "f", 1, 100, map[core.AttributeHandle][]ddm.RegionHandle{attr: {pub}}); err != nil {
		t.Fatal(err)
	}
	if err := regions.SubscribeObjectClassAttributesWithRegionsAndOptions(ctx, "f", 3, cls, []core.AttributeHandle{attr}, []ddm.RegionHandle{sub}, core.SubscriptionOptions{Active: true, UpdateRateDesignator: "Fast"}); err != nil {
		t.Fatal(err)
	}
	if err := out.Send(ctx, "f", 1, &fakeOutboundEvent{seq: 99}); err != nil {
		t.Fatal(err)
	}
	if err := regions.SetRangeBounds("f", 3, sub, dim, core.DDMRange{Lower: 5, Upper: 15}); err != nil {
		t.Fatal(err)
	}
	if err := regions.CommitRegionModifications(ctx, "f", 3, []ddm.RegionHandle{sub}); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("full queue=%v", err)
	}
	if bounds, _ := regions.QueryBounds("f", sub, dim); bounds.Lower != 20 {
		t.Fatal("failed admission committed region bounds")
	}
	if demand, _ := decl.EffectiveAttributeUpdateRate("f", 100, attr); demand.Rate != 2 {
		t.Fatalf("failed admission committed rate=%+v", demand)
	}
	<-queue
	if err := regions.CommitRegionModifications(ctx, "f", 3, []ddm.RegionHandle{sub}); err != nil {
		t.Fatal(err)
	}
	select {
	case batch := <-queue:
		e := batch[0].(*core.UpdateRateAdvisory)
		if e.UpdateRateDesignator == nil || *e.UpdateRateDesignator != "Fast" {
			t.Fatal("retry lost Fast edge")
		}
	default:
		t.Fatal("missing retry edge")
	}
	if err := out.Send(ctx, "f", 1, &fakeOutboundEvent{seq: 99}); err != nil {
		t.Fatal(err)
	}
	regions.OnFederateResign("f", 3)
	if state := regions.ExportState("f"); len(state.ObjectSubscriptions) != 0 || len(state.Regions) != 1 {
		t.Fatal("full queue retained resigned DDM state")
	}
	if demand, _ := decl.EffectiveAttributeUpdateRate("f", 100, attr); demand.Rate != 2 {
		t.Fatal("resign did not commit lower demand")
	}
	<-queue
	if err := decl.RetryAdvisories(ctx, "f"); err != nil {
		t.Fatal(err)
	}
	select {
	case batch := <-queue:
		e := batch[0].(*core.UpdateRateAdvisory)
		if e.UpdateRateDesignator == nil || *e.UpdateRateDesignator != "Slow" {
			t.Fatal("resign retry lost Slow edge")
		}
	default:
		t.Fatal("missing resign retry")
	}
}
