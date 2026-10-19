package save_restore_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

type preparedState struct {
	value   string
	commits int
}

func TestCompleteStateRequiresExplicitMembersAndAtomicDelivery(t *testing.T) {
	s := newStore(t)
	for _, members := range []bool{false, true} {
		opts := savepoint.Options{Outbox: &outbox{}, BundleStore: s,
			Roster: func(core.FederationName) []core.FederationMember {
				return []core.FederationMember{{Handle: 1, Name: "a"}, {Handle: 2, Name: "b"}}
			},
			ManagerSnapshots: map[string]savepoint.ManagerSnapshotter{"state": &preparedState{}}, RequiredSnapshots: []string{"state"}}
		if members {
			opts.Members = func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1, 2} }
			opts.Outbox = sendOnlyOutbox{&outbox{}}
		}
		if _, err := savepoint.New(opts); err == nil {
			t.Fatalf("unsafe configuration accepted; members=%v", members)
		}
	}
}

type sendOnlyOutbox struct{ target *outbox }

func (o sendOnlyOutbox) Send(ctx context.Context, fed core.FederationName, h core.FederateHandle, event core.OutboundEvent) error {
	return o.target.Send(ctx, fed, h, event)
}

func (s *preparedState) Marshal(core.FederationName) ([]byte, error) { return []byte(s.value), nil }
func (s *preparedState) Unmarshal(fed core.FederationName, data []byte) error {
	commit, err := s.PrepareRestore(fed, data)
	if err == nil {
		commit()
	}
	return err
}
func (s *preparedState) PrepareRestore(_ core.FederationName, data []byte) (func(), error) {
	value := string(data)
	if value == "corrupt" {
		return nil, errors.New("corrupt snapshot")
	}
	return func() { s.value = value; s.commits++ }, nil
}

func strictManager(t *testing.T, snap *preparedState, snapshots map[string][]byte, members *[]core.FederateHandle) (*savepoint.Manager, *outbox) {
	t.Helper()
	s := newStore(t)
	seed(t, s, savedStateValue, snapshots)
	o := &outbox{}
	m, err := savepoint.New(savepoint.Options{
		Outbox: o, BundleStore: s,
		Members: func(core.FederationName) []core.FederateHandle {
			return append([]core.FederateHandle(nil), (*members)...)
		},
		ManagerSnapshots:  map[string]savepoint.ManagerSnapshotter{"state": snap},
		RequiredSnapshots: []string{"state"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return m, o
}

func TestCompleteStateRestoreCommitsOnlyAfterAllParticipants(t *testing.T) {
	ctx := context.Background()
	members := []core.FederateHandle{1, 2}
	snap := &preparedState{value: liveStateValue}
	m, _ := strictManager(t, snap, map[string][]byte{"state": []byte(savedStateValue)}, &members)
	if err := m.RequestFederationRestore(ctx, "fed", savedStateValue); err != nil {
		t.Fatal(err)
	}
	if snap.value != liveStateValue || snap.commits != 0 {
		t.Fatal("request changed live state")
	}
	if err := m.FederateRestoreComplete(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if snap.value != liveStateValue || snap.commits != 0 {
		t.Fatal("first participant changed live state")
	}
	if err := m.FederateRestoreComplete(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	if snap.value != savedStateValue || snap.commits != 1 || m.QueryRestoreState("fed", savedStateValue) != savepoint.RestoreCompleted {
		t.Fatalf("state=%+v", snap)
	}
}

func TestCompleteStateRestoreFailureAndAbortPreserveLiveState(t *testing.T) {
	for _, abort := range []bool{false, true} {
		t.Run(map[bool]string{false: "participant-failed", true: "aborted"}[abort], func(t *testing.T) {
			ctx := context.Background()
			members := []core.FederateHandle{1, 2}
			snap := &preparedState{value: liveStateValue}
			m, _ := strictManager(t, snap, map[string][]byte{"state": []byte(savedStateValue)}, &members)
			if err := m.RequestFederationRestore(ctx, "fed", savedStateValue); err != nil {
				t.Fatal(err)
			}
			if err := m.FederateRestoreComplete(ctx, "fed", 1); err != nil {
				t.Fatal(err)
			}
			var err error
			if abort {
				err = m.AbortRestore(ctx, "fed")
			} else {
				err = m.FederateRestoreNotComplete(ctx, "fed", 2)
			}
			if err != nil {
				t.Fatal(err)
			}
			if snap.value != liveStateValue || snap.commits != 0 || m.QueryRestoreState("fed", savedStateValue) != savepoint.RestoreFailed {
				t.Fatalf("state=%+v", snap)
			}
		})
	}
}

func TestCompleteStateRestoreRejectsIncompleteAndIncompatibleBundles(t *testing.T) {
	for _, scenario := range []string{"missing", "corrupt", "rejoined", "extra-member", "unknown-snapshot"} {
		t.Run(scenario, func(t *testing.T) {
			members := []core.FederateHandle{1, 2}
			snapshots := map[string][]byte{"state": []byte(savedStateValue)}
			switch scenario {
			case "missing":
				delete(snapshots, "state")
			case "corrupt":
				snapshots["state"] = []byte("corrupt")
			case "rejoined":
				members = []core.FederateHandle{3, 4}
			case "extra-member":
				members = append(members, 3)
			case "unknown-snapshot":
				snapshots["future"] = []byte("data")
			}
			snap := &preparedState{value: liveStateValue}
			m, o := strictManager(t, snap, snapshots, &members)
			if err := m.RequestFederationRestoreBy(context.Background(), "fed", 1, savedStateValue); err == nil {
				t.Fatal("unsafe bundle accepted")
			}
			if snap.value != liveStateValue || snap.commits != 0 {
				t.Fatal("rejected restore changed live state")
			}
			for _, e := range o.snapshot() {
				if _, ok := e.event.(*savepoint.RestoreRequestFailedEvent); !ok {
					t.Fatalf("success callback %T", e.event)
				}
			}
		})
	}
}

func TestCompleteStateRestoreRevalidatesRosterAtCommit(t *testing.T) {
	ctx := context.Background()
	members := []core.FederateHandle{1, 2}
	snap := &preparedState{value: liveStateValue}
	m, _ := strictManager(t, snap, map[string][]byte{"state": []byte(savedStateValue)}, &members)
	if err := m.RequestFederationRestore(ctx, "fed", savedStateValue); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateRestoreComplete(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	members = []core.FederateHandle{1, 3}
	if err := m.FederateRestoreComplete(ctx, "fed", 2); !errors.Is(err, savepoint.ErrSaveBundleIncompatible) {
		t.Fatalf("commit error=%v", err)
	}
	if snap.value != liveStateValue || snap.commits != 0 || m.QueryRestoreState("fed", savedStateValue) != savepoint.RestoreFailed {
		t.Fatalf("state=%+v", snap)
	}
}

type failingInitiationOutbox struct{ outbox }

func (o *failingInitiationOutbox) Send(ctx context.Context, fed core.FederationName, h core.FederateHandle, event core.OutboundEvent) error {
	if _, ok := event.(*savepoint.InitiateRestoreEvent); ok && h == 2 {
		return errors.New("callback unavailable")
	}
	return o.outbox.Send(ctx, fed, h, event)
}

func TestCompleteStateRestoreNotificationFailurePreservesLiveState(t *testing.T) {
	s := newStore(t)
	seed(t, s, savedStateValue, map[string][]byte{"state": []byte(savedStateValue)})
	snap := &preparedState{value: liveStateValue}
	o := &failingInitiationOutbox{}
	m, err := savepoint.New(savepoint.Options{Outbox: o, BundleStore: s,
		Members:          func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1, 2} },
		ManagerSnapshots: map[string]savepoint.ManagerSnapshotter{"state": snap}, RequiredSnapshots: []string{"state"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.RequestFederationRestoreBy(context.Background(), "fed", 1, savedStateValue); err == nil {
		t.Fatal("lost initiation callback accepted")
	}
	if snap.value != liveStateValue || snap.commits != 0 || m.QueryRestoreState("fed", savedStateValue) != savepoint.RestoreFailed {
		t.Fatalf("state=%+v", snap)
	}
	if err = m.RequestFederationSave(context.Background(), "fed", "next", nil); err != nil {
		t.Fatalf("failed restore kept reservation: %v", err)
	}
}
