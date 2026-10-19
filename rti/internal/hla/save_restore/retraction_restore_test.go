package savepoint

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type previewRetractionSnapshot struct {
	transitionSnapshot
	next     uint64
	err      error
	producer core.FederateHandle
}

func (s *previewRetractionSnapshot) PreviewNextRetractionHandle(_ core.FederationName, data []byte, producer core.FederateHandle) (uint64, error) {
	s.producer = producer
	if string(data) != testSaved {
		return 0, errors.New("preview did not receive saved ledger")
	}
	return s.next, s.err
}

func TestRestoreTerminalCarriesMergedRetractionHintBeforeReservation(t *testing.T) {
	ctx := context.Background()
	snapshot := &previewRetractionSnapshot{transitionSnapshot: transitionSnapshot{live: testSaved}, next: 101}
	member := core.FederateHandle(1)
	var terminal *RestoredEvent
	m, err := New(Options{
		Outbox: &transitionOutbox{}, BundleStore: newMemStore(),
		Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{member} },
		Roster: func(core.FederationName) []core.FederationMember {
			return []core.FederationMember{{Handle: member, Name: "one"}}
		},
		RequiredSnapshots: []string{"retractions"}, ManagerSnapshots: map[string]ManagerSnapshotter{"retractions": snapshot},
		PrepareRestoreTransition: func(_ context.Context, _ core.FederationName, _ []RestoreParticipant, ds []core.OutboxDelivery) (core.OutboxReservation, error) {
			terminal = ds[0].Event.(*RestoredEvent)
			if terminal.NextRetractionHandle != 201 {
				t.Errorf("terminal hint %d; want recomputed saved/current next 201 before queue cloning", terminal.NextRetractionHandle)
			}
			return &transitionReservation{commit: func() {}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	completeSave(t, m, testSaved)
	member = 7
	if err := m.RequestFederationRestore(ctx, "fed", testSaved); err != nil {
		t.Fatal(err)
	}
	snapshot.next = 201
	if err := m.FederateRestoreComplete(ctx, "fed", member); err != nil {
		t.Fatal(err)
	}
	if terminal == nil || snapshot.producer != 1 {
		t.Fatalf("terminal %+v preview producer %d", terminal, snapshot.producer)
	}
}

func TestRestoreRejectsRetractionExhaustionBeforeInitiation(t *testing.T) {
	snapshot := &previewRetractionSnapshot{transitionSnapshot: transitionSnapshot{live: testSaved}, err: core.ErrRetractionIDExhausted}
	out := &transitionOutbox{}
	m, err := New(Options{Outbox: out, BundleStore: newMemStore(), Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} }, RequiredSnapshots: []string{"retractions"}, ManagerSnapshots: map[string]ManagerSnapshotter{"retractions": snapshot}})
	if err != nil {
		t.Fatal(err)
	}
	completeSave(t, m, testSaved)
	if err := m.RequestFederationRestore(context.Background(), "fed", testSaved); !errors.Is(err, core.ErrRetractionIDExhausted) {
		t.Fatalf("exhausted restore: %v", err)
	}
}
