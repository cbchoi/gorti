package server

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/pkg/encoding"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestProductionMOMTransportationConfirmationTargetActivationAndCompletion(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		for _, service := range []string{momServiceRequestAttributeTransportationTypeChange, momServiceRequestInteractionTransportationTypeChange} {
			t.Run(factory+"/"+service, func(t *testing.T) {
				runtime, conn, _ := integerProductionServer(t, t.TempDir(), 232)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				const fed = "production-mom-transport"
				module := momCompletionReviewModule(t, "Item")
				module.Xml = bytes.ReplaceAll(module.Xml, []byte("<transportation>HLAbestEffort</transportation>"), []byte("<transportation>HLAreliable</transportation>"))
				created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: factory, FomModules: []*rtiv1.FOMModule{module}})
				if err != nil {
					t.Fatal(err)
				}
				sender := momCompletionReviewJoin(ctx, t, conn, fed, "manager", "", created.FederationGeneration)
				target := momCompletionReviewJoin(ctx, t, conn, fed, "target", "", created.FederationGeneration)
				senderCtx := metadata.AppendToOutgoingContext(ctx, session.MetadataKey, string(sender.SessionToken))
				targetCtx := metadata.AppendToOutgoingContext(ctx, session.MetadataKey, string(target.SessionToken))
				model, err := runtime.foms.Get(ctx, fed)
				if err != nil {
					t.Fatal(err)
				}
				class, ok := model.LookupInteractionClass(mom.ServiceClassPrefix + "HLA" + service)
				if !ok {
					t.Fatal("MIM service missing")
				}
				decl := rtiv1.NewDeclarationServiceClient(conn)
				objects := rtiv1.NewObjectServiceClient(conn)
				if _, err := decl.PublishInteractionClass(senderCtx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: fed, FederateHandle: sender.FederateHandle, InteractionClassHandle: uint64(class)}); err != nil {
					t.Fatal(err)
				}
				params := map[uint64][]byte{}
				put := func(name string, value []byte) {
					h, ok := model.LookupParameter(class, name)
					if !ok {
						t.Fatalf("MIM parameter absent: %s", name)
					}
					params[uint64(h)] = value
				}
				put("HLAfederate", catalogRPCHandle(target.FederateHandle))
				var objectHandle, attributeHandle, interactionHandle uint64
				if service == momServiceRequestAttributeTransportationTypeChange {
					objectClass, ok := model.LookupObjectClass("Item")
					if !ok {
						t.Fatal("object class missing")
					}
					attribute, ok := model.LookupAttribute(objectClass, "Inherited")
					if !ok {
						t.Fatal("attribute missing")
					}
					attributeHandle = uint64(attribute)
					if _, err := decl.PublishObjectClassAttributes(targetCtx, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: target.FederateHandle, ObjectClassHandle: uint64(objectClass), AttributeHandles: []uint64{attributeHandle}}); err != nil {
						t.Fatal(err)
					}
					registered, err := objects.RegisterObjectInstance(targetCtx, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: fed, FederateHandle: target.FederateHandle, ObjectClassHandle: uint64(objectClass)})
					if err != nil {
						t.Fatal(err)
					}
					objectHandle = registered.ObjectHandle
					put("HLAobjectInstance", catalogRPCHandle(objectHandle))
					put("HLAattributeList", append(catalogRPCInt(1), catalogRPCHandle(attributeHandle)...))
				} else {
					interaction, ok := model.LookupInteractionClass("Event")
					if !ok {
						t.Fatal("interaction missing")
					}
					interactionHandle = uint64(interaction)
					if _, err := decl.PublishInteractionClass(targetCtx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: fed, FederateHandle: target.FederateHandle, InteractionClassHandle: interactionHandle}); err != nil {
						t.Fatal(err)
					}
					put("HLAinteractionClass", catalogRPCHandle(interactionHandle))
				}
				query := func() rtiv1.TransportationType {
					t.Helper()
					var result *rtiv1.TransportationTypeResponse
					var err error
					if objectHandle != 0 {
						result, err = objects.QueryAttributeTransportationType(targetCtx, &rtiv1.QueryAttributeTransportationTypeRequest{WireVersion: 1, FederationName: fed, FederateHandle: target.FederateHandle, ObjectHandle: objectHandle, AttributeHandle: attributeHandle})
					} else {
						result, err = objects.QueryInteractionTransportationType(targetCtx, &rtiv1.QueryInteractionTransportationTypeRequest{WireVersion: 1, FederationName: fed, FederateHandle: target.FederateHandle, TargetFederateHandle: target.FederateHandle, InteractionClassHandle: interactionHandle})
					}
					if err != nil {
						t.Fatal(err)
					}
					return result.TransportType
				}
				if query() != rtiv1.TransportationType(core.TransportTypeReliable) {
					t.Fatal("initial transportation is not reliable")
				}
				stream, err := rtiv1.NewStreamServiceClient(conn).EventBatches(targetCtx, &rtiv1.EventsRequest{WireVersion: 1, FederationName: fed, FederateHandle: target.FederateHandle})
				if err != nil {
					t.Fatal(err)
				}
				if ready, err := stream.Recv(); err != nil || !ready.Ready {
					t.Fatalf("ready=%v error=%v", ready, err)
				}
				var previousSequence uint64
				var previousTicket []byte
				for _, transport := range []string{bestEffortTransportationName, "HLAreliable"} {
					encoded, err := (encoding.HLAunicodeString{}).Encode(transport)
					if err != nil {
						t.Fatal(err)
					}
					put("HLAtransportation", encoded)
					before := query()
					if _, err := objects.SendInteraction(senderCtx, &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: fed, FederateHandle: sender.FederateHandle, InteractionClassHandle: uint64(class), Parameters: params}); err != nil {
						t.Fatal("actual production MOM request:", err)
					}
					var event *rtiv1.FederateEvent
					for event == nil {
						batch, err := stream.Recv()
						if err != nil {
							t.Fatal(err)
						}
						for _, candidate := range batch.Events {
							if candidate.GetTransportationChangeConfirmed() != nil {
								event = candidate
							}
						}
					}
					change := event.GetTransportationChangeConfirmed()
					if len(change.Ticket) != 32 || len(event.CallbackReceipt) != 32 {
						t.Fatal("required confirmation ticket/receipt missing")
					}
					if event.Seq <= previousSequence || bytes.Equal(change.Ticket, previousTicket) {
						t.Fatalf("confirmation order/ticket not distinct: previous=%d current=%d", previousSequence, event.Seq)
					}
					previousSequence, previousTicket = event.Seq, bytes.Clone(change.Ticket)
					if query() != before {
						t.Fatal("delivery activated transportation before callback entry")
					}
					activation := &rtiv1.ActivateTransportationTypeChangeRequest{WireVersion: 1, FederationName: fed, FederateHandle: target.FederateHandle, Ticket: change.Ticket, ExpectedFederationGeneration: change.FederationGeneration, ExpectedCallbackEpoch: change.CallbackEpoch}
					if _, err := objects.ActivateTransportationTypeChange(senderCtx, activation); status.Code(err) != codes.PermissionDenied {
						t.Fatalf("manager impersonated callback target: %v", err)
					}
					if query() != before {
						t.Fatal("unauthorized activation changed transportation")
					}
					report := &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: target.FederateHandle, CallbackReceipt: event.CallbackReceipt, InvocationEntry: true, Success: true}
					support := rtiv1.NewSupportServiceClient(conn)
					if _, err := support.ReportCallbackInvocation(targetCtx, report); err != nil {
						t.Fatal("callback entry:", err)
					}
					if _, err := objects.ActivateTransportationTypeChange(targetCtx, activation); err != nil {
						t.Fatal("target activation:", err)
					}
					if query() != change.TransportType {
						t.Fatal("entry activation did not change actual transport")
					}
					report.InvocationEntry = false
					for attempt := 0; attempt < 2; attempt++ {
						if _, err := support.ReportCallbackInvocation(targetCtx, report); err != nil {
							t.Fatal("completion retry:", err)
						}
					}
				}
			})
		}
	}
}
