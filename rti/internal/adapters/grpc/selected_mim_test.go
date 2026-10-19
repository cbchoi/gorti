package grpc

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/pkg/federate"
	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// Uses the unchanged production V1 guard, not a separately implemented mock
// interpretation of version negotiation. Rejection precedes all mutation.
type legacyMIMServer struct {
	rtiv1.UnimplementedFederationServiceServer
	mu       sync.Mutex
	created  int
	joined   int
	versions []rtiv1.WireVersion
}

func (s *legacyMIMServer) CreateFederation(_ context.Context, request *rtiv1.CreateFederationRequest) (*rtiv1.CreateFederationResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.versions = append(s.versions, request.GetWireVersion())
	if err := requireWireVersion(request.GetWireVersion()); err != nil {
		return nil, err
	}
	s.created++
	return &rtiv1.CreateFederationResponse{}, nil
}

func (s *legacyMIMServer) JoinFederation(_ context.Context, request *rtiv1.JoinFederationRequest) (*rtiv1.JoinFederationResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := requireWireVersion(request.GetWireVersion()); err != nil {
		return nil, err
	}
	s.joined++
	return &rtiv1.JoinFederationResponse{FederateHandle: 1}, nil
}

func TestSelectedMIMGoSDKLegacyGuardRejectsBeforeMutation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	legacy := &legacyMIMServer{}
	server := stdgrpc.NewServer()
	rtiv1.RegisterFederationServiceServer(server, legacy)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close(); <-done })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connection, err := federate.Connect(ctx, listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	spec := federate.FederationSpec{Name: "selected", MIMModule: &federate.FOMModule{Path: "selected.xml", XML: mim.StandardMIMBytes()}}
	if _, err := connection.CreateFederation(ctx, spec); err == nil {
		t.Fatal("SDK creation accepted legacy server")
	}
	if _, err := connection.JoinFederation(ctx, spec, "member"); err == nil {
		t.Fatal("SDK create-or-join accepted legacy server")
	}
	legacy.mu.Lock()
	created, versions := legacy.created, append([]rtiv1.WireVersion(nil), legacy.versions...)
	legacy.mu.Unlock()
	if created != 0 {
		t.Fatal("legacy server mutated on explicit MIM")
	}
	if len(versions) != 2 || versions[0] != 2 || versions[1] != 2 {
		t.Fatal("SDK did not negotiate selected-MIM profile")
	}
	if _, err := connection.CreateFederation(ctx, federate.FederationSpec{Name: "old"}); err != nil {
		t.Fatal(err)
	}
	legacy.mu.Lock()
	created, versions = legacy.created, append([]rtiv1.WireVersion(nil), legacy.versions...)
	legacy.mu.Unlock()
	if created != 1 || versions[2] != 1 {
		t.Fatal("omitted-MIM V1 behavior changed")
	}
	wire, err := stdgrpc.NewClient(listener.Addr().String(), stdgrpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer wire.Close()
	_, err = rtiv1.NewFederationServiceClient(wire).JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 2, FederationName: "old", ExpectedMimModule: &rtiv1.FOMModule{Path: "chosen.xml", Xml: mim.StandardMIMBytes()}})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("legacy join profile: %v", err)
	}
	legacy.mu.Lock()
	joined := legacy.joined
	legacy.mu.Unlock()
	if joined != 0 {
		t.Fatal("legacy join mutated before version rejection")
	}
}

func TestSelectedMIMCreateProfiles(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version rtiv1.WireVersion
		module  *rtiv1.FOMModule
		code    codes.Code
	}{
		{"old", 1, nil, codes.OK},
		{"explicit", 2, &rtiv1.FOMModule{Path: "chosen.xml", Xml: mim.StandardMIMBytes()}, codes.OK},
		{"old-explicit", 1, &rtiv1.FOMModule{Path: "chosen.xml"}, codes.InvalidArgument},
		{"new-omitted", 2, nil, codes.FailedPrecondition},
		{"anonymous", 2, &rtiv1.FOMModule{Xml: mim.StandardMIMBytes()}, codes.InvalidArgument},
		{"future", 3, nil, codes.FailedPrecondition},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeFedStore{}
			_, err := newFedSvc(store).CreateFederation(context.Background(), &rtiv1.CreateFederationRequest{WireVersion: tc.version, FederationName: "selected", MimModule: tc.module})
			if status.Code(err) != tc.code {
				t.Fatalf("code %v: %v", status.Code(err), err)
			}
			if tc.code != codes.OK && len(store.createCalls) != 0 {
				t.Fatal("profile rejection mutated state")
			}
			if tc.module != nil && tc.code == codes.OK && store.createCalls[0].MIMModule.Path != tc.module.Path {
				t.Fatal("MIM role lost")
			}
		})
	}
	if err := requireWireVersion(2); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("non-create V2 guard widened")
	}
}
