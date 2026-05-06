// MutatingService unit tests — verify Probe shape, ForceResign +
// DestroyFederation reuse the FederationStore primitives (so the
// eventlog + MOM hooks fire identically to the federate-initiated
// path), and the various error / idempotency edges.
//
// Owner: Agent A — rtid-TUI Phase 5.

package grpc

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// recordingFedStore captures the calls the mutating handler makes so
// tests can assert the manager primitives were exercised in the
// expected order. It satisfies core.FederationStore enough for the
// Phase-5 handler — federate-side methods (CreateFederation /
// JoinFederation / List) are unused so they panic if the test
// accidentally exercises a non-mutating path.
type recordingFedStore struct {
	rosters []core.FederationRoster

	resignCalls  []recordedResign
	destroyCalls []core.FederationName

	resignErr  map[core.FederateHandle]error
	destroyErr error
}

type recordedResign struct {
	Fed    core.FederationName
	Handle core.FederateHandle
	Action core.ResignAction
}

func (s *recordingFedStore) ResignFederation(_ context.Context, fed core.FederationName, h core.FederateHandle, action core.ResignAction) error {
	s.resignCalls = append(s.resignCalls, recordedResign{Fed: fed, Handle: h, Action: action})
	if err, ok := s.resignErr[h]; ok {
		return err
	}
	return nil
}

func (s *recordingFedStore) DestroyFederation(_ context.Context, name core.FederationName) error {
	s.destroyCalls = append(s.destroyCalls, name)
	return s.destroyErr
}

func (s *recordingFedStore) Snapshot() []core.FederationRoster { return s.rosters }

// Unused federate-path methods.
func (*recordingFedStore) CreateFederation(_ context.Context, _ core.CreateFederationRequest) error {
	panic("CreateFederation should not be called by Phase-5 handler tests")
}
func (*recordingFedStore) JoinFederation(_ context.Context, _ core.JoinFederationRequest) (core.FederateHandle, error) {
	panic("JoinFederation should not be called by Phase-5 handler tests")
}
func (*recordingFedStore) List(_ context.Context) ([]core.FederationSummary, error) {
	panic("List should not be called by Phase-5 handler tests")
}

// --- Probe -----------------------------------------------------------------

func TestMutatingService_Probe_ReturnsVersionAndEnabled(t *testing.T) {
	t.Parallel()
	svc := newMutatingService(MutatingOptions{Version: "v0.test"})
	resp, err := svc.Probe(context.Background(), &rtiv1.MutatingProbeRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1,
	})
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if resp.GetRtidVersion() != "v0.test" {
		t.Errorf("rtid_version: got %q want %q", resp.GetRtidVersion(), "v0.test")
	}
	if !resp.GetMutatingEnabled() {
		t.Errorf("mutating_enabled: got false; want true")
	}
}

// --- ForceResign -----------------------------------------------------------

// Happy path: handler delegates to FederationStore.ResignFederation
// with the recorded handle. The wire-side reply has
// already_resigned=false (federate was actually evicted).
func TestMutatingService_ForceResign_DelegatesToFederationStore(t *testing.T) {
	t.Parallel()
	store := &recordingFedStore{}
	svc := newMutatingService(MutatingOptions{Federations: store})
	resp, err := svc.ForceResign(context.Background(), &rtiv1.ForceResignRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "demo",
		FederateHandle: 42,
	})
	if err != nil {
		t.Fatalf("ForceResign: %v", err)
	}
	if resp.GetAlreadyResigned() {
		t.Errorf("already_resigned: got true; want false")
	}
	if len(store.resignCalls) != 1 {
		t.Fatalf("resignCalls: got %d want 1", len(store.resignCalls))
	}
	got := store.resignCalls[0]
	if got.Fed != "demo" || got.Handle != 42 || got.Action != core.ResignActionUnconditionallyDivestAttributes {
		t.Errorf("delegate args: got %+v", got)
	}
}

// Idempotency: ResignFederation returns ErrFederateNotJoined → the
// handler reports already_resigned=true rather than failing.
func TestMutatingService_ForceResign_AlreadyResignedIsIdempotent(t *testing.T) {
	t.Parallel()
	store := &recordingFedStore{
		resignErr: map[core.FederateHandle]error{42: core.ErrFederateNotJoined},
	}
	svc := newMutatingService(MutatingOptions{Federations: store})
	resp, err := svc.ForceResign(context.Background(), &rtiv1.ForceResignRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "demo",
		FederateHandle: 42,
	})
	if err != nil {
		t.Fatalf("ForceResign: %v", err)
	}
	if !resp.GetAlreadyResigned() {
		t.Errorf("already_resigned: got false; want true on ErrFederateNotJoined")
	}
}

// Validation: zero handle is rejected with InvalidArgument.
func TestMutatingService_ForceResign_RejectsZeroHandle(t *testing.T) {
	t.Parallel()
	svc := newMutatingService(MutatingOptions{Federations: &recordingFedStore{}})
	_, err := svc.ForceResign(context.Background(), &rtiv1.ForceResignRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "demo",
	})
	if err == nil {
		t.Fatalf("ForceResign zero handle: want error, got nil")
	}
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Errorf("error code: got %v want InvalidArgument", got)
	}
}

// FailedPrecondition when Federations source is not wired (composition
// root contract violation).
func TestMutatingService_ForceResign_NoFederationsSource(t *testing.T) {
	t.Parallel()
	svc := newMutatingService(MutatingOptions{})
	_, err := svc.ForceResign(context.Background(), &rtiv1.ForceResignRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "demo",
		FederateHandle: 1,
	})
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Errorf("error code: got %v want FailedPrecondition", got)
	}
}

// --- DestroyFederation -----------------------------------------------------

// Without evict: handler delegates straight to DestroyFederation; if
// the manager refuses (joined federates), the error is propagated.
func TestMutatingService_DestroyFederation_NoEvict_DelegatesAndPropagates(t *testing.T) {
	t.Parallel()
	store := &recordingFedStore{
		destroyErr: core.ErrFederationHasFederatesJoined,
	}
	svc := newMutatingService(MutatingOptions{Federations: store})
	_, err := svc.DestroyFederation(context.Background(), &rtiv1.AdminDestroyFederationRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "demo",
	})
	if err == nil {
		t.Fatalf("DestroyFederation: want propagated error, got nil")
	}
	if len(store.resignCalls) != 0 {
		t.Errorf("expected zero ResignFederation calls when evict_joined_federates=false; got %d", len(store.resignCalls))
	}
	if len(store.destroyCalls) != 1 {
		t.Errorf("destroyCalls: got %d want 1", len(store.destroyCalls))
	}
}

// With evict: handler first walks the roster, ResignFederation each
// joined federate, then DestroyFederation. evicted_handles surfaces
// the kicked federates.
func TestMutatingService_DestroyFederation_EvictsThenDestroys(t *testing.T) {
	t.Parallel()
	store := &recordingFedStore{
		rosters: []core.FederationRoster{
			{
				Name: core.FederationName("demo"),
				Mode: core.ModeVerbose,
				Federates: []core.FederateInfo{
					{Handle: 1, Name: "alice"},
					{Handle: 2, Name: "bob"},
				},
			},
		},
	}
	svc := newMutatingService(MutatingOptions{Federations: store})
	resp, err := svc.DestroyFederation(context.Background(), &rtiv1.AdminDestroyFederationRequest{
		WireVersion:           rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:        "demo",
		EvictJoinedFederates:  true,
	})
	if err != nil {
		t.Fatalf("DestroyFederation: %v", err)
	}
	if got := len(store.resignCalls); got != 2 {
		t.Errorf("resignCalls: got %d want 2", got)
	}
	if got := len(store.destroyCalls); got != 1 {
		t.Errorf("destroyCalls: got %d want 1", got)
	}
	if got := resp.GetEvictedHandles(); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("evicted_handles: got %v want [1 2]", got)
	}
}

// Evict path tolerates an already-resigned federate on its
// ResignFederation call (race between the operator's confirmation
// and a self-resigning federate).
func TestMutatingService_DestroyFederation_EvictTolerates_NotJoined(t *testing.T) {
	t.Parallel()
	store := &recordingFedStore{
		rosters: []core.FederationRoster{
			{
				Name: core.FederationName("demo"),
				Federates: []core.FederateInfo{
					{Handle: 1, Name: "alice"},
				},
			},
		},
		resignErr: map[core.FederateHandle]error{1: core.ErrFederateNotJoined},
	}
	svc := newMutatingService(MutatingOptions{Federations: store})
	if _, err := svc.DestroyFederation(context.Background(), &rtiv1.AdminDestroyFederationRequest{
		WireVersion:          rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:       "demo",
		EvictJoinedFederates: true,
	}); err != nil {
		t.Fatalf("DestroyFederation tolerating NotJoined: %v", err)
	}
}

// Destroy → unknown federation propagates the ErrFederationNotFound
// status.
func TestMutatingService_DestroyFederation_UnknownFederationPropagates(t *testing.T) {
	t.Parallel()
	store := &recordingFedStore{
		destroyErr: core.ErrFederationNotFound,
	}
	svc := newMutatingService(MutatingOptions{Federations: store})
	_, err := svc.DestroyFederation(context.Background(), &rtiv1.AdminDestroyFederationRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "ghost",
	})
	if !errors.Is(err, core.ErrFederationNotFound) && status.Code(err) != codes.NotFound {
		t.Errorf("expected ErrFederationNotFound (or NotFound code), got %v", err)
	}
}
