package grpc

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

type receiptWireOutbox struct {
	*delivery.MultiOutbox
	subscribed chan struct{}
	mu         sync.Mutex
	last       core.OutboundEvent
}

func (o *receiptWireOutbox) Subscribe(ctx context.Context, fed core.FederationName, h core.FederateHandle) (<-chan []core.OutboundEvent, func() error, error) {
	ch, closeQueue, err := o.MultiOutbox.Subscribe(ctx, fed, h)
	if err == nil {
		close(o.subscribed)
	}
	return ch, closeQueue, err
}

func (o *receiptWireOutbox) Send(ctx context.Context, fed core.FederationName, h core.FederateHandle, e core.OutboundEvent) error {
	o.mu.Lock()
	o.last = e
	o.mu.Unlock()
	return o.MultiOutbox.Send(ctx, fed, h, e)
}

func TestOwnershipReceiptRealWireInvocationAndDuplicateReplay(t *testing.T) {
	for _, batched := range []bool{false, true} {
		t.Run(map[bool]string{false: "events", true: "batches"}[batched], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			out := &receiptWireOutbox{MultiOutbox: delivery.NewWithBatch(16, 1, 0), subscribed: make(chan struct{})}
			mgr, err := ownership.New(ownership.Options{Outbox: out})
			if err != nil {
				t.Fatal(err)
			}
			scope := core.OwnershipCallbackScope{Federation: "fed", Recipient: 2, FederationGeneration: 4}
			receipts, err := NewOwnershipCallbackReceipts(OwnershipCallbackReceiptOptions{Coordinator: mgr,
				Authenticate: func(ctx context.Context, _ core.FederationName, h core.FederateHandle) error {
					md, _ := metadata.FromIncomingContext(ctx)
					if h != 2 || len(md.Get("receipt-owner")) != 1 || md.Get("receipt-owner")[0] != "2" {
						return status.Error(codes.PermissionDenied, "recipient authentication")
					}
					return nil
				},
				AcquireScope: func(_ context.Context, s core.OwnershipCallbackScope) (func(), error) {
					if s != scope {
						return nil, core.ErrOwnershipCallbackScope
					}
					return func() {}, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			streams := newStreamService(out)
			streams.ownershipReceipts = receipts
			streams.receipts, _ = NewCallbackReceipts(CallbackReceiptOptions{Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error {
				return errors.New("optional metrics unavailable")
			}, Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
				return nil
			}})
			server := grpcgo.NewServer()
			rtiv1.RegisterStreamServiceServer(server, streams)
			rtiv1.RegisterOwnershipCallbackServiceServer(server, NewOwnershipCallbackService(receipts))
			listener := bufconn.Listen(1 << 20)
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(func() { server.Stop(); _ = listener.Close() })
			conn, err := grpcgo.NewClient("passthrough:///ownership-receipt", grpcgo.WithTransportCredentials(insecure.NewCredentials()), grpcgo.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			ctx = metadata.AppendToOutgoingContext(ctx, "receipt-owner", "2")
			client := rtiv1.NewOwnershipCallbackServiceClient(conn)
			request := &rtiv1.OwnershipCallbackScopeRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 2, FederationGeneration: 4, CallbackEpoch: 0}
			if _, err := client.EnableOwnershipCallbackReceipts(ctx, request); err != nil {
				t.Fatal(err)
			}
			streamClient := rtiv1.NewStreamServiceClient(conn)
			var recv func() (*rtiv1.FederateEvent, error)
			if batched {
				stream, err := streamClient.EventBatches(ctx, &rtiv1.EventsRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 2})
				if err != nil {
					t.Fatal(err)
				}
				ready, err := stream.Recv()
				if err != nil || !ready.GetReady() {
					t.Fatal("ready", ready, err)
				}
				var pending []*rtiv1.FederateEvent
				recv = func() (*rtiv1.FederateEvent, error) {
					for len(pending) == 0 {
						batch, err := stream.Recv()
						if err != nil {
							return nil, err
						}
						pending = batch.Events
					}
					event := pending[0]
					pending = pending[1:]
					return event, nil
				}
			} else {
				stream, err := streamClient.Events(ctx, &rtiv1.EventsRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 2})
				if err != nil {
					t.Fatal(err)
				}
				recv = stream.Recv
			}
			select {
			case <-out.subscribed:
			case <-ctx.Done():
				t.Fatal("stream not subscribed")
			}
			mgr.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1})
			if err := mgr.AcquireIfAvailable(ctx, "fed", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
				t.Fatal(err)
			}
			event, err := recv()
			if err != nil {
				t.Fatal(err)
			}
			wire := event.GetOwnershipReceipt()
			if event.GetOwnershipUnavailable() == nil || wire == nil || wire.CallbackId == 0 || wire.DeliveryAttempt != 1 || len(event.CallbackReceipt) != 0 {
				t.Fatal("mandatory receipt depends on optional metrics", event)
			}
			if !mgr.HasAcquisitionObligation("fed", 2, 100, nil) {
				t.Fatal("gRPC send cleared actual invocation obligation")
			}
			bad := &rtiv1.ReportOwnershipCallbackInvocationRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 3, Receipt: wire}
			if _, err := client.ReportOwnershipCallbackInvocation(ctx, bad); status.Code(err) != codes.PermissionDenied {
				t.Fatal("cross recipient receipt accepted", err)
			}
			stale := proto.Clone(wire).(*rtiv1.OwnershipCallbackReceipt)
			stale.FederationGeneration++
			bad.FederateHandle = 2
			bad.Receipt = stale
			if _, err := client.ReportOwnershipCallbackInvocation(ctx, bad); status.Code(err) != codes.FailedPrecondition {
				t.Fatal("stale generation receipt accepted", err)
			}
			if !mgr.HasAcquisitionObligation("fed", 2, 100, nil) {
				t.Fatal("invalid report cleared obligation")
			}
			bad.Receipt = wire
			if _, err := client.ReportOwnershipCallbackInvocation(ctx, bad); err != nil {
				t.Fatal(err)
			}
			if mgr.HasAcquisitionObligation("fed", 2, 100, nil) {
				t.Fatal("authenticated receipt did not release obligation")
			}
			out.mu.Lock()
			duplicate := out.last
			out.mu.Unlock()
			if err := out.MultiOutbox.Send(ctx, "fed", 2, duplicate); err != nil {
				t.Fatal(err)
			}
			if err := out.MultiOutbox.Send(ctx, "fed", 2, &object.CallbackEvent{Kind: object.CallbackDiscover, Object: 100, ObjectClass: 8, ObjectName: "new-discovery"}); err != nil {
				t.Fatal(err)
			}
			next, err := recv()
			if err != nil || next.GetDiscover() == nil {
				t.Fatal("acknowledged duplicate closed stream or displaced discovery", next, err)
			}
		})
	}
}

func receiptAdapterForDomain(t *testing.T) (*OwnershipCallbackReceipts, *ownership.Manager, *fakeSubscribableOutbox, core.OwnershipCallbackScope, *atomic.Bool) {
	t.Helper()
	out := newFakeSubscribableOutbox()
	mgr, err := ownership.New(ownership.Options{Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	scope := core.OwnershipCallbackScope{Federation: "fed", Recipient: 2, FederationGeneration: 5}
	if err := mgr.EnableOwnershipCallbackReceipts(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	mgr.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1, 2})
	held := &atomic.Bool{}
	receipts, err := NewOwnershipCallbackReceipts(OwnershipCallbackReceiptOptions{Coordinator: mgr, Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil }, AcquireScope: func(_ context.Context, s core.OwnershipCallbackScope) (func(), error) {
		if s != scope {
			return nil, core.ErrOwnershipCallbackScope
		}
		held.Store(true)
		return func() { held.Store(false) }, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return receipts, mgr, out, scope, held
}

type receiptFailingStream struct {
	fakeStream
	held   *atomic.Bool
	before func(*rtiv1.FederateEvent)
}

func (s *receiptFailingStream) Send(event *rtiv1.FederateEvent) error {
	if s.held.Load() {
		return errors.New("scope pin crosses transport")
	}
	if s.before != nil {
		s.before(event)
	}
	return s.fakeStream.Send(event)
}

func TestOwnershipReceiptFailedTransportAndEarlyReport(t *testing.T) {
	receipts, mgr, out, scope, held := receiptAdapterForDomain(t)
	if err := mgr.AcquireIfAvailable(context.Background(), "fed", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	event, err := toFederateEvent((<-out.ch)[0])
	if err != nil {
		t.Fatal(err)
	}
	sink := &receiptFailingStream{fakeStream: fakeStream{ctx: context.Background(), sendErr: errors.New("failed transport")}, held: held}
	sink.before = func(event *rtiv1.FederateEvent) {
		wire := event.GetOwnershipReceipt()
		err := mgr.ReportOwnershipCallbackInvocation(context.Background(), scope, core.OwnershipCallbackReceipt{FederationGeneration: wire.FederationGeneration, CallbackEpoch: wire.CallbackEpoch, CallbackID: wire.CallbackId, DeliveryAttempt: wire.DeliveryAttempt})
		if err != nil {
			t.Fatal(err)
		}
	}
	stream := &ownershipReceiptEventsStream{StreamService_EventsServer: sink, receipts: receipts, fed: "fed", handle: 2}
	if err := stream.Send(event); err == nil {
		t.Fatal("failed transport accepted")
	}
	if !mgr.HasAcquisitionObligation("fed", 2, 100, nil) {
		t.Fatal("failed send+early report lost terminal")
	}
	sink.sendErr = nil
	if err := stream.Send(event); err != nil {
		t.Fatal(err)
	}
	if mgr.HasAcquisitionObligation("fed", 2, 100, nil) {
		t.Fatal("successful send+early invocation did not complete")
	}
}

func TestOwnershipReceiptBatchPreparationRollbackAndDuplicate(t *testing.T) {
	receipts, mgr, out, _, _ := receiptAdapterForDomain(t)
	for _, attr := range []core.AttributeHandle{1, 2} {
		if err := mgr.AcquireIfAvailable(context.Background(), "fed", 2, 100, []core.AttributeHandle{attr}, nil); err != nil {
			t.Fatal(err)
		}
	}
	first, _ := toFederateEvent((<-out.ch)[0])
	second, _ := toFederateEvent((<-out.ch)[0])
	sink := &fakeBatchStream{ctx: context.Background()}
	stream := &ownershipReceiptBatchesStream{StreamService_EventBatchesServer: sink, receipts: receipts, fed: "fed", handle: 2}
	second.OwnershipReceipt.FederationGeneration++
	if err := stream.Send(&rtiv1.FederateEventBatch{Events: []*rtiv1.FederateEvent{first, second}}); err == nil {
		t.Fatal("invalid later scope accepted")
	}
	if len(sink.sent) != 0 || len(mgr.ExportState("fed").PendingCallbacks) != 2 {
		t.Fatal("partial batch send or lost obligation")
	}
	second.OwnershipReceipt.FederationGeneration--
	if err := stream.Send(&rtiv1.FederateEventBatch{Events: []*rtiv1.FederateEvent{first, first, second}}); err != nil {
		t.Fatal(err)
	}
	if len(sink.sent) != 1 || len(sink.sent[0].Events) != 2 {
		t.Fatal("pending replay duplicate caused busy/partial fanout", sink.sent)
	}
	if sink.sent[0].Events[0].OwnershipReceipt.DeliveryAttempt != 2 {
		t.Fatal("rolled back attempt was reused")
	}
	if len(mgr.ExportState("fed").PendingCallbacks) != 2 {
		t.Fatal("batch send treated as invocation")
	}
}

func TestOwnershipReceiptConstructorRequiresMandatoryHooks(t *testing.T) {
	if _, err := NewOwnershipCallbackReceipts(OwnershipCallbackReceiptOptions{}); err == nil {
		t.Fatal("missing authentication/scope accepted")
	}
}
