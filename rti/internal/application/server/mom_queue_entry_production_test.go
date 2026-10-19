package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"reflect"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	ddmdomain "github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"google.golang.org/grpc/metadata"
)

// This oracle observes the production TCP boundary, not time-manager buffers.
func requireProductionQueueLengths(t *testing.T, f *conveyedProductionFixture, phase string, ro, tso uint32) {
	t.Helper()
	response, err := rtiv1.NewMomServiceClient(f.conn).QueryFederateAttributes(f.receiverCtx, &rtiv1.QueryFederateAttributesRequest{
		WireVersion: 1, FederationName: f.fed, FederateHandle: f.receiver.FederateHandle,
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]uint32{"HLAROlength": ro, "HLATSOlength": tso} {
		value := response.AttributeValues[name]
		if unavailable := response.UnavailableAttributes[name]; unavailable != "" {
			t.Errorf("%s: %s coverage unavailable: %s", phase, name, unavailable)
			continue
		}
		if len(value) != 4 {
			t.Errorf("%s: %s invalid encoding %x", phase, name, value)
			continue
		}
		if got := binary.BigEndian.Uint32(value); got != want {
			t.Errorf("%s: %s=%d want %d (count until authenticated invocation ENTRY)", phase, name, got, want)
		}
	}
}

func TestMOMProductionDataQueueIdenticalMessagesAndMultipleRecipients(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		t.Run(factory, func(t *testing.T) {
			f := newConveyedProductionFixture(t, factory)
			second := *f
			second.receiver, second.receiverCtx = f.foreign, f.foreignCtx
			if _, err := rtiv1.NewDeclarationServiceClient(f.conn).SubscribeInteractionClass(second.receiverCtx,
				&rtiv1.SubInterRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: second.receiver.FederateHandle, InteractionClassHandle: f.class}); err != nil {
				t.Fatal(err)
			}
			if _, err := rtiv1.NewTimeServiceClient(f.conn).EnableAsynchronousDelivery(second.receiverCtx,
				&rtiv1.EnableAsynchronousDeliveryRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: second.receiver.FederateHandle}); err != nil {
				t.Fatal(err)
			}
			f.send(t)
			f.send(t)
			requireProductionQueueLengths(t, f, "first recipient raw ready", 2, 0)
			requireProductionQueueLengths(t, &second, "second recipient raw ready", 2, 0)
			for _, receiver := range []*conveyedProductionFixture{f, &second} {
				stream, cancel := receiver.stream(t)
				defer cancel()
				messages := queueProductionReceiveMany(t, stream, 2)
				first, last := messages[0], messages[1]
				if bytes.Equal(first.CallbackReceipt, last.CallbackReceipt) || len(first.CallbackReceipt) != 32 || len(last.CallbackReceipt) != 32 {
					t.Fatal("separate identical messages did not receive separate identities")
				}
				if !reflect.DeepEqual(first.GetReceive().Parameters, last.GetReceive().Parameters) {
					t.Fatal("fixture payloads were not identical")
				}
				requireProductionQueueLengths(t, receiver, "both received but not entered", 2, 0)
				receiver.report(t, first, true)
				requireProductionQueueLengths(t, receiver, "one entered", 1, 0)
				receiver.report(t, first, true)
				receiver.report(t, first, false)
				requireProductionQueueLengths(t, receiver, "duplicate entry/completion", 1, 0)
				receiver.report(t, last, true)
				receiver.report(t, last, false)
				requireProductionQueueLengths(t, receiver, "both entered", 0, 0)
			}
		})
	}
}

func TestMOMProductionDataQueueReconnectDoesNotAddStageCopies(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		t.Run(factory, func(t *testing.T) {
			f := newConveyedProductionFixture(t, factory)
			stream, cancel := f.stream(t)
			f.send(t)
			event := conveyedProductionReceive(t, stream)
			requireProductionQueueLengths(t, f, "before disconnect", 1, 0)
			f.cancelAndDrainStream(t, stream, cancel)
			requireProductionQueueLengths(t, f, "disconnected client", 1, 0)
			stream, cancel = f.stream(t)
			defer cancel()
			replayed := conveyedProductionReceive(t, stream)
			if !bytes.Equal(event.CallbackReceipt, replayed.CallbackReceipt) {
				t.Fatal("reconnect created a second delivery identity")
			}
			requireProductionQueueLengths(t, f, "same opaque ticket replayed", 1, 0)
			f.report(t, replayed, true)
			f.report(t, event, true)
			f.report(t, replayed, false)
			requireProductionQueueLengths(t, f, "duplicate copy entry/completion", 0, 0)
		})
	}
}

func queueProductionTimestampedInteraction(t *testing.T, f *conveyedProductionFixture, factory string, retraction uint64) {
	t.Helper()
	if retraction != 0 {
		request := &rtiv1.EnableRegulationRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.producer.FederateHandle}
		if factory == integer64TimeName {
			request.LookaheadValue = integerWireTime(t, 0)
		}
		if _, err := rtiv1.NewTimeServiceClient(f.conn).EnableTimeRegulation(f.producerCtx, request); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := rtiv1.NewTimeServiceClient(f.conn).EnableTimeConstrained(f.receiverCtx,
		&rtiv1.EnableConstrainedRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.receiver.FederateHandle}); err != nil {
		t.Fatal(err)
	}
	request := &rtiv1.SendInteractionWithRegionsRequest{WireVersion: 1, FederationName: f.fed,
		FederateHandle: f.producer.FederateHandle, InteractionClassHandle: f.class,
		Parameters: map[uint64][]byte{f.parameter: {0, 0, 0, 1}}, RegionHandles: []uint64{f.publisher}, MessageRetractionHandle: retraction}
	if factory == integer64TimeName {
		request.LogicalTimeValue = integerWireTime(t, 5)
	} else {
		stamp := float64(5)
		request.LogicalTime = &stamp
	}
	if _, err := rtiv1.NewDDMServiceClient(f.conn).SendInteractionWithRegions(f.producerCtx, request); err != nil {
		t.Fatal(err)
	}
}

func queueProductionReceiveMany(t *testing.T, stream rtiv1.StreamService_EventBatchesClient, count int) []*rtiv1.FederateEvent {
	t.Helper()
	var result []*rtiv1.FederateEvent
	for len(result) < count {
		batch, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range batch.Events {
			if event.GetReceive() != nil {
				result = append(result, event)
			}
		}
	}
	if len(result) != count {
		t.Fatalf("received %d data messages, want %d", len(result), count)
	}
	return result
}

func queueProductionAdvance(t *testing.T, f *conveyedProductionFixture, factory string) {
	t.Helper()
	request := &rtiv1.TARRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.receiver.FederateHandle}
	if factory == integer64TimeName {
		request.LogicalTimeValue = integerWireTime(t, 5)
	} else {
		request.LogicalTime = 5
	}
	if _, err := rtiv1.NewTimeServiceClient(f.conn).TimeAdvanceRequest(f.receiverCtx, request); err != nil {
		t.Fatal(err)
	}
}

func TestMOMProductionDataQueueDiscardRetractionAndInterest(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		for _, retract := range []bool{false, true} {
			name := factory + "/filter"
			if retract {
				name = factory + "/retract"
			}
			t.Run(name, func(t *testing.T) {
				f := newConveyedProductionFixture(t, factory)
				queueProductionTimestampedInteraction(t, f, factory, 17)
				requireProductionQueueLengths(t, f, "waiting data", 0, 1)
				if retract {
					if _, err := rtiv1.NewObjectServiceClient(f.conn).Retract(f.producerCtx,
						&rtiv1.RetractRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.producer.FederateHandle, MessageRetractionHandle: 17}); err != nil {
						t.Fatal(err)
					}
				} else {
					var regions []uint64
					state := f.runtime.ddmMgr.(*ddmdomain.Manager).ExportState(core.FederationName(f.fed))
					for _, subscription := range state.InteractionSubscriptions {
						if uint64(subscription.Subscriber) == f.receiver.FederateHandle && uint64(subscription.InteractionClass) == f.class {
							for _, region := range subscription.Regions {
								regions = append(regions, uint64(region))
							}
						}
					}
					if len(regions) == 0 {
						t.Fatal("fixture has no regional interest to remove")
					}
					if _, err := rtiv1.NewDDMServiceClient(f.conn).UnsubscribeInteractionClassWithRegions(f.receiverCtx,
						&rtiv1.UnsubscribeICWithRegionsRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.receiver.FederateHandle, InteractionClassHandle: f.class, RegionHandles: regions}); err != nil {
						t.Fatal(err)
					}
				}
				producerAdvance := &rtiv1.TARRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.producer.FederateHandle}
				if factory == integer64TimeName {
					producerAdvance.LogicalTimeValue = integerWireTime(t, 10)
				} else {
					producerAdvance.LogicalTime = 10
				}
				if _, err := rtiv1.NewTimeServiceClient(f.conn).TimeAdvanceRequest(f.producerCtx, producerAdvance); err != nil {
					t.Fatal(err)
				}
				queueProductionAdvance(t, f, factory)
				requireProductionQueueLengths(t, f, "discarded before ready admission", 0, 0)
			})
		}
	}
}

func TestMOMProductionDataQueueRestoreRebindsWaitingAndReadyEvidence(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		t.Run(factory, func(t *testing.T) {
			f := newConveyedProductionFixture(t, factory)
			stream, cancel := f.stream(t)
			defer cancel()
			queueProductionTimestampedInteraction(t, f, factory, 0)
			requireProductionQueueLengths(t, f, "before save", 0, 1)
			saves := rtiv1.NewSavepointServiceClient(f.conn)
			if _, err := saves.RequestFederationSave(f.producerCtx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.producer.FederateHandle, Label: "queue-evidence"}); err != nil {
				t.Fatal(err)
			}
			members := []struct {
				handle uint64
				ctx    context.Context
			}{{f.producer.FederateHandle, f.producerCtx}, {f.receiver.FederateHandle, f.receiverCtx}, {f.foreign.FederateHandle, f.foreignCtx}}
			for _, member := range members {
				request := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: member.handle}
				if _, err := saves.FederateSaveBegun(member.ctx, request); err != nil {
					t.Fatal(err)
				}
				if _, err := saves.FederateSaveComplete(member.ctx, request); err != nil {
					t.Fatal(err)
				}
			}
			queueProductionAdvance(t, f, factory)
			old := conveyedProductionReceive(t, stream)
			f.report(t, old, true)
			f.report(t, old, false)
			requireProductionQueueLengths(t, f, "post-save original entered", 0, 0)
			if _, err := saves.RequestFederationRestore(f.producerCtx, &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.producer.FederateHandle, Label: "queue-evidence"}); err != nil {
				t.Fatal(err)
			}
			for _, member := range members {
				if _, err := saves.FederateRestoreComplete(member.ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: member.handle}); err != nil {
					t.Fatal(err)
				}
			}
			for {
				batch, err := stream.Recv()
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, event := range batch.Events {
					if event.GetRestoreCompleted() != nil {
						f.receiver.FederateHandle = event.GetRestoreCompleted().GetFederateHandle()
						found = true
					}
				}
				if found {
					break
				}
			}
			requireProductionQueueLengths(t, f, "restored waiting data", 0, 1)
			queueProductionAdvance(t, f, factory)
			restored := conveyedProductionReceive(t, stream)
			if len(restored.CallbackReceipt) != 32 || bytes.Equal(old.CallbackReceipt, restored.CallbackReceipt) {
				t.Fatal("restore reused old ready receipt instead of new admission")
			}
			requireProductionQueueLengths(t, f, "restored ready data", 0, 1)
			f.report(t, restored, true)
			f.report(t, restored, false)
			requireProductionQueueLengths(t, f, "restored callback entered", 0, 0)
		})
	}
}

func TestMOMProductionDataQueueReflectAndRemoveExcludeDiscovery(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		t.Run(factory, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			srv, conn, _ := integerProductionServer(t, t.TempDir(), 253)
			const fed = "object-queue-evidence"
			created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx,
				&rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed,
					LogicalTimeImplementationName: factory, FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")}})
			if err != nil {
				t.Fatal(err)
			}
			producer := momCompletionReviewJoin(ctx, t, conn, fed, "producer", "", created.FederationGeneration)
			receiver := momCompletionReviewJoin(ctx, t, conn, fed, "receiver", "", created.FederationGeneration)
			producerCtx := metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(producer.SessionToken))
			receiverCtx := metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(receiver.SessionToken))
			f := &conveyedProductionFixture{ctx: ctx, runtime: srv, conn: conn, fed: fed,
				generation: created.FederationGeneration, producer: producer, receiver: receiver,
				producerCtx: producerCtx, receiverCtx: receiverCtx}
			model, err := srv.foms.Get(ctx, fed)
			if err != nil {
				t.Fatal(err)
			}
			class, ok := model.LookupObjectClass("HLAobjectRoot.Item.Leaf")
			if !ok {
				t.Fatal("fixture object class absent")
			}
			attribute, ok := model.LookupAttribute(class, "Local")
			if !ok {
				t.Fatal("fixture attribute absent")
			}
			decl := rtiv1.NewDeclarationServiceClient(conn)
			if _, err := decl.PublishObjectClassAttributes(producerCtx, &rtiv1.PubObjAttrsRequest{
				WireVersion: 1, FederationName: fed, FederateHandle: producer.FederateHandle,
				ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attribute)}}); err != nil {
				t.Fatal(err)
			}
			if _, err := decl.SubscribeObjectClassAttributes(receiverCtx, &rtiv1.SubObjAttrsRequest{
				WireVersion: 1, FederationName: fed, FederateHandle: receiver.FederateHandle,
				ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attribute)}}); err != nil {
				t.Fatal(err)
			}
			if _, err := rtiv1.NewTimeServiceClient(conn).EnableAsynchronousDelivery(receiverCtx,
				&rtiv1.EnableAsynchronousDeliveryRequest{WireVersion: 1, FederationName: fed, FederateHandle: receiver.FederateHandle}); err != nil {
				t.Fatal(err)
			}
			stream, stop := f.stream(t)
			defer stop()
			objects := rtiv1.NewObjectServiceClient(conn)
			registered, err := objects.RegisterObjectInstance(producerCtx, &rtiv1.RegisterObjectRequest{
				WireVersion: 1, FederationName: fed, FederateHandle: producer.FederateHandle, ObjectClassHandle: uint64(class)})
			if err != nil {
				t.Fatal(err)
			}
			next := func(kind string) *rtiv1.FederateEvent {
				t.Helper()
				for {
					batch, err := stream.Recv()
					if err != nil {
						t.Fatal(err)
					}
					for _, event := range batch.Events {
						if kind == "discover" && event.GetDiscover() != nil || kind == "reflect" && event.GetReflect() != nil || kind == "remove" && event.GetRemove() != nil {
							return event
						}
					}
				}
			}
			next("discover")
			requireProductionQueueLengths(t, f, "discovery is not data", 0, 0)
			if _, err := objects.UpdateAttributeValues(producerCtx, &rtiv1.UpdateAttributeValuesRequest{
				WireVersion: 1, FederationName: fed, FederateHandle: producer.FederateHandle,
				ObjectHandle: registered.ObjectHandle, Attributes: map[uint64][]byte{uint64(attribute): {0, 0, 0, 1}}}); err != nil {
				t.Fatal(err)
			}
			reflected := next("reflect")
			if len(reflected.CallbackReceipt) != 32 {
				t.Fatal("reflect lacks mandatory data receipt")
			}
			requireProductionQueueLengths(t, f, "reflection before entry", 1, 0)
			f.report(t, reflected, true)
			f.report(t, reflected, false)
			requireProductionQueueLengths(t, f, "reflection entered", 0, 0)
			if _, err := objects.DeleteObjectInstance(producerCtx, &rtiv1.DeleteObjectInstanceRequest{
				WireVersion: 1, FederationName: fed, FederateHandle: producer.FederateHandle, ObjectHandle: registered.ObjectHandle}); err != nil {
				t.Fatal(err)
			}
			removed := next("remove")
			if len(removed.CallbackReceipt) != 32 {
				t.Fatal("remove lacks mandatory data receipt")
			}
			requireProductionQueueLengths(t, f, "removal before entry", 1, 0)
			f.report(t, removed, true)
			f.report(t, removed, false)
			requireProductionQueueLengths(t, f, "removal entered", 0, 0)
		})
	}
}

func TestMOMProductionQueueLengthRetainsDataUntilCallbackEntry(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		for _, timestamped := range []bool{false, true} {
			name := factory + "/RO"
			if timestamped {
				name = factory + "/TSO"
			}
			t.Run(name, func(t *testing.T) {
				f := newConveyedProductionFixture(t, factory)
				times := rtiv1.NewTimeServiceClient(f.conn)
				if timestamped {
					if _, err := times.EnableTimeConstrained(f.receiverCtx, &rtiv1.EnableConstrainedRequest{
						WireVersion: 1, FederationName: f.fed, FederateHandle: f.receiver.FederateHandle,
					}); err != nil {
						t.Fatal(err)
					}
				}
				request := &rtiv1.SendInteractionWithRegionsRequest{
					WireVersion: 1, FederationName: f.fed, FederateHandle: f.producer.FederateHandle,
					InteractionClassHandle: f.class, Parameters: map[uint64][]byte{f.parameter: {0, 0, 0, 1}},
					RegionHandles: []uint64{f.publisher},
				}
				var ro, tso uint32 = 1, 0
				if timestamped {
					ro, tso = 0, 1
					if factory == integer64TimeName {
						request.LogicalTimeValue = integerWireTime(t, 5)
					} else {
						stamp := float64(5)
						request.LogicalTime = &stamp
					}
				}
				if _, err := rtiv1.NewDDMServiceClient(f.conn).SendInteractionWithRegions(f.producerCtx, request); err != nil {
					t.Fatal(err)
				}
				requireProductionQueueLengths(t, f, "committed before stream", ro, tso)
				stream, cancel := f.stream(t)
				defer cancel()
				if timestamped {
					advance := &rtiv1.TARRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.receiver.FederateHandle}
					if factory == integer64TimeName {
						advance.LogicalTimeValue = integerWireTime(t, 5)
					} else {
						advance.LogicalTime = 5
					}
					if _, err := times.TimeAdvanceRequest(f.receiverCtx, advance); err != nil {
						t.Fatal(err)
					}
				}
				event := conveyedProductionReceive(t, stream)
				if len(event.CallbackReceipt) != 32 {
					t.Fatal("data callback lacks mandatory opaque ENTRY identity")
				}
				order := rtiv1.OrderType_ORDER_TYPE_RECEIVE
				if timestamped {
					order = rtiv1.OrderType_ORDER_TYPE_TIMESTAMP
				}
				if event.GetReceive().GetMetadata().GetReceivedOrder() != order {
					t.Fatal("test did not establish the intended RECEIVED order")
				}
				requireProductionQueueLengths(t, f, "wire received but body not entered", ro, tso)
				if _, err := rtiv1.NewSupportServiceClient(f.conn).ReportCallbackInvocation(f.foreignCtx,
					&rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: f.fed,
						FederateHandle: f.receiver.FederateHandle, CallbackReceipt: event.CallbackReceipt,
						InvocationEntry: true, Success: true}); err == nil {
					t.Fatal("foreign session entered recipient callback")
				}
				requireProductionQueueLengths(t, f, "rejected foreign entry", ro, tso)
				f.report(t, event, true)
				requireProductionQueueLengths(t, f, "authenticated entry", 0, 0)
				f.report(t, event, true)
				requireProductionQueueLengths(t, f, "idempotent duplicate entry", 0, 0)
				f.report(t, event, false)
				requireProductionQueueLengths(t, f, "completion does not decrement twice", 0, 0)
			})
		}
	}
}
