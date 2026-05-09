package grpc

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
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
func (stubObjRegistryForServerTest) Snapshot(_ core.FederationName) core.ObjectSnapshot {
	return core.ObjectSnapshot{}
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
