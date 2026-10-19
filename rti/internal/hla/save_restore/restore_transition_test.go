package savepoint

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type transitionReservation struct {
	commit func()
	done   bool
}

func (r *transitionReservation) Commit() error {
	if !r.done {
		r.done = true
		r.commit()
	}
	return nil
}
func (r *transitionReservation) Release() { r.done = true }

type transitionOutbox struct{ fakeOutbox }

func (o *transitionOutbox) Reserve(ctx context.Context, fed core.FederationName, ds []core.OutboxDelivery) (core.OutboxReservation, error) {
	return &transitionReservation{commit: func() {
		for _, d := range ds {
			_ = o.Send(ctx, fed, d.Recipient, d.Event)
		}
	}}, nil
}

type transitionSnapshot struct {
	live   string
	key    string
	order  *[]string
	digest string
}

func (s *transitionSnapshot) Marshal(core.FederationName) ([]byte, error) { return []byte(s.live), nil }
func (s *transitionSnapshot) Unmarshal(_ core.FederationName, b []byte) error {
	s.live = string(b)
	return nil
}
func (s *transitionSnapshot) PrepareRestore(_ core.FederationName, b []byte) (func(), error) {
	if s.order != nil {
		*s.order = append(*s.order, "prepare:"+s.key)
	}
	return func() {
		if s.order != nil {
			*s.order = append(*s.order, "commit:"+s.key)
		}
		s.live = string(b)
	}, nil
}
func (s *transitionSnapshot) ValidateSnapshotDigest(b []byte, digest string) error {
	if string(b) != testSaved || digest != s.digest {
		return errors.New("saved FOM digest mismatch")
	}
	return nil
}

func TestRestoreTransitionRebindsOnlyAfterSuccessfulStateCommit(t *testing.T) {
	ctx := context.Background()
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "reserve-failure"}[fail], func(t *testing.T) {
			roster := []core.FederationMember{{Handle: 1, Name: "one"}}
			snap := &transitionSnapshot{live: testSaved}
			out := &transitionOutbox{}
			calls := 0
			m, err := New(Options{Outbox: out, BundleStore: newMemStore(), Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{roster[0].Handle} }, Roster: func(core.FederationName) []core.FederationMember { return roster }, RequiredSnapshots: []string{"state"}, ManagerSnapshots: map[string]ManagerSnapshotter{"state": snap},
				PrepareRestoreTransition: func(ctx context.Context, fed core.FederationName, participants []RestoreParticipant, terminal []core.OutboxDelivery) (core.OutboxReservation, error) {
					calls++
					if !reflect.DeepEqual(participants, []RestoreParticipant{{Name: "one", CurrentHandle: 7, SavedHandle: 1}}) {
						t.Fatalf("mapping: %+v", participants)
					}
					if len(terminal) != 1 || terminal[0].Recipient != 7 {
						t.Fatalf("terminal destinations: %+v", terminal)
					}
					if fail {
						return nil, errors.New("epoch reservation rejected")
					}
					return &transitionReservation{commit: func() {
						if snap.live != testSaved {
							t.Error("identity visible before state")
						}
						roster[0].Handle = 1
						_ = out.Send(ctx, fed, 1, terminal[0].Event)
					}}, nil
				}})
			if err != nil {
				t.Fatal(err)
			}
			completeSave(t, m, testSaved)
			snap.live = testChanged
			roster[0].Handle = 7
			if err := m.RequestFederationRestore(ctx, "fed", testSaved); err != nil {
				t.Fatal(err)
			}
			if calls != 0 || snap.live != testChanged || roster[0].Handle != 7 {
				t.Fatal("restore changed state before all participant responses")
			}
			if err := m.FederateRestoreComplete(ctx, "fed", 7); (err != nil) != fail {
				t.Fatalf("complete: %v", err)
			}
			if calls != 1 {
				t.Fatalf("transition calls %d", calls)
			}
			if fail {
				if snap.live != testChanged || roster[0].Handle != 7 {
					t.Fatal("rejected transition changed live state")
				}
			} else if snap.live != testSaved || roster[0].Handle != 1 {
				t.Fatal("saved identity/state not restored")
			}
		})
	}
}

func TestRestoreUsesSavedFOMDigestAndPreparesFOMFirst(t *testing.T) {
	order := []string{}
	generation := uint64(1)
	digest := testFOMDigest
	fom := &transitionSnapshot{live: testSaved, key: "fom", order: &order, digest: testFOMDigest}
	dependent := &transitionSnapshot{live: testSaved, key: "ddm", order: &order}
	opts := strictOptions(newMemStore(), &generation, nil)
	opts.Outbox = &transitionOutbox{}
	opts.FOMSHA256 = func(core.FederationName) (string, bool) { return digest, true }
	opts.RequiredSnapshots = []string{"fom", "ddm"}
	opts.ManagerSnapshots = map[string]ManagerSnapshotter{"fom": fom, "ddm": dependent}
	m, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	completeSave(t, m, "old-model")
	digest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fom.live = "extended"
	dependent.live = testChanged
	if err := m.RequestFederationRestore(context.Background(), "fed", "old-model"); err != nil {
		t.Fatal(err)
	}
	if fom.live != "extended" {
		t.Fatal("preparation published FOM")
	}
	if err := m.FederateRestoreComplete(context.Background(), "fed", 1); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"prepare:fom", "prepare:ddm", "commit:fom", "commit:ddm"}) {
		t.Fatalf("order: %v", order)
	}
}

func TestSaveRestoreParticipantModuleBaseline(t *testing.T) {
	ctx := context.Background()
	modules := []core.FOMModule{{Path: "saved.xml", XML: []byte("saved-module")}}
	var got []RestoreParticipant
	roster := []core.FederationMember{{Handle: 1, Name: "one"}}
	out := &transitionOutbox{}
	m, err := New(Options{Outbox: out, BundleStore: newMemStore(), Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{roster[0].Handle} }, Roster: func(core.FederationName) []core.FederationMember { return roster }, JoinedModules: func(context.Context, core.FederationName, core.FederateHandle) ([]core.FOMModule, error) {
		return modules, nil
	}, RequiredSnapshots: []string{"state"}, ManagerSnapshots: map[string]ManagerSnapshotter{"state": &transitionSnapshot{live: testSaved}}, PrepareRestoreTransition: func(_ context.Context, _ core.FederationName, p []RestoreParticipant, _ []core.OutboxDelivery) (core.OutboxReservation, error) {
		got = p
		return &transitionReservation{commit: func() {}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	completeSave(t, m, "modules")
	modules[0].XML[0] = 'X'
	roster[0].Handle = 7
	if err := m.RequestFederationRestore(ctx, "fed", "modules"); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateRestoreComplete(ctx, "fed", 7); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].ModulesKnown || len(got[0].SavedModules) != 1 || string(got[0].SavedModules[0].XML) != "saved-module" {
		t.Fatalf("saved module baseline lost: %+v", got)
	}
}

type leasedTransitionSnapshot struct {
	transitionSnapshot
	commitErr         error
	prepareErr        error
	commits, releases int
}
type leasedTransitionPrepared struct {
	owner *leasedTransitionSnapshot
	data  []byte
}

func (s *leasedTransitionSnapshot) PrepareRestoreState(_ core.FederationName, data []byte) (core.PreparedRestore, error) {
	return &leasedTransitionPrepared{owner: s, data: append([]byte(nil), data...)}, s.prepareErr
}
func (p *leasedTransitionPrepared) Commit() error {
	p.owner.commits++
	if p.owner.commitErr != nil {
		return p.owner.commitErr
	}
	p.owner.live = string(p.data)
	return nil
}
func (p *leasedTransitionPrepared) Release() { p.owner.releases++ }

func TestPreparedRestoreLeaseFailureAndAbortRelease(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"abort", "commit-failure", "prepare-failure", "success"} {
		t.Run(mode, func(t *testing.T) {
			leased := &leasedTransitionSnapshot{transitionSnapshot: transitionSnapshot{live: testSaved}}
			dependent := &transitionSnapshot{live: testSaved}
			m, err := New(Options{Outbox: &transitionOutbox{}, BundleStore: newMemStore(), Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} }, RequiredSnapshots: []string{"aaa", "state"}, ManagerSnapshots: map[string]ManagerSnapshotter{"aaa": leased, "state": dependent}})
			if err != nil {
				t.Fatal(err)
			}
			completeSave(t, m, "leased")
			leased.live, dependent.live = testLive, testLive
			failure := errors.New("prepared lease failed")
			if mode == "prepare-failure" {
				leased.prepareErr = failure
			}
			if mode == "commit-failure" {
				leased.commitErr = failure
			}
			err = m.RequestFederationRestore(ctx, "fed", "leased")
			if mode == "prepare-failure" {
				if !errors.Is(err, failure) {
					t.Fatal(err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if mode == "abort" {
					err = m.AbortRestore(ctx, "fed")
				} else {
					err = m.FederateRestoreComplete(ctx, "fed", 1)
				}
				if (mode == "commit-failure") != errors.Is(err, failure) {
					t.Fatalf("result: %v", err)
				}
			}
			if leased.releases != 1 {
				t.Fatalf("preparation released %d times", leased.releases)
			}
			if mode == "success" {
				if leased.live != testSaved || dependent.live != testSaved {
					t.Fatal("successful state not applied")
				}
			} else if leased.live != testLive || dependent.live != testLive {
				t.Fatal("failed restore changed state")
			}
		})
	}
}
