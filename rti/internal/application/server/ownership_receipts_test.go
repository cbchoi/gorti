package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestMandatoryOwnershipReceiptsProductionSaveRestore(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	srv, err := newRTID(rtidConfig{GenerationEpoch: 1, SaveDir: t.TempDir(), OutboxBatchSize: 1, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	done := make(chan error, 1)
	go func() { done <- srv.grpcS.Serve(listener) }()
	t.Cleanup(func() { srv.sessions.Close(); srv.grpcS.Stop(); _ = listener.Close(); <-done; _ = srv.plugins.Close() })
	conn, err := stdgrpc.NewClient("passthrough:///ownership-receipts", stdgrpc.WithTransportCredentials(insecure.NewCredentials()), stdgrpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	federations := rtiv1.NewFederationServiceClient(conn)
	const fed = "receipt-save"
	_, err = federations.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, FomModules: []*rtiv1.FOMModule{{Path: "receipt.xml", Xml: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>V</name><attribute><name>x</name></attribute></objectClass></objectClass></objects></objectModel>`)}}})
	if err != nil {
		t.Fatal(err)
	}
	generation, _ := srv.fedMgr.GenerationFor(fed)
	joined := make([]*rtiv1.JoinFederationResponse, 2)
	auth := make([]context.Context, 2)
	for i, name := range []string{"owner", "requester"} {
		joined[i], err = federations.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: fed, FederateName: name, ExpectedFederationGeneration: &generation})
		if err != nil {
			t.Fatal(err)
		}
		auth[i] = metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(joined[i].SessionToken))
	}
	model, err := srv.foms.Get(ctx, fed)
	if err != nil {
		t.Fatal(err)
	}
	class, _ := model.LookupObjectClass("HLAobjectRoot.V")
	attr, _ := model.LookupAttribute(class, "x")
	decl := rtiv1.NewDeclarationServiceClient(conn)
	for i, member := range joined {
		_, err = decl.PublishObjectClassAttributes(auth[i], &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attr)}})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = decl.SubscribeObjectClassAttributes(auth[1], &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: joined[1].FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attr)}})
	if err != nil {
		t.Fatal(err)
	}
	objects := rtiv1.NewObjectServiceClient(conn)
	registered, err := objects.RegisterObjectInstance(auth[0], &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: fed, FederateHandle: joined[0].FederateHandle, ObjectClassHandle: uint64(class)})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := rtiv1.NewStreamServiceClient(conn).EventBatches(auth[1], &rtiv1.EventsRequest{WireVersion: 1, FederationName: fed, FederateHandle: joined[1].FederateHandle})
	if err != nil {
		t.Fatal(err)
	}
	if ready, err := stream.Recv(); err != nil || !ready.GetReady() {
		t.Fatalf("stream ready: %v %v", ready, err)
	}
	epoch, err := srv.outbox.CallbackEpoch(fed, core.FederateHandle(joined[1].FederateHandle))
	if err != nil {
		t.Fatal(err)
	}
	scope := &rtiv1.OwnershipCallbackScopeRequest{WireVersion: 1, FederationName: fed, FederateHandle: joined[1].FederateHandle, FederationGeneration: generation, CallbackEpoch: epoch}
	receipts := rtiv1.NewOwnershipCallbackServiceClient(conn)
	if _, err := receipts.EnableOwnershipCallbackReceipts(ctx, scope); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unsigned capability: %v", err)
	}
	if _, err := receipts.EnableOwnershipCallbackReceipts(auth[0], scope); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("cross-member capability: %v", err)
	}
	if _, err := receipts.EnableOwnershipCallbackReceipts(auth[1], scope); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, err = rtiv1.NewOwnershipServiceClient(conn).AttributeOwnershipAcquisition(auth[1], &rtiv1.AcquireRequest{WireVersion: 1, FederationName: fed, FederateHandle: joined[1].FederateHandle, ObjectHandle: registered.ObjectHandle, AttributeHandles: []uint64{uint64(attr)}, IfAvailable: true})
		if err != nil {
			t.Fatal(err)
		}
	}
	var delivered []*rtiv1.FederateEvent
	for len(delivered) < 2 {
		batch, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range batch.Events {
			if event.GetOwnershipUnavailable() != nil {
				if event.GetOwnershipReceipt() == nil || len(event.CallbackReceipt) != 32 {
					t.Fatal("terminal callback has no receipt")
				}
				delivered = append(delivered, event)
			}
		}
	}
	owner := srv.ownMgr.(*ownership.Manager)
	assertPending := func(want bool) {
		t.Helper()
		if got := owner.HasAcquisitionObligation(fed, core.FederateHandle(joined[1].FederateHandle), core.ObjectHandle(registered.ObjectHandle), []core.AttributeHandle{attr}); got != want {
			t.Fatalf("terminal obligation=%v, want %v", got, want)
		}
	}
	assertPending(true)
	support := rtiv1.NewSupportServiceClient(conn)
	reportGeneral := func(event *rtiv1.FederateEvent, entry bool) {
		t.Helper()
		if _, err := support.ReportCallbackInvocation(auth[1], &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: joined[1].FederateHandle, CallbackReceipt: event.CallbackReceipt, InvocationEntry: entry, Success: true}); err != nil {
			t.Fatalf("general invocation entry=%v: %v", entry, err)
		}
	}
	saves := rtiv1.NewSavepointServiceClient(conn)
	if _, err := saves.RequestFederationSave(auth[0], &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: fed, FederateHandle: joined[0].FederateHandle, Label: "pending-receipt"}); err != nil {
		t.Fatal(err)
	}
	invocation := &rtiv1.ReportOwnershipCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: joined[1].FederateHandle, Receipt: delivered[0].OwnershipReceipt}
	// Callback invocation reports remain legal while ordinary services are save-blocked.
	reportGeneral(delivered[0], true)
	if _, err := receipts.ReportOwnershipCallbackInvocation(auth[1], invocation); err != nil {
		t.Fatalf("invocation during save: %v", err)
	}
	assertPending(true)
	if _, err := receipts.ReportOwnershipCallbackInvocation(auth[1], invocation); err != nil {
		t.Fatalf("duplicate invocation: %v", err)
	}
	reportGeneral(delivered[0], false)
	for i, member := range joined {
		r := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.FederateHandle}
		if _, err := saves.FederateSaveBegun(auth[i], r); err != nil {
			t.Fatal(err)
		}
		if _, err := saves.FederateSaveComplete(auth[i], r); err != nil {
			t.Fatal(err)
		}
	}
	reportGeneral(delivered[1], true)
	invocation.Receipt = delivered[1].OwnershipReceipt
	if _, err := receipts.ReportOwnershipCallbackInvocation(auth[1], invocation); err != nil {
		t.Fatal(err)
	}
	reportGeneral(delivered[1], false)
	assertPending(false)
	if _, err := saves.RequestFederationRestore(auth[0], &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: fed, FederateHandle: joined[0].FederateHandle, Label: "pending-receipt"}); err != nil {
		t.Fatal(err)
	}
	for i, member := range joined {
		if _, err := saves.FederateRestoreComplete(auth[i], &rtiv1.FederateRestoreResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.FederateHandle}); err != nil {
			t.Fatal(err)
		}
	}
	assertPending(true)
	if _, err := receipts.ReportOwnershipCallbackInvocation(auth[1], invocation); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("pre-restore invocation crossed epoch: %v", err)
	}
	scope.CallbackEpoch, err = srv.outbox.CallbackEpoch(fed, core.FederateHandle(joined[1].FederateHandle))
	if err != nil || scope.CallbackEpoch == epoch {
		t.Fatalf("restore epoch: %v %v", scope, err)
	}
	// The general checkpoint publishes the restored terminal automatically.
	// A domain retry before consuming it would supersede its delivery attempt.
	var replayedEvent *rtiv1.FederateEvent
	for replayedEvent == nil {
		batch, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range batch.Events {
			if event.GetOwnershipUnavailable() != nil {
				replayedEvent = event
			}
		}
	}
	replayed := replayedEvent.GetOwnershipReceipt()
	if replayed == nil || replayed.CallbackId != delivered[1].OwnershipReceipt.CallbackId || replayed.CallbackEpoch != scope.CallbackEpoch || replayed.FederationGeneration != generation {
		t.Fatalf("restored callback identity: %v", replayed)
	}
	reportGeneral(replayedEvent, true)
	invocation.Receipt = replayed
	if _, err := receipts.ReportOwnershipCallbackInvocation(auth[1], invocation); err != nil {
		t.Fatal(err)
	}
	reportGeneral(replayedEvent, false)
	assertPending(false)
	if _, err := receipts.RetryOwnershipCallbacks(auth[1], scope); err != nil {
		t.Fatalf("reconciled terminal retry: %v", err)
	}
}
