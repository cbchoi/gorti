package server

import (
	"context"
	"errors"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	"google.golang.org/grpc/metadata"
)

func TestUpdateRateProductionRegionalOwnerAndSupport(t *testing.T) {
	srv, conn, _ := scheduledServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const fed core.FederationName = "rates"
	xml := []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>position</name><dataType>HLAinteger32BE</dataType><dimensions><dimension>X</dimension></dimensions></attribute></objectClass></objectClass></objects><dimensions><dimension><name>X</name><upperBound>100</upperBound></dimension></dimensions><updateRates><updateRate><name>Slow</name><rate>2.5</rate></updateRate><updateRate><name>Fast</name><rate>20</rate></updateRate></updateRates></objectModel>`)
	created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: string(fed), FomModules: []*rtiv1.FOMModule{{Path: "rates.xml", Xml: xml}}})
	if err != nil {
		t.Fatal(err)
	}
	var hs []core.FederateHandle
	auth := make(map[core.FederateHandle]context.Context)
	for _, name := range []string{"owner", "slow", "fast"} {
		joined := momCompletionReviewJoin(ctx, t, conn, string(fed), name, "", created.FederationGeneration)
		h := core.FederateHandle(joined.FederateHandle)
		auth[h] = metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(joined.SessionToken))
		hs = append(hs, h)
	}
	model, err := srv.foms.Get(ctx, fed)
	if err != nil {
		t.Fatal(err)
	}
	cls, _ := model.LookupObjectClass("HLAobjectRoot.Vehicle")
	attr, _ := model.LookupAttribute(cls, "position")
	decl := srv.declMgr.(*declaration.Manager)
	regions := srv.ddmMgr.(*ddm.Manager)
	owners := srv.ownMgr.(*ownership.Manager)
	queue, stop, err := srv.outbox.Subscribe(ctx, fed, hs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stop() }()
	expect := func(name string, enabled bool) {
		t.Helper()
		for {
			select {
			case batch := <-queue:
				for _, event := range batch {
					wire, err := grpcadapter.EncodeOutboundEvent(event)
					if err != nil {
						t.Fatal(err)
					}
					if wire.GetTurnUpdatesOn() != nil || wire.GetTurnUpdatesOff() != nil {
						if (wire.GetTurnUpdatesOn() != nil) != enabled {
							t.Fatalf("enabled=%v want=%v", wire.GetTurnUpdatesOn() != nil, enabled)
						}
						if name != "" && wire.GetTurnUpdatesOn().GetUpdateRateDesignator() != name {
							t.Fatalf("rate=%v want=%s", wire.GetTurnUpdatesOn(), name)
						}
						return
					}
				}
			case <-ctx.Done():
				t.Fatal("missing production update-rate callback")
				return
			}
		}
	}
	if err := decl.PublishObjectClassAttributes(ctx, fed, hs[0], cls, []core.AttributeHandle{attr}); err != nil {
		t.Fatal(err)
	}
	obj, _, err := srv.objReg.Register(ctx, fed, hs[0], cls, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := decl.SubscribeObjectClassAttributesWithOptions(ctx, fed, hs[1], cls, []core.AttributeHandle{attr}, core.SubscriptionOptions{Active: true, UpdateRateDesignator: "Slow"}); err != nil {
		t.Fatal(err)
	}
	expect("Slow", true)
	space, _ := regions.LookupRoutingSpace(fed, "default")
	dim, _ := regions.LookupDimension(fed, space, "X")
	pub, err := regions.CreateRegion(ctx, fed, hs[0], space, []core.DDMDimensionHandle{dim})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := regions.CreateRegion(ctx, fed, hs[2], space, []core.DDMDimensionHandle{dim})
	if err != nil {
		t.Fatal(err)
	}
	bounds := func(h core.FederateHandle, r ddm.RegionHandle, lo, hi uint64) {
		t.Helper()
		if err := regions.SetRangeBounds(fed, h, r, dim, core.DDMRange{Lower: lo, Upper: hi}); err != nil {
			t.Fatal(err)
		}
		if err := regions.CommitRegionModifications(ctx, fed, h, []ddm.RegionHandle{r}); err != nil {
			t.Fatal(err)
		}
	}
	bounds(hs[0], pub, 0, 10)
	bounds(hs[2], sub, 20, 30)
	if err := regions.AssociateRegionsForUpdates(ctx, fed, hs[0], obj, map[core.AttributeHandle][]ddm.RegionHandle{attr: {pub}}); err != nil {
		t.Fatal(err)
	}
	if err := regions.SubscribeObjectClassAttributesWithRegionsAndOptions(ctx, fed, hs[2], cls, []core.AttributeHandle{attr}, []ddm.RegionHandle{sub}, core.SubscriptionOptions{Active: true, UpdateRateDesignator: "Fast"}); err != nil {
		t.Fatal(err)
	}
	demand, _ := decl.EffectiveAttributeUpdateRate(fed, obj, attr)
	if demand.Rate != 2.5 {
		t.Fatalf("nonoverlap raised rate: %+v", demand)
	}
	bounds(hs[2], sub, 5, 15)
	expect("Fast", true)
	if _, knownClass, known := srv.objReg.LookupKnownObjectInstance(fed, hs[2], obj); !known || knownClass != cls {
		t.Fatal("committed overlap did not discover the object")
	}
	support := rtiv1.NewSupportServiceClient(conn)
	rate, err := support.GetUpdateRateValueForAttribute(ctx, &rtiv1.GetUpdateRateValueForAttributeRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(hs[0]), ObjectHandle: uint64(obj), AttributeHandle: uint64(attr)})
	if err != nil || rate.GetUpdateRate() != 20 {
		t.Fatalf("attribute query=%v %v", rate, err)
	}
	dims, err := support.GetAvailableDimensionsForClassAttribute(ctx, &rtiv1.GetAvailableDimensionsForClassAttributeRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(hs[0]), ObjectClassHandle: uint64(cls), AttributeHandle: uint64(attr)})
	if err != nil || len(dims.GetDimensionHandles()) != 1 || dims.DimensionHandles[0] != uint64(dim) {
		t.Fatalf("class dimensions=%v %v", dims, err)
	}
	regionDims, err := support.GetDimensionHandleSet(auth[hs[2]], &rtiv1.GetDimensionHandleSetRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(hs[2]), RegionHandle: uint64(sub)})
	if err != nil || len(regionDims.GetDimensionHandles()) != 1 {
		t.Fatalf("region dimensions=%v %v", regionDims, err)
	}
	if _, err := support.GetDimensionHandleSet(auth[hs[0]], &rtiv1.GetDimensionHandleSetRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(hs[0]), RegionHandle: uint64(sub)}); err == nil {
		t.Fatal("foreign federate queried creator's region dimensions")
	}
	bounds(hs[2], sub, 20, 30)
	expect("Slow", true)
	if err := regions.UnassociateRegionsForUpdates(ctx, fed, hs[0], obj, map[core.AttributeHandle][]ddm.RegionHandle{attr: {pub}}); err != nil {
		t.Fatal(err)
	}
	expect("Fast", true)
	regions.OnFederateResign(fed, hs[2])
	expect("Slow", true)
	if state := regions.ExportState(fed); len(state.ObjectSubscriptions) != 0 {
		t.Fatal("regional resign retained subscription")
	}
	if err := owners.UnconditionalDivest(ctx, fed, hs[0], obj, []core.AttributeHandle{attr}); err != nil {
		t.Fatal(err)
	}
	if err := decl.PublishObjectClassAttributes(ctx, fed, hs[2], cls, []core.AttributeHandle{attr}); err != nil {
		t.Fatal(err)
	}
	if err := decl.SubscribeObjectClassAttributesWithOptions(ctx, fed, hs[2], cls, []core.AttributeHandle{attr}, core.SubscriptionOptions{Active: false}); err != nil {
		t.Fatal(err)
	}
	if err := owners.AcquireIfAvailable(ctx, fed, hs[2], obj, []core.AttributeHandle{attr}, nil); err != nil {
		t.Fatal(err)
	}
	state := decl.ExportState(fed)
	found := false
	for _, o := range state.UpdateRateObjects {
		if o.Object == obj {
			for _, a := range o.Owners {
				if a.Attribute == attr {
					found = a.Owner == hs[2]
				}
			}
		}
	}
	if !found {
		t.Fatal("production ownership transition did not update rate target")
	}
	if _, err := srv.objReg.GetUpdateRateValueForAttribute(ctx, fed, 999, obj, attr); !errors.Is(err, core.ErrObjectHandleInvalid) {
		t.Fatalf("unknown object query=%v", err)
	}
}
