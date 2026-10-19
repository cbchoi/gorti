package savepoint

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type terminalPublicationLog struct {
	permissiveLog
	restored int
}

func (l *terminalPublicationLog) Append(_ context.Context, _ core.FederationName, record core.EventRecord) error {
	if event, ok := record.(*eventRecord); ok && event.kind == evtFederationRestored {
		l.restored++
	}
	return nil
}

type terminalPublicationReservation struct {
	commit func() error
	calls  int
}

func (r *terminalPublicationReservation) Commit() error {
	r.calls++
	return r.commit()
}
func (*terminalPublicationReservation) Release() {}

func TestTerminalPublicationFailureAfterImportBothFactories(t *testing.T) {
	t.Run("HLAfloat64Time", func(t *testing.T) { testTerminalPublicationFailureAfterImport[float64](t) })
	t.Run("HLAinteger64Time", func(t *testing.T) { testTerminalPublicationFailureAfterImport[int64](t) })
}

func testTerminalPublicationFailureAfterImport[T core.LogicalTimeScalar](t *testing.T) {
	for _, manifestAware := range []bool{false, true} {
		for _, identityCommitted := range []bool{false, true} {
			name := "plain/before-identity"
			if manifestAware {
				name = "manifest/before-identity"
			}
			if identityCommitted {
				name = strings.Replace(name, "before-identity", "after-identity", 1)
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				roster := []core.FederationMember{{Handle: 1, Name: "one"}}
				snapshot := &transitionSnapshot{live: testSaved}
				out := &transitionOutbox{}
				log := &terminalPublicationLog{}
				resumes, completionObservers := 0, 0
				failure := errors.New("injected terminal publication failure")
				reservation := &terminalPublicationReservation{commit: func() error {
					if snapshot.live != testSaved {
						t.Fatal("terminal commit did not follow domain import")
					}
					if identityCommitted {
						roster[0].Handle = 1
					}
					return failure
				}}
				opts := OptionsOf[T]{Outbox: out, EventLog: log, BundleStore: newMemStore(),
					Members:           func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{roster[0].Handle} },
					Roster:            func(core.FederationName) []core.FederationMember { return roster },
					RequiredSnapshots: []string{"state"}, ManagerSnapshots: map[string]ManagerSnapshotter{"state": snapshot},
					ResumeTime:      func(context.Context, core.FederationName) error { resumes++; return nil },
					OnSaveCompleted: func(context.Context, core.FederationName) { completionObservers++ },
				}
				prepare := func(_ context.Context, _ core.FederationName, participants []RestoreParticipant, terminal []core.OutboxDelivery) (core.OutboxReservation, error) {
					if len(participants) != 1 || participants[0].CurrentHandle != 7 || participants[0].SavedHandle != 1 || len(terminal) != 1 || terminal[0].Recipient != 7 {
						t.Fatal("unexpected restore transition identities")
					}
					return reservation, nil
				}
				if manifestAware {
					opts.PrepareRestoreTransitionWithManifest = func(ctx context.Context, fed core.FederationName, _ ManifestOf[T], participants []RestoreParticipant, terminal []core.OutboxDelivery) (core.OutboxReservation, error) {
						return prepare(ctx, fed, participants, terminal)
					}
				} else {
					opts.PrepareRestoreTransition = prepare
				}
				manager, err := NewOf(opts)
				if err != nil {
					t.Fatal(err)
				}
				for _, operation := range []func() error{
					func() error { return manager.RequestFederationSave(ctx, "fed", testSaved, nil) },
					func() error { return manager.FederateSaveBegun(ctx, "fed", 1) },
					func() error { return manager.FederateSaveComplete(ctx, "fed", 1) },
				} {
					if err := operation(); err != nil {
						t.Fatal(err)
					}
				}
				completionObservers = 0
				snapshot.live, roster[0].Handle = "changed", 7
				if err := manager.RequestFederationRestore(ctx, "fed", testSaved); err != nil {
					t.Fatal(err)
				}
				before := len(out.Sent())
				err = manager.FederateRestoreComplete(ctx, "fed", 7)
				if !errors.Is(err, failure) || !errors.Is(err, ErrRestoreStateCommitted) || !strings.Contains(err.Error(), "state committed") {
					t.Errorf("post-commit diagnostic lost: %v", err)
				}
				if snapshot.live != testSaved || reservation.calls != 1 {
					t.Error("post-commit failure rolled back or repeated domain/terminal commit")
				}
				wantHandle := core.FederateHandle(7)
				if identityCommitted {
					wantHandle = 1
				}
				if roster[0].Handle != wantHandle {
					t.Error("manager guessed or rolled back the terminal identity")
				}
				if state := manager.QueryRestoreState("fed", testSaved); state != RestoreFailed {
					t.Errorf("failed publication recorded restore success: %v", state)
				}
				if log.restored != 0 || completionObservers != 0 || len(out.Sent()) != before {
					t.Errorf("post-commit failure fabricated terminal completion: log=%d observer=%d callbacks=%d", log.restored, completionObservers, len(out.Sent())-before)
				}
				if manager.HasPendingScheduledSave("fed") {
					t.Error("post-commit failure scheduled time resumption")
				}
				if err := manager.ProgressScheduledSave(ctx, "fed"); err != nil || resumes != 0 {
					t.Errorf("post-commit failure resumed time: calls=%d err=%v", resumes, err)
				}
				if err := manager.FederateRestoreComplete(ctx, "fed", wantHandle); !errors.Is(err, core.ErrFederateNotInRestore) || reservation.calls != 1 {
					t.Errorf("closed failed publication retried: %v", err)
				}
			})
		}
	}
}

func TestTerminalPublicationSuccessStillCompletesBothFactories(t *testing.T) {
	t.Run("HLAfloat64Time", func(t *testing.T) { testTerminalPublicationSuccess[float64](t) })
	t.Run("HLAinteger64Time", func(t *testing.T) { testTerminalPublicationSuccess[int64](t) })
}

func testTerminalPublicationSuccess[T core.LogicalTimeScalar](t *testing.T) {
	ctx := context.Background()
	snapshot := &transitionSnapshot{live: testSaved}
	out, log := &transitionOutbox{}, &terminalPublicationLog{}
	resumes := 0
	manager, err := NewOf(OptionsOf[T]{Outbox: out, EventLog: log, BundleStore: newMemStore(),
		Members:           func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} },
		RequiredSnapshots: []string{"state"}, ManagerSnapshots: map[string]ManagerSnapshotter{"state": snapshot},
		ResumeTime: func(context.Context, core.FederationName) error { resumes++; return nil },
		PrepareRestoreTransition: func(ctx context.Context, fed core.FederationName, _ []RestoreParticipant, terminal []core.OutboxDelivery) (core.OutboxReservation, error) {
			return &terminalPublicationReservation{commit: func() error {
				if snapshot.live != testSaved {
					t.Fatal("success terminal preceded import")
				}
				return out.Send(ctx, fed, terminal[0].Recipient, terminal[0].Event)
			}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []func() error{
		func() error { return manager.RequestFederationSave(ctx, "fed", testSaved, nil) },
		func() error { return manager.FederateSaveBegun(ctx, "fed", 1) },
		func() error { return manager.FederateSaveComplete(ctx, "fed", 1) },
	} {
		if err := operation(); err != nil {
			t.Fatal(err)
		}
	}
	snapshot.live = "changed"
	if err := manager.RequestFederationRestore(ctx, "fed", testSaved); err != nil {
		t.Fatal(err)
	}
	before := len(out.Sent())
	if err := manager.FederateRestoreComplete(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if manager.QueryRestoreState("fed", testSaved) != RestoreCompleted || log.restored != 1 || len(out.Sent()) != before+1 || snapshot.live != testSaved {
		t.Fatal("successful restoration completion changed")
	}
	if _, ok := out.Sent()[before].Event.(*RestoredEvent); !ok {
		t.Fatal("success terminal body changed")
	}
	if !manager.HasPendingScheduledSave("fed") || resumes != 0 {
		t.Fatal("successful resume no longer follows terminal publication")
	}
	if err := manager.ProgressScheduledSave(ctx, "fed"); err != nil || resumes != 1 || manager.HasPendingScheduledSave("fed") {
		t.Fatalf("success resume: calls=%d err=%v", resumes, err)
	}
}
