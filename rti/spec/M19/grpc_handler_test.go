package m19spec

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	"github.com/cbchoi/gorti/rti/internal/federation"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/object"
	grpcsvc "github.com/cbchoi/gorti/rti/internal/transport/grpc"
)

// minimalServer constructs a transport/grpc Server fronting a real
// federation.Manager. Used by the M19 handler tests to drive
// CreateFederation / JoinFederation directly through the gRPC service
// layer without spinning up a real network listener.
func minimalServer(t *testing.T, ddsEnabled bool, defaultDomain int32) (*grpcsvc.Server, *federation.Manager) {
	t.Helper()
	mgr := newM19Manager(t)

	// object.Registry needs a few wires; M19 spec tests only exercise
	// FederationService so the dependents are minimal placeholders.
	declMgr := declaration.New()
	outbox := &noopOutbox{}
	reg, err := object.New(object.Options{
		EventLog:     nil,
		Declarations: declMgr,
		Outbox:       outbox,
		FOMs:         stubFOMRepo{},
		Clock:        core.NewRealClock(),
		Federations:  mgr,
	})
	if err != nil {
		t.Fatalf("object.New: %v", err)
	}

	srv, err := grpcsvc.NewServer(grpcsvc.Options{
		Federations:        mgr,
		Declarations:       declMgr,
		Objects:            reg,
		Outbox:             outbox,
		DDSEnabled:         ddsEnabled,
		DDSDefaultDomainID: defaultDomain,
		TransportLookup:    mgr.TransportFor,
	})
	if err != nil {
		t.Fatalf("grpc.NewServer: %v", err)
	}
	return srv, mgr
}

// noopOutbox satisfies core.Outbox without doing anything; the M19
// federation tests never produce outbound events.
type noopOutbox struct{}

func (*noopOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.OutboundEvent) error {
	return nil
}

// TestCreateFederation_RejectsDDSWhenDisabled asserts the cut-2 default
// build refuses DDS-mode federations with FailedPrecondition + a clear
// "not built with DDS support" message. Critical regression guard:
// without this, a user could craft a CreateFederation that silently
// records DDS mode but never actually exposes a DDS data plane.
func TestCreateFederation_RejectsDDSWhenDisabled(t *testing.T) {
	t.Parallel()
	srv, _ := minimalServer(t, false, 0)
	fedSvc := grpcsvc.FederationServiceForTest(srv)

	_, err := fedSvc.CreateFederation(context.Background(), &rtiv1.CreateFederationRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "fed-dds",
		TransportMode:  rtiv1.TransportMode_TRANSPORT_MODE_DDS,
	})
	if err == nil {
		t.Fatal("CreateFederation transport_mode=DDS should be rejected when DDS is not enabled")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("err is not a gRPC status: %v", err)
	}
	if st.Code() != codes.FailedPrecondition {
		t.Errorf("got code=%v; want FailedPrecondition", st.Code())
	}
}

// TestCreateFederation_AcceptsDDSWhenEnabled asserts that --enable-dds
// unlocks DDS-mode CreateFederation at the gRPC layer (the actual
// participant lifecycle is Phase 1b; Phase 1a only verifies the gate).
func TestCreateFederation_AcceptsDDSWhenEnabled(t *testing.T) {
	t.Parallel()
	srv, mgr := minimalServer(t, true, 7)
	fedSvc := grpcsvc.FederationServiceForTest(srv)

	_, err := fedSvc.CreateFederation(context.Background(), &rtiv1.CreateFederationRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "fed-dds",
		TransportMode:  rtiv1.TransportMode_TRANSPORT_MODE_DDS,
	})
	if err != nil {
		t.Fatalf("CreateFederation: %v", err)
	}
	tm, dom, ok := mgr.TransportFor("fed-dds")
	if !ok {
		t.Fatal("TransportFor: federation not recorded")
	}
	if tm != core.TransportModeDDS {
		t.Errorf("transport=%v; want DDS", tm)
	}
	if dom != 7 {
		t.Errorf("domain=%d; want 7 (the rtid default)", dom)
	}
}

// TestJoinFederation_EchoesTransportMode asserts the federate sees
// the federation's recorded mode + DDS domain ID on JoinFederation,
// so the SDK can pick the right wire path.
func TestJoinFederation_EchoesTransportMode(t *testing.T) {
	t.Parallel()
	srv, _ := minimalServer(t, true, 13)
	fedSvc := grpcsvc.FederationServiceForTest(srv)
	ctx := context.Background()

	if _, err := fedSvc.CreateFederation(ctx, &rtiv1.CreateFederationRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "fed-dds",
		TransportMode:  rtiv1.TransportMode_TRANSPORT_MODE_DDS,
	}); err != nil {
		t.Fatalf("CreateFederation: %v", err)
	}
	resp, err := fedSvc.JoinFederation(ctx, &rtiv1.JoinFederationRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "fed-dds",
		FederateName:   "f1",
	})
	if err != nil {
		t.Fatalf("JoinFederation: %v", err)
	}
	if resp.GetTransportMode() != rtiv1.TransportMode_TRANSPORT_MODE_DDS {
		t.Errorf("response transport_mode=%v; want DDS", resp.GetTransportMode())
	}
	if resp.GetDdsDomainId() != 13 {
		t.Errorf("response dds_domain_id=%d; want 13", resp.GetDdsDomainId())
	}
}

// TestJoinFederation_LegacyGRPCDefault asserts a federation created
// without setting transport_mode (the cut-2 default path) reports
// GRPC on JoinFederation. The append-only contract.
func TestJoinFederation_LegacyGRPCDefault(t *testing.T) {
	t.Parallel()
	srv, _ := minimalServer(t, false, 0)
	fedSvc := grpcsvc.FederationServiceForTest(srv)
	ctx := context.Background()

	if _, err := fedSvc.CreateFederation(ctx, &rtiv1.CreateFederationRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "fed-legacy",
	}); err != nil {
		t.Fatalf("CreateFederation: %v", err)
	}
	resp, err := fedSvc.JoinFederation(ctx, &rtiv1.JoinFederationRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "fed-legacy",
		FederateName:   "f1",
	})
	if err != nil {
		t.Fatalf("JoinFederation: %v", err)
	}
	if resp.GetTransportMode() != rtiv1.TransportMode_TRANSPORT_MODE_GRPC {
		t.Errorf("legacy response transport_mode=%v; want GRPC", resp.GetTransportMode())
	}
}
