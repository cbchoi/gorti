package save_restore_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func beginBoth(t *testing.T, m *savepoint.Manager, label string) {
	t.Helper()
	must(t, m.RequestFederationSave(context.Background(), "fed", label, nil))
	for _, h := range []core.FederateHandle{1, 2} {
		must(t, m.FederateSaveBegun(context.Background(), "fed", h))
	}
}
func TestSaveBegunRequiredAndStatuses(t *testing.T) {
	m, o := manager(t, newStore(t), nil)
	ctx := context.Background()
	if err := m.FederateSaveBegun(ctx, "fed", 1); !errors.Is(err, savepoint.ErrSaveNotInitiated) {
		t.Fatalf("begun without request: %v", err)
	}
	must(t, m.RequestFederationSave(ctx, "fed", "save", nil))
	for _, complete := range []func(context.Context, core.FederationName, core.FederateHandle) error{m.FederateSaveComplete, m.FederateSaveNotComplete} {
		if err := complete(ctx, "fed", 1); !errors.Is(err, core.ErrFederateNotInSave) {
			t.Fatalf("completion before begun: %v", err)
		}
	}
	if err := m.FederateSaveBegun(ctx, "fed", 99); !errors.Is(err, savepoint.ErrSaveNotInitiated) {
		t.Fatalf("unknown participant: %v", err)
	}
	label, entries, err := m.QueryFederationSaveStatus("fed")
	must(t, err)
	if label != "save" || len(entries) != 2 || entries[0].FederateHandle != 1 || entries[0].Status != savepoint.FederateInstructedToSave {
		t.Fatalf("initial statuses: %q %+v", label, entries)
	}
	must(t, m.FederateSaveBegun(ctx, "fed", 1))
	if err := m.FederateSaveBegun(ctx, "fed", 1); !errors.Is(err, savepoint.ErrSaveNotInitiated) {
		t.Fatalf("duplicate begun: %v", err)
	}
	_, entries, err = m.QueryFederationSaveStatus("fed")
	must(t, err)
	if entries[0].Status != savepoint.FederateSaving {
		t.Fatalf("not saving: %+v", entries)
	}
	must(t, m.FederateSaveComplete(ctx, "fed", 1))
	if err := m.FederateSaveNotComplete(ctx, "fed", 1); !errors.Is(err, core.ErrFederateNotInSave) {
		t.Fatalf("contradictory report: %v", err)
	}
	_, entries, err = m.QueryFederationSaveStatus("fed")
	must(t, err)
	if entries[0].Status != savepoint.FederateWaitingForFederationToSave || entries[1].Status != savepoint.FederateInstructedToSave {
		t.Fatalf("mixed statuses: %+v", entries)
	}
	must(t, m.FederateSaveBegun(ctx, "fed", 2))
	must(t, m.FederateSaveComplete(ctx, "fed", 2))
	if m.QuerySaveState("fed", "save") != savepoint.StateSaved {
		t.Fatal("save did not persist")
	}
	_, entries, err = m.QueryFederationSaveStatus("fed")
	must(t, err)
	for _, entry := range entries {
		if entry.Status != savepoint.NoSaveInProgress {
			t.Fatalf("terminal status: %+v", entry)
		}
	}
	saved := 0
	for _, e := range o.snapshot() {
		if _, ok := e.event.(*savepoint.SavedEvent); ok {
			saved++
		}
	}
	if saved != 2 {
		t.Fatalf("success callbacks=%d", saved)
	}
}

func TestParticipantFailureIsNotAbort(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	m, o := manager(t, s, nil)
	beginBoth(t, m, "failed")
	must(t, m.FederateSaveNotComplete(ctx, "fed", 1))
	if m.QuerySaveState("fed", "failed") != savepoint.StateInitiated {
		t.Fatal("failure report prematurely aborted save")
	}
	must(t, m.FederateSaveComplete(ctx, "fed", 2))
	if s.Exists("fed", "failed") || m.QuerySaveState("fed", "failed") != savepoint.StateNotSaved {
		t.Fatal("failed save persisted or succeeded")
	}
	for _, e := range o.snapshot() {
		if failure, ok := e.event.(*savepoint.NotSavedEvent); ok && failure.Reason != savepoint.FederateReportedFailureDuringSave {
			t.Fatalf("failure reason=%v", failure.Reason)
		}
	}
	seed(t, s, "restore", nil)
	must(t, m.RequestFederationRestore(ctx, "fed", "restore"))
	if err := m.FederateRestoreNotComplete(ctx, "fed", 99); !errors.Is(err, core.ErrFederateNotInRestore) {
		t.Fatalf("unknown restore participant: %v", err)
	}
	must(t, m.FederateRestoreNotComplete(ctx, "fed", 1))
	if err := m.FederateRestoreComplete(ctx, "fed", 1); !errors.Is(err, core.ErrFederateNotInRestore) {
		t.Fatalf("contradictory restore report: %v", err)
	}
	_, entries, err := m.QueryFederationRestoreStatus("fed")
	must(t, err)
	if entries[0].Status != savepoint.FederateWaitingForFederationToRestore || entries[1].Status != savepoint.FederateRestoring {
		t.Fatalf("restore statuses: %+v", entries)
	}
	must(t, m.FederateRestoreComplete(ctx, "fed", 2))
	if m.QueryRestoreState("fed", "restore") != savepoint.RestoreFailed {
		t.Fatal("failed restore succeeded")
	}
	failures := 0
	for _, e := range o.snapshot() {
		if f, ok := e.event.(*savepoint.NotRestoredEvent); ok {
			failures++
			if f.Reason != savepoint.FederateReportedFailureDuringRestore {
				t.Fatalf("restore reason=%v", f.Reason)
			}
		}
	}
	if failures != 2 {
		t.Fatalf("failure callbacks=%d", failures)
	}
}

func TestSaveRestoreExclusionAndAbortReasons(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	seed(t, s, "restore", nil)
	m, o := manager(t, s, nil)
	beginBoth(t, m, "save")
	if err := m.RequestFederationRestore(ctx, "fed", "restore"); !errors.Is(err, core.ErrSaveAlreadyInProgress) {
		t.Fatalf("overlapping restore: %v", err)
	}
	if _, _, err := m.QueryFederationRestoreStatus("fed"); !errors.Is(err, core.ErrSaveAlreadyInProgress) {
		t.Fatalf("restore status during save: %v", err)
	}
	must(t, m.AbortSave(ctx, "fed"))
	must(t, m.RequestFederationRestore(ctx, "fed", "restore"))
	if err := m.RequestFederationSave(ctx, "fed", "next", nil); !errors.Is(err, core.ErrRestoreAlreadyInProgress) {
		t.Fatalf("overlapping save: %v", err)
	}
	if _, _, err := m.QueryFederationSaveStatus("fed"); !errors.Is(err, core.ErrRestoreAlreadyInProgress) {
		t.Fatalf("save status during restore: %v", err)
	}
	must(t, m.AbortRestore(ctx, "fed"))
	saveAbort, restoreAbort := 0, 0
	for _, e := range o.snapshot() {
		switch f := e.event.(type) {
		case *savepoint.NotSavedEvent:
			if f.Reason == savepoint.SaveAborted {
				saveAbort++
			}
		case *savepoint.NotRestoredEvent:
			if f.Reason == savepoint.RestoreAborted {
				restoreAbort++
			}
		}
	}
	if saveAbort != 2 || restoreAbort != 2 {
		t.Fatalf("abort callbacks=%d,%d", saveAbort, restoreAbort)
	}
}

func TestStatusResolversRunOutsideSavepointLock(t *testing.T) {
	var m *savepoint.Manager
	var err error
	m, err = savepoint.New(savepoint.Options{Outbox: &outbox{}, BundleStore: newStore(t), Members: func(fed core.FederationName) []core.FederateHandle {
		_ = m.Snapshot(fed)
		return []core.FederateHandle{1}
	}})
	must(t, err)
	done := make(chan error, 1)
	go func() {
		_, _, err := m.QueryFederationSaveStatus("fed")
		if err == nil {
			_, _, err = m.QueryFederationRestoreStatus("fed")
		}
		if err == nil {
			err = m.RequestFederationSave(context.Background(), "fed", "resolver", nil)
		}
		done <- err
	}()
	must(t, await(t, done))
}
