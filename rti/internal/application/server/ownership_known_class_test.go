package server

import (
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

func TestKnownClassOwnershipProduction(t *testing.T) {
	const fed core.FederationName = "known-ownership"
	ctx, srv, conn, _ := momCompletionReviewServer(t, fed)
	created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{
		WireVersion: 1, FederationName: string(fed), FomModules: []*rtiv1.FOMModule{{Path: "known.xml", Xml: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Base</name><attribute><name>position</name></attribute><objectClass><name>Child</name><attribute><name>childValue</name></attribute></objectClass></objectClass></objectClass></objects></objectModel>`)}}})
	if err != nil {
		t.Fatal(err)
	}
	owner := core.FederateHandle(momCompletionReviewJoin(ctx, t, conn, string(fed), "owner", "", created.FederationGeneration).FederateHandle)
	candidate := core.FederateHandle(momCompletionReviewJoin(ctx, t, conn, string(fed), "candidate", "", created.FederationGeneration).FederateHandle)
	fom, err := srv.foms.Get(ctx, fed)
	if err != nil {
		t.Fatal(err)
	}
	base, _ := fom.LookupObjectClass("Base")
	child, _ := fom.LookupObjectClass("Child")
	baseAttr, _ := fom.LookupAttribute(base, "position")
	actualAttr, _ := fom.LookupAttribute(child, "position")
	childAttr, _ := fom.LookupAttribute(child, "childValue")
	if baseAttr == actualAttr {
		t.Fatal("fixture must use different inherited attribute handles")
	}
	queue, done, err := srv.outbox.Subscribe(ctx, fed, candidate)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := done(); err != nil {
			t.Error(err)
		}
	}()
	ownerQueue, ownerDone, err := srv.outbox.Subscribe(ctx, fed, owner)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ownerDone(); err != nil {
			t.Error(err)
		}
	}()
	if err := srv.declMgr.PublishObjectClassAttributes(ctx, fed, owner, child, []core.AttributeHandle{actualAttr, childAttr}); err != nil {
		t.Fatal(err)
	}
	if err := srv.declMgr.SubscribeObjectClassAttributes(ctx, fed, candidate, base, []core.AttributeHandle{baseAttr}); err != nil {
		t.Fatal(err)
	}
	obj, _, err := srv.objReg.Register(ctx, fed, owner, child, "")
	if err != nil {
		t.Fatal(err)
	}
	drainScheduledEvents(queue)
	client := rtiv1.NewOwnershipServiceClient(conn)
	if _, err := client.UnconditionalAttributeOwnershipDivestiture(ctx, &rtiv1.UnconditionalDivestRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(owner), ObjectHandle: uint64(obj), AttributeHandles: []uint64{uint64(actualAttr)}}); err != nil {
		t.Fatal(err)
	}
	if len(ownershipPublicationAssumptions(t, drainScheduledEvents(queue))) != 0 {
		t.Fatal("nonpublisher was solicited")
	}
	if err := srv.declMgr.PublishObjectClassAttributes(ctx, fed, candidate, base, []core.AttributeHandle{baseAttr}); err != nil {
		t.Fatal(err)
	}
	assumptions := ownershipPublicationAssumptions(t, drainScheduledEvents(queue))
	if len(assumptions) != 1 || len(assumptions[0].AttributeHandles) != 1 || assumptions[0].AttributeHandles[0] != uint64(baseAttr) {
		t.Fatalf("ancestor publisher assumption uses wrong coordinates: %+v", assumptions)
	}
	if _, err := client.AttributeOwnershipAcquisition(ctx, &rtiv1.AcquireRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(candidate), ObjectHandle: uint64(obj), AttributeHandles: []uint64{uint64(baseAttr)}, IfAvailable: true}); err != nil {
		t.Fatal(err)
	}
	acquired := 0
	for _, event := range drainScheduledEvents(queue) {
		if callback := ownershipReviewWireEvent(t, event).GetOwnershipAcquired(); callback != nil {
			acquired++
			if len(callback.AttributeHandles) != 1 || callback.AttributeHandles[0] != uint64(baseAttr) {
				t.Fatalf("acquired wrong handles: %+v", callback)
			}
		}
	}
	if acquired != 1 {
		t.Fatalf("acquisition callbacks=%d", acquired)
	}
	owners := srv.ownMgr.(*ownership.Manager)
	if got, ok := owners.QueryOwnership(fed, obj, actualAttr); !ok || got != candidate {
		t.Fatalf("inherited owner=%d,%t", got, ok)
	}
	if got, ok := owners.QueryOwnership(fed, obj, childAttr); !ok || got != owner {
		t.Fatalf("unrelated attribute owner changed: %d,%t", got, ok)
	}
	if err := srv.objReg.UpdateAttributes(ctx, fed, candidate, obj, map[core.AttributeHandle][]byte{baseAttr: {7}}, nil); err != nil {
		t.Fatalf("ancestor owner cannot update acquired attribute: %v", err)
	}
	drainScheduledEvents(ownerQueue)
	if _, err := client.AttributeOwnershipAcquisition(ctx, &rtiv1.AcquireRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(owner), ObjectHandle: uint64(obj), AttributeHandles: []uint64{uint64(actualAttr)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AttributeOwnershipReleaseDenied(ctx, &rtiv1.ReleaseDeniedRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(candidate), ObjectHandle: uint64(obj), AttributeHandles: []uint64{uint64(baseAttr)}}); err != nil {
		t.Fatalf("mapped release denial: %v", err)
	}
	unavailable := 0
	for _, event := range drainScheduledEvents(ownerQueue) {
		if callback := ownershipReviewWireEvent(t, event).GetOwnershipUnavailable(); callback != nil {
			unavailable++
			if len(callback.AttributeHandles) != 1 || callback.AttributeHandles[0] != uint64(actualAttr) {
				t.Fatalf("unavailable wrong coordinates: %+v", callback)
			}
		}
	}
	if unavailable != 1 {
		t.Fatalf("unavailable callbacks=%d", unavailable)
	}
}
