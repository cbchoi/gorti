package federation_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
)

const (
	resignStagePrepare     = "prepare"
	resignStageJournal     = "journal"
	resignStageCommit      = "commit"
	federationStageSuccess = "success"
)

type r4ResignPreparation struct {
	commit  func() error
	release func()
}

func (p r4ResignPreparation) Commit() error { return p.commit() }
func (p r4ResignPreparation) Release()      { p.release() }

type r4ResignJournal struct {
	core.EventLog
	appendResign func() error
}

func (l r4ResignJournal) Append(_ context.Context, _ core.FederationName, event core.EventRecord) error {
	if _, ok := event.(*core.FederationResignedJournalRecord); ok {
		return l.appendResign()
	}
	return nil
}

func TestR4ResignOutcomeFaultOrder(t *testing.T) {
	fault := errors.New("injected resign failure")
	for _, stage := range []string{federationStageSuccess, resignStagePrepare, resignStageJournal, resignStageCommit} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			const fed core.FederationName = "r4-order"
			var manager *federation.Manager
			var handle core.FederateHandle
			var order []string
			retired := false
			joined := func() bool { return slices.Contains(manager.MembersOf(fed), handle) }
			opts := validOptions()
			opts.EventLog = r4ResignJournal{appendResign: func() error {
				order = append(order, resignStageJournal)
				if !joined() {
					t.Error("journal append observed a removed member")
				}
				if stage == resignStageJournal {
					return fault
				}
				return nil
			}}
			opts.PrepareResign = func(context.Context, core.FederationName, core.FederateHandle, core.ResignAction) (federation.ResignPreparation, error) {
				order = append(order, resignStagePrepare)
				if stage == resignStagePrepare {
					return nil, fault
				}
				return r4ResignPreparation{commit: func() error {
					order = append(order, resignStageCommit)
					if !joined() {
						t.Error("commit observed a removed member")
					}
					if stage == resignStageCommit {
						return fault
					}
					return nil
				}, release: func() { order = append(order, "release") }}, nil
			}
			opts.OnFederateResigning = func(context.Context, core.FederationName, core.FederateHandle, core.ResignAction) {
				order = append(order, "resigning")
				if !joined() {
					t.Error("pre-remove hook observed a removed member")
				}
			}
			opts.OnFederateResignOutcome = func(_ context.Context, f core.FederationName, h core.FederateHandle, action core.ResignAction, outcome error) {
				order = append(order, "outcome")
				// A sole member's supplied NoAction is normatively overridden by
				// DeleteObjects (IEEE 1516.1-2010 4.10); hook order is unchanged.
				if f != fed || h != handle || action != core.ResignActionDeleteObjects || joined() || retired {
					t.Errorf("wrong outcome boundary: federation=%s handle=%d action=%v joined=%v retired=%v", f, h, action, joined(), retired)
				}
				if (stage == resignStageCommit && outcome != fault) || (stage != resignStageCommit && outcome != nil) {
					t.Errorf("outcome=%v for stage %s", outcome, stage)
				}
			}
			opts.OnFederateResigned = func(context.Context, core.FederationName, core.FederateHandle) {
				order = append(order, "retired")
				retired = true
			}
			var err error
			manager, err = federation.New(opts)
			if err != nil {
				t.Fatal(err)
			}
			if err := manager.CreateFederation(ctx, core.CreateFederationRequest{Name: fed}); err != nil {
				t.Fatal(err)
			}
			handle, err = manager.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: "worker"})
			if err != nil {
				t.Fatal(err)
			}
			err = manager.ResignFederation(ctx, fed, handle, core.ResignActionNoAction)
			if (stage == federationStageSuccess && err != nil) || (stage != federationStageSuccess && !errors.Is(err, fault)) {
				t.Fatalf("service result changed: %v", err)
			}
			want := []string{resignStagePrepare, resignStageJournal, resignStageCommit, "resigning", "outcome", "retired", "release"}
			if stage == resignStagePrepare {
				want = []string{resignStagePrepare}
			} else if stage == resignStageJournal {
				want = []string{resignStagePrepare, resignStageJournal, "release"}
			}
			if !reflect.DeepEqual(order, want) {
				t.Fatalf("callback order=%v, want %v", order, want)
			}
			if wantJoined := stage == resignStagePrepare || stage == resignStageJournal; joined() != wantJoined {
				t.Fatalf("joined=%v after %s", joined(), stage)
			}
		})
	}
}

func TestR4ResignOutcomeRetainsExclusiveLease(t *testing.T) {
	ctx := context.Background()
	const fed core.FederationName = "r4-lease"
	var manager *federation.Manager
	var survivor core.FederateHandle
	started, admitted := make(chan struct{}), make(chan error, 1)
	checkBlocked := func(phase string) {
		select {
		case err := <-admitted:
			t.Errorf("survivor admitted during %s: %v", phase, err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	opts := validOptions()
	opts.OnFederateResignOutcome = func(context.Context, core.FederationName, core.FederateHandle, core.ResignAction, error) {
		go func() {
			close(started)
			release, err := manager.AcquireMember(fed, survivor)
			if release != nil {
				release()
			}
			admitted <- err
		}()
		<-started
		checkBlocked("outcome")
	}
	opts.OnFederateResigned = func(context.Context, core.FederationName, core.FederateHandle) { checkBlocked("retirement") }
	var err error
	manager, err = federation.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.CreateFederation(ctx, core.CreateFederationRequest{Name: fed}); err != nil {
		t.Fatal(err)
	}
	handle, err := manager.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	survivor, err = manager.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: "observer"})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.ResignFederation(ctx, fed, handle, core.ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-admitted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("exclusive lease leaked after retirement")
	}
}

func TestR4ResignOutcomeFencesIdentityAndAutomaticNoop(t *testing.T) {
	ctx := context.Background()
	const fed core.FederationName = "r4-identity"
	var outcomes []core.FederateHandle
	opts := validOptions()
	opts.GenerationEpoch = 7
	opts.OnFederateResignOutcome = func(_ context.Context, _ core.FederationName, h core.FederateHandle, _ core.ResignAction, _ error) {
		outcomes = append(outcomes, h)
	}
	manager, err := federation.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.CreateFederation(ctx, core.CreateFederationRequest{Name: fed}); err != nil {
		t.Fatal(err)
	}
	join := func(name string) core.MemberIdentity {
		t.Helper()
		h, err := manager.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: name})
		if err != nil {
			t.Fatal(err)
		}
		id, err := manager.MemberIdentity(fed, h)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	a, b := join("a"), join("b")
	if err := manager.AutomaticResignFederation(ctx, a, func() bool { return false }); err != nil || len(outcomes) != 0 {
		t.Fatalf("unexecuted automatic resign produced an outcome: %v %v", outcomes, err)
	}
	r, err := manager.PrepareRestoreIdentity(fed, a.Generation, []federation.RestoreIdentity{
		{Name: "a", CurrentHandle: a.Handle, SavedHandle: b.Handle},
		{Name: "b", CurrentHandle: b.Handle, SavedHandle: a.Handle},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(); err != nil {
		r.Release()
		t.Fatal(err)
	}
	r.Release()
	if err := manager.ResignFederationIdentity(ctx, a, core.ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(outcomes, []core.FederateHandle{b.Handle}) {
		t.Fatalf("restore rebound reported stale handle: %v", outcomes)
	}
	fresh := join("a")
	if err := manager.ResignFederationIdentity(ctx, a, core.ResignActionNoAction); !errors.Is(err, core.ErrFederateNotJoined) || len(outcomes) != 1 {
		t.Fatalf("stale incarnation reached outcome: %v %v", outcomes, err)
	}
	if err := manager.ValidateMember(fed, fresh.Handle); err != nil {
		t.Fatal(err)
	}
	for _, id := range []core.MemberIdentity{fresh, b} {
		if err := manager.ResignFederationIdentity(ctx, id, core.ResignActionNoAction); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.DestroyFederation(ctx, fed); err != nil {
		t.Fatal(err)
	}
	if err := manager.CreateFederation(ctx, core.CreateFederationRequest{Name: fed}); err != nil {
		t.Fatal(err)
	}
	current := join("a")
	before := len(outcomes)
	if err := manager.ResignFederationIdentity(ctx, fresh, core.ResignActionNoAction); !errors.Is(err, core.ErrFederateNotJoined) || len(outcomes) != before {
		t.Fatalf("stale generation reached outcome: %v %v", outcomes, err)
	}
	if err := manager.ValidateMember(fed, current.Handle); err != nil {
		t.Fatal(err)
	}
}
