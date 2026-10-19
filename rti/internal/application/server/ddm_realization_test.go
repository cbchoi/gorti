package server

import (
	"context"
	"testing"
	"time"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func TestProductionDDMExcludedDefaultRecipientBoundaries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	const fed core.FederationName = "excluded-root"
	srv, conn, _ := restartCheckpointServer(t, t.TempDir(), 37)
	ok := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: string(fed), FomModules: []*rtiv1.FOMModule{{Path: "excluded.xml", Xml: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>B</name><dataType>HLAinteger32BE</dataType><dimensions><dimension>Y</dimension></dimensions></attribute></objectClass></objectClass></objects><interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Signal</name><dimensions><dimension>Y</dimension></dimensions></interactionClass></interactionClass></interactions><dimensions><dimension><name>Y</name><upperBound>100</upperBound><value>Excluded</value></dimension></dimensions></objectModel>`)}}})
	ok(err)
	var handles []core.FederateHandle
	for _, name := range []string{"producer", "regional", "ordinary"} {
		h, err := srv.fedMgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: name})
		ok(err)
		handles = append(handles, h)
	}
	model, err := srv.foms.Get(ctx, fed)
	ok(err)
	cls, _ := model.LookupObjectClass("Vehicle")
	attr, _ := model.LookupAttribute(cls, "B")
	signal, _ := model.LookupInteractionClass("Signal")
	y, _ := model.(core.FOMHandleNameLookup).LookupDimension("Y")
	decl := rtiv1.NewDeclarationServiceClient(conn)
	regions := rtiv1.NewDDMServiceClient(conn)
	objects := rtiv1.NewObjectServiceClient(conn)
	queues := map[core.FederateHandle]<-chan []core.OutboundEvent{}
	for _, h := range handles[1:] {
		q, release, err := srv.outbox.Subscribe(ctx, fed, h)
		ok(err)
		t.Cleanup(func() { _ = release() })
		queues[h] = q
		_, err = rtiv1.NewTimeServiceClient(conn).EnableAsynchronousDelivery(ctx, &rtiv1.EnableAsynchronousDeliveryRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(h)})
		ok(err)
	}
	spec := func(h core.FederateHandle, explicit bool) uint64 {
		t.Helper()
		var dims []uint64
		if explicit {
			dims = []uint64{uint64(y)}
		}
		r, err := regions.CreateRegion(ctx, &rtiv1.CreateRegionRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(h), RoutingSpaceHandle: 1, DimensionHandles: dims})
		ok(err)
		if explicit {
			_, err = regions.SetRangeBounds(ctx, &rtiv1.SetRangeBoundsRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(h), RegionHandle: r.RegionHandle, DimensionHandle: uint64(y), Bounds: &rtiv1.Range{Lower: 1, Upper: 2}})
			ok(err)
		}
		_, err = regions.CommitRegionModifications(ctx, &rtiv1.CommitRegionRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(h), RegionHandles: []uint64{r.RegionHandle}})
		ok(err)
		return r.RegionHandle
	}
	emptyPub, emptySub := spec(handles[0], false), spec(handles[1], false)
	_, err = decl.PublishObjectClassAttributes(ctx, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(handles[0]), ObjectClassHandle: uint64(cls), AttributeHandles: []uint64{uint64(attr)}})
	ok(err)
	_, err = decl.PublishInteractionClass(ctx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(handles[0]), InteractionClassHandle: uint64(signal)})
	ok(err)
	_, err = decl.SubscribeObjectClassAttributes(ctx, &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(handles[2]), ObjectClassHandle: uint64(cls), AttributeHandles: []uint64{uint64(attr)}})
	ok(err)
	_, err = decl.SubscribeInteractionClass(ctx, &rtiv1.SubInterRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(handles[2]), InteractionClassHandle: uint64(signal)})
	ok(err)
	_, err = regions.SubscribeInteractionClassWithRegions(ctx, &rtiv1.SubscribeICWithRegionsRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(handles[1]), InteractionClassHandle: uint64(signal), RegionHandles: []uint64{emptySub}})
	ok(err)
	registered, err := regions.RegisterObjectInstanceWithRegions(ctx, &rtiv1.RegisterObjectWithRegionsRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(handles[0]), ObjectClassHandle: uint64(cls), AttributeRegions: []*rtiv1.AttributeRegions{{AttributeHandle: uint64(attr), RegionHandles: []uint64{emptyPub}}}})
	ok(err)
	for _, q := range queues {
		drainScheduledEvents(q)
	}
	count := func(h core.FederateHandle, kind object.CallbackKind) int {
		t.Helper()
		n := 0
		for _, event := range drainScheduledEvents(queues[h]) {
			wire, err := grpcadapter.EncodeOutboundEvent(event)
			ok(err)
			if kind == object.CallbackReflect && wire.GetReflect() != nil || kind == object.CallbackReceive && wire.GetReceive() != nil {
				n++
			}
		}
		return n
	}
	update := func() {
		_, err := objects.UpdateAttributeValues(ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(handles[0]), ObjectHandle: registered.ObjectHandle, Attributes: map[uint64][]byte{uint64(attr): {1}}})
		ok(err)
	}
	update()
	if count(handles[2], object.CallbackReflect) != 0 {
		t.Fatal("empty producer realization reached ordinary attribute subscriber")
	}
	_, err = objects.SendInteraction(ctx, &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(handles[0]), InteractionClassHandle: uint64(signal)})
	ok(err)
	if count(handles[1], object.CallbackReceive) != 0 || count(handles[2], object.CallbackReceive) != 1 {
		t.Fatal("ordinary interaction default-region union is incorrect")
	}
	_, err = regions.SendInteractionWithRegions(ctx, &rtiv1.SendInteractionWithRegionsRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(handles[0]), InteractionClassHandle: uint64(signal), RegionHandles: []uint64{emptyPub}})
	ok(err)
	if count(handles[1], object.CallbackReceive) != 0 || count(handles[2], object.CallbackReceive) != 0 {
		t.Fatal("empty interaction producer reached default region")
	}
	explicitPub := spec(handles[0], true)
	_, err = regions.AssociateRegionsForUpdates(ctx, &rtiv1.AssociateRegionsForUpdatesRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(handles[0]), ObjectHandle: registered.ObjectHandle, AttributeRegions: []*rtiv1.AttributeRegions{{AttributeHandle: uint64(attr), RegionHandles: []uint64{explicitPub}}}})
	ok(err)
	for _, q := range queues {
		drainScheduledEvents(q)
	}
	update()
	if count(handles[2], object.CallbackReflect) != 1 {
		t.Fatal("explicit override of Excluded failed to reach default region")
	}
}
