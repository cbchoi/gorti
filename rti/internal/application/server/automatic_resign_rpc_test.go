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
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestAutomaticResignProductionSessionAndCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	server, err := newRTID(rtidConfig{OutboxBatchSize: 1, AutoResignGrace: 80 * time.Millisecond, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	done := make(chan error, 1)
	go func() { done <- server.grpcS.Serve(listener) }()
	t.Cleanup(func() {
		server.sessions.Close()
		server.grpcS.Stop()
		_ = listener.Close()
		<-done
		_ = server.plugins.Close()
	})
	conn, err := stdgrpc.NewClient("passthrough:///automatic-resign", stdgrpc.WithTransportCredentials(insecure.NewCredentials()), stdgrpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	fed := rtiv1.NewFederationServiceClient(conn)
	_, err = fed.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: "auto", FomModules: []*rtiv1.FOMModule{{Path: "auto.xml", Xml: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>V</name><attribute><name>x</name></attribute></objectClass></objectClass></objects><switches><automaticResignAction resignAction="CancelThenDeleteThenDivest"/></switches></objectModel>`)}}})
	if err != nil {
		t.Fatal(err)
	}
	joined, err := fed.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: "auto", FederateName: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	if len(joined.SessionToken) != 32 {
		t.Fatal("join did not issue session capability")
	}
	auth := metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(joined.SessionToken))
	query := &rtiv1.GetAutomaticResignDirectiveRequest{WireVersion: 1, FederationName: "auto", FederateHandle: joined.FederateHandle}
	if _, err := fed.GetAutomaticResignDirective(ctx, query); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated query: %v", err)
	}
	state, err := fed.GetAutomaticResignDirective(auth, query)
	if err != nil || state.GetAction() != rtiv1.ResignAction_RESIGN_ACTION_CANCEL_THEN_DELETE {
		t.Fatalf("FOM default: %v %v", state, err)
	}
	other, err := fed.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: "auto", FederateName: "other"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = fed.SetAutomaticResignDirective(auth, &rtiv1.SetAutomaticResignDirectiveRequest{WireVersion: 1, FederationName: "auto", FederateHandle: other.FederateHandle, Action: rtiv1.ResignAction_RESIGN_ACTION_DELETE_OBJECTS})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("cross-member setter: %v", err)
	}
	_, err = fed.ResignFederation(auth, &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: "auto", FederateHandle: other.FederateHandle, Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("cross-member signed resign: %v", err)
	}
	_, err = fed.SetAutomaticResignDirective(auth, &rtiv1.SetAutomaticResignDirectiveRequest{WireVersion: 1, FederationName: "auto", FederateHandle: joined.FederateHandle, Action: rtiv1.ResignAction(255)})
	if err == nil {
		t.Fatal("invalid directive accepted")
	}
	model, err := server.foms.Get(ctx, "auto")
	if err != nil {
		t.Fatal(err)
	}
	class, _ := model.LookupObjectClass("HLAobjectRoot.V")
	attribute, _ := model.LookupAttribute(class, "x")
	_, err = rtiv1.NewDeclarationServiceClient(conn).PublishObjectClassAttributes(ctx, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: "auto", FederateHandle: joined.FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attribute)}})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := rtiv1.NewObjectServiceClient(conn).RegisterObjectInstance(ctx, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: "auto", FederateHandle: joined.FederateHandle, ObjectClassHandle: uint64(class)})
	if err != nil {
		t.Fatal(err)
	}
	streamCtx, stopStream := context.WithCancel(auth)
	stream, err := rtiv1.NewStreamServiceClient(conn).EventBatches(streamCtx, &rtiv1.EventsRequest{WireVersion: 1, FederationName: "auto", FederateHandle: joined.FederateHandle})
	if err != nil {
		t.Fatal(err)
	}
	ready, err := stream.Recv()
	if err != nil || !ready.GetReady() {
		t.Fatalf("ready: %v %v", ready, err)
	}
	stopStream()
	deadline := time.Now().Add(3 * time.Second)                                                                                  //nolint:forbidigo // Exercise real reconnection deadlines against the server's wall clock.
	for server.fedMgr.ValidateMember("auto", core.FederateHandle(joined.FederateHandle)) == nil && time.Now().Before(deadline) { //nolint:forbidigo // Exercise real reconnection deadlines against the server's wall clock.
		time.Sleep(5 * time.Millisecond)
	}
	if err := server.fedMgr.ValidateMember("auto", core.FederateHandle(joined.FederateHandle)); err == nil {
		t.Fatal("disconnected federate retained")
	}
	if class := server.objReg.ClassOf("auto", core.ObjectHandle(registered.ObjectHandle)); class != 0 {
		t.Fatal("automatic delete left object")
	}
	if err := server.fedMgr.ValidateMember("auto", core.FederateHandle(other.FederateHandle)); err != nil {
		t.Fatal("other federate affected", err)
	}
	replacement, err := fed.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: "auto", FederateName: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	query.FederateHandle = replacement.FederateHandle
	if _, err := fed.GetAutomaticResignDirective(auth, query); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("old token rejoin: %v", err)
	}
	resign := &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: "auto", FederateHandle: replacement.FederateHandle, Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION}
	if _, err := fed.ResignFederation(auth, resign); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("old token signed resign: %v", err)
	}
	replacementAuth := metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(replacement.SessionToken))
	if _, err := fed.ResignFederation(replacementAuth, resign); err != nil {
		t.Fatalf("authenticated self resign: %v", err)
	}
	if err := server.fedMgr.ValidateMember("auto", core.FederateHandle(other.FederateHandle)); err != nil {
		t.Fatal("authenticated resign affected another member", err)
	}
}
