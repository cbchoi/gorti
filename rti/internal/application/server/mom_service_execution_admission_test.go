package server

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type momExecutionRPC struct {
	f                        *momExecutionFixture
	client                   rtiv1.ObjectServiceClient
	conn                     *grpc.ClientConn
	senderToken, targetToken []byte
}

func newMOMExecutionRPC(t *testing.T, service string, configure ...func(*grpcadapter.Options, *momServiceAdmissionDependencies)) *momExecutionRPC {
	t.Helper()
	f := newMOMExecutionFixture(t, service)
	sessions, err := session.New(session.Options{Membership: f.deps.Federations})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sessions.Close)
	r := &momExecutionRPC{f: f}
	for _, entry := range []struct {
		handle core.FederateHandle
		token  *[]byte
	}{{f.sender, &r.senderToken}, {f.target, &r.targetToken}} {
		id, err := f.deps.Federations.MemberIdentity("catalog-provider", entry.handle)
		if err != nil {
			t.Fatal(err)
		}
		*entry.token, err = session.NewToken()
		if err != nil {
			t.Fatal(err)
		}
		sessions.Register(id, *entry.token)
	}
	options := grpcadapter.Options{
		Federations: f.deps.Federations, Membership: f.deps.Federations,
		Declarations: f.deps.Declarations, Objects: f.deps.Objects, Time: f.time,
		Outbox: f.out, Savepoint: f.save, CheckpointAdmission: f.save, Sessions: sessions,
		EnableConfirmedObjectStream: true, EnableInteractionStream: true,
	}
	admissionDeps := momServiceAdmissionDependencies{
		Dispatcher: f.dispatcher, FOMs: f.deps.FOMs, Federations: f.deps.Federations,
		Declarations: f.deps.Declarations, Authenticate: sessions.Authenticate, Checkpoint: f.save,
	}
	for _, apply := range configure {
		apply(&options, &admissionDeps)
	}
	server, err := grpcadapter.NewServer(options)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := newMOMServiceAdmissionInterceptor(server.UnaryMembershipInterceptor(), admissionDeps)
	if err != nil {
		t.Fatal(err)
	}
	deps := f.executor.(*momServiceExecutor).deps
	deps.Admission = admittedMOMServiceExecution
	if err := configureMOMServiceExecution(f.dispatcher, deps); err != nil {
		t.Fatal(err)
	}
	streamAdmission, err := newMOMServiceStreamAdmissionInterceptor(server.StreamMembershipInterceptor(), admissionDeps)
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer(grpc.UnaryInterceptor(admission), grpc.StreamInterceptor(streamAdmission))
	if err := server.Register(s); err != nil {
		t.Fatal(err)
	}
	lis := bufconn.Listen(1 << 20)
	done := make(chan error, 1)
	go func() { done <- s.Serve(lis) }()
	t.Cleanup(func() { s.Stop(); _ = lis.Close(); <-done })
	conn, err := grpc.NewClient("passthrough:///mom-execution", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	r.client = rtiv1.NewObjectServiceClient(conn)
	r.conn = conn
	return r
}

func (r *momExecutionRPC) request(t *testing.T, service string, target core.FederateHandle, extra map[string][]byte) *rtiv1.SendInteractionRequest {
	t.Helper()
	fom, err := r.f.deps.FOMs.Get(context.Background(), "catalog-provider")
	if err != nil {
		t.Fatal(err)
	}
	name := mom.ServiceClassPrefix + "HLA" + service
	class, ok := fom.LookupInteractionClass(name)
	if !ok {
		t.Fatal(name)
	}
	p := map[string][]byte{"HLAfederate": catalogRPCHandle(uint64(target))}
	for k, v := range extra {
		p[k] = v
	}
	params := map[uint64][]byte{}
	for k, v := range p {
		h, ok := fom.LookupParameter(class, k)
		if !ok {
			t.Fatal(k)
		}
		params[uint64(h)] = v
	}
	return &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: "catalog-provider", FederateHandle: uint64(r.f.sender), InteractionClassHandle: uint64(class), Parameters: params}
}

func (r *momExecutionRPC) publish(t *testing.T, req *rtiv1.SendInteractionRequest) {
	t.Helper()
	if err := r.f.deps.Declarations.PublishInteractionClass(context.Background(), "catalog-provider", r.f.sender, core.InteractionClassHandle(req.InteractionClassHandle)); err != nil {
		t.Fatal(err)
	}
}

func (r *momExecutionRPC) send(req *rtiv1.SendInteractionRequest, token []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, session.MetadataKey, string(token))
	_, err := r.client.SendInteraction(ctx, req)
	return err
}

func TestMOMServiceAdmissionRPCResignDoesNotHoldMembershipReader(t *testing.T) {
	r := newMOMExecutionRPC(t, momServiceResignFederationExecution)
	req := r.request(t, momServiceResignFederationExecution, r.f.target, map[string][]byte{"HLAresignAction": catalogRPCInt(6)})
	r.publish(t, req)
	if err := r.send(req, r.senderToken); err != nil {
		t.Fatal("authenticated MOM resign failed or deadlocked:", err)
	}
	if _, err := r.f.deps.Federations.MemberIdentity("catalog-provider", r.f.target); err == nil {
		t.Fatal("target remains joined")
	}
	if _, err := r.f.deps.Federations.MemberIdentity("catalog-provider", r.f.sender); err != nil {
		t.Fatal("managing sender was resigned:", err)
	}
}

func TestMOMServiceAdmissionRPCCredentialsAndPublication(t *testing.T) {
	for _, tc := range []string{"missing-token", "target-token", testUnpublishedLabel, "valid-sender"} {
		t.Run(tc, func(t *testing.T) {
			r := newMOMExecutionRPC(t, momServiceEnableAsynchronousDelivery)
			req := r.request(t, momServiceEnableAsynchronousDelivery, r.f.target, nil)
			if tc != testUnpublishedLabel {
				r.publish(t, req)
			}
			token := r.senderToken
			if tc == "missing-token" {
				token = nil
			}
			if tc == "target-token" {
				token = r.targetToken
			}
			err := r.send(req, token)
			if tc == "valid-sender" {
				if err != nil || !r.f.time.Metrics("catalog-provider", r.f.target).AsynchronousDelivery {
					t.Fatal("authenticated managing sender failed:", err)
				}
			} else {
				want := codes.Unauthenticated
				if tc == "target-token" {
					want = codes.PermissionDenied
				}
				if tc == testUnpublishedLabel {
					want = codes.FailedPrecondition
				}
				if status.Code(err) != want {
					t.Fatalf("error=%v want=%v", err, want)
				}
				if r.f.time.Metrics("catalog-provider", r.f.target).AsynchronousDelivery {
					t.Fatal("rejected request changed target")
				}
			}
		})
	}
}

func TestMOMServiceAdmissionRPCCorrectCheckpointControls(t *testing.T) {
	for _, service := range []string{momServiceFederateSaveBegun, momServiceFederateSaveComplete, momServiceFederateRestoreComplete} {
		t.Run(service, func(t *testing.T) {
			r := newMOMExecutionRPC(t, service)
			extra := map[string][]byte{}
			if service != momServiceFederateSaveBegun {
				extra["HLAsuccessIndicator"] = catalogRPCInt(1)
			}
			req := r.request(t, service, r.f.target, extra)
			r.publish(t, req)
			if err := r.send(req, r.senderToken); err != nil {
				t.Fatal("MOM checkpoint confirmation blocked:", err)
			}
			blocked := r.request(t, momServiceEnableAsynchronousDelivery, r.f.target, nil)
			r.publish(t, blocked)
			if err := r.send(blocked, r.senderToken); status.Code(err) != codes.FailedPrecondition {
				t.Fatal("ordinary service bypassed active checkpoint:", err)
			}
			if r.f.time.Metrics("catalog-provider", r.f.target).AsynchronousDelivery {
				t.Fatal("blocked service mutated time state")
			}
		})
	}
}

func TestMOMServiceAdmissionRPCUndefinedParameterHasNoEffect(t *testing.T) {
	r := newMOMExecutionRPC(t, momServiceEnableAsynchronousDelivery)
	req := r.request(t, momServiceEnableAsynchronousDelivery, r.f.target, nil)
	r.publish(t, req)
	req.Parameters[999] = []byte{1}
	if err := r.send(req, r.senderToken); err == nil {
		t.Fatal("undefined parameter 999 accepted on real RPC")
	}
	if r.f.time.Metrics("catalog-provider", r.f.target).AsynchronousDelivery {
		t.Fatal("undefined parameter caused a target side effect")
	}
}
