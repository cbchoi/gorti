package m19spec

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/federation"
)

// stubFOMRepo accepts any FOM modules so tests can focus on the
// transport-mode plumbing rather than FOM validation.
type stubFOMRepo struct{}

func (stubFOMRepo) Load(_ context.Context, _ []core.FOMModule) (core.FOMHandle, error) {
	return stubFOMHandle{}, nil
}

func (stubFOMRepo) Get(_ context.Context, _ core.FederationName) (core.FOMHandle, error) {
	return stubFOMHandle{}, nil
}

type stubFOMHandle struct{}

func (stubFOMHandle) IsValid() bool                                                  { return true }
func (stubFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool)        { return 1, true }
func (stubFOMHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 1, true
}
func (stubFOMHandle) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (stubFOMHandle) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}

func newM19Manager(t *testing.T) *federation.Manager {
	t.Helper()
	mgr, err := federation.New(federation.Options{
		Clock: core.NewRealClock(),
		FOMs:  stubFOMRepo{},
	})
	if err != nil {
		t.Fatalf("federation.New: %v", err)
	}
	return mgr
}

// TestManagerRecordsTransportMode asserts CreateFederation persists the
// requested TransportMode + DDSDomainID and TransportFor reads it back.
func TestManagerRecordsTransportMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := newM19Manager(t)

	// GRPC mode (the cut-2 default).
	if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{
		Name:          "fed-grpc",
		TransportMode: core.TransportModeGRPC,
	}); err != nil {
		t.Fatalf("CreateFederation grpc: %v", err)
	}
	tm, dom, ok := mgr.TransportFor("fed-grpc")
	if !ok {
		t.Fatal("TransportFor(fed-grpc): not found")
	}
	if tm != core.TransportModeGRPC {
		t.Errorf("transport=%v; want GRPC", tm)
	}
	if dom != 0 {
		t.Errorf("dds_domain=%d; want 0 for GRPC mode", dom)
	}

	// DDS mode with non-zero domain ID.
	if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{
		Name:          "fed-dds",
		TransportMode: core.TransportModeDDS,
		DDSDomainID:   42,
	}); err != nil {
		t.Fatalf("CreateFederation dds: %v", err)
	}
	tm, dom, ok = mgr.TransportFor("fed-dds")
	if !ok {
		t.Fatal("TransportFor(fed-dds): not found")
	}
	if tm != core.TransportModeDDS {
		t.Errorf("transport=%v; want DDS", tm)
	}
	if dom != 42 {
		t.Errorf("dds_domain=%d; want 42", dom)
	}

	// Unspecified collapses to GRPC.
	if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{
		Name: "fed-default",
	}); err != nil {
		t.Fatalf("CreateFederation default: %v", err)
	}
	tm, _, ok = mgr.TransportFor("fed-default")
	if !ok {
		t.Fatal("TransportFor(fed-default): not found")
	}
	if tm != core.TransportModeGRPC {
		t.Errorf("default transport=%v; want GRPC (UNSPECIFIED collapses)", tm)
	}
}

// TestSnapshotSurfacesTransportMode asserts the FederationRoster
// returned by Snapshot carries the transport_mode + dds_domain_id so
// the rtid-TUI / AdminService can render the per-federation header.
func TestSnapshotSurfacesTransportMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := newM19Manager(t)

	if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{
		Name:          "fed-dds",
		TransportMode: core.TransportModeDDS,
		DDSDomainID:   17,
	}); err != nil {
		t.Fatalf("CreateFederation: %v", err)
	}

	rosters := mgr.Snapshot()
	if len(rosters) != 1 {
		t.Fatalf("Snapshot: got %d rosters; want 1", len(rosters))
	}
	r := rosters[0]
	if r.TransportMode != core.TransportModeDDS {
		t.Errorf("roster.TransportMode=%v; want DDS", r.TransportMode)
	}
	if r.DDSDomainID != 17 {
		t.Errorf("roster.DDSDomainID=%d; want 17", r.DDSDomainID)
	}
}

// TestTransportForUnknown returns the conventional "not found" tuple
// for an unknown federation. Callers (the gRPC handler) rely on this
// so a join against a non-existent federation surfaces a clean
// not-found error rather than a misleading transport response.
func TestTransportForUnknown(t *testing.T) {
	t.Parallel()
	mgr := newM19Manager(t)
	tm, dom, ok := mgr.TransportFor("missing")
	if ok {
		t.Errorf("TransportFor(missing) ok=true; want false")
	}
	if tm != core.TransportModeUnspecified {
		t.Errorf("TransportFor(missing) tm=%v; want UNSPECIFIED", tm)
	}
	if dom != 0 {
		t.Errorf("TransportFor(missing) dom=%d; want 0", dom)
	}
}
