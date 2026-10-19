package server

import (
	"context"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	"google.golang.org/grpc/metadata"
)

func TestOwnershipCallbackRetryRecoversFullOutboxAfterDeletionProduction(t *testing.T) {
	for _, implementation := range []string{float64TimeName, integer64TimeName} {
		t.Run(implementation, func(t *testing.T) {
			srv, conn, ctx := resignRPCServer(t, 2)
			federations := rtiv1.NewFederationServiceClient(conn)
			const fed = "retry-deleted"
			_, err := federations.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: implementation, FomModules: []*rtiv1.FOMModule{{Path: "retry.xml", Xml: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>V</name><attribute><name>x</name></attribute></objectClass></objectClass></objects></objectModel>`)}}})
			if err != nil {
				t.Fatal(err)
			}
			generation, _ := srv.fedMgr.GenerationFor(fed)
			members := make([]*rtiv1.JoinFederationResponse, 2)
			auth := make([]context.Context, 2)
			for i, name := range []string{"owner", "requester"} {
				members[i], err = federations.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: fed, FederateName: name, ExpectedFederationGeneration: &generation})
				if err != nil {
					t.Fatal(err)
				}
				auth[i] = metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(members[i].SessionToken))
			}
			fom, _ := srv.foms.Get(ctx, fed)
			class, _ := fom.LookupObjectClass("HLAobjectRoot.V")
			attr, _ := fom.LookupAttribute(class, "x")
			decl := rtiv1.NewDeclarationServiceClient(conn)
			for i, member := range members {
				_, err = decl.PublishObjectClassAttributes(auth[i], &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attr)}})
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err = decl.SubscribeObjectClassAttributes(auth[1], &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: members[1].FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attr)}})
			if err != nil {
				t.Fatal(err)
			}
			objects := rtiv1.NewObjectServiceClient(conn)
			registered, err := objects.RegisterObjectInstance(auth[0], &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: fed, FederateHandle: members[0].FederateHandle, ObjectClassHandle: uint64(class)})
			if err != nil {
				t.Fatal(err)
			}
			epoch, err := srv.outbox.CallbackEpoch(fed, core.FederateHandle(members[1].FederateHandle))
			if err != nil {
				t.Fatal(err)
			}
			scope := &rtiv1.OwnershipCallbackScopeRequest{WireVersion: 1, FederationName: fed, FederateHandle: members[1].FederateHandle, FederationGeneration: generation, CallbackEpoch: epoch}
			if _, err := rtiv1.NewOwnershipCallbackServiceClient(conn).EnableOwnershipCallbackReceipts(auth[1], scope); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 3; i++ {
				_, err = rtiv1.NewOwnershipServiceClient(conn).AttributeOwnershipAcquisition(auth[1], &rtiv1.AcquireRequest{WireVersion: 1, FederationName: fed, FederateHandle: members[1].FederateHandle, ObjectHandle: registered.ObjectHandle, AttributeHandles: []uint64{uint64(attr)}, IfAvailable: true})
				if err != nil {
					t.Fatal(err)
				}
			}
			owner := srv.ownMgr.(*ownership.Manager)
			if len(owner.PendingOwnershipAdmissions()) == 0 {
				t.Fatal("fixture did not produce full-outbox admission failure")
			}
			streamCtx, cancel := context.WithCancel(auth[1])
			defer cancel()
			stream, err := rtiv1.NewStreamServiceClient(conn).EventBatches(streamCtx, &rtiv1.EventsRequest{WireVersion: 1, FederationName: fed, FederateHandle: members[1].FederateHandle})
			if err != nil {
				t.Fatal(err)
			}
			if ready, err := stream.Recv(); err != nil || !ready.Ready {
				t.Fatalf("ready: %v %v", ready, err)
			}
			// Drain the queued discovery before submitting the global deletion.
			first, err := stream.Recv()
			if err != nil {
				t.Fatal(err)
			}
			_, err = objects.DeleteObjectInstance(auth[0], &rtiv1.DeleteObjectInstanceRequest{WireVersion: 1, FederationName: fed, FederateHandle: members[0].FederateHandle, ObjectHandle: registered.ObjectHandle})
			if err != nil {
				t.Fatal(err)
			}
			stop := startOwnershipCallbackRetries(ctx, owner, srv.fedMgr, nil, time.Millisecond)
			defer stop()
			ids := make(map[uint64]bool)
			for _, event := range first.Events {
				if event.GetOwnershipUnavailable() != nil {
					ids[event.GetOwnershipReceipt().CallbackId] = true
				}
			}
			removed := false
			for len(ids) < 3 || !removed {
				batch, err := stream.Recv()
				if err != nil {
					t.Fatalf("ids=%v removed=%v admissions=%v: %v", ids, removed, owner.PendingOwnershipAdmissions(), err)
				}
				for _, event := range batch.Events {
					if event.GetOwnershipUnavailable() != nil {
						receipt := event.GetOwnershipReceipt()
						if receipt == nil {
							t.Fatal("terminal receipt missing")
						}
						ids[receipt.CallbackId] = true
					}
					if event.GetRemove() != nil {
						removed = true
					}
				}
			}
			if len(owner.PendingOwnershipAdmissions()) != 0 {
				t.Fatal("successful retry remained eligible for periodic resend")
			}
			if !owner.HasAcquisitionObligation(fed, core.FederateHandle(members[1].FederateHandle), core.ObjectHandle(registered.ObjectHandle), nil) {
				t.Fatal("queue recovery finished strict obligation before invocation")
			}
		})
	}
}
