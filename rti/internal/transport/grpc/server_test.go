package grpc

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// stubFedStoreForServerTest is the smallest core.FederationStore that lets
// the compose tests exercise NewServer's required-options validation
// without dragging in the real federation manager.
type stubFedStoreForServerTest struct{}

func (stubFedStoreForServerTest) CreateFederation(_ context.Context, _ core.CreateFederationRequest) error {
	return nil
}
func (stubFedStoreForServerTest) DestroyFederation(_ context.Context, _ core.FederationName) error {
	return nil
}
func (stubFedStoreForServerTest) JoinFederation(_ context.Context, _ core.JoinFederationRequest) (core.FederateHandle, error) {
	return 0, nil
}
func (stubFedStoreForServerTest) ResignFederation(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ResignAction) error {
	return nil
}
func (stubFedStoreForServerTest) List(_ context.Context) ([]core.FederationSummary, error) {
	return nil, nil
}
func (stubFedStoreForServerTest) Snapshot() []core.FederationRoster { return nil }
func (stubFedStoreForServerTest) ListMembers(_ core.FederationName) []core.FederationMember {
	return nil
}

type stubObjRegistryForServerTest struct{}

func (stubObjRegistryForServerTest) Register(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectClassHandle, _ string) (core.ObjectHandle, string, error) {
	return 0, "", errors.New("stub")
}
func (stubObjRegistryForServerTest) UpdateAttributes(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ map[core.AttributeHandle][]byte, _ *core.LogicalTime) error {
	return errors.New("stub")
}
func (stubObjRegistryForServerTest) SendInteraction(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle, _ map[core.ParameterHandle][]byte, _ *core.LogicalTime) error {
	return errors.New("stub")
}
func (stubObjRegistryForServerTest) Delete(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ *core.LogicalTime, _ []byte) error {
	return errors.New("stub")
}
func (stubObjRegistryForServerTest) LocalDelete(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle) error {
	return errors.New("stub")
}
func (stubObjRegistryForServerTest) RequestAttributeValueUpdate(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ []core.AttributeHandle, _ []byte) error {
	return errors.New("stub")
}
func (stubObjRegistryForServerTest) RequestClassAttributeValueUpdate(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectClassHandle, _ []core.AttributeHandle, _ []byte) error {
	return errors.New("stub")
}
func (stubObjRegistryForServerTest) ChangeAttributeTransportType(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ []core.AttributeHandle, _ core.TransportType) error {
	return errors.New("stub")
}
func (stubObjRegistryForServerTest) ChangeInteractionTransportType(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle, _ core.TransportType) error {
	return errors.New("stub")
}
func (stubObjRegistryForServerTest) Snapshot(_ core.FederationName) core.ObjectSnapshot {
	return core.ObjectSnapshot{}
}
func (stubObjRegistryForServerTest) UpdateAttributesRetractable(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ map[core.AttributeHandle][]byte, _ *core.LogicalTime, _ uint64) error {
	return errors.New("stub")
}
func (stubObjRegistryForServerTest) SendInteractionRetractable(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle, _ map[core.ParameterHandle][]byte, _ *core.LogicalTime, _ uint64) error {
	return errors.New("stub")
}
func (stubObjRegistryForServerTest) RetractMessage(_ core.FederationName, _ core.FederateHandle, _ uint64) int {
	return 0
}

type stubOutboxForServerTest struct{}

func (stubOutboxForServerTest) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.OutboundEvent) error {
	return nil
}

func validOpts() Options {
	return Options{
		Federations:  stubFedStoreForServerTest{},
		Declarations: declaration.New(),
		Objects:      stubObjRegistryForServerTest{},
		Outbox:       stubOutboxForServerTest{},
	}
}

func TestNewServer_RequiresFederations(t *testing.T) {
	o := validOpts()
	o.Federations = nil
	srv, err := NewServer(o)
	if err == nil {
		t.Fatalf("want error for nil Federations, got nil; srv=%v", srv)
	}
	if !errors.Is(err, ErrFederationsRequired) {
		t.Errorf("want ErrFederationsRequired, got %v", err)
	}
	if srv != nil {
		t.Errorf("want nil server on error, got %v", srv)
	}
}

func TestNewServer_RequiresDeclarations(t *testing.T) {
	o := validOpts()
	o.Declarations = nil
	srv, err := NewServer(o)
	if err == nil {
		t.Fatalf("want error for nil Declarations, got nil; srv=%v", srv)
	}
	if !errors.Is(err, ErrDeclarationsRequired) {
		t.Errorf("want ErrDeclarationsRequired, got %v", err)
	}
	if srv != nil {
		t.Errorf("want nil server on error, got %v", srv)
	}
}

func TestNewServer_RequiresObjects(t *testing.T) {
	o := validOpts()
	o.Objects = nil
	srv, err := NewServer(o)
	if err == nil {
		t.Fatalf("want error for nil Objects, got nil; srv=%v", srv)
	}
	if !errors.Is(err, ErrObjectsRequired) {
		t.Errorf("want ErrObjectsRequired, got %v", err)
	}
	if srv != nil {
		t.Errorf("want nil server on error, got %v", srv)
	}
}

func TestNewServer_RequiresOutbox(t *testing.T) {
	o := validOpts()
	o.Outbox = nil
	srv, err := NewServer(o)
	if err == nil {
		t.Fatalf("want error for nil Outbox, got nil; srv=%v", srv)
	}
	if !errors.Is(err, ErrOutboxRequired) {
		t.Errorf("want ErrOutboxRequired, got %v", err)
	}
	if srv != nil {
		t.Errorf("want nil server on error, got %v", srv)
	}
}

func TestNewServer_AcceptsAllRequiredOptions(t *testing.T) {
	srv, err := NewServer(validOpts())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srv == nil {
		t.Fatal("want non-nil server, got nil")
	}
	if srv.fedService == nil {
		t.Error("fedService should be initialized")
	}
	if srv.declService == nil {
		t.Error("declService should be initialized")
	}
	if srv.objService == nil {
		t.Error("objService should be initialized")
	}
	if srv.streamService == nil {
		t.Error("streamService should be initialized")
	}
	if srv.timeService != nil {
		t.Error("timeService should be nil at M2 (Time deferred to M3)")
	}
}

// TestRegister_RejectsNonRegistrar asserts the Register guard against
// values that do not satisfy grpc.ServiceRegistrar.
func TestRegister_RejectsNonRegistrar(t *testing.T) {
	srv, err := NewServer(validOpts())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Register("not a grpc server"); err == nil {
		t.Error("want error registering against non-ServiceRegistrar, got nil")
	}
}

// TestRegister_AttachesToRealGRPCServer composes a real *grpc.Server and
// asserts Register completes without error. Sibling services (declaration,
// object, stream) are nil-guarded in this branch — see server.go for the
// W3B/W3C coordination contract.
func TestRegister_AttachesToRealGRPCServer(t *testing.T) {
	srv, err := NewServer(validOpts())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	gs := grpc.NewServer()
	defer gs.Stop()
	if err := srv.Register(gs); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Federation service must be in the registered list. The other
	// services in this branch are stub shells without RegisterFederationXxx
	// shims, so we only assert on the federation handler here.
	info := gs.GetServiceInfo()
	if _, ok := info["rti.v1.FederationService"]; !ok {
		t.Errorf("FederationService not registered; got services=%v", keys(info))
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

type stubMembershipValidator struct {
	valid map[core.FederationName]core.FederateHandle
}

func (v stubMembershipValidator) ValidateMember(fed core.FederationName, h core.FederateHandle) error {
	if v.valid[fed] != h {
		return core.ErrFederateNotJoined
	}
	return nil
}

func (stubMembershipValidator) GenerationFor(core.FederationName) (uint64, bool) { return 0, true }

func TestMembershipInterceptorRejectsStaleHandleBeforeHandler(t *testing.T) {
	opts := validOpts()
	opts.Membership = stubMembershipValidator{valid: map[core.FederationName]core.FederateHandle{"fed": 9}}
	srv, err := NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	handler := func(context.Context, any) (any, error) {
		called = true
		return &rtiv1.Empty{}, nil
	}
	interceptor := srv.UnaryMembershipInterceptor()
	_, err = interceptor(context.Background(), &rtiv1.SendInteractionRequest{
		FederationName: "fed", FederateHandle: 1,
	}, &grpc.UnaryServerInfo{}, handler)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale handle status = %v, want FailedPrecondition", status.Code(err))
	}
	if called {
		t.Fatal("handler ran for stale generation handle")
	}
	_, err = interceptor(context.Background(), &rtiv1.SendInteractionRequest{
		FederationName: "fed", FederateHandle: 9,
	}, &grpc.UnaryServerInfo{}, handler)
	if err != nil || !called {
		t.Fatalf("current member call = (%v, called=%v)", err, called)
	}
}

func TestMembershipInterceptorDoesNotTreatMOMQueryTargetAsCaller(t *testing.T) {
	opts := validOpts()
	opts.Membership = stubMembershipValidator{valid: map[core.FederationName]core.FederateHandle{"fed": 9}}
	srv, err := NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	_, err = srv.UnaryMembershipInterceptor()(
		context.Background(),
		&rtiv1.QueryFederateAttributesRequest{FederationName: "fed", FederateHandle: 99999},
		&grpc.UnaryServerInfo{FullMethod: "/rti.v1.MomService/QueryFederateAttributes"},
		func(context.Context, any) (any, error) {
			called = true
			return &rtiv1.QueryFederateAttributesResponse{Found: false}, nil
		},
	)
	if err != nil || !called {
		t.Fatalf("MOM target lookup = (%v, called=%v)", err, called)
	}
}

type leaseMembershipValidator struct {
	valid              core.FederateHandle
	acquired, released int
}

func (v *leaseMembershipValidator) ValidateMember(_ core.FederationName, h core.FederateHandle) error {
	if h != v.valid {
		return core.ErrFederateNotJoined
	}
	return nil
}

func (*leaseMembershipValidator) GenerationFor(core.FederationName) (uint64, bool) { return 1, true }

func (v *leaseMembershipValidator) AcquireMember(fed core.FederationName, h core.FederateHandle) (func(), error) {
	if err := v.ValidateMember(fed, h); err != nil {
		return nil, err
	}
	v.acquired++
	return func() { v.released++ }, nil
}

type scriptedMembershipStream struct {
	ctx      context.Context
	requests []*rtiv1.SendInteractionRequest
}

func (*scriptedMembershipStream) SetHeader(metadata.MD) error  { return nil }
func (*scriptedMembershipStream) SendHeader(metadata.MD) error { return nil }
func (*scriptedMembershipStream) SetTrailer(metadata.MD)       {}
func (s *scriptedMembershipStream) Context() context.Context   { return s.ctx }
func (*scriptedMembershipStream) SendMsg(any) error            { return nil }
func (s *scriptedMembershipStream) RecvMsg(message any) error {
	request := s.requests[0]
	s.requests = s.requests[1:]
	*(message.(*rtiv1.SendInteractionRequest)) = *request
	return nil
}

func TestP0InteractionStreamValidatesEveryMessageAndHoldsLeaseToAck(t *testing.T) {
	validator := &leaseMembershipValidator{valid: 9}
	base := &scriptedMembershipStream{ctx: context.Background(), requests: []*rtiv1.SendInteractionRequest{
		{},
		{FederationName: "fed", FederateHandle: 9},
		{FederationName: "fed", FederateHandle: 1},
	}}
	stream := &membershipServerStream{
		ServerStream: base, server: &Server{membership: validator}, interaction: true,
	}
	if err := stream.RecvMsg(new(rtiv1.SendInteractionRequest)); err != nil {
		t.Fatal(err)
	}
	if validator.acquired != 0 {
		t.Fatal("capability handshake acquired membership lease")
	}
	if err := stream.RecvMsg(new(rtiv1.SendInteractionRequest)); err != nil {
		t.Fatal(err)
	}
	if validator.acquired != 1 || validator.released != 0 {
		t.Fatalf("lease before ACK = acquired %d released %d", validator.acquired, validator.released)
	}
	if err := stream.SendMsg(&rtiv1.Empty{}); err != nil {
		t.Fatal(err)
	}
	if validator.released != 1 {
		t.Fatalf("lease releases after ACK = %d, want 1", validator.released)
	}
	if err := stream.RecvMsg(new(rtiv1.SendInteractionRequest)); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale second message status = %v, want FailedPrecondition", status.Code(err))
	}
}
