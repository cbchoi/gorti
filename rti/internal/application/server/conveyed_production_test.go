package server

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/pkg/federate"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const conveyedProductionFOM = `<objectModel><interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Signal</name><dimensions><dimension>X</dimension></dimensions><parameter><name>value</name><dataType>HLAinteger32BE</dataType></parameter></interactionClass></interactionClass></interactions><dimensions><dimension><name>X</name><upperBound>18446744073709551615</upperBound></dimension></dimensions></objectModel>`

func TestConveyedProductionTCPGoSDKQueryLifetime(t *testing.T) {
	for _, implementation := range []string{float64TimeName, integer64TimeName} {
		t.Run(implementation, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			srv, conn, _ := integerProductionServer(t, t.TempDir(), 251)
			fed := "conveyed-sdk"
			sdk := federate.WrapGRPCClientConn(conn)
			spec := federate.FederationSpec{Name: fed, LogicalTimeImplementationName: implementation, FOMModules: []federate.FOMModule{{Path: "copied.xml", XML: []byte(conveyedProductionFOM)}}}
			receiver, err := sdk.JoinFederation(ctx, spec, "receiver")
			if err != nil {
				t.Fatal(err)
			}
			generation, _ := srv.fedMgr.GenerationFor(core.FederationName(fed))
			producer := momCompletionReviewJoin(ctx, t, conn, fed, "producer", "", generation)
			foreign := momCompletionReviewJoin(ctx, t, conn, fed, "foreign", "", generation)
			foreignCtx := metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(foreign.SessionToken))
			model, err := srv.foms.Get(ctx, core.FederationName(fed))
			if err != nil {
				t.Fatal(err)
			}
			cls, _ := model.LookupInteractionClass("Signal")
			param, _ := model.LookupParameter(cls, "value")
			dim, _ := model.(core.FOMHandleNameLookup).LookupDimension("X")
			regions := rtiv1.NewDDMServiceClient(conn)
			create := func(h uint64) uint64 {
				t.Helper()
				r, err := regions.CreateRegion(ctx, &rtiv1.CreateRegionRequest{WireVersion: 1, FederationName: fed, FederateHandle: h, RoutingSpaceHandle: 1, DimensionHandles: []uint64{uint64(dim)}})
				if err != nil {
					t.Fatal(err)
				}
				_, err = regions.SetRangeBounds(ctx, &rtiv1.SetRangeBoundsRequest{WireVersion: 1, FederationName: fed, FederateHandle: h, RegionHandle: r.RegionHandle, DimensionHandle: uint64(dim), Bounds: &rtiv1.Range{Lower: 1<<53 + 1, Upper: ^uint64(0)}})
				if err != nil {
					t.Fatal(err)
				}
				_, err = regions.CommitRegionModifications(ctx, &rtiv1.CommitRegionRequest{WireVersion: 1, FederationName: fed, FederateHandle: h, RegionHandles: []uint64{r.RegionHandle}})
				if err != nil {
					t.Fatal(err)
				}
				return r.RegionHandle
			}
			pub, sub := create(producer.FederateHandle), create(receiver.CurrentHandle())
			if _, err := rtiv1.NewDeclarationServiceClient(conn).PublishInteractionClass(ctx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: fed, FederateHandle: producer.FederateHandle, InteractionClassHandle: uint64(cls)}); err != nil {
				t.Fatal(err)
			}
			if err := receiver.SubscribeInteractionClassWithRegions(ctx, uint64(cls), []uint64{sub}); err != nil {
				t.Fatal(err)
			}
			if err := receiver.EnableAsynchronousDelivery(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := regions.SendInteractionWithRegions(ctx, &rtiv1.SendInteractionWithRegionsRequest{WireVersion: 1, FederationName: fed, FederateHandle: producer.FederateHandle, InteractionClassHandle: uint64(cls), Parameters: map[uint64][]byte{uint64(param): {0, 0, 0, 1}}, RegionHandles: []uint64{pub}}); err != nil {
				t.Fatal(err)
			}
			var received federate.ReceiveInteraction
			for received.Metadata == nil {
				select {
				case event, open := <-receiver.Events():
					if !open {
						t.Fatal("SDK callback stream closed")
					}
					if value, ok := event.(federate.ReceiveInteraction); ok {
						received = value
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if len(received.CallbackReceipt()) != 32 || len(received.Metadata.ReceivedRegions) != 1 {
				t.Fatalf("missing mandatory copied-region metadata: %+v", received)
			}
			regionCopy := received.Metadata.ReceivedRegions[0]
			if regionCopy == pub {
				t.Fatal("SDK received mutable producer region")
			}
			query := &rtiv1.QueryBoundsRequest{WireVersion: 1, FederationName: fed, RegionHandle: regionCopy, DimensionHandle: uint64(dim)}
			if _, err := regions.QueryBounds(ctx, query); status.Code(err) != codes.Unauthenticated {
				t.Fatalf("missing query token: %v", err)
			}
			if _, err := regions.QueryBounds(foreignCtx, query); err == nil {
				t.Fatal("foreign receiver queried copied region")
			}
			if _, _, _, err := receiver.QueryBounds(ctx, regionCopy, uint64(dim)); err == nil {
				t.Fatal("query before callback entry succeeded")
			}
			if err := receiver.ReportCallbackEntry(ctx, received); err != nil {
				t.Fatal(err)
			}
			lower, upper, found, err := receiver.QueryBounds(ctx, regionCopy, uint64(dim))
			if err != nil || !found || lower != 1<<53+1 || upper != ^uint64(0) {
				t.Fatalf("SDK active exact copy query: %d %d %v %v", lower, upper, found, err)
			}
			if dims, err := receiver.GetDimensionHandleSet(ctx, regionCopy); err != nil || len(dims) != 1 || dims[0] != uint64(dim) {
				t.Fatalf("SDK copied dimensions: %v %v", dims, err)
			}
			if _, err := regions.SetRangeBounds(ctx, &rtiv1.SetRangeBoundsRequest{WireVersion: 1, FederationName: fed, FederateHandle: producer.FederateHandle, RegionHandle: pub, DimensionHandle: uint64(dim), Bounds: &rtiv1.Range{Lower: 10, Upper: 20}}); err != nil {
				t.Fatal(err)
			}
			if _, err := regions.CommitRegionModifications(ctx, &rtiv1.CommitRegionRequest{WireVersion: 1, FederationName: fed, FederateHandle: producer.FederateHandle, RegionHandles: []uint64{pub}}); err != nil {
				t.Fatal(err)
			}
			if low, high, _, err := receiver.QueryBounds(ctx, regionCopy, uint64(dim)); err != nil || low != lower || high != upper {
				t.Fatalf("sender mutation changed callback realization: %v", err)
			}
			if _, _, _, err := receiver.QueryBounds(ctx, pub, uint64(dim)); err == nil {
				t.Fatal("receiver queried foreign mutable specification")
			}
			if err := receiver.ReportCallbackInvocation(ctx, received, true, ""); err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := receiver.QueryBounds(ctx, regionCopy, uint64(dim)); err == nil {
				t.Fatal("completed callback retained query authority")
			}
			if err := receiver.ReportCallbackInvocation(ctx, received, true, ""); err != nil {
				t.Fatal(fmt.Errorf("duplicate completion: %w", err))
			}
		})
	}
}

type conveyedProductionFixture struct {
	ctx                                    context.Context
	runtime                                *rtid
	conn                                   *grpcgo.ClientConn
	fed                                    string
	generation                             uint64
	producer, receiver, foreign            *rtiv1.JoinFederationResponse
	producerCtx, receiverCtx, foreignCtx   context.Context
	class, parameter, dimension, publisher uint64
}

func newConveyedProductionFixture(t *testing.T, implementation string) *conveyedProductionFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	srv, conn, _ := integerProductionServer(t, t.TempDir(), 252)
	f := &conveyedProductionFixture{ctx: ctx, runtime: srv, conn: conn, fed: "conveyed-wire"}
	created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: f.fed, LogicalTimeImplementationName: implementation, FomModules: []*rtiv1.FOMModule{{Path: "copied.xml", Xml: []byte(conveyedProductionFOM)}}})
	if err != nil {
		t.Fatal(err)
	}
	f.generation = created.FederationGeneration
	f.producer = momCompletionReviewJoin(ctx, t, conn, f.fed, "producer", "", f.generation)
	f.receiver = momCompletionReviewJoin(ctx, t, conn, f.fed, "receiver", "", f.generation)
	f.foreign = momCompletionReviewJoin(ctx, t, conn, f.fed, "foreign", "", f.generation)
	f.producerCtx = metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(f.producer.SessionToken))
	f.receiverCtx = metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(f.receiver.SessionToken))
	f.foreignCtx = metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(f.foreign.SessionToken))
	model, err := srv.foms.Get(ctx, core.FederationName(f.fed))
	if err != nil {
		t.Fatal(err)
	}
	class, _ := model.LookupInteractionClass("Signal")
	parameter, _ := model.LookupParameter(class, "value")
	dimension, _ := model.(core.FOMHandleNameLookup).LookupDimension("X")
	f.class, f.parameter, f.dimension = uint64(class), uint64(parameter), uint64(dimension)
	regions := rtiv1.NewDDMServiceClient(conn)
	create := func(h uint64) uint64 {
		t.Helper()
		r, err := regions.CreateRegion(ctx, &rtiv1.CreateRegionRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: h, RoutingSpaceHandle: 1, DimensionHandles: []uint64{f.dimension}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := regions.SetRangeBounds(ctx, &rtiv1.SetRangeBoundsRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: h, RegionHandle: r.RegionHandle, DimensionHandle: f.dimension, Bounds: &rtiv1.Range{Lower: 1<<53 + 1, Upper: ^uint64(0)}}); err != nil {
			t.Fatal(err)
		}
		if _, err := regions.CommitRegionModifications(ctx, &rtiv1.CommitRegionRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: h, RegionHandles: []uint64{r.RegionHandle}}); err != nil {
			t.Fatal(err)
		}
		return r.RegionHandle
	}
	f.publisher = create(f.producer.FederateHandle)
	sub := create(f.receiver.FederateHandle)
	if _, err := rtiv1.NewDeclarationServiceClient(conn).PublishInteractionClass(ctx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.producer.FederateHandle, InteractionClassHandle: f.class}); err != nil {
		t.Fatal(err)
	}
	if _, err := regions.SubscribeInteractionClassWithRegions(ctx, &rtiv1.SubscribeICWithRegionsRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.receiver.FederateHandle, InteractionClassHandle: f.class, RegionHandles: []uint64{sub}}); err != nil {
		t.Fatal(err)
	}
	if _, err := rtiv1.NewTimeServiceClient(conn).EnableAsynchronousDelivery(ctx, &rtiv1.EnableAsynchronousDeliveryRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.receiver.FederateHandle}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *conveyedProductionFixture) stream(t *testing.T) (rtiv1.StreamService_EventBatchesClient, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(f.receiverCtx)
	stream, err := rtiv1.NewStreamServiceClient(f.conn).EventBatches(ctx, &rtiv1.EventsRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.receiver.FederateHandle, ExpectedFederationGeneration: &f.generation})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if ready, err := stream.Recv(); err != nil || !ready.GetReady() {
		cancel()
		t.Fatal("stream readiness", ready, err)
	}
	return stream, cancel
}

func (f *conveyedProductionFixture) cancelAndDrainStream(t *testing.T, stream rtiv1.StreamService_EventBatchesClient, cancel context.CancelFunc) {
	t.Helper()
	cancel()
	for {
		if _, err := stream.Recv(); err != nil {
			if status.Code(err) != codes.Canceled {
				t.Fatalf("cancelled stream terminal status: %v", err)
			}
			break
		}
	}
	// The handler marks receipts retryable before removing its subscriber.
	// Client cancellation alone does not acknowledge either server-side step.
	for {
		attached := false
		for _, row := range f.runtime.outbox.OutboxStats() {
			if row.Federation == core.FederationName(f.fed) && row.Handle == core.FederateHandle(f.receiver.FederateHandle) {
				attached = true
				break
			}
		}
		if !attached {
			return
		}
		if err := f.ctx.Err(); err != nil {
			t.Fatalf("cancelled stream did not release its subscriber: %v", err)
		}
		runtime.Gosched()
	}
}

func (f *conveyedProductionFixture) send(t *testing.T) {
	t.Helper()
	if _, err := rtiv1.NewDDMServiceClient(f.conn).SendInteractionWithRegions(f.producerCtx, &rtiv1.SendInteractionWithRegionsRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.producer.FederateHandle, InteractionClassHandle: f.class, Parameters: map[uint64][]byte{f.parameter: {0, 0, 0, 1}}, RegionHandles: []uint64{f.publisher}}); err != nil {
		t.Fatal(err)
	}
}

func conveyedProductionReceive(t *testing.T, stream rtiv1.StreamService_EventBatchesClient) *rtiv1.FederateEvent {
	t.Helper()
	for {
		batch, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range batch.Events {
			if event.GetReceive() != nil {
				return event
			}
		}
	}
}

func (f *conveyedProductionFixture) report(t *testing.T, event *rtiv1.FederateEvent, entry bool) {
	t.Helper()
	if _, err := rtiv1.NewSupportServiceClient(f.conn).ReportCallbackInvocation(f.receiverCtx, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.receiver.FederateHandle, CallbackReceipt: event.CallbackReceipt, InvocationEntry: entry, Success: true}); err != nil {
		t.Fatal(err)
	}
}

func TestConveyedProductionTCPDimensionsRequireAuthenticatedReceiver(t *testing.T) {
	for _, implementation := range []string{float64TimeName, integer64TimeName} {
		t.Run(implementation, func(t *testing.T) {
			f := newConveyedProductionFixture(t, implementation)
			stream, cancel := f.stream(t)
			defer cancel()
			f.send(t)
			event := conveyedProductionReceive(t, stream)
			f.report(t, event, true)
			regionCopy := event.GetReceive().GetMetadata().GetReceivedRegions().RegionHandles[0]
			req := &rtiv1.GetDimensionHandleSetRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.receiver.FederateHandle, RegionHandle: regionCopy}
			client := rtiv1.NewSupportServiceClient(f.conn)
			if _, err := client.GetDimensionHandleSet(f.ctx, req); status.Code(err) != codes.Unauthenticated {
				t.Fatalf("missing dimension query token: %v", err)
			}
			if _, err := client.GetDimensionHandleSet(f.foreignCtx, req); err == nil {
				t.Fatal("foreign session forged receiver identity")
			}
			if dims, err := client.GetDimensionHandleSet(f.receiverCtx, req); err != nil || len(dims.DimensionHandles) != 1 || dims.DimensionHandles[0] != f.dimension {
				t.Fatalf("authenticated dimensions: %v %v", dims, err)
			}
			f.report(t, event, false)
		})
	}
}

func TestConveyedProductionTCPCompletedDeliveryReclaimsCache(t *testing.T) {
	for _, implementation := range []string{float64TimeName, integer64TimeName} {
		t.Run(implementation, func(t *testing.T) {
			f := newConveyedProductionFixture(t, implementation)
			stream, cancel := f.stream(t)
			defer cancel()
			manager := f.runtime.ddmMgr.(interface {
				ExportState(core.FederationName) *ddm.ManagerState
			})
			var previous uint64
			for i := 0; i < 100; i++ {
				f.send(t)
				event := conveyedProductionReceive(t, stream)
				handles := event.GetReceive().GetMetadata().GetReceivedRegions().GetRegionHandles()
				if len(event.CallbackReceipt) != 32 || len(handles) != 1 || handles[0] <= previous || handles[0] == f.publisher {
					t.Fatalf("callback %d identity: %v", i, event)
				}
				previous = handles[0]
				f.report(t, event, true)
				bounds, err := rtiv1.NewDDMServiceClient(f.conn).QueryBounds(f.receiverCtx, &rtiv1.QueryBoundsRequest{WireVersion: 1,
					FederationName: f.fed, RegionHandle: handles[0], DimensionHandle: f.dimension})
				if err != nil || !bounds.GetFound() || bounds.GetBounds().GetLower() != 1<<53+1 || bounds.GetBounds().GetUpper() != ^uint64(0) {
					t.Fatalf("active copied bounds %d: %v %v", i, bounds, err)
				}
				f.report(t, event, false)
				if state := manager.ExportState(core.FederationName(f.fed)); len(state.ConveyedRegions) != 0 {
					t.Fatalf("completed callback %d retained cache: %+v", i, state.ConveyedRegions)
				}
			}
		})
	}
}

func TestConveyedProductionTCPReconnectPreservesPendingCopy(t *testing.T) {
	for _, implementation := range []string{float64TimeName, integer64TimeName} {
		t.Run(implementation, func(t *testing.T) {
			f := newConveyedProductionFixture(t, implementation)
			stream, cancel := f.stream(t)
			defer func() { cancel() }()
			f.send(t)
			event := conveyedProductionReceive(t, stream)
			regionCopy := event.GetReceive().GetMetadata().GetReceivedRegions().RegionHandles[0]
			query := &rtiv1.QueryBoundsRequest{WireVersion: 1, FederationName: f.fed, RegionHandle: regionCopy, DimensionHandle: f.dimension}
			for cycle := 0; cycle < 10; cycle++ {
				f.cancelAndDrainStream(t, stream, cancel)
				stream, cancel = f.stream(t)
				retried := conveyedProductionReceive(t, stream)
				if string(retried.CallbackReceipt) != string(event.CallbackReceipt) || retried.GetReceive().GetMetadata().GetReceivedRegions().RegionHandles[0] != regionCopy {
					t.Fatalf("reconnect %d changed callback identity or copied designator", cycle)
				}
				if _, err := rtiv1.NewDDMServiceClient(f.conn).QueryBounds(f.receiverCtx, query); err == nil {
					t.Fatal("pending reconnected callback obtained query authority before entry")
				}
			}
			f.report(t, event, true)
			if got, err := rtiv1.NewDDMServiceClient(f.conn).QueryBounds(f.receiverCtx, query); err != nil || got.GetBounds().Lower != 1<<53+1 || got.GetBounds().Upper != ^uint64(0) {
				t.Fatalf("reconnected copied query: %v %v", got, err)
			}
			f.report(t, event, false)
			if _, err := rtiv1.NewDDMServiceClient(f.conn).QueryBounds(f.receiverCtx, query); err == nil {
				t.Fatal("completed reconnected query succeeded")
			}
		})
	}
}

func TestConveyedProductionTCPQueuedSaveRestoreReissuesReceipt(t *testing.T) {
	for _, implementation := range []string{float64TimeName, integer64TimeName} {
		t.Run(implementation, func(t *testing.T) {
			f := newConveyedProductionFixture(t, implementation)
			stream, cancel := f.stream(t)
			defer cancel()
			times := rtiv1.NewTimeServiceClient(f.conn)
			if _, err := times.EnableTimeConstrained(f.receiverCtx, &rtiv1.EnableConstrainedRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.receiver.FederateHandle}); err != nil {
				t.Fatal(err)
			}
			request := &rtiv1.SendInteractionWithRegionsRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.producer.FederateHandle, InteractionClassHandle: f.class, Parameters: map[uint64][]byte{f.parameter: {0, 0, 0, 1}}, RegionHandles: []uint64{f.publisher}}
			if implementation == integer64TimeName {
				request.LogicalTimeValue = integerWireTime(t, 5)
			} else {
				value := float64(5)
				request.LogicalTime = &value
			}
			if _, err := rtiv1.NewDDMServiceClient(f.conn).SendInteractionWithRegions(f.producerCtx, request); err != nil {
				t.Fatal(err)
			}
			saves := rtiv1.NewSavepointServiceClient(f.conn)
			if _, err := saves.RequestFederationSave(f.producerCtx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.producer.FederateHandle, Label: "copied-queue"}); err != nil {
				t.Fatal(err)
			}
			members := []struct {
				response *rtiv1.JoinFederationResponse
				ctx      context.Context
			}{{f.producer, f.producerCtx}, {f.receiver, f.receiverCtx}, {f.foreign, f.foreignCtx}}
			for _, member := range members {
				req := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: member.response.FederateHandle}
				if _, err := saves.FederateSaveBegun(member.ctx, req); err != nil {
					t.Fatal(err)
				}
				if _, err := saves.FederateSaveComplete(member.ctx, req); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := rtiv1.NewDDMServiceClient(f.conn).SetRangeBounds(f.producerCtx, &rtiv1.SetRangeBoundsRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.producer.FederateHandle, RegionHandle: f.publisher, DimensionHandle: f.dimension, Bounds: &rtiv1.Range{Lower: 10, Upper: 20}}); err != nil {
				t.Fatal(err)
			}
			if _, err := rtiv1.NewDDMServiceClient(f.conn).CommitRegionModifications(f.producerCtx, &rtiv1.CommitRegionRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.producer.FederateHandle, RegionHandles: []uint64{f.publisher}}); err != nil {
				t.Fatal(err)
			}
			if _, err := saves.RequestFederationRestore(f.producerCtx, &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.producer.FederateHandle, Label: "copied-queue"}); err != nil {
				t.Fatal(err)
			}
			for _, member := range members {
				if _, err := saves.FederateRestoreComplete(member.ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: member.response.FederateHandle}); err != nil {
					t.Fatal(err)
				}
			}
			var restored *rtiv1.FederateEvent
			for restored == nil {
				batch, err := stream.Recv()
				if err != nil {
					t.Fatal(err)
				}
				for _, event := range batch.Events {
					if event.GetRestoreCompleted() != nil {
						restored = event
						break
					}
				}
			}
			f.receiver.FederateHandle = restored.GetRestoreCompleted().GetFederateHandle()
			advance := &rtiv1.TARRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.receiver.FederateHandle}
			if implementation == integer64TimeName {
				advance.LogicalTimeValue = integerWireTime(t, 5)
			} else {
				advance.LogicalTime = 5
			}
			if _, err := times.TimeAdvanceRequest(f.receiverCtx, advance); err != nil {
				t.Fatal(err)
			}
			event := conveyedProductionReceive(t, stream)
			if len(event.CallbackReceipt) != 32 {
				t.Fatal("restored domain queue has no new mandatory receipt")
			}
			regionCopy := event.GetReceive().GetMetadata().GetReceivedRegions().RegionHandles[0]
			if regionCopy == f.publisher {
				t.Fatal("restored copied designator became mutable source")
			}
			f.report(t, event, true)
			query := &rtiv1.QueryBoundsRequest{WireVersion: 1, FederationName: f.fed, RegionHandle: regionCopy, DimensionHandle: f.dimension}
			if bounds, err := rtiv1.NewDDMServiceClient(f.conn).QueryBounds(f.receiverCtx, query); err != nil || bounds.GetBounds().Lower != 1<<53+1 || bounds.GetBounds().Upper != ^uint64(0) {
				t.Fatalf("restored queued copy: %v %v", bounds, err)
			}
			f.report(t, event, false)
			if _, err := rtiv1.NewDDMServiceClient(f.conn).QueryBounds(f.receiverCtx, query); err == nil {
				t.Fatal("restored completion did not revoke copied query")
			}
		})
	}
}
