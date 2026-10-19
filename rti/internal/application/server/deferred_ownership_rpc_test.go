package server

import (
	"context"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	"google.golang.org/grpc/metadata"
)

func TestDeferredDeletionOwnershipTerminalProduction(t *testing.T) {
	for _, implementation := range []string{float64TimeName, integer64TimeName} {
		for _, advance := range []string{"TAR", testNextMessageRequest, testFlushQueueRequest} {
			t.Run(implementation+"/"+advance, func(t *testing.T) {
				srv, conn, ctx := resignRPCServer(t)
				const fed = "deferred-owner"
				federations := rtiv1.NewFederationServiceClient(conn)
				_, err := federations.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: implementation, FomModules: []*rtiv1.FOMModule{{Path: "deferred.xml", Xml: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>V</name><attribute><name>x</name></attribute></objectClass></objectClass></objects></objectModel>`)}}})
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
					if _, err := decl.PublishObjectClassAttributes(auth[i], &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attr)}}); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := decl.SubscribeObjectClassAttributes(auth[1], &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: members[1].FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attr)}}); err != nil {
					t.Fatal(err)
				}
				objects := rtiv1.NewObjectServiceClient(conn)
				registered, err := objects.RegisterObjectInstance(auth[0], &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: fed, FederateHandle: members[0].FederateHandle, ObjectClassHandle: uint64(class)})
				if err != nil {
					t.Fatal(err)
				}
				stream, err := rtiv1.NewStreamServiceClient(conn).EventBatches(auth[1], &rtiv1.EventsRequest{WireVersion: 1, FederationName: fed, FederateHandle: members[1].FederateHandle})
				if err != nil {
					t.Fatal(err)
				}
				if ready, err := stream.Recv(); err != nil || !ready.Ready {
					t.Fatalf("ready: %v %v", ready, err)
				}
				epoch, err := srv.outbox.CallbackEpoch(fed, core.FederateHandle(members[1].FederateHandle))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := rtiv1.NewOwnershipCallbackServiceClient(conn).EnableOwnershipCallbackReceipts(auth[1], &rtiv1.OwnershipCallbackScopeRequest{WireVersion: 1, FederationName: fed, FederateHandle: members[1].FederateHandle, FederationGeneration: generation, CallbackEpoch: epoch}); err != nil {
					t.Fatal(err)
				}
				if _, err := rtiv1.NewOwnershipServiceClient(conn).AttributeOwnershipAcquisition(auth[1], &rtiv1.AcquireRequest{WireVersion: 1, FederationName: fed, FederateHandle: members[1].FederateHandle, ObjectHandle: registered.ObjectHandle, AttributeHandles: []uint64{uint64(attr)}}); err != nil {
					t.Fatal(err)
				}
				times := rtiv1.NewTimeServiceClient(conn)
				regulate := &rtiv1.EnableRegulationRequest{WireVersion: 1, FederationName: fed, FederateHandle: members[0].FederateHandle, Lookahead: 1}
				deleteRequest := &rtiv1.DeleteObjectInstanceRequest{WireVersion: 1, FederationName: fed, FederateHandle: members[0].FederateHandle, ObjectHandle: registered.ObjectHandle}
				target := float64(10)
				deleteRequest.LogicalTime = &target
				var exact *rtiv1.LogicalTimeValue
				if implementation == integer64TimeName {
					regulate.Lookahead = 0
					regulate.LookaheadValue = integerWireTime(t, 1)
					deleteRequest.LogicalTime = nil
					target = 0
					exact = integerWireTime(t, 1<<53+17)
					deleteRequest.LogicalTimeValue = exact
				}
				if _, err := times.EnableTimeRegulation(auth[0], regulate); err != nil {
					t.Fatal(err)
				}
				if _, err := objects.DeleteObjectInstance(auth[0], deleteRequest); err != nil {
					t.Fatal(err)
				}
				owner := srv.ownMgr.(*ownership.Manager)
				if len(owner.ExportState(fed).PendingCallbacks) != 0 {
					t.Fatal("future timestamp prematurely terminated acquisition")
				}
				removed := false
				for !removed {
					batch, err := stream.Recv()
					if err != nil {
						t.Fatal(err)
					}
					for _, event := range batch.Events {
						if event.GetOwnershipUnavailable() != nil {
							t.Fatal("terminal preceded sender timestamp")
						}
						if event.GetRemove() != nil {
							removed = true
						}
					}
				}
				switch advance {
				case "TAR":
					_, err = times.TimeAdvanceRequest(auth[0], &rtiv1.TARRequest{WireVersion: 1, FederationName: fed, FederateHandle: members[0].FederateHandle, LogicalTime: target, LogicalTimeValue: exact})
				case testNextMessageRequest:
					_, err = times.NextMessageRequest(auth[0], &rtiv1.NERRequest{WireVersion: 1, FederationName: fed, FederateHandle: members[0].FederateHandle, LogicalTime: target, LogicalTimeValue: exact})
				case testFlushQueueRequest:
					_, err = times.FlushQueueRequest(auth[0], &rtiv1.FQRRequest{WireVersion: 1, FederationName: fed, FederateHandle: members[0].FederateHandle, LogicalTime: target, LogicalTimeValue: exact})
				}
				if err != nil {
					t.Fatal(err)
				}
				found := false
				var receipt *rtiv1.OwnershipCallbackReceipt
				for !found {
					batch, err := stream.Recv()
					if err != nil {
						t.Fatal(err)
					}
					for _, event := range batch.Events {
						if callback := event.GetOwnershipUnavailable(); callback != nil {
							if callback.ObjectHandle != registered.ObjectHandle || event.GetOwnershipReceipt() == nil {
								t.Fatalf("wrong terminal identity: object=%d want=%d ownershipReceipt=%v generalReceiptBytes=%d", callback.ObjectHandle, registered.ObjectHandle, event.GetOwnershipReceipt(), len(event.GetCallbackReceipt()))
							}
							found = true
							receipt = event.GetOwnershipReceipt()
						}
					}
				}
				state := owner.ExportState(fed)
				if len(state.PendingAcquires) != 0 || len(state.PendingCallbacks) != 1 || state.PendingCallbacks[0].RetainedProjection == nil {
					t.Fatalf("pending outcomes: %+v", state.PendingCallbacks)
				}
				classOf := srv.objReg.ClassOf
				if implementation == integer64TimeName {
					classOf = srv.integerObjects.ClassOf
				}
				if classOf(fed, core.ObjectHandle(registered.ObjectHandle)) != 0 || owner.IsOwnedBy(fed, core.FederateHandle(members[0].FederateHandle), core.ObjectHandle(registered.ObjectHandle), attr) {
					t.Fatal("timestamp deletion failed to retire object and ownership")
				}
				if !owner.HasAcquisitionObligation(fed, core.FederateHandle(members[1].FederateHandle), core.ObjectHandle(registered.ObjectHandle), nil) {
					t.Fatal("delivered terminal completed before invocation")
				}
				if _, err := rtiv1.NewOwnershipCallbackServiceClient(conn).ReportOwnershipCallbackInvocation(auth[1], &rtiv1.ReportOwnershipCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: members[1].FederateHandle, Receipt: receipt}); err != nil {
					t.Fatal(err)
				}
				if owner.HasAcquisitionObligation(fed, core.FederateHandle(members[1].FederateHandle), core.ObjectHandle(registered.ObjectHandle), nil) {
					t.Fatal("invocation did not finish deferred deletion terminal")
				}
			})
		}
	}
}
