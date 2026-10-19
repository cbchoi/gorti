package server

import (
	"context"
	"slices"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestProductionUnpublicationOwnershipAndInvocation(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		t.Run(factory, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			srv, conn, _ := integerProductionServer(t, t.TempDir(), 181)
			const fed = "unpublication"
			created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: factory, FomModules: []*rtiv1.FOMModule{{Path: "unpublication.xml", Xml: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>V</name><attribute><name>x</name></attribute><attribute><name>y</name></attribute></objectClass></objectClass></objects></objectModel>`)}}})
			if err != nil {
				t.Fatal(err)
			}
			joined := []*rtiv1.JoinFederationResponse{
				momCompletionReviewJoin(ctx, t, conn, fed, "owner", "", created.FederationGeneration),
				momCompletionReviewJoin(ctx, t, conn, fed, "requester", "", created.FederationGeneration),
			}
			auth := []context.Context{
				metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(joined[0].SessionToken)),
				metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(joined[1].SessionToken)),
			}
			model, err := srv.foms.Get(ctx, fed)
			if err != nil {
				t.Fatal(err)
			}
			class, _ := model.LookupObjectClass("HLAobjectRoot.V")
			x, _ := model.LookupAttribute(class, "x")
			y, _ := model.LookupAttribute(class, "y")
			privilege, _ := model.LookupAttribute(class, "HLAprivilegeToDeleteObject")
			declarations := rtiv1.NewDeclarationServiceClient(conn)
			declarationState := srv.declMgr.(*declaration.Manager)
			for i, member := range joined {
				if _, err := declarations.PublishObjectClassAttributes(auth[i], &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(x), uint64(y)}}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := declarations.SubscribeObjectClassAttributes(auth[1], &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: joined[1].FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(x), uint64(y)}}); err != nil {
				t.Fatal(err)
			}
			registered, err := rtiv1.NewObjectServiceClient(conn).RegisterObjectInstance(auth[0], &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: fed, FederateHandle: joined[0].FederateHandle, ObjectClassHandle: uint64(class)})
			if err != nil {
				t.Fatal(err)
			}
			stream, err := rtiv1.NewStreamServiceClient(conn).EventBatches(auth[1], &rtiv1.EventsRequest{WireVersion: 1, FederationName: fed, FederateHandle: joined[1].FederateHandle})
			if err != nil {
				t.Fatal(err)
			}
			if ready, err := stream.Recv(); err != nil || !ready.Ready {
				t.Fatalf("ready=%v error=%v", ready, err)
			}
			epoch, err := srv.outbox.CallbackEpoch(fed, core.FederateHandle(joined[1].FederateHandle))
			if err != nil {
				t.Fatal(err)
			}
			receipts := rtiv1.NewOwnershipCallbackServiceClient(conn)
			if _, err := receipts.EnableOwnershipCallbackReceipts(auth[1], &rtiv1.OwnershipCallbackScopeRequest{WireVersion: 1, FederationName: fed, FederateHandle: joined[1].FederateHandle, FederationGeneration: created.FederationGeneration, CallbackEpoch: epoch}); err != nil {
				t.Fatal(err)
			}
			acquire := &rtiv1.AcquireRequest{WireVersion: 1, FederationName: fed, FederateHandle: joined[1].FederateHandle, ObjectHandle: registered.ObjectHandle, AttributeHandles: []uint64{uint64(x)}, IfAvailable: true}
			if _, err := rtiv1.NewOwnershipServiceClient(conn).AttributeOwnershipAcquisition(auth[1], acquire); err != nil {
				t.Fatal(err)
			}
			var receipt *rtiv1.OwnershipCallbackReceipt
			for receipt == nil {
				batch, err := stream.Recv()
				if err != nil {
					t.Fatal(err)
				}
				for _, event := range batch.Events {
					if event.GetOwnershipUnavailable() != nil {
						receipt = event.OwnershipReceipt
					}
				}
			}
			unpublish := &rtiv1.UnpubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: joined[1].FederateHandle, ObjectClassHandle: uint64(class)}
			if _, err := declarations.UnpublishObjectClassAttributes(auth[1], unpublish); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("transport delivery prematurely discharged guard: %v", err)
			}
			present := true
			unpublish.AttributeSetPresent = &present
			if _, err := declarations.UnpublishObjectClassAttributes(auth[1], unpublish); err != nil {
				t.Fatal("empty set consulted unrelated pending result", err)
			}
			published := declarationState.PublishedObjectAttributes(fed, core.FederateHandle(joined[1].FederateHandle), class)
			if len(published) != 3 || !slices.Contains(published, x) || !slices.Contains(published, y) || !slices.Contains(published, privilege) {
				t.Fatal("explicit empty removed publication")
			}
			if _, err := receipts.ReportOwnershipCallbackInvocation(auth[1], &rtiv1.ReportOwnershipCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: joined[1].FederateHandle, Receipt: receipt}); err != nil {
				t.Fatal(err)
			}
			acquire.IfAvailable = false
			if _, err := rtiv1.NewOwnershipServiceClient(conn).AttributeOwnershipAcquisition(auth[1], acquire); err != nil {
				t.Fatal(err)
			}
			unpublish.FederateHandle = joined[0].FederateHandle
			unpublish.AttributeHandles = []uint64{uint64(x)}
			if _, err := declarations.UnpublishObjectClassAttributes(auth[0], unpublish); err != nil {
				t.Fatal(err)
			}
			if !srv.ownMgr.IsOwnedBy(fed, core.FederateHandle(joined[1].FederateHandle), core.ObjectHandle(registered.ObjectHandle), x) || !srv.ownMgr.IsOwnedBy(fed, core.FederateHandle(joined[0].FederateHandle), core.ObjectHandle(registered.ObjectHandle), y) {
				t.Fatal("unpublication ownership transfer is not coherent with declaration")
			}
			published = declarationState.PublishedObjectAttributes(fed, core.FederateHandle(joined[0].FederateHandle), class)
			if len(published) != 2 || !slices.Contains(published, y) || !slices.Contains(published, privilege) {
				t.Fatal("targeted unpublication did not retain y")
			}
		})
	}
}
