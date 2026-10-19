package grpc

import (
	"context"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Recreate at the roster read rather than relying on scheduler timing. This
// reproduces the gap after generation validation and before eviction selection.
type recreatingAdminStore struct {
	stubFedStoreForServerTest
	generation   uint64
	recreateAt   string
	validated    bool
	members      []core.FederateInfo
	resignCalls  []core.FederateHandle
	destroyCalls []uint64
}

func (s *recreatingAdminStore) GenerationFor(core.FederationName) (uint64, bool) {
	s.validated = true
	return s.generation, true
}

func (*recreatingAdminStore) ValidateMember(core.FederationName, core.FederateHandle) error {
	return nil
}

func (s *recreatingAdminStore) AcquireGeneration(_ core.FederationName, expected uint64) (func(), error) {
	if expected != s.generation {
		return nil, core.ErrFederationGenerationMismatch
	}
	return func() {}, nil
}

func (s *recreatingAdminStore) recreate() {
	s.generation++
	s.members = []core.FederateInfo{{Name: "replacement", Handle: core.FederateHandle(s.generation<<32 | 1)}}
}

func (s *recreatingAdminStore) Snapshot() []core.FederationRoster {
	if !s.validated {
		panic("roster read before generation validation")
	}
	if s.recreateAt == "before-roster" {
		s.recreate()
	}
	roster := core.FederationRoster{Name: "fed", Generation: s.generation, Federates: append([]core.FederateInfo(nil), s.members...)}
	if s.recreateAt == "after-roster" {
		s.recreate()
	}
	return []core.FederationRoster{roster}
}

func (s *recreatingAdminStore) ResignFederation(_ context.Context, _ core.FederationName, h core.FederateHandle, _ core.ResignAction) error {
	s.resignCalls = append(s.resignCalls, h)
	for i, member := range s.members {
		if member.Handle == h {
			s.members = append(s.members[:i], s.members[i+1:]...)
			return nil
		}
	}
	return core.ErrFederateNotJoined
}

func (s *recreatingAdminStore) DestroyFederationGeneration(_ context.Context, _ core.FederationName, expected uint64) error {
	s.destroyCalls = append(s.destroyCalls, expected)
	if s.generation != expected {
		return core.ErrFederationGenerationMismatch
	}
	return nil
}

func TestAdminDestroyRejectsRecreationBeforeRosterSelection(t *testing.T) {
	expected := uint64(9)
	store := &recreatingAdminStore{generation: expected, recreateAt: "before-roster", members: []core.FederateInfo{{Name: "original", Handle: core.FederateHandle(expected<<32 | 1)}}}
	svc := newMutatingService(MutatingOptions{Federations: store, Membership: store})
	response, err := svc.DestroyFederation(context.Background(), &rtiv1.AdminDestroyFederationRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "fed", ExpectedFederationGeneration: &expected, EvictJoinedFederates: true,
	})
	if response != nil || status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("response=%v error=%v", response, err)
	}
	if len(store.resignCalls) != 0 {
		t.Fatalf("eviction crossed generation boundary: handles=%v", store.resignCalls)
	}
	if len(store.destroyCalls) != 0 {
		t.Fatalf("destroy called after roster mismatch: %v", store.destroyCalls)
	}
	want := []core.FederateInfo{{Name: "replacement", Handle: core.FederateHandle(10<<32 | 1)}}
	if !reflect.DeepEqual(store.members, want) {
		t.Fatalf("replacement members=%v, want %v", store.members, want)
	}
}

func TestAdminDestroyNeverRefreshesHandlesAfterRosterSelection(t *testing.T) {
	expected := uint64(9)
	original := core.FederateHandle(expected<<32 | 1)
	store := &recreatingAdminStore{generation: expected, recreateAt: "after-roster", members: []core.FederateInfo{{Name: "original", Handle: original}}}
	svc := newMutatingService(MutatingOptions{Federations: store, Membership: store})
	_, err := svc.DestroyFederation(context.Background(), &rtiv1.AdminDestroyFederationRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "fed", ExpectedFederationGeneration: &expected, EvictJoinedFederates: true,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("error=%v", err)
	}
	if !reflect.DeepEqual(store.resignCalls, []core.FederateHandle{original}) {
		t.Fatalf("eviction handles=%v, want original only", store.resignCalls)
	}
	if len(store.members) != 1 || store.members[0].Handle != core.FederateHandle(10<<32|1) {
		t.Fatalf("replacement members changed: %v", store.members)
	}
}

func TestAdminDestroyMatchingRosterGenerationEvictsAndDestroys(t *testing.T) {
	for _, expected := range []uint64{0, 9} {
		original := core.FederateHandle(expected<<32 | 1)
		store := &recreatingAdminStore{generation: expected, members: []core.FederateInfo{{Name: "original", Handle: original}}}
		svc := newMutatingService(MutatingOptions{Federations: store, Membership: store})
		response, err := svc.DestroyFederation(context.Background(), &rtiv1.AdminDestroyFederationRequest{
			WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "fed", ExpectedFederationGeneration: &expected, EvictJoinedFederates: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(response.GetEvictedHandles(), []uint64{uint64(original)}) {
			t.Fatalf("evicted=%v", response.GetEvictedHandles())
		}
		if !reflect.DeepEqual(store.resignCalls, []core.FederateHandle{original}) || !reflect.DeepEqual(store.destroyCalls, []uint64{expected}) {
			t.Fatalf("resign=%v destroy=%v", store.resignCalls, store.destroyCalls)
		}
		if len(store.members) != 0 {
			t.Fatalf("members remain=%v", store.members)
		}
	}
}
