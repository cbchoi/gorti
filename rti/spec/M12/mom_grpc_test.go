package m12spec

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/mom"
	grpcsvc "github.com/cbchoi/gorti/rti/internal/transport/grpc"
)

// momHarness composes a *mom.Manager + grpcsvc.Server with the
// MomService wired, exposing a real gRPC client to the test body.
//
// Mirrors newM12Harness for shape; the federate-port-side MomService
// is the only handler under test here so the harness does not stand
// up sync / ownership / ddm / savepoint to keep the test focused.
type momHarness struct {
	mgr     *mom.Manager
	server  *grpc.Server
	conn    *grpc.ClientConn
	client  rtiv1.MomServiceClient
	cleanup func()
}

func newMomHarness(t *testing.T) *momHarness {
	t.Helper()
	outbox := &fakeOutbox{}

	mgr, err := mom.New(mom.Options{Outbox: outbox})
	if err != nil {
		t.Fatalf("mom.New: %v", err)
	}

	srv, err := grpcsvc.NewServer(grpcsvc.Options{
		Federations:  stubFedStore{},
		Declarations: declaration.New(),
		Objects:      stubObjRegistry{},
		Outbox:       outbox,
		MOM:          mgr,
	})
	if err != nil {
		t.Fatalf("grpcsvc.NewServer: %v", err)
	}

	gs := grpc.NewServer()
	if err := srv.Register(gs); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	go func() { _ = gs.Serve(ln) }()

	conn, err := grpc.NewClient(ln.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		gs.GracefulStop()
		_ = ln.Close()
		t.Fatalf("grpc.NewClient: %v", err)
	}

	h := &momHarness{
		mgr:    mgr,
		server: gs,
		conn:   conn,
		client: rtiv1.NewMomServiceClient(conn),
	}
	h.cleanup = func() {
		_ = conn.Close()
		gs.GracefulStop()
		_ = ln.Close()
	}
	return h
}

// TestSpec_M12_MomService_QueryFederationAttributes_GRPCRoundTrip:
// after the runtime registers a federation + joins one federate via
// the MOM lifecycle hooks, QueryFederationAttributes returns the
// canonical snapshot — name, federate handle list, FOM module names
// — over the wire.
//
// Implements: M12 W3 — MOM gRPC exposure (federation snapshot).
func TestSpec_M12_MomService_QueryFederationAttributes_GRPCRoundTrip(t *testing.T) {
	h := newMomHarness(t)
	defer h.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const fedName = "mom-alpha"

	// Drive the runtime hooks the way cmd/rtid does. The MOM handler
	// itself is read-only; lifecycle changes flow through the
	// FederationCreated / FederateJoined hooks (MOM is wired off
	// federation.Manager success callbacks in production).
	if err := h.mgr.FederationCreated(ctx, fedName, []core.FOMModule{
		{Path: "alpha.fom.xml"},
	}); err != nil {
		t.Fatalf("FederationCreated: %v", err)
	}
	if err := h.mgr.FederateJoined(ctx, fedName, 1, "fed-a", "Producer"); err != nil {
		t.Fatalf("FederateJoined: %v", err)
	}
	if err := h.mgr.FederateJoined(ctx, fedName, 2, "fed-b", "Consumer"); err != nil {
		t.Fatalf("FederateJoined: %v", err)
	}

	// Query over the wire.
	resp, err := h.client.QueryFederationAttributes(ctx,
		&rtiv1.QueryFederationAttributesRequest{
			WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName: fedName,
		})
	if err != nil {
		t.Fatalf("QueryFederationAttributes: %v", err)
	}
	if got, want := resp.GetFederationName(), fedName; got != want {
		t.Errorf("federation_name=%q, want %q", got, want)
	}
	if got, want := resp.GetFederateHandles(), []uint64{1, 2}; !equalUint64s(got, want) {
		t.Errorf("federate_handles=%v, want %v", got, want)
	}
	if got, want := resp.GetFomModuleNames(), []string{"alpha.fom.xml"}; !equalStrings(got, want) {
		t.Errorf("fom_module_names=%v, want %v", got, want)
	}

	// Unknown federation: empty lists, federation_name echoed.
	resp2, err := h.client.QueryFederationAttributes(ctx,
		&rtiv1.QueryFederationAttributesRequest{
			WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName: "no-such-fed",
		})
	if err != nil {
		t.Fatalf("QueryFederationAttributes(unknown): %v", err)
	}
	if len(resp2.GetFederateHandles()) != 0 || len(resp2.GetFomModuleNames()) != 0 {
		t.Errorf("unknown-fed response should have empty lists, got handles=%v fom=%v",
			resp2.GetFederateHandles(), resp2.GetFomModuleNames())
	}
}

// TestSpec_M12_MomService_QueryFederateAttributes_GRPCRoundTrip:
// after the runtime registers a federation + joins one federate +
// fires three IncrementInteractionsSent calls, the per-federate query
// returns the right counters over the wire and the federate name /
// type / handle round-trip cleanly.
//
// Implements: M12 W3 — MOM gRPC exposure (per-federate snapshot +
// counters).
func TestSpec_M12_MomService_QueryFederateAttributes_GRPCRoundTrip(t *testing.T) {
	h := newMomHarness(t)
	defer h.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const fedName = "mom-beta"
	const fedHandle = core.FederateHandle(7)

	if err := h.mgr.FederationCreated(ctx, fedName, nil); err != nil {
		t.Fatalf("FederationCreated: %v", err)
	}
	if err := h.mgr.FederateJoined(ctx, fedName, fedHandle, "metric-fed", "Sensor"); err != nil {
		t.Fatalf("FederateJoined: %v", err)
	}

	// Drive a few counter increments the way the dispatcher fan-out
	// would. Each IncrementX call is what the object.Registry calls
	// off the OnInteractionSent / OnInteractionDelivered / OnUpdateSent
	// / OnReflectDelivered hooks.
	for i := 0; i < 3; i++ {
		h.mgr.IncrementInteractionsSent(fedName, fedHandle)
	}
	for i := 0; i < 5; i++ {
		h.mgr.IncrementUpdatesSent(fedName, fedHandle)
	}
	h.mgr.IncrementInteractionsReceived(fedName, fedHandle)
	h.mgr.IncrementInteractionsReceived(fedName, fedHandle)
	h.mgr.IncrementReflectionsReceived(fedName, fedHandle)

	resp, err := h.client.QueryFederateAttributes(ctx,
		&rtiv1.QueryFederateAttributesRequest{
			WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName: fedName,
			FederateHandle: uint64(fedHandle),
		})
	if err != nil {
		t.Fatalf("QueryFederateAttributes: %v", err)
	}
	if !resp.GetFound() {
		t.Fatalf("found=false; want true for tracked federate")
	}
	if got, want := resp.GetFederateHandle(), uint64(fedHandle); got != want {
		t.Errorf("federate_handle=%d, want %d", got, want)
	}
	if got, want := resp.GetFederateName(), "metric-fed"; got != want {
		t.Errorf("federate_name=%q, want %q", got, want)
	}
	if got, want := resp.GetFederateType(), "Sensor"; got != want {
		t.Errorf("federate_type=%q, want %q", got, want)
	}
	if got, want := resp.GetInteractionsSent(), uint32(3); got != want {
		t.Errorf("interactions_sent=%d, want %d", got, want)
	}
	if got, want := resp.GetInteractionsReceived(), uint32(2); got != want {
		t.Errorf("interactions_received=%d, want %d", got, want)
	}
	if got, want := resp.GetUpdatesSent(), uint32(5); got != want {
		t.Errorf("updates_sent=%d, want %d", got, want)
	}
	if got, want := resp.GetReflectionsReceived(), uint32(1); got != want {
		t.Errorf("reflections_received=%d, want %d", got, want)
	}

	// Unknown federate: found=false, remaining fields zero-valued.
	resp2, err := h.client.QueryFederateAttributes(ctx,
		&rtiv1.QueryFederateAttributesRequest{
			WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName: fedName,
			FederateHandle: 999,
		})
	if err != nil {
		t.Fatalf("QueryFederateAttributes(unknown): %v", err)
	}
	if resp2.GetFound() {
		t.Errorf("found=true for unknown federate; want false")
	}
}

// TestSpec_M12_MomService_EnumerateMomInstances_GRPCRoundTrip:
// EnumerateMomInstances returns the federation singleton followed by
// one HLAfederate per joined federate, ordered by federate-handle
// ascending.
//
// Implements: M12 W3 — MOM gRPC exposure (instance enumeration).
func TestSpec_M12_MomService_EnumerateMomInstances_GRPCRoundTrip(t *testing.T) {
	h := newMomHarness(t)
	defer h.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const fedName = "mom-gamma"

	if err := h.mgr.FederationCreated(ctx, fedName, nil); err != nil {
		t.Fatalf("FederationCreated: %v", err)
	}
	// Join in non-sorted handle order to assert the response sorts.
	if err := h.mgr.FederateJoined(ctx, fedName, 5, "fed-five", "T5"); err != nil {
		t.Fatalf("FederateJoined: %v", err)
	}
	if err := h.mgr.FederateJoined(ctx, fedName, 2, "fed-two", "T2"); err != nil {
		t.Fatalf("FederateJoined: %v", err)
	}
	if err := h.mgr.FederateJoined(ctx, fedName, 9, "fed-nine", "T9"); err != nil {
		t.Fatalf("FederateJoined: %v", err)
	}

	resp, err := h.client.EnumerateMomInstances(ctx,
		&rtiv1.EnumerateMomInstancesRequest{
			WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName: fedName,
		})
	if err != nil {
		t.Fatalf("EnumerateMomInstances: %v", err)
	}
	insts := resp.GetInstances()
	if len(insts) != 4 {
		t.Fatalf("len(instances)=%d, want 4 (1 federation + 3 federates); got %v",
			len(insts), insts)
	}
	if got, want := insts[0].GetClassName(), "HLAobjectRoot.HLAmanager.HLAfederation"; got != want {
		t.Errorf("instances[0].class_name=%q, want %q", got, want)
	}
	if got, want := insts[0].GetInstanceName(), fedName; got != want {
		t.Errorf("instances[0].instance_name=%q, want %q", got, want)
	}
	// Sorted-by-handle: 2, 5, 9.
	wantOrder := []struct {
		handle uint64
		name   string
	}{
		{2, "fed-two"},
		{5, "fed-five"},
		{9, "fed-nine"},
	}
	for i, want := range wantOrder {
		got := insts[i+1]
		if got.GetClassName() != "HLAobjectRoot.HLAmanager.HLAfederate" {
			t.Errorf("instances[%d].class_name=%q, want HLAfederate", i+1, got.GetClassName())
		}
		if got.GetFederateHandle() != want.handle {
			t.Errorf("instances[%d].federate_handle=%d, want %d", i+1, got.GetFederateHandle(), want.handle)
		}
		if got.GetInstanceName() != want.name {
			t.Errorf("instances[%d].instance_name=%q, want %q", i+1, got.GetInstanceName(), want.name)
		}
	}

	// Unknown federation: empty list (not an error).
	resp2, err := h.client.EnumerateMomInstances(ctx,
		&rtiv1.EnumerateMomInstancesRequest{
			WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName: "no-such-fed",
		})
	if err != nil {
		t.Fatalf("EnumerateMomInstances(unknown): %v", err)
	}
	if len(resp2.GetInstances()) != 0 {
		t.Errorf("unknown-fed enumerate should be empty, got %d instances", len(resp2.GetInstances()))
	}
}

// equalUint64s / equalStrings keep the tests free of go-cmp without
// pulling reflect into a hot test path. Spec tests are not perf-bound;
// the explicit form keeps the failure messages line-accurate.
func equalUint64s(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
