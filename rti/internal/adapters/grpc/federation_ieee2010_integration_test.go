package grpc_test

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	grpcsvc "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestIEEE2010FederationMIMAndImmediateJoinedViewRealWire(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	repo := composition.NewFOMRepository()
	clock := core.NewManualClock(time.Unix(0, 0))
	members, err := federation.New(federation.Options{Clock: clock, FOMs: repo, PrepareJoinFOM: repo.PrepareLiveExtension})
	check(err)
	decl := declaration.New()
	decl.SetFOMRepository(repo)
	out := delivery.NewWithBatch(64, 1, 0)
	objects, err := object.New(object.Options{Clock: clock, FOMs: repo, Declarations: decl, Outbox: out})
	check(err)
	svc, err := grpcsvc.NewServer(grpcsvc.Options{
		Federations: members, Membership: members, FOMs: repo, Declarations: decl, Objects: objects, Outbox: out,
		OnCreateFederationSuccess: func(_ context.Context, fed core.FederationName, _ []core.FOMModule) {
			h, err := members.FOMFor(fed)
			if err != nil {
				panic(err)
			}
			repo.RememberFor(fed, h)
		},
		OnDestroyFederationSuccess: func(_ context.Context, fed core.FederationName) { repo.ForgetFor(fed) },
	})
	check(err)
	server := grpcgo.NewServer(grpcgo.UnaryInterceptor(svc.UnaryMembershipInterceptor()))
	check(svc.Register(server))
	listener := bufconn.Listen(1 << 20)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close(); <-done })
	conn, err := grpcgo.NewClient("passthrough:///ieee-federation", grpcgo.WithTransportCredentials(insecure.NewCredentials()), grpcgo.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	check(err)
	t.Cleanup(func() { _ = conn.Close() })
	client := rtiv1.NewFederationServiceClient(conn)
	const name = "ieee-federation"
	selected := &rtiv1.FOMModule{Path: "HLAstandardMIM", Xml: mim.StandardMIMBytes()}
	request := &rtiv1.CreateFederationRequest{WireVersion: 2, FederationName: name, MimModule: selected,
		FomModules: []*rtiv1.FOMModule{{Path: "base.xml", Xml: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Base</name></objectClass></objectClass></objects></objectModel>`)}}}
	_, err = client.CreateFederation(ctx, request)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("reserved MIM: %v", err)
	}
	if _, exists := members.GenerationFor(name); exists {
		t.Fatal("reserved MIM published execution")
	}
	if _, err := repo.Get(ctx, name); err == nil {
		t.Fatal("reserved MIM published FOM")
	}
	selected.Path = "custom.xml"
	created, err := client.CreateFederation(ctx, request)
	check(err)
	generation := created.GetFederationGeneration()
	first, err := client.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 2, FederationName: name, FederateName: "first", ExpectedFederationGeneration: &generation, ExpectedMimModule: selected})
	check(err)
	if !bytes.Equal(first.GetFomView().GetMimModule().GetXml(), selected.Xml) {
		t.Fatal("join lost selected MIM")
	}
	view, err := client.GetFOMView(ctx, &rtiv1.GetFOMViewRequest{WireVersion: 1, FederationName: name, FederateHandle: first.FederateHandle})
	check(err)
	if view.GetRevision() == 0 {
		t.Fatal("immediate joined service has no FOM")
	}
	extra := &rtiv1.FOMModule{Path: "extra.xml", Xml: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Extra</name></objectClass></objectClass></objects></objectModel>`)}
	second, err := client.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: name, ExpectedFederationGeneration: &generation, AdditionalFomModules: []*rtiv1.FOMModule{extra}})
	check(err)
	if second.GetFomView().GetRevision() != view.GetRevision()+1 {
		t.Fatal("joined extension not available before response")
	}
	for _, member := range members.ListMembers(name) {
		if member.Name == "" {
			t.Fatal("unnamed join did not receive a name")
		}
	}
	_, err = client.DestroyFederation(ctx, &rtiv1.DestroyFederationRequest{WireVersion: 1, FederationName: name, ExpectedFederationGeneration: &generation})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("destroy while joined: %v", err)
	}
	_, err = client.ResignFederation(ctx, &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: name, FederateHandle: second.FederateHandle, Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION})
	check(err)
	view, err = client.GetFOMView(ctx, &rtiv1.GetFOMViewRequest{WireVersion: 1, FederationName: name, FederateHandle: first.FederateHandle})
	check(err)
	if view.GetRevision() != second.FomView.Revision || len(view.GetModules()) != 2 {
		t.Fatal("resign removed federation FOM extension")
	}
	_, err = client.ResignFederation(ctx, &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: name, FederateHandle: first.FederateHandle, Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION})
	check(err)
	_, err = client.DestroyFederation(ctx, &rtiv1.DestroyFederationRequest{WireVersion: 1, FederationName: name, ExpectedFederationGeneration: &generation})
	check(err)
	if _, exists := members.GenerationFor(name); exists {
		t.Fatal("destroy left execution present")
	}
}
