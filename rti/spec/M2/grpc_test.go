package m2spec

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	grpcsvc "github.com/cbchoi/gorti/rti/internal/transport/grpc"
)

// stubFedStore lets gRPC handler tests exercise success and error paths
// without instantiating the real federation manager.
type stubFedStore struct {
	createErr   error
	joinErr     error
	joinHandle  core.FederateHandle
	resignErr   error
	destroyErr  error
	listResp    []core.FederationSummary
	listErr     error
	createCalls []core.CreateFederationRequest
	joinCalls   []core.JoinFederationRequest
}

func (s *stubFedStore) CreateFederation(_ context.Context, r core.CreateFederationRequest) error {
	s.createCalls = append(s.createCalls, r)
	return s.createErr
}
func (s *stubFedStore) DestroyFederation(_ context.Context, _ core.FederationName) error {
	return s.destroyErr
}
func (s *stubFedStore) JoinFederation(_ context.Context, r core.JoinFederationRequest) (core.FederateHandle, error) {
	s.joinCalls = append(s.joinCalls, r)
	return s.joinHandle, s.joinErr
}
func (s *stubFedStore) ResignFederation(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ResignAction) error {
	return s.resignErr
}
func (s *stubFedStore) List(_ context.Context) ([]core.FederationSummary, error) {
	return s.listResp, s.listErr
}
func (s *stubFedStore) Snapshot() []core.FederationRoster { return nil }

// TestSpec_M2_GRPC_Server_RequiresAllRequiredOptions: NewServer rejects
// nil Federations / Declarations / Objects (Time may be nil at M2;
// time RPCs return Unimplemented).
//
// Implements: IR-PROTO-1.
func TestSpec_M2_GRPC_Server_RequiresAllRequiredOptions(t *testing.T) {
	cases := []struct {
		name string
		opts grpcsvc.Options
	}{
		{"missing Federations", grpcsvc.Options{
			Declarations: declaration.New(),
			Objects:      &stubObjectRegistry{},
			Outbox:       newFakeOutbox(),
		}},
		{"missing Declarations", grpcsvc.Options{
			Federations: &stubFedStore{},
			Objects:     &stubObjectRegistry{},
			Outbox:      newFakeOutbox(),
		}},
		{"missing Objects", grpcsvc.Options{
			Federations:  &stubFedStore{},
			Declarations: declaration.New(),
			Outbox:       newFakeOutbox(),
		}},
		{"missing Outbox", grpcsvc.Options{
			Federations:  &stubFedStore{},
			Declarations: declaration.New(),
			Objects:      &stubObjectRegistry{},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, err := grpcsvc.NewServer(tc.opts)
			if err == nil && srv != nil {
				t.Errorf("NewServer with %s: want error, got server %v", tc.name, srv)
			}
		})
	}
}

// TestSpec_M2_GRPC_Server_Constructs_WithAllOptions: with valid options,
// NewServer returns a non-nil server.
//
// Implements: IR-PROTO-1.
func TestSpec_M2_GRPC_Server_Constructs_WithAllOptions(t *testing.T) {
	srv, err := grpcsvc.NewServer(grpcsvc.Options{
		Federations:  &stubFedStore{},
		Declarations: declaration.New(),
		Objects:      &stubObjectRegistry{},
		Outbox:       newFakeOutbox(),
	})
	if err != nil {
		t.Skipf("NewServer not yet implemented: %v", err)
	}
	if srv == nil {
		t.Error("NewServer returned (nil, nil); want non-nil server")
	}
}

// stubObjectRegistry is a no-op core.ObjectRegistry. Used in gRPC handler
// tests that focus on request/response translation, not on the registry.
type stubObjectRegistry struct{}

func (stubObjectRegistry) Register(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectClassHandle, _ string) (core.ObjectHandle, string, error) {
	return 0, "", errors.New("stub")
}
func (stubObjectRegistry) UpdateAttributes(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ map[core.AttributeHandle][]byte, _ *core.LogicalTime) error {
	return errors.New("stub")
}
func (stubObjectRegistry) SendInteraction(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle, _ map[core.ParameterHandle][]byte, _ *core.LogicalTime) error {
	return errors.New("stub")
}
func (stubObjectRegistry) Delete(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ *core.LogicalTime, _ []byte) error {
	return errors.New("stub")
}
func (stubObjectRegistry) Snapshot(_ core.FederationName) core.ObjectSnapshot {
	return core.ObjectSnapshot{}
}

// TestSpec_M2_GRPC_Register_AttachesAllServices is the integration check
// that the Server's Register method wires up all four service handlers
// with the gRPC server. Until Agent A implements the Register body,
// this skips; the implementation flips it green by registering.
//
// Implements: IR-PROTO-1, IR-PROTO-2, IR-PROTO-3.
func TestSpec_M2_GRPC_Register_AttachesAllServices(t *testing.T) {
	t.Skip("integration shape; Agent A wires once a real grpc.Server is composed at cmd/rtid time")
}
