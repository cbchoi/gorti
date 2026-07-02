package savepoint

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// M36 DB-3 — IEEE 1516.1 §4.27 matches restore participants by
// federate NAME, not by handle. gorti never reuses handles across
// resign + rejoin, so routing initiateFederateRestore to the SAVED
// handles meant a federate that resigned and rejoined (same name, new
// handle) never received the initiate and its federateRestoreComplete
// was rejected with ErrFederateNotInRestore (traced via parity-CB on
// fm_save_restore_roundtrip).
//
// Scenario: save with roster {1:"pub", 2:"sub"}; "sub" resigns and
// rejoins as handle 7; restore must fan out to {1, 7}, carry the SAVED
// handle in the event payload, and accept completions from {1, 7}.
func TestRestore_RoutesByFederateName(t *testing.T) {
	store := newMemStore()
	roster := []core.FederationMember{
		{Handle: 1, Name: "pub"},
		{Handle: 2, Name: "sub"},
	}
	mgr, err := New(Options{
		Outbox:      &fakeOutbox{},
		BundleStore: store,
		Members: func(core.FederationName) []core.FederateHandle {
			out := make([]core.FederateHandle, 0, len(roster))
			for _, mem := range roster {
				out = append(out, mem.Handle)
			}
			return out
		},
		Roster: func(core.FederationName) []core.FederationMember { return roster },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.RequestFederationSave(ctx, "fed", "lbl", nil); err != nil {
		t.Fatalf("RequestFederationSave: %v", err)
	}
	for _, h := range []core.FederateHandle{1, 2} {
		if err := mgr.FederateSaveComplete(ctx, "fed", h); err != nil {
			t.Fatalf("FederateSaveComplete(%d): %v", h, err)
		}
	}

	// The bundle must have captured the names alongside the handles.
	manifest, err := mgr.LoadManifest("fed", "lbl")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(manifest.FederateNames) != 2 || manifest.FederateNames[0] != "pub" || manifest.FederateNames[1] != "sub" {
		t.Fatalf("manifest.FederateNames = %v, want [pub sub]", manifest.FederateNames)
	}

	// "sub" resigns and rejoins with a fresh handle.
	roster = []core.FederationMember{
		{Handle: 1, Name: "pub"},
		{Handle: 7, Name: "sub"},
	}

	out := &fakeOutbox{}
	mgr2, err := New(Options{
		Outbox:      out,
		BundleStore: store,
		Roster:      func(core.FederationName) []core.FederationMember { return roster },
	})
	if err != nil {
		t.Fatalf("New (restore side): %v", err)
	}
	if err := mgr2.RequestFederationRestore(ctx, "fed", "lbl"); err != nil {
		t.Fatalf("RequestFederationRestore: %v", err)
	}

	// initiateFederateRestore must be routed to the CURRENT handles.
	var dsts []core.FederateHandle
	savedByDst := map[core.FederateHandle]core.FederateHandle{}
	for _, rec := range out.Sent() {
		evt, ok := rec.Event.(*initiateFederateRestoreOutbound)
		if !ok {
			continue
		}
		dsts = append(dsts, rec.Federate)
		savedByDst[rec.Federate] = core.FederateHandle(
			evt.pb.GetRestoreInitiate().GetFederateHandle())
	}
	if len(dsts) != 2 || dsts[0] != 1 || dsts[1] != 7 {
		t.Fatalf("initiate destinations = %v, want [1 7]", dsts)
	}
	// Payload carries the handle the federate had AT SAVE TIME (§4.13).
	if savedByDst[1] != 1 || savedByDst[7] != 2 {
		t.Errorf("saved-handle payloads = %v, want {1:1, 7:2}", savedByDst)
	}

	// Completion must be accepted from the rejoined handle 7 ...
	if err := mgr2.FederateRestoreComplete(ctx, "fed", 7); err != nil {
		t.Errorf("FederateRestoreComplete(7): %v, want nil", err)
	}
	// ... and rejected from the stale saved handle 2.
	if err := mgr2.FederateRestoreComplete(ctx, "fed", 2); !errors.Is(err, core.ErrFederateNotInRestore) {
		t.Errorf("FederateRestoreComplete(2): %v, want ErrFederateNotInRestore", err)
	}
	if err := mgr2.FederateRestoreComplete(ctx, "fed", 1); err != nil {
		t.Errorf("FederateRestoreComplete(1): %v, want nil", err)
	}
	if got := mgr2.QueryRestoreState("fed", "lbl"); got != RestoreCompleted {
		t.Errorf("QueryRestoreState = %v, want RestoreCompleted", got)
	}
}

// TestRestore_NoRoster_FallsBackToSavedHandles: without a Roster
// resolver (or an old bundle without names) restore keeps the pre-M36
// handle-routed behavior.
func TestRestore_NoRoster_FallsBackToSavedHandles(t *testing.T) {
	store := newMemStore()
	mgr, err := New(Options{
		Outbox:      &fakeOutbox{},
		BundleStore: store,
		Members:     func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1, 2} },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	_ = mgr.RequestFederationSave(ctx, "fed", "lbl", nil)
	_ = mgr.FederateSaveComplete(ctx, "fed", 1)
	_ = mgr.FederateSaveComplete(ctx, "fed", 2)

	out := &fakeOutbox{}
	mgr2, err := New(Options{Outbox: out, BundleStore: store})
	if err != nil {
		t.Fatalf("New (restore side): %v", err)
	}
	if err := mgr2.RequestFederationRestore(ctx, "fed", "lbl"); err != nil {
		t.Fatalf("RequestFederationRestore: %v", err)
	}
	var dsts []core.FederateHandle
	for _, rec := range out.Sent() {
		if _, ok := rec.Event.(*initiateFederateRestoreOutbound); ok {
			dsts = append(dsts, rec.Federate)
		}
	}
	if len(dsts) != 2 || dsts[0] != 1 || dsts[1] != 2 {
		t.Errorf("initiate destinations = %v, want [1 2]", dsts)
	}
}
